# Agent Note: glTF 2.0 morph target evaluation and GPU morph meshes

Status: implemented

## Problem

`game/gltf` previously parsed morph target delta accessors (`Primitive.Targets`), `Mesh.Weights`, `Node.Weights`, and `PathWeights` animation channels, and offered `EvaluateMorphPositions` / `EvaluateMorphNormals`, but lacked three capabilities required to play real glTF 2.0 morph assets such as the Khronos `AnimatedMorphCube`:

1. **Complete glTF 2.0 morph evaluation without per-frame allocation**: `EvaluateMorphTangents` (blending tangent `XYZ` while preserving `W` handedness), combined evaluation with updated object-space bounds (`ApplyMorphWeights` into a reusable `MorphResult`), glTF weight precedence (`node.Weights` overriding `mesh.Weights`, else zeros), and zero-allocation weight sampling (`AnimationChannel.Sample` allocates a fresh `[]float32` slice on every `PathWeights` sample and an additional scratch slice for `CUBICSPLINE`).
2. **GPU mesh updates on `gpu.View`**: `game/render3d` had static `GPUMesh` upload (`UploadMesh`), but no way to update an uploaded vertex buffer from animated morph target weights with dirty tracking, updated culling bounds, and `Retained` device-loss survival.
3. **Cross-backend portability**: Morph meshes must render identically on Metal, Direct3D 11, OpenGL 3.3, WebGPU, WebGL 2, and OpenGL ES 3.0—including backends without compute shaders or storage buffers (`nsgotest.NoGPUCompute`).

## Decision

Morph target support is split cleanly across `packages/game/gltf/morph.go` and `packages/game/render3d/morph.go` without coupling `render3d` to `gltf`:

- **`game/gltf` (`packages/game/gltf/morph.go`)**:
  - `EvaluateMorphTangents` blends `prim.Tangents[i].xyz + sum_k(w_k * dT_k[i])`, renormalizes `xyz` when morphed, and preserves `prim.Tangents[i].w` handedness unchanged.
  - `ApplyMorphWeights` evaluates positions, normals, tangents, and recomputes `Bounds` into a caller-owned `*MorphResult` with zero steady-state heap allocations.
  - `ResolveWeights` and `ResolveWeightsInto` enforce glTF 2.0 weight precedence (`node.Weights` > `mesh.Weights` > zeros matching the primitive's morph target count).
  - `SampleMorphWeights` samples the `PathWeights` channel for a node using `STEP`, `LINEAR`, or `CUBICSPLINE` (unpacking `3*N` values `[inTangent, value, outTangent]` per keyframe) into a reusable `dst` slice using an inline binary search with zero allocations.
- **`game/render3d` (`packages/game/render3d/morph.go`)**:
  - `MorphTarget` and `MorphMesh` mirror `gltf.MorphTarget` / `gltf.Primitive` field types without importing `game/gltf`, keeping `render3d` format-agnostic (matching `render3d.Mesh`).
  - `UploadMorphMesh` validates the base mesh, targets, and initial weights (every error wraps `ErrInvalidMesh`), deep-clones the CPU base and target slices so caller mutations cannot corrupt GPU updates, evaluates initial weights, and uploads the resulting vertex and optional index buffers through `uploadMesh` with `gpu.UsageVertex | gpu.UsageCopyDst` and `MeshUpload.Retained`.
  - `GPUMorphMesh` blends morph targets on the CPU into pre-allocated scratch buffers and uploads interleaved 32-byte `MeshVertexLayout` vertices via `dev.WriteBuffer` when dirty.
  - Dual dirty flags (`cpuDirty` and `gpuDirty`) guarantee that calling `Bounds()` before `Update(dev)` computes exact live morphed bounds once and caches the blended positions/normals for `Update(dev)`, while unchanged weights skip both CPU blending and `WriteBuffer`.
  - During GPU device loss (`!m.mesh.Valid()`), `Update(dev)` returns `false` without clearing `gpuDirty`; for a `Retained` mesh, NSGo restores the last uploaded morphed bytes automatically, and any weight change made while lost uploads on the first `Update(dev)` after restore.

## Alternatives considered

### Why not blend morph targets in vertex shaders via extra vertex attributes or storage buffers?

Passing morph target deltas to the vertex shader eliminates CPU-to-GPU vertex uploads per frame for high-vertex-count meshes. It was rejected as the primary `render3d` path for two reasons: (1) WebGL 2 and OpenGL ES 3.0 cap vertex attributes at 16 (limiting meshes to at most 4–6 position+normal targets) and lack storage buffers / compute shaders (`nsgotest.NoGPUCompute`), and (2) requiring specialized morph vertex layouts breaks `render3d`'s core contract that every mesh shares the single 32-byte `MeshVertexLayout` so any pipeline can draw any mesh. CPU blending into the existing `MeshVertexLayout` vertex buffer supports unlimited morph targets, works identically across all six NSGo backends, and keeps `Bounds()` exact for frustum culling.

### Why not have `render3d` import `game/gltf` directly?

Accepting `gltf.Primitive` directly in `UploadMorphMesh` would save callers a small target-slice copy when loading glTF files. It was rejected because `render3d` is intentionally format-agnostic (`render3d.Mesh` likewise mirrors `gltf.Primitive` without importing `gltf`), avoiding pulling the glTF, Draco, and meshopt decoders into callers that construct procedural or non-glTF morph meshes.

### Why not modify `gltf.AnimationChannel.Sample` in place instead of adding `SampleMorphWeights`?

Rewriting `AnimationChannel.Sample` to take an output buffer would break its existing exported signature. Keeping `AnimationChannel.Sample` unchanged and providing `SampleMorphWeights(anim, nodeIndex, t, dst)` preserves backwards compatibility while giving morph playback a zero-allocation API that directly finds and unpacks a node's `PathWeights` channel.

## Consequences

- **Benefits**:
  - Real glTF 2.0 morph assets (including sparse morph targets and animated `STEP`/`LINEAR`/`CUBICSPLINE` weights) evaluate and draw cleanly across every NSGo target, including WebGL 2 and OpenGL ES 3.0.
  - Steady-state weight sampling, weight resolution, and morph evaluation perform zero heap allocations; unchanged weights perform zero GPU uploads.
  - Existing 3D pipelines using `MeshVertexLayout` draw `GPUMorphMesh` with no shader modifications, and `Retained` morph meshes survive device loss with their latest morphed pose.
- **Costs & limits**:
  - `GPUMorphMesh` keeps cloned CPU copies of base vertices and morph target deltas and streams the morphed vertex buffer via `dev.WriteBuffer` on frames where weights change, which is CPU- and bandwidth-bound for very large dense meshes (>100k vertices animated every frame); a future compute/storage-buffer path can optimize high-poly targets on WebGPU/Metal/D3D11 if profiling warrants it.
  - `render3d.MorphTarget` currently carries `Positions` and `Normals` (matching `MeshVertexLayout`); tangent-space morphing is evaluated in `game/gltf` (`EvaluateMorphTangents`, `ApplyMorphWeights`) until a normal-mapped tangent vertex layout is added to `render3d`.

## Verification

- `packages/game/gltf/morph_test.go` loads the unmodified Khronos `AnimatedMorphCube` (`testdata/khronos/AnimatedMorphCube/glTF/AnimatedMorphCube.gltf`), verifies exact position/normal/tangent blending and handedness preservation across `t = 0`, mid-cycle, peak `"thin"`/`"angle"` weights, and loop wrap, tests sparse morph targets, `STEP`/`LINEAR`/`CUBICSPLINE` interpolation, `node.Weights` vs `mesh.Weights` precedence, and asserts zero steady-state allocations.
- `packages/game/render3d/morph_test.go` renders the Khronos `AnimatedMorphCube` through `gpu.View` under both full capabilities and `nsgotest.NoGPUCompute` (WebGL 2 / OpenGL ES 3.0 profile), verifying distinct morphed silhouettes and loop-wrap pixel parity via `GPUReadPixels`, dirty-tracking upload suppression, live bounds updates, `Retained` device-loss restoration (both pre-loss and mid-loss weight updates), and validation/error handling.
- `bun run verify-notes`, `go run ./tools/changelog check`, `go tool nsgo check -target macos,windows,linux,android,web`, and `go tool nsgo test` validate formatting, schemas, cross-target compilation, and tests.
