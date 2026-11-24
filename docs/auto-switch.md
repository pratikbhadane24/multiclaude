# Auto-switch

Let multiclaude move a running conversation to another account by itself when the current one hits its usage limit, with no keypress (useful when you are away from the computer):

```sh
ccm config set auto-switch on      # off to disable; shown in `ccm config show`
ccm config set auto-switch-prompt "keep going"   # optional: replace the resume prompt
ccm config clear auto-switch-prompt
```

On [workers](workers.md), `ccm worker add` and `ccm worker sync` copy this setting from the hub. A worker switches only between its own accounts, because its chats live on that Mac.

It applies to every interactive launch: `ccm <name>`, `run`, `best`, `continue` and ⏎ in the dashboard (which shows `⟳ auto-switch` and skips its "Continue on X?" prompt). With it on:

1. Each launch pins a known conversation: a new chat gets `--session-id <uuid>`, `--resume <id>` is kept and `--continue` is turned into `--resume <latest id for this folder>`. A bare `--resume` (the picker), a search term or `--fork-session` cannot be followed, so that launch runs without auto-switch (with a warning).
2. While claude runs, multiclaude watches the conversation transcript (`projects/<folder>/<id>.jsonl`). If you `/clear` or `/resume` another chat inside claude, multiclaude follows the newest transcript written in that folder since launch and resumes that conversation instead. When Claude Code records a usage-limit error there (`"isApiErrorMessage":true` with `"error":"rate_limit"`), multiclaude stops claude (Ctrl-C, then SIGTERM after 2s, SIGKILL after 5s more), marks that account limited in the usage cache, picks the account with the most headroom from the cache (no extra usage request; same-login duplicates count once), prints `ccm: work hit its limit — continuing on main` and resumes the same conversation there with your default/per-account args, `--resume <id>` and a prompt telling Claude to check what was already done and carry on.
3. It keeps going from account to account. If every account is limited it prints `ccm: all accounts are limited — next reset …`, waits until the soonest known reset plus a minute, and resumes on that account. Ctrl-C while waiting stops cleanly (exit 130). With no known reset time it stops. A limit whose reset time cannot be read from the message counts as lasting one hour.
   If the account that just reset hits its limit again within 10 minutes, multiclaude backs off (`… backing off 15m`): the next wait is at least 15m, then 30m, then 60m. There is never more than one relaunch within 5 minutes of a reset wait.
4. Quitting claude yourself never switches. At most 10 switches happen per chain and two switches are always at least 30 seconds apart.
5. Ctrl-Z works as usual: claude and multiclaude are suspended together and `fg` brings both back.
6. `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_API_KEY` and `ANTHROPIC_AUTH_TOKEN` are not passed to a supervised claude (one warning line says so), since they would pin one credential and make switching useless.

Server errors, overloaded/network errors, billing and login errors never trigger a switch.

## T3 Code / SDK sessions

With `autoSwitch` on in `~/.claude-accounts/config.json` (`"autoSwitch": true`), the shim supervises SDK sessions (claude started with `--output-format stream-json --input-format stream-json`, which is how T3 runs it) instead of exec'ing claude. When the account hits its usage limit mid-turn, the session moves to another account by itself:

- **Detected by** a `rate_limit_event` with status `rejected` (not `allowed_warning`, not while running on extra usage), or an error result / `rate_limit` assistant error reading "You've hit your … limit" or "Request rejected (429)". A later `allowed`/`allowed_warning` event cancels a pending `rejected`. Network, billing and auth errors are passed through untouched (with any held-back event before them).
- **Then** the account is marked limited in the shared usage cache (with its reset time), the freest account is chosen **from the cache only** (no network; logged-out, limited and duplicate logins skipped), and claude is restarted as that account with `--resume <session>`. The SDK's setup requests are replayed (answers the SDK already had are hidden; a request still waiting for its answer gets it from the new child), control requests the old child never answered (e.g. `mcp_status`) are sent again, and instead of resending your message (the transcript already has it) the new child gets a short "[multiclaude] … resume where you left off, check what was already done first" prompt. Messages you sent while that turn was running are sent again after it, in order. Customize the prompt with `"autoSwitchPrompt"` in the config.
- **Credentials:** `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_API_KEY` and `ANTHROPIC_AUTH_TOKEN` are removed from claude's environment (stderr says so once), so each account's own login is used and a switch really changes credentials.
- **No account free:** the turn waits (checking every minute) until the soonest reset + 1 minute, then continues — on the same account if that is the first one free. Pressing stop (an SDK `interrupt`) while it waits is answered at once: the turn ends with the limit error and multiclaude stops waiting until your next message. If the SDK closes stdin while messages are still unanswered, they are delivered once an account is free and the proxy exits when claude finishes.
- **Guards:** at most 10 switches in a row, never two within 30 s; after 10 the limit error is shown as usual for that turn. The count starts again after a turn succeeds, after waiting for a reset, and with your next message. stderr gets one line per switch: `ccm: work limited — continuing on main`.
- **Opt out** for one run with `CCM_NO_PROXY=1`. Everything else (terminal `claude`, `-p`, plain `--output-format json`) still execs claude directly.
