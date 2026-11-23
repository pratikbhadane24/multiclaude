# Contributing to multiclaude

Thanks for helping. Bug reports, fixes and small focused features are all welcome.

## Before you start

- For anything larger than a bug fix, open an issue first so the approach can be agreed before you write code.
- multiclaude handles logins and other people's chat history. Changes must keep the guarantees in [docs/how-it-works.md](docs/how-it-works.md#safety). In particular: tokens are never logged, printed or written to files, and nothing in `~/.claude` is overwritten or deleted.

## Build and test

You need macOS and the Go version named in `go.mod`.

```sh
make build     # bin/ccm and bin/ccm-shim
make check     # gofmt check, go vet, go test (what CI runs)
make install   # build, then install your build as the active ccm
```

Tests never touch your real accounts: they run against a temporary home folder and fake `claude` and `security` commands.

`scripts/e2e-autoswitch.sh` runs an end-to-end auto-switch test with a fake Claude in a throwaway home folder.

To see the dashboard with canned data (for screenshots or UI work):

```sh
CCM_DASH_DEMO=1 ./bin/ccm dash
```

## Code style

- Format with `gofmt`. `go vet` must pass.
- Write the test first, and keep tests in Arrange / Act / Assert order with names that describe the behaviour.
- Keep functions short (under about 50 lines) and files focused.
- Return new values instead of mutating arguments.
- Handle every error. Do not discard one silently.
- No new dependencies without discussion. multiclaude is the standard library plus the Charm libraries used by the dashboard.

## Layout

| Path | Contents |
|---|---|
| `cmd/ccm` | The CLI entry point |
| `cmd/ccm-shim` | The launcher installed as `~/.claude-accounts/bin/claude` |
| `cmd/fakeclaude` | A fake `claude` used by end-to-end tests |
| `internal/cli` | Command parsing and one file per command |
| `internal/dash` | The terminal dashboard |
| `internal/autoswitch`, `internal/sdkproxy` | Auto-switch for terminal and SDK sessions |
| `internal/usage`, `internal/status` | Reading, caching and rendering usage |
| `internal/keychain`, `internal/auth` | Keychain reads and `claude auth` calls |
| `internal/links`, `internal/history`, `internal/mcp` | Shared setup, history merge, MCP sync |

## Commits and pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`, `perf:`, `ci:`.
- One logical change per pull request, with tests.
- Update the docs in `docs/` and the usage text in `internal/cli/dispatch.go` when behaviour changes.
- Add a line to `CHANGELOG.md` under "Unreleased".

## Releasing (maintainers)

1. Move the "Unreleased" entries in `CHANGELOG.md` under a new version heading and commit.
2. Tag and push:

   ```sh
   git tag v1.2.3
   git push origin v1.2.3
   ```

The Release workflow builds the macOS archives and publishes the GitHub release. `make dist VERSION=v1.2.3` builds the same archives locally into `dist/`.
