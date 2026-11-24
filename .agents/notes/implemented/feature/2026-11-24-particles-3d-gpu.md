# Agent Note: GPU-friendly 3D particles in game/particles

Status: implemented

## Problem

`game/particles` previously provided only a 2D CPU-drawn `Emitter` backed by `sprite.Batch` (`core.Drawing`). Rendering 10,000 particles in 3D at 60 fps requires:

1. Sharing the existing 2D emitter model (`FloatCurve`, `ColorCurve`, `SimulationSpace`, seeded PCG determinism, `Rate`, `Burst`, `LifetimeMin`/`Max`, `SpeedMin`/`Max`, versioned YAML definitions) without creating a circular import with `game/render3d` (which already imports `game/particles` for trail curves).
2. Stepping, optionally back-to-front sorting, and packing 10,000 live 3D particles well inside a 16.6 ms desktop frame budget with zero per-frame heap allocations.
3. Rendering all live particles in one instanced GPU draw call across every backend `gpu.View` targets, including WebGL 2 and OpenGL ES 3.0 (`NoGPUCompute`), where compute shaders and storage buffers are unavailable.

## Decision

3D particles live alongside 2D particles in `packages/game/particles` (`emitter3d.go`, `gpu3d.go`, `format3d.go`) with three core structural choices:

- **SoA storage with swap-remove and zero-allocation radix sort (`emitter3d.go`)**: `Emitter3D` stores live particles in dense structure-of-arrays slices (`pos`, `vel`, `age`, `life`, `invLife`, `baseSize`, `initRot`, `rotSpeed`, `size`, `rot`, `alpha`, `color`) with O(1) swap-remove expiration and precomputed `invLife = 1/life`. `SortByDepth(camPos, camForward)` runs a deterministic 4-pass 8-bit LSB radix sort over IEEE-754 descending float keys into reusable scratch slices (`sortKeys`, `sortOrder`, `sortTmpK`, `sortTmpO`), avoiding `O(N log N)` comparison overhead and heap allocations. For `SpaceLocal` emitters, `SortByDepth` projects `camForward` into emitter-local space once outside the particle loop rather than transforming every particle position.
- **Pre-oriented 40-byte per-instance GPU layout without compute shaders (`gpu3d.go`)**: `Emitter3D.AppendInstances` packs each live particle into 40 bytes (`ParticleInstanceStride`): `center: vec3<f32>` (12), `right: vec3<f32>` (12), `up: vec3<f32>` (12), and `color: unorm8x4` (4). Because the world-space quad basis `(right, up)` is computed during packing, one single vertex layout (`ParticleVertexLayouts`) and one `DrawIndexedInstanced(6, count, 0, 0, 0)` call renders `BillboardSpherical` (with roll), `BillboardCylindrical` (locked to `Axis`), and `BillboardStretched` (elongated along velocity) with no shader permutations and no compute/storage-buffer requirement.
- **Separate WGSL modules per fragment mode and `render3d.CameraWGSL` layout compatibility (`gpu3d.go`)**: `ParticleWGSL`, `ParticleSoftCircleWGSL`, and `ParticleTexturedWGSL` each define a single `@vertex` (`vs_main`) and single `@fragment` entry point (because `nsgo/gpu`'s `shaderir.Program` carries one `FragmentFunc` per compiled module) and declare `@group(0) @binding(0) var<uniform> camera: Camera` with the exact 208-byte `(view, proj, view_proj, eye)` layout of `render3d.CameraBuffer`, allowing direct interoperability without importing `render3d`.
- **Versioned YAML/JSON config (`format3d.go`)**: `Emitter3DConfig` serializes deterministically with `version: 1`, strict unknown-field rejection, null rejection, and shape-kind parameter validation, matching the 2D `format.go` contract.

## Alternatives considered

### Compute-shader GPU particle simulation

Simulating particles on the GPU via compute shaders and storage buffers removes the ~400 KB/frame CPU-to-GPU upload and frees CPU cycles at 100k+ particle counts. It was rejected because:
- `gpu.View` must work on WebGL 2 and OpenGL ES 3.0 (`nsgotest.NoGPUCompute`), which lack compute shaders and storage buffers.
- GPU simulation breaks cross-platform bit-for-bit seeded determinism (`Emitter3D` replays identically across software, Metal, Vulkan, D3D11, and WebGPU) and prevents gameplay/test code from inspecting `Particle(i)` synchronously on the fixed loop.
- At 10,000 particles, CPU SoA update + 400 KB instance pack takes ~0.34 ms (~0.44 ms with depth sort) and 0 allocs/op, well within the 60 fps target.

### Placing `Emitter3D` in `packages/game/render3d`

Putting 3D particles in `render3d` would allow importing `render3d.CameraBuffer` and `render3d.CameraBasis` directly. It was rejected because `render3d` already imports `particles` (for `Curve` and `ColorCurve` on 3D trails), and splitting 2D and 3D emitters across separate packages would duplicate curve/space/YAML config helpers and fragment the shared emitter model requested in #280.

### Expanding billboard orientation in the vertex shader via per-instance position/size/rotation/velocity

Passing raw `(pos, vel, size, rot, color)` and branching on billboard mode in WGSL avoids computing `sincos` or cross products on the CPU. It was rejected because supporting spherical, cylindrical, and velocity-stretched billboards in WGSL would require extra uniform parameters and shader permutations or dynamic branches per vertex (4× per particle instead of 1× per particle on CPU), and would still require ~44 bytes per instance once velocity is included.

## Consequences

- **Benefits**:
  - 10,000 active 3D particles step and pack in ~0.34 ms (`0 B/op`, `0 allocs/op`) on Apple Silicon, or ~0.44 ms with full back-to-front radix depth sorting, and render in a single `DrawIndexedInstanced` call.
  - Identical behavior and pixel output across full WebGPU/Metal/Vulkan/D3D11 and `NoGPUCompute` (WebGL 2 / OpenGL ES 3.0) profiles, plus bit-identical seeded determinism across platforms.
  - Direct bind-group compatibility with `render3d.CameraBuffer` at `@group(0) @binding(0)` without introducing a package dependency cycle.
- **Costs & limits**:
  - `Emitter3D` transfers `40 * count` bytes per frame via `dev.WriteBuffer` (400 KB at 10k particles); scaling past ~100k particles would eventually motivate an optional compute-shader backend on capable devices.
  - Swap-remove expiration does not preserve spawn order; alpha-blended emitters should call `SortByDepth` before `Upload` / `AppendInstances`.
  - If `render3d.CameraWGSL`'s 208-byte `Camera` uniform layout ever changes, `ParticleWGSL`, `ParticleSoftCircleWGSL`, and `ParticleTexturedWGSL` must be updated in lockstep.

## Verification

- `emitter3d_test.go`: tests 10,000-particle seed determinism and `Reset` replay, all 6 3D spawn shapes, gravity/drag/curves/expiration, `SpaceLocal` vs `SpaceWorld`, all 3 billboard modes, `SortByDepth` back-to-front ordering, 0-alloc steady-state checks at 10k particles, `ExampleEmitter3D`, and `BenchmarkEmitter3D10kStepAndPack` / `BenchmarkEmitter3D10kStepSortAndPack`.
- `gpu3d_test.go`: verifies pixel readback on `gpu.SoftwareRenderer` (alpha-sorted overlap, additive overlap, soft-circle radial falloff, textured particles, dynamic buffer growth), `gpu.View` pixel readback on both `full` and `nsgotest.NoGPUCompute()` profiles, 10,000 particles in 1 `DrawIndexedInstanced` call, and `Retained` buffer survival across GPU device loss/restore.
- `format3d_test.go`: verifies deterministic YAML/JSON round-trip and validation rejections.
