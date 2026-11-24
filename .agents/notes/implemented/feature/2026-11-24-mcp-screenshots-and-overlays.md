# Agent Note: MCP game-view screenshots with aligned diagnostic overlays

Status: implemented

## Problem

Under `nsgo dev -automation` and `nsgo test`, `game/mcp` (introduced in [MCP scene, layer, entity and component inspection](2026-11-24-mcp-scene-entity-inspection.md)) exposes structured scene, layer, entity and component state, but coding agents cannot visually verify rendered frames or check whether sprites align with their bounds and 2D physics collision shapes (#99). Capturing a native OS screenshot also misses games running headlessly and cannot inject sprite-bounds or physics-shape overlays aligned with the game's camera and viewport transforms.

## Decision

Add a `game_screenshot` tool in `packages/game/mcp/screenshot.go` (registered alongside existing tools via `Bridge.Tools()`) that captures the game view into a deterministic `*image.RGBA` and composites optional pixel-aligned diagnostic overlays (`bounds`, `collision_shapes`/`physics`, `grid`, `camera`, `labels`) using the exact same `screen.Viewport` and `tilemap.Camera` transforms as the rendered frame, returning PNG bytes inline as `png_base64` or writing them confined inside `Config.ScreenshotDir`.

## Architecture and contracts

- **Frame acquisition**: `Config` adds optional `CaptureFrame`, `DrawFrame`, `Viewport`, `Physics`/`PhysicsFunc`, `RigidPhysics`/`RigidPhysicsFunc`, `ScreenshotDir`, and `ScreenshotBackground` fields. When `CaptureFrame` is configured, `game_screenshot` clones the returned `*image.RGBA` (never mutating the caller's buffer) before compositing overlays. Otherwise it records the frame into a `core.Drawing` via `DrawFrame` or the active `Scenes`/`Scene` wrapped in `Viewport.Draw` when set, and rasterizes the ops with a self-contained software rasterizer.
- **Self-contained software rasterizer (no `gametest` import)**: `game/mcp` rasterizes `core.DrawOp` (transforms, `Save`/`Restore`, rectangular `Clip`, rects, lines, arcs, fills, strokes, `DrawImage` with tint, and `DrawAtlas`) directly in `screenshot.go` instead of importing `packages/game/gametest`. Importing `gametest` from non-test game packages would transitively import `testing` and register the `-update-golden` CLI flag in game binaries, and `gametest.RenderOps` does not clip `screen.Viewport` bars or rasterize path lines/arcs.
- **Exact pixel-aligned overlay geometry**:
  - Viewport transform and clip rect come directly from `Viewport.Layout(window)`, clipping overlays out of letterbox/pillarbox bars.
  - Camera transform folds `tilemap.Camera.Apply` ops into the same 2D affine matrix (`cameraAffine2D`), so shake, zoom, rotation, parallax (`cam.Parallax(px, py)` plus layer `OffsetX`/`OffsetY` and repeat ranges) and `SpaceScreen` HUD layers match `scene.Scene.Draw` identically.
  - Axis-aligned rectangles (`sp.Bounds()`, `physics.Box`, solid tiles) compute exact inclusive pixel edges (`Ceil(min - 0.5)` to `Ceil(max - 0.5) - 1`) matching pixel-center `(px + 0.5, py + 0.5)` rasterization coverage so 1-pixel outlines sit cleanly on sprite/box borders without asymmetric 2-pixel bleed.
  - Collision shape colors match `physics.DefaultDebugStyle()` with explicit hex fallbacks so overlays remain visible regardless of build tags (even when `physics`'s debug draw stubs out under `-tags release`).
- **Strict argument decoding and confined disk writes**: Both top-level flags (`bounds`, `collision_shapes`, `physics`, `grid`, `camera`, `labels`) and the nested `overlays` object refuse unknown fields (`DisallowUnknownFields()`), invalid dimensions, unsupported formats, and non-positive `grid_step`. When `path` is supplied, `Config.ScreenshotDir` must be configured, relative slash paths are validated against absolute/traversal segments, and writes go through `os.OpenRoot` so symlinks cannot escape `ScreenshotDir`.

## Alternatives considered

### Reusing `gametest.RenderOps` directly from `packages/game/mcp`

Reusing `gametest.RenderOps` would avoid duplicating basic affine image/rect rasterization code. It was rejected for two reasons: `packages/game/gametest` imports `testing` and defines a global `flag.Bool("update-golden", ...)`, which would pollute any game binary importing `game/mcp`, and `RenderOps` omits `DrawClip`, `DrawMoveTo`/`DrawLineTo`/`DrawArc`, and `DrawTintedImage` needed for `screen.Viewport` letterbox clipping and custom drawables.

### Calling `physics.DrawWorld` and `debug.ShapeOverlay` into a `core.Drawing` for overlays

Recording overlays through `physics.DrawWorld` and `debug.ShapeOverlay` would delegate shape geometry directly to existing debug packages. It was rejected because both `physics/debugdraw_release.go` and `debug/release.go` compile to no-ops under `-tags release`, `debug` carries heavy UI/profiler dependencies, and generic centered `StrokeRect` rasterization produces asymmetric 2px left/top vs 1px right/bottom borders on integer pixel grids; matching `physics.DefaultDebugStyle()` colors while computing exact inclusive border pixels keeps overlays crisp, aligned, and dependency-light.

### Stateful `game_overlays_config` tool mutating persistent bridge flags

Exposing a separate stateful toggle tool would let agents turn overlays on once and call `game_screenshot` without repeating flags. It was rejected in favor of stateless per-call arguments (supporting both flat booleans and an `overlays` object) so concurrent or multi-step agent inspections never suffer hidden cross-call overlay state leakage.

## Consequences

- **Benefits**: Coding agents can capture PNG screenshots of headless or windowed games with pixel-accurate sprite bounds, physics collision shapes, triggers, tiles, rigid bodies, grids, camera frames and labels aligned across camera pan/zoom/parallax and viewport scaling/letterboxing. Parallel MCP tool PRs remain isolated because `mcp.go` only receives additive `Config` fields and one append in `Bridge.Tools()`.
- **Costs**: `packages/game/mcp/screenshot.go` maintains a focused software rasterizer for common 2D `core.DrawOp` primitives; exotic canvas ops such as gradients, shadows or custom GPU shaders still require providing `Config.CaptureFrame` to supply the base pixels before overlays are composited.

## Verification

Verified with `go tool nsgo test ./mcp`, `go tool nsgo test -run TestStabilityTable .`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, `gofmt`, and `bun run verify-notes`.
