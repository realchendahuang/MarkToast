#!/usr/bin/env bash
# Build a distributable macOS app + zip. Signing/notarization are opt-in:
#   CODESIGN_IDENTITY="Developer ID Application: ..." \
#   NOTARY_PROFILE="marktoast-notary" ./scripts/release.sh v1.0.0
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo 0.0.1-dev)}"
ARCH="$(uname -m)"
NAME="MarkToast-${VERSION}-macos-${ARCH}"

rm -rf dist
mkdir -p dist
APP_DIR="$ROOT/dist/MarkToast.app" CLI_DIR="$ROOT/dist/bin" VERSION="$VERSION" ./scripts/install.sh

ditto -c -k --sequesterRsrc --keepParent "dist/MarkToast.app" "dist/${NAME}.zip"
if command -v hdiutil >/dev/null 2>&1; then
  hdiutil create -volname "MarkToast" -srcfolder "dist/MarkToast.app" -ov -format UDZO "dist/${NAME}.dmg" >/dev/null
fi
shasum -a 256 dist/${NAME}.* > "dist/SHA256SUMS"

if [ -n "${NOTARY_PROFILE:-}" ]; then
  echo "→ notarizing ${NAME}.zip (profile: ${NOTARY_PROFILE})"
  xcrun notarytool submit "dist/${NAME}.zip" --keychain-profile "$NOTARY_PROFILE" --wait
  xcrun stapler staple "dist/MarkToast.app"
  ditto -c -k --sequesterRsrc --keepParent "dist/MarkToast.app" "dist/${NAME}.zip"
  if command -v hdiutil >/dev/null 2>&1; then
    hdiutil create -volname "MarkToast" -srcfolder "dist/MarkToast.app" -ov -format UDZO "dist/${NAME}.dmg" >/dev/null
  fi
  shasum -a 256 dist/${NAME}.* > "dist/SHA256SUMS"
fi

echo
echo "✔ dist/${NAME}.zip"
cat "dist/SHA256SUMS"
