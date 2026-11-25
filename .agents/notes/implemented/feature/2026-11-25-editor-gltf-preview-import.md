# Agent Note: Editor glTF/GLB model preview and animation playback in the import inspector

Status: implemented

## Problem

Selecting a `.gltf` or `.glb` file in the editor's file browser showed its `model` and `credit` sidecar fields via [editor-import-settings](2026-11-25-editor-import-settings.md), but authors had no way to inspect the model's visual appearance, scale, node hierarchy, meshes, materials, bounds, triangle counts, or animations inside the editor without running the game.

## Decision

Extend the asset import inspector (`asset_import_model.go`, `asset_import_model_ui.go`, wired into `modelImportFields` in `asset_import_ui.go`) with an interactive 3D glTF/GLB preview:

- **Offscreen software GPU preview on `core.Canvas`**: `RenderModelPreview` renders the selected `.gltf`/`.glb` through a cached `gpu.SoftwareRenderer` and `scenemodel.Renderer` lit by `DefaultLights3D()`, then `DrawModelPreview` blits the resulting `*image.RGBA` (with a monotonically bumped `core.ImageSource.Version`) and a corner XYZ orientation triad onto a `core.Canvas` inside `InspectorView`. Reusing the software renderer and scene across orbit drags and node-TRS animation steps avoids re-uploading static meshes and textures while remaining portable across desktop, mobile, web, and headless `nsgotest` suites without embedding native GPU subviews inside a `ui.ScrollView`.
- **Direct glTF-to-`scenemodel.Built` preview scene with full animation support**: `loadModelPreview` loads the file via `gltf.LoadFS` (after `verifyFileWithinRoot`), constructs an `anim3d.Player`, and builds a `scenemodel.Built` with a 1-to-1 `sceneNodes` slice indexed by glTF node index under a `scaleRoot` node driven by `EffectiveModelScale()`. Node TRS animations update `sceneNodes` in place via `player.ApplyToNodes`; skinned meshes (`render3d.SkinVertex` with `player.SkinMatricesForNode`) and morph-animated meshes (`gltf.EvaluateMorphPositions` / `EvaluateMorphNormals`) re-evaluate deformed vertices only when `poseRev` advances, not during paused camera orbits.
- **Scale-adaptive orbit camera and playback lifecycle**: `frameModelPreviewCamera` and `ZoomModelPreview` clamp camera distance and clip planes relative to the model's scaled bounding radius (down to `1e-4`), so models with small import scales such as `0.01` stay inspectable without hitting the scene viewport's `0.5` minimum distance. `PlayModelPreviewAnimation` drives a `game.Loop` when `Editor.scope` is attached and stops/releases GPU resources on file change, `Play`, `OpenProject`, `OpenDemo`, `AttachScope(nil)`, and `Close`.
- **Graceful fallback for invalid models and zero toolbar growth**: When a selected `.gltf`/`.glb` fails to parse, the preview renders a non-blocking error banner (`import-model-preview-error`) while keeping all sidecar `model` and `credit` controls functional. No buttons are added to the 2D `editor-toolbar`.

## Alternatives considered

- **Embedding a live `gpu.View` inside `InspectorView`'s `ui.ScrollView`** — Would use hardware rasterization directly when available. Rejected because native GPU subviews inside scrollable inspector panels complicate clipping across desktop/mobile backends and do not render in headless `nsgotest` environments, whereas `gpu.SoftwareRenderer` + `core.Canvas` already powers `ModelThumbnail` and works identically everywhere.
- **Using `scenemodel.Build` on a synthetic `SceneDoc`** — Reuses scene loading directly. Rejected because `scenemodel.Build` omits skins and does not expose a glTF-node-index-to-`scene3d.Node` table needed for `anim3d.Player.ApplyToNodes` and per-frame morph/skin updates.
- **Adding a separate bottom dock or toolbar button for Model Preview** — Gives more horizontal width. Rejected to keep the 2D toolbar compact (<= 2 rows) and to follow the established import-inspector pattern in [editor-import-settings](2026-11-25-editor-import-settings.md) and [editor-spritesheet-slice-pivot](2026-11-25-editor-spritesheet-slice-pivot.md), where selecting an asset in the file browser immediately previews and edits its sidecar in the Inspector.

## Consequences

- **Benefits**:
  - Selecting any `.gltf` or `.glb` in the file browser immediately previews the rendered model with orbit/pan/zoom/reset controls, displays unscaled and scaled bounds, triangle/vertex counts, nodes, meshes, materials, and animations, and plays/pauses/stops/scrubs every animation (including node TRS, skeletal skins, and morph targets).
  - Editing `scale` updates the preview and scaled bounds immediately, writes the sidecar deterministically via `importsettings.Encode`, and causes the next `game assets` run to rebuild only that model target.
- **Costs and known limits**:
  - Software rasterization caps preview resolution at `320 px` on the longest side to keep interactive orbiting and animation playback responsive on CPU.
  - Skinned or morph-animated models re-upload their deformed `scenemodel.Renderer` meshes when animation time advances (not on camera orbit while paused).

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo check ./editor && env -u GOROOT go tool nsgo check -target ios ./editor)`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestFormatsPage|TestStabilityTable|TestAgentSkillValid')`
- `(cd packages/game && env -u GOROOT go run ./cmd/gameschema -check)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
