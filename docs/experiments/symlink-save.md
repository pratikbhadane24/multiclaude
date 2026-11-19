# Experiment: does Claude Code replace symlinked settings.json / plugins/ on save?

Claude Code v2.1.328, macOS. Date: 2026-11-19.

## Setup
- Throwaway dir under `/private/tmp/claude-501/` (deleted afterwards); no login, real `~/.claude`, `~/.claude.json` and Keychain untouched.
- `CLAUDE_CONFIG_DIR=<tmp>/cfg`, with `cfg/settings.json` -> symlink to a COPY of `~/.claude/settings.json`, and `cfg/plugins` -> symlink to an empty dir.
- `claude config set` does not exist in this version; used the plugin subcommands, which write `settings.json` and `plugins/`.

## Commands run
1. `claude plugin marketplace add <local marketplace dir>`
2. `claude plugin install tiny@tinymkt` (tiny local plugin)
3. `claude plugin disable tiny@tinymkt`

## Result
- All three succeeded without login.
- `test -L cfg/settings.json` -> still a symlink. The target file contained the new `enabledPlugins` entry (`tiny@tinymkt: false`) and `extraKnownMarketplaces` entry (`tinymkt`), so writes went THROUGH the link.
- `test -L cfg/plugins` -> still a symlink; target holds `cache/`, `installed_plugins.json`, `known_marketplaces.json`, `marketplaces/`.
- `.claude.json` and `backups/` were created as real per-account files in `cfg/`.

## Conclusion
CLI-driven saves of `settings.json` and plugin installs preserve symlinks. The spec Decisions 3 fallback (link parent dirs only) is not needed for these paths.

## Caveat
The interactive `/config` UI save path was not exercised (needs a TUI session). Re-verify in the Task 12 live E2E; `ccm doctor` still detects a replaced link.
