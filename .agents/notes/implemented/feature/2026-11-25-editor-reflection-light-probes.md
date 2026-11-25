# Agent Note: Editor reflection probes, light probe grids, and offline baking

Status: implemented

## Problem

Issue #903 requires authoring and baking 3D reflection probes and light probe grids in the scene editor so the game's runtime shading matches the editor's 3D viewport pixel for pixel. Previously `render3d` supported runtime-only reflection probes and light probe grids ([render3d probes IBL](2026-11-25-render3d-probes-ibl.md)), but `sceneformat`, `scenemodel`, `editor`, `refs`, and `tooling/assetcheck` had no representation, on-disk baked file formats, bounds handles, or offline baking workflow for them.

## Decision

- **Scene format (`sceneformat` v4, `scene.v4.json`)**: Add `ReflectionProbe *ReflectionProbeDoc` (`center`, `size`, `blendDistance`, `intensity`, `priority`, `boxProjection`, `resolution`, `cubemap`) and `LightProbeGrid *LightProbeGridDoc` (`center`, `size`, `counts`, `intensity`, `data`) on `ObjectDoc` and `ObjectOverride` in version 4 scenes, keeping version 1–3 scenes unchanged and covered by `merge.go`, `resolve.go`, and `cmd/gameschema`.
- **On-disk baked formats (`render3d/probe_io.go`)**:
  - **Reflection probe cubemaps as 6×1 Radiance `.hdr` horizontal strips** (`EncodeCubemapHDR` / `DecodeCubemapHDR` / `CubemapFromHDRImage`): stores all six faces (`+X, -X, +Y, -Y, +Z, -Z`) side by side in RGBE format, preserving HDR radiance > 1.0 with lossless face-texel round-trip and zero equirectangular projection blur, while still accepting externally authored 2:1 equirectangular `.hdr` images.
  - **Light probe grids as versioned canonical YAML (`*.probes.yaml`, `LightProbeGridVersion = 1`, `lightprobes.v1.json`)**: stores `version`, `bounds` (`min`, `max`), `counts`, and deterministic fixed-precision `probes` (`[[r,g,b] × 9]` L2 SH bands) with strict unknown-field rejection and round-trip idempotency verified via `formattest.Run`.
- **Unified shading and baking (`scenemodel`)**:
  - `scenemodel.Build` loads referenced `.hdr` cubemaps (prefiltering roughness mips via `render3d.PrefilterEnvironmentHDR`) and `.probes.yaml` grids via `HDRResolver` / `LightProbeGridResolver` (`scenemodel.Assets`).
  - `scenemodel.Renderer` binds per-object `ProbeIBLUniform` and probe/sky cubemaps at `@group(2)` bindings `1..5` (alongside `pbr_draw` at binding `0`) via `render3d.ProbeIBLWGSLForGroup`, keeping the 4-bind-group portability ceiling intact while combining shadows/fog at `@group(3)` with local reflection probes, light probe SH irradiance, and global sky IBL in the same pass.
  - `scenemodel.BakeReflectionProbe` and `scenemodel.BakeLightProbeGrid` render the scene with `scenemodel.Renderer` (`LinearOutput: true`, `DisableProbes: true` to prevent feedback) on an offscreen `gpu.SoftwareRenderer` across all 6 cubemap faces using `render3d.ProbeFaceViewProj`.
- **Editor authoring, 3D bounds handles, and baking (`editor`)**:
  - `AddReflectionProbe3D`, `AddLightProbeGrid3D`, `SetObjectReflectionProbe`, `SetObjectLightProbeGrid`, `BakeReflectionProbe3D`, `BakeLightProbeGrid3D`, and `BakeAllProbes3D`, plus Inspector sections, Scene Environment panel "Bake Probes" button (leaving the 2D toolbar untouched), and command palette actions.
  - Six draggable face-center bounds handles (`Handle3DProbeBoundsPosX`..`Handle3DProbeBoundsNegZ`) resize bounds while anchoring the opposite face, syncing live in `sync3D` without cubemap reloads, committing as one undoable History step (`EndHandleDrag3D`), and cancelling cleanly on Escape (`CancelHandleDrag3D`).
  - Baked outputs validate against path traversal, symlinks, and reserved project filenames, write atomically, link back onto the probe undoably, and invalidate editor asset caches so the 3D viewport updates immediately.
- **Project integrity (`refs`, `tooling/assetcheck`)**: Scan and validate `reflectionProbe.cubemap` (`DecodeCubemapHDR`) and `lightProbeGrid.data` (`ParseLightProbeGrid`) across objects and overrides.

### Invariants

- Baked `.probes.yaml` files obey the repository's plain-text canonical format contract (versioned, strict unknown-field refusal, deterministic output, lossless load/save round trip, registered in `formats_test.go` and `cmd/gameschema`).
- Baking never feeds a probe's previous baked output back into itself (`RendererOptions.DisableProbes = true`).
- Game shading (`scenemodel.Build` + `scenemodel.NewRenderer`) and editor 3D viewport shading share the exact same `scenemodel.Renderer` path and produce pixel-identical output for the same scene and camera.

## Alternatives considered

### KTX2 cubemaps for baked reflection probes

Writing reflection probe cubemaps as `.ktx2` files was considered. Rejected because `packages/game/tooling/ktx2/container.go` currently enforces `FaceCount == 1` (2D textures only) and lacks cubemap container support, whereas `render3d` already provides complete pure-Go Radiance `.hdr` RGBE encode/decode and `HDRCubemap` prefiltering that integrates directly with `sceneformat.IsHDRPath` and `scenemodel.HDRResolver`.

### Equirectangular 2:1 `.hdr` for baked reflection cubemaps

Projecting the 6 baked cubemap faces into a 2:1 equirectangular `.hdr` on save and re-sampling back to a cubemap on load was considered. Rejected because double resampling blurs sharp specular reflections and introduces pole distortion; a 6×1 horizontal face strip preserves every baked face texel losslessly while `CubemapFromHDRImage` still falls back to equirectangular decoding for externally authored `.hdr` files.

### Separate bind group (`@group(4)`) for probe IBL in `scenemodel.Renderer`

Adding a fifth bind group for probe IBL would avoid touching `@group(2)` in `scenemodel/renderer.go`. Rejected because WebGPU / WebGL 2 portable pipelines only guarantee 4 bind groups (`0..3`); placing per-object probe bindings alongside `pbr_draw` in `@group(2)` keeps all backends portable and lets shadows and fog remain on `@group(3)`.

## Consequences

- **Benefit:** Authors can place reflection probes and light probe grids, adjust bounds interactively with undoable 3D handles, bake deterministic `.hdr` and `.probes.yaml` files on disk, and preview exact runtime shading in the editor viewport.
- **Benefit:** Moving scene geometry (such as a colored wall) and rebaking deterministically updates both baked files and live shading, while `refs` and `assetcheck` catch missing or corrupt baked probe assets in CI.
- **Cost:** Offline software-GPU baking renders 6 faces per reflection probe and 6 low-res (`16×16`) faces per light-probe sample point; dense grids take proportionally longer to bake on CPU.
- **Cost:** Each drawable part in `scenemodel.Renderer` holds a small `ProbeIBLUniform` buffer and bind group updated per frame in `Prepare`/`PrepareScene`.

## Testing

- `packages/game/render3d/probe_io_test.go`: cubemap strip HDR round-trip, equirectangular fallback, and `formattest.Run` conformance tests for `*.probes.yaml` (including EN/ZH `formats.md` examples).
- `packages/game/sceneformat/probes_test.go`, `merge_test.go`, `resolve_test.go`: version 4 round-trip, validation errors, deep clone, merge field coverage, and override resolution.
- `packages/game/scenemodel/probes_test.go`: end-to-end offline baking of a reflection probe and light probe grid in a colored-wall room and verification of baked IBL shading.
- `packages/game/editor/probes3d_test.go` and `probes3d_gpu_internal_test.go`: placement, undo/redo, 3D bounds handle dragging + Escape cancel, unsafe output path rejection, golden image + pixel-for-pixel game/editor parity (`viewport3d_probes.png`), and rebaking after moving a colored wall.
- `packages/game/refs/probes_test.go` and `packages/game/tooling/assetcheck/probes_test.go`: reference scanning and corrupt-asset reporting.
- Verified with `gofmt`, `go run ./cmd/gameschema -check`, desktop and `-target ios` `go tool nsgo check`, `go tool nsgo test` on all touched packages + root `TestFormatsPage|TestStabilityTable|TestAgentSkillValid`, `go run ./tools/changelog check`, and `bun run verify-notes`.
