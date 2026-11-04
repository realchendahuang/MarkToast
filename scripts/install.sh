#!/usr/bin/env bash
# Build and install MarkToast: CLI + native macOS app bundle.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP="${APP_DIR:-/Applications/MarkToast.app}"
CLI_DIR="${CLI_DIR:-/usr/local/bin}"

cd "$ROOT"
make build desktop

if ! mkdir -p "$CLI_DIR" 2>/dev/null || [ ! -w "$CLI_DIR" ]; then
  CLI_DIR="$HOME/.local/bin"
  mkdir -p "$CLI_DIR"
  echo "note: /usr/local/bin is not writable; installing the CLI to $CLI_DIR"
fi
cp bin/marktoast "$CLI_DIR/marktoast"
chmod 755 "$CLI_DIR/marktoast"

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp bin/marktoast-desktop "$APP/Contents/MacOS/MarkToast"
cp bin/marktoast "$APP/Contents/MacOS/marktoast-cli"
cp -R frontend "$APP/Contents/MacOS/frontend"
cp cmd/marktoast-desktop/icon.png "$APP/Contents/Resources/icon.png"

ICONSET="$(mktemp -d)/MarkToast.iconset"
mkdir -p "$ICONSET"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" cmd/marktoast-desktop/icon.png --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
  double=$((size * 2))
  sips -z "$double" "$double" cmd/marktoast-desktop/icon.png --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/icon.icns"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>MarkToast</string>
  <key>CFBundleDisplayName</key><string>MarkToast</string>
  <key>CFBundleIdentifier</key><string>com.realchendahuang.marktoast</string>
  <key>CFBundleVersion</key><string>0.0.1</string>
  <key>CFBundleShortVersionString</key><string>0.0.1</string>
  <key>CFBundleExecutable</key><string>MarkToast</string>
  <key>CFBundleIconFile</key><string>icon.icns</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleSignature</key><string>????</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>LSApplicationCategoryType</key><string>public.app-category.productivity</string>
</dict>
</plist>
PLIST

IDENTITY="${CODESIGN_IDENTITY:--}"
codesign --force --deep --sign "$IDENTITY" "$APP" >/dev/null 2>&1 || true
echo "✔ installed $APP"
echo "✔ CLI at $CLI_DIR/marktoast"
echo
echo "open $APP"
