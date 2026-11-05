#!/usr/bin/env bash
# Build, sign, optionally notarize, and package a macOS release.
#
# Usage:
#   CODESIGN_IDENTITY="Developer ID Application: ..." \
#   [NOTARY_PROFILE="marktoast-notary"] \
#     ./scripts/release.sh 0.1.0
#
# VERSION must be an explicit Apple-numeric version (one to three dot-separated
# integers). Unsigned local packaging is opt-in with ALLOW_UNSIGNED=1.
set -euo pipefail

die() {
  echo "error: $*" >&2
  exit 1
}

if [[ "$(uname -s)" != "Darwin" ]]; then
  die "scripts/release.sh only supports macOS"
fi

if [[ $# -ne 1 || -z "${1:-}" ]]; then
  echo "usage: CODESIGN_IDENTITY='Developer ID Application: ...' [NOTARY_PROFILE=...] ./scripts/release.sh 0.1.0" >&2
  echo "       ALLOW_UNSIGNED=1 ./scripts/release.sh 0.1.0  # local unsigned package" >&2
  exit 2
fi

VERSION="$1"
export VERSION
if [[ ! "$VERSION" =~ ^[0-9]+(\.[0-9]+){0,2}$ ]]; then
  die "VERSION must be an explicit Apple-numeric version such as 0.1.0"
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "release requires a git work tree"
if [[ -n "$(git status --porcelain)" ]]; then
  echo "error: git work tree must be clean before releasing" >&2
  git status --short >&2
  exit 1
fi

if [[ "${ALLOW_UNSIGNED:-0}" != "1" ]]; then
  [[ -n "${CODESIGN_IDENTITY:-}" && "${CODESIGN_IDENTITY}" != "-" ]] \
    || die "CODESIGN_IDENTITY is required unless ALLOW_UNSIGNED=1 (and must not be '-')"
fi
if [[ -n "${NOTARY_PROFILE:-}" ]]; then
  [[ -n "${CODESIGN_IDENTITY:-}" && "${CODESIGN_IDENTITY}" != "-" ]] \
    || die "NOTARY_PROFILE requires a real CODESIGN_IDENTITY"
fi

echo "→ verifying release tree"
make verify

GOARCH="$(go env GOARCH)"
[[ -n "$GOARCH" ]] || die "go env GOARCH returned an empty value"
[[ "$(go env GOOS)" == "darwin" ]] || die "release requires a darwin Go toolchain"

DIST="$ROOT/dist"
APP="$DIST/MarkToast.app"
NAME="MarkToast-${VERSION}-macos-${GOARCH}"
ZIP_NAME="${NAME}.zip"
DMG_NAME="${NAME}.dmg"
ZIP="$DIST/$ZIP_NAME"
DMG="$DIST/$DMG_NAME"

rm -rf "$DIST"
mkdir -p "$DIST"

# install.sh performs the only build for packaging, then ad-hoc/real-signs and
# verifies the app. It also copies LICENSE and docs/CREDITS.md into Resources.
APP_DIR="$APP" CLI_DIR="$DIST/bin" VERSION="$VERSION" ./scripts/install.sh

codesign --verify --deep --strict --verbose=2 "$APP"

CLI_VERSION="$("$DIST/bin/marktoast" version | tr -d '\r\n')"
PLIST_SHORT_VERSION="$(plutil -extract CFBundleShortVersionString raw -o - "$APP/Contents/Info.plist")"
PLIST_BUILD_VERSION="$(plutil -extract CFBundleVersion raw -o - "$APP/Contents/Info.plist")"
if [[ "$CLI_VERSION" != "$VERSION" ]]; then
  die "CLI version $CLI_VERSION does not equal requested VERSION $VERSION"
fi
if [[ "$PLIST_SHORT_VERSION" != "$VERSION" || "$PLIST_BUILD_VERSION" != "$VERSION" ]]; then
  die "Info.plist version $PLIST_SHORT_VERSION/$PLIST_BUILD_VERSION does not equal requested VERSION $VERSION"
fi

make_archives() {
  rm -f "$ZIP" "$DMG"
  ditto -c -k --sequesterRsrc --keepParent "$APP" "$ZIP"
  hdiutil create -volname "MarkToast" -srcfolder "$APP" -ov -format UDZO "$DMG" >/dev/null
}
make_archives

if [[ -n "${NOTARY_PROFILE:-}" ]]; then
  echo "→ notarizing $ZIP_NAME (profile: $NOTARY_PROFILE)"
  xcrun notarytool submit "$ZIP" --keychain-profile "$NOTARY_PROFILE" --wait
  xcrun stapler staple "$APP"
  xcrun stapler validate "$APP"
  codesign --verify --deep --strict --verbose=2 "$APP"
  echo "→ rebuilding archives with the stapled app"
  make_archives
fi

# Gatekeeper assessment only makes sense for a real Developer ID signature that
# was notarized; ad-hoc signatures are structurally verified but not trusted.
if [[ -n "${NOTARY_PROFILE:-}" && -n "${CODESIGN_IDENTITY:-}" && "${CODESIGN_IDENTITY}" != "-" ]]; then
  spctl --assess --type execute --verbose=4 "$APP"
fi

(
  cd "$DIST"
  shasum -a 256 "$ZIP_NAME" "$DMG_NAME" > SHA256SUMS
)

echo
echo "✔ dist/$ZIP_NAME"
echo "✔ dist/$DMG_NAME"
echo "✔ dist/SHA256SUMS"
echo
cat "$DIST/SHA256SUMS"
