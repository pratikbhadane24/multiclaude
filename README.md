# ccm (multiclaude)

Run several Claude Code subscription accounts (OAuth, no API keys) in parallel on one Mac. One shared setup, per-account logins, per-account and combined plan limits, and every account available as a T3 Code instance.

macOS only (uses the Keychain). Go, stdlib + golang.org/x/text.

## Quick start

```sh
# 1. Build and install
make build
./bin/ccm install            # symlinks ccm into ~/.local/bin, installs the ccm-shim binary as ~/.claude-accounts/bin/claude
                             # after every `make build`, re-run `ccm install` to update the launcher (it is a copy)

# 2. Add the shell-init line to ~/.zshrc (ccm never edits it for you)
eval "$(ccm shell-init zsh)"

# 3. Check the install
ccm doctor

# 4. Add a second account (creates its dir, links your setup, copies a login link to the clipboard)
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
| `ccm add <name> [--email e] [--open mode]` | Create the account dir, link shared setup, sync MCP servers, register it, then log in. Prompts for the email if `--email` is omitted. Re-runnable; skips login if already logged in as that email. |
| `ccm login <name> [--open mode]` / `ccm login --all` | (Re)login and verify the signed-in email matches. `--all` logs in every logged-out account (`--all` cannot be combined with `--open`). |
| `ccm list` | Table with columns `NAME  DIR  EMAIL  PLAN  LOGGED IN` (plan comes from the account's `claude auth status`). |
| `ccm run <name> [claude args]` / `ccm <name> [claude args]` | Run `claude` as that account (`main` uses the default config). Exit code is the child's. |
| `ccm best [claude args]` | Run on the account with the most headroom (lowest max of 5h and 7d use); prints the pick to stderr. Exit 2 if every account is limited. |
| `ccm use <name>` | Print shell code that switches the current shell to the account (needs the `shell-init` wrapper). Sets `CCM_ACCOUNT`. |
| `ccm shell-init zsh` | Print the zsh wrapper and PATH line for `~/.zshrc`. Only `zsh` is supported. |
| `ccm status [--json] [--watch [d]]` | Per-account table: `ACCOUNT EMAIL PLAN 5H USED/LEFT/RESET 7D USED/LEFT/RESET TODAY TOK STATE`, plus a combined `ALL` row. Adds `7D OPUS` and `7D SONNET` (used %) columns when the API reports those windows. `STATE` shows `limited (5h)` or `limited (7d)` when a window is at 100%. `--json` adds `seven_day_opus`, `seven_day_sonnet` and `limited` per account. `--watch` refreshes (default `30s`, or a duration like `10s`). `--json` and `--watch` cannot be combined. |
| `ccm sync` | Re-link new shared items from `~/.claude` and copy MCP servers into every account. |
| `ccm t3-sync` | Add or update one `claude-<account>` instance per account in `~/.t3/userdata/settings.json`. Also sets each instance's `launchArgs` from the config (default args + account args). A `launchArgs` you edited in T3 is kept, with a warning. Idempotent. Prints the backup path when it changes the file. Restart T3 afterwards. |
| `ccm config show` | Show the default args and per-account args (stored in `~/.claude-accounts/config.json`, mode 0600). |
| `ccm config set default-args <args...>` | Args added to every interactive launch (`ccm <name>`, `run`, `best`) and to T3 `launchArgs`, e.g. `ccm config set default-args --dangerously-skip-permissions`. Everything after the key is taken verbatim, including `--flags`. Order: default args, account args, then the args you type. |
| `ccm config set args <account> <args...>` | Extra args for one account (the account must exist). |
| `ccm config clear default-args` / `ccm config clear args <account>` | Remove them. Run `ccm t3-sync` afterwards to update T3. |
| `ccm doctor [--fix]` | Check binaries, shim, links, MCP drift, Keychain entries, T3 config. `--fix` re-links and re-syncs; a replaced file whose content differs is renamed to `<file>.ccm-backup-<unix>` first (identical copies are dropped). Exit 0 only if nothing fails or warns. |
| `ccm remove <name> [--purge]` | Unregister after a `[y/N]` prompt. Files are kept. `--purge` also deletes the account dir and its Keychain item, but only for a direct, non-symlink child of `~/.claude-accounts` that does not contain `~/.claude` or `$HOME`. `main` cannot be removed. T3 instances are not removed. |
| `ccm install` | Copy the native `ccm-shim` to `~/.claude-accounts/bin/claude` and symlink `ccm` into `~/.local/bin`. |
| `ccm help` | Usage text. |

### Login link (`--open`)

`add` and `login` take `--open copy|incognito|chrome:<Profile dir>`. The mode is stored per account and reused on later logins.

| Mode | Behavior |
|---|---|
| `copy` (default) | The login link is copied to the clipboard and printed. No browser is opened. |
| `incognito` | Open the link in a new Google Chrome incognito window. |
| `chrome:<dir>` | Open the link in that Chrome profile directory, e.g. `chrome:"Profile 2"`. |

Why not auto-open: the default browser is usually signed in to a different Claude account, so an automatic open can authorize the wrong one and attach it to the new account dir. With `copy` you paste the link into the profile or private window that is signed in to the right account. After login, ccm checks the email and exits 2 on a mismatch. If Chrome cannot be opened, ccm falls back to `copy`. Only Claude hosts are ever opened or copied.

Account names are `[a-z0-9-]{1,32}` and cannot be a reserved name: `main add login list run best use status sync t3-sync doctor remove shell-init help install bin browser config`.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | OK |
| 1 | Error (also `doctor` problems, declined `remove` prompt) |
| 2 | Login email mismatch, or `best` with no usable account (all limited) |
| 64 | Usage error (unknown command, bad flags or args) |
| 130 | Cancelled (Ctrl-C at a prompt or watch) |
| 128+N | `run`/`<name>`/`best`: the child `claude` was killed by signal N |

## How it works

```
~/.claude/                 account "main" (untouched)
~/.claude-accounts/
  accounts.json            registry
  state.json               which MCP servers came from main
  bin/claude               native Go shim `ccm-shim` (syncs MCP, then execs the real claude)
  <name>/                  CLAUDE_CONFIG_DIR for that account
    .claude.json  projects/  history.jsonl  sessions/   per account
    CLAUDE.md settings.json rules/ skills/ plugins/ agents/ commands/ hooks/ ...
                           symlinks to ~/.claude/<same>
```

- **Isolation:** each account runs with its own `CLAUDE_CONFIG_DIR`. Claude Code stores that account's OAuth token in the Keychain under `Claude Code-credentials-<first 8 hex of sha256(config dir)>`. `main` (no variable set) uses plain `Claude Code-credentials`. Logins never overwrite each other and running sessions never change account.
- **Shared (via symlinks, edit once in `~/.claude`):** CLAUDE.md, settings.json, rules, skills, plugins, agents, commands, hooks, output-styles, keybindings.
- **Shared (via the shim):** user-scope MCP servers. They live in `.claude.json`, which is per account, so symlinks cannot carry them. Every launch goes through `~/.claude-accounts/bin/claude`, which merges main's `mcpServers` into the account's file (main wins on name clashes, account-only servers are kept), then execs the real `claude`. `ccm`, the zsh wrapper and T3 all use the shim. A sync failure (or an internal error in it) never blocks launch. If an account's `.claude.json` is a symlink, the MCP sync replaces it with a regular file.
- **Per account:** login, `.claude.json` (onboarding, project trust), transcripts and auto-memory under `projects/`, history, sessions.
- **Usage:** `ccm status` reads each account's token from the Keychain and queries the usage endpoint. Expired tokens are refreshed only by running the official `claude auth status` for that account.
- **T3 Code:** `ccm t3-sync` adds `claude-<account>` instances (driver `claudeAgent`, `binaryPath` = the shim, `homePath` = the account dir) and leaves other entries alone.

## Safety

- Never writes `~/.claude` or `~/.claude.json`. Main is only read.
- Existing files are backed up before change: T3 settings become `settings.json.ccm-backup-<unix>`; replaced links become `<file>.ccm-backup-<unix>`. Backups are never deleted.
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

Then rerun T3 settings cleanup if you want: delete the `claude-<account>` entries from `providerInstances` in `~/.t3/userdata/settings.json` (`settings.json.ccm-backup-<unix>` from `t3-sync` is next to it). `~/.claude` is untouched throughout.
