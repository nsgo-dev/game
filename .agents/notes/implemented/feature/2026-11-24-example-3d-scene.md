# Agent Note: 3D glTF scene example (Lantern 3D)

Status: implemented

## Problem

Issue #205 (part of #200, "Examples: one for each kind of game") requires a runnable, cross-target 3D glTF scene example with a camera and lights that draws on every target with a GPU view (`macos`, `windows`, `linux`, `android`, `web`) and passes the repository's asset attribution (`TestExamplesUseCreditedAssets`), project schema (`project.Check`), and CI cross-target checks.

Existing examples (`caverun`, `cryptquest`, `slidegem`, `starburst`, `fountain`, `tutorial`) exercise only the 2D `Canvas` stage. A 3D example must prove the end-to-end Stage 2 stack—`game/gltf`, `game/math3d`, `game/scene3d`, `game/render3d`, `game/scene.HUD3D`, and `nsgo/gpu.View`—without breaking WebGL 2 / GLES 3.0 limits or NSGo's software GPU renderer used in headless tests.

## Decision

Add `examples/lantern3d` ("Lantern 3D"), a self-contained interactive glTF 2.0 shrine courtyard diorama registered in `go.work` and `.github/workflows/examples.yml`:

1. **Self-contained deterministic glTF 2.0 asset (`assets/lantern_shrine.glb`)**:
   - Generated deterministically using only the Go standard library by `examples/lantern3d/tools/genassets/main.go` and dedicated under `CC0-1.0` in `examples/lantern3d/CREDITS.md` (mirroring `examples/caverun/tools/genassets` and `examples/caverun/CREDITS.md`).
   - Contains a 15-node hierarchical glTF 2.0 scene (stone courtyard floor, two-tier octagonal dais, tiered lantern shrine with glowing emissive crystal core, four multi-primitive corner lantern bollards, rotated cedar crate, azure crystal cluster, and animated orbiting light wisp), 7 PBR materials, and 2 embedded 64×64 PNG textures (flagstone and cedar wood) decoded via `gltf.Document.DecodeImage`.
2. **Portable forward-lit WGSL pipeline (`renderer.go`)**:
   - Uses 4 bind groups within the WebGPU/WebGL 2 minimum (`maxBindGroups = 4`): `@group(0)` `render3d.CameraBuffer` (`render3d.CameraWGSL`), `@group(1)` `render3d.TextureBindingLayout` + `render3d.BindTexture` (falling back to a 1×1 white `GPUTexture` for untextured materials so one pipeline draws every mesh), `@group(2)` `SceneLights` uniform, and `@group(3)` per-drawable `ObjectUniform` (model matrix, normal matrix, base color, emissive + specular weight).
   - Uses zero compute shaders, zero storage buffers, and zero transcendental builtins (`exp`, `exp2`, `log2`, `pow`—Blinn-Phong specular uses repeated squaring and horizon distance fog uses linear `clamp`), guaranteeing identical execution across WebGPU, Metal, Vulkan, D3D12, WebGL 2, GLES 3.0, and NSGo's software GPU VM.
   - Enables `RetainResources: true` on `gpu.ViewProps` and `Retained: true` on uploaded meshes, textures, samplers, and uniform buffers; `Renderer.Setup` validates handles and safely restores or recreates resources on `OnDeviceRestored` / `OnReady`.
3. **Interactive camera, dynamic lights, and native HUD (`scene.go`, `game.go`)**:
   - Drives a `scene3d.Camera` via `scene3d.OrbitController` with fixed-step damping on `game.Loop`, camera interpolation (`PrevCamera.Lerp`), mouse drag, 1-finger touch drag, 2-finger pinch zoom, mouse wheel zoom, and keyboard shortcuts (`Arrows`/`WASD`, `=`/`-`, `Space`, `L`, `R`, `H`).
   - Composes native NSGo `ui.Button` controls and pass-through stats/hints over the 3D viewport using `scene.HUD3D.Wrap` (see [native HUD over a 3D view](./2026-11-24-native-hud-over-3d-view.md)) and displays a native fallback card when `gpu.View` reports `gpu.ErrUnsupported` (`NoGPU`).
4. **Canonical project configuration (`game.yaml`, `nsgo.yaml`)**:
   - Embeds `game.yaml` parsed via `project.MustParse` and verifies canonical encoding and `nsgo.yaml` parity in `project_test.go`, matching the other showcase examples.

## Bind group layout

| Slot | Owner | Bindings | Update cadence |
| --- | --- | --- | --- |
| `@group(0)` | `render3d.CameraBuffer` | `0`: `Camera` uniform (`view`, `proj`, `view_proj`, `eye`) | Once per frame |
| `@group(1)` | `render3d.BindTexture` | `0`: `texture_2d<f32>`, `1`: `sampler` | Per material (or 1×1 white fallback) |
| `@group(2)` | `Renderer.lightsGroup` | `0`: `SceneLights` uniform (key light, orbiting point light, inner shrine glow, hemisphere ambient, linear fog) | Once per frame |
| `@group(3)` | `gpuDrawable.objGroup` | `0`: `ObjectUniform` (`model`, `normal_mat`, `base_color`, `emissive_spec`) | Per drawable |

## Alternatives considered

### Loading models only from `packages/game/placeholder` without an example-local `.glb`

Strongest argument: reusing `placeholder.Model("crate.glb")` and `placeholder.Model("gem.glb")` adds zero binary asset bytes to `examples/lantern3d` and avoids maintaining a generator.

Rejected because the placeholder models are untextured single-object prototypes (`crate.glb` has no embedded image texture), so the example would neither exercise `gltf.Document.DecodeImage` -> `render3d.UploadTexture` (see [render3d textures and samplers](./2026-11-24-render3d-textures-and-samplers.md)) nor demonstrate a multi-node hierarchical glTF scene graph with multiple materials and embedded textures. Generating a compact ~51 KB `.glb` deterministically with `tools/genassets/main.go` and crediting it as `CC0-1.0` matches `examples/caverun`'s established pattern while demonstrating the full glTF + texture + lighting pipeline.

### Packing all drawable transforms into a dynamic storage buffer or instanced vertex buffer

Strongest argument: a single storage buffer reduces per-drawable bind group updates and scales to thousands of instances.

Rejected because WebGL 2 and OpenGL ES 3.0 (`nsgotest.NoGPUCompute()`) do not support read-only storage buffers in vertex/fragment stages universally, and the example's 22 drawables fit trivially in per-drawable uniform buffers with zero backend-specific code paths.

## Consequences

- **Benefit**: Exercises the complete Stage 2 3D pipeline (glTF loading, embedded PNG texture upload with CPU mipmaps, retained GPU buffers, `CameraBuffer`, `OrbitController`, multi-light portable WGSL, `HUD3D.Wrap` pass-through, and `NoGPU` fallback) in a single concise example.
- **Benefit**: Headless tests verify lit pixel output on both `full` and `nsgotest.NoGPUCompute()` (WebGL 2 / GLES 3.0) profiles, plus camera orbit, HUD pass-through, device loss/restore, and clean scope disposal.
- **Cost**: Adds a 51 KB generated `.glb` binary under `examples/lantern3d/assets/` and a `lantern3d` CI job in `.github/workflows/examples.yml`. Any geometry/material changes must go through `examples/lantern3d/tools/genassets/main.go` so the binary stays reproducible.

## Verification

- `examples/lantern3d/game_test.go` and `examples/lantern3d/project_test.go`:
  - `TestGLBAssetLoadsWithTexturesAndHierarchy` verifies glTF hierarchy, embedded PNG decoding, mesh/triangle counts, and animated point-light stepping.
  - `TestSceneDrawsOnFullAndWebGL2Profiles` verifies lit 3D pixel rendering (sky clear, non-sky geometry coverage, >=12 distinct shaded tones, warm lantern highlights) on both `full` and `nsgotest.NoGPUCompute()` profiles.
  - `TestCameraOrbitAndResetChangeRenderedView` verifies keyboard orbit/zoom measurably changes rendered GPU pixels and `Reset` restores default camera angles/distance.
  - `TestHUDControlsAndOpenAreaDragPassThrough` verifies `ui.Button` clicks do not leak into 3D orbit drags while open-region mouse drag, touch drag, and wheel zoom drive the orbit camera, and `H` toggles HUD visibility.
  - `TestDeviceLossRestoreAndCleanShutdown` verifies `TriggerGPUDeviceLost` / `RestoreGPUDevice` continues rendering lit geometry and scope disposal releases GPU resources cleanly.
  - `TestNoGPUFallbackShowsMessage` verifies `nsgotest.NoGPU()` displays the fallback card while keeping HUD controls interactive.
  - `TestProjectFileIsValidAndCanonical` and `TestRootTitlesTheWindowFromTheProject` verify canonical `game.yaml` / `nsgo.yaml` parity and window title binding.
- Verified with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web` (both `packages/game` and `examples/lantern3d`), `go tool nsgo test ./placeholder ./tooling/credits ./tooling/assetcheck`, `TestFormatPackagesRunConformance|TestFormatsPage|TestStabilityTable`, `go run ./tools/changelog check`, and `bun run verify-notes`.
