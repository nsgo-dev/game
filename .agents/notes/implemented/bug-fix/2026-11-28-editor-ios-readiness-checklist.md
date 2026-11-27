# Agent Note: Share real nsgo.yaml iOS readiness checks and valid agent fixes in internal/releaseplan

Status: implemented

## Problem

The scene editor's Build & Release iOS readiness checklist (`pkg/editor/build_release.go`, introduced in [editor build and release screen](../../implemented/feature/2026-11-26-editor-build-release-screen.md)) checked paths that neither `nsgo build` nor `pkg/input` reads:

- Icons looked for `art/icon.png`, `assets/icon.png`, and `icon.png` (lowercase), missing NSGo's `icon.apple` / `icon.png` in `nsgo.yaml` and failing on case-sensitive Linux filesystems for the documented default `assets/Icon.png`.
- Certificates and provisioning profiles looked for `export/ios.cert`, `ios/distribution.cer`, `export/ios.mobileprovision`, and `ios/profile.mobileprovision`, blocking projects properly configured with `sign.profile` and `sign.identity` in `nsgo.yaml`.
- Privacy manifest checks warned whenever root `PrivacyInfo.xcprivacy` was absent, ignoring that NSGo writes `PrivacyInfo.xcprivacy` into every Apple bundle automatically and merges `apple.privacy` and `resources/PrivacyInfo.xcprivacy`. Its agent action wrote an empty `NSPrivacyAccessedAPITypes` plist to the project root, where nothing packages it.
- Touch control checks looked for `input/touch.input.yaml` instead of `*.touch.yaml` layouts (`input.TouchControls`), and its agent action appended an unused `touch_action` `touchRegion` to `game.yaml`.

Because the checks lived inline in `pkg/editor`, `game doctor` (#1672) could not reuse them either.

## Decision

Move iOS readiness evaluation, privacy manifest validation/merging, and touch layout generation into `internal/releaseplan` so `pkg/editor` and future CLI doctor checks share one authoritative implementation aligned with NSGo v0.3.4:

1. **Extend `NSGoSettings` (`internal/releaseplan/plan.go`)**: read `id`, `icon` (`apple`, `png`), `sign` (`identity`, `profile`, `macos_profile`, `keystore`, `key_alias`, `certificate`, `sign_command`, `notarize`), and `apple.privacy` (`accessed_apis`, `collected_data`, `tracking`, `tracking_domains`) from `nsgo.yaml` while leaving unknown NSGo fields untouched.
2. **Shared readiness evaluator (`internal/releaseplan/readiness.go`)**: `EvaluateReadiness("ios", dir, f)` returns ordered `ReadinessItem` checks matching what `nsgo build` actually reads:
   - **Bundle ID**: compares `nsgo.yaml`'s `id` and `game.yaml`'s `id`; flags missing or disagreed `nsgo.yaml` IDs with an agent `Sync nsgo.yaml` action.
   - **App icon**: checks explicit `icon.apple` (`.icon` passes; `.icns` is macOS-only so iOS falls through to PNG), default `assets/AppIcon.icon`, explicit `icon.png`, and default `assets/Icon.png` using exact segment case matching (`exactRelativePath`) so macOS and Linux behave identically, and enforces square PNG dimensions matching NSGo's `readSquareIcon`.
   - **Distribution certificate & provisioning profile**: reads `sign.identity` and `sign.profile` from `nsgo.yaml` (resolving relative paths against the project root as NSGo does); when `sign.profile` exists on disk, `distribution_cert` passes even without explicit `sign.identity` because `nsgo build` selects the matching keychain identity from the profile.
   - **Privacy manifest**: treats the manifest as present (`ReadinessOK`) by default because NSGo generates `PrivacyInfo.xcprivacy` on every Apple build, validates `resources/PrivacyInfo.xcprivacy` and `apple.privacy` (plus their merged result) with NSGo's store-privacy rules (`internal/releaseplan/privacy.go`), flags stray root/legacy `PrivacyInfo.xcprivacy` files with a warning, and lets the agent write a store-valid `resources/PrivacyInfo.xcprivacy` containing NSGo's three required-reason APIs (`NSPrivacyAccessedAPICategoryFileTimestamp`, `NSPrivacyAccessedAPICategorySystemBootTime`, `NSPrivacyAccessedAPICategoryUserDefaults`) via `DefaultPrivacyManifestXML`.
   - **Touch controls**: scans for valid `*.touch.yaml` layouts (`input.ParseTouchLayout`) and `game.yaml` `touchRegion` bindings; when missing, offers `Generate controls.touch.yaml` (`GenerateTouchLayout` + `input.EncodeTouchLayout`) only when `game.yaml` already declares real input actions or axes, and stays purely advisory otherwise so no placeholder `touch_action` is ever invented.
3. **Editor integration (`pkg/editor/build_release.go`)**: `evaluateReadinessFor` delegates to `releaseplan.EvaluateReadiness`, and `RunBuildReadinessAction` performs symlink-safe atomic writes for `nsgo.yaml` (`bundle_id`), `resources/PrivacyInfo.xcprivacy` (`privacy_manifest`), and `controls.touch.yaml` (`touch_controls`).

## Alternatives considered

- **Keep readiness checks inside `pkg/editor` and duplicate them in `cmd/game`**: avoids adding types to `internal/releaseplan`. Rejected because #1672 (`game doctor`) requires the CLI and editor to share the exact same checks, and `internal/releaseplan` is already the shared release contract between both.
- **Drop the privacy manifest agent fix completely**: simpler since NSGo writes a manifest on build. Rejected because authors adding app-specific declarations under `resources/PrivacyInfo.xcprivacy` still benefit from a one-click store-valid template that includes NSGo's required-reason APIs and merges cleanly with `apple.privacy`, and #1667 explicitly accepts writing under `resources/` and testing the merged manifest.
- **Always generate a placeholder touch control even without `game.yaml` actions**: guarantees the agent button always appears. Rejected because inventing an action no game code reads turns the checklist green without making the game playable on touch screens.

## Consequences

- **Benefits**:
  - Projects configured through `nsgo.yaml` (`id`, `icon`, `sign.profile`, `apple.privacy`) and `*.touch.yaml` pass every iOS readiness item and stay queued/included for release builds.
  - Empty or misconfigured projects report actionable details naming the real `nsgo.yaml` keys, and case-sensitive default icon lookup works identically on macOS and Linux.
  - Agent readiness actions write files NSGo and `pkg/input` actually consume (`resources/PrivacyInfo.xcprivacy`, `controls.touch.yaml`, synced `nsgo.yaml`) with symlink path hardening intact.
  - `internal/releaseplan.EvaluateReadiness` and `MergePrivacyManifest` are ready for `game doctor` (#1672) to reuse directly.
- **Costs**:
  - `internal/releaseplan/privacy.go` mirrors NSGo v0.3.4's `NSGoPrivacyReasons` and store-privacy validation rules because NSGo keeps them unexported in `cmd/nsgo`; update `NSGoPrivacyReasons` if a future NSGo release adds another required-reason API category.
  - Default icon checks use `os.ReadDir` per path segment (`exactRelativePath`) to enforce case sensitivity on case-insensitive filesystems, adding minimal directory-read overhead on preflight refreshes.
