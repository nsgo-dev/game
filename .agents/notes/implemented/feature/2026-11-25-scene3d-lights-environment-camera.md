# Agent Note: 3D lights, environment, and 3D cameras in scene files and the 3D viewport

Status: implemented

## Problem

Scene files (`*.scene.yaml`) could place 3D meshes and glTF models (`scenemodel`, #889), and the editor's 3D viewport could render them on a GPU view (#890), but scenes could not declare lights, sky/IBL, fog, tonemapping, exposure, or a starting 3D camera. Both games and the editor had to fall back to hardcoded Go lighting rigs and cameras, making it impossible to author a 3D scene's look in plain text or preview the game's first frame inside the editor.

## Decision

- **Scene format version 4 (`sceneformat/scene3d.go`, `scene.v4.json`)**:
  - Added optional `environment` on `SceneDoc` (`sky` with `color`, `gradient`, or `hdr` path; `ambientColor`/`ambientIntensity`/`iblIntensity`; distance & height `fog`; `tonemapping` `none|reinhard|aces|neutral|agx`; and `exposure`).
  - Added optional `light` (`directional`, `point`, `spot`, with CSS `color`, `intensity`, `range`, `innerCone`/`outerCone` in degrees, and `shadows`) and `camera` (`perspective` or `orthographic`, `fov` in degrees or `orthoHeight`, `near`, `far`, `current`) on `ObjectDoc` and `ObjectOverride`.
  - Enforced strict version gating (`>= 4` for all new fields), single `current: true` 3D camera per scene, finite/range bounds, and canonical deterministic YAML round-tripping. Regenerated `apps/website/public/schemas/scene.v4.json` alongside `v1`/`v2`/`v3`.
  - Wired `environment.sky.hdr` into `refs.Scan` (`FieldSkyHDR`) for move/rename rewriting and into `tooling/assetcheck` (`game check`) to verify the file exists and decodes via `render3d.DecodeHDR`.
- **Unified `scenemodel` 3D build and `Renderer` (`scenemodel/environment.go`, `scenemodel/renderer.go`)**:
  - `scenemodel.Build` populates `Built.Lights`, `Built.Cameras`, and `Built.Environment` (generating gradient/HDR sky cubemaps and prefiltered IBL + SH irradiance on demand via `HDRResolver`).
  - `scenemodel.Renderer` packs scene lighting, fog, environment params, directional/spot/point shadow bindings, and `render3d.ClusteredLightSystem` local-light bindings into `@group(1)` (`render3d.LightGroup`, bindings 0–15 in clustered mode or 0–12 in fallback mode; see [2026-11-28-scenemodel-clustered-lighting.md](../bug-fix/2026-11-28-scenemodel-clustered-lighting.md)) so the total stays within the 4-bind-group limit across Metal, WebGPU, Vulkan, D3D11, GLES3, iOS, and NSGo's software GPU. It uses portable WGSL tonemapping and fog helpers without `exp`/`log2` builtins (matching `render3d`'s workaround for `nsgo-dev/nsgo#2334`).
  - Added `PrepareScene(cam)`, `RecordShadows(target)`, `DrawSky(pass)`, and `ClearColor(fallback)` while keeping `Prepare(cam, lights)` fully backward-compatible for scenes without scene lighting.
- **Editor 3D viewport parity & authoring (`editor/scene3d.go`, `editor/viewport3d_gpu.go`, `editor/scene3d_edit.go`, `editor/handles3d.go`, `editor/environment_panel.go`)**:
  - When `Editor.Lights3D == nil` and `built.HasSceneLighting()` is true, `viewport3DRenderer` invokes the exact same `PrepareScene`, `RecordShadows`, `ClearColor`, `DrawSky`, and `Draw` path as a game. Scenes without lights or environment keep `DefaultLights3D()` + the editor's default gradient sky.
  - Non-structural light and camera fields (color, intensity, range, cone angles, FOV, near/far) sync in place in `sync3D` so dragging handles never rebuilds meshes or GPU buffers; structural changes (`doc.Environment`, light kind/shadows, camera presence) hash in `lookKey3D`.
  - Added light and camera 2D overlay icons (pickable in `Pick3D`), interactive undoable handles (`Handle3DLightDirection`, `Handle3DLightRange`, `Handle3DLightOuterCone`, `Handle3DLightInnerCone`, `Handle3DCameraFOV`, `Handle3DCameraFar`), the docked `EnvironmentPanelView`, `LIGHT 3D`/`CAMERA 3D` inspector sections, atomic `current` camera switching, and the **Look Through Scene Camera** toggle (`CmdView3DCamera`, shortcut `0`).

## Alternatives considered

- **Separate bind groups for fog, IBL, and each shadow map**: simpler binding layouts, but exceeds the portable 4-bind-group limit (`@group(0)` camera, `@group(1)` lights, `@group(2)` draw, `@group(3)` material textures already occupy all 4 groups). Packing scene-wide lighting/environment/IBL/shadow bindings into `@group(1)` keeps all 4 groups intact without changing per-draw or per-material layouts.
- **Radiance-space radians for YAML `fov`, `innerCone`, `outerCone`**: consistent with `rotX`/`rotY`/`rotZ`, but authoring camera FOV (`60`) and spot cone angles (`18` / `30`) in degrees is vastly more readable and reviewable in YAML text files; `scenemodel` converts via `math3d.DegToRad` at build/rig time.
- **Rebuilding the 3D scene on every handle drag tick**: simpler than syncing `Built.Lights`/`Built.Cameras` in `sync3D`, but would re-upload meshes and shadow maps on every mouse move while dragging a light's range or cone handle.

## Consequences

- Benefits: 3D scenes are self-contained plain-text YAML files; games and the editor share one rendering path with pixel-exact parity; light/camera handles and environment edits integrate cleanly with `Editor.History` Undo/Redo and scene version upgrades.
- Costs: `scenemodel.Renderer`'s `@group(1)` bind group holds dummy 1x1 depth/cubemap textures when IBL or shadow kinds are inactive, and `scenemodel.Build` computes a small (16x16, 4 mip) IBL prefilter on the CPU when IBL intensity is positive.
- Related notes: extends [2026-11-25-editor-3d-viewport-on-gpu.md](2026-11-25-editor-3d-viewport-on-gpu.md) and [2026-11-25-scene-objects-draw-gltf-models.md](2026-11-25-scene-objects-draw-gltf-models.md).

## Testing

- `packages/game/sceneformat/scene3d_test.go` & `conformance_test.go`: round-trip, validation, version gating (`v1`/`v2`/`v3` reject v4 fields), single-current-camera rule, and overrides.
- `packages/game/cmd/gameschema/main_test.go`: `TestSceneSchemaV4Shape` and `-check` against `apps/website/public/schemas/scene.v4.json`.
- `packages/game/refs/sky_hdr_test.go` & `packages/game/tooling/assetcheck/sky_hdr_test.go`: HDR sky path scanning, move rewriting, and missing/corrupt HDR validation.
- `packages/game/scenemodel/environment_test.go`: builds and renders directional+shadows, point, spot, gradient sky, HDR sky + IBL, fog, tonemapping, and active 3D cameras on `gpu.NewSoftwareRenderer`.
- `packages/game/editor/lighting3d_gpu_internal_test.go`: proves pixel-for-pixel parity between the editor 3D viewport (looking through the scene camera) and `scenemodel`/`render3d`'s game first frame (including HDR sky and golden PNG `testdata/golden/viewport3d_scene_lighting.png`), plus Undo/Redo pixel restoration.
- `packages/game/editor/lighting3d_test.go`: proves all light/camera handles edit values as single undoable steps, icons pick cleanly, shortcut `0` toggles look-through-camera, and `EnvironmentPanelView` + inspector controls mount in `nsgotest`.
