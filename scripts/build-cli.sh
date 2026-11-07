#!/usr/bin/env bash
# Build the standalone CLI for every supported target and drop the archives in
# dist/. Release builds are made on this machine; there is no CI workflow.
#
# Usage: scripts/build-cli.sh 0.1.0
set -euo pipefail

die() {
  echo "error: $*" >&2
  exit 1
}

if [[ $# -ne 1 || -z "${1:-}" ]]; then
  echo "usage: scripts/build-cli.sh 0.1.0" >&2
  exit 2
fi

VERSION="$1"
[[ "$VERSION" =~ ^[0-9]+(\.[0-9]+){0,2}$ ]] || die "VERSION must be one to three dot-separated integers"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
DIST="$ROOT/dist"
mkdir -p "$DIST"

# Every target the CLI is published for. The desktop shell is macOS-only and
# has its own packaging in scripts/release.sh.
TARGETS=(
  darwin/arm64
  darwin/amd64
  linux/amd64
  linux/arm64
  windows/amd64
  windows/arm64
)

for target in "${TARGETS[@]}"; do
  GOOS="${target%/*}"
  GOARCH="${target#*/}"
  NAME="marktoast-${VERSION}-${GOOS}-${GOARCH}"
  EXT=""
  if [[ "$GOOS" == "windows" ]]; then
    EXT=".exe"
  fi
  echo "→ ${NAME}"
  mkdir -p "$DIST/$NAME"
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath \
    -ldflags "-s -w -X github.com/realchendahuang/MarkToast/internal/version.Version=${VERSION}" \
    -o "$DIST/$NAME/marktoast${EXT}" ./cmd/marktoast
  cp LICENSE README.md "$DIST/$NAME/"
  if [[ "$GOOS" == "windows" ]]; then
    (cd "$DIST" && zip -q -r "$NAME.zip" "$NAME")
  else
    tar -C "$DIST" -czf "$DIST/$NAME.tar.gz" "$NAME"
  fi
  rm -rf "$DIST/$NAME"
done

echo
echo "✔ CLI archives in dist/"
ls -1 "$DIST" | grep '^marktoast-' || true
