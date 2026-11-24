# Troubleshooting

## Start with doctor

```sh
ccm doctor          # check binaries, shim, links, shared history, MCP, Keychain, T3
ccm doctor --fix    # re-link and re-sync; replaced files are backed up first
```

`ccm doctor` exits 0 only when nothing fails or warns. In the dashboard, press `d`.

## Usage rows say `stale` or `rate limited`

`ok · stale 4m` means the numbers shown are the last good read, and the latest read failed. multiclaude keeps the old numbers instead of blanking the row. See [how usage is read](dashboard.md#status-line).

**A row that stays stale for hours** is usually an account you have not used for a while. Its access token has expired, and multiclaude does not refresh tokens itself (see [Safety](how-it-works.md#safety)). Start Claude on that account once so Claude Code renews the token:

```sh
ccm <name>
```

The row recovers on the next read after its current retry wait ends (shown in the details pane as `retry in …`). An account that has been idle keeps the same usage, so stale numbers for it are normally still right.

**Every row is rate limited** right after heavy use of `ccm status --refresh` or `r` in the dashboard: the usage endpoint answered `429`. multiclaude backs off 5m → 10m → 20m → 30m per account. Waiting is the only fix; refreshing never skips a backoff.

## An account shows `logged out`

```sh
ccm login <name>      # or: ccm login --all
```

If the login finishes but multiclaude reports an email mismatch (exit code 2), the browser was signed in to a different Claude account. Paste the link into the right browser profile, or use `--open chrome:"<Profile>"` (see [Accounts](accounts.md)).

## macOS will not open multiclaude

Archives downloaded with a browser are quarantined and the binaries are not signed with an Apple Developer ID. Clear the flag once:

```sh
xattr -d com.apple.quarantine ~/.local/share/ccm/ccm ~/.local/share/ccm/ccm-shim
```

## `ccm-shim not found next to …`

`ccm install` copies `ccm-shim` from the folder that holds `ccm`. Keep both binaries together, then run `ccm install` again.

## `~/.local/bin/ccm exists; remove it or install manually`

Another install already owns that link. Remove it (`rm ~/.local/bin/ccm`) and run `ccm install` again.

## `ccm: command not found`

`~/.local/bin` is not on your `PATH`. Add `eval "$(ccm shell-init zsh)"` to `~/.zshrc` (it sets the path) and open a new terminal. Until then, run `~/.local/bin/ccm`.

## Changes after an update do not show up

The launcher is a copy, so run `ccm install` after every update. `ccm doctor` reports an outdated shim.

## MCP servers are missing in an account

User-scope MCP servers are copied from `main` on every launch that goes through the shim, and by `ccm sync`. Remote OAuth MCP servers must be authorized once per account.

## Auto-switch did not switch

- It only follows a known conversation. A bare `--resume` (the picker), a search term or `--fork-session` runs without auto-switch, with a warning.
- Only usage-limit errors trigger it. Server, network, billing and login errors never do.
- Quitting Claude yourself never switches.
- On a worker, check `ccm config show` there. Workers set up before auto-switch was copied had it `off`. Run `ccm worker sync --all` on the hub.

Details are in [Auto-switch](auto-switch.md).

## `No conversation found` after a switch

The chat lives only in the account that started it, so the other account cannot resume it. This happens when an account kept its own `projects` folder instead of the shared one. `ccm doctor` shows it as `projects is no longer shared`. Close Claude in that account, then run:

```sh
ccm sync          # on the Mac with the problem (on a worker: ssh into it first)
```

It merges the account's chats into the shared history and links it. Then resume again.

## Reporting a bug

Open an issue with the output of `ccm version` and `ccm doctor`, your macOS and Claude Code versions, and the steps that lead to the problem. Remove email addresses if you prefer. Never paste tokens or the contents of `.claude.json`.
