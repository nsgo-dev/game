# Agent Note: Sparse chunked tilemap layers and streaming for unbounded 2D worlds

Status: implemented

## Problem

`game/tilemap` previously backed every `TileLayer` with a dense `[]uint32` slice indexed on `[0, Width) × [0, Height)`, and `LoadTiled` flattened infinite `.tmj` chunk lists into a single dense array spanning `[minX, maxX) × [minY, maxY)`. Open-world and sandbox 2D games (such as Terraria- or Starbound-style worlds) need unbounded negative and positive tile coordinates, per-chunk static caching and dirty tracking so single-tile edits do not invalidate the whole map, bounded-memory streaming with `world3d.ChunkStreamer` plus plain-text persistence of modified chunks, and direct sparse loading of infinite Tiled maps without breaking `game/tilemap`'s `Stable` stability contract (#853).

## Decision

Add sparse power-of-two chunked storage, per-chunk static caching, versioned plain-text chunk persistence, and synchronous streaming inside `packages/game/tilemap`, and place the `world3d.ChunkStreamer` bridge in `packages/game/world3d`:

- **Sparse power-of-two chunk storage inside `TileLayer` (`packages/game/tilemap/chunk.go`)**:
  - `TileLayer.ChunkSize` (power of two in `[MinChunkSize, MaxChunkSize]`, default `DefaultChunkSize = 16`) enables sparse `map[ChunkCoord]*Chunk` storage behind `NewChunkedTileLayer` and `NewInfinite`.
  - Coordinate conversion uses branch-free arithmetic right shift (`col >> shift`) and bitmask (`col & mask`), flooring negative tile coordinates toward negative infinity so chunk-local coordinates always lie in `[0, size)`.
  - `TileAt`, `SetTile`, `FillRect`, `SetCellFlags`, `FlagsAt`, `TilesInRect`, `Raycast`, `DrawLayer`, `drawProjectedLayer`, `AppendYSortTiles`, `ApplyAutotile`, and `SetAutotile` all support sparse chunked layers transparently across negative and positive coordinates while keeping dense layers on their existing fast path.
  - `SetTile` on an unloaded coordinate with `gid == 0` is a no-op so clearing empty space never allocates chunks.
- **Per-chunk static caches and minimal dirty tracking (`packages/game/tilemap/cache.go`, `tilemap.go`)**:
  - Each `Chunk` tracks `Dirty` (static cache stale), `Modified` (edited after load/generation), and an optional per-chunk `*LayerCache`.
  - Editing a tile via `SetTile` or `FillRect` marks only the touched chunk(s) `Dirty` and `Modified`; all other chunks keep their clean static caches intact.
  - `CacheStaticChunk` bakes a single chunk into a premultiplied `*image.RGBA` in layer-local world pixel space; `CacheDirtyChunks` rebakes only dirty/uncached loaded chunks. `DrawLayer` emits a single `DrawImageFrom` per visible clean cached chunk and falls back to culled live tile drawing for uncached or dirty chunks.
- **Deterministic plain-text chunk format (`packages/game/tilemap/chunk_format.go`)**:
  - `ChunkDoc` (`ChunkFormatVersion = 1`) stores `version`, `x`, `y`, `size`, and row-major run-length encoded `runs` (`"gid"` or `"gid*count"`, preserving full 32-bit raw GIDs including flip flags).
  - `EncodeChunkYAML`/`DecodeChunkYAML` and `EncodeChunkJSON`/`DecodeChunkJSON` enforce strict decoding (`KnownFields(true)` / `DisallowUnknownFields()`), reject trailing documents/tokens, validate run totals against `size*size`, and round-trip byte for byte. Conformance is verified via `internal/formattest.Run` (`tilemap#chunk`).
- **Stability-preserving streaming split (`tilemap/chunk_stream.go` and `world3d/tilemap_stream.go`)**:
  - Because `game/tilemap` is `Stable` and `game/world3d` is `Experimental`, `TestStabilityTable` forbids `tilemap` from importing `world3d`.
  - `tilemap` defines the `ChunkStore` interface, concurrency-safe `MemoryChunkStore` and `DirChunkStore`, and a synchronous `ChunkStream` for pure-2D callers.
  - `world3d.NewTilemapStreamer` (in `packages/game/world3d/tilemap_stream.go`, importing Stable `game/tilemap`) wraps `world3d.ChunkStreamer[*tilemap.Chunk]`, providing background worker goroutines, context cancellation, per-frame integration budgets, hysteresis unloading, non-square tile scaling, and automatic YAML/JSON persistence of modified chunks.
- **Direct sparse loading of infinite Tiled `.tmj` maps (`packages/game/tilemap/tiled.go`)**:
  - `buildChunkedTileLayer` populates a sparse chunked `TileLayer` directly at each chunk's true `(rc.X + c, rc.Y + r)` coordinates (reusing a uniform power-of-two chunk size when all Tiled chunks match and align, or `DefaultChunkSize` otherwise) instead of allocating a dense bounding-box array and shifting `OffsetX`/`OffsetY`.

### Invariants

- Dependency direction remains acyclic and stability-safe: `game/tilemap` (Stable) never imports `game/world3d` (Experimental); `game/world3d` imports `game/tilemap`.
- Active chunk count under `ChunkStream` and `world3d.TilemapStreamer` stays bounded by `(2*UnloadRadius + 1)^2` regardless of walk distance.
- Only chunks with `Modified == true` are serialized to `ChunkStore` on unload; untouched generated chunks are discarded and regenerated on return.
- Dense `TileLayer` behavior (`ChunkSize == 0`) is unchanged.

## Alternatives considered

### Putting `world3d.ChunkStreamer` directly inside `packages/game/tilemap`

Importing `world3d.ChunkStreamer` in `tilemap` would avoid a bridge type in `world3d`, but `game/world3d` is `Experimental` while `game/tilemap` is `Stable`, which violates the stability contract enforced by `TestStabilityTable`. Moving `ChunkStreamer` into a new stable package was also unnecessary because `world3d` already imports only `math3d` and stdlib, so placing `world3d.TilemapStreamer` in `world3d` (importing Stable `tilemap`) alongside `tilemap.ChunkStream` keeps the stability boundary intact and serves both pure-2D and `world3d`-backed streaming callers.

### Flattening infinite Tiled `.tmj` maps with a shifted `OffsetX`/`OffsetY`

Keeping the existing dense flattening in `buildChunkedTileLayer` would avoid updating `TestLoadTiledInfiniteImageAndNestedGroupLayers`'s `OffsetX`/`OffsetY` expectation, but widely separated sparse chunks (for example at `(-10000, -10000)` and `(10000, 10000)`) would attempt a multi-gigabyte dense allocation and break negative-coordinate `TileAt`/`SetTile`/`Raycast` queries. Loading directly into sparse chunks keeps memory proportional to actual non-empty chunks while producing identical rendered world positions.

### Storing flat `tiles: [...]` arrays instead of RLE `runs` in `ChunkDoc`

A flat array of 256 integers per 16×16 chunk is simpler to parse, but produces verbose diffs and large files for mostly uniform terrain chunks. Row-major RLE tokens (`"0*128"`, `"1*16"`) match `game/voxel`'s proven YAML pattern, stay plain-text and human-diffable, preserve full 32-bit GIDs with flip bits, and round-trip byte for byte.

## Consequences

- **Benefits:**
  - Unbounded 2D worlds support arbitrary negative and positive coordinates across all query, edit, Y-sort, projection, autotile, and culled draw APIs.
  - Editing one tile dirties and rebakes only its owning chunk rather than the full map.
  - Walking 10,000 tiles in each cardinal direction keeps loaded chunks and memory strictly bounded with smooth frame times.
  - Modified chunks unload to deterministic YAML or JSON and reload byte for byte identically; infinite Tiled `.tmj` maps with distant sparse chunks load in kilobytes instead of gigabytes.
- **Costs:**
  - Infinite Tiled `.tmj` layers now keep real tile coordinates and pure layer `OffsetX`/`OffsetY` (no longer adding `StartX*TileWidth` into `OffsetX`); callers inspecting `TileLayer.Data` directly on infinite maps should use `TileAt`/`SetTile` or `Chunk` APIs instead.
  - `CacheStaticLayer` returns an error on chunked layers, directing callers to `CacheStaticChunk` / `CacheDirtyChunks`.

## Verification

Run from `/Users/e-hu/worktrees/agy-game-853`:

- `(cd packages/game && env -u GOROOT go tool nsgo test ./tilemap/... ./world3d/...)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./tilemap/... -- -run=^$ -bench=Chunk -benchmem)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./world3d/... -- -run=^$ -bench=TilemapStreamer -benchmem)`
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid|TestFormatsPage|TestFormatPackagesRunConformance')`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
