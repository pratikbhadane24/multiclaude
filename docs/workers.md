# Workers

A worker is another Mac that runs Claude for you, while you stay on your main Mac (the hub). You choose a worker per task in [Orca](https://github.com/stablyai/orca) under **Run on**, so the agent's CPU and memory come from that Mac. `ccm worker` sets a worker up from the hub with one command and keeps it in step.

> Workers must be the same kind of Mac as the hub (macOS on Apple silicon, or macOS on Intel). Linux workers are planned.

## Before you start (once per worker)

1. Use the **same user name** on the worker as on the hub (`whoami` on the hub). Paths then match on every machine.
2. In System Settings → General → Sharing, turn on **Remote Login**.
3. Install [Tailscale](https://tailscale.com/download/mac) (the Standalone build) on the hub and the worker, signed in to the same account.
4. On the worker, keep it awake and install Apple's command-line tools:
   ```sh
   sudo pmset -a sleep 0 disksleep 0 womp 1 autorestart 1
   xcode-select --install
   ```
5. On the hub, give the worker a short SSH name and copy your key to it:
   ```sh
   # ~/.ssh/config
   Host worker1
     HostName <tailscale name of the worker>
     User <your user name>
   ```
   ```sh
   ssh-copy-id worker1
   ssh worker1 'echo ok'     # must print ok without a password
   ```
6. In Orca, add the worker under Settings → SSH.

## Set up a worker

```sh
ccm worker add worker1 --accounts inbox2,inbox3
```

Run it in a terminal; the account logins are interactive. Each step prints ✓ or ✗, and running the command again is safe.

1. **check worker:** SSH works, the OS and architecture match the hub, and git is present.
2. **Claude Code:** installed with the official installer if missing.
3. **install ccm:** this `ccm` and `ccm-shim` are copied to `~/.local/share/ccm`, then `ccm install` runs.
4. **shell setup:** a marked block (`# >>> ccm >>>`) goes into the worker's `~/.zshenv` (PATH for SSH commands) and `~/.zshrc` (`ccm shell-init zsh --worker`). The worker block also defines a `claude` function, so in Orca panes `claude` always goes through ccm. ccm asks first unless you pass `--yes`.
5. **push shared setup:** `CLAUDE.md`, rules, skills, plugins, scripts, agents, commands, hooks, output styles and keybindings are copied (changes only, nothing deleted). `settings.json` is merged: your settings win, the worker's own hooks (Orca's) are kept, and the status line points at the worker's ccm. The hub's MCP servers go to `~/.claude-accounts/mcp-hub.json`, so the worker's `~/.claude.json` is never written. Auto-pick is turned on (`defaultAccount: best`).
6. **log in accounts:** each account that is not logged in yet runs `ccm add` on the worker. Copy the link it prints into the browser profile signed in to that account, then paste the code back.
7. **doctor:** `ccm doctor` runs on the worker.

Never pushed: chat history (`projects`, `file-history`, `todos`, `plans`), logins (`.credentials.json`) and `~/.claude.json`. Logins are never copied between machines, because one machine's token refresh would log the other out. Each worker logs in once, per account.

## Day to day

```sh
ccm worker status      # load, free memory, running Claude sessions, ccm version, logins
ccm worker sync --all  # after you change skills, plugins, settings or MCP servers on the hub
ccm worker list
```

On a worker, plain `claude` starts on the worker account with the most room, judged from cached usage. `ccm inbox2` or `ccm use inbox2` still picks an account explicitly.

## Notes

- **Logins on workers are files.** Over SSH the macOS Keychain is locked, so Claude Code keeps each login in that account's `.credentials.json` (mode 0600). ccm reads it there; it never writes it.
- **Chats stay on the worker** that ran them. They are shared between that worker's accounts, not with the hub.
- **MCP runtimes** (`npx`, `uvx`) are not installed for you. If your MCP servers need them, install them on the worker yourself, for example `brew install node uv`.
- **FileVault and reboots:** after a reboot, a worker with FileVault waits at the unlock screen, and SSH and Tailscale come back only after you unlock it.
