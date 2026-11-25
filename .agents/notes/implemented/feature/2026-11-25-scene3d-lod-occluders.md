# Agent Note: 3D model LOD levels and scene object occluders

Status: implemented

## Problem

`render3d` already selects per-object levels of detail with hysteresis (`DrawItem.LODs`) and culls objects behind occluders (`render3d.OcclusionBuffer`), but neither could be authored in text files: glTF models had no convention or sidecar syntax for declaring LOD levels and screen-ratio thresholds, `sceneformat.ObjectDoc` had no way to mark an object as an occluder, `scenemodel` emitted flat single-mesh draw items without an occlusion pass, and the editor's 3D viewport showed neither active LODs, occluders, nor `render3d.RenderStats`.

## Decision

Wire LOD declarations and occluders across the existing 3D pipeline (`gltf`, `tooling/importsettings`, `sceneformat`, `scenemodel`, and `editor`) while preserving the boundary from [scene-objects-draw-gltf-models](2026-11-25-scene-objects-draw-gltf-models.md) and extending the sidecar workflow from [editor-import-settings](2026-11-25-editor-import-settings.md):

- **LOD naming parser (`packages/game/gltf/lod.go`)**: `gltf.ParseLODNodeName` recognizes case-insensitive `_LOD<N>` suffixes (ignoring Blender `.001` duplicate suffixes) and returns `(baseName, level, ok)` so naming-convention matching is shared consistently across tools and runtime builders.
- **Model import sidecar (`packages/game/tooling/importsettings`)**: `Model` adds `LODs []ModelLOD` (`model`, `node`, `mesh`, `minScreenRatio`, `maxDistance`) and `LODHysteresis float32` inside `import.v1.json` (version 1 remains backwards-compatible because all new fields are optional). Validation enforces strictly decreasing `minScreenRatio` (`[0, 1]`), strictly increasing `maxDistance`, mutual exclusion between `node` and `mesh`, valid slash-separated `.gltf`/`.glb` paths for external `model` levels, and `lodHysteresis` in `[0, 0.5]`. Negative zero floats normalize to `0` for deterministic YAML round-trips.
- **Scene object occluders (`packages/game/sceneformat`)**: version 2 `ObjectDoc` and `ObjectOverride` add `occluder: *OccluderDoc` (`mode: "model"` or `"box"`, optional `center`/`size` as `*[3]float32` so unset fields omit cleanly in `yaml.v3`, and `doubleSided`). `mode: model` requires the object to carry a `mesh` or `model`; `mode: box` forbids non-positive sizes and defaults to centered `[1, 1, 1]`. `scene.v2.json` and `import.v1.json` are regenerated via `cmd/gameschema`.
- **Scene 3D builder & renderer (`packages/game/scenemodel`)**:
  - `ImportSettingsResolver` is an optional capability interface on the `Build` resolver (implemented with caching by `Assets`), keeping `ModelResolver` minimal for callers that do not use sidecars.
  - `Build` pairs sibling `_LOD1`/`_LOD2` nodes or meshes under their base drawable (suppressing `_LODn` siblings from also spawning as independent drawables), applies sidecar thresholds or explicit external-model/node/mesh LOD entries, and falls back to `DefaultLODScreenRatio` (`0.25, 0.10, 0.04, 0.015`, coarsest `0`). `Built.AppendDrawItems` populates `DrawItem.LODs` with pre-converted `render3d.LODLevel` slices and sets `Part.LODs` with shared `MeshID`s so renderers upload each unique LOD mesh once.
  - `Build` extracts `Built.Occluders` from every object with `occluder`: `mode: box` creates a 12-triangle box mesh in local space, and `mode: model` reuses the object's coarsest LOD mesh when available. `Built.AppendOccluders` and `Built.RasterizeOccluders` let custom renderers drive their own `render3d.OcclusionBuffer`, while `scenemodel.Renderer.Prepare` rasterizes visible occluders automatically (unless `RendererOptions.DisableOcclusion` is set) and exposes the resulting `render3d.RenderPlan` (including `Stats.Occluded`) via `Renderer.Plan()`.
- **Editor 3D viewport & inspectors (`packages/game/editor`)**:
  - `Editor` caches a `scenemodel.Built` + `render3d.OcclusionBuffer` + `render3d.Queue` evaluation for `View3D` (`ensureViewport3DPlan`, invalidated on document revision, import-sidecar revision, camera/size, visibility, or clip time changes), automatically constructing `scenemodel.NewAssets` for open projects or using an injected `Editor.ModelResolver`.
  - `DrawViewport3D` color-codes multi-LOD objects and draws `LOD<n>` badges (`ShowLODOverlay`), draws magenta occluder wireframes with `OCC` / `OCC BOX` labels (`ShowOccluders`), dims occluded objects, and overlays `render3d.RenderStats` (`viewport3d-render-stats`, `ShowRenderStats`).
  - The 3D object inspector adds active LOD readout (`inspector-lod-active`) and undoable `SetObjectOccluder` controls (auto-raising v1 scenes to v2); the asset import inspector adds `lodHysteresis` and `lods` controls (`SetModelImportLODs`, `SetModelImportLODHysteresis`) that invalidate the 3D viewport build immediately.

## Alternatives considered

- **Declare model LODs inside `scene.yaml` on each object instead of `*.import.yaml` + `_LODn` naming**: avoids touching `tooling/importsettings`. Rejected because LOD meshes and switch ratios are properties of the model asset reused across every instance in every scene; duplicating them per object in scene files would drift and violates the issue specification.
- **Bump `importsettings.CurrentVersion` from 1 to 2**: signals schema growth via a new version number. Rejected because `model.lods` and `model.lodHysteresis` are strictly optional additive fields on `Model`, so existing `import.v1` sidecars remain valid unchanged without forcing every project's sidecars to migrate versions.
- **Use `[3]float32` values instead of `*[3]float32` for `OccluderDoc.Center` and `Size`**: avoids pointer indirection. Rejected because `go.yaml.in/yaml/v3` does not treat zero-valued fixed-size Go arrays as empty for `omitempty`, which would emit spurious `center: [0, 0, 0]` and `size: [0, 0, 0]` lines on `mode: model` occluders and fail minimal round-trip expectations.
- **Duplicate LOD selection and occlusion culling math inside `editor/viewport3d.go`**: avoids calling `scenemodel.Build` from the editor. Rejected because duplicating screen-ratio projection, hysteresis, and conservative depth rasterization would inevitably drift from `render3d.Queue`; running the real `scenemodel` + `render3d` pipeline guarantees that editor LOD badges, occluder previews, and `RenderStats.Occluded` match in-game behavior.

## Consequences

- **Benefits**:
  - 3D levels authored in YAML automatically gain LOD switching with hysteresis and software occlusion culling in both `scenemodel.Renderer` and custom `render3d.Queue` pipelines.
  - The editor 3D viewport provides live visual feedback (per-object LOD level, occluder geometry, and `RenderStats` including `Occluded`) backed by the exact runtime culling pipeline.
  - Both `scene.v2.json` and `import.v1.json` validate the new fields for editors and AI tools.
- **Costs and known limits**:
  - `scenemodel.Build` reads `<model>.import.yaml` when the resolver implements `ImportSettingsResolver`; editing a sidecar outside the editor requires rebuilding `Built` (inside the editor, `importSettingsCmd` invalidates the 3D build cache automatically).
  - Model occluders (`mode: model`) use the object's triangle mesh as authored (preferring the coarsest LOD when present) rather than computing a convex hull or simplified proxy; complex high-poly meshes should use `mode: box` or a coarse LOD for best CPU rasterization cost.
  - In the editor, `ensureViewport3DPlan` requires an open project directory or an injected `Editor.ModelResolver` to load glTF models; without one, `mode: box` occluders still render and cull primitive objects while missing models degrade gracefully without breaking the wireframe viewport.

## Verification

- `env -u GOROOT go run ./cmd/gameschema -check`
- `env -u GOROOT go tool nsgo test ./gltf`
- `env -u GOROOT go tool nsgo test ./tooling/importsettings`
- `env -u GOROOT go tool nsgo test ./sceneformat`
- `env -u GOROOT go tool nsgo test ./scenemodel`
- `env -u GOROOT go tool nsgo test ./cmd/gameschema`
- `TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor`
- `TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor/editormcp`
- `env -u GOROOT go tool nsgo test . -- -run 'TestFormatPackagesRunConformance|TestStabilityTable|TestAgentSkillValid|TestFormatsPage'`
- `env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web` and `-target ios` on each touched package
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
