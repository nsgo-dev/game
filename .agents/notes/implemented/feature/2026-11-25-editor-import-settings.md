# Agent Note: Editor asset import settings inspector and live asset rebuilding

Status: implemented

## Problem

Import settings and asset credits live in `*.import.yaml` sidecar files read by `game assets`, `game tex`, `game audio`, and `game credits`, but authors had to create and edit those files by hand and restart the editor to see rebuilt atlas frames or textures. Meanwhile, `InspectorView` only displayed scene objects and `AssetBrowserView` only read `Editor.Assets()` once on initial mount instead of reacting to reloaded atlases.

## Decision

Add asset import inspection/editing (`asset_import.go`, `asset_import_ui.go`) and asynchronous asset building with live reloading (`asset_build.go`) to `packages/game/editor`:

- **Inspector switching without clobbering scene selection**: `Editor.inspectAssetFile` tracks whether the most recent selection was an importable asset file in `FilesView` (`.png`/`.jpg`/`.jpeg`/`.webp`, `.wav`/`.ogg`/`.mp3`/`.flac`, `.ttf`/`.otf`, `.gltf`/`.glb`, excluding `*.import.yaml` sidecars and `project.Assets.IsOutput` generated files). Selecting an importable asset switches `InspectorView` to `assetImportInspectorView`, while calling `SelectObject` (even for the already-selected object ID) switches back to the scene object inspector.
- **Target inheritance, validated sidecar writes, and exact Undo/Redo**: `InspectAssetImport` resolves defaults from the matching `game.yaml` `assets` target (`AtlasTarget`, `TextureTarget`, `AudioTarget`, `FontTarget`, `ModelTarget`) and overlays the parsed sidecar. `EditAssetImportSettings` and its typed setters (`SetImageImport*`, `SetAudioImport*`, `SetFontImport*`, `SetModelImport*`, `SetAssetImportCredit`, `ResetAssetImportField`) prune empty sections, validate and encode via `importsettings.Encode`, and execute a `fallibleCommand` (`importSettingsCmd`) that writes atomically or removes an empty sidecar. `Undo` restores the exact previous sidecar bytes verbatim (preserving hand-authored comments) or removes a sidecar created by the edit.
- **Asynchronous `go tool game assets` streaming into `OutputPanelView`**: `ReimportAsset` (inspector **Reimport** button) and `BuildAssets` / `BuildAssetsWith` (toolbar **Build Assets** button) run `go tool game assets` off the UI goroutine through `Editor.AssetsCommandRunner` (defaulting to `os/exec` on desktop targets, guarded by `canRunGameDesktop` so mobile/web targets compile cleanly). Stdout and stderr stream into the existing Output panel from [editor-run-log-panel](2026-11-25-editor-run-log-panel.md).
- **Live atlas and texture reloading**: After a successful build, `ReloadAssets` hashes `game.yaml`'s declared atlas and texture outputs, reloads changed atlases via `sprite.LoadAtlas` with fresh PNG bytes and a monotonically incremented `core.ImageSource.Version`, updates `sceneformat.AtlasMap` or existing `*sprite.Atlas` pointers in place, notifies any `AssetReloader` / `OnReloadAssets` hook, rebuilds `Editor.Built` for the viewport, refreshes the tile palette, and re-runs `CheckProblems`. `AssetBrowserView` now reads `Editor.Assets()` reactively through `core.NewMemoFunc` + `core.Dynamic` so changed or newly sliced frames appear immediately without restarting the editor.

## Alternatives considered

- **Separate docked Import panel instead of reusing `InspectorView`** — Keeps the inspector solely for scene objects. Rejected because issue #915 explicitly specifies showing an asset's import settings in the inspector when selected in the file browser, matching Unity's import inspector and avoiding another bottom dock competing for viewport height.
- **Replacing `Editor.Resolver` wholesale on reload instead of in-place atlas updates** — Simpler when the resolver is a plain `AtlasMap`. Rejected because games and custom editor commands wrap `AtlasMap` inside composite resolvers (implementing `SceneResolver` or `MaterialResolver`); updating existing `*sprite.Atlas` values in place plus offering `AssetReloader` works for both plain maps and custom resolvers.
- **Buffering sidecar edits in memory until clicking Save** — Matches `EditProject` / `SaveProject`. Rejected because `game assets` reads sidecars directly from disk, and issue #915 specifies that editing writes the sidecar through `importsettings.Encode` as an undoable step whose Undo restores the previous file.

## Consequences

- **Benefits**:
  - Authors can configure image, audio, font, model, and credit sidecars with live `game.yaml` target defaults, per-field Reset, strict SPDX/schema validation, and full Undo/Redo without hand-editing YAML.
  - Reimporting an asset or building project assets streams logs into the Output panel and updates the Asset Browser, Viewport, and Problems panel immediately without restarting the editor.
- **Costs and known limits**:
  - `AssetImportKind` skips files reported by `project.Assets.IsOutput` so generated outputs (such as packed atlas PNGs or encoded `.glb` files) do not accidentally receive sidecars.
  - Non-desktop targets without an injected `AssetsCommandRunner` return `ErrAssetsBuildUnsupported` instead of spawning subprocesses.

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo test ./editor/...)`
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestFormatPackagesRunConformance|TestStabilityTable|TestAgentSkillValid')`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
