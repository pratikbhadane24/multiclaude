# ccm (multiclaude)

Run several Claude Code subscription accounts (OAuth, no API keys) in parallel on one Mac. One shared setup, shared chat history, per-account logins and plan limits, and every account available as a T3 Code instance.

macOS only (uses the Keychain). Go, stdlib plus a few UI libraries for the dashboard.

## Daily use

```text
ccm            open the dashboard:  ↑↓ pick an account, ⏎ start Claude on it, q quit
ccm status     see how much of each account's limit is used
ccm best       start Claude on the account with the most room left
ccm continue   carry on this folder's last chat on a free account
```

That is all you need day to day. Everything below is setup and reference.

## Install / update

```sh
make build && ./bin/ccm install    # links ccm into ~/.local/bin and installs the launcher
```

Add this line to `~/.zshrc` once (ccm never edits it for you), then open a new terminal:

```sh
eval "$(ccm shell-init zsh)"
```

Run `ccm doctor` to check the install. After every `make build`, re-run `ccm install` (the launcher is a copy, so it needs refreshing).

`main` is your existing `~/.claude` login. It is never moved or deleted.

## Add an account

```sh
ccm add work
```

ccm creates the account, links your shared setup, then **copies a login link to the clipboard**. It never opens your default browser, because that browser is usually signed in to a different Claude account. Paste the link into the browser profile or private window that is signed in to the right one, finish the login, and ccm checks the email matches.

Pick where the link goes with `--open` (remembered per account):

| Mode | What happens |
|---|---|
| `copy` (default) | Link copied to the clipboard and printed. No browser opens. |
| `incognito` | Opens in a new Google Chrome incognito window. |
| `chrome:<Profile>` | Opens in that Chrome profile, e.g. `--open chrome:"Profile 2"`. |

If Chrome cannot be opened, ccm falls back to `copy`. Only Claude links are ever copied or opened. Log in again any time with `ccm login <name>`, or every logged-out account with `ccm login --all`.

## Dashboard

Run `ccm` (or `ccm dash`) in a terminal. It shows each account with 5h and 7d usage bars, a combined row, today's tokens (`in/out` and `cache` separately) and a details pane. It refreshes every 30 seconds.

| Key | Action |
|---|---|
| `↑` `↓` / `j` `k` | Pick an account |
| `⏎` | Start Claude on it. When Claude exits you land back on the dashboard. |
| `b` | Start on the best (most room) account |
| `n` | Open the account in a new Terminal.app window |
| `l` | Log in the picked account |
| `a` | Add an account (asks for a name and email) |
| `d` | Run doctor and show the summary |
| `s` | Sync shared setup |
| `t` | Sync accounts to T3 Code |
| `A` | Show or hide accounts that are signed in as the same login as another |
| `r` | Refresh now |
| `?` | Help |
| `q` / `Ctrl-C` | Quit |

**States** shown per account:

| State | Meaning |
|---|---|
| `ok` | Normal. |
| `idle` | Nothing used in that window (shows 0% used, 100% left). Common on Max accounts. |
| `low (5h)` / `low (7d)` | Yellow: 95% or more used, not yet full. |
| `limited (5h)` / `limited (7d)` | 100% used. Wait for the reset or switch account. |
| `logged out` | Needs `ccm login <name>`. |

**Duplicates:** if two entries are logged in as the same person, the extra one is collapsed into the first, shown as `main (= admin)`, so the limits are not counted twice. Press `A` (or run `ccm status --all`) to list the extra one on its own row marked `same as <name>`.

**Limit prompts:**
- Pressing `⏎` on a limited account asks `<name> is limited (resets in X). Start on <best>? [y/N]`.
- If the account you just used hits its limit while you were in Claude, the dashboard asks `Continue this conversation on <best>? [y/N]` and resumes the same chat there.

A `⚠ skip-permissions` badge appears when your default args include `--dangerously-skip-permissions`.

## Status line

Show the active account and its limits inside every Claude session, e.g. `ccm: work 5h 42% · 7d 18%` (`5h idle` when nothing is used).

```sh
ccm statusline install      # shows the exact change, asks [y/N], backs up settings.json first
ccm statusline uninstall    # restores your previous status line
```

If you already have a status line, install **wraps** it: your existing output stays, with the ccm segment added after it. Running install twice changes nothing. It reads a short-lived cache, so it never slows the prompt; if the cache is missing you just see `ccm: <account>`. A trailing `~` means the numbers are from the last good read because the latest one failed.

**Usage reads are rate limited.** The usage API is undocumented and answers `429` when polled too often, so every ccm command (status, dash, the status line, best, continue) reads it through one shared cache: an account is read at most once every 3 minutes, a `429` backs off 5m → 10m → 20m → 30m (or the server's `Retry-After` if longer), other errors wait 2 minutes, and a failed read never replaces the last good numbers. Such rows show their numbers with a state like `ok · stale 4m`; with no earlier numbers they say `rate limited (retry in 5m)`. `r` in the dashboard and `ccm status --refresh` skip the 3-minute reuse but never a backoff.

This is the one place ccm writes `~/.claude/settings.json`, and only when you say yes.

## Default args

Add flags to every interactive launch (`ccm <name>`, `run`, `best`, the dashboard):

```sh
ccm config set default-args --dangerously-skip-permissions
ccm config set args work --model opus     # extra args for one account only
ccm config show
ccm config clear default-args             # or: ccm config clear args work
```

Order: default args, then the account's args, then anything you type. They are never added to internal login/status checks or to plain `claude`.

They are also copied into T3 Code. Run `ccm t3-sync` after changing them. If you edited an instance's launch args inside T3, ccm keeps your edit and warns instead of overwriting it.

**Security note, in plain words:** `--dangerously-skip-permissions` tells Claude to run commands and edit files without asking you first. Anything Claude decides to do, it does. Only use it in folders and projects you trust, and remember it applies to every account and every T3 instance once it is a default. Remove it with `ccm config clear default-args`.

## Shared history

Every account sees the same chats. Run `/resume` in any account and all conversations (and project memory, file history, todos, plans) are listed, so when one account is limited you can pick up on another.

`ccm continue` does this for the folder you are in: it resumes the latest chat on the account with the most room (`--from`, `--to`, `--session` to choose).

If an account already has its own history, `ccm sync` (also run by `add` and `doctor --fix`) merges it into the shared copy safely:
- Nothing is overwritten or deleted.
- An account with a **running Claude session is skipped** (close it and run `ccm sync` again).
- Where two versions of a file differ, both are kept; the extra one is saved as `*.from-<account>-<unix>.jsonl`.

## T3 Code and phone

```sh
ccm t3-sync     # one claude-<account> instance per account, with your default args; then restart T3
t3 up           # T3's own remote service, for using it from your phone
```

## Doctor

```sh
ccm doctor          # check binaries, shim, links, shared history, MCP, Keychain, T3
ccm doctor --fix    # re-link and re-sync; replaced files are backed up first
```

## Remove an account

```sh
ccm remove <name>            # unregister (files kept), asks [y/N]
ccm remove <name> --purge    # also delete its folder and Keychain login
```

`main` cannot be removed. T3 instances are not removed.

## Command reference

| Command | What it does |
|---|---|
| `ccm` / `ccm dash` | Dashboard (bare `ccm` needs a terminal). |
| `ccm add <name> [--email e] [--open mode]` | Create the account, link shared setup, sync MCP servers, register, log in. Prompts for the email if omitted. Re-runnable. |
| `ccm login <name> [--open mode]` / `ccm login --all` | (Re)login and verify the email. `--all` cannot be combined with `--open`. |
| `ccm list` | Table: `NAME  DIR  EMAIL  PLAN  LOGGED IN`. |
| `ccm run <name> [claude args]` / `ccm <name> [claude args]` | Run Claude as that account. Exit code is the child's. |
| `ccm best [claude args]` | Run on the account with the most headroom; prints the pick to stderr. Exit 2 if all are limited. |
| `ccm use <name>` | Print shell code that switches this shell to the account (needs the `shell-init` wrapper). Sets `CCM_ACCOUNT`. |
| `ccm continue [--from a] [--to b] [--session id]` | Resume this folder's latest chat on another account. `--from` defaults to `$CCM_ACCOUNT`, else the most limited; `--to` to the account with most headroom. Never picks a chat that is still open in another window (exit 1 if that is the only one; pass `--session`). The `--to` account must be a different login than `--from`. Exit 2 if none free, 1 if no chat. |
| `ccm status [--json] [--all] [--refresh] [--watch [d]]` | Per-account 5h/7d used, left, reset; `TODAY TOK`; `STATE`; plus an `ALL` row. `--watch` default `30s`. `--all` also lists accounts that share a login as separate rows (`same as <name>`). `--json` and `--watch` cannot be combined. `--refresh` reads usage now instead of reusing a result under 3 minutes old. `--refresh-cache` updates the status-line cache. |
| `ccm statusline [install\|uninstall]` | Install or remove the status line segment. Without a subcommand it is the status line itself (reads Claude's JSON on stdin, optional `--wrap "<cmd>"`). |
| `ccm config show` | Show default and per-account args (`~/.claude-accounts/config.json`, mode 0600). |
| `ccm config set default-args <args...>` | Args for every launch. Everything after the key is taken verbatim, including `--flags`. |
| `ccm config set args <account> <args...>` | Extra args for one account. |
| `ccm config clear default-args` / `ccm config clear args <account>` | Remove them. Run `ccm t3-sync` after. |
| `ccm sync` | Link shared items, merge account history, copy MCP servers into every account. |
| `ccm t3-sync` | Add or update one `claude-<account>` instance per account and sync `launchArgs`. Backs up T3 settings; restart T3 after. |
| `ccm doctor [--fix]` | Check (and repair) the setup. Exit 0 only if nothing fails or warns. |
| `ccm remove <name> [--purge]` | Unregister (and optionally delete). |
| `ccm shell-init zsh` | Print the zsh wrapper and PATH line for `~/.zshrc`. |
| `ccm install` | Install the launcher and the `ccm` link. |
| `ccm help` | Usage text. |

Account names are `[a-z0-9-]{1,32}` and cannot be a reserved name: `main add login list run best use status sync t3-sync doctor remove shell-init help install bin browser config`, plus the other command names (`continue`, `statusline`, `dash`).

## Exit codes

| Code | Meaning |
|---|---|
| 0 | OK |
| 1 | Error (also `doctor` problems, declined `remove` prompt, `continue` with no chat) |
| 2 | Login email mismatch, or `best`/`continue` with no usable account (all limited) |
| 64 | Usage error (unknown command, bad flags or args) |
| 130 | Cancelled (Ctrl-C at a prompt or watch) |
| 128+N | Launch commands: the child `claude` was killed by signal N |

## How it works

```
~/.claude/                 account "main" (its history is the shared copy)
~/.claude-accounts/
  accounts.json            registry
  config.json              default and per-account args
  state.json               MCP origin, last T3 launchArgs
  cache/usage.json         usage cache + per-account fetch state (shared by every ccm process)
  cache/usage.lock         lock guarding writes to usage.json
  bin/claude               native Go shim `ccm-shim` (syncs MCP, then execs the real claude)
  <name>/                  CLAUDE_CONFIG_DIR for that account
    .claude.json  history.jsonl  sessions/   per account
    CLAUDE.md settings.json rules/ skills/ plugins/ agents/ commands/ hooks/ ...
    projects/ file-history/ todos/ plans/   (shared chat history)
                           symlinks to ~/.claude/<same>
```

- **Isolation:** each account runs with its own `CLAUDE_CONFIG_DIR`. Claude Code stores that account's OAuth token in the Keychain under `Claude Code-credentials-<first 8 hex of sha256(config dir)>`; `main` uses plain `Claude Code-credentials`. Logins never overwrite each other.
- **Shared (symlinks, edit once in `~/.claude`):** CLAUDE.md, settings.json, rules, skills, plugins, agents, commands, hooks, output-styles, keybindings, and chat history (`projects`, `file-history`, `todos`, `plans`).
- **Shared (via the shim):** user-scope MCP servers. They live in the per-account `.claude.json`, so every launch goes through `~/.claude-accounts/bin/claude`, which merges main's `mcpServers` in (main wins on name clashes) and then execs the real `claude`. A sync failure never blocks launch.
- **Per account:** login, `.claude.json` (onboarding, project trust), prompt history, sessions.
- **Usage:** `ccm status` reads each account's token from the Keychain and queries the usage endpoint. Expired tokens are refreshed only by running the official `claude auth status`.
- **T3 Code:** `ccm t3-sync` adds `claude-<account>` instances (driver `claudeAgent`, `binaryPath` = the shim, `homePath` = the account dir, `launchArgs` from your config) and leaves other entries alone.

## Safety

- Writes to `~/.claude` are limited to two user-approved cases: the history migration (moves an account's own `projects`, `file-history`, `todos`, `plans` entries into the shared copy, never overwriting or deleting differing data) and `ccm statusline install` (shows the change, asks `[y/N]`, backs up first). `~/.claude.json` is never written.
- Existing files are backed up before change: T3 settings and `~/.claude/settings.json` become `*.ccm-backup-<unix>`; replaced links become `<file>.ccm-backup-<unix>`. Backups are never deleted.
- Tokens stay in the macOS Keychain (read with `/usr/bin/security`) and in memory. They are never logged, printed, written to files, or included in `--json`. They are sent only to `api.anthropic.com`. `ccm` never calls the OAuth refresh endpoint itself.
- `ccm remove` never deletes anything without `--purge` plus confirmation.
- The history migration refuses an account dir that is, contains or aliases `~/.claude`, skips an account with a running Claude session (a live pid in its `sessions/`, or an unparsable session file touched in the last 10 minutes), needs both dirs on the same volume, and keeps symlinks inside history as renamed links (`*.from-<account>-<unix>`) instead of moving them.
- Default args are never applied to internal `claude auth` calls.

## Caveats

- **Undocumented usage endpoint.** `status` relies on `GET /api/oauth/usage`, which Anthropic does not document. It may change; ccm shows `usage API error` for that account and keeps going.
- **MCP secrets are duplicated.** Secrets in MCP `env`/`headers` end up in each account's `.claude.json`, written with mode `0600`.
- **Remote OAuth MCP servers authorize once per account.**
- **Symlink saves.** `claude plugin ...` commands kept the links intact (`docs/experiments/symlink-save.md`, Claude Code 2.1.328). The interactive `/config` save path is checked in the live run (`docs/e2e-checklist.md`). `ccm doctor` detects a replaced link either way.
- Chat history is shared, so `status` shows TODAY tokens once (in the `ALL` row), not per account.
- Moving a running session between accounts is not supported; `ccm continue` resumes the chat in a new session.

## Uninstall

```sh
ccm statusline uninstall     # if you installed the status line
ccm remove <name> --purge    # for each added account (optional; deletes its dir and Keychain item)
rm ~/.local/bin/ccm
rm -rf ~/.claude-accounts    # registry, config, shim, and any remaining account dirs
# remove the eval "$(ccm shell-init zsh)" line from ~/.zshrc
```

Then, if you want, delete the `claude-<account>` entries from `providerInstances` in `~/.t3/userdata/settings.json` (the `settings.json.ccm-backup-<unix>` from `t3-sync` is next to it). Shared chat history stays in `~/.claude` (accounts merged into it keep their chats there).
