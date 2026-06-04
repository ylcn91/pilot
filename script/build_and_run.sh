#!/usr/bin/env bash
set -euo pipefail

MODE="${1:-run}"
APP_NAME="Pilot91"
APP_MENU_NAME="Pilot"
DISPLAY_NAME="Pilot - 91"
BUNDLE_ID="com.ylcn91.pilot91"
MIN_SYSTEM_VERSION="14.0"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PACKAGE_DIR="$ROOT_DIR/native-macos/Pilot91"
DIST_DIR="$ROOT_DIR/dist"
APP_BUNDLE="$DIST_DIR/$DISPLAY_NAME.app"
APP_CONTENTS="$APP_BUNDLE/Contents"
APP_MACOS="$APP_CONTENTS/MacOS"
APP_BINARY="$APP_MACOS/$APP_NAME"
INFO_PLIST="$APP_CONTENTS/Info.plist"
APP_RESOURCES="$APP_CONTENTS/Resources"
ICON_SOURCE="$PACKAGE_DIR/Assets/AppIcon.svg"
ICONSET_DIR="$DIST_DIR/AppIcon.iconset"
ICON_FILE="$APP_RESOURCES/AppIcon.icns"

pkill -x "$APP_NAME" >/dev/null 2>&1 || true

swift build --package-path "$PACKAGE_DIR"
BUILD_BINARY="$(swift build --package-path "$PACKAGE_DIR" --show-bin-path)/$APP_NAME"

rm -rf "$APP_BUNDLE" "$ICONSET_DIR"
mkdir -p "$APP_MACOS" "$APP_RESOURCES"
cp "$BUILD_BINARY" "$APP_BINARY"
chmod +x "$APP_BINARY"

if [[ -f "$ICON_SOURCE" ]]; then
  MAGICK_BIN="$(command -v magick || true)"
  if [[ -z "$MAGICK_BIN" ]]; then
    echo "ImageMagick is required to build the app icon." >&2
    exit 1
  fi
  mkdir -p "$ICONSET_DIR"
  for size in 16 32 128 256 512; do
    "$MAGICK_BIN" -background none "$ICON_SOURCE" -resize "${size}x${size}" "PNG32:$ICONSET_DIR/icon_${size}x${size}.png"
    retina=$((size * 2))
    "$MAGICK_BIN" -background none "$ICON_SOURCE" -resize "${retina}x${retina}" "PNG32:$ICONSET_DIR/icon_${size}x${size}@2x.png"
  done
  /usr/bin/iconutil -c icns "$ICONSET_DIR" -o "$ICON_FILE"
fi

cat >"$INFO_PLIST" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key>
  <string>$APP_NAME</string>
  <key>CFBundleIdentifier</key>
  <string>$BUNDLE_ID</string>
  <key>CFBundleName</key>
  <string>$APP_MENU_NAME</string>
  <key>CFBundleDisplayName</key>
  <string>$DISPLAY_NAME</string>
  <key>CFBundleIconFile</key>
  <string>AppIcon</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>LSMinimumSystemVersion</key>
  <string>$MIN_SYSTEM_VERSION</string>
  <key>NSPrincipalClass</key>
  <string>NSApplication</string>
  <key>NSRemovableVolumesUsageDescription</key>
  <string>Pilot reads and runs commands in your selected worktree.</string>
  <key>NSDocumentsFolderUsageDescription</key>
  <string>Pilot reads and runs commands in your selected worktree.</string>
  <key>PilotSourceRoot</key>
  <string>$ROOT_DIR</string>
</dict>
</plist>
PLIST

open_app() {
  /usr/bin/open -n "$APP_BUNDLE"
  sleep 0.2
  /usr/bin/osascript -e "tell application id \"$BUNDLE_ID\" to activate" >/dev/null 2>&1 || true
}

handle_permission_prompt() {
  /usr/bin/osascript <<'APPLESCRIPT' >/dev/null 2>&1 || true
tell application "System Events"
  repeat 12 times
    set didClick to false
    repeat with proc in (processes whose name is "Pilot91" or name is "CoreServicesUIAgent")
      try
        repeat with win in windows of proc
          if exists button "Allow" of win then
            click button "Allow" of win
            set didClick to true
          else if exists button "OK" of win then
            click button "OK" of win
            set didClick to true
          end if
        end repeat
      end try
    end repeat
    if didClick then exit repeat
    delay 0.25
  end repeat
end tell
APPLESCRIPT

  if [[ -n "${PILOT_PERMISSION_PROMPT_CLICK:-}" ]]; then
    CLICLICK_BIN="$(command -v cliclick || true)"
    if [[ -z "$CLICLICK_BIN" ]]; then
      echo "PILOT_PERMISSION_PROMPT_CLICK requires cliclick." >&2
      return 1
    fi
    if [[ ! "$PILOT_PERMISSION_PROMPT_CLICK" =~ ^[0-9]+,[0-9]+$ ]]; then
      echo "PILOT_PERMISSION_PROMPT_CLICK must be x,y." >&2
      return 1
    fi
    "$CLICLICK_BIN" "c:$PILOT_PERMISSION_PROMPT_CLICK" >/dev/null 2>&1 || true
  fi
}

verify_app() {
  [[ -x "$APP_BINARY" ]] || {
    echo "missing app binary: $APP_BINARY" >&2
    return 1
  }

  local actual_bundle_id actual_display_name
  actual_bundle_id="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$INFO_PLIST")"
  actual_display_name="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleDisplayName' "$INFO_PLIST")"
  [[ "$actual_bundle_id" == "$BUNDLE_ID" ]] || {
    echo "bundle id mismatch: $actual_bundle_id" >&2
    return 1
  }
  [[ "$actual_display_name" == "$DISPLAY_NAME" ]] || {
    echo "display name mismatch: $actual_display_name" >&2
    return 1
  }

  pgrep -x "$APP_NAME" >/dev/null || {
    echo "$APP_NAME is not running" >&2
    return 1
  }

  /usr/bin/osascript <<APPLESCRIPT
tell application "System Events"
  set processNames to {"$APP_NAME", "$DISPLAY_NAME"}
  repeat 24 times
    repeat with processName in processNames
      if exists process (processName as text) then
        tell process (processName as text)
          if (count of windows) > 0 then
            set windowSize to size of window 1
            if item 1 of windowSize < 900 then error "Pilot 91 window is too narrow"
            if item 2 of windowSize < 600 then error "Pilot 91 window is too short"
            return
          end if
          if visible is true then
            return
          end if
        end tell
      end if
    end repeat
    delay 0.25
  end repeat
  error "Pilot 91 has no visible window"
end tell
APPLESCRIPT
}

case "$MODE" in
  run)
    open_app
    handle_permission_prompt
    ;;
  --debug|debug)
    lldb -- "$APP_BINARY"
    ;;
  --logs|logs)
    open_app
    handle_permission_prompt
    /usr/bin/log stream --info --style compact --predicate "process == \"$APP_NAME\""
    ;;
  --telemetry|telemetry)
    open_app
    handle_permission_prompt
    /usr/bin/log stream --info --style compact --predicate "subsystem == \"$BUNDLE_ID\""
    ;;
  --verify|verify)
    open_app
    handle_permission_prompt
    sleep 1
    verify_app
    ;;
  *)
    echo "usage: $0 [run|--debug|--logs|--telemetry|--verify]" >&2
    exit 2
    ;;
esac
