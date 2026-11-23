# Changelog

Notable changes to multiclaude. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow [Semantic Versioning](https://semver.org/).

## Unreleased

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
