# Security policy

multiclaude reads Claude Code OAuth tokens from the macOS Keychain and manages files in `~/.claude` and `~/.claude-accounts`, so security reports are taken seriously.

## Reporting a vulnerability

Please do not open a public issue for a security problem.

Report it privately through GitHub: open the repository's **Security** tab and choose **Report a vulnerability** ([direct link](https://github.com/pratikbhadane24/multiclaude/security/advisories/new)).

Include the multiclaude version (`ccm version`), your macOS and Claude Code versions, and steps to reproduce. Never include real tokens.

You can expect a first reply within a week. Please allow time for a fix to be released before disclosing the issue publicly.

## Supported versions

Only the latest release receives security fixes.

## What multiclaude promises

- Tokens stay in the macOS Keychain and in process memory. They are never logged, printed, written to files or included in `--json` output.
- Tokens are sent only to `api.anthropic.com`.
- multiclaude never calls the OAuth refresh endpoint itself.
- Config and cache files under `~/.claude-accounts` are written with mode `0600`.

A way to break any of these is a vulnerability. The full list of guarantees is in [docs/how-it-works.md](docs/how-it-works.md#safety).

## Known limitations

- Secrets in MCP server `env` or `headers` are copied into each account's `.claude.json` (mode `0600`).
- Release binaries are not signed with an Apple Developer ID. Verify downloads against `checksums.txt`.
- `--dangerously-skip-permissions` as a default argument applies to every account and every T3 Code instance. See [docs/configuration.md](docs/configuration.md).
