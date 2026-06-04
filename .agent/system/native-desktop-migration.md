# Native macOS app: the primary desktop surface

**Status:** active migration. Native (Pilot 91, SwiftUI) is now the primary
desktop client; the Wails desktop app is retired.

## What changed

- `native-macos/Pilot91/` (SwiftUI / SwiftPM) is the product desktop app.
- The Wails desktop **app shell** (`desktop/main.go`, `desktop/app*.go`,
  `desktop/types.go`, `desktop/wails.json`, `desktop/build/`) is removed,
  along with the `wails build` Makefile targets and the `wailsapp/wails/v2`
  dependency.
- `desktop/frontend/` is **retained**: it doubles as the source for the
  gateway's embedded web dashboard (`make build-with-dashboard` →
  `cmd/pilot/dashboard_dist`, served at `/dashboard/` behind the
  `embed_dashboard` build tag). It is no longer a Wails frontend; the Wails
  runtime branch (`wailsjs.ts`, `isWails`) is now dead in browser-only mode
  and can be pruned in a follow-up.

## Build & release

| Task | Command |
|------|---------|
| Build | `make native-build` (`swift build -c release`) |
| Test | `make native-test` (`swift test`) |
| Bundle `.app` | `make native-bundle` → `native-macos/Pilot91/dist/Pilot91.app` |
| Package zip | `make native-package VERSION=X.Y.Z` → `bin/Pilot91-macOS-X.Y.Z.zip` |

`scripts/bundle-app.sh` wraps the SwiftPM executable into a `.app` bundle
(Info.plist + ad-hoc signature + best-effort `.icns` from `Assets/AppIcon.svg`).
CI lives in `.github/workflows/native-macos.yml`: build + test on every
native change; bundle + upload artifact on `v*` tags.

## Out of scope (follow-ups)

- **Code signing / notarization.** The bundle is ad-hoc signed for local
  runs only. A distributable build needs a Developer ID Application
  certificate, team ID, and `notarytool` credentials wired into CI secrets.
- **`.dmg` installer.** Only a `.zip` is produced today.
- **Frontend cleanup.** Prune the dead Wails runtime branch from
  `desktop/frontend/src` (and consider relocating it out of `desktop/`,
  e.g. `dashboard-web/`, since it is now web-only).
- **Sparkle auto-update.** Not wired.
