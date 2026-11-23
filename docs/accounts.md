# Accounts

How to add, log in, and remove Claude accounts. `main` is your existing `~/.claude` login; it is never moved or deleted.

## Add an account

```sh
ccm add work
```

multiclaude creates the account, links your shared setup, then **copies a login link to the clipboard**. It never opens your default browser, because that browser is usually signed in to a different Claude account. Paste the link into the browser profile or private window that is signed in to the right one, finish the login, and multiclaude checks the email matches.

Pick where the link goes with `--open` (remembered per account):

| Mode | What happens |
|---|---|
| `copy` (default) | Link copied to the clipboard and printed. No browser opens. |
| `incognito` | Opens in a new Google Chrome incognito window. |
| `chrome:<Profile>` | Opens in that Chrome profile, e.g. `--open chrome:"Profile 2"`. |

If Chrome cannot be opened, multiclaude falls back to `copy`. Only Claude links are ever copied or opened. Log in again any time with `ccm login <name>`, or every logged-out account with `ccm login --all`.

## Remove an account

```sh
ccm remove <name>            # unregister (files kept), asks [y/N]
ccm remove <name> --purge    # also delete its folder and Keychain login
```

`main` cannot be removed. T3 instances are not removed.
