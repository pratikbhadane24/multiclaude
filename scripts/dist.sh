#!/usr/bin/env bash
# Builds the release archives into dist/:
#   ccm_darwin_arm64.tar.gz  ccm_darwin_amd64.tar.gz  checksums.txt
# Archive names carry no version so that
#   https://github.com/<owner>/<repo>/releases/latest/download/<archive>
# always points at the newest release. Usage: VERSION=v1.2.3 scripts/dist.sh
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
MODULE="github.com/pratikbhadane24/multiclaude"
VERSION="${VERSION:?set VERSION, e.g. VERSION=v1.2.3}"
ARCHES=(arm64 amd64)
DIST="$REPO/dist"
LDFLAGS="-s -w -X $MODULE/internal/cli.Version=$VERSION"

sha256() {
  if command -v sha256sum >/dev/null; then sha256sum "$@"; else shasum -a 256 "$@"; fi
}

cd "$REPO"
rm -rf "$DIST"
mkdir -p "$DIST"

for arch in "${ARCHES[@]}"; do
  name="ccm_darwin_$arch"
  stage="$DIST/$name"
  mkdir -p "$stage"
  for cmd in ccm ccm-shim; do
    CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" \
      go build -trimpath -ldflags "$LDFLAGS" -o "$stage/$cmd" "./cmd/$cmd"
  done
  cp LICENSE README.md "$stage/"
  tar -C "$DIST" -czf "$DIST/$name.tar.gz" "$name"
  rm -rf "$stage"
  echo "built $name.tar.gz"
done

(cd "$DIST" && sha256 ./*.tar.gz | sed 's|\./||' > checksums.txt)
echo "wrote dist/checksums.txt ($VERSION)"
