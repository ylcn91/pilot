#!/usr/bin/env bash
#
# Assemble Pilot91.app from the SwiftPM executable product.
#
# SwiftPM only produces a bare CLI binary; this wraps it into a proper macOS
# .app bundle (Info.plist, MacOS/, Resources/) so it launches as an app.
# Signing/notarization are intentionally out of scope: the bundle is ad-hoc
# signed only so Gatekeeper will run it locally. A real Developer ID pass is
# a separate step gated on credentials.
#
# Env overrides:
#   CONFIG   build configuration (default: release)
#   VERSION  marketing/build version written into Info.plist (default: 0.0.0)
#   OUT_DIR  output directory for the .app (default: <package>/dist)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG="${CONFIG:-release}"
VERSION="${VERSION:-0.0.0}"
OUT_DIR="${OUT_DIR:-$SCRIPT_DIR/dist}"
APP="$OUT_DIR/Pilot91.app"

echo "==> Building Pilot91 ($CONFIG)"
swift build --package-path "$SCRIPT_DIR" -c "$CONFIG" --product Pilot91
BIN_PATH="$(swift build --package-path "$SCRIPT_DIR" -c "$CONFIG" --show-bin-path)/Pilot91"
if [ ! -x "$BIN_PATH" ]; then
    echo "error: built executable not found at $BIN_PATH" >&2
    exit 1
fi

echo "==> Assembling $APP"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$BIN_PATH" "$APP/Contents/MacOS/Pilot91"
cp "$SCRIPT_DIR/Resources/Info.plist" "$APP/Contents/Info.plist"
printf 'APPL????' > "$APP/Contents/PkgInfo"

# Stamp the requested version into the bundle plist.
if command -v /usr/libexec/PlistBuddy >/dev/null 2>&1; then
    /usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $VERSION" "$APP/Contents/Info.plist"
    /usr/libexec/PlistBuddy -c "Set :CFBundleVersion $VERSION" "$APP/Contents/Info.plist"
fi

# Best-effort app icon: rasterize the SVG into an .icns when the tooling is
# present. Skipped silently otherwise — the app still launches without it.
make_icns() {
    local svg="$SCRIPT_DIR/Assets/AppIcon.svg"
    [ -f "$svg" ] || return 0
    command -v rsvg-convert >/dev/null 2>&1 || return 0
    command -v iconutil >/dev/null 2>&1 || return 0

    local iconset
    iconset="$(mktemp -d)/Pilot91.iconset"
    mkdir -p "$iconset"
    local size
    for size in 16 32 128 256 512; do
        rsvg-convert -w "$size" -h "$size" "$svg" -o "$iconset/icon_${size}x${size}.png"
        rsvg-convert -w "$((size * 2))" -h "$((size * 2))" "$svg" -o "$iconset/icon_${size}x${size}@2x.png"
    done
    iconutil -c icns "$iconset" -o "$APP/Contents/Resources/Pilot91.icns"
    echo "==> Embedded app icon"
}
make_icns || echo "==> Skipped icon (tooling unavailable)"

# Ad-hoc sign so Gatekeeper runs the local build. Not a distribution signature.
if command -v codesign >/dev/null 2>&1; then
    codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || \
        echo "==> Ad-hoc codesign skipped"
fi

echo "==> Done: $APP"
