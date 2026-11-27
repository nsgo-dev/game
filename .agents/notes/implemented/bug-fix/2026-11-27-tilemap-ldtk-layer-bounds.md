# Agent Note: Bound LDtk layer dimensions and tile allocations in pkg/tilemap

Status: implemented

## Problem

`pkg/tilemap/ldtk.go` trusted the grid dimensions declared in `.ldtk` files without checking for non-positive dimensions, integer overflow, or unbounded slice allocations (`nsgo-dev/game#1551`):

- `buildLDtkLevel` fell back to `m.Width` / `m.Height` when a layer's `__cWid` or `__cHei` was `<= 0`. When a level was narrower than one grid cell (`pxWid / gridSize == 0`), an `IntGrid` layer with any non-zero cell ran `idx % lw` with `lw == 0` and panicked with integer divide by zero.
- `buildLDtkTileData` allocated `make([]uint32, lw*lh)` with no upper bound or overflow guard. A 177-byte file with `__cWid: 20000, __cHei: 20000` allocated 1.5 GiB, and `__cWid: 4294967296, __cHei: 4294967297` overflowed `int` multiplication on 64-bit targets to allocate 16 GiB while leaving layer `Width`/`Height` inconsistent with `len(Data)`.
- `intGridCsv` could exceed `lw * lh` without error, and `pkg/tilemap` had CI fuzz targets for Tiled (`FuzzLoadTiled*`) but none for LDtk.

## Decision

`pkg/tilemap/ldtk.go` validates level pixel sizes, layer grid sizes, tileset ID ranges, `intGridCsv` lengths, and cumulative dense tile allocations before allocating any layer buffers, returning `*loaderr.Error` values pointing at the offending JSON field:

- **Per-layer limit (`maxLDtkLayerTiles`)**: shares Tiled's `maxTiledLayerTiles` (`16 << 20`, 16,777,216 cells = 64 MiB of `uint32` GIDs). Every grid layer (`Tiles`, `AutoLayer`, `IntGrid`) must have non-negative explicit `__cWid`/`__cHei`, positive effective width and height after level fallback, `lw <= math.MaxInt/lh`, and `lw * lh <= maxLDtkLayerTiles`.
- **Per-load total budget (`maxLDtkTotalTiles`)**: caps total dense tile cells allocated across all tile-bearing layers (`Tiles`, `AutoLayer`, and `IntGrid` with `autoLayerTiles`) in one `LoadLDtk`, `LoadLDtkAll`, or lazy `WorldLevel.Load` call at `64 << 20` (67,108,864 cells = 256 MiB of `uint32` GIDs). Because LDtk `gridTiles`/`autoLayerTiles` are sparse JSON arrays expanded into dense `[]uint32` grids, a per-layer cap alone still allows a small file with many empty 4096×4096 layers to allocate gigabytes.
- **`intGridCsv` and level/tileset bounds**: `IntGrid` layers refuse `len(intGridCsv) > lw*lh` on `intGridCsv`. Negative `pxWid`/`pxHei` are refused in both `buildLDtkLevel` and `loadLDtkWorld`, and tileset grid counts check against `TileIDMask` so `FirstGID` arithmetic cannot overflow into flip-flag bits.
- **CI fuzzing**: `pkg/tilemap/ldtk_fuzz_test.go` adds `FuzzLoadLDtk` covering `LoadLDtkAllWith` and `LoadLDtkWorldWith` (including lazy level loading), asserting the per-layer and total cell invariants on every loaded `*Tilemap`, and `.github/workflows/quality.yml` runs it alongside the Tiled fuzzers.

## Alternatives considered

- **Only checking `lw*lh > maxLDtkLayerTiles` without a cumulative load budget**: simpler and matches Tiled's per-layer constant alone, but unlike Tiled's uncompressed JSON arrays (which require bytes proportional to tile count) or compressed base64 (already capped at `maxMapDecompressedBytes = 64 MiB` per map), LDtk expands empty sparse tile lists into dense `make([]uint32, lw*lh)` buffers. Without `maxLDtkTotalTiles`, a few kilobytes of repeated 4096×4096 layers in `LoadLDtkAll` would still exhaust memory.
- **Silently skipping empty layers or falling back to `m.Width` on negative `__cWid`**: hides corrupt exports and hand-edit typos instead of surfacing a `*loaderr.Error` in `game check` and `sceneformat.Build`. Refusing negative values explicitly while keeping `0` as "inherit level size" matches Tiled's loader (`pkg/tilemap/tiled.go`).

## Consequences

- **Benefits**: malformed `.ldtk` files can no longer panic `game check`, the editor, or games with divide-by-zero or exhaust memory with crafted layer dimensions, and `FuzzLoadLDtk` guards the loader continuously in CI.
- **Costs and known limits**: projects that intentionally load more than 64 Mi dense tile cells eagerly in one `LoadLDtkAll` call must use `LoadLDtkWorld` (which loads levels lazily with an independent per-level budget) or reduce layer dimensions. `Entities`-only levels with `pxWid == 0` / `pxHei == 0` remain accepted.
