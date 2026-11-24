# Agent Note: GPU skinning for glTF skinned meshes in render3d

Status: implemented

## Problem

3D characters exported from glTF 2.0 (such as Khronos `CesiumMan` and `RiggedSimple`) deform each vertex by up to four joint matrices (`JOINTS_0`, `WEIGHTS_0`, `skins[].joints`, and `skins[].inverseBindMatrices`). Performing skinning on the GPU in `game/render3d` (#121) must work identically across all NSGo GPU backends—including WebGL 2 and OpenGL ES 3.0 where compute shaders and storage buffers are unavailable (`nsgotest.NoGPUCompute`) and `MAX_UNIFORM_BLOCK_SIZE` is guaranteed only up to 16 KiB—while staying allocation-free per frame, recovering across GPU device loss when `Retained` is enabled, and matching a deterministic CPU reference.

## Decision

`packages/game/render3d/skin.go` implements portable vertex-shader linear blend skinning using a fixed-layout interleaved skinned vertex buffer and an 8 KiB uniform joint palette:

- **Vertex layout (`SkinnedVertexLayout`, 64 bytes/vertex)**: extends `MeshVertexLayout` locations 0..2 (`pos`, `normal`, `uv`) with `@location(3) joints: vec4<u32>` (`gpu.VertexUint32x4`, offset 32) and `@location(4) weights: vec4<f32>` (`gpu.VertexFloat32x4`, offset 48). `SkinnedMesh.Validate` enforces geometry validity (wrapping `ErrInvalidMesh`) and skinning invariants (wrapping `ErrInvalidSkin`): finite non-negative weights and every joint index strictly below `JointCount` (or `MaxSkinJoints` when `JointCount == 0`), preventing out-of-bounds uniform array reads in shaders. `SkinnedMeshFromGLTF` and `AppendVertexData` normalize weights via `NormalizeSkinWeights` (falling back to `(1, 0, 0, 0)` when all four weights sum to zero).
- **Joint palette (`GPUSkinPalette`, `SkinGroup = 1`)**: stores `MaxSkinJoints = 128` column-major `mat4x4<f32>` entries in a `SkinUniformSize = 8192`-byte uniform buffer (`UsageUniform | UsageCopyDst`), fitting well inside WebGL 2 / GLES 3.0's 16 KiB UBO minimum while covering humanoid character rigs. Unused slots `[len(joints), 128)` are padded with identity matrices. `AppendJointMatrices`, `SkinJointMatrices`, `SkinJointMatricesWithTransform`, and `GPUSkinPalette.UpdateFromGLTF` reuse pre-allocated buffers for zero steady-state heap allocations.
- **glTF 2.0 matrix convention**: `SkinJointMatrices` computes `doc.Nodes[jointNode].WorldMatrix.Mul(skin.InverseBindMatrices[i])` (using identity when `InverseBindMatrices` is omitted), ignoring the skinned mesh node's own transform per glTF 2.0 section 3.7.3.2 so skeletons rooted under axis-conversion nodes such as `CesiumMan`'s `Z_UP` stand upright in Y-up world space.
- **Shader & CPU parity (`SkinWGSL`, `NewSkinnedMeshPipeline`, `SkinVertex`)**: `skin_position` and `skin_normal` evaluate linear blend skinning as `sum_i (skin.joints[j_i] * v) * w_i` rather than summing weighted 4x4 matrices first. Normal skinning uses `vec4<f32>(normal, 0.0)` and guards normalization with `dot(blended, blended) > 1e-12` so zero normals never produce `NaN`. `SkinVertex` executes the identical formula on the CPU.
- **Device loss (`Retained`)**: `UploadSkinnedMesh`, `NewGPUSkinPalette`, and `NewSkinnedMeshPipeline` forward `Retained` to NSGo GPU descriptors, and `GPUSkinPalette.Update` updates its cached device pointer whenever a non-nil `*gpu.Device` is passed so post-restore pose updates write cleanly to the restored buffer.

## Alternatives considered

### Why not a storage buffer (`var<storage, read>`) for arbitrary joint counts?

A storage buffer removes the 128-joint ceiling and lets one buffer hold multiple characters' skeletons. However, NSGo's WebGL 2 and OpenGL ES 3.0 backends do not support storage buffers (`Device.SupportsCompute() == false`, modeled by `nsgotest.NoGPUCompute()`). Using a uniform buffer of 128 `mat4x4<f32>` matrices (8 KiB) guarantees every NSGo target—desktop, mobile, and WebGL 2—runs the same shader and pipeline without feature branching.

### Why not `uint16x4` (`u16`) vertex attributes for `JOINTS_0`?

Packing joint indices as four `uint16` values would shrink the vertex stride from 64 bytes to 56 bytes. NSGo's portable `gpu.VertexFormat` set does not expose a `Uint16x4` attribute format; `gpu.VertexUint32x4` is supported across every NSGo backend (Metal, D3D11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0, and the software rasterizer) and keeps the interleaved vertex stride aligned to 64 bytes.

### Why not blend scaled 4x4 matrices (`m0*w.x + m1*w.y + ...`) before multiplying the vertex?

Computing a single blended `mat4x4<f32>` before transforming both position and normal reads closely to textbook linear blend skinning. However, evaluating `(skin.joints[j_i] * v) * w_i` per influence uses fewer scalar operations on GPUs and avoids matrix-plus-matrix addition, which NSGo's software GPU shader interpreter (`evalBinary` in `nsgo/internal/driver/gpu_software.go`) only loops over vector element counts rather than 16 matrix elements. Vector-side blending is algebraically identical and exact on both hardware GPUs and the software rasterizer.

### Why not store inverse-transpose normal matrices alongside joint matrices?

Storing a second palette of 128 `mat3x3`/`mat4x4` normal matrices would double the uniform block to 16 KiB (hitting WebGL 2 / GLES 3.0's exact limit with zero headroom) and double per-frame CPU uploads. For rigid and uniformly scaled skeletal joints—which cover standard glTF character rigs—transforming normals by the upper-left 3x3 of the blended joint matrix and renormalizing produces exact results at half the uniform bandwidth. Non-uniformly scaled bones with extreme shear would need inverse-transpose normal matrices in a future extension.

## Consequences

- **Benefit:** Khronos `CesiumMan` (19 joints, 3,273 vertices) and `RiggedSimple` (2 joints) render identically on GPU and CPU across both full and `NoGPUCompute` (WebGL 2 / GLES 3.0) profiles, with zero steady-state allocations on joint updates.
- **Benefit:** Malformed meshes, negative/non-finite weights, and out-of-range joint indices are rejected upfront before GPU upload, keeping uniform indexing memory-safe.
- **Cost:** A single draw call is capped at `MaxSkinJoints = 128` joints; characters exceeding 128 joints must be split into multiple sub-meshes with per-sub-mesh joint subsets.
- **Cost:** Normals transformed by heavily non-uniform joint scales are approximate because the upper-left 3x3 is used instead of an inverse-transpose normal palette.

## Testing

- `packages/game/render3d/skin_test.go` verifies:
  - Khronos `CesiumMan` (`packages/game/gltf/testdata/khronos/CesiumMan/`) bind-pose and animated walk-cycle GPU renders against CPU `SkinVertex` reference renders on both `full` and `nsgotest.NoGPUCompute()` profiles.
  - Khronos `RiggedSimple` bind-pose and animated bend GPU-vs-CPU parity on both profiles.
  - Multi-influence `SkinVertex` math, weight normalization, zero-weight fallback, out-of-palette identity padding, validation errors (`ErrInvalidMesh` / `ErrInvalidSkin`), uint32 index promotion at 65,536 vertices, non-indexed and double-sided draws, `Retained` device-loss restoration + post-restore pose update, `core.Scope` disposal, zero-allocation steady-state benchmarks, and `FuzzSkinnedMeshData`.
- Verified with `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./render3d`, `go tool nsgo test -run TestStabilityTable .`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`.
