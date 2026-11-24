# Agent Note: Forward-path box-projected 3D decals in scene3d

Status: implemented

## Problem

3D games built on `packages/game` need surface decals (bullet marks, scorches, footsteps, blob shadows, and splats) that project cleanly onto arbitrary scene geometry, respect surface normals with smooth grazing-angle fade, honor per-node layer masks so characters or excluded props stay clean, and cap live counts through a pool—while working identically on NSGo's mobile forward rendering targets (OpenGL ES 3.0 on Android and WebGL 2 on web, where `NoGPUCompute` disables compute shaders and there is no deferred G-buffer or portable depth-buffer readback).

Placing decal projection inside `scene3d` also has a hard package-boundary constraint: `render3d` already imports `scene3d` (`render3d/camera.go`), so `scene3d` cannot import `render3d` without introducing a Go import cycle.

## Decision

Box-projected 3D decals live in `packages/game/scene3d` (`decal.go`, `decal_gpu.go`, `decal_test.go`) and use CPU BVH-accelerated mesh clipping to emit forward-path triangle geometry rendered by a single alpha-blended WGSL pipeline:

- **BVH-accelerated Sutherland–Hodgman mesh clipping (`ProjectDecal`, `DecalPool.BuildGeometry`)**: Each `Decal` defines an oriented projection box (`Transform` + `HalfExtents`, projecting along local `-Z` onto surfaces facing `+Z`). Projection queries the scene's dynamic BVH with `Decal.WorldBounds()`, filters candidate nodes by `Node.Layer & Decal.LayerMask != 0` and memoized `Scene.activeVisible`, resolves each node's `DecalReceiver` (`Node.SetDecalReceiver` attachment or `*MeshCollider`), and clips receiver triangles against the six planes of normalized local box space `[-0.5, +0.5]^3` using stack-allocated ping-pong buffers (`[12]decalClipVert`).
- **Normal-angle fade, depth fade, and z-fighting prevention**: Triangles whose geometric face normal is perpendicular or back-facing (`dot(faceN, +Z) <= 1e-5`) or whose vertex normals all fall past `AngleFadeEnd` are discarded before clipping. Surviving clipped vertices evaluate smoothstep angle fade between `AngleFadeStart` and `AngleFadeEnd`, smoothstep depth fade near `|local.z| -> 0.5`, UV coordinates `(local.x + 0.5, 0.5 - local.y)` (top-left WebGPU/GLES origin), and world-space normal offset `Bias`. `GPUDecalRenderer` draws with `DepthWrite: false`, `DepthCompare: gpu.CompareLessEqual`, `DepthBias`/`DepthBiasSlopeScale`, and `gpu.BlendAlpha()`.
- **Fixed-capacity generational pool (`DecalPool`)**: `NewDecalPool(maxDecals)` pre-allocates fixed slot storage, a free-slot stack, an active draw-order scratch slice, and a BVH query scratch slice. When full, `Spawn` deterministically evicts the live decal with the lowest `SortOrder`, breaking ties by oldest spawn sequence (FIFO ring behavior for equal priorities). `Step(dt)` advances `Age`, computes `FadeIn`/`FadeOut` opacity, and expires decals at `Age >= Lifetime`. Concrete type switches on `*MeshCollider` and `*DecalReceiverMesh` avoid interface-closure escapes so steady-state `Spawn` + `Step` + `BuildGeometry` perform zero heap allocations.
- **Standalone GPU forward renderer compatible with `render3d.CameraBuffer` (`GPUDecalRenderer`)**: `DecalWGSL` declares the exact 208-byte camera uniform layout at `@group(0) @binding(0)` as `render3d.CameraWGSL` and an albedo texture + sampler at `@group(1)`. Callers can pass `render3d.CameraBuffer.Layout()` as `GPUDecalOptions.CameraLayout` and bind `camBuf.BindGroup()` via `DrawWithCamera`, or omit `CameraLayout` to use the renderer's internal camera buffer via `WriteCamera` + `Draw`. Consecutive `DecalBatch` entries sharing a `TextureID` merge into one `DrawIndexed` call.

## Alternatives considered

### Why not deferred / screen-space depth-reconstructed box decals?

Drawing an inverted unit cube in a screen-space shader that samples the scene depth buffer (and G-buffer normals) handles curved or dynamic skinned meshes without CPU triangle clipping and supports thousands of overlapping decals in O(1) CPU work. It was rejected because NSGo's mobile forward path (GLES 3.0 / WebGL 2, represented in tests by `nsgotest.NoGPUCompute`) has no deferred G-buffer and does not guarantee readable hardware depth textures alongside an active color/depth attachment; reading back depth requires an extra depth pre-pass or copy pass on mobile tile GPUs, hurting bandwidth and portability. Forward clipped decal meshes work in the existing single forward pass across every backend without extra render targets.

### Why not place decals in `packages/game/render3d` instead of `packages/game/scene3d`?

Putting decals in `render3d` would allow direct reuse of `render3d.Mesh` and `render3d.CameraBuffer` types without defining `DecalReceiverMesh` or duplicating the 208-byte camera uniform encoding. It was rejected because decal projection tightly couples to `scene3d.Scene`'s internal BVH, `activeVisible` memoization, `Node.Layer` masks, and `MeshCollider`, and keeping decals in `scene3d` avoids touching `render3d` while parallel `render3d` work is in flight; layout compatibility at `@group(0)` preserves zero-overhead interop with `render3d.CameraBuffer`.

### Why not back `DecalPool` with an unbounded slice or a pure ring buffer without priorities?

An unbounded slice is simpler to write, and a pure ring buffer avoids scanning slots on eviction. Both were rejected because issue #385 requires a pool that caps live count deterministically, and games routinely mix persistent/high-priority decals (e.g., scorch craters or boss footprints with higher `SortOrder`) with transient marks (bullet holes with `SortOrder == 0`). Scanning fixed `slots` only when full is O(`maxDecals`), zero-allocation, and preserves strict FIFO eviction whenever priorities are equal.

## Consequences

- **Benefits**:
  - Works identically on every NSGo GPU target (WebGPU, Metal, Vulkan, OpenGL ES 3.0, WebGL 2, and `gpu.NewSoftwareRenderer`) inside a single forward render pass with zero compute shaders or extra render targets.
  - Strictly enforces surface normal direction, smooth grazing-angle fade, per-node `LayerMask` exclusion, and bounded pool capacity with zero steady-state allocations.
  - Interoperates directly with `render3d.CameraBuffer` at `@group(0)` while remaining usable standalone from `scene3d`.
- **Costs & limits**:
  - Receiver geometry must be exposed on the CPU via `*MeshCollider`, `*DecalReceiverMesh`, or a custom `DecalReceiver`; GPU-only procedurally displaced or GPU-skinned vertex animations are not reflected in static CPU triangles unless updated on the CPU.
  - Very dense receiver meshes overlapped by many moving decals scale with the number of candidate triangles inside each decal's BVH AABB; static decals should only rebuild geometry when decals or overlapping receivers change rather than every frame.
  - `DecalCameraUniformSize` and `DecalWGSL`'s `DecalCamera` block must stay byte-compatible with `render3d.CameraUniformSize` and `render3d.CameraWGSL`.

## Verification

- `go tool nsgo test ./scene3d/...` runs unit and GPU pixel-readback tests covering box clipping and UV mapping, normal-angle fade and back-face/perpendicular rejection, smooth vertex normals and depth fade, `LayerMask` and visibility exclusion, pool capacity/priority/FIFO eviction and `FadeIn`/`FadeOut` expiration, `testing.AllocsPerRun == 0` on steady-state `Spawn`+`Step`+`BuildGeometry`, `gpu.NewSoftwareRenderer` depth-tested pixel readback (including excluded layers, textured splats, and 55° angle-faded blending), and `gpu.View` pixel readback under both full and `nsgotest.NoGPUCompute()` (WebGL 2 / GLES 3.0) profiles.
- `go tool nsgo check -target macos,windows,linux,android,web` verifies cross-platform compilation and vetting across all five targets.
- `bun run verify-notes` validates this note's tree placement and format.
