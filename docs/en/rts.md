# rts

`game/rts` is the unit-control layer of a real-time strategy game: flow fields that steer many units toward a goal over an `ai.Grid`, a `Crowd` that moves those units with separation and wall sliding, formations kept in versioned YAML, box and click selection, and numbered control groups.

```go
import "github.com/nsgo-dev/game/packages/game/rts"
```

Everything is deterministic and steps on `game.Loop`'s fixed steps with no background goroutines: the same orders on the same grid move the same units to the same places on every run on the same CPU architecture. Positions are `float32`, and Go may fuse a multiply and an add into one instruction on arm64 but not on x64, so the same orders can end a few ulps apart on different architectures; a lockstep multiplayer game across architectures should send state from an authoritative host rather than have each peer step its own `Crowd`.

## Flow fields

A `FlowField` holds, for every cell of a grid, the cost of the cheapest path to the nearest of its goal cells (the integration field) and the direction a unit in that cell moves to follow it (the direction field). Building it is one Dijkstra search over the grid; after that, any number of units read their direction in constant time, so a thousand units heading for the same place cost one search instead of a thousand.

```go
grid := ai.NewCostMap(160, 128) // or any ai.Grid / ai.CostGrid, such as an ai.GridFunc over a tilemap
field := rts.NewFlowField(math2d.Vec2{}, 16) // cells 16 world units wide, cell (0, 0) at the origin
field.Build(grid, ai.Cell{Col: 140, Row: 64})

dir, ok := field.Sample(unitPos) // unit vector, blended from the four cells around unitPos
```

- Movement is 8-way by default (`Diagonal`), with diagonal steps costing sqrt(2) and never cutting the corner of a blocked cell. Stepping into a cell costs its `ai.CostGrid` `MoveCost`, so a cell's `Cost` equals the cost `ai.AStar` finds from it to the nearest goal.
- `Build` takes several goals at once; each cell then leads to the nearest of them. Blocked and out-of-bounds goals are ignored, and `Build` reports whether any goal was valid.
- `Sample` blends the directions of the reachable cells around a point, so units turn smoothly instead of in 45-degree steps and are never pulled into a wall. `Cost`, `Reachable`, `Direction`, `CellAt` and `CellCenter` read the field cell by cell.
- Rebuilding a field of the same size allocates nothing.

`FieldCache` keeps the most recently used fields over one grid, keyed by their goal set, so units ordered to the same goals share a field. Its `Rebuild` integrates every cached field again in place and drops any field none of whose goals is walkable any more. A `Crowd` keeps its own cache: after changing the grid (a wall built, a bridge destroyed), call the crowd's `Rebuild`, which also rebuilds the fields its units still follow after the cache dropped them to make room, so every unit sees the change on its next step. Units whose order led only to cells that are now blocked stop where they stand. `Builds` counts the searches, which tells a test or a profiler whether fields are being shared.

## Crowds and move orders

A `Crowd` moves units over a grid. Add units with `Add`, order them with `Move`, and call `Step` from the loop's `Update`:

```go
crowd := rts.NewCrowd(grid, rts.CrowdOptions{CellSize: 16})
crowd.Add(rts.UnitSpec{Pos: math2d.V2(40, 650), Radius: 5, MaxSpeed: 96, Kind: kindRifleman})

crowd.Move(selection.IDs(), rts.MoveOrder{
	Target:    clickWorld,
	Formation: rts.Formation{Shape: rts.ShapeBox, Spacing: 14},
	KeepPace:  true, // move at the slowest unit's speed
})

loop.Update = func(dt time.Duration) { crowd.Step(dt) }
```

`Move` lays the formation out around the target, facing from the group toward it unless `Facing` is set, and moves any slot that falls on a blocked cell or on a cell the group cannot reach to the nearest free cell that can be reached. When the units cannot reach the target at all, as when it lies on an island, the formation centers on the nearest cell they can reach. Units are matched to slots so their paths do not cross, and the whole group shares one flow field whose goals are the slots' cells. Each unit follows that field until it can see its own slot within `DirectRange`, then walks straight to it; a unit that reaches the formation with a wall between it and its slot follows its own `ai.AStar` path there for the rest of the order, planning it again whenever the crowd pushes it off it. Path searches run on one step in eight for each unit, staggered by ID, so their cost stays bounded when many units need one.

`Step` runs in time linear in the number of units plus the number of neighbor buckets covering the units' bounding box, so its cost follows the units rather than the size of the map: neighbors come from that bucket grid, rebuilt each step without allocating once its buffers have grown, units push apart where their bodies overlap (`Separation`, `Gap`), and bodies slide along blocked cells and the grid's edge. Every unit's force is computed from the positions at the start of the step before any unit moves, so the result does not depend on the order units move in within a step. It does depend on the order units were added in, which sets their IDs and the order their pushes are summed, so a replay adds the same units in the same order.

Each `Unit` reports its `State`: `Idle` before any order and after `Stop`, `Moving`, and `Arrived` once within `ArriveRadius` of its slot. Arrived units hold their slots; one pushed well away walks back. `Units` returns every unit for drawing, `Unit(id)` one of them, and `Remove` deletes one.

On an Apple M1 Max, `BenchmarkCrowdStep1000` steps 1000 units crowding through a maze in about 0.4 ms, under 3% of a 60 fps frame. A step allocates only when a unit's own detour path outgrows the buffer it kept from its earlier searches, so allocations stop once every unit has planned its longest detour. The order itself costs more: `BenchmarkMove1000` lays out, assigns and builds both flow fields for 1000 units on that maze in about 6.5 ms with an empty `FieldCache`, so issue orders on input rather than every step. `TestCrowdThousandUnitsReachTarget` deterministically moves 1000 units in a box formation across a 160 by 128 maze and fails if any unit does not arrive, if any ends inside a wall, or if flow fields are not shared; `BenchmarkCrowdThousandUnits` measures the full crossing (reporting average and 99th-percentile step times) and is tracked alongside `BenchmarkCrowdStep1000` by the CI performance baselines in `benchmarks/baseline.yaml`.

## Formations

A `Formation` lays units out in rows across its facing direction, row 0 at the front:

| Shape | Layout | `Width` |
| --- | --- | --- |
| `line` | Side by side | Slots per row; 0 for one row |
| `column` | One behind another | Slots per row; 0 for one |
| `box` | A filled square | Slots per row; 0 for a square |
| `wedge` | One at the point, each row behind one wider | Ignored |

`Spacing` is the distance between neighboring slot centers; keep it at least twice the largest unit radius. `Layout` returns the slots around the formation's center, `Place` turns them into world positions for a center and a facing, and `Assign` matches units to slots in O(n log n) so that front units take front slots and units on the left take left slots. `Move` uses all three; call them yourself to preview a formation under the cursor.

A game's formations live in a YAML file, so a designer can edit them and review the diff:

```yaml
version: 1
formations:
  - name: line
    shape: line
    spacing: 1.5
  - name: block
    shape: box
    spacing: 1.2
    width: 8
```

`ParseFormations` refuses unknown fields, a second YAML document, a version other than 1, an empty or repeated name, an unknown shape, a spacing that is not a positive finite number and a negative width. `Encode` writes the fields in a fixed order with two-space indentation, so a file already in that form loaded and saved again comes back byte for byte; comments, other key orders, quoted values and fields at their zero value, such as `width: 0`, are normalized away. `Get` finds a formation by name.

## Selection

`Selection` is the set of selected units in the order they were selected, with `Set`, `Add`, `Remove`, `Toggle` and `Retain` (to drop units that died). `BoxSelector` turns pointer input in world space into selections:

```go
sel := &rts.Selection{}
box := &rts.BoxSelector{
	Picker:    crowd, // Crowd implements rts.Picker
	Selection: sel,
	Filter:    func(id rts.UnitID) bool { return owner[id] == me },
}

view := core.View{
	OnPressDown: func(ev core.PressEvent) { box.Press(toWorld(ev.Position), ev.Modifiers, ev.Clicks) },
	OnMouseMove: func(ev core.MouseEvent) { box.Drag(toWorld(ev.Position)) },
	OnPressUp:   func(ev core.PressEvent) { box.Release(toWorld(ev.Position)) },
}
```

- A press and release that moves less than `DragThreshold` picks the unit under the pointer (within `ClickSlop`); a click on empty ground clears the selection.
- A drag picks every unit its rectangle touches. While it is in progress, `Box` returns the rectangle and `DrawSelectionBox` draws it.
- A double click picks every unit of the clicked unit's `Kind` inside `View`, the world area on screen.
- `ModeFor` reads the modifiers held at the press: Shift adds to the selection, Control (Command on macOS) toggles units in and out, and no modifier replaces it.

## Control groups

`ControlGroups` holds the ten groups bound to the digit keys. `HandleKey` applies the usual bindings to a `core.KeyEvent`:

| Keys | Action |
| --- | --- |
| Control or Command + digit | `GroupAssigned`: the group becomes the selection |
| Shift + digit | `GroupAdded`: the selection joins the group, and the whole group is selected |
| Digit | `GroupRecalled`: the selection becomes the group |
| Digit twice within `DoubleTapSteps` | `GroupFocused`: the game centers its camera on the group (`Crowd.Centroid`) |

The double tap counts game steps rather than wall-clock time, so a replay recalls the same groups; pass the same step counter, such as `Crowd.Steps`, to every call. `Forget` and `Retain` drop dead units from every group.

## Debug drawing

`DrawFlowField` strokes each reachable cell's direction, and a cross on each goal, as one path under whatever camera transform the `core.Drawing` already has; use it in a debug overlay to see how units will move.
