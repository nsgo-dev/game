# Agent Note: Tilemap world origin, LDtk multi-level worlds, Tiled .world loader, and WorldStream

Status: implemented

## Problem

Metroidvanias and open 2D worlds are authored as many levels laid out in LDtk's world view (`Free` and `GridVania` layouts) or in a Tiled `.world` file, and gameplay keeps the active level and its neighbours loaded at their world positions. Previously `tilemap.Tilemap` assumed every map started at `(0, 0)`, LDtk loaders discarded `worldX`/`worldY` and `__neighbours`, Tiled `.world` files had no reader, and `physics.TileGrid` / `fog.Fog` had no origin support, forcing games to offset drawing, queries, fog, and collision by hand (#852).

## Decision

Add world-space placement across `tilemap`, `fog`, and `physics` while keeping `game/tilemap` and `game/physics` strictly Stable and decoupled from each other:

- **World origin on `Tilemap`** (`packages/game/tilemap/tilemap.go`, `projection.go`, `query.go`, `ysort.go`, `chunk_stream.go`):
  - Add `Origin core.Point` and `Neighbours []LevelNeighbour` to `Tilemap`. Zero-value `Origin{0, 0}` preserves existing behavior and allocation characteristics.
  - Keep internal helpers `tileOrigin(col, row)` and `projectedWorldBounds(w, h)` in map-local space so static layer/chunk caches (`LayerCache`) remain origin-independent and do not require rebaking if `Origin` changes; add `Origin` once at the world boundary in `TileOrigin`, `TileToWorld`, `WorldToTile`, `WorldBounds`, `TilesInRect`, `Raycast`, `effectiveLayerOffset` call sites (`DrawLayer`, `drawChunkedLayer`, `DrawImageLayer`, `AppendYSortTiles`), and `ChunkStream.CenterChunk`.
- **LDtk world positions and neighbours** (`packages/game/tilemap/ldtk.go`):
  - `LoadLDtk`, `LoadLDtkAll`, and the new `LoadLDtkWorld(data, worldIdentifier, resolveImage)` populate `Origin` from `(worldX, worldY)` for `Free` and `GridVania` (or omitted) layouts and populate `Neighbours []LevelNeighbour` from `__neighbours`, resolving both `levelIid` (LDtk 1.2+) and `levelUid` (legacy) against the world's levels to fill `Identifier`, `IID`, `UID`, and `Dir` (`n`/`s`/`e`/`w`/`ne`/`nw`/`se`/`sw`/`o`/`<`/`>`).
  - `LoadLDtkWorld` supports both top-level `levels` and multi-world `worlds`, refuses `LinearHorizontal`/`LinearVertical` layouts via `*loaderr.Error`, and returns a lazy `*LevelWorld` reusing the parsed project.
- **Tiled `.world` loader** (`packages/game/tilemap/tiled_world.go`):
  - `ParseTiledWorld(data)` parses explicit `maps` (`fileName`, `x`, `y`, optional `width`/`height`) and regex `patterns` (`regexp`, `multiplierX`, `multiplierY`, `offsetX`, `offsetY`, `mapWidth`, `mapHeight`) with Tiled's reciprocal multiplier/map-size defaults, reporting malformed JSON, invalid `type`, negative sizes, and invalid/group-less regexes as `*loaderr.Error` with line/column/field/fix.
  - `TiledWorld.Resolve(fileNames)` emits explicit `Maps` first and lexicographically sorted pattern matches second without duplicating explicit entries.
  - `LoadTiledWorld(data, loader)` builds a lazy `*LevelWorld` that sets each loaded map's `Origin` and peeks `.tmj` header dimensions only for explicit entries that omit `width` or `height`.
- **Region-based streamer (`LevelWorld` and `WorldStream`)** (`packages/game/tilemap/world.go`):
  - `WorldStream` loads levels whose `Bounds` overlap the query/camera rect expanded by `LoadMargin`, unloads levels outside `UnloadMargin` (hysteresis), fires `OnEnter`/`OnLeave` and returns `[]WorldStreamEvent` in deterministic `World.Levels` order, and provides `Draw`, `UpdateAnimations`, `LevelAt`, `FlagsAtWorld`, `TilesInRect`, `Raycast`, and O(1) uniform-grid accessors (`TileSize`, `GridSize`, `GridOrigin`, `GridFlagsAt`).
- **Fog and physics origin adapters**:
  - `fog.Fog` adds `Origin core.Point`, copied in `fog.NewFromTilemap`, honored in `CellAt`, `MoveTo`, `VisibleAt`, and `Draw` (orthogonal and projected), and serialized via `originX`/`originY` with `omitempty` so zero-origin snapshots remain byte-identical.
  - `physics` adds the optional `TileGridOrigin` interface (`GridOrigin() (x, y float32)`) and `Grid.OriginX`/`Grid.OriginY`, honored by `MoveAndSlide`, `CharacterBody.Move`, `World.QueryOverlapLayer`, `raycastTiles`, `SoftWorld`, `DrawTileGrid`, `DrawTileContacts`, and `mcp`'s `drawPhysicsTilesOverlay`.

### Invariants

- Package dependency direction stays strictly acyclic and preserves stability rules: `game/tilemap` imports only `nsgo/core`, `game/sprite`, and `game/loaderr`; `game/physics` imports only `nsgo/core`; neither imports the other or any Experimental package.
- Zero-origin `Tilemap`, `fog.Fog`, and `physics.TileGrid` values behave identically to before, including JSON snapshot bytes for `fog.Fog`.
- `WorldStream.Update` always processes unloads before loads in deterministic `World.Levels` order, and `UnloadMargin` is clamped to at least `LoadMargin` so a level cannot thrash in one update.

## Alternatives considered

### Why not add `GridOrigin()` directly to `physics.TileGrid`?

Adding a method to `physics.TileGrid` would avoid type assertions in `physics`. Rejected because `game/physics` is a Stable package and adding a method to an exported interface is a breaking API change for every existing `TileGrid` implementation in user code and examples. An optional `TileGridOrigin` interface is 100% backward-compatible and adds a single O(1) interface check per call.

### Why not have `tilemap` import `physics` (or `physics` import `tilemap`) to return a concrete `physics.TileGrid`?

Importing `physics` from `tilemap` would let `WorldStream` implement `physics.TileGrid` directly without a 5-line adapter. Rejected because `physics` is a large package (rigid bodies, XPBD soft bodies, character controllers, debug drawing) that non-physics `tilemap` consumers (such as UI maps, VNs, and `wasmprobe`) should not pull into their binaries, and `physics/tile.go` intentionally decouples `TileGrid` from any specific tilemap implementation. Exposing `TileSize`, `GridSize`, `GridOrigin`, and `GridFlagsAt` on `WorldStream` keeps the adapter trivial while preserving clean package boundaries.

### Why not apply `Tilemap.Origin` inside internal `tileOrigin(col, row)`?

Shifting `tileOrigin(col, row)` directly would shorten `TileOrigin` and `TileToWorld`. Rejected because `CacheStaticLayer`, `CacheStaticChunk`, `drawProjectedLayer`, and `AppendYSortTiles` all call `tileOrigin(col, row)` in map-local space before applying `offX, offY`. Baking `Origin` into `tileOrigin` would either double-count `Origin` during drawing or tie static RGBA caches to a fixed world coordinate. Keeping `tileOrigin` map-local makes cached layers movable by simply updating `Tilemap.Origin`.

## Consequences

- **Benefit:** LDtk `Free`/`GridVania` worlds and Tiled `.world` files load directly into one world coordinate space where drawing, queries, raycasts, fog, and physics work across level seams without manual coordinate offsetting.
- **Benefit:** `WorldStream` supports lazy loading and hysteresis unloading with deterministic enter/leave events and seamless cross-seam movement.
- **Cost:** Cross-seam `physics.TileGrid` collision via `WorldStream.GridFlagsAt` assumes loaded levels share a uniform tile grid aligned to `(GridOrigin, TileSize)`; levels placed at sub-tile fractional pixel offsets in LDtk `Free` mode require per-level collision or `WorldStream.TilesInRect`/`Raycast`.

## Testing

- `packages/game/tilemap/world_test.go` verifies `Tilemap.Origin` across `Draw`, `CacheStaticLayer`, `AppendYSortTiles`, `WorldBounds`, `TileOrigin`, `TileToWorld`, `WorldToTile`, `TilesInRect`, `Raycast`, isometric/staggered/hex projections, LDtk `Free` and `GridVania` adjacent-level seamless drawing (`nsgotest`) and cross-seam `physics.MoveAndSlide` traversal, multi-world LDtk and `LinearHorizontal` error reporting, Tiled `.world` explicit maps and regex patterns with multipliers/offsets/deduplication/error diagnostics, and `WorldStream` enter/leave events and hysteresis.
- `packages/game/fog/origin_test.go` verifies `fog.NewFromTilemap`, `CellAt`, `MoveTo`, `VisibleAt`, orthogonal/isometric `Draw`, and JSON snapshot round-tripping with non-zero `Origin`.
- `packages/game/physics/origin_test.go` verifies `TileGridOrigin` across `MoveAndSlide`, `CharacterBody.Move`, `World.QueryOverlap`, `World.RaycastFirst`, and `DrawTileGrid`.
- Verified with `env -u GOROOT go tool nsgo test` on `./tilemap/...`, `./fog/...`, `./physics/...`, `./mcp/...`, root `TestFormatsPage|TestStabilityTable|TestAgentSkillValid`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
