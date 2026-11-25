# Agent Note: Tile minimap with incremental SetTile updates and fog revision caching

Status: implemented

## Problem

`game/worldmap` previously supported only room-based metroidvania maps (`Map`). RTS, strategy, roguelike, and top-down RPG games need a terrain minimap backed directly by `tilemap.Tilemap` with `fog.Fog` visibility, unit/POI markers, a camera viewport frame, orthogonal and diamond isometric rendering, and click-to-pan (#855). Redrawing a 256×256 map when a single tile changes must take well under 1 ms, and frames where fog has not changed must skip recompositing the fog overlay.

## Decision

Add `TileMinimap` to `packages/game/worldmap` (`packages/game/worldmap/tileminimap.go`) and a minimal pull-based tile edit log to `packages/game/tilemap` (`TileRevision` and `TileChangesSince` in `packages/game/tilemap/runtime.go`):

- **Pull-based edit ring in `tilemap`**: `Tilemap` holds a lazy-allocated `*tileEditLog` (a 1024-entry power-of-2 ring buffer plus monotonic `rev` and `minRev`). `SetTile` and small `FillRect` calls record each changed `(col, row)` in O(1) with zero heap allocations; bulk `FillRect` calls larger than the ring capacity advance `minRev = rev` to trigger a full rebuild. `TileChangesSince(rev, fn)` replays only the changed cells since `rev` or returns `ok = false` on overflow.
- **Two-buffer caching in `TileMinimap`**: `terrain *image.RGBA` stores fog-free terrain composited across visible tile layers, and `composited *image.RGBA` (lazily allocated when fog is drawn) stores terrain with `fog.Fog` overlaid.
  - Tile colors resolve in order: optional `TileMinimapOptions.TileColor` callback → tile custom property (`DefaultTileColorProperty = "minimap_color"`, configurable) → alpha-weighted mean color of non-transparent pixels in the tile's tileset image, cached per stripped tile ID.
  - `Sync` skips fog recompositing whenever `(fogPtr, fogTeam, fog.Revision(team), fogStyle)` is unchanged. When a `SetTile` occurs on an unchanged-fog frame, `Sync` patches only the changed cells in both `terrain` and `composited` so single-tile updates stay O(1) even with fog active.
- **Solid diamond isometric layout**: Orthogonal maps use `W × H` pixels (1 pixel per tile). `tilemap.OrientationIsometric` maps use `(W+H) × (W+H-1)` pixels where each tile `(col, row)` owns two disjoint horizontal pixels `(col - row + H - 1, col + row)` and `(col - row + H, col + row)`, leaving pixels outside the diamond transparent. Drawn into `WorldBounds` inset vertically by `TileHeight/4`, every tile center aligns exactly with `Tilemap.TileToWorld(col, row)` under the same uniform `Projection` used by markers, the camera rectangle, `WorldToMinimap`, `MinimapToWorld`, and `PanCamera`.

### Invariants

- Package dependency direction remains strictly acyclic: `tilemap` ← `fog` ← `worldmap` (with `worldmap` importing `tilemap` and `fog`).
- `Tilemap` adds only one pointer field (`edits *tileEditLog`) and does not allocate it until `SetTile` or `FillRect` mutates a tile.
- Isometric diamond cells never overlap or leave checkerboard holes; corners outside the diamond stay transparent even when `fog.Fog` is active.
- `PanCamera` modifies `cam` only when `cam != nil`, `click` is finite and inside `dst`, and the minimap projection is non-empty, then calls `cam.Clamp()`.

## Alternatives considered

### Why not register a callback/listener on `Tilemap` for `SetTile`?

A callback (`OnTileChange(func(col, row int))`) would push edits directly to `TileMinimap` without a ring buffer. However, listeners require explicit unregistration to avoid leaking discarded minimaps, mutate `Tilemap` when attaching a view, and complicate multiple views sharing one map. A pull-based monotonic revision + ring buffer requires no teardown, supports any number of observers, and matches `fog.Fog.Revision`'s pull-based caching model.

### Why not place `TileMinimap` in a new `packages/game/minimap` package?

A separate package would isolate `tilemap` and `fog` dependencies from room-based `worldmap` callers. However, `worldmap` already owns `Projection`, marker styling conventions, and minimap documentation, and `worldmap` has no reverse dependents in `packages/game` that would create a cycle. Adding `TileMinimap` to `worldmap` keeps both metroidvania and strategy minimaps discoverable in one package and reuses `worldmap.Projection`.

### Why not average all pixels (including transparent corners) for tileset fallback color?

Averaging premultiplied RGBA over every pixel in the tile's bounding rectangle naturally blends sparse overlay tiles (like a tree sprite) over ground layers. For isometric diamond tiles, however, 50% of the rectangular tile bounds are transparent corners, which would dim solid ground tiles to 50% opacity against the minimap background. Averaging RGB weighted by alpha over non-transparent pixels (with alpha equal to the mean alpha of non-transparent pixels) keeps isometric ground tiles opaque while preserving translucency for semi-transparent tiles such as water.

### Why not use `(W+H-1) × (W+H-1)` with 1 pixel per isometric tile?

Mapping `(col, row)` to `(col - row + H - 1, col + row)` uses half the width, but `x + y = 2*col + H - 1` has constant parity, leaving checkerboard holes on every alternate pixel inside the diamond. Writing 2 horizontally adjacent pixels per tile in a `(W+H) × (W+H-1)` buffer fills the diamond solidly with zero overlaps and aligns horizontally with `WorldBounds` (`(W+H) * TileWidth/2`) with zero error.

## Consequences

- **Benefit:** Single-tile updates on a 256×256 map (orthogonal or isometric, with or without active fog) update O(1) pixels and redraw in microseconds—orders of magnitude under the 1 ms target.
- **Benefit:** Unchanged fog frames perform zero fog recompositing and keep `core.ImageSource.Version` stable, avoiding redundant GPU texture uploads.
- **Benefit:** `worldmap.Projection`, `MinimapToWorld`, and `PanCamera` work uniformly across both orthogonal and isometric maps.
- **Cost:** `worldmap` now imports `tilemap` and `fog`. Direct mutations to `TileLayer.Data` (bypassing `SetTile`/`FillRect`) or runtime changes to `TileLayer.Visible`/`Opacity` are not tracked automatically and require calling `TileMinimap.Invalidate()`.

## Testing

- `packages/game/tilemap/editlog_test.go` verifies `TileRevision` and `TileChangesSince` across `SetTile`, no-op/out-of-bounds edits, small `FillRect`, bulk `FillRect` overflow, and ring-buffer wraparound.
- `packages/game/worldmap/tileminimap_test.go` and `tileminimap_internal_test.go` verify tile property precedence vs. tileset average color (opaque, blended, PNG `Data`, atlas URI, isometric diamond transparent corners), multi-layer compositing, incremental `SetTile` updates, `fog.Revision` skipping, `RequireVisible` marker hiding, unrotated and rotated camera rectangles, orthogonal and isometric click-to-pan (`PanCamera` with and without `Camera.Bounds`), and `nsgotest` `core.Canvas` integration.
- `packages/game/worldmap/tileminimap_bench_test.go` benchmarks 256×256 single-tile redraws (orthogonal, orthogonal with fog, isometric, and unchanged fog frame).
- Verified with `go tool nsgo test` across `./worldmap/...`, `./tilemap/...`, `./fog/...`, root stability/skill tests, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
