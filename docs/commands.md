# Command reference

| Command | What it does |
|---|---|
| `ccm` / `ccm dash` | Dashboard (bare `ccm` needs a terminal). |
| `ccm add <name> [--email e] [--open mode]` | Create the account, link shared setup, sync MCP servers, register, log in. Prompts for the email if omitted. Re-runnable. |
| `ccm login <name> [--open mode]` / `ccm login --all` | (Re)login and verify the email. `--all` cannot be combined with `--open`. |
| `ccm list [--json]` | Table: `NAME  DIR  EMAIL  PLAN  LOGGED IN`. `--json` prints name, email and login state. |
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
| `ccm config set auto-switch on\|off` | Resume on another account automatically when one hits its limit (see Auto-switch). |
| `ccm config set auto-switch-prompt <text>` / `ccm config clear auto-switch-prompt` | Replace (or restore) the prompt sent after a switch. |
| `ccm config set default-account best\|off` | `best`: plain `claude` starts on the account with the most headroom (from cached usage). Set on workers by `ccm worker add`. |
| `ccm worker add <host> --accounts a,b [--name n] [--yes]` | Set up another Mac as a worker over SSH (see [Workers](workers.md)). Re-runnable. |
| `ccm worker sync <name>` / `ccm worker sync --all` | Push ccm and the shared setup to workers again. |
| `ccm worker status [--json]` | Per worker: reachable, load, free memory, running Claude sessions, ccm version, account logins. |
| `ccm worker list` | Workers and their accounts. |
| `ccm sync` | Link shared items, merge account history, copy MCP servers into every account. |
| `ccm t3-sync` | Add or update one `claude-<account>` instance per account and sync `launchArgs`. Backs up T3 settings; restart T3 after. |
| `ccm doctor [--fix] [--worker]` | Check (and repair) the setup. Exit 0 only if nothing fails or warns. `--worker` skips main's login (used by worker setup). |
| `ccm remove <name> [--purge]` | Unregister (and optionally delete). |
| `ccm shell-init zsh [--worker]` | Print the zsh wrapper and PATH line for `~/.zshrc`. `--worker` also defines `claude()` to run the launcher. |
| `ccm install` | Install the launcher and the `ccm` link. |
| `ccm version` (`--version`, `-v`) | Print the multiclaude version. |
| `ccm help` | Usage text. |

Account names are `[a-z0-9-]{1,32}` and cannot be a reserved name: `main add login list run best use status sync t3-sync doctor remove shell-init help install bin browser config`, plus the other command names (`continue`, `statusline`, `dash`, `version`, `worker`).

## Exit codes

| Code | Meaning |
|---|---|
| 0 | OK |
| 1 | Error (also `doctor` problems, declined `remove` prompt, `continue` with no chat) |
| 2 | Login email mismatch, or `best`/`continue` with no usable account (all limited) |
| 64 | Usage error (unknown command, bad flags or args) |
| 130 | Cancelled (Ctrl-C at a prompt or watch) |
| 128+N | Launch commands: the child `claude` was killed by signal N |
