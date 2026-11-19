# multiclaude — multiple Claude Code OAuth accounts, in parallel

Date: 2026-11-19 · Status: draft for approval

## Intent

Run several Claude subscription accounts (OAuth login, no API keys) on one Mac:

- **in parallel** — different terminals / T3 Code threads on different accounts at the same time, with no cross-talk;
- **switchable** — choose which account a new session uses, or let the tool pick the one with most headroom;
- **observable** — see the 5-hour and weekly limits (used %, remaining %, reset time) **for each account and for all accounts combined**;
- **remote** — drive sessions on any account from the phone.

## Goals

1. One isolated Claude config dir per account, each with its own OAuth login, kept separate by Claude Code's own `CLAUDE_CONFIG_DIR` mechanism.
2. A small CLI, `ccm`, to add accounts, launch `claude` on an account, switch the current shell's account, and show usage.
3. Every account shares the user's single setup (rules, skills, plugins, settings, hooks), edited once in `~/.claude`.
4. T3 Code gets one Claude provider instance per account, generated from the same account list.

## Non-goals

- API-key accounts or Bedrock/Vertex.
- A GUI/menu-bar app (T3 Code and the terminal are the UI).
- Moving a running session from one account to another.
- Windows/Linux (macOS Keychain only for v1).

## Anti-goals (would be a failure even if it "works")

- Any account's login gets logged out or overwritten by another (the CCSwitcher model of swapping one global credential).
- Running sessions silently changing account.
- Tokens copied into plaintext files, logs, or anywhere besides the macOS Keychain where Claude Code already keeps them.
- The existing `~/.claude` setup being moved, rewritten or broken.

## Facts this design rests on (verified 2026-11-19, Claude Code 2.1.328)

- If `CLAUDE_CONFIG_DIR` is set, Claude Code uses that dir instead of `~/.claude` + `~/.claude.json`. It stores the OAuth token in the Keychain service
  `Claude Code-credentials-<first 8 hex of sha256(NFC(configDir))>`. If it's unset, the service is `Claude Code-credentials` (no suffix).
  (Read from the shipped binary, function building the service name.)
- `GET https://api.anthropic.com/api/oauth/usage` with `Authorization: Bearer <accessToken>` and `anthropic-beta: oauth-2025-04-20` returns
  `five_hour`, `seven_day`, `seven_day_opus`, `seven_day_sonnet`, … each `{utilization, resets_at}`, plus `extra_usage`.
  **Undocumented** (CCSwitcher and Claude Code's `/usage` use it). It may change; the tool must degrade gracefully.
- Access tokens expire (~8h). Running `claude auth status` with the same `CLAUDE_CONFIG_DIR` makes the official CLI refresh and re-store the token (CCSwitcher's approach). We never call the OAuth refresh endpoint ourselves.
- T3 Code (installed, beta) supports multiple `providerInstances` with driver `claudeAgent`, each with `binaryPath` and `homePath`; it launches with `CLAUDE_CONFIG_DIR=homePath` and per-instance token-usage tracking from transcripts.
  Config: `~/.t3/userdata/settings.json`. Currently T3 reports "`claude` was not found on PATH"; binary is `~/.local/bin/claude`.

## Approaches considered

| | Parallel | Switch | Limits per acct + combined | Cost |
|---|---|---|---|---|
| **A. Per-account config dirs + `ccm` CLI + T3 instances** (chosen) | yes | yes | yes, via `ccm status` | small CLI |
| B. Shell aliases only | yes | per tab | no | trivial |
| C. CCSwitcher (swaps one global credential) | **no**: switching moves running sessions too | yes | active account only | none |

Chosen: **A**. The user delegated the choice. A is the only option with parallel use plus per-account and combined limits. CCSwitcher is excluded because it would conflict: it rewrites the default login, which is also one of our accounts.

## Design

### Layout

```
~/.claude/                     ← account "main" (existing, untouched, default Keychain entry)
~/.claude-accounts/
  accounts.json                ← registry: [{name, dir, label?}], "main" → ~/.claude implicit
  <name>/                      ← CLAUDE_CONFIG_DIR for account <name>
    .claude.json               ← per account (oauthAccount, project state)
    projects/ history.jsonl sessions/ …   ← per account (transcripts, memory)
    CLAUDE.md  settings.json  rules/  skills/  plugins/  scripts/  agents/  commands/
    hooks/  output-styles/  keybindings.json   ← symlinks → ~/.claude/<same>  (only those that exist)
```

- **Shared** (symlinked): user setup. Edit once, applies everywhere. User MCP servers are shared too, via the launcher shim (see Decisions §1).
- **Per account**: login, `.claude.json`, transcripts/`projects/` (which includes auto-memory), history, sessions.
  Decision (Claude's call): memory/history **not** shared. Reasons: transcripts are how usage is attributed per account, and sharing `projects/` would mix them; concurrent writers from different accounts to the same files is risk with no clear payoff.
- Symlinks are re-checked by `ccm doctor`/`ccm add`, so new shared items in `~/.claude` can be linked later.

### `ccm` CLI

Go (user decision 2026-11-19, replacing Bun/TS): native `ccm` binary + native shim binary, stdlib only. Installed via `ccm install` (symlink into `~/.local/bin/ccm`).

| Command | Behavior |
|---|---|
| `ccm add <name> [--email e]` | validate name `[a-z0-9-]+`; ask for email if not given; create dir + symlinks + MCP sync; register; then login (below). Re-runnable: if the dir exists, it resumes instead of failing. |
| `ccm login <name>` | easy (re)login: prints a clear "sign in as <email> in the browser" line, runs `claude auth login --claudeai --email <email>` with that `CLAUDE_CONFIG_DIR` (browser opens with email pre-filled), then verifies with `claude auth status --json`. If the signed-in email differs from the registered one, or matches another account, it warns loudly and offers to log out of it. Tip printed: if the browser is signed in to another claude.ai account, open the printed URL in a private window. |
| login link safety (user decision 2026-11-19) | Never auto-open the default browser, which may be signed in to another account. During login, ccm sets `BROWSER=<ccm exe>` (Claude opens URLs via `$BROWSER <url>`). ccm then copies the URL to the clipboard and prints guidance (default), or opens a remembered per-account target: `--open incognito` (Chrome incognito) or `--open chrome:<Profile dir>`. Only claude.ai/claude.com/anthropic.com hosts are handled. |
| `ccm login --all` | runs login one after another for every account that's logged out or expired. |
| `ccm list` | accounts, email/plan (from `claude auth status`), logged-in state. |
| `ccm run <name> [claude args…]` / `ccm <name> …` | `exec claude` with `CLAUDE_CONFIG_DIR` for that account. `main` = unset var. |
| `ccm best [claude args…]` | fetch usage for all, launch on account with lowest max(5h, 7d) utilization that isn't blocked. |
| `ccm use <name>` | print `export CLAUDE_CONFIG_DIR=…` (or `unset`). A zsh function wrapper `eval`s it so the current shell switches. Prompt/statusline can show the active account. |
| `ccm status [--json] [--watch N]` | per-account table + combined row (below). Accounts fetched in parallel. |
| `ccm t3-sync` | write T3 `providerInstances`: one `claude` instance per account (`binaryPath` absolute, `homePath` = account dir; main → empty homePath). Preserves other keys/instances; backs up file first; tells user to restart T3. |
| `ccm sync` | re-link any new shared items from `~/.claude`; copy `mcpServers` one way into every account. |
| `ccm shell-init zsh` | print the zsh wrapper (for `ccm use` + `CCM_ACCOUNT`). |
| `ccm doctor [--fix]` | check binary, dirs, symlinks (incl. replaced-by-file), MCP drift, Keychain entries present, T3 config in sync, T3 service running. |
| `ccm remove <name>` | unregister; confirm; never deletes the dir or Keychain item unless `--purge` + confirmation. |

### `ccm status` output

Per account: name, email, plan, **5h used % / remaining % / resets in**, **7d used % / remaining % / resets in**, model-specific weekly windows when non-null, extra-usage if enabled, today's tokens (in/out/cache) from that account's transcripts, and state (`ok`, `limited`, `expired→refreshed`, `logged out`, `usage API error`).

Combined row:
- **Remaining capacity** per window = Σ(100 − util) over reachable accounts, shown as "N.N of M accounts" and as average %.
- **Accounts available now** = count whose 5h and 7d are both < 100%.
- **Next reset** = soonest `resets_at` among accounts at 100%.
- Today's tokens summed.

Unreachable accounts are listed with their error and left out of combined numbers, and the combined row says so.

### Token handling

- Read: `/usr/bin/security find-generic-password -s <service> -w` (system binary, so the "Always Allow" grant sticks), parse JSON → `claudeAiOauth.accessToken`, `expiresAt`.
- If expired or the usage call returns 401: run `claude auth status` for that account (official refresh), re-read, retry once.
- Tokens live only in memory. They are never logged or printed (`--json` excludes them). They are sent only to `api.anthropic.com`.

### T3 Code

`ccm t3-sync` fixes the current "claude not found" by setting absolute `binaryPath` = the launcher shim `~/.claude-accounts/bin/claude`. That way T3 sessions also get automatic MCP sync. Each account becomes a selectable Claude instance, so threads in parallel on different accounts and T3's per-instance token tracking come for free. Limits/remaining come from `ccm status` (T3 doesn't show plan limits).

Instance shape (from T3 source, `apps/mobile/src/features/settings/claude-accounts.ts` / server `ClaudeHome.ts`): `providerInstances.<id> = { driver: "claudeAgent", enabled: true, config: { binaryPath, homePath } }`. T3's mobile app has a "Claude accounts" screen that edits these same entries, so `t3-sync` must be idempotent and must not clobber instances or fields it didn't create. Instance ids are `claude-<account>`; existing entries matched by `homePath` are updated in place, not duplicated.

### Remote / mobile access

Primary: **T3 Code remote access** via `t3 up` (T3 Connect, or LAN/tailnet with pairing code), then T3's mobile app or browser. All accounts are available as instances, threads run on the Mac, and the phone only drives them. `ccm` doesn't implement networking; it documents the one-time `t3 up` step in its README and `ccm doctor` reports whether the T3 service is running.

Usage on the phone (v1): ask any T3 thread to run `ccm status`. Zero extra surface. A dedicated mobile usage view is deferred until v1 is in use.

Fallback: `ccm run <name> remote-control` launches official Claude Code Remote Control on that account. The session appears in the Claude app / claude.ai of **that** account only.

### Error handling

Each external step (Keychain, CLI, HTTP, file write) is reported with an actionable message, e.g. "account `work` not logged in → `ccm add work` / `ccm login work`". Non-zero exit on failure. A failure in one account never aborts the others in `status`.

### Testing

TDD with `go test`, target 80%+: pure units (service-name hash, usage parsing/aggregation, T3 settings merge, registry), integration with mocked `security`/`claude`/HTTP via injected runners, and one manual E2E checklist (add account, parallel sessions, status, T3 sync).

## Decisions from builder check (Claude's calls, user delegated)

1. **MCP servers shared automatically (user decision: "shared automatically").** User-scope MCP servers live in `~/.claude.json`, which is per account, so symlinks can't carry them. Mechanism: a **launcher shim**, `~/.claude-accounts/bin/claude`, which every launch path goes through. `ccm run`/`best`, the zsh wrapper and T3 (`binaryPath` = shim) all use it.
   - The shim runs a fast MCP sync for `$CLAUDE_CONFIG_DIR` and then `exec`s the real `~/.local/bin/claude` with untouched args/env. Claude reads MCP config only at session start, so syncing at every launch = always current, with no daemon.
   - The shim's shebang uses the absolute bun path, because T3's PATH may lack bun.
   - No-op when `CLAUDE_CONFIG_DIR` is unset or is `~/.claude` (main is the source).
   - Merge rule: account `mcpServers` = main's servers ∪ account-only servers. On a name clash, main wins. `~/.claude-accounts/state.json` remembers which names came from main, so removing a server in main removes it from accounts without touching account-only servers.
   - Write safety: compare first and write only when different, which is rare (right after MCP changes in main). Write = re-read file → change only the `mcpServers` key → temp file in the same dir (mode `0600`) → `rename`. If the file fails to parse, skip it with a warning on stderr and never block launch. A sync failure never prevents Claude from starting.
   - Never writes `~/.claude.json`. Other keys (onboarding, project trust) stay per account.
   - Trade-off (README): secrets inside MCP `env`/`headers` exist in more files, with the same `0600` protection as the original.
   - Rejected alternatives: passing `--mcp-config` (could collide with the Agent SDK's own `--mcp-config` under T3); moving MCPs into a shared plugin (renames tools to `mcp__plugin_…`, which breaks existing permission rules).
2. **Token refresh while sessions run.** Refresh only through the official CLI (`claude auth status`). That is the same code path every extra `claude` session on an account already uses: they all share one Keychain entry and re-read it, so a refresh by `ccm` behaves like opening one more session. Limits: at most one refresh attempt per account per `ccm` invocation, and `--watch` retries a failed account only after its token's `expiresAt` changes or 10 min pass. No direct calls to the OAuth endpoint, ever.
3. **Symlink replaced by a real file (user: "handle it carefully").** Detection runs in `ccm doctor`, `ccm status` (one-line warning) and the shim (warning on stderr, never blocks).
   - Nothing is changed automatically.
   - `ccm doctor` shows the diff vs `~/.claude`.
   - `ccm doctor --fix`: if the content is identical, re-link silently. If not, back up to `<file>.ccm-backup-<timestamp>`, re-link, and print the backup path so changes can be copied over by hand.
   - Never merges into `~/.claude`. Never deletes a backup.
   - The first implementation task is an experiment: does Claude Code's save of `settings.json` / plugin installs replace symlinks? If it does for `settings.json`, the fallback is to link the parent dirs only where possible and document it. Result recorded in README.
4. **Shell integration / statusline.** v1 does not edit `~/.zshrc` or `~/.claude/settings.json`. `ccm shell-init zsh` prints the wrapper function (makes `ccm use` affect the current shell, and exports `CCM_ACCOUNT` for prompts). The user adds `eval "$(ccm shell-init zsh)"` themselves. A Claude statusline showing the account is deferred.
5. **Minor (builder-level):** account names can't collide with subcommands (reserved list rejected at `add`). "Today" = local midnight. Account dirs are stored and passed as absolute, NFC-normalized, no trailing slash, so `ccm`, T3 and Claude Code all compute the same Keychain service name.

## Left to the builder

Module split, table rendering, `--watch` refresh mechanics, exact zsh wrapper text, name of the `login` re-auth subcommand.
