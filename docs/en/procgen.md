# procgen

`github.com/nsgo-dev/game/pkg/procgen` generates 2D levels on a tile grid with four generators: BSP rooms joined by corridors, cellular-automata caves, wave function collapse over a YAML rule file, and hand-made room templates placed on a dungeon layout graph.

Every generator takes a [`random.Stream`](determinism.md) and draws all of its randomness from it, so the same seed and configuration give the same level, cell for cell, on every operating system and architecture. BSP, Cave and GenerateRooms always leave their walkable cells as one 4-connected region, and so does WFC unless `AllowDisconnected` is set or its rules have no walkable tile.

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

Defaults are `WallChance` 0.45, `Steps` 5, `WallAt` 5 and `FloorAt` 3. `FloorAt` takes its default only when `WallAt` is unset too; once `WallAt` is set, `FloorAt` 0 means only cells with no wall neighbour become floor.

Every generator refuses a level of more than `procgen.MaxCells` (2^24) cells or a side longer than `procgen.MaxSide` with an error wrapping `ErrInvalidConfig`.

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
- Each tile has a unique `name` and a positive `weight`, its relative frequency, and the weights must sum to a finite number; `walkable` marks the tiles the connectivity constraint joins up.
- Each entry of `neighbors` allows one ordered pair, `left` next to `right` or `up` above `down`. A pair that is not listed is forbidden, so list both orders for a symmetric rule.

```go
rules, err := procgen.ParseRules(data)
g, err := procgen.WFC(random.New(seed), procgen.WFCConfig{Width: 32, Height: 24, Rules: rules})
// g.At(x, y) is an index into rules.Tiles
```

While the wave collapses, a path constraint keeps the walkable tiles connected: cells that can no longer reach the walkable area lose their walkable tiles, and a cell whose loss would split the walkable area loses its other tiles. A level WFC returns therefore has at least one walkable cell and passes `Connected(rules.Walkable)`. Set `AllowDisconnected` to turn the constraint off.

Each collapse rescans the grid and, with the constraint on, searches the walkable area again, so a solve costs roughly the square of the cell count: tens of milliseconds for 32 by 24, about a second for 64 by 64, and more than ten for 128 by 128. `WFC` therefore accepts at most `procgen.MaxWFCCells` (64 by 64) cells; build a larger map from smaller pieces or use BSP or Cave for it.

`EncodeRules` writes a rule set back as YAML with a fixed field order and the tiles and neighbors in their given order, so parsing and encoding a file it wrote reproduces it byte for byte.

## Room templates on a dungeon graph

`GenerateRooms` builds roguelike levels from authored room templates placed on a generated `LayoutGraph` with a start room, a goal room, side branches, cycles and lock/key progression pairs.

Room templates are defined in a versioned YAML file:

```yaml
version: 1
rooms:
  - id: entry
    weight: 1
    tags:
      - start
    doors:
      - side: north
        offset: 3
      - side: south
        offset: 3
      - side: east
        offset: 2
      - side: west
        offset: 2
    tiles:
      - '###.###'
      - '#.....#'
      - '.......'
      - '#.....#'
      - '###.###'
  - id: boss_vault
    tags:
      - goal
    width: 7
    height: 5
    source: rooms/boss_vault.tmj
    doors:
      - side: north
        offset: 3
      - side: south
        offset: 3
```

- `version` must be 1 (`RoomTemplatesVersion`), and unknown fields are refused.
- Each room has a unique `id`, an optional positive `weight` (default 1), optional `tags` (such as `start`, `goal`, `key`, `combat`), optional `allowRotate` (enabling 90° clockwise rotations), and one or more `doors` (`side`: `north`/`south`/`east`/`west`, 0-based `offset`, optional `width` defaulting to 1, optional `channel` restricting which sockets can connect).
- Each room defines either inline ASCII `tiles` (`#` for `Wall`, `.` for `Floor`, `a`..`z` for tile values `2..27`) or an external `source` path (such as a `.tmj` tilemap or scene file) together with `width` and `height`.
- `ParseRoomTemplates` and `EncodeRoomTemplates` validate and round-trip canonical 2-space YAML byte for byte.

### Layout graph and lock/key progression

`GenerateGraph` builds a `LayoutGraph` standalone, or `GenerateRooms` builds and embeds it in one call:

| Field (`GraphConfig`) | Default | Meaning |
| --- | --- | --- |
| `CriticalPath` | 5 | Rooms on the main path from `Start` to `Goal` (at least 2) |
| `Branches` | 0 | Side rooms attached off the critical path and earlier branches |
| `BranchMaxDepth` | 2 | Maximum length of a branch chain |
| `Loops` | 0 | Target count of extra cycle edges added within the same lock tier |
| `Locks` | 0 | Lock/key pairs placed along the critical path (strictly less than `CriticalPath`) |
| `MaxDegree` | 4 | Maximum passages per room |

Locks split the graph into progression tiers `0..Locks`. Each lock's key is placed in a room whose tier strictly precedes the lock, and loops only connect rooms in the same tier (and opposite tree parity so cycles have even length). `graph.VerifyLocks()` confirms that every key is reachable from `Start` without crossing its lock and that `Goal` is reachable once keys are collected.

### Stitching and Spelunky-style grid placement

```go
templates, err := procgen.ParseRoomTemplates(data)
lvl, err := procgen.GenerateRooms(random.New(seed), procgen.RoomsConfig{
	Templates: templates,
	Mode:      procgen.PlacementGrid, // or procgen.PlacementStitch (default)
	GridCols:  4,
	GridRows:  4,
	Graph: procgen.GraphConfig{
		CriticalPath: 6,
		Branches:     4,
		Loops:        2,
		Locks:        2,
	},
})
// lvl.Grid (Connected holds), lvl.Rooms (TemplateID, Source, Bounds, GridSlot, Rotation, Role, KeyID),
// lvl.Doors (FromRoom, ToRoom, Cells, LockID), lvl.Graph
```

- `PlacementStitch` (default) places variable-sized room templates freely by aligning matching door sockets without overlap, closing same-tier loops where aligned sockets meet, and retrying deterministically from the `random.Stream` up to `Attempts` times (default 50) before returning `ErrNoLayout`.
- `PlacementGrid` lays out uniform-sized templates on a coarse `GridCols × GridRows` Spelunky-style grid of room slots (auto-sized when both are 0), starting on row 0 and aligning every graph edge with adjacent slots.
- `PlaceRooms` places an already-generated `LayoutGraph` in either mode.
- For templates that reference a `.tmj` or scene via `source`, pass a `ResolveGrid` callback (using `procgen.GridFromTilemap` for Tiled maps loaded with `tilemap.LoadTiled`) to stamp its tiles into `Grid`; when `ResolveGrid` is nil, a walled floor footprint of `Width × Height` is stamped automatically. Unused door sockets are sealed with `Wall` and connected sockets are opened so `Grid.Connected` always holds.

## Procedural placement

A placement file (`*.placement.yaml`) lists steps that each sample points over an area and name the prefab to spawn at them:

```yaml
version: 1
seed: 7
steps:
  - id: trees
    sampler: grid
    spacing: 2
    count: 6
    prefab: tree
  - id: lamps
    sampler: grid
    spacing: 4
    prefab: lamp
```

- `version` must be 1 (`PlacementVersion`), and unknown fields are refused.
- Each step has a unique `id`, a `sampler`, and a `prefab`. `grid` (`SamplerGrid`) is the only sampler so far: it places a point at every cell whose coordinates are multiples of `spacing` (default 1). A positive `count` keeps that many of those points, chosen at random; without it every lattice point is kept.
- Each step forks its own `random.Stream` from `seed` by its `id`, so adding, removing or reordering steps does not move the points of the others, and the same file and area give the same points on every machine.

```go
p, err := procgen.ParsePlacement(data)
points := p.Points(width, height) // []PlacementPoint{X, Y, ID, Step, Prefab}
```

`Points` returns the steps' points in step order, each step's ordered by Y and then X, with IDs such as `trees-0`. Spawning the prefabs is up to the game. `ParsePlacement` and `EncodePlacement` round-trip canonical 2-space YAML byte for byte.

## Seeds

Fork a named stream for each level from the run's seed, so regenerating one floor does not change the others:

```go
streams := random.NewStreams(runSeed)
cave, _ := procgen.Cave(streams.Stream("cave-1"), caveCfg)
```

A generator advances the stream it is given. To regenerate the same level, pass a fresh stream with the same seed, or save the stream's `State()` before generating and restore it with `SetState`.

## Placement pipelines

Version-1 `*.placement.yaml` files use a seeded grid sampler to produce deterministic prefab points. `ParsePlacement` rejects unknown fields, unsupported samplers, duplicate step IDs, and trailing documents. Each step forks its random stream by ID, so adding or reordering independent steps does not move existing points.
