# Agent Note: Clustered forward Metal MSL compatibility, storage-buffer detection, and desktop GPU benchmark

Status: implemented

## Problem

Running `ClusteredLightSystem` with 256 animated point lights on a physical desktop GPU (MacBook Pro, Apple M4 Pro, Metal 4, macOS arm64, `2560 × 1440`, 120 Hz) to complete #382 revealed two real-hardware defects that headless software-renderer tests could not catch:

1. **Apple Metal MSL forward-reference compilation failure (`light.go`)**: `LightWGSL` declared `shade_pbr` calling `shade_pbr_shadowed`, which in turn called `shade_pbr_visibility` declared below both. Because NSGo v0.3.0's WGSL-to-MSL translator emits functions in source order without forward declarations, Metal rejected every `LightWGSL`-based pipeline with `use of undeclared identifier 'F4'` / `'F5'`.
2. **Overly strict capability gate (`clustered.go`)**: `DeviceSupportsClusteredLighting` required `caps.Features.Compute && caps.Features.StorageBuffers`. Because NSGo v0.3.0 on Apple Metal exposes `Features.StorageBuffers = true` (`MaxStorageBufferBindingSize = 128 MiB`) and `Features.Compute = false`, every macOS/iOS Metal device silently fell back to the 16-light uniform-buffer path even though `ClusteredLightSystem` performs cluster binning on the CPU and uses zero compute shaders.

## Decision

1. **Topological helper declaration order in `LightWGSL` (`packages/game/render3d/light.go`)**: declared `shade_pbr_visibility` first, then `shade_pbr_shadowed`, then `shade_pbr`, and added `TestLightAndClusteredWGSLNoForwardCalls` in `packages/game/render3d/clustered_test.go` to ensure no WGSL helper in `LightWGSL` or the clustered modules calls a function declared later.
2. **Storage-buffer-only capability gate (`packages/game/render3d/clustered.go`)**: updated `DeviceSupportsClusteredLighting` to check `caps.Features.StorageBuffers && caps.Limits.MaxStorageBufferBindingSize >= clusterMinStorageBindingBytes`. Devices without storage buffers (WebGL 2, OpenGL ES 3.0, Linux `GtkGLArea`, and `nsgotest.NoGPUCompute`) still take the uniform-buffer fallback automatically.
3. **Desktop GPU verification (`docs/en/render3d.md`, `docs/zh/render3d.md`)**: measured 256 animated clustered point lights on Apple M4 Pro Metal 4 at `2560 × 1440` (`clustered=true`, ~250 visible lights, ~475 active clusters): sustained `120.0 fps` (matching the 120 Hz display) with ~145 µs/frame CPU cluster assignment and upload, and visually verified the 16×16 colored point-light grid render.

## Alternatives considered

- **Wait for NSGo's MSL emitter to emit forward declarations**: would leave all `LightWGSL` and `ClusteredLitWGSL` pipelines broken on Apple Metal under NSGo v0.3.0. Reordering the three helpers is zero-cost, spec-compliant WGSL, and immediately fixes Metal compilation.
- **Keep requiring `Features.Compute` in `DeviceSupportsClusteredLighting`**: prevents Apple Metal desktops from running the clustered path even though no compute shaders are dispatched. Removing the unused compute check matches what the shader actually uses (`var<storage, read>` in the fragment stage).

## Consequences

- **Benefits**: `LightWGSL` and `ClusteredLitWGSL` compile and run cleanly on Apple Metal; desktop Apple Silicon holds 120 fps at `2560 × 1440` with 256 clustered point lights; `TestLightAndClusteredWGSLNoForwardCalls` guards against future Metal forward-reference regressions.
- **Costs**: None; `nsgotest.NoGPUCompute()` still disables `StorageBuffers` as well, so all fallback tests and WebGL 2 / OpenGL ES 3.0 / Linux `GtkGLArea` behavior remain intact.

Related: [2026-11-24-render3d-clustered-forward.md](../feature/2026-11-24-render3d-clustered-forward.md)
