# Agent Note: Tiled Wang sets and multi-terrain autotiling in game/tilemap

Status: implemented

## Problem

`game/tilemap` previously shipped only code-defined single-terrain bitmask autotiling (`AutotileSet`, `ApplyAutotile`, `SetAutotile`) for 4-bit and 8-bit corner-gated blobs. Maps with multiple terrains (such as grass, dirt, water, and cliffs) had to hand-maintain Go tables instead of reusing the `wangsets` (`corner`, `edge`, and `mixed`) and per-tile `probability` weights authors already configure in Tiled (#850).

## Decision

Extend `game/tilemap` with Tiled JSON Wang set loading (`tiled.go`, `tilemap.go`) and Tiled-compatible multi-terrain painting (`wang.go`) while keeping `AutotileSet` intact and preserving `game/tilemap`'s Stable status and WASM size budget:

- **Tiled JSON parsing (`packages/game/tilemap/tiled.go`, `tilemap.go`)**:
  - Parse per-tile `probability` into `Tileset.TileProbabilities` (with `Tileset.TileProbability(localID)` defaulting to `1`).
  - Parse `wangsets` from both embedded `.tmj` tilesets and external `.tsj` tilesets into `Tileset.WangSets []WangSet` (`WangCorner`, `WangEdge`, `WangMixed`), carrying `Name`, `Type`, `Tile` (default `-1`), `Class`, `FirstGID`, `Colors []WangColor` (`Name`, `Color`, `Tile`, `Probability`, `Class`, `Properties`), `WangTiles []WangTile` (`TileID`, `WangID [8]uint8` in Tiled's clockwise `[Top, TopRight, Right, BottomRight, Bottom, BottomLeft, Left, TopLeft]` order, `Probability`), and `Properties`.
  - Validate Wang set `type`, hex `color`, non-negative finite `probability`, 8-entry `wangid` length, and color-index bounds via `*loaderr.Error` pointing at the exact embedded `.tmj` or external `.tsj` field.
  - Expose `Tileset.WangSet(name)`, `Tileset.WangSetAt(index)`, `Tilemap.WangSet(name)`, `WangSet.ColorIndex(name)`, and `WangSet.WangIDForTile(localTileID)`.
- **Multi-terrain autotiling (`packages/game/tilemap/wang.go`)**:
  - Add `TerrainRand` (`interface { Float64() float64 }`, satisfied by `*random.Stream`) plus `Tilemap.SetTerrain(layerIndex, col, row, set, terrain, rng)` and `Tilemap.SetTerrainRect(layerIndex, col, row, w, h, set, terrain, rng)`.
  - Lazily build and cache an immutable `wangIndex` on `WangSet` (invalidated automatically if `Type`, `len(WangTiles)`, or `len(Colors)` changes) containing active-slot masks, `localTileID -> WangID` lookup, `TileID`-sorted candidate order, and an all-pairs shortest-path color distance table (Floyd-Warshall over `0..maxColor`) matching Tiled's transition penalty matrix.
  - Paint the target cell(s) in deterministic row-major order, then run bounded BFS wave propagation outward (processing cardinal neighbors before diagonal neighbors in each wave so diagonal cells observe both adjacent cardinal cells settled). Each neighbor minimizes `(hardPenalty, softPenalty)` against settled neighbor slots (hard) and its existing outer slots (soft), automatically producing seamless direct 2/3-terrain transitions as well as multi-hop rings (such as Water -> Dirt -> Grass when no direct Grass-Water tiles exist).
  - Read existing flipped tiles on the layer through `transformWangID` (diagonal, horizontal, vertical in Tiled's order) so neighbor constraints honor flipped tiles, and never overwrite non-Wang tiles during neighbor correction.
  - Only materialize tiles in previously empty cells when the WangSet explicitly contains `0` (empty) in active slots (`hasZeroColor`) and an exact empty-transition match exists; erasing (`terrain == 0`) clears painted cells and any orphaned transition neighbors whose active slots all become `0`.
  - Break ties among equal-scoring best matches by tile probability (scaled by unconstrained color probability, matching Tiled) using `TerrainRand` (e.g. `*random.Stream`, or deterministically by lowest positive-weight `TileID` when `rng == nil`).
  - Route all tile writes through `Tilemap.SetTile`, so dense layers, sparse chunked layers (including negative coordinates), static layer/chunk cache invalidation, and `TileRevision` edit logs work uniformly.

### Invariants

- `game/tilemap` remains strictly Stable and imports only Stable module packages (`game/sprite`, `game/loaderr`) plus `nsgo/core` (no dependency on `game/random`).
- Because Tiled diamond isometric grids rotate the square grid 45° clockwise, the 8 clockwise `WangID` indices map to the exact same `(dx, dy)` grid neighbor offsets for both `OrientationOrthogonal` and `OrientationIsometric`.
- `AutotileSet`, `ApplyAutotile`, and `SetAutotile` are unchanged.

## Alternatives considered

### Import `game/random` directly and take concrete `*random.Stream`

Taking a concrete `*random.Stream` would avoid defining the single-method `TerrainRand` interface. Rejected because pulling `game/random` into `game/tilemap` adds unnecessary WASM binary weight for tilemap callers that do not use `SetTerrain`, whereas `type TerrainRand interface { Float64() float64 }` accepts `*random.Stream` directly with zero extra import edges.

### Restrict neighbor updates to a single 1-cell ring around the painted cells

Updating only immediate neighbors without BFS wave propagation is simpler when every terrain pair has direct transition tiles. Rejected because real 3+ terrain tilesets frequently omit direct transitions between incompatible terrains (for example Water <-> Dirt <-> Grass with no Grass-Water tile), and Tiled's terrain brush uses shortest-path color distances with multi-ring corrections to insert the intermediate shoreline ring seamlessly.

### Store terrain grids separately from tile GIDs on `TileLayer`

Keeping a parallel terrain-ID grid on `TileLayer` would avoid mapping GIDs back to `WangID`s. Rejected because loaded Tiled `.tmj` maps store only tile GIDs, multiple WangSets can target the same layer, and inspecting existing tiles' `WangID`s directly matches Tiled's own `WangFiller` model with zero extra memory per layer.

## Consequences

- **Benefit:** Tiled Wang sets (`corner`, `edge`, `mixed`) and tile probabilities load directly from `.tmj`/`.tsj` files and drive seamless multi-terrain painting on orthogonal and isometric maps, dense and chunked layers, with seed-reproducible variant selection.
- **Benefit:** Multi-hop transitions (such as Grass-Dirt-Water shorelines) resolve automatically without hand-written Go transition tables.
- **Cost:** `Tileset` grows by `TileProbabilities` and `WangSets` fields (nil when unused), and `TerrainRand` is a small single-method interface in `game/tilemap`.

## Testing

- `packages/game/tilemap/wang_test.go` verifies embedded `.tmj` and external `.tsj` Wang set loading, `*loaderr.Error` diagnostics for malformed Wang sets/probabilities, a 3-terrain golden test matching Tiled's terrain brush (single-cell, multi-terrain corners, rect equivalence, repaint restoration), 2-hop Grass-Dirt-Water shoreline propagation, `corner`/`edge`/`mixed` sets, `OrientationIsometric` diamond geometry alignment, sparse chunked negative coordinates, probability-weighted `*random.Stream` determinism and `nil`-rng lowest-`TileID` fallback, zero-color painting and erasing, and flipped/non-Wang neighbor preservation.
- Verified with `go tool nsgo test ./tilemap/...`, `go run ./cmd/sizecheck -kind wasm -budget ../../benchmarks/web-budget.json`, `go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid'`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
