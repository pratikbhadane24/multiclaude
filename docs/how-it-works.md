# How it works

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
- Tokens stay where Claude Code keeps them: the macOS Keychain (read with `/usr/bin/security`), or, when the Keychain has no entry (SSH sessions on workers), the account's private `.credentials.json` (read only if it is a regular file owned by you with mode 0600; ccm never writes it). In ccm they live only in memory. They are never logged, printed, written to files, or included in `--json`. They are sent only to `api.anthropic.com`. `ccm` never calls the OAuth refresh endpoint itself.
- Workers (`ccm worker`): the only place ccm edits shell files is a marked block in a worker's `~/.zshenv` and `~/.zshrc`, after asking. Remote files are backed up before change and written privately and atomically. Logins and chat history are never copied between machines.
- `ccm remove` never deletes anything without `--purge` plus confirmation.
- The history migration refuses an account dir that is, contains or aliases `~/.claude`, skips an account with a running Claude session (a live pid in its `sessions/`, or an unparsable session file touched in the last 10 minutes), needs both dirs on the same volume, and keeps symlinks inside history as renamed links (`*.from-<account>-<unix>`) instead of moving them.
- Default args are never applied to internal `claude auth` calls.

## Caveats

- **Undocumented usage endpoint.** `status` relies on `GET /api/oauth/usage`, which Anthropic does not document. It may change; multiclaude shows `usage API error` for that account and keeps going.
- **MCP secrets are duplicated.** Secrets in MCP `env`/`headers` end up in each account's `.claude.json`, written with mode `0600`.
- **Remote OAuth MCP servers authorize once per account.**
- **Symlink saves.** `claude plugin ...` commands keep the shared links intact (checked with Claude Code 2.1.328). If Claude Code ever replaces a link with a real file, `ccm doctor` detects it and `ccm doctor --fix` re-links it (the replaced file is backed up first).
- Chat history is shared, so `status` shows TODAY tokens once (in the `ALL` row), not per account.
- Moving a running session between accounts is not supported; `ccm continue` resumes the chat in a new session.
