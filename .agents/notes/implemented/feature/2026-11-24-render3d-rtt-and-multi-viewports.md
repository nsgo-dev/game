# Agent Note: Render-to-Texture Cameras and Multiple Viewports in render3d

Status: implemented

## Problem

`render3d` previously exposed a single `CameraBuffer` and uploaded static `GPUTexture` images, leaving three multi-camera 3D workflows unaddressed (#381, parent #243):

1. Offscreen cameras (mirrors, minimaps, portals, security monitors) need a scope-owned render target whose color output plugs directly into `render3d.BindTexture` and material texture slots, including optional depth, 4x MSAA resolve, dynamic `Resize`, and `Retained` device-loss restoration.
2. Each camera must filter which `scene3d.Node.Layer` bitmasks it renders (for example, hiding a mirror quad from its own reflection camera or rendering player-specific overlays).
3. Multiple 3D viewports (side-by-side split screen, 4-player quad split, and picture-in-picture overlays) must render within one `gpu.Frame` or `gpu.Encoder` without clobbering each other's camera uniforms, distorting aspect ratios, or bleeding pixels across split seams.

## Decision

`packages/game/render3d/viewport.go` adds offscreen render textures, normalized split-screen viewports, zero-allocation layer filtering, and multi-view GPU pass orchestration without modifying existing `render3d/*.go` files:

- **Scope-owned `RenderTexture` wrapping `gpu.RenderTarget`**: `NewRenderTexture` validates dimensions, non-depth color format (defaulting to `gpu.FormatRGBA8Unorm`), optional depth/stencil format, and MSAA sample count (0/1 or 4) before allocating via `dev.CreateRenderTarget`. `RenderTexture.GPUTexture()` exposes a `*GPUTexture` backed by `target.Resolve` when `SampleCount == 4` or `target.Color` otherwise, so offscreen renders plug directly into `render3d.BindTexture` and material texture bindings. `Resize(dev, w, h)` allocates the replacement target before closing the old one, increments `Generation()`, and replaces the `*GPUTexture` wrapper so stale cached handles report `Valid() == false`.
- **Per-view retained camera uniform pool in `ViewportRenderer`**: Because `gpu.Device.WriteBuffer` executes on the queue timeline before submitted command buffers begin, reusing a single `CameraBuffer` across multiple passes in one frame would overwrite earlier cameras' matrices. `ViewportRenderer` maintains a shared `CameraLayout()` (or borrows one via `NewViewportRendererWithLayout`) and allocates one retained uniform buffer + bind group slot per active view, uploading all camera uniforms before pass recording.
- **Deterministic offscreen-first scheduling and pass sharing**: `OrderCameraViews` sorts active (non-disabled) views stably with offscreen targets (`Target != nil`) before main-surface views (`Target == nil`) and ascending `Priority` within each phase. Consecutive views sharing the same target reuse one `gpu.RenderPass` (avoiding extra load/store passes on tile-based GPUs), switching only `SetViewport`, `SetScissor`, and `CameraGroup`. Setting `CameraView.ResetDepth` ends the prior pass and begins a continuation pass with `LoadLoad` color and `LoadClear` depth for overlapping picture-in-picture viewports.
- **Edge-rounded `ViewportRect` tiling and non-mutating aspect adjustment**: `ViewportRect.Resolve` rounds left/right and top/bottom pixel edges independently (`x0 = round(X*W)`, `x1 = round((X+Width)*W)`) so arbitrary N-way splits (`SplitScreenHorizontal`/`SplitScreenSideBySide`, `SplitScreenVertical`/`SplitScreenStacked`, `SplitScreenQuad`) tile non-divisible surface sizes with zero gaps and zero overlap, and `PixelViewport.Apply` sets both `SetViewport` and `SetScissor`. Each view renders a copy of `*CameraView.Camera` with `Projection.Aspect` matched to the resolved pixel viewport unless `PreserveAspect` is true, never mutating the caller's camera.
- **Additive layer filtering without editing `DrawItem`**: `LayeredDrawItem`, `LayerMatches`, `FilterLayeredItems`, and `FilterNodesByLayer` mirror `scene3d.Node.Layer` bitmasks with zero steady-state allocations when reusing the destination slice, while `CameraView.EffectiveLayerMask()` defaults zero to `scene3d.LayerAll` so struct literals render all layers unless restricted.

## Alternatives considered

### Reuse a single `CameraBuffer` across all viewports in a frame

Updating one `CameraBuffer` between `BeginRenderPass` calls would save uniform buffers, but `gpu.Device.WriteBuffer` runs before the recorded `CommandBuffer` executes on WebGPU, Metal, Vulkan, D3D11, and NSGo's software device; every pass in the encoder would read only the last camera's matrices. One retained uniform buffer slot per active view (reused across frames) is required for single-submit multi-camera frames.

### Add a `Layer uint32` field directly onto `render3d.DrawItem` in `queue.go`

Embedding `Layer` directly in `DrawItem` would allow `Queue.Build` to filter layers internally. It was deferred in favor of `LayeredDrawItem` + `FilterLayeredItems` because existing `DrawItem` literals leave new fields zero (requiring a special-case default in the hot `Queue.Build` loop) and keeping all changes in `viewport.go` avoids merge conflicts with parallel `render3d` work while staying zero-allocation before `Queue.Build`.

### Mutate `RenderTexture.gpuTex` in place during `Resize`

Updating `rt.gpuTex.tex` in place on `Resize` would keep the same `*GPUTexture` pointer, but any `*gpu.BindGroup` created earlier via `BindTexture` would still reference the closed `*gpu.Texture` while a caller checking `oldTex.Valid()` would falsely see `true`. Replacing `rt.gpuTex` and incrementing `Generation()` ensures stale texture handles report `Valid() == false` and forces explicit rebinding.

## Consequences

- **Benefits**:
  - Offscreen 3D cameras render directly into `RenderTexture` targets (including 4x MSAA resolve) usable verbatim with `render3d.BindTexture` and material slots, with `Retained` device-loss restoration and `Resize` support.
  - Per-camera layer masks and side-by-side/stacked/quad/PiP viewports execute cleanly in a single GPU frame submission with zero seam bleed and non-mutating camera aspect adjustment.
  - Verified on both full and `nsgotest.NoGPUCompute()` (WebGL 2 / GLES 3.0) profiles via headless pixel readback in `packages/game/render3d/viewport_test.go`, alongside `testing.AllocsPerRun == 0` checks for `FilterLayeredItems`, `FilterNodesByLayer`, and `OrderCameraViews`.
- **Costs**:
  - Callers that resize a `RenderTexture` (`resized == true` or `Generation()` changed) must recreate any `*gpu.BindGroup` built from the previous `GPUTexture()`.
  - Callers using `Queue.Build` with per-camera layer masks filter a `[]LayeredDrawItem` scratch slice via `FilterLayeredItems` before calling `Queue.Build` rather than passing a layer mask directly to `Queue.Build`.
