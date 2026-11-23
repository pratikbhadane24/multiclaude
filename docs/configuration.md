# Configuration

Settings live in `~/.claude-accounts/config.json` (mode 0600). Manage them with `ccm config`.

## Default args

Add flags to every interactive launch (`ccm <name>`, `run`, `best`, the dashboard):

```sh
ccm config set default-args --dangerously-skip-permissions
ccm config set args work --model opus     # extra args for one account only
ccm config show
ccm config clear default-args             # or: ccm config clear args work
```

Order: default args, then the account's args, then anything you type. They are never added to internal login/status checks or to plain `claude`.

They are also copied into T3 Code. Run `ccm t3-sync` after changing them. If you edited an instance's launch args inside T3, multiclaude keeps your edit and warns instead of overwriting it.

**Security note, in plain words:** `--dangerously-skip-permissions` tells Claude to run commands and edit files without asking you first. Anything Claude decides to do, it does. Only use it in folders and projects you trust, and remember it applies to every account and every T3 instance once it is a default. Remove it with `ccm config clear default-args`.
