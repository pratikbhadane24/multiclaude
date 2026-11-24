# Dashboard, status and status line

## Dashboard

Run `ccm` (or `ccm dash`) in a terminal. It shows each account with 5h and 7d usage bars, combined capacity left across accounts, today's tokens (`in/out` and `cache` separately) and a details pane. It refreshes every 30 seconds. Bars ease to their new values and a spinner runs while usage is being read; a resting dashboard draws nothing. Set `CCM_DASH_MOTION=0` to turn the animation off.

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
| `idle` | Nothing used in the 5h window yet (shows 0% used, 100% left). |

A Max account that reports no 5h window shows `—` and `no limit` in 5H RESET instead of `idle`.

**7D RESET** shows when the weekly window resets; it is gold when quota is still left and it resets within 24h (use it or lose it).

**★ best** marks the account `b` would launch. The Details panel says why, e.g. `★ best main · 36% weekly left, resets in 16h38m`. See [how the best account is chosen](auto-switch.md#how-the-account-is-chosen).
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

If you already have a status line, install **wraps** it: your existing output stays, with the multiclaude segment added after it. Running install twice changes nothing. It reads a short-lived cache, so it never slows the prompt; if the cache is missing you just see `ccm: <account>`. A trailing `~` means the numbers are from the last good read because the latest one failed.

**Usage reads are rate limited.** The usage API is undocumented and answers `429` when polled too often, so every multiclaude command (status, dash, the status line, best, continue) reads it through one shared cache: an account is read at most once every 3 minutes, a `429` backs off 5m → 10m → 20m → 30m (or the server's `Retry-After` if longer), other errors wait 2 minutes, and a failed read never replaces the last good numbers. Such rows show their numbers with a state like `ok · stale 4m`; with no earlier numbers they say `rate limited (retry in 5m)`. `r` in the dashboard and `ccm status --refresh` skip the 3-minute reuse but never a backoff.

This is the one place multiclaude writes `~/.claude/settings.json`, and only when you say yes.
