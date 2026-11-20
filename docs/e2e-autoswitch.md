# Auto-switch end-to-end test

`scripts/e2e-autoswitch.sh` drives ccm's auto-switch-on-usage-limit against
`cmd/fakeclaude`, a stand-in for the `claude` binary.

## Run

```
scripts/e2e-autoswitch.sh              # prints PASS/FAIL per scenario, exit 1 on any FAIL
E2E_KEEP=1 scripts/e2e-autoswitch.sh   # keep the temp dir (logs, outputs) for debugging
E2E_WAIT_SECS=60 scripts/e2e-autoswitch.sh
```

Requires `go`, `python3` and `script(1)`. The script builds `ccm`, `ccm-shim`
and `fakeclaude` into a temp dir, then runs with `HOME` set to a throwaway
directory (it refuses to run if that resolves into the real home). It sets
`CCM_HOME`, `CCM_CLAUDE_BIN=<fakeclaude>` and a dead HTTP proxy, so the real
`~/.claude`, `~/.claude-accounts` and the network are never used. ccm may still
run `security find-generic-password` for service names derived from the
throwaway account dirs; that is a read-only lookup that finds nothing.

Fixtures: accounts `e2e-a`, `e2e-b`, `e2e-c` in `accounts.json`, a fresh
`cache/usage.json` (all low usage, `main` at 95%), and `config.json` with
`"autoSwitch": true`. `FAKECLAUDE_LIMIT_ACCOUNTS=e2e-a`.

## Scenarios

1. Interactive: `ccm run e2e-a --session-id <id> "initial prompt"` under a pty.
   After 2s fakeclaude appends the rate-limit transcript line and waits for a
   signal. Expect ccm to stop it and start another account with
   `--resume <id>` and a `[ccm]` prompt in argv.
2. stream-json: an `initialize` control request and a user message are piped
   into `ccm-shim` with `CLAUDE_CONFIG_DIR=<e2e-a>`. fakeclaude answers with a
   `rate_limit_event` and an error `result`. Expect a second stream-json
   process on another account started with `--resume <same id>` that receives
   a user message beginning with `[ccm]`.

Both are asserted from `$FAKECLAUDE_LOG`.

## fakeclaude

Env: `FAKECLAUDE_LOG` (JSON lines: one per invocation, plus `user_message`
events in stream-json mode), `FAKECLAUDE_LIMIT_ACCOUNTS` (account dir
basenames, `main` for unset `CLAUDE_CONFIG_DIR`), `FAKECLAUDE_LIMIT_AFTER`
(seconds, default 2). Non-session subcommands (`auth`, `mcp`, `--version`, ...)
are logged with `mode: subcommand` and exit at once. Unit tests:
`go test ./cmd/fakeclaude/...`.
