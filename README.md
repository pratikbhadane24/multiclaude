# multiclaude

[![CI](https://github.com/pratikbhadane24/multiclaude/actions/workflows/ci.yml/badge.svg)](https://github.com/pratikbhadane24/multiclaude/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/pratikbhadane24/multiclaude)](https://github.com/pratikbhadane24/multiclaude/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Run several Claude Code subscription accounts side by side on one Mac, with one short command: `ccm`.

multiclaude gives every account its own login and plan limits while sharing one setup (settings, skills, plugins, MCP servers) and one chat history between them. A terminal dashboard shows how much of each account's limit is left, and a conversation can carry on from another account when the current one runs out.

- **Accounts stay separate.** Each account has its own OAuth login in the macOS Keychain. No API keys.
- **Setup is shared.** `CLAUDE.md`, settings, rules, skills, plugins, agents, commands, hooks and MCP servers are edited once and used by every account.
- **History is shared.** `/resume` lists every chat from every account.
- **Usage at a glance.** A dashboard and a status line show 5-hour and 7-day usage per account.
- **Hand-off.** `ccm continue` resumes a chat on the account with the most room. Optional auto-switch does it for you when a limit is hit.
- **T3 Code.** Every account can be added as a T3 Code instance.
- **Workers.** Set up other Macs as workers with one command, then run agents on them from [Orca](https://github.com/stablyai/orca) while your accounts and setup stay in step.

> macOS only. multiclaude is an unofficial tool and is not affiliated with or endorsed by Anthropic. See [Disclaimer](#disclaimer).

## Install

You need macOS and [Claude Code](https://docs.claude.com/en/docs/claude-code) already installed and logged in.

**Install script** (downloads the latest release, verifies its checksum, installs to `~/.local/share/ccm`):

```sh
curl -fsSL https://raw.githubusercontent.com/pratikbhadane24/multiclaude/main/install.sh | sh
```

**Direct download:** get the archive for your Mac from the [latest release](https://github.com/pratikbhadane24/multiclaude/releases/latest):

| Mac | Archive |
|---|---|
| Apple silicon (M1 and later) | [`ccm_darwin_arm64.tar.gz`](https://github.com/pratikbhadane24/multiclaude/releases/latest/download/ccm_darwin_arm64.tar.gz) |
| Intel | [`ccm_darwin_amd64.tar.gz`](https://github.com/pratikbhadane24/multiclaude/releases/latest/download/ccm_darwin_amd64.tar.gz) |

Unpack it somewhere permanent and run `./ccm install` from that folder.

**With Go**, or from source: see [docs/install.md](docs/install.md), which also covers updating, checksums and uninstalling.

After any install, add this line to `~/.zshrc` once (multiclaude never edits it for you), then open a new terminal:

```sh
eval "$(ccm shell-init zsh)"
```

Run `ccm doctor` to check the install.

## Quick start

```sh
ccm add work        # create an account called "work" and log it in
ccm                 # open the dashboard
```

`main` is your existing `~/.claude` login. It is never moved or deleted. `ccm add` copies a login link to the clipboard instead of opening your default browser, so you can paste it into the browser profile that is signed in to the right Claude account.

## Daily use

```text
ccm            open the dashboard:  ↑↓ pick an account, ⏎ start Claude on it, q quit
ccm status     see how much of each account's limit is used
ccm best       start Claude on the account with the most room left
ccm continue   carry on this folder's last chat on a free account
ccm work       start Claude on the account called "work"
```

## Workers

Run Claude on other Macs (for example a Mac mini) while you stay on your main one. Get SSH to the worker working over [Tailscale](https://tailscale.com) first (steps in [Workers](docs/workers.md#before-you-start-once-per-worker)), then:

```sh
ccm worker add worker1 --accounts work,personal   # set up the Mac and log its accounts in
ccm worker status                                 # load, memory, sessions and logins per worker
ccm worker sync --all                             # push setup changes from this Mac
```

In Orca, add the worker under Settings → SSH and pick it under **Run on**. Each worker logs in its own accounts (logins are never copied between Macs), uses the account with the most room, and copies your auto-switch setting. Chats stay on the worker that ran them.

## Documentation

| Guide | What it covers |
|---|---|
| [Install](docs/install.md) | Install script, direct download, Go, source builds, updating, uninstalling |
| [Accounts](docs/accounts.md) | Adding, logging in and removing accounts; login link modes |
| [Dashboard and status line](docs/dashboard.md) | Dashboard keys and states, the Claude status line segment, how usage is read |
| [Configuration](docs/configuration.md) | Default and per-account Claude arguments |
| [Workers](docs/workers.md) | Running agents on other Macs from one hub with Orca |
| [Auto-switch](docs/auto-switch.md) | Moving a running conversation to another account when a limit is hit |
| [Shared history and T3 Code](docs/history-and-t3.md) | How chats are shared and merged; T3 Code instances |
| [Command reference](docs/commands.md) | Every command, flag and exit code |
| [How it works](docs/how-it-works.md) | File layout, isolation, safety guarantees, caveats |
| [Troubleshooting](docs/troubleshooting.md) | `ccm doctor`, stale usage rows, Gatekeeper, common errors |

## Safety in short

- Tokens stay in the macOS Keychain and in memory. They are never logged, printed or written to files, and are sent only to `api.anthropic.com`.
- multiclaude never calls the OAuth refresh endpoint itself.
- Files that multiclaude replaces are backed up first, and backups are never deleted.
- `ccm remove` deletes nothing without `--purge` and a confirmation.

The full list is in [How it works](docs/how-it-works.md#safety).

## Disclaimer

multiclaude is an independent, unofficial project. It is not affiliated with, endorsed by or supported by Anthropic. "Claude" and "Claude Code" are trademarks of Anthropic.

- You are responsible for using each account in line with the terms that apply to it, including Anthropic's terms of service and usage policies. Check them before using several accounts or auto-switch.
- Usage figures come from an undocumented endpoint that may change or stop working at any time.
- The software is provided as is, without warranty. See [LICENSE](LICENSE).

## Contributing

Bug reports and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for how to build and test, and [SECURITY.md](SECURITY.md) for reporting security issues.

## License

[MIT](LICENSE)
