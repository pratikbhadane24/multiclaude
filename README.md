# ccm (multiclaude)

Run several Claude Code subscription accounts (OAuth, no API keys) in parallel on one Mac. One shared setup, per-account logins, per-account and combined plan limits, and every account available as a T3 Code instance.

macOS only (uses the Keychain). Go, stdlib only.

## Quick start

```sh
# 1. Build and install
make build
./bin/ccm install            # puts ccm in ~/.local/bin, shim in ~/.claude-accounts/bin/claude

# 2. Add the shell-init line to ~/.zshrc (ccm never edits it for you)
eval "$(ccm shell-init zsh)"

# 3. Check the install
ccm doctor

# 4. Add a second account (creates its config dir, links your setup, opens the browser to log in)
ccm add work

# 5. Use accounts in parallel, in different terminals
ccm main                     # your existing ~/.claude login
ccm work                     # the new account

# 6. See limits per account and combined
ccm status

# 7. T3 Code: one Claude instance per account, then restart T3
ccm t3-sync

# 8. Phone access (runs T3's own remote service)
t3 up
```

`main` is your existing `~/.claude` account. It is never moved or modified.

## Commands

| Command | What it does |
|---|---|
| `ccm add <name> [--email e]` | Create an account dir, link the shared setup, sync MCP servers, register it, then log in. Re-runnable. |
| `ccm login <name>` / `ccm login --all` | (Re)login. Opens the browser with the email pre-filled and verifies the signed-in email. `--all` handles every logged-out account. |
| `ccm list` | Accounts, email, plan, logged-in state. |
| `ccm run <name> [args]` / `ccm <name> [args]` | Launch `claude` on that account. `main` uses the default config. |
| `ccm best [args]` | Launch on the account with the most headroom (lowest max of 5h and 7d use). |
| `ccm use <name>` | Switch the current shell to an account (needs the shell-init wrapper). Sets `CCM_ACCOUNT` for prompts. |
| `ccm status [--json] [--watch N]` | Per-account 5h/7d used %, remaining %, reset time, today's tokens, plus a combined row. |
| `ccm t3-sync` | Write one T3 Claude instance per account into `~/.t3/userdata/settings.json`. Idempotent; backs up first. Restart T3 afterwards. |
| `ccm sync` | Re-link new shared items from `~/.claude` and copy MCP servers into every account. |
| `ccm shell-init zsh` | Print the zsh wrapper and PATH line. |
| `ccm doctor [--fix]` | Check binaries, shim, links, MCP drift, Keychain entries, T3 config. `--fix` repairs; replaced files are backed up, never overwritten. |
| `ccm remove <name> [--purge]` | Unregister (asks first). `--purge` also deletes the dir and its Keychain item, with confirmation. |
| `ccm install` | Install `ccm` and the launcher shim. |

Account names are `[a-z0-9-]+` and cannot match a subcommand.

## How it works

```
~/.claude/                 account "main" (untouched)
~/.claude-accounts/
  accounts.json            registry
  state.json               which MCP servers came from main
  bin/claude               launcher shim (syncs MCP, then execs the real claude)
  <name>/                  CLAUDE_CONFIG_DIR for that account
    .claude.json  projects/  history.jsonl  sessions/   per account
    CLAUDE.md settings.json rules/ skills/ plugins/ agents/ commands/ hooks/ ...
                           symlinks to ~/.claude/<same>
```

- **Isolation:** each account runs with its own `CLAUDE_CONFIG_DIR`. Claude Code stores that account's OAuth token in the Keychain under `Claude Code-credentials-<first 8 hex of sha256(config dir)>`. `main` (no variable set) uses plain `Claude Code-credentials`. Logins never overwrite each other and running sessions never change account.
- **Shared (via symlinks, edit once in `~/.claude`):** CLAUDE.md, settings.json, rules, skills, plugins, agents, commands, hooks, output-styles, keybindings.
- **Shared (via the shim):** user-scope MCP servers. They live in `.claude.json`, which is per account, so symlinks cannot carry them. Every launch goes through `~/.claude-accounts/bin/claude`, which merges main's `mcpServers` into the account's file (main wins on name clashes, account-only servers are kept), then execs the real `claude`. `ccm`, the zsh wrapper and T3 all use the shim. A sync failure never blocks launch.
- **Per account:** login, `.claude.json` (onboarding, project trust), transcripts and auto-memory under `projects/`, history, sessions.
- **Usage:** `ccm status` reads each account's token from the Keychain and queries the usage endpoint. Expired tokens are refreshed only by running the official `claude auth status` for that account.
- **T3 Code:** `ccm t3-sync` adds `claude-<account>` instances (driver `claudeAgent`, `binaryPath` = the shim, `homePath` = the account dir) and leaves other entries alone.

## Safety

- Never writes `~/.claude` or `~/.claude.json`. Main is only read.
- Existing files are backed up before change (T3 settings; replaced symlinks become `<file>.ccm-backup-<timestamp>` and backups are never deleted).
- Nothing is changed automatically when a link has been replaced by a file: `ccm doctor` shows it, `doctor --fix` backs up and re-links.
- Tokens stay in the macOS Keychain (read with `/usr/bin/security`) and in memory. They are never logged, printed, written to files, or included in `--json`. They are sent only to `api.anthropic.com`.
- `ccm` never calls the OAuth refresh endpoint itself.
- `ccm remove` never deletes anything without `--purge` plus confirmation.

## Caveats

- **Undocumented usage endpoint.** `status` relies on `GET /api/oauth/usage`, which Anthropic does not document. It may change; `ccm` shows `usage API error` for that account and keeps going.
- **MCP secrets are duplicated.** Secrets in MCP `env`/`headers` end up in each account's `.claude.json`, written with mode `0600` (same protection as the original).
- **Remote OAuth MCP servers authorize once per account.** Their OAuth tokens are not copied; authorize each in every account.
- **Symlink saves.** Experiment (`docs/experiments/symlink-save.md`, Claude Code 2.1.328): `claude plugin marketplace add/install/disable` wrote `settings.json` and `plugins/` through the symlinks and left them intact. The interactive `/config` save path is unverified until the live run (`docs/e2e-checklist.md`). `ccm doctor` detects a replaced link either way.
- Per-account memory and history are deliberately not shared.
- Moving a running session between accounts is not supported.

## Uninstall

```sh
ccm remove <name> --purge    # for each added account (optional; deletes its dir and Keychain item)
rm ~/.local/bin/ccm
rm -rf ~/.claude-accounts    # registry, shim, and any remaining account dirs
# remove the eval "$(ccm shell-init zsh)" line from ~/.zshrc
```

Then rerun T3 settings cleanup if you want: delete the `claude-<account>` entries from `providerInstances` in `~/.t3/userdata/settings.json` (a `.bak` from `t3-sync` is next to it). `~/.claude` is untouched throughout.
