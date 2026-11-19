# Live E2E checklist

Run with the user. Fill in Result/Notes during the run.

- [ ] Install: `make build && ./bin/ccm install`, add shell-init line, new shell
  - Result:
- [ ] `ccm doctor` passes (or only expected warnings)
  - Result:
- [ ] `ccm add <second>`; user logs in; email matches
  - Result:
- [ ] Login safety: `ccm login <name>` with default `copy` mode; browser does not open, link is on the clipboard; paste into the right profile or private window and sign in; `ccm list` shows the correct email
  - Result:
- [ ] Parallel: `ccm main` and `ccm <second>` in two terminals at once; `/status` shows different emails
  - Result:
- [ ] `ccm status` shows both accounts plus the `ALL` row
  - Result:
- [ ] `ccm t3-sync`, restart T3, one thread per instance works
  - Result:
- [ ] `t3 up`; open from the phone and drive a thread
  - Result:
- [ ] MCP add: `claude mcp add -s user` a dummy server in main; a new `<second>` session lists it under `/mcp`
  - Result:
- [ ] MCP remove: remove it from main; gone from `<second>` at the next launch
  - Result:
- [ ] Interactive `/config` change in an account: is `settings.json` still a symlink? (`test -L <dir>/settings.json`)
  - Result:

## Phase 2 live checks

- [ ] Dashboard: `ccm`, pick an account, `⏎` launches Claude; exiting Claude returns to the dashboard with the terminal intact
  - Result:
- [ ] Dashboard `n` opens a new Terminal.app window running `ccm <name>`
  - Result:
- [ ] Dashboard `l` logs in the picked account (link copied, email verified)
  - Result:
- [ ] `ccm statusline install` shows the diff, asks `[y/N]`, wraps the existing (caveman) status line; inside Claude it shows `ccm: <acct> 5h … · 7d …`; `uninstall` restores the old one
  - Result:
- [ ] `ccm config set default-args --dangerously-skip-permissions`, then `ccm <name>` starts with the flag (and the dashboard shows the skip-permissions badge); `ccm t3-sync` puts it in the T3 instance `launchArgs`
  - Result:
- [ ] `ccm sync` merges an existing account's own history into `~/.claude` (no overwrite; conflicts kept as `*.from-<acct>-<unix>`; an account with a running session is skipped)
  - Result:
- [ ] `/resume` in a second account lists all chats from every account
  - Result:
- [ ] Limit prompt flow: on a limited account `⏎` asks `Start on <best>? [y/N]`; after Claude exits at a limit it asks `Continue this conversation on <best>? [y/N]` and resumes the chat
  - Result:
- [ ] An idle Max account (null 5h window) shows `idle` (0% used, 100% left) in status, the dashboard and the status line
  - Result:
- [ ] Logging a second account in as the same person collapses to one dashboard row (`same as <name>`)
  - Result:
- [ ] T3 `launchArgs` with a spaced arg (e.g. `--append-system-prompt "two words"`) round-trips: correct in T3 settings and the session starts with the full arg
  - Result:
