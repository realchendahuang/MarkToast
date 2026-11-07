#!/usr/bin/env bash
# Build the desktop bundle for Windows and Linux and drop it in dist/.
# The macOS bundle is packaged by scripts/release.sh (it needs the .app layout
# and codesign), this script covers the two portable targets.
#
# Usage: scripts/build-desktop.sh 0.1.0
set -euo pipefail

die() {
  echo "error: $*" >&2
  exit 1
}

if [[ $# -ne 1 || -z "${1:-}" ]]; then
  echo "usage: scripts/build-desktop.sh 0.1.0" >&2
  exit 2
fi

VERSION="$1"
[[ "$VERSION" =~ ^[0-9]+(\.[0-9]+){0,2} ]] || die "VERSION must be one to three dot-separated integers"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
DIST="$ROOT/dist"
mkdir -p "$DIST"

RUNTIME_NOTES="$(mktemp)"
trap 'rm -f "$RUNTIME_NOTES"' EXIT
cat > "$RUNTIME_NOTES" <<'TXT'
MarkToast desktop — runtime requirements
==========================================

The window is a web UI over the local hub; the hub is started inside this
process, so nothing else needs to run first. On launch it listens on
http://127.0.0.1:3777 (pass --root / --addr to change the repository or port)
and opens a native window on the same page.

Windows
-------
Needs the WebView2 runtime. Windows 11 ships it; on Windows 10 install the
Evergreen WebView2 Runtime from Microsoft. Data lives in %USERPROFILE%\Memory.

Linux
-----
Needs GTK 4 and WebKitGTK 6.0, loaded at runtime with dlopen (no build-time C
dependency):

  Debian / Ubuntu:  sudo apt install libgtk-4-1 libwebkitgtk-6.0-4
  Fedora:           sudo dnf install gtk4 webkitgtk6.0
  Arch:             sudo pacman -S gtk4 webkitgtk-6.0

A Wayland or X11 session both work. Data lives in ~/Memory. The binary is the
command itself; `./marktoast-desktop` starts the hub and the window, and the
CLI archives ship the same hub as `marktoast serve` if you prefer a browser.

Both builds are ad-hoc/unsigned: Windows may warn on first run (More info →
Run anyway), Linux has no signature to check.
TXT

TARGETS=(
  windows/amd64
  windows/arm64
  linux/amd64
  linux/arm64
)

for target in "${TARGETS[@]}"; do
  GOOS="${target%/*}"
  GOARCH="${target#*/}"
  NAME="MarkToast-desktop-${VERSION}-${GOOS}-${GOARCH}"
  PKG="$DIST/$NAME"
  rm -rf "$PKG"
  mkdir -p "$PKG"

  echo "→ ${NAME}"
  LDFLAGS="-s -w -X github.com/realchendahuang/MarkToast/internal/version.Version=${VERSION}"
  TAGS=""
  BIN="marktoast-desktop"
  if [[ "$GOOS" == "windows" ]]; then
    LDFLAGS="$LDFLAGS -H=windowsgui"
    BIN="marktoast-desktop.exe"
  else
    # The Linux backend is the CGO-free one; it loads GTK4/WebKitGTK at runtime.
    TAGS="-tags purego"
  fi

  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath $TAGS -ldflags "$LDFLAGS" -o "$PKG/$BIN" ./cmd/marktoast-desktop
  cp LICENSE README.md "$PKG/"
  cp "$RUNTIME_NOTES" "$PKG/RUNTIME-REQUIREMENTS.txt"

  if [[ "$GOOS" == "linux" ]]; then
    cp cmd/marktoast-desktop/icon.png "$PKG/marktoast.png"
    cat > "$PKG/marktoast.desktop" <<'DESKTOP'
[Desktop Entry]
Type=Application
Name=MarkToast
Comment=Local-first memory for coding agents
Exec=marktoast-desktop
Icon=marktoast
Terminal=false
Categories=Development;Utility;
DESKTOP
  fi

  if [[ "$GOOS" == "windows" ]]; then
    (cd "$DIST" && zip -q -r "$NAME.zip" "$NAME")
  else
    tar -C "$DIST" -czf "$DIST/$NAME.tar.gz" "$NAME"
  fi
  rm -rf "$PKG"
done

echo
echo "✔ desktop bundles in dist/"
ls -1 "$DIST" | grep '^MarkToast-desktop-' || true
