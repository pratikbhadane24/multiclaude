# Install

multiclaude runs on macOS only (it uses the macOS Keychain). You need [Claude Code](https://docs.claude.com/en/docs/claude-code) installed and logged in first.

multiclaude is two binaries that must sit in the same folder:

- `ccm`, the command you run.
- `ccm-shim`, a small launcher that `ccm install` copies to `~/.claude-accounts/bin/claude`.

Every method below ends with `ccm install`, which copies the launcher into place and links `ccm` into `~/.local/bin`.

## Install script

```sh
curl -fsSL https://raw.githubusercontent.com/pratikbhadane24/multiclaude/main/install.sh | sh
```

The script downloads the archive for your Mac from the latest release, checks it against the release's `checksums.txt`, unpacks it into `~/.local/share/ccm` and runs `ccm install`.

| Variable | Meaning | Default |
|---|---|---|
| `CCM_VERSION` | Release tag to install, for example `v0.1.0` | `latest` |
| `CCM_INSTALL_DIR` | Folder that holds the two binaries | `~/.local/share/ccm` |

```sh
curl -fsSL https://raw.githubusercontent.com/pratikbhadane24/multiclaude/main/install.sh | CCM_VERSION=v0.1.0 sh
```

## Direct download

1. Download the archive for your Mac from the [latest release](https://github.com/pratikbhadane24/multiclaude/releases/latest):
   - Apple silicon: [`ccm_darwin_arm64.tar.gz`](https://github.com/pratikbhadane24/multiclaude/releases/latest/download/ccm_darwin_arm64.tar.gz)
   - Intel: [`ccm_darwin_amd64.tar.gz`](https://github.com/pratikbhadane24/multiclaude/releases/latest/download/ccm_darwin_amd64.tar.gz)
2. Optional: check it against [`checksums.txt`](https://github.com/pratikbhadane24/multiclaude/releases/latest/download/checksums.txt) from the same release:

   ```sh
   shasum -a 256 ccm_darwin_arm64.tar.gz
   ```

3. Unpack it into a folder you will keep, then install:

   ```sh
   mkdir -p ~/.local/share/ccm
   tar -xzf ccm_darwin_arm64.tar.gz --strip-components 1 -C ~/.local/share/ccm
   ~/.local/share/ccm/ccm install
   ```

The binaries are not signed with an Apple Developer ID. If you downloaded the archive with a browser, macOS may refuse to run them. Clear the quarantine flag once:

```sh
xattr -d com.apple.quarantine ~/.local/share/ccm/ccm ~/.local/share/ccm/ccm-shim
```

Downloads made with `curl` (including the install script) are not quarantined.

## With Go

```sh
go install github.com/pratikbhadane24/multiclaude/cmd/...@latest
"$(go env GOPATH)/bin/ccm" install
```

This builds `ccm` and `ccm-shim` (and the `fakeclaude` test helper) into your Go bin folder.

## From source

```sh
git clone https://github.com/pratikbhadane24/multiclaude.git
cd multiclaude
make install        # make build, then ./bin/ccm install
```

Needs the Go version named in `go.mod`.

## After installing

Add this line to `~/.zshrc` once, then open a new terminal:

```sh
eval "$(ccm shell-init zsh)"
```

Then check the setup:

```sh
ccm version
ccm doctor
```

Next: [add an account](accounts.md).

## Update

Run the same install method again. The launcher at `~/.claude-accounts/bin/claude` is a copy of `ccm-shim`, so `ccm install` must run after every update to refresh it. The install script and `make install` do that for you.

If `ccm install` says `~/.local/bin/ccm exists`, an older install lives somewhere else. Remove that link (`rm ~/.local/bin/ccm`) and run `ccm install` again.

## Uninstall

```sh
ccm statusline uninstall     # if you installed the status line
ccm remove <name> --purge    # for each added account (optional; deletes its dir and Keychain item)
rm ~/.local/bin/ccm
rm -rf ~/.local/share/ccm    # the binaries, if you used the install script
rm -rf ~/.claude-accounts    # registry, config, shim, and any remaining account dirs
# remove the eval "$(ccm shell-init zsh)" line from ~/.zshrc
```

Then, if you want, delete the `claude-<account>` entries from `providerInstances` in `~/.t3/userdata/settings.json` (the `settings.json.ccm-backup-<unix>` from `t3-sync` is next to it). Shared chat history stays in `~/.claude` (accounts merged into it keep their chats there).
