# Agent Note: 2D sprite batches and screen layers over a 3D GPU view

Status: implemented

## Problem

Games using NSGo's 3D `gpu.View` need to render 2D HUD elements—sprite batches, screen-space `scene.Layer`s, crosshairs, and world-tracked markers—directly over the 3D scene without re-implementing 2D rendering in WGSL shaders or breaking pointer interaction with the underlying 3D view. At the same time, virtual resolution scaling (`screen.Viewport`) defaults zero `BarColor` to opaque black, which would cover the 3D scene in letterbox/pillarbox regions if applied naively.

## Decision

`packages/game/scene/overlay3d.go` adds `Overlay3D`, `Scene.DrawScreenLayers`, and `ProjectToOverlay` to compose 2D layers over a 3D `gpu.View` using NSGo's native view-tree stacking:

- **View-tree composition (`Overlay3D.View` and `Overlay3D.Canvas`)**: `Overlay3D.Canvas(cx)` returns an absolutely positioned, backgroundless `core.Canvas` (`Top: 0, Left: 0, Right: 0, Bottom: 0`) that fills its parent bounds. `Overlay3D.View(cx, props)` prepends that canvas to `props.Children` before calling `gpu.View(cx, merged)`, placing the 2D overlay directly above the 3D GPU surface and beneath any native NSGo HUD views supplied in `props.Children`. When `Env.SupportsGPU()` is false, `gpu.View`'s fallback container still mounts and renders both the 2D overlay canvas and native HUD children.
- **Deterministic draw order and `SpaceWorld` filtering**: `Overlay3D.Draw` merges visible `SpaceScreen` layers from `Overlay3D.Scene` (skipping `SpaceWorld` layers because the 3D view supplies the world) with visible layers in `Overlay3D.Layers` in stable ascending `Layer.Order`, then draws standalone `Overlay3D.Batches` in slice order, and finally invokes `Overlay3D.OnDraw` inside `Save`/`Restore`. `Scene.DrawScreenLayers` exposes the same `SpaceScreen`-only drawing for custom canvases.
- **Transparent-by-default virtual viewports**: When `Overlay3D.Viewport` is set, `Overlay3D.Draw` copies the `screen.Viewport` value and substitutes `core.Transparent` whenever `BarColor == 0` so letterbox/pillarbox margins leave the 3D scene visible without mutating the caller's struct. Callers who explicitly set a non-zero `BarColor` (such as `core.RGB(0x000000)`) still get opaque bars.
- **Non-blocking pointer routing by default**: `Overlay3D.Canvas` sets `core.PointerEventsNone` unless `Interactive` is true or `OnPress` is non-nil, ensuring clicks and touches pass straight through to `gpu.View` by default. `HitTest`, `HitTestAll`, `DispatchPress`, and `DispatchTouch` test standalone batches and merged layers in reverse draw order (converting through `Viewport.ViewToGame` when set) so games can either opt into canvas interaction or dispatch 2D hits before 3D raycasting.
- **On-demand invalidation**: `Overlay3D` binds an internal `core.Signal[uint64]` to the mounting `*core.Scope` and reads it inside `Draw`. Static 2D overlays trigger zero view-tree commits while `gpu.View.OnFrame` runs off-commit; dynamic overlays call `Overlay3D.Invalidate()` (or `SetHidden`) to redraw.
- **3D-to-2D world projection without experimental dependencies**: `game/scene` is a stable package and cannot import experimental `game/math3d` or `game/scene3d` (enforced by `TestStabilityTable`). `ProjectToOverlay(viewProj [16]float32, worldX, worldY, worldZ, viewWidth, viewHeight float32)` accepts a column-major `[16]float32` view-projection matrix with `[0, 1]` clip depth—directly assignable from `math3d.Mat4` / `scene3d.Camera.ViewProj()` without conversion—and `Overlay3D.Project` additionally maps visible screen coordinates into `Viewport` virtual space via `ViewToGame`.

## Alternatives considered

### Rendering 2D sprites inside the 3D GPU render pass

Drawing 2D sprite batches and `scene.Layer`s as textured quads inside the 3D `gpu.Frame` render pass would combine 3D and 2D into a single GPU submission and allow custom 3D post-processing over 2D elements. It was rejected because `game/scene` layers also contain arbitrary `core.Drawing` drawables, vector shapes, masks, and `sprite.Batch` composite modes that already execute on NSGo's native 2D canvas renderer; re-implementing `core.Drawing` in WGSL would duplicate NSGo's 2D engine and break feature parity with 2D scenes.

### Wrapping `gpu.View` and `core.Canvas` in a separate outer `core.View` container

Stacking `gpu.View` and `core.Canvas` as siblings inside a new wrapper `core.View` would avoid touching `gpu.ViewProps.Children`. It was rejected because NSGo's `gpu.View` is explicitly designed to host overlay `Children` over its GPU surface and preserve layout/fallback props (`Style`, `Ref`, `OnLayout`, `TestID`, `NoGPU` fallback) on one node. Mounting the overlay canvas as the first child of `gpu.View` keeps native HUD views in `props.Children` stacked cleanly above the 2D canvas without adding an extra layout container; callers who prefer sibling stacking can still mount `Overlay3D.Canvas(cx)` manually.

## Consequences

- **Benefits**:
  - Any existing `sprite.Batch`, `scene.Layer`, `scene.Drawable`, `screen.Viewport`, or `scene.Scene` screen layer works unchanged over a 3D `gpu.View` across all targets (including WebGL 2 / OpenGL ES 3.0 and `NoGPU` fallbacks).
  - Keeping the overlay in `packages/game/scene/overlay3d.go` avoids modifying existing `scene` files, preserves `game/scene`'s stable-package boundary (importing only stable `game/screen`, `game/sprite`, `nsgo/core`, and `nsgo/gpu`), and introduces no circular imports.
  - Zero per-frame UI commits when the 2D overlay is static.
- **Costs & limits**:
  - 2D overlay elements always composite on top of the entire 3D surface; they cannot depth-interleave with 3D geometry (use `render3d.BillboardRenderer` when 3D depth occlusion is needed).
  - Callers updating animated 2D sprites or world-projected markers over a 3D view must call `Overlay3D.Invalidate()` (or read a reactive signal in `OnDraw`/`Drawable.Draw`) to trigger canvas redraws.

## Verification

- `packages/game/scene/overlay3d_test.go` exercises 3D mesh rendering in `gpu.View` (both default and `nsgotest.NoGPUCompute()` profiles) alongside `gpu.NewSoftwareRenderer`, verifying view hierarchy, transparent canvas background, `GPUReadPixels` + 2D `DrawOp` alpha-composited pixels, layer ordering, visibility/hidden toggling, `Viewport` transparent-bar scaling, 3D-to-2D projection accuracy, pointer pass-through vs interactive hit testing, and `NoGPU` fallback.
- Cross-target static checks (`go tool nsgo check -target macos,windows,linux,android,web`), `go tool nsgo test ./scene/... .`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`.
