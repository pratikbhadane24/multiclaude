#!/bin/sh
# ccm installer: downloads a release, verifies its checksum, and runs
# `ccm install`.
#
#   curl -fsSL https://raw.githubusercontent.com/pratikbhadane24/multiclaude/main/install.sh | sh
#
# Environment:
#   CCM_VERSION      release tag to install (default: latest)
#   CCM_INSTALL_DIR  where the binaries live (default: ~/.local/share/ccm)
set -eu

REPO="pratikbhadane24/multiclaude"
VERSION="${CCM_VERSION:-latest}"
INSTALL_DIR="${CCM_INSTALL_DIR:-$HOME/.local/share/ccm}"
LINK="$HOME/.local/bin/ccm"

fail() {
  echo "ccm install: $*" >&2
  exit 1
}

[ "$(uname -s)" = "Darwin" ] || fail "ccm supports macOS only (it uses the macOS Keychain)"

case "$(uname -m)" in
  arm64) ARCH=arm64 ;;
  x86_64) ARCH=amd64 ;;
  *) fail "unsupported CPU: $(uname -m)" ;;
esac

ARCHIVE="ccm_darwin_$ARCH.tar.gz"
if [ "$VERSION" = "latest" ]; then
  BASE="https://github.com/$REPO/releases/latest/download"
else
  BASE="https://github.com/$REPO/releases/download/$VERSION"
fi

TMP="$(mktemp -d "${TMPDIR:-/tmp}/ccm-install.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT INT TERM

echo "Downloading $ARCHIVE ($VERSION)..."
curl -fsSL "$BASE/$ARCHIVE" -o "$TMP/$ARCHIVE" || fail "download failed: $BASE/$ARCHIVE"
curl -fsSL "$BASE/checksums.txt" -o "$TMP/checksums.txt" || fail "download failed: $BASE/checksums.txt"

WANT="$(awk -v f="$ARCHIVE" '$2 == f { print $1 }' "$TMP/checksums.txt")"
[ -n "$WANT" ] || fail "no checksum listed for $ARCHIVE"
GOT="$(shasum -a 256 "$TMP/$ARCHIVE" | awk '{ print $1 }')"
[ "$WANT" = "$GOT" ] || fail "checksum mismatch for $ARCHIVE (expected $WANT, got $GOT)"

tar -xzf "$TMP/$ARCHIVE" -C "$TMP"
SRC="$TMP/ccm_darwin_$ARCH"
[ -x "$SRC/ccm" ] && [ -x "$SRC/ccm-shim" ] || fail "archive is missing ccm or ccm-shim"

mkdir -p "$INSTALL_DIR"
# Copy next to the target first, then rename, so a running ccm is never half-written.
for bin in ccm ccm-shim; do
  cp "$SRC/$bin" "$INSTALL_DIR/.$bin.new"
  chmod 755 "$INSTALL_DIR/.$bin.new"
  mv -f "$INSTALL_DIR/.$bin.new" "$INSTALL_DIR/$bin"
done

# `ccm install` refuses to replace an existing ~/.local/bin/ccm. A symlink left
# by an earlier install (for example a source build) is safe to repoint.
if [ -L "$LINK" ] && [ "$(readlink "$LINK")" != "$INSTALL_DIR/ccm" ]; then
  echo "Repointing $LINK (was -> $(readlink "$LINK"))"
  rm "$LINK"
fi

"$INSTALL_DIR/ccm" install
echo "Installed $("$INSTALL_DIR/ccm" version) in $INSTALL_DIR"
