# procgen

`github.com/nsgo-dev/game/packages/game/procgen` generates 2D levels on a tile grid with three generators: BSP rooms joined by corridors, cellular-automata caves, and wave function collapse over a YAML rule file.

Every generator takes a [`random.Stream`](determinism.md) and draws all of its randomness from it, so the same seed and configuration give the same level, cell for cell, on every operating system and architecture. Every generator also guarantees that its walkable cells form one 4-connected region.

## Grid

A `Grid` is `Width` by `Height` `uint32` values stored row by row in `Cells`. BSP and Cave write `procgen.Wall` (0) and `procgen.Floor` (1); WFC writes the index of each cell's tile in its rule set.

```go
g.At(x, y)            // Wall outside the grid
g.Set(x, y, procgen.Floor)
g.Fill(procgen.Rect{X: 2, Y: 2, W: 5, H: 3}, procgen.Floor)
fmt.Print(g)          // '#' Wall, '.' Floor, 'a'... for higher values
```

`Regions(passable)` flood-fills the cells that `passable` accepts and returns the 4-connected regions, largest first. `Connected(passable)` reports whether there is at most one. Use `procgen.IsFloor` for BSP and Cave levels and `rules.Walkable` for WFC levels, and run the same check after a game edits a level.

`Paint` copies a grid into a [tilemap](tilemap.md) layer, mapping each value to a tile GID:

```go
m := tilemap.New(g.Width, g.Height, 16, 16, tileset)
m.Layers = append(m.Layers, tilemap.TileLayer{Name: "ground", Visible: true})
g.Paint(m, 0, 0, 0, func(v uint32) uint32 {
	if v == procgen.Floor {
		return floorGID
	}
	return wallGID
})
```

## BSP rooms

`BSP` splits the level into partitions, places one room in each leaf and joins sibling partitions with L-shaped corridors through their closest rooms, so every room is reachable.

```go
streams := random.NewStreams(runSeed)
d, err := procgen.BSP(streams.Stream("floor-3"), procgen.BSPConfig{
	Width:  80,
	Height: 50,
	Loops:  3, // extra corridors so the level has cycles
})
// d.Grid, d.Rooms (rooms as Rects), d.Corridors (From, To, Path)
```

| Field | Default | Meaning |
| --- | --- | --- |
| `MinLeaf` | 10 | Smallest partition side; a partition splits while a side is at least twice this |
| `MinRoom` | 4 | Smallest room side |
| `MaxRoom` | 0 | Largest room side; 0 lets rooms fill their partition |
| `Padding` | 1 | Wall cells between a room and its partition's edges, so rooms never merge |
| `Loops` | 0 | Extra corridors between random rooms |

The outer ring of the level is always wall. Pick a spawn point in `d.Rooms[0]` and an exit in the room farthest from it.

## Cellular-automata caves

`Cave` fills the inside of the level with random wall at `WallChance`, then smooths it `Steps` times: a cell with at least `WallAt` wall neighbours of 8 becomes wall, one with at most `FloorAt` becomes floor. The caves left over are then made into one:

- By default only the largest cave is kept and the others are filled with wall.
- With `Join`, every cave of at least `MinRegion` cells is tunnelled into the largest along the shortest path through rock.

```go
g, err := procgen.Cave(random.New(seed), procgen.CaveConfig{
	Width:     64,
	Height:    40,
	Join:      true,
	MinRegion: 6,
})
if errors.Is(err, procgen.ErrNoFloor) {
	// The noise smoothed into solid rock; try the next seed.
}
```

Defaults are `WallChance` 0.45, `Steps` 5, `WallAt` 5 and `FloorAt` 3.

## Wave function collapse

`WFC` fills a grid with the simple tiled model: it collapses the cell with the fewest remaining tiles to one tile picked by weight and propagates the adjacency rules to its neighbours, restarting on a contradiction up to `Attempts` times (20 by default) before it returns `ErrNoSolution`.

The tiles and their rules are a YAML file:

```yaml
version: 1
tiles:
  - name: floor
    weight: 6
    walkable: true
  - name: pillar
    weight: 1
  - name: water
    weight: 2
neighbors:
  - {left: floor, right: floor}
  - {left: floor, right: pillar}
  - {left: pillar, right: floor}
  - {up: floor, down: floor}
  - {up: floor, down: pillar}
  - {up: pillar, down: floor}
  # ...
```

- `version` must be 1, and unknown fields are refused.
- Each tile has a unique `name` and a positive `weight`, its relative frequency; `walkable` marks the tiles the connectivity constraint joins up.
- Each entry of `neighbors` allows one ordered pair, `left` next to `right` or `up` above `down`. A pair that is not listed is forbidden, so list both orders for a symmetric rule.

```go
rules, err := procgen.ParseRules(data)
g, err := procgen.WFC(random.New(seed), procgen.WFCConfig{Width: 32, Height: 24, Rules: rules})
// g.At(x, y) is an index into rules.Tiles
```

While the wave collapses, a path constraint keeps the walkable tiles connected: cells that can no longer reach the walkable area lose their walkable tiles, and a cell whose loss would split the walkable area loses its other tiles. A level WFC returns therefore has at least one walkable cell and passes `Connected(rules.Walkable)`. Set `AllowDisconnected` to turn the constraint off.

`EncodeRules` writes a rule set back as YAML with a fixed field order and the tiles and neighbors in their given order, so parsing and encoding a file it wrote reproduces it byte for byte.

## Seeds

Fork a named stream for each level from the run's seed, so regenerating one floor does not change the others:

```go
streams := random.NewStreams(runSeed)
cave, _ := procgen.Cave(streams.Stream("cave-1"), caveCfg)
```

A generator advances the stream it is given. To regenerate the same level, pass a fresh stream with the same seed, or save the stream's `State()` before generating and restore it with `SetState`.
