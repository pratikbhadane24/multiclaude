# multiclaude (`ccm`) Implementation Plan — Go

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `ccm` CLI plus a native launcher shim to run multiple Claude Code OAuth accounts in parallel, share one setup (including MCP servers), show per-account and combined plan limits, and expose every account as a T3 Code instance.

**Architecture:** Each account is a `CLAUDE_CONFIG_DIR` under `~/.claude-accounts/<name>`. Shared items are symlinked to `~/.claude`. A Go shim binary installed as `~/.claude-accounts/bin/claude` syncs MCP servers, then `syscall.Exec`s the real `claude`. Packages under `internal/` are small and pure where possible. Side effects (subprocess, HTTP, clock) go through small interfaces so they can be faked in tests. Built to grow into a larger CLI later.

**Tech Stack:** Go 1.27 (installed at `/opt/homebrew/bin/go`), standard library only (`encoding/json`, `net/http`, `crypto/sha256`, `os/exec`, `syscall`, `golang.org/x/text/unicode/norm` is the ONE allowed dependency, for NFC). `go test`, `go vet`. Module path `multiclaude`.

**Spec:** `docs/superpowers/specs/2026-11-19-multiclaude-design.md` (language switched from Bun/TS to Go by user decision 2026-11-19; everything else in the spec stands).

**Already known:** `docs/experiments/symlink-save.md`: `settings.json` and `plugins/` symlinks survive Claude CLI plugin writes. Interactive `/config` save is unverified → Task 12.

## Global Constraints

- macOS only. Real claude: env `CCM_CLAUDE_BIN` if set, else `~/.local/bin/claude`. The shim must refuse to exec itself (compare `filepath.EvalSymlinks` of both) → error `ccm: real claude resolves to the shim; set CCM_CLAUDE_BIN`.
- Accounts root: `~/.claude-accounts` (env `CCM_HOME` overrides). Main account = `~/.claude`, name `main`, `CLAUDE_CONFIG_DIR` unset.
- Account names: `^[a-z0-9-]{1,32}$`. Reserved: `main add login list run best use status sync t3-sync doctor remove shell-init help`.
- Account dir paths: absolute, NFC-normalized, no trailing slash, everywhere (registry, env, T3 `homePath`).
- Keychain service: `Claude Code-credentials` for main; `Claude Code-credentials-<sha256(NFC(dir)) hex[0:8]>` otherwise. Read only via `/usr/bin/security find-generic-password -s <svc> -w`.
- Keychain JSON: `claudeAiOauth.{accessToken, expiresAt (ms), subscriptionType, rateLimitTier}`.
- Usage API: `GET https://api.anthropic.com/api/oauth/usage`, headers `Authorization: Bearer <t>`, `anthropic-beta: oauth-2025-04-20`. Windows `five_hour`, `seven_day`, `seven_day_opus`, `seven_day_sonnet` = `{utilization: number|null, resets_at: string|null}`; `extra_usage` optional. Unknown keys ignored. HTTP timeout 10s.
- `claude auth status --json` fields used: `loggedIn, email, subscriptionType, planTier, loginExpiresAt`.
- Refresh = run `claude auth status` with the account env. Max 1 refresh per account per invocation. Never call OAuth endpoints directly.
- Tokens never printed, logged, written to disk, included in `--json`, or included in error strings.
- Never write `~/.claude.json` or anything under `~/.claude/`. Never edit `~/.zshrc`.
- Account `.claude.json` writes: re-read → change only `mcpServers` (preserve all other keys as `json.RawMessage`) → temp file in same dir, mode `0600` → `os.Rename`.
- Shim never blocks launching Claude: sync errors print `ccm: warning: …` to stderr, then exec continues.
- Shared items (link only if source exists): `CLAUDE.md settings.json rules skills plugins scripts agents commands hooks output-styles keybindings.json`.
- T3 instance: id `claude-<name>` (main → `claude-main`), `{"driver":"claudeAgent","enabled":true,"config":{"binaryPath":<shim abs>,"homePath":<dir or "" for main>}}`. Match existing by `homePath`. Back up `settings.json` to `settings.json.ccm-backup-<unix>` before writing.
- Tests: only `t.TempDir()`, fake `Runner`/`http.RoundTripper`/clock. No test touches real `~`, the Keychain, the network, or `~/.t3`. Destructive commands (`security delete-generic-password`) are only ever run through fakes in tests.
- Style: no mutation of inputs (return new values, copy maps), functions <50 lines, files <400 lines, named constants, errors wrapped with `%w` and context, `go vet` clean, `gofmt` clean.
- Commits: conventional (`feat:`, `chore:`, …), ending with a blank line + `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. Path given with `~`, trailing slash or relative → same Keychain service as Claude computes. Owned by Task 3 (test with trailing slash + NFD input).
2. `~/.claude.json` missing, unparseable or without `mcpServers` → shim warns, launches anyway; account file untouched. Task 5.
3. Usage API returns `null` windows, unknown keys, 429/500 or HTML → that account `api-error`/`n/a`, others render, combined row notes the exclusion. Tasks 7/9.
4. Two shims launching the same account at once → file never corrupt (atomic rename, compare-before-write). Task 5 (concurrent goroutines test).
5. Registered account with no Keychain item → `logged out → ccm login <name>`, no crash. Tasks 3/9.

---

### Task 1: Module scaffold, constants, paths, runner

**Files:** `go.mod`, `internal/consts/consts.go`, `internal/paths/paths.go`, `internal/paths/paths_test.go`, `internal/run/run.go`, `internal/run/run_test.go`, `Makefile`

**Interfaces (Produces):**
- `consts`: `AccountNameRE *regexp.Regexp`, `ReservedNames map[string]struct{}`, `SharedItems []string`, `UsageURL`, `UsageBeta = "oauth-2025-04-20"`, `KeychainBase = "Claude Code-credentials"`, `MainName = "main"`.
- `paths`: `type Paths struct{ Home, ClaudeDir, ClaudeJSON, AccountsRoot, ShimPath, RealClaude, T3Settings string }`; `Resolve(getenv func(string) string, home string) Paths`.
- `run`: `type Result struct{ Code int; Stdout, Stderr string }`; `type Runner interface{ Run(ctx context.Context, name string, args []string, env []string, inherit bool) (Result, error) }`; `OS Runner` (real, `os/exec`; `env == nil` → inherit process env; `inherit` → wire stdio to the terminal); `type Fake struct{ Calls []Call; Respond func(Call) (Result, error) }` (test helper, exported for other packages' tests).
- `Makefile`: `test` (`go test ./... -cover`), `vet`, `build` (→ `bin/ccm`, `bin/ccm-shim`).

- [ ] **Step 1: Failing tests:** `Resolve` with home `/h` and empty env → `ClaudeDir=/h/.claude`, `ClaudeJSON=/h/.claude.json`, `AccountsRoot=/h/.claude-accounts`, `ShimPath=/h/.claude-accounts/bin/claude`, `RealClaude=/h/.local/bin/claude`, `T3Settings=/h/.t3/userdata/settings.json`; `CCM_HOME=/x` → `AccountsRoot=/x`, `ShimPath=/x/bin/claude`; `CCM_CLAUDE_BIN=/b` → `RealClaude=/b`. `OS.Run` of `/bin/echo hi` → `Code 0`, `Stdout "hi\n"`; `/usr/bin/false` → `Code 1`, nil error. Reserved set has exactly 14 names and contains `t3-sync`.
- [ ] **Step 2:** `go test ./...` → FAIL. **Step 3:** implement. **Step 4:** `go test ./... && go vet ./...` → PASS.
- [ ] **Step 5:** Commit `chore: go module scaffold with paths and runner`.

### Task 2: Account registry

**Files:** `internal/registry/registry.go`, `internal/registry/registry_test.go`

**Interfaces:**
- Consumes: `paths.Paths`, `consts`.
- Produces: `type Account struct{ Name string \`json:"name"\`; Dir string \`json:"dir"\` /* "" = main */; Email string \`json:"email,omitempty"\` }`; `(a Account) IsMain() bool`; `NormalizeDir(p, home string) string`; `ValidateName(name string) error`; `Load(p paths.Paths) ([]Account, error)` (main always first; missing file → `[main]`; file `<AccountsRoot>/accounts.json`); `Save(p paths.Paths, accts []Account) error` (atomic, excludes main); `With(list []Account, a Account) []Account` (replace by name or append, new slice); `Without(list []Account, name string) []Account`; `Find(list []Account, name string) (Account, bool)`.

- [ ] **Step 1: Failing tests:** `ValidateName`: `work` ok; `Work`, `a b`, `""`, 33×`a`, `status` → error (reserved error message contains `reserved`). `NormalizeDir("~/.claude-accounts/work/", "/h")` → `/h/.claude-accounts/work`; `"x"` → `/h/x`; NFD `"é"` → NFC `"é"`. Load without file → `[{main}]`. Save→Load round-trip. `With` leaves its input slice unchanged. Corrupt file → error mentioning the path.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: account registry`.

### Task 3: Keychain service name + credential reader

**Files:** `internal/keychain/keychain.go`, `internal/keychain/keychain_test.go`

**Interfaces:**
- Consumes: `run.Runner`, `registry.Account`.
- Produces: `Service(dir string) string` (`""` → base); `type Creds struct{ AccessToken string; ExpiresAt int64; SubscriptionType, RateLimitTier string }`; `(c Creds) String() string` returns `Creds{redacted}`; `Read(ctx, r run.Runner, a registry.Account) (*Creds, error)` → `nil, nil` when `security` exits non-zero; `ErrCredsParse` on bad JSON (error text never contains stdout).

- [ ] **Step 1: Failing tests:** `Service("")` → `Claude Code-credentials`; `Service("/Users/test/.claude-accounts/work")` → `Claude Code-credentials-d9263ab7`. Fake runner exit 44 → `nil, nil`. Valid JSON → fields parsed. Invalid JSON containing `SECRET` → `errors.Is(err, ErrCredsParse)` and `err.Error()` lacks `SECRET`. `fmt.Sprint(creds)` lacks the token. The runner is called with `/usr/bin/security`, `[find-generic-password -s <svc> -w]`.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: per-account keychain service and creds reader`.

### Task 4: Shared symlinks (link, detect, careful fix)

**Files:** `internal/links/links.go`, `internal/links/links_test.go`

**Interfaces:**
- Produces: `type State string` (`StateOK "ok"`, `StateMissing "missing"`, `StateReplaced "replaced"`, `StateWrongTarget "wrong-target"`, `StateNoSource "no-source"`); `type Item struct{ Name string; State State }`; `Inspect(p paths.Paths, accountDir string) ([]Item, error)`; `LinkMissing(p, accountDir) ([]string, error)` (creates only missing; never touches existing real files); `type FixResult struct{ Action string /* "relinked-identical" | "relinked-with-backup" */; BackupPath string }`; `FixReplaced(p, accountDir, item string, now time.Time) (FixResult, error)`.

- [ ] **Step 1: Failing tests** (temp fake `~/.claude`): `LinkMissing` links only existing sources, and a second call returns empty. A replaced identical file → `Inspect` shows `replaced`; `FixReplaced` → `relinked-identical`, no backup. A replaced differing file → `relinked-with-backup`, backup `<item>.ccm-backup-<unix>` holds the account's content, item is a symlink again, source content and mtime unchanged. A replaced differing dir (`rules/`) → backup made by `os.Rename` (the dir inode is preserved) and relinked. An existing backup path → a new unique suffix, never overwritten.
- [ ] **Step 2:** FAIL. **Step 3:** implement (recursive byte compare for dirs). **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: shared symlinks with careful replace detection and fix`.

### Task 5: MCP sync + shim binary

**Files:** `internal/mcp/merge.go`, `internal/mcp/sync.go`, `internal/mcp/*_test.go`, `internal/shim/shim.go`, `internal/shim/shim_test.go`, `cmd/ccm-shim/main.go`

**Interfaces:**
- Produces:
  - pure `Merge(main, account map[string]json.RawMessage, prevSynced []string) (servers map[string]json.RawMessage, synced []string, changed bool)`. `synced` is sorted. Inputs are not mutated.
  - `Sync(p paths.Paths, accountDir string) (changed bool, err error)`. State `<AccountsRoot>/state.json` = `{"mcpSynced": {"<dir>": ["a","b"]}}` (atomic write). `ErrSource` when main `.claude.json` is missing or bad.
  - `shim.Main(args []string, env []string, deps shim.Deps) error` with `type Deps struct{ Paths paths.Paths; Exec func(argv0 string, argv []string, env []string) error /* syscall.Exec in prod */; Stderr io.Writer; Inspect func(p paths.Paths, dir string) ([]links.Item, error) }`. Syncs when `CLAUDE_CONFIG_DIR` is set and ≠ `ClaudeDir`; prints warnings; checks shim≠real; execs `RealClaude` with argv `["claude", args...]` and env unchanged.
  - `cmd/ccm-shim/main.go` wires the real deps; on exec failure prints `ccm: cannot start claude: <err>` and exits 127.

- [ ] **Step 1: Failing tests (`Merge`):** main `{a,b}`, acct `{x}`, prev `[]` → `{a,b,x}`, synced `[a b]`, changed. Main `{a}`, acct `{a,b,x}`, prev `[a b]` → `{a,x}`, synced `[a]`. Clash main `a=1`, acct `a=2` → `a=1`. Identical → not changed. Inputs unchanged (`reflect.DeepEqual` with copies).
- [ ] **Step 2: Failing tests (`Sync`):** non-`mcpServers` keys byte-equal after sync (compare `json.RawMessage`); mode `0600`; no change → mtime unchanged; main missing/corrupt → `errors.Is(err, ErrSource)` and the account file is untouched; account file missing → created; 20 goroutines calling `Sync` concurrently → final file valid JSON with the expected servers, and no `*.tmp` files left.
- [ ] **Step 3: Failing tests (`shim.Main`):** `CLAUDE_CONFIG_DIR` unset → fake Exec called with `RealClaude`, argv `["claude", ...args]`, the same env, and no sync. Sync error → stderr has `ccm: warning:` and Exec still called. Replaced link → stderr `ccm: warning: <item> is no longer shared — run ccm doctor`, Exec still called. `RealClaude` symlinked to `ShimPath` → error containing `CCM_CLAUDE_BIN`, Exec not called.
- [ ] **Step 4:** FAIL. **Step 5:** implement. **Step 6:** PASS; `go build ./cmd/ccm-shim`.
- [ ] **Step 7:** Commit `feat: native launcher shim with automatic one-way MCP sync`.

### Task 6: Auth + `ccm add`, `ccm login`

**Files:** `internal/auth/auth.go`, `internal/auth/auth_test.go`, `internal/cli/add.go`, `internal/cli/login.go`, `internal/cli/app.go`, `internal/cli/add_login_test.go`

**Interfaces:**
- Produces:
  - `auth.Env(a registry.Account, base []string) []string` (new slice; sets or removes `CLAUDE_CONFIG_DIR`).
  - `type Status struct{ LoggedIn bool; Email, SubscriptionType, PlanTier, LoginExpiresAt string }` (JSON tags as in the Global Constraints); `auth.GetStatus(ctx, r run.Runner, p paths.Paths, a registry.Account) (Status, error)` runs `<ShimPath> auth status --json`.
  - `auth.Refresh` = `GetStatus` (discarding the result).
  - `auth.Login(ctx, r, p, a, email string) (Status, error)` runs `<ShimPath> auth login --claudeai [--email e]` with inherited stdio, then `GetStatus`. `*MismatchError{Want, Got string}` when the email differs.
  - `cli.App struct{ Paths paths.Paths; Runner run.Runner; HTTP *http.Client; Now func() time.Time; Out, Err io.Writer; In io.Reader }` — shared by all commands.
  - `(*App) Add(ctx, name, email string) int`, `(*App) Login(ctx, target string /* name or "--all" */) int`.
- [ ] **Step 1: Failing tests:** `Env(main)` drops `CLAUDE_CONFIG_DIR`; `Env(work)` sets it; base slice unchanged. `Login` passes `--email you@x.com`; a mismatch → `*MismatchError`. An email equal to another account's → output contains `already used by account "<name>"`. `Add("status")` → 1 + reserved message. `Add("work","you@x.com")` (temp) → dir, links, MCP synced, registry entry with email, exactly one login call. Re-running `Add("work")` → resumes, no duplicate. `Login("--all")` logs in only `LoggedIn=false` accounts, in registry order. Output contains exactly: `Sign in as <email> in the browser. If it's signed in to another Claude account, open the printed URL in a private window.`
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: easy account add and login with email prefill and checks`.

### Task 7: Usage API client + aggregation

**Files:** `internal/usage/api.go`, `internal/usage/aggregate.go`, `internal/usage/*_test.go`

**Interfaces:**
- Produces:
  - `type Window struct{ UsedPct *float64; ResetsAt *time.Time }`; `type Usage struct{ FiveHour, SevenDay Window; SevenDayOpus, SevenDaySonnet *Window; Extra *Extra }`; `type Extra struct{ Enabled bool; UsedPct *float64 }`.
  - `Parse(b []byte) (Usage, error)`: tolerant; error only if not a JSON object.
  - `type State string` (`ok`, `logged-out`, `expired-refreshed`, `api-error`); `type AccountUsage struct{ Account, Email, Plan string; State State; Err string; Usage *Usage }`.
  - `type Fetcher struct{ Runner run.Runner; Paths paths.Paths; HTTP *http.Client; Now func() time.Time }`; `(f Fetcher) Fetch(ctx, a registry.Account) AccountUsage`: read creds → none → `logged-out`; `ExpiresAt <= now` → refresh once, re-read; GET; 401 → refresh once (if not already) then retry once; non-200 → `api-error` with `Err = "HTTP <code>"`.
  - `type Combined struct{ FiveHourLeft, SevenDayLeft float64; Counted int; Excluded []string; AvailableNow int; NextResetAt *time.Time }`; `Aggregate(list []AccountUsage) Combined` — left = Σ(100−used)/100 over `ok`/`expired-refreshed` accounts with non-nil used; available = both windows <100; next reset = soonest reset among windows ≥100.
- [ ] **Step 1: Failing tests:** `Parse` of `{"five_hour":{"utilization":42.5,"resets_at":"2026-11-19T20:00:00Z"},"seven_day":null,"iguana_necktie":{}}` → 5h 42.5, 7d nil, no error; `Parse("[]")` → error. Via `httptest.Server` + a URL override field: 401→200 → `expired-refreshed`, one refresh; 401→401 → `api-error`, one refresh; 500 with an HTML body → `Err == "HTTP 500"`; no creds → `logged-out` with zero HTTP calls; request headers exact; the token is absent from `fmt.Sprintf("%+v", result)`. `Aggregate` over [ok 40/10, ok 100/50 reset T, api-error x] → 5h left 0.6, 7d left 1.4, counted 2, excluded [x], available 1, next reset T.
- [ ] **Step 2:** FAIL. **Step 3:** implement (`Fetcher` has an unexported `url` field defaulting to `consts.UsageURL`, settable in tests via an option). **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: per-account usage fetch with safe refresh and combined totals`.

### Task 8: Today's tokens from transcripts

**Files:** `internal/tokens/tokens.go`, `internal/tokens/tokens_test.go`

**Interfaces:**
- Produces: `type Tokens struct{ Input, Output, CacheRead, CacheWrite int64 }`; `Today(projectsDir string, now time.Time) (Tokens, error)`. Walks `*.jsonl` with mtime ≥ local midnight. Counts lines with `type=="assistant"` and `message.usage` whose `timestamp` is today. Skips model `<synthetic>`. Dedupes on `message.id + ":" + requestId`, keeping the max per channel. Skips unparseable lines. `bufio.Scanner` with a 16 MB buffer.
- [ ] **Step 1: Failing tests:** two lines with the same key, output 10 then 25 → output 25. A `<synthetic>` line is ignored. A line timestamped yesterday is ignored. A garbage line is ignored. A missing dir → zero, nil.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: today's token totals per account`.

### Task 9: `ccm status` and `ccm best`

**Files:** `internal/cli/status.go`, `internal/cli/render.go`, `internal/cli/best.go`, `internal/cli/status_test.go`, `internal/cli/best_test.go`

**Interfaces:**
- Consumes: `usage.Fetcher`, `usage.Aggregate`, `tokens.Today`, `auth.GetStatus`, `links.Inspect`.
- Produces: `(*App) Status(ctx, jsonOut bool, watch time.Duration) int`; `RenderStatus(rows []Row, c usage.Combined, now time.Time) string` with `type Row struct{ usage.AccountUsage; Today tokens.Tokens; LinkWarnings []string }`; `PickBest(list []usage.AccountUsage) (usage.AccountUsage, bool)`; `(*App) Best(ctx, claudeArgs []string) int`.
- [ ] **Step 1: Failing tests:**
  - Header is exactly `ACCOUNT  EMAIL  PLAN  5H USED  5H LEFT  5H RESET  7D USED  7D LEFT  7D RESET  TODAY TOK  STATE` (tabwriter, columns may widen). One row per account.
  - `ALL` line format: `ALL  5h left 0.6/2 acct (30%) · 7d left 1.4/2 (70%) · available now 1 · next reset in 1h12m · excluded: x`.
  - A nil window renders `n/a`. Logged out → `logged out → ccm login <name>`.
  - A replaced link → line `⚠ <account>: <item> is no longer shared — run ccm doctor`.
  - `--json` → valid JSON with no `accessToken`/`AccessToken` substring.
  - Fetches run concurrently (errgroup-free: `sync.WaitGroup`). With 3 fakes × 100 ms delay, the total is < 250 ms.
  - Watch mode: injected clock; an account in error is retried only after 10 min.
  - `PickBest([90/20, 30/60, 100/x])` → second. All limited → false, and `Best` exits 2 printing `all accounts limited; next reset in …`.
  - `Best` execs the shim with `auth.Env`. Injected through `App.Runner` with `inherit=true`; the exit code is propagated.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: status dashboard and best-account launcher`.

### Task 10: `ccm t3-sync`

**Files:** `internal/t3/t3.go`, `internal/t3/t3_test.go`, `internal/cli/t3sync.go`

**Interfaces:**
- Produces: pure `Merge(settings []byte, accts []registry.Account, shimPath string) (out []byte, added, updated []string, err error)`. Works on `map[string]json.RawMessage` so unknown keys and fields round-trip. `(*App) T3Sync(ctx) int` (backup → atomic write → print `Restart T3 Code to apply.`).
- [ ] **Step 1: Failing tests:** an existing `codex` instance and unrelated keys round-trip byte-equal (compare `RawMessage`). An existing `claudeAgent` instance with matching `homePath` → updated in place, keeping its id and extra `config` fields (`customModels`, `launchArgs`), with `binaryPath` set to the shim. New accounts → `claude-<name>`. Main → `homePath: ""`. A second run → added and updated both empty. Empty/missing settings → creates `{"providerInstances":{…}}`.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: generate T3 Code claude instances per account`.

### Task 11: Remaining commands, dispatch, install

**Files:** `internal/cli/{run,use,shellinit,list,sync,doctor,remove,dispatch}.go`, tests alongside, `cmd/ccm/main.go`, `internal/cli/install.go`

**Interfaces:**
- Produces: `(*App) Main(ctx, args []string) int`. Commands:
  - `run <name> [args]` and `<name> [args]` (registered name) → shim with `auth.Env`, inherited stdio, exit code propagated.
  - `use <name>` → prints `export CLAUDE_CONFIG_DIR='<dir>'; export CCM_ACCOUNT='<name>'`. Main → `unset CLAUDE_CONFIG_DIR; export CCM_ACCOUNT='main'`. Single quotes are escaped.
  - `shell-init zsh` → `export PATH="<AccountsRoot>/bin:$PATH"`, plus a `ccm()` function: for `use`, `eval "$(command ccm use "$2")"`; else `command ccm "$@"`.
  - `list`, `sync` (`LinkMissing` + `mcp.Sync` for all accounts), `remove <name> [--purge]` (prompt `Remove account "<name>"? [y/N]` on `In`; `--purge` also `os.RemoveAll(dir)` and `security delete-generic-password -s <svc>` via the Runner).
  - `doctor [--fix]`: checks that the real claude exists and ≠ shim; the shim is present and executable; per account: links, MCP drift (`Merge(...).changed`), Keychain item present, T3 instance present; `t3` CLI on PATH (report only). `--fix` runs `LinkMissing`, `FixReplaced` (prints backup paths) and `mcp.Sync`. Exit 0 iff all ok.
  - `install`: builds nothing. It copies its own sibling `ccm-shim` binary to `ShimPath` (mode 0755), symlinks the `ccm` executable (`os.Executable`) into `~/.local/bin/ccm`, then prints the line to add: `eval "$(ccm shell-init zsh)"`.
  - `help`/unknown → usage text; unknown exits 64.
- [ ] **Step 1: Failing tests:** dispatch routes each command and an unknown one → 64. `use main` output exact. `use` with a quote in the dir is escaped. `shell-init zsh` contains `eval "$(command ccm use` and the PATH line. `doctor` with a replaced link → 1 and names the item; `--fix` → 0 and prints the backup path. `remove work` with input `n` → registry unchanged; `--purge` with `y` → the fake runner saw `delete-generic-password`.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** `go test ./... -cover` → total ≥ 80%; `go vet ./...` clean; `make build`.
- [ ] **Step 5:** Commit `feat: ccm commands, doctor, and installer`.

### Task 12: README + live E2E (with the user)

**Files:** `README.md`, `docs/e2e-checklist.md`

- [ ] **Step 1:** README: install (`make build && ./bin/ccm install`, add the shell-init line), `add`/`login`, parallel use, `status`/`best`, T3 (`ccm t3-sync`, restart, `t3 up` for phone), MCP auto-share + secret-duplication trade-off, remote OAuth MCP servers need a one-time authorization per account, symlink experiment results, undocumented usage endpoint caveat, uninstall.
- [ ] **Step 2: Live run with the user:** install → `ccm doctor` → `ccm add <second>` (user logs in) → `ccm main` and `ccm <second>` in two terminals at the same time, `/status` shows different emails → `ccm status` shows both + `ALL` → `ccm t3-sync`, restart T3, a thread per instance → `t3 up`, open from the phone → `claude mcp add -s user` a dummy server; a new `<second>` session lists it under `/mcp`; remove it and it's gone at the next launch → interactive `/config` change in an account: is `settings.json` still a symlink? Record in `docs/e2e-checklist.md`.
- [ ] **Step 3:** Commit `docs: README and e2e checklist`.
