# tilemap

`game/tilemap` draws orthogonal tiled worlds with camera frustum culling, loads Tiled `.tmj` maps and animated tiles, queries tile collision flags and rays, and drives a 2D camera with smoothing, clamping, and screen shake.

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

`LoadTiled` parses uncompressed orthogonal Tiled JSON maps (`.tmj`), supporting embedded and external (`.tsj`) tilesets, GID flip bits (`FlipHorizontal`, `FlipVertical`, `FlipDiagonal`), object layers, typed custom properties, and animated tiles:

```go
//go:embed resources/maps/*.tmj resources/maps/*.tsj
var mapFS embed.FS

raw, err := mapFS.ReadFile("resources/maps/level1.tmj")
if err != nil {
	return err
}

m, err := tilemap.LoadTiled(raw, tilemap.TilesetLoader{
	LoadJSON: func(source string) ([]byte, error) {
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
- For pathfinding and tactics movement ranges on these projections, pass `ai.TopologyFromTilemap(m)` to [`game/ai`](ai.md#hexagonal-and-staggered-topologies) (`AStar.Topology`, `AStar.MovementRange`, `ReachableCells`).
- For line-of-sight visibility and tile-aligned fog rendering over isometric, staggered, and hex maps, build a [`game/fog`](fog.md) with `fog.NewFromTilemap(m, tilemap.TileSolid)`.
