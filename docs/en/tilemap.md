# tilemap

`game/tilemap` draws orthogonal, isometric, staggered, and hexagonal tiled worlds with camera frustum culling, loads Tiled `.tmx`/`.tmj` maps (with embedded or external `.tsx`/`.tsj` tilesets and animated tiles), queries tile collision flags and rays, and drives a 2D camera with smoothing, clamping, and screen shake.

```go
import "github.com/nsgo-dev/game/packages/game/tilemap"
```

## Camera

`Camera` maps world coordinates onto a view of `Width` by `Height` points centered on `(X, Y)`, scaled by `Zoom` (`<= 0` behaves as `1`), and rotated clockwise by `Rotation` radians:

```go
bounds := m.WorldBounds()
cam := &tilemap.Camera{
	Width:  320,
	Height: 180,
	Zoom:   1,
	Bounds: &bounds,
}
```

### Following, clamping, and shaking

Advance camera follow and shake inside `game.Loop.Update`:

```go
// Exponentially smooth toward player (smoothing <= 0 snaps immediately)
cam.Follow(playerPos, 8, dt)

// Advance active screen shake
cam.Update(dt)

// Trigger a deterministic shake on impact
if tookDamage {
	cam.Shake(6, 250*time.Millisecond)
}
```

- `Clamp()` keeps `VisibleBounds()` inside `Bounds` when set; if the world is smaller than the view on an axis, the camera centers on `Bounds` along that axis. `Follow` calls `Clamp` automatically.
- `Shake(intensity, duration)` applies a deterministic displacement seeded by `ShakeSeed` (decays linearly to zero over `duration`; non-positive arguments cancel any active shake). Inspect the current offset with `ShakeOffset()`.
- Convert coordinates with `WorldToView(p)` and `ViewToWorld(p)`, and inspect the axis-aligned visible world box with `VisibleBounds()`.

### Applying the camera during draw

Wrap world-space drawing in `d.Save()` / `d.Restore()` and call `cam.Apply(d)` before drawing tilemaps and world sprites:

```go
d.Save()
cam.Apply(d)
m.Draw(d, cam)
actorsBatch.Draw(d)
d.Restore()
```

Passing `cam` to `m.Draw(d, cam)` or `m.DrawLayer(d, layer, cam)` visits only the columns and rows intersecting `cam.VisibleBounds()`.

## Loading Tiled maps

`LoadTiled` parses Tiled XML (`.tmx`) and JSON (`.tmj`) maps into the same `*Tilemap`, auto-detecting the format from the input bytes. It supports embedded and external `.tsx`/`.tsj` tilesets (including cross-format references such as a `.tmx` map using a `.tsj` tileset or a `.tmj` map using a `.tsx` tileset), CSV, legacy XML `<tile>`, and base64 tile data (uncompressed, `zlib`, and `gzip`) on both finite layers and infinite-map chunks, image layers, nested group layers, Tiled JSON Wang sets (`wangsets` in `.tmj`/`.tsj`) and per-tile probabilities, GID flip bits (`FlipHorizontal`, `FlipVertical`, `FlipDiagonal`), object layers, typed custom properties, animated tiles, and the collision shapes drawn on tiles in Tiled's collision editor. `zstd` compression is refused with a `*loaderr.Error` pointing at the file, line, and fix hint.

```go
//go:embed resources/maps/*.tmx resources/maps/*.tsx
var mapFS embed.FS

raw, err := mapFS.ReadFile("resources/maps/level1.tmx")
if err != nil {
	return err
}

m, err := tilemap.LoadTiled(raw, tilemap.TilesetLoader{
	LoadTileset: func(source string) ([]byte, error) {
		return mapFS.ReadFile("resources/maps/" + source)
	},
	LoadImage: func(imagePath string) (core.ImageSource, error) {
		return core.Src("asset:maps/" + path.Base(imagePath)), nil
	},
})
if err != nil {
	return err
}
```

Existing callers that set `TilesetLoader.LoadJSON` continue to work for both `.tsj` and `.tsx` tilesets when `LoadTileset` is nil.

### Building tilemaps in code

For procedural maps or tests, build a tileset with `NewGridTileset` or `NewAtlasTileset` and populate a `Tilemap` directly:

```go
ts := tilemap.NewGridTileset(1, core.Src("asset:tiles.png"), 16, 16, 8, 64)
m := tilemap.New(40, 20, 16, 16, ts)
ground := tilemap.NewTileLayer("ground", 40, 20, nil)
m.Layers = append(m.Layers, ground)
m.SetTile(0, 5, 19, 1) // layer 0, col 5, row 19 -> GID 1
```

Look up layers and objects by name with `m.Layer("ground")` and `m.ObjectLayer("spawns")`. Read typed custom properties on maps, layers, tilesets, tiles (`m.TileProperties(gid)`), and objects via `Properties` helpers (`Bool`, `Int`, `Float`, `String`, `Color`, `File`, `ObjectID`, `Has`). For polygon/polyline objects, `obj.WorldPoints()` returns vertices translated by `(obj.X, obj.Y)`.

Objects drawn as collision geometry become [physics](physics.md) shapes with `obj.PhysicsShape()`, and `layer.PhysicsBodies()` returns a static `physics.Body` per object, moved by the layer's offset, with the object's ID, its type as `Tag` and the object itself as `UserData`. Hidden objects count, since collision layers are often hidden:

| Tiled object | Shape |
| --- | --- |
| Rectangle | `Box`, or a `Polygon` when rotated |
| Ellipse | `Circle` when round, otherwise an 8-vertex `Polygon` inscribed in it |
| Polygon | `Polygon` when convex with at most 8 vertices, otherwise a closed `Chain` along its outline |
| Polyline | `Segment` for two points, otherwise an open `Chain` |
| Point, tile | none |

```go
for _, ground := range m.ObjectLayer("collision").PhysicsBodies() {
	rigid.AddBody(physics.NewStaticRigidBody(ground.Shape))
}
```

A missing layer returns no bodies. Like the objects themselves, the bodies are in the map's local coordinates; for a map placed by `Origin`, move each shape with `ground.Shape.Translated(float32(m.Origin.X), float32(m.Origin.Y))`.

## Tiled Wang sets and multi-terrain autotiling

`LoadTiled` parses Tiled `wangsets` (`corner`, `edge`, and `mixed`) from embedded `.tmj` and external `.tsj` tilesets into `Tileset.WangSets`, together with each terrain color (`WangColor`: `Name`, `Color`, `Tile`, `Probability`, `Properties`), each tile's 8-slot clockwise `WangID` (`[Top, TopRight, Right, BottomRight, Bottom, BottomLeft, Left, TopLeft]`), and per-tile selection weights (`Tileset.TileProbabilities` / `Tileset.TileProbability`).

Use `m.SetTerrain` and `m.SetTerrainRect` to paint multi-terrain transitions (such as grass, dirt, water, and cliffs) with Tiled's terrain brush semantics across orthogonal and isometric maps as well as dense and sparse chunked layers:

```go
ws := m.WangSet("Terrains") // or ts.WangSet("Terrains") / ts.WangSetAt(0)
grass, _ := ws.ColorIndex("Grass")
dirt, _ := ws.ColorIndex("Dirt")
water, _ := ws.ColorIndex("Water")

rng := random.New(42)
m.SetTerrainRect(0, 0, 0, m.Width, m.Height, ws, grass, rng)
m.SetTerrain(0, 4, 3, ws, dirt, rng)
m.SetTerrainRect(0, 6, 2, 3, 3, ws, water, rng)
```

- **Corner, edge, and mixed sets**: `WangCorner` matches the four tile corners, `WangEdge` matches the four tile edges, and `WangMixed` matches all eight slots. Existing neighbors whose shared corners or edges change are updated automatically, including multi-hop transitions (for example creating a dirt shoreline ring when painting water onto grass in a tileset without direct grass-to-water tiles).
- **Deterministic probability selection**: when multiple tiles tie for the best match, `SetTerrain` and `SetTerrainRect` choose weighted by tile probability from `*random.Stream` (or any `TerrainRand`, so the same seed always picks the same variants). Pass `rng == nil` for deterministic lowest-`TileID` selection.
- **Code-defined blobs**: `AutotileSet`, `ApplyAutotile`, and `SetAutotile` remain available for code-defined 4-bit (16-tile) and 8-bit corner-gated (47-tile) single-terrain blobs.

## Animated tiles

Tiled tile animations load into `Tileset.Animations`, keyed by 0-based local tile ID. Call `m.Update(dt)` on every `game.Loop.Update` step to advance all animated tiles cleanly across cycles:

```go
m.Update(dt)
```

You can also attach a `TileAnimation` manually to a tileset by setting `ts.Animations[localID] = &tilemap.TileAnimation{Frames: ...}`.

## Y-sorting tiles with sprites

Enable per-layer Y-sorting on any `TileLayer` (`layer.YSort = true` or `m.SetLayerYSort(index, true)`, or via a boolean Tiled layer custom property named `y_sort`, `ysort`, `ySort`, or `y-sort`) so tall tiles such as trees, walls, and pillars interleave with scene sprites and custom drawables by depth:

```go
m.Layer("trees").YSort = true

// Optional: shift a tree tile's sort line upward from its bottom edge to its trunk base.
// Equivalent to setting a float/int tile custom property "sort_origin_y" = -8 in Tiled.
m.SetTileSortOriginY(treeGID, -8)

world := sc.AddLayer(scene.NewLayer("world", scene.SpaceWorld, 0))
world.DepthSort = scene.SortY
world.AddTilemap(m)
world.AddBatch(actorsBatch)
```

- **Default sort line**: each visible tile sorts by its world-space bottom edge (`offY + tileOriginY + TileHeight`), which works across orthogonal, isometric, staggered, and hexagonal orientations and matches bottom-pivoted character sprites (`OriginY = height`).
- **Per-tile sort origin**: attach the custom tile property `sort_origin_y` (`tilemap.PropSortOriginY`, aliases `y_sort_origin` and `ysort_origin`) in Tiled or call `m.SetTileSortOriginY(gid, offsetY)` to offset a tile's sort line vertically (negative moves it up into a tree trunk or isometric diamond center; positive moves it down). Inspect it with `m.TileSortOriginY(gid)`.
- **Layer ordering & culling**: inside a `scene.SortY` layer, non-Y-sorted layers before the first Y-sorted layer draw behind sprites (keeping any `CacheStaticLayer` fast path), Y-sorted layers interleave individual visible tiles with sprites and drawables, and trailing non-Y-sorted layers (such as roofs or canopies) draw above them. Camera frustum culling (including tall tiles and atlas frames), animated tiles, flip flags, and layer opacity are all respected.
- **Custom draw loops**: use `m.AppendYSortTiles(dst[:0], layer, cam)`, `tilemap.SortYSortTiles(tiles)`, and `tilemap.YSortDrawer` (or `YSortTile.Draw(d)`) to collect, sort, and draw Y-sorted tiles with reusable scratch buffers and zero allocations in `tilemap` once warmed up.

## Tile flags, overlap queries, and raycasts

Associate collision or gameplay `TileFlags` (`TileSolid`, `TileOneWay`, or custom bitflags) with global tile IDs or individual map cells:

```go
m.SetFlags(1, tilemap.TileSolid)
m.SetFlags(2, tilemap.TileOneWay)

// Override a single cell (e.g. a breakable wall cleared at runtime)
m.SetCellFlags(10, 6, 0)
// Restore tile-ID flags later
m.ClearCellFlags(10, 6)
```

Query the grid without allocating unnecessary geometry:

- `FlagsAt(col, row)` returns the combined `TileFlags` across all layers (or the cell override if set).
- `TilesInRect(r, mask)` returns every cell overlapping `r` matching `mask` (or any non-empty cell when `mask == 0`) in row-major order as `[]TileHit`. Cells are half-open `[col*TileWidth, (col+1)*TileWidth) × [row*TileHeight, (row+1)*TileHeight)`.
- `Raycast(from, to, mask)` traces the segment `from -> to` and returns the first matching `RaycastHit` (`Col`, `Row`, `TileID`, `Flags`, `Point`, `Normal`, and fraction `T` in `[0, 1]`).

```go
if hit, ok := m.Raycast(enemyEye, playerPos, tilemap.TileSolid); ok {
	// Line of sight blocked at hit.Point with surface normal hit.Normal
	_ = hit
}
```

## Isometric, staggered, and hexagonal maps

Set `Orientation` (`OrientationOrthogonal`, `OrientationIsometric`, `OrientationStaggered`, or `OrientationHexagonal`) together with `StaggerAxis` (`StaggerY` / `StaggerX`), `StaggerIndex` (`StaggerOdd` / `StaggerEven`), and `HexSideLength` to project and draw diamond isometric, staggered isometric, and pointy-top or flat-top hexagonal maps:

- `TileOrigin(col, row)` and `TileToWorld(col, row)` return each cell's bounding-box origin and center in world coordinates, and `WorldToTile(p)` round-trips any interior world point back to `(col, row)`.
- `HexNeighbors(col, row)` and `HexDistance(c1, r1, c2, r2)` compute exact 6-neighbor adjacency and cube distance for both stagger axes and indices.
- For pathfinding and tactics movement ranges on these projections, pass `ai.TopologyFromTilemap(m)` to [`game/ai`](ai.md#hexagonal-and-staggered-topologies) (`AStar.Topology`, `AStar.MovementRange`, `ReachableCells`), or wrap a hex map directly with `ai.NewHexGrid(m)` for [`ai.GraphSearch`](ai.md#generic-graph-search-hex-adapters-and-waypoint-graphs) (including reconstructed paths to every cell in range).
- For line-of-sight visibility and tile-aligned fog rendering over isometric, staggered, and hex maps, build a [`game/fog`](fog.md) with `fog.NewFromTilemap(m, tilemap.TileSolid)`.

## Sparse chunked layers and streaming

For unbounded 2D sandbox or open-world maps (negative and arbitrarily large coordinates), use a sparse chunked `TileLayer` created with `NewChunkedTileLayer(name, chunkSize)` inside `NewInfinite(tileWidth, tileHeight, tilesets...)`. Chunk sizes are fixed powers of two in `[MinChunkSize, MaxChunkSize]` (default `DefaultChunkSize = 16`), allowing branch-free shift/mask coordinate lookup (`TileToChunk`) across negative and positive coordinates:

```go
m := tilemap.NewInfinite(16, 16, ts)
m.Layers = []tilemap.TileLayer{
	tilemap.NewChunkedTileLayer("world", tilemap.DefaultChunkSize),
}
m.SetFlags(1, tilemap.TileSolid)

// Arbitrary negative and positive coordinates work through the exact same APIs:
m.SetTile(0, -17, -16, 1)
gid := m.TileAt(0, -17, -16)
hits := m.TilesInRect(rect, tilemap.TileSolid)
rayHit, ok := m.Raycast(from, to, tilemap.TileSolid)
```

- **Infinite Tiled maps**: `LoadTiled` loads infinite `.tmj` layers (`"infinite": true` or layers carrying `"chunks"`) directly into sparse chunked `TileLayer`s, preserving negative chunk coordinates instead of flattening into a giant dense array.
- **Per-chunk static caches and dirty tracking**: editing a tile with `SetTile` or `FillRect` marks only the affected chunk `Dirty` and `Modified` without invalidating other chunks. Bake static chunks with `m.CacheStaticChunk(layerIndex, coord, images)` or rebake only dirty chunks with `m.CacheDirtyChunks(layerIndex, images)`; inspect and invalidate individual chunks via `ChunkCache`, `InvalidateChunkCache`, `ClearChunkCache`, `layer.DirtyChunks()`, and `layer.ModifiedChunks()`.
- **Deterministic plain-text chunk persistence**: `Chunk.ToDoc()` / `ChunkFromDoc` and `EncodeChunkYAML` / `DecodeChunkYAML` (or `EncodeChunkJSON` / `DecodeChunkJSON`) encode chunks as versioned (`ChunkFormatVersion = 1`) run-length encoded YAML or JSON that preserves all 32-bit GIDs including flip flags and round-trips byte for byte. Built-in stores include `NewMemoryChunkStore()` and `NewDirChunkStore(dir, format)` (writing `chunk_<x>_<y>.yaml` or `.json`).
- **Bounded streaming**:
  - Use `tilemap.NewChunkStream(tilemap.ChunkStreamConfig{...})` for synchronous 2D streaming with a `Generator`, `LoadRadius`/`UnloadRadius` hysteresis, `MaxLoadsPerUpdate`, and automatic save/restore of modified chunks through a `ChunkStore`.
  - For multi-threaded background chunk loading on top of `world3d.ChunkStreamer`, use `world3d.NewTilemapStreamer(world3d.TilemapStreamerConfig{...})` (in `game/world3d`), which streams `*tilemap.Chunk` values with background workers, cancellation, per-frame integration budgets, and modified-chunk YAML/JSON persistence.

```go
store := tilemap.NewDirChunkStore(saveDir, tilemap.ChunkFormatYAML)
stream := tilemap.NewChunkStream(tilemap.ChunkStreamConfig{
	Map:          m,
	LayerIndex:   0,
	LoadRadius:   2,
	UnloadRadius: 3,
	Format:       tilemap.ChunkFormatYAML,
	Store:        store,
	Generator: func(coord tilemap.ChunkCoord, ch *tilemap.Chunk) error {
		// Populate procedural terrain for coord...
		return nil
	},
})
defer stream.Close()

// Inside your update loop:
stream.Update(playerPos)
```

## Multi-level worlds (LDtk and Tiled `.world`)

Place multiple tilemaps into one shared world coordinate space using `Tilemap.Origin`, LDtk world layouts, Tiled `.world` files, and region-based streaming with `WorldStream`:

- **World origin (`Tilemap.Origin`)**: every `Tilemap` carries `Origin core.Point` (defaulting to `(0, 0)` with zero overhead). `Draw`, `DrawLayer`, `DrawImageLayer`, `CacheStaticLayer`, `AppendYSortTiles`, `WorldBounds`, `TileOrigin`, `TileToWorld`, `WorldToTile`, `TilesInRect`, and `Raycast` all honor `Origin`, as do `fog.NewFromTilemap` (`fog.Fog.Origin`) and `physics.TileGrid` adapters implementing the optional `physics.TileGridOrigin` interface (`GridOrigin() (x, y float32)`).
- **LDtk multi-level worlds**: `LoadLDtk` and `LoadLDtkAll` populate each level's `Origin` from `(worldX, worldY)` (for `Free` and `GridVania` layouts) and its `Neighbours` list (`[]LevelNeighbour`, resolving `Identifier`, `IID`, `UID`, and direction `n`/`s`/`e`/`w`/`ne`/`nw`/`se`/`sw`/`o`/`<`/`>`). `LoadLDtkWorld(data, worldIdentifier, resolveImage)` returns a lazy `*LevelWorld` supporting both single-world and multi-world `.ldtk` projects (refusing `LinearHorizontal` and `LinearVertical` layouts that assign no 2D world coordinates).
- **Tiled `.world` files**: `ParseTiledWorld(data)` parses Tiled `.world` JSON files — both explicit `maps` entries (`fileName`, `x`, `y`, optional `width`/`height`) and regex `patterns` (`regexp`, `multiplierX`, `multiplierY`, `offsetX`, `offsetY`, `mapWidth`, `mapHeight`) — returning `*loaderr.Error` with line, field, and fix guidance on malformed JSON or invalid patterns. `TiledWorld.Resolve(fileNames)` expands patterns deterministically without duplicating explicit entries, and `LoadTiledWorld(data, loader)` builds a lazy `*LevelWorld` that sets each loaded `.tmj`'s `Origin` to its `.world` placement.
- **Region-based level streaming (`WorldStream`)**: keeps only the levels overlapping a camera or query rectangle (expanded by `LoadMargin`) loaded at their world origins and unloads levels outside `UnloadMargin`, firing `OnEnter`/`OnLeave` and returning `[]WorldStreamEvent` in deterministic world order. `WorldStream` also provides seamless multi-level `Draw`, `UpdateAnimations`, `LevelAt`, `FlagsAtWorld`, `TilesInRect`, `Raycast`, and uniform-grid accessors (`TileSize`, `GridSize`, `GridOrigin`, `GridFlagsAt`) so a `physics.Body` or `physics.CharacterBody` walks across seams between adjacent levels without snagging.

```go
world, err := tilemap.LoadLDtkWorld(ldtkBytes, "", func(relPath string) core.ImageSource {
	return core.Src("asset:" + relPath)
})
if err != nil {
	return err
}

stream := tilemap.NewWorldStream(tilemap.WorldStreamConfig{
	World:        world,
	LoadMargin:   64,
	UnloadMargin: 128,
	OnEnter: func(lvl *tilemap.WorldLevel, m *tilemap.Tilemap) {
		// Spawn entities or attach fog for lvl.ID...
	},
	OnLeave: func(lvl *tilemap.WorldLevel, m *tilemap.Tilemap) {
		// Clean up entities belonging to lvl.ID...
	},
})
defer stream.Close()

// Inside update:
stream.UpdateCamera(cam)
stream.UpdateAnimations(dt)

// Inside draw (after cam.Apply(d)):
stream.Draw(d, cam)
```

## Collision shapes and change tracking

The rectangles, polygons, ellipses and polylines drawn on a tile in Tiled's collision editor load into `Tileset.Shapes`, keyed by local tile ID, as `TileShape` values in the tile's own coordinates: an ellipse becomes `EllipseShapeSegments` points, and an object's rotation is applied. `m.TileShapes(gid)` returns a placed tile's shapes flipped and rotated by its GID's flip bits, the same transform `DrawLayer` draws the tile with, and `m.TileBox(gid)` the size of the tile they are measured in.

`m.TileRevision()` and `m.TileChangesSince(rev, fn)` track collision flag edits as well as tile edits: `SetCellFlags` and `ClearCellFlags` log the cell they change, and `SetFlags`, which can change every cell showing the tile, asks callers for a full rebuild. Calls that change nothing log nothing. `m.Flags(gid)` returns the flags set on a tile ID, and `m.CellFlags(col, row)` the per-cell override, if any.

[tileshape](tileshape.md) builds on both: its `View` is the `physics.TileGrid` of a map, derives slopes from tile shapes, and merges solid cells and shapes into an outline and light occluders that it rebuilds around each change.
