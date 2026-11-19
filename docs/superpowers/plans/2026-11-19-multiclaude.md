# multiclaude (`ccm`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `ccm` CLI plus launcher shim to run multiple Claude Code OAuth accounts in parallel, share one setup (including MCP servers), show per-account and combined plan limits, and expose every account as a T3 Code instance.

**Architecture:** Each account is a `CLAUDE_CONFIG_DIR` under `~/.claude-accounts/<name>`. Shared items are symlinked to `~/.claude`. A shim at `~/.claude-accounts/bin/claude` syncs MCP servers, then execs the real `claude`. `ccm` modules are small and pure where possible. Side effects (subprocess, fs, fetch, clock) are injected so they can be tested.

**Tech Stack:** Bun 1.2.x, TypeScript (strict), `bun test`. No runtime deps.

**Spec:** `docs/superpowers/specs/2026-11-19-multiclaude-design.md`

## Global Constraints

- macOS only. Real claude: `~/.local/bin/claude` (resolve via `CCM_CLAUDE_BIN` env override, else that path).
- Accounts root: `~/.claude-accounts` (override `CCM_HOME` for tests). Main account = `~/.claude`, name `main`, `CLAUDE_CONFIG_DIR` unset.
- Account names: `^[a-z0-9-]{1,32}$`. Reserved: `main add login list run best use status sync t3-sync doctor remove shell-init help`.
- Account dir paths: absolute, NFC-normalized, no trailing slash, everywhere (registry, env, T3 `homePath`).
- Keychain service: `Claude Code-credentials` for main; `Claude Code-credentials-<sha256(NFC(dir)).hex[0..8]>` otherwise. Read only via `/usr/bin/security find-generic-password -s <svc> -w`.
- Keychain JSON: `claudeAiOauth.{accessToken, expiresAt(ms), subscriptionType, rateLimitTier}`.
- Usage API: `GET https://api.anthropic.com/api/oauth/usage`, headers `Authorization: Bearer <t>`, `anthropic-beta: oauth-2025-04-20`. Windows `five_hour`, `seven_day`, `seven_day_opus`, `seven_day_sonnet` = `{utilization: number|null, resets_at: string|null}`; `extra_usage` optional. Unknown keys are ignored.
- `claude auth status --json` fields used: `loggedIn, email, subscriptionType, planTier, loginExpiresAt`.
- Refresh = run `claude auth status` with the account env. Max 1 refresh per account per invocation. Never call OAuth endpoints directly.
- Tokens never printed, logged, written to disk, or included in `--json`.
- Never write `~/.claude.json` or anything under `~/.claude/`. Never edit `~/.zshrc`.
- Account `.claude.json` writes: re-read → change only `mcpServers` → temp file in same dir, mode `0600` → `rename`.
- Shim must never block launching Claude: all sync errors go to stderr as `ccm: warning: …` and then exec continues.
- Shared items (link only if source exists): `CLAUDE.md settings.json rules skills plugins scripts agents commands hooks output-styles keybindings.json`.
- T3 instance: id `claude-<name>` (main → `claude-main`), `{driver:"claudeAgent", enabled:true, config:{binaryPath:<shim abs>, homePath:<dir or "" for main>}}`. Match existing by `homePath`. Back up `settings.json` to `settings.json.ccm-backup-<ts>` before writing.
- Code style: no mutation of inputs (return new objects), functions <50 lines, files <400 lines, named constants.

## Review Focus

1. Path given with `~`, a trailing slash or a relative path → same Keychain service name as Claude computes (normalize before hashing). Owned by Task 3.
2. `~/.claude.json` missing, unparseable, or without `mcpServers` → shim warns and launches Claude anyway; account file untouched. Owned by Task 5.
3. Usage API returns `null` windows, extra unknown keys, a 429/500, or HTML → that account shows `usage API error`/`n/a`, the others still render, and the combined row notes the exclusion. Owned by Task 7/9.
4. Two shims launching the same account at the same moment → file is never corrupt (atomic rename, compare-before-write). Owned by Task 5.
5. Account registered but its Keychain item is missing (never logged in / logged out) → status `logged out → ccm login <name>`, no crash. Owned by Task 3/9.

---

### Task 1: Scaffold, constants, injectable runtime + symlink experiment

**Files:**
- Create: `package.json`, `tsconfig.json`, `.gitignore`, `src/constants.ts`, `src/runtime.ts`, `tests/runtime.test.ts`, `docs/experiments/symlink-save.md`

**Interfaces:**
- Produces:
  - `constants.ts`: `ACCOUNT_NAME_RE`, `RESERVED_NAMES: ReadonlySet<string>`, `SHARED_ITEMS: readonly string[]`, `USAGE_URL`, `USAGE_BETA = "oauth-2025-04-20"`, `KEYCHAIN_BASE = "Claude Code-credentials"`.
  - `runtime.ts`: `interface Paths { home; claudeDir; claudeJson; accountsRoot; shimPath; realClaude; t3Settings }`, `resolvePaths(env: Record<string,string|undefined>, home: string): Paths`; `interface Exec { (cmd: string, args: string[], opts?: {env?: Record<string,string>; inherit?: boolean}): Promise<{code:number; stdout:string; stderr:string}> }`; `realExec: Exec` (Bun.spawn); `type Fetch = typeof fetch`; `interface Runtime { paths: Paths; exec: Exec; fetch: Fetch; now: () => number; log: (m:string)=>void; warn: (m:string)=>void }`.

- [ ] **Step 1: Symlink experiment.** In a temp `CLAUDE_CONFIG_DIR` with `settings.json` symlinked to a copy of `~/.claude/settings.json`, run a `claude` command that saves settings (e.g. `claude config set` or toggling via `/config` in a throwaway session). Check if the link is still a symlink (`test -L`). Repeat for `plugins/` (install a tiny plugin). Write results to `docs/experiments/symlink-save.md`. If `settings.json` gets replaced, note the fallback from spec §Decisions 3.
- [ ] **Step 2: Failing tests** `resolvePaths` → with `HOME=/h` and no overrides: `claudeDir=/h/.claude`, `accountsRoot=/h/.claude-accounts`, `shimPath=/h/.claude-accounts/bin/claude`, `realClaude=/h/.local/bin/claude`, `t3Settings=/h/.t3/userdata/settings.json`. `CCM_HOME=/x` → `accountsRoot=/x`. `CCM_CLAUDE_BIN=/b` → `realClaude=/b`.
- [ ] **Step 3:** `bun test tests/runtime.test.ts` → FAIL (module missing).
- [ ] **Step 4:** Implement `constants.ts` and `runtime.ts`.
- [ ] **Step 5:** `bun test` → PASS. `bunx tsc --noEmit` → clean.
- [ ] **Step 6:** Commit `chore: scaffold ccm with runtime paths and symlink experiment`.

### Task 2: Account registry

**Files:** Create `src/registry.ts`, `tests/registry.test.ts`

**Interfaces:**
- Consumes: `Paths`, `ACCOUNT_NAME_RE`, `RESERVED_NAMES`.
- Produces: `interface Account { name: string; dir: string | null /* null = main */; email?: string }`; `normalizeDir(p: string, home: string): string`; `validateName(name: string): {ok:true}|{ok:false; reason:string}`; `loadAccounts(paths): Promise<Account[]>` (main always first; missing file → `[main]`); `saveAccounts(paths, accounts: Account[]): Promise<void>` (atomic, excludes main); `withAccount(list, acct): Account[]`; `withoutAccount(list, name): Account[]`; `findAccount(list, name): Account | undefined`.

- [ ] **Step 1: Failing tests:**
  - `validateName("work")` ok; `"Work"`, `"a b"`, `""`, 33 chars, `"status"` → not ok with reason.
  - `normalizeDir("~/.claude-accounts/work/", "/h")` → `"/h/.claude-accounts/work"`. Relative `"x"` resolves against home. NFD `"é"` input comes back NFC.
  - load with no file → `[{name:"main", dir:null}]`. save then load round-trips. `withAccount` doesn't mutate its input (deep-equal original after).
  - corrupt `accounts.json` → `loadAccounts` throws an error whose message contains the file path.
- [ ] **Step 2:** run → FAIL. **Step 3:** implement. **Step 4:** run → PASS.
- [ ] **Step 5:** Commit `feat: add account registry`.

### Task 3: Keychain service name + credential reader

**Files:** Create `src/keychain.ts`, `tests/keychain.test.ts`

**Interfaces:**
- Consumes: `Exec`, `Account`, `normalizeDir`.
- Produces: `keychainService(dir: string | null): string`; `interface Creds { accessToken: string; expiresAt: number; subscriptionType?: string; rateLimitTier?: string }`; `readCreds(exec: Exec, acct: Account): Promise<Creds | null>` (null when `security` exits non-zero, e.g. item not found); throws `CredsParseError` on malformed JSON (the message must not include the secret).

- [ ] **Step 1: Failing tests:**
  - `keychainService(null)` → `"Claude Code-credentials"`.
  - `keychainService("/Users/test/.claude-accounts/work")` → `"Claude Code-credentials-d9263ab7"` (vector computed from Claude's algorithm).
  - fake exec returning code 44 → `readCreds` returns `null`. Fake exec returning valid JSON → `accessToken`/`expiresAt` parsed. Invalid JSON containing the text `SECRET` → throws, and `String(err)` does not contain `SECRET`.
  - exec called with `["find-generic-password","-s",<svc>,"-w"]` on `/usr/bin/security`.
- [ ] **Step 2:** FAIL. **Step 3:** implement (`crypto.createHash("sha256")`). **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: compute per-account keychain service and read creds`.

### Task 4: Shared symlinks (link, detect, careful fix)

**Files:** Create `src/shared-links.ts`, `tests/shared-links.test.ts`

**Interfaces:**
- Consumes: `Paths`, `SHARED_ITEMS`.
- Produces: `type LinkState = {item:string; state:"ok"|"missing"|"replaced"|"wrong-target"|"no-source"}`; `inspectLinks(paths, accountDir): Promise<LinkState[]>`; `linkShared(paths, accountDir): Promise<string[]>` (creates missing links, returns created; never touches existing real files); `fixReplaced(paths, accountDir, item, now: number): Promise<{action:"relinked-identical"|"relinked-with-backup"; backupPath?: string}>`.

- [ ] **Step 1: Failing tests** (in a tmp dir fake `~/.claude`):
  - `linkShared` links only items that exist in the source; second call creates nothing.
  - item replaced with an identical real file → `inspectLinks` shows `replaced`; `fixReplaced` → `relinked-identical`, no backup file.
  - replaced with different content → `relinked-with-backup`, backup exists at `<item>.ccm-backup-<now>` with the account's content, item is a symlink again, source untouched.
  - replaced directory (e.g. `rules/`) differing → backup dir moved (not copied-then-deleted), relinked.
  - `fixReplaced` never deletes an existing backup; the source file's mtime is unchanged.
- [ ] **Step 2:** FAIL. **Step 3:** implement. Compare with recursive content equality for dirs. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: shared symlinks with careful replace detection and fix`.

### Task 5: MCP sync + launcher shim

**Files:** Create `src/mcp-sync.ts`, `src/shim.ts`, `tests/mcp-sync.test.ts`, `tests/shim.test.ts`

**Interfaces:**
- Consumes: `Paths`, `normalizeDir`.
- Produces:
  - pure `mergeMcp(main: Record<string,unknown>, account: Record<string,unknown>, prevSynced: string[]): {servers: Record<string,unknown>; synced: string[]; changed: boolean}`.
  - `syncMcp(paths, accountDir): Promise<{changed:boolean}>`. State in `<accountsRoot>/state.json` → `{ mcpSynced: { [dir]: string[] } }`.
  - `shimMain(argv: string[], env, rt: Runtime): Promise<never>`: syncs when `CLAUDE_CONFIG_DIR` is set and is not `claudeDir`; warns on any error; then `exec` replaces the process (`Bun.spawn` inherit + `process.exit(code)`, forwarding signals).
  - `renderShimScript(bunPath: string, shimModulePath: string): string`: shebang `#!<abs bun>` file that imports `shimMain`.

- [ ] **Step 1: Failing tests (`mergeMcp`):**
  - main `{a,b}`, account `{x}`, prev `[]` → servers `{a,b,x}`, synced `[a,b]`, changed true.
  - main `{a}` (b removed), account `{a,b,x}`, prev `[a,b]` → `{a,x}`, synced `[a]`.
  - name clash: main `a=1`, account `a=2`, prev `[]` → `a=1`.
  - identical inputs → changed false.
  - inputs are deep-equal to the originals after the call.
- [ ] **Step 2: Failing tests (`syncMcp`, tmp fs):**
  - account `.claude.json` keeps every non-`mcpServers` key byte-for-byte equal (parse-compare). File mode is `0600`.
  - no change → file mtime unchanged.
  - main `.claude.json` missing or corrupt → throws a typed `McpSourceError`; account file untouched.
  - account `.claude.json` missing → created with only `mcpServers`.
  - 20 concurrent `syncMcp` calls → final file parses and has the expected servers.
- [ ] **Step 3: Failing tests (`shimMain` with fake exec):**
  - `CLAUDE_CONFIG_DIR` unset → no sync, exec real claude with the same argv/env.
  - sync throws → stderr contains `ccm: warning:` and exec still happens.
  - account with a replaced shared link → stderr `ccm: warning: <item> is no longer shared — run ccm doctor`, exec still happens (uses `inspectLinks` from Task 4).
- [ ] **Step 4:** FAIL. **Step 5:** implement. **Step 6:** PASS.
- [ ] **Step 7:** Commit `feat: launcher shim with automatic one-way MCP sync`.

### Task 6: Auth (status / login / refresh) + `ccm add`, `ccm login`

**Files:** Create `src/auth.ts`, `src/commands/add.ts`, `src/commands/login.ts`, `tests/auth.test.ts`, `tests/commands/add-login.test.ts`

**Interfaces:**
- Consumes: `Runtime`, registry, `linkShared`, `syncMcp`, `keychainService`.
- Produces:
  - `accountEnv(acct: Account, base: Record<string,string>): Record<string,string>` (sets or deletes `CLAUDE_CONFIG_DIR`, returns a new object).
  - `authStatus(rt, acct): Promise<{loggedIn:boolean; email?:string; subscriptionType?:string; planTier?:string; loginExpiresAt?:string}>` (runs `claude auth status --json` through the shim path).
  - `refreshAuth(rt, acct): Promise<void>` = `authStatus` (side effect: CLI refreshes).
  - `login(rt, acct, email?): Promise<{email:string}>`: runs `claude auth login --claudeai [--email e]` inheriting stdio; then `authStatus`. Throws `LoginMismatchError` (with expected/actual) on email mismatch.
  - `cmdAdd(rt, name, opts:{email?:string}): Promise<number>`, `cmdLogin(rt, name | "--all"): Promise<number>`.

- [ ] **Step 1: Failing tests:**
  - `accountEnv(main)` has no `CLAUDE_CONFIG_DIR`; for `work` it's the dir; the input env is not mutated.
  - `login` passes `--email you@x.com` when given; mismatch → `LoginMismatchError`; email equal to another registered account's → the result flags `duplicateOf: "<name>"` and `cmdLogin` prints a warning naming it.
  - `cmdAdd("status")` → exit 1 with the reserved-name message. `cmdAdd("work")` in tmp → dir created, links made, MCP synced, registry has `work` with email, login invoked once. Re-running `cmdAdd("work")` resumes (no error, no duplicate entry).
  - `cmdLogin("--all")` logs in only accounts where `loggedIn=false`, in registry order.
  - Printed guidance contains the line `Sign in as <email> in the browser. If it's signed in to another Claude account, open the printed URL in a private window.`
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: easy account add and login with email prefill and mismatch checks`.

### Task 7: Usage API client + aggregation

**Files:** Create `src/usage/api.ts`, `src/usage/aggregate.ts`, `tests/usage/api.test.ts`, `tests/usage/aggregate.test.ts`

**Interfaces:**
- Consumes: `Creds`, `readCreds`, `refreshAuth`, `Fetch`.
- Produces:
  - `type Window = {usedPct: number|null; resetsAt: number|null /* ms */}`; `type Usage = {fiveHour: Window; sevenDay: Window; sevenDayOpus?: Window; sevenDaySonnet?: Window; extra?: {enabled:boolean; usedPct:number|null}}`.
  - `parseUsage(json: unknown): Usage` (tolerant: missing/null → `{usedPct:null, resetsAt:null}`; ignores unknown keys; throws `UsageShapeError` if not an object).
  - `type AccountUsage = {account: string; email?: string; plan?: string; state: "ok"|"logged-out"|"expired-refreshed"|"api-error"; error?: string; usage?: Usage}`.
  - `fetchAccountUsage(rt, acct): Promise<AccountUsage>`: read creds → if missing → `logged-out`; if `expiresAt <= now` → refresh once, re-read; GET; on 401 → refresh once then retry once; non-200 → `api-error` with status code (never the body if it contains `Bearer`).
  - `aggregate(list: AccountUsage[], now:number): {fiveHourRemaining: number; sevenDayRemaining: number /* sum of (100-used)/100 over ok accounts */; counted: number; excluded: string[]; availableNow: number /* both windows <100 */; nextResetAt: number|null /* soonest reset among accounts with a window >=100 */}`.

- [ ] **Step 1: Failing tests:**
  - `parseUsage({five_hour:{utilization:42.5,resets_at:"2026-11-19T20:00:00Z"}, seven_day:null, iguana_necktie:{}})` → `fiveHour.usedPct=42.5`, `sevenDay.usedPct=null`, no throw.
  - fetch 401 then 200 → state `expired-refreshed`, refresh called exactly once. Fetch 401 twice → `api-error`, refresh called once.
  - fetch 500 with an HTML body → `api-error` with error `HTTP 500`.
  - no creds → `logged-out`, fetch not called.
  - request carries both headers exactly. A test asserts the token string never appears in `JSON.stringify(result)`.
  - aggregate over [ok 5h=40/7d=10, ok 5h=100/7d=50 resets T, api-error] → `fiveHourRemaining=0.6`, `sevenDayRemaining=1.4`, `counted=2`, `excluded=[third]`, `availableNow=1`, `nextResetAt=T`.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: per-account usage fetch with safe refresh and combined aggregation`.

### Task 8: Today's tokens from transcripts

**Files:** Create `src/usage/tokens.ts`, `tests/usage/tokens.test.ts`

**Interfaces:**
- Produces: `type Tokens = {input:number; output:number; cacheRead:number; cacheWrite:number}`; `tokensToday(projectsDir: string, now: number): Promise<Tokens>`. Scans `**/*.jsonl` with mtime ≥ local midnight. Counts `type:"assistant"` lines with `message.usage`, skips model `<synthetic>`, dedupes by `message.id:requestId` keeping the max per channel (same rule as T3 `claudeUsage.ts`). Skips unparseable lines.

- [ ] **Step 1: Failing tests:** fixture with two lines sharing a dedupe key (output 10 then 25) → output 25. A `<synthetic>` line is ignored. A line from yesterday (by `timestamp`) is ignored. A garbage line is ignored.
- [ ] **Step 2:** FAIL. **Step 3:** implement (stream lines; no full-file regex). **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: today's token totals per account`.

### Task 9: `ccm status` and `ccm best`

**Files:** Create `src/commands/status.ts`, `src/render/table.ts`, `src/commands/best.ts`, `tests/commands/status.test.ts`, `tests/commands/best.test.ts`

**Interfaces:**
- Consumes: `fetchAccountUsage`, `aggregate`, `tokensToday`, `authStatus`, `inspectLinks`.
- Produces: `cmdStatus(rt, opts:{json:boolean; watch?:number}): Promise<number>`; `renderStatus(rows, agg, now): string`; `pickBest(list: AccountUsage[]): AccountUsage | null` (ok accounts with both windows <100, lowest `max(5h,7d)`, ties → registry order); `cmdBest(rt, claudeArgs: string[]): Promise<number>` (prints the chosen account + reason, then execs the shim with `accountEnv`).

- [ ] **Step 1: Failing tests:**
  - Rendered table has a header `ACCOUNT  EMAIL  PLAN  5H USED  5H LEFT  5H RESET  7D USED  7D LEFT  7D RESET  TODAY TOK  STATE`, one row per account, and a `ALL` row like `5h left 0.6/2 acct (30%) · 7d left 1.4/2 (70%) · available now 1 · next reset in 1h12m · excluded: x`.
  - `null` window renders `n/a`. Logged-out row state reads `logged out → ccm login <name>`.
  - A replaced shared link adds one line `⚠ <account>: <item> is no longer shared — run ccm doctor`.
  - `--json` output parses and has no `accessToken` key anywhere.
  - Accounts are fetched concurrently (fake fetch with delays: total time < sum).
  - `--watch 30`: refresh happens at most once per account per its token `expiresAt` or 10 min (inject clock).
  - `pickBest`: [5h 90/7d 20, 5h 30/7d 60, 5h 100] → second. All limited → `null`, and `cmdBest` exits 2 with `all accounts limited; next reset in …`.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: status dashboard and best-account launcher`.

### Task 10: `ccm t3-sync`

**Files:** Create `src/t3.ts`, `src/commands/t3-sync.ts`, `tests/t3.test.ts`

**Interfaces:**
- Produces: pure `mergeT3(settings: unknown, accounts: Account[], shimPath: string): {settings: object; added: string[]; updated: string[]}`; `cmdT3Sync(rt): Promise<number>` (backup → write atomically → print "restart T3 Code to apply").

- [ ] **Step 1: Failing tests:**
  - Existing `codex` instance and unrelated keys are preserved unchanged.
  - Existing `claudeAgent` instance whose `homePath` equals an account dir → updated in place (keeps its id and extra config fields such as `customModels`, `launchArgs`), `binaryPath` set to the shim.
  - New accounts → `claude-<name>` entries. Main → `homePath: ""`.
  - Running twice → second result has `added=[]` and `updated=[]`.
  - Input object is not mutated.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: generate T3 Code claude instances per account`.

### Task 11: Remaining commands, CLI dispatch, install

**Files:** Create `src/commands/{run,use,shell-init,list,sync,doctor,remove}.ts`, `src/cli.ts`, `bin/ccm`, `scripts/install.ts`, `tests/cli.test.ts`, `tests/commands/doctor.test.ts`

**Interfaces:**
- Consumes: everything above.
- Produces: `main(argv: string[], rt: Runtime): Promise<number>`; `bin/ccm` = `#!/usr/bin/env bun` → `process.exit(await main(...))`.
  - `run <name> [args]` and `<name> [args]` (when name is registered) → exec shim with `accountEnv`.
  - `use <name>` → prints `export CLAUDE_CONFIG_DIR='<dir>'; export CCM_ACCOUNT='<name>'` (main → `unset CLAUDE_CONFIG_DIR; export CCM_ACCOUNT='main'`).
  - `shell-init zsh` → prints a `ccm()` function that `eval`s `command ccm use …` for `use` and otherwise calls `command ccm "$@"`, plus `export PATH="$HOME/.claude-accounts/bin:$PATH"` so plain `claude` goes through the shim.
  - `list`, `sync`, `remove <name> [--purge]` (asks for confirmation via stdin `y`; `--purge` also deletes the dir and runs `security delete-generic-password -s <svc>`).
  - `doctor [--fix]` checks: real claude exists; shim exists and is executable; per account: links (`inspectLinks`), MCP drift (`mergeMcp(...).changed`), Keychain item present, T3 settings contain a matching instance, T3 service reachable (`t3` CLI present; report only). `--fix` applies `linkShared`, `fixReplaced`, `syncMcp`, re-renders the shim. Exit 0 only when all ok.
  - `scripts/install.ts`: write the shim (`renderShimScript(Bun.which("bun") abs, src/shim-entry)`), `chmod 755`, symlink `bin/ccm` → `~/.local/bin/ccm`, then print the `eval "$(ccm shell-init zsh)"` line for the user to add.
  - Unknown command → usage text, exit 64.
- [ ] **Step 1: Failing tests:** dispatch table (each command routes; unknown → 64); `use main` output exact; `shell-init zsh` output contains `eval "$(command ccm use` and the PATH line; `doctor` with one replaced link → exit 1 and the line names the item; `doctor --fix` → exit 0 and the backup is reported; `remove work` without `y` → nothing changed.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS. `bun test --coverage` ≥ 80% lines.
- [ ] **Step 5:** Commit `feat: ccm CLI commands, doctor, and installer`.

### Task 12: README + live E2E checklist

**Files:** Create `README.md`, `docs/e2e-checklist.md`

- [ ] **Step 1:** README covers: install (`bun scripts/install.ts`, add the shell-init line), `ccm add`/`login`, parallel use, `status`/`best`, T3 (`ccm t3-sync`, restart T3, `t3 up` for phone), MCP auto-share and its secret-duplication trade-off, remote MCP servers that use OAuth (their tokens live per account in the Keychain → authorize once per account), the symlink experiment result, the undocumented usage endpoint caveat, and uninstall.
- [ ] **Step 2: Live run (with the user):** install → `ccm doctor` → `ccm add <second>` (user logs in) → two terminals `ccm main` + `ccm <second>` at the same time, each `/status` showing a different email → `ccm status` shows both + the ALL row → `ccm t3-sync`, restart T3, both instances visible, one thread on each → `t3 up` and open from the phone → add a dummy MCP to `~/.claude.json` via `claude mcp add -s user`, start a new `<second>` session, `/mcp` lists it → remove it, and it's gone next launch. Record results in `docs/e2e-checklist.md`.
- [ ] **Step 3:** Commit `docs: README and e2e checklist`.
