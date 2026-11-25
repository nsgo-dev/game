# Agent Note: Per-layer tilemap Y-sorting with scene sprites

Status: implemented

## Problem

Top-down RPGs and isometric games draw tall tiles (trees, walls, pillars) behind a character when the character stands above the tile's trunk line and in front when the character walks below it (#851). Previously `game/scene` treated every `tilemap.Tilemap` as a single `drawEntry` at `-math.MaxFloat32` under `SortY`, so characters were always drawn either entirely above or entirely below an entire tilemap. Per-layer Y-sorting must work with camera frustum culling, orthogonal and isometric (as well as staggered and hexagonal) maps, animated tiles, per-tile sort origin properties, and zero heap allocations per frame in steady state for a 100×100 Y-sorted layer.

## Decision

Add per-layer Y-sorting primitives in `packages/game/tilemap` (`packages/game/tilemap/ysort.go`) and integrate them into `scene.Layer`'s `SortY` draw list in `packages/game/scene` (`depth.go`, `scene.go`, `parallax.go`):

- **Per-layer flag and per-tile sort origin (`tilemap`)**:
  - `TileLayer.YSort` (plus `Tilemap.SetLayerYSort` and `Tilemap.HasYSortLayers`) enables Y-sorting per tile layer. `LoadTiled` automatically enables `YSort` when a tile layer carries a boolean custom property named `y_sort`, `ysort`, `ySort`, or `y-sort`.
  - Each visible tile's `SortY` is its world-space bottom edge (`offY + tileOriginY + TileHeight`) plus an optional vertical offset from tile custom property `PropSortOriginY` (`"sort_origin_y"`, aliases `"y_sort_origin"` and `"ysort_origin"`), also readable/writable via `Tilemap.TileSortOriginY` / `Tilemap.SetTileSortOriginY`. Negative offsets move the sort line upward (for example into a tall tree's trunk or an isometric diamond's center).
  - `Tilemap.AppendYSortTiles(dst, layer, cam)` visits only tiles overlapping `cam.VisibleBounds()` in deterministic painter's order across orthogonal, isometric, staggered, and hexagonal orientations, resolves animated tiles at collection time (checking the base tile's `PropSortOriginY` first), and appends compact `YSortTile` values to the caller-owned slice `dst` with zero heap allocations once warm.
  - `maxExtraTileSize()` also inspects `Tileset.Frames` so tall atlas tiles in `NewAtlasTileset` (whose `TileHeight` defaults to frame 0) are never prematurely culled when their canopy enters the viewport.
  - `Tilemap.DrawLayer` bypasses any `LayerCache` when `layer.YSort` is true, sorting and drawing tiles via reusable `Tilemap.ysortBuf` and `Tilemap.ysortDrawer`.
- **Atlas run-batching (`YSortDrawer`)**:
  - `YSortDrawer` queues consecutive unflipped `YSortTile`s sharing the same `*Tileset` and `opacity` into reusable `[]core.Bounds` and `[]core.RSXform` buffers, flushing runs of 2+ tiles as a single `Drawing.DrawAtlas` op and single or flipped tiles via `YSortTile.Draw` (`Drawing.DrawImageFrom`).
  - This matches `sprite.Batch.Draw` and avoids emitting thousands of individual `DrawImageFrom` ops (each of which causes NSGo v0.3.0 to heap-allocate a 96-byte `&ImageSource`) for contiguous runs of tiles between sprites.
- **Scene integration (`scene`)**:
  - `Layer.drawWithParallax` builds `l.entries` after computing the effective parallax/repeat `tilemap.Camera` (`&pcam` or `&tileCam`) so Y-sorted tile collection uses the exact culled camera region. `HitTest` passes `collectTiles = false` so hit-testing skips tile expansion.
  - Under `SortY`, a `Tilemap` with Y-sorted layers is walked in exact map order via `Tilemap.VisitLayers` (using a stack `[64]bool` array for zero allocations): non-Y-sorted layers before the first Y-sorted layer receive `-math.MaxFloat32` (background), Y-sorted tile layers expand into `entryYSortTile` entries referencing a reusable `Layer.ysortTiles` slice by index (keeping `drawEntry` small), and trailing non-Y-sorted layers after the last Y-sorted layer receive `+math.MaxFloat32` (foreground, such as roofs).
  - `Item.TileLayer` and `Layer.AddTileLayer(m, layer)` allow binding an individual `TileLayer` to a specific `scene.Layer`; `Layer.update` deduplicates `Tilemap.Update(dt)` calls per unique `*tilemap.Tilemap` so animated tiles never double-step.

### Invariants

- Package dependency order stays acyclic and Stable-only: `core` ← `sprite` ← `tilemap` ← `scene`.
- `AppendYSortTiles`, `SortYSortTiles`, `VisitLayers`, and `scene.Layer.buildDrawEntries` perform strictly 0 heap allocations per frame once scratch slices are warmed up.
- `drawEntries` always flushes `Layer.ysortDrawer` before any non-tile entry and at the end of the loop, and clears `l.entries` and `l.ysortTiles` after drawing so no image or tileset pointers are retained across frames.
- Maps with no `YSort: true` layers keep the exact pre-existing `entryTilemap` path and `LayerCache` behavior.

## Alternatives considered

### Why not embed `tilemap.YSortTile` directly inside `scene.drawEntry`?

Embedding `YSortTile` by value inside `drawEntry` avoids an indirection into `Layer.ysortTiles`. However, `YSortTile` carries source/destination rects, flip flags, opacity, and a `*Tileset` (~88 bytes), which would inflate every sprite and drawable `drawEntry` and copy larger values on every swap inside `slices.SortStableFunc`. Keeping a compact `tileIdx int` inside `drawEntry` and storing `YSortTile` in a separate reusable `Layer.ysortTiles` slice keeps sorting cache-friendly and zero-allocation.

### Why not emit `core.DrawOp{Kind: core.DrawImage, Image: &ts.Image}` directly to bypass NSGo's `DrawImageFrom` allocation?

Appending `core.DrawOp` directly to `d.Ops` with a pointer to `ts.Image` would avoid NSGo v0.3.0's per-call `&img` heap copy in `Drawing.DrawImageFrom`. However, `Drawing.op` attaches unexported `d.sampling` (configured via `SetImageSampling`, such as `SamplingNearest` for pixel art) to every image op; bypassing `Drawing`'s public methods would silently break nearest-neighbor sampling and rely on `DrawOp` internals. Using `YSortDrawer` with `Drawing.DrawAtlas` stays strictly on NSGo's public API while collapsing 10,000 tile draw ops into 1 batched op (or one run per sprite boundary).

### Why not require callers to split every map into separate `Tilemap` instances for ground, Y-sorted props, and roofs?

Forcing separate `Tilemap` values would avoid splitting layers inside `scene.Layer`. However, Tiled maps commonly author `ground`, `trees` (`y_sort: true`), and `roofs` in one `.tmj` file. Supporting both whole-map `AddTilemap` (splitting automatically around `YSort` layers) and single-layer `AddTileLayer` handles both workflows cleanly.

## Consequences

- **Benefit:** Characters walk smoothly behind and in front of tall tiles on both orthogonal and isometric maps, with optional per-tile `sort_origin_y` trunk lines, camera culling, and animated tiles.
- **Benefit:** Y-sort collection, sorting, and scene draw-list assembly run at 0 B/op and 0 allocs/op on a 100×100 layer once warm; `YSortDrawer` batches contiguous unflipped tile runs into `DrawAtlas` ops.
- **Cost:** Setting `YSort: true` on a tile layer bypasses `CacheStaticLayer` (by necessity, since tiles interleave with dynamic sprites). Layers with `Opacity < 1` and flipped tiles flush individual `Save`/`Restore` ops around those tiles.

## Testing

- `packages/game/tilemap/ysort_test.go` verifies orthogonal default bottom-edge sorting, `PropSortOriginY` (`sort_origin_y`, `y_sort_origin`, `ysort_origin`), `LayerCache` bypass, isometric/staggered/hexagonal sorting and culling, tall atlas tile culling, animated tiles with flips and partial opacity, Tiled property loading, strict 0-alloc `AppendYSortTiles`+`SortYSortTiles` on a 100×100 layer, and `YSort` benchmarks.
- `packages/game/scene/ysort_test.go` and `packages/game/scene/ysort_alloc_test.go` verify a character walking behind and in front of a tall tree tile (both default bottom edge and custom trunk `sort_origin_y`) on orthogonal and isometric maps, ground/roof layer sandwiching, camera culling, animated tiles without double-update under `AddTileLayer`, deterministic tie-breaking, and 0-alloc `buildDrawEntries` for 100×100 layers with sprites.
- Verified with `go tool nsgo test ./tilemap/...`, `go tool nsgo test ./scene/...`, root stability/skill tests, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
