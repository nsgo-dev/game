# tileshape

`github.com/nsgo-dev/game/packages/game/tileshape` derives collision and lighting geometry from a [`tilemap.Tilemap`](tilemap.md): a `View` that [physics](physics.md) and the [platformer](platformer.md) controller take as their `TileGrid`, slopes from slanted and partial tiles, the merged outline of solid cells and tile shapes, static boxes, and [lights2d](lights2d.md) occluders. It plays the part of the physics and occlusion polygons of Godot's TileSet.

```go
import "github.com/nsgo-dev/game/packages/game/tileshape"
```

## Tile shapes

Draw a tile's collision shapes in Tiled's collision editor: rectangles, polygons, ellipses and polylines. `tilemap.LoadTiled` reads them into `Tileset.Shapes`, keyed by local tile ID, as `TileShape` values with their points in the tile's own coordinates; an ellipse becomes a 16-sided polygon (`EllipseShapeSegments`), and an object's rotation is applied. Points and tile objects in the collision editor are skipped. Maps built in code set `Tileset.Shapes` directly.

A placed tile's shapes follow the flip bits of its GID, the same transform its image is drawn with, so a flipped or rotated slope tile is a mirrored or rotated slope. `m.TileShapes(gid)` returns them already flipped, and `TileShape.Flipped` applies the bits to one shape.

## View

```go
m, err := tilemap.LoadTiled(data, loader)
if err != nil {
	return err
}
v := tileshape.New(m, tileshape.Options{LayerNames: []string{"ground"}})

player := physics.NewCharacterBody(physics.NewBoxBody(32, 200, 12, 16))
ctl, _ := platformer.NewController(player, platformer.DefaultConfig())

// Inside game.Loop.Update:
near := player.Body.Bounds()
slopes = v.AppendSlopesIn(slopes[:0], core.Bounds{
	Origin: core.Point{X: core.Points(near.X - 64), Y: core.Points(near.Y - 64)},
	Size:   core.Size{Width: core.Points(near.W + 128), Height: core.Points(near.H + 128)},
})
ctl.Step(in, v, slopes, platforms, dt)
```

`View` implements `physics.TileGrid` and `physics.TileGridOrigin`, and every segment, slope and box it returns is in world coordinates that include the map's `Origin`, so the same value goes to `physics.MoveAndSlide`, `CharacterBody.Move`, `platformer.Controller.Step` and `physics.World{Tiles: v}`. `Options.Layers` and `Options.LayerNames` choose the tile layers that collide; with neither, every tile layer does, including layers added to `m.Layers` later.

A cell collides as a whole when its tiles carry `TileSolid` or `TileOneWay` (from `SetFlags`, a `solid` or `oneWay` tile property, or a `SetCellFlags` override, which takes precedence over every tile in the cell), or when one of its tiles has a shape that fills the cell. A tile whose shapes do not fill the cell collides through them: `TileFlagsAt` reports neither `TileSolid` nor `TileOneWay` for that cell, and the shapes' edges become slopes. Set `Options.ShapedSolid` to report such cells as whole solid cells instead, for code that knows only whole cells, such as `MoveAndSlide` alone or `World` raycasts. A polyline is an open edge a character stands on: it is a slope, but not part of the outline or the occluders, and never makes its cell solid.

| Method | Returns |
| --- | --- |
| `TileFlagsAt(col, row)` | The cell's collision flags for physics |
| `Slopes()` | The upward-facing and vertical edges of solid shapes, the upward-facing edges of one-way shapes, and every polyline, as `physics.Slope` segments |
| `AppendSlopesIn(dst, r)` | The slopes of the chunks around `r`, for passing only nearby slopes to `CharacterBody.Move` on a large map |
| `Outline()` | The boundary of every solid cell and solid shape, with shared edges removed and collinear edges merged, solid side on the right |
| `Boxes()` | Solid cells merged into rectangles, then rectangular solid shapes such as half tiles, for `physics.NewStaticRigidBox` |
| `Occluders()` | `Outline` as `lights2d` occluders, one per chunk |

With these slopes, `CharacterBody` walks 45 degree, 2:1 (about 26.6 degree, the tiles tilesets often call 22.5 degree), true 22.5 degree and half-height tiles, steps from a slope onto the solid tiles at its top, and snaps onto the floor tiles past a slope's lower end, with no hand-placed segments. `CharacterBody` has no ceiling segments, so the underside of a partial shape does not stop a jump; use whole cells, or `ShapedSolid`, where a ceiling matters.

## Lights

```go
lighting := &lights2d.Scene{Ambient: core.RGBA(12, 16, 30, 0.85)}
lighting.Occluders = v.Occluders()

// After the map changes:
m.SetTile(0, col, row, 0)
lighting.Occluders = v.Occluders()
```

The derived occluders cast the same shadows as a `RectOccluder` per solid cell and a `PolygonOccluder` per solid shape, with far fewer segments. One-way cells and shapes cast none.

## Changes at runtime

Geometry is cached in chunks of 16 by 16 cells and built on first use. A `View` follows the map's change log, `Tilemap.TileRevision` and `Tilemap.TileChangesSince`, so after `SetTile`, `FillRect`, `SetCellFlags` or `ClearCellFlags` its next call rebuilds only the chunks around the cells that changed. `SetFlags`, a large `FillRect`, or more edits than the log holds rebuild every cell instead. After writing a layer's `Data` directly call `v.Invalidate(col, row, w, h)`. After editing a tileset's `Shapes` call `v.InvalidateAll()`: `Invalidate` keeps the geometry already derived from each tile's shapes.

A tile larger than a map cell is placed bottom-aligned in its cell, so its shapes can reach into the cells above it and to its right. A `View` widens the neighbourhood it compares edges in, and the chunks it rebuilds around a change, by as many cells as the largest tile reaches.

The slices a `View` returns are shared with its cache: they stay valid, and must not be modified, until the map changes. Only bounded orthogonal maps are supported: a `View` of an unbounded map (`Tilemap.Infinite`, or a sparse chunked layer such as an infinite Tiled map loads into) has no cells. Layer offsets are ignored, as they are by the tilemap's own collision queries.
