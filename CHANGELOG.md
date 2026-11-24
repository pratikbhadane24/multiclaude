# Changelog

Notable changes to multiclaude. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow [Semantic Versioning](https://semver.org/).

## Unreleased

### Added

- `ccm worker add|sync|status|list`: set up other Macs as workers over SSH and keep them in step (for Orca SSH hosts).
- `ccm config set default-account best`: plain `claude` starts on the account with the most headroom.
- `ccm list --json` and `ccm shell-init zsh --worker`.
- `ccm worker add` copies your git identity, installs `gh` and signs it in to GitHub on the worker.
- `ccm worker add|sync` copy the hub's auto-switch setting and resume prompt to the worker.
- `ccm doctor --worker` skips main's login check. Worker setup uses it, since ccm never logs main in on a worker.

### Changed

- Logins are also read from an account's `.credentials.json` when the Keychain has none (SSH sessions).

### Fixed

- Chat history (`projects`, `file-history`, `todos`, `plans`) is shared even when `~/.claude` has none yet, as on a fresh worker. Before, each account kept its own chats there, so an auto-switch resume on another account failed with "No conversation found". `ccm sync` merges chats that were already split.

## v0.1.0 - 2026-11-23

First public release.

### Added

- Per-account Claude Code logins with a shared setup, shared MCP servers and shared chat history.
- Terminal dashboard (`ccm`, `ccm dash`) with 5-hour and 7-day usage per account.
- `ccm status`, `ccm best` and `ccm continue`.
- Claude status line segment (`ccm statusline install`).
- Default and per-account Claude arguments (`ccm config`).
- Auto-switch to another account when a usage limit is hit, for terminal and T3 Code / SDK sessions.
- T3 Code integration (`ccm t3-sync`).
- `ccm doctor` to check and repair the setup.
- `ccm version`.
- Release archives for Apple silicon and Intel Macs, with an install script.
