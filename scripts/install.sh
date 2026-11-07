#!/usr/bin/env bash
# Build and install MarkToast: CLI + native macOS app bundle.
#
# Optional environment:
#   VERSION             build version (default: 0.0.1-dev)
#   APP_DIR             app destination (default: /Applications/MarkToast.app)
#   CLI_DIR             CLI destination (default: /usr/local/bin)
#   CODESIGN_IDENTITY   signing identity; omit for ad-hoc signing
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: scripts/install.sh only supports macOS" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP="${APP_DIR:-/Applications/MarkToast.app}"
CLI_DIR="${CLI_DIR:-/usr/local/bin}"
VERSION="${VERSION:-0.0.1-dev}"

if [[ -z "$VERSION" ]]; then
  echo "error: VERSION must not be empty" >&2
  exit 2
fi
if [[ ! "$VERSION" =~ ^[A-Za-z0-9._+-]+$ ]]; then
  echo "error: VERSION contains unsupported characters: $VERSION" >&2
  exit 2
fi

# Apple bundle versions must be one to three dot-separated integers. Keep the
# human-facing version for the CLI, and normalize only the bundle fields.
normalize_apple_version() {
  local raw="$1" numeric major minor patch
  raw="${raw#v}"
  raw="${raw#V}"
  raw="${raw%%-*}"
  raw="${raw%%+*}"
  numeric="$(printf '%s' "$raw" | sed -E 's/[^0-9.]+/./g; s/^\.+//; s/\.+$//; s/\.+/./g')"
  if [[ -z "$numeric" ]]; then
    echo "error: VERSION '$1' has no numeric bundle version" >&2
    return 1
  fi
  IFS=. read -r major minor patch _ <<< "$numeric"
  major="${major:-0}"
  if [[ -n "${patch:-}" ]]; then
    printf '%s.%s.%s\n' "$major" "${minor:-0}" "$patch"
  elif [[ -n "${minor:-}" ]]; then
    printf '%s.%s\n' "$major" "$minor"
  else
    printf '%s\n' "$major"
  fi
}

BUNDLE_VERSION="$(normalize_apple_version "$VERSION")"

cd "$ROOT"

# install.sh owns both builds, so `make install` intentionally has no build
# prerequisites and the release script does not invoke make again for packaging.
VERSION="$VERSION" make build desktop

if ! mkdir -p "$CLI_DIR" 2>/dev/null || [[ ! -w "$CLI_DIR" ]]; then
  if [[ "$CLI_DIR" == "/usr/local/bin" ]]; then
    CLI_DIR="$HOME/.local/bin"
    mkdir -p "$CLI_DIR"
    echo "note: /usr/local/bin is not writable; installing the CLI to $CLI_DIR"
  else
    echo "error: CLI directory is not writable: $CLI_DIR" >&2
    exit 1
  fi
fi
# Replace the CLI by rename, not by overwriting its bytes: `marktoast serve`
# may be running from this path (launchd keeps it alive), and rewriting a
# running executable's inode leaves a file macOS then kills on the next exec.
cp bin/marktoast "$CLI_DIR/.marktoast.new.$$"
chmod 755 "$CLI_DIR/.marktoast.new.$$"
mv -f "$CLI_DIR/.marktoast.new.$$" "$CLI_DIR/marktoast"

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp bin/marktoast-desktop "$APP/Contents/MacOS/MarkToast"
cp bin/marktoast "$APP/Contents/MacOS/marktoast-cli"
cp cmd/marktoast-desktop/icon.png "$APP/Contents/Resources/icon.png"
cp LICENSE "$APP/Contents/Resources/LICENSE"
cp docs/CREDITS.md "$APP/Contents/Resources/THIRD_PARTY_NOTICES"

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/marktoast-install.XXXXXX")"
trap 'rm -rf "$WORK_DIR"' EXIT
ICONSET="$WORK_DIR/MarkToast.iconset"
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
  <key>CFBundleVersion</key><string>${BUNDLE_VERSION}</string>
  <key>CFBundleShortVersionString</key><string>${BUNDLE_VERSION}</string>
  <key>CFBundleExecutable</key><string>MarkToast</string>
  <key>CFBundleIconFile</key><string>icon.icns</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleSignature</key><string>????</string>
  <key>LSMinimumSystemVersion</key><string>12.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>LSApplicationCategoryType</key><string>public.app-category.productivity</string>
</dict>
</plist>
PLIST

IDENTITY="${CODESIGN_IDENTITY:--}"
if [[ -z "$IDENTITY" ]]; then
  IDENTITY="-"
fi
if [[ "$IDENTITY" == "-" ]]; then
  echo "→ ad-hoc signing $APP"
  codesign --force --deep --sign - "$APP"
else
  echo "→ signing $APP with $IDENTITY"
  codesign --force --deep --options runtime --timestamp --sign "$IDENTITY" "$APP"
fi
codesign --verify --deep --strict --verbose=2 "$APP"

CLI_VERSION="$("$CLI_DIR/marktoast" version | tr -d '\r\n')"
APP_CLI_VERSION="$("$APP/Contents/MacOS/marktoast-cli" version | tr -d '\r\n')"
PLIST_SHORT_VERSION="$(plutil -extract CFBundleShortVersionString raw -o - "$APP/Contents/Info.plist")"
PLIST_BUILD_VERSION="$(plutil -extract CFBundleVersion raw -o - "$APP/Contents/Info.plist")"
if [[ "$CLI_VERSION" != "$VERSION" || "$APP_CLI_VERSION" != "$VERSION" ]]; then
  echo "error: CLI version mismatch (expected $VERSION, got $CLI_VERSION/$APP_CLI_VERSION)" >&2
  exit 1
fi
if [[ "$PLIST_SHORT_VERSION" != "$BUNDLE_VERSION" || "$PLIST_BUILD_VERSION" != "$BUNDLE_VERSION" ]]; then
  echo "error: Info.plist version mismatch (expected $BUNDLE_VERSION, got $PLIST_SHORT_VERSION/$PLIST_BUILD_VERSION)" >&2
  exit 1
fi

echo "✔ installed $APP"
echo "✔ CLI at $CLI_DIR/marktoast"
echo
echo "open $APP"
