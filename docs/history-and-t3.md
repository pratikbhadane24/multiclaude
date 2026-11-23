# Shared history and T3 Code

## Shared history

Every account sees the same chats. Run `/resume` in any account and all conversations (and project memory, file history, todos, plans) are listed, so when one account is limited you can pick up on another.

`ccm continue` does this for the folder you are in: it resumes the latest chat on the account with the most room (`--from`, `--to`, `--session` to choose).

If an account already has its own history, `ccm sync` (also run by `add` and `doctor --fix`) merges it into the shared copy safely:
- Nothing is overwritten or deleted.
- An account with a **running Claude session is skipped** (close it and run `ccm sync` again).
- Where two versions of a file differ, both are kept; the extra one is saved as `*.from-<account>-<unix>.jsonl`.

## T3 Code and phone

```sh
ccm t3-sync     # one claude-<account> instance per account, with your default args; then restart T3
t3 up           # T3's own remote service, for using it from your phone
```

Auto-switch for T3 / SDK sessions is described in [auto-switch.md](auto-switch.md#t3-code--sdk-sessions).
