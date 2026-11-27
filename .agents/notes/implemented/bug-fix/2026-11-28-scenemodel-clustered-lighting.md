# Agent Note: Clustered Forward Lighting in scenemodel.Renderer and Scene Light Cap Checking

Status: implemented

## Problem

`render3d.ClusteredLightSystem` ([2026-11-24-render3d-clustered-forward.md](../feature/2026-11-24-render3d-clustered-forward.md)) supports up to 512 visible local point and spot lights on storage-buffer targets and 16 camera-prioritized visible lights on fallback targets without storage buffers (WebGL 2, OpenGL ES 3.0, Linux GtkGLArea, `nsgotest.NoGPUCompute`), but `scenemodel.Renderer` ([2026-11-25-scene3d-lights-environment-camera.md](../feature/2026-11-25-scene3d-lights-environment-camera.md)) never used it. `PrepareScene` called `Built.LightRig()`, which truncated point lights at `render3d.MaxPointLights` (16) and spot lights at `render3d.MaxSpotLights` (8) in document order after shadow casters, silently dropping later lights in both games and the editor's 3D viewport. `game check` (`pkg/tooling/assetcheck`) did not warn when a scene exceeded the fallback cap.

## Decision

- **Share `render3d.ClusteredLightSystem` inside `scenemodel.Renderer`'s `@group(1)` bind group (`pkg/render3d/clustered.go`, `pkg/scenemodel/renderer.go`)**:
  - `render3d.ClusteredLightSystem` exposes `RigBuffer()`, `AppendLocalLayoutEntries(dst, baseBinding)`, `AppendLocalBindGroupEntries(dst, baseBinding)`, and `LocalBindingsWGSL(group, baseBinding)` (plus package-level `ClusteredLocalBindingsWGSL` and `ClusteredFallbackLocalBindingsWGSL`), and adds `shade_pbr_clustered_visibility(..., shadow_vis, point0_vis, spot0_vis, frag_pos)` so shadowed rig slot 0 lights and clustered local lights share one BRDF and one attenuation path.
  - `scenemodel.Renderer` replaces its standalone `lightsBuf` with a `*render3d.ClusteredLightSystem`. Binding 0 of `@group(1)` binds `ClusteredLightSystem.RigBuffer()`; bindings 1–11 stay fog, environment, and directional/spot/point shadow maps; bindings 12–15 (clustered) or binding 12 (fallback) hold the local-light buffers. This stays within the 4-bind-group limit (`MaxBindGroups = 4`) and keeps fragment uniform buffers at 10 (within the 12-UBO WebGL 2 / GLES 3.0 minimum).
  - `Renderer.PrepareScene(cam)` splits visible lights via `Built.SceneLights()`: ambient, up to 4 directional lights (shadow casters first), and at most 1 shadow-casting point light and 1 shadow-casting spot light go into the `LightRig` (matching `ShadowLights()`), while every other visible point and spot light becomes a `render3d.ClusterLight`. `Renderer.Prepare(cam, lights)` continues to upload a custom `LightRig` with zero local lights.
  - `Renderer.SetViewport(width, height)` lets callers (including the editor 3D viewport and probe baker) supply the render target size for cluster tile mapping; otherwise `Prepare`/`PrepareScene` reads `dev.SurfaceSize()`.
  - If default clustered creation hits `render3d.ErrInvalidClusterConfig` on a device with a tiny `MaxStorageBufferBindingSize`, `NewRenderer` falls back automatically to `ForceFallback: true` rather than failing.
- **Distance-prioritized truncation in `Built.LightRig()` (`pkg/scenemodel/environment.go`)**:
  - Added `Built.LightRigNear(cameraPos)` and made `Built.LightRig()` pass the active camera's world position (or the origin when no active camera exists). When a scene fits within `MaxPointLights` / `MaxSpotLights`, document order (shadow casters first) is preserved; when it must cut, slot 0 keeps the first shadow caster and the remaining slots pick the lights nearest the camera.
- **`game check` / Problems panel warning (`pkg/tooling/assetcheck/scene.go`)**:
  - `checkScene` counts visible directional lights and unshadowed local lights (point and spot, excluding at most one shadow-casting point light and one shadow-casting spot light in rig slot 0) and emits a `"warning: "` `Problem` at the file, line, column, and object ID of the first light exceeding `render3d.DefaultFallbackMaxLights` (16) or `render3d.MaxDirectionalLights` (4), naming the dropped count on fallback (and clustered targets when above 512). Scenes that inherit or instance other scenes also check their resolved light counts. The `"warning: "` prefix integrates directly with the editor Problems panel's existing `problemIsWarning` classification.

## Alternatives considered

### Putting clustered light buffers in a fifth bind group (`@group(4)`)

Binding `ClusteredLightSystem.Bind(pass)` directly without adding entry helpers to `render3d` would require moving fog, environment, or shadows to another bind group. All 4 portable bind groups (`CameraGroup`, `LightGroup`, `PBRDrawGroup`, `BaseColorGroup`) are already occupied, and WebGPU/WebGL 2/GLES 3.0 guarantee only 4 bind groups. Appending local-light bindings at binding 12 of `@group(1)` preserves the 4-group layout and reuses `ClusteredLightSystem`'s exact CPU clustering, fallback ranking, validation, and WGSL math.

### Separate `Warnings` slice on `assetcheck.Report`

Splitting warnings out of `Report.Problems` would require changing `game check`, `editor.Problems()`, and all callers expecting `editor.Problems()` to match `assetcheck.Check().Problems`. The editor already distinguishes warnings via the `"warning: "` message prefix (`problemIsWarning`), so reporting a `"warning: "` `Problem` works in both `game check` and the editor's Problems panel without breaking the `assetcheck.Report` contract.

### Putting all point and spot lights into `ClusteredLightSystem`, including shadow casters

Moving slot 0's shadow-casting point and spot lights into `ClusterLight` would require teaching `ClusterLightGPU` and `cluster_light_radiance` about shadow-map visibility and keeping track of which clustered index matches `ShadowLights()`, overlapping with #1387's shadow atlas work. Keeping slot 0's shadow-casting point and spot lights in the rig preserves exact shadow sampling with zero changes to the shadow shaders.

## Consequences

- **Benefits**: Scene files and the editor 3D viewport now draw up to 512 visible local lights on storage-buffer targets and the 16 most important visible lights near the camera on WebGL 2 / OpenGL ES 3.0 / `nsgotest.NoGPUCompute` targets; direct `Built.LightRig()` callers keep nearby lights instead of file-order first N; and `game check` + the editor Problems panel surface excess local light counts with file, line, and dropped count.
- **Costs**: `scenemodel.Renderer` allocates `ClusteredLightSystem`'s cluster grid and storage buffers (about 950 KiB with `DefaultClusterConfig`) on storage-buffer devices even for scenes with few lights, and runs CPU cluster assignment on `PrepareScene` (zero steady-state allocations). Scenes with more than 16 unshadowed local lights emit a `"warning: "` problem in `game check`.

## Testing

- `pkg/scenemodel/clustered_lights_test.go`: 64 point lights in a row rendered through `scenemodel.Renderer` on both the full GPU profile (all 64 lamps lit, including lamps 48..63 near the camera) and `nsgotest.NoGPUCompute` (fallback picks the 16 nearest lamps); `Built.LightRig` / `Built.LightRigNear` nearest-camera truncation and `Built.SceneLights` shadow-slot partitioning.
- `pkg/editor/clustered_lights_gpu_internal_test.go`: editor 3D viewport rendering the 64-lamp corridor scene with parity against `scenemodel.Renderer`.
- `pkg/tooling/assetcheck/lights_test.go` and `internal/gamecli/check_test.go`: `game check` reporting scenes above the 16-light fallback cap (including instanced/inherited scenes and directional > 4) with file, line, and dropped count.
