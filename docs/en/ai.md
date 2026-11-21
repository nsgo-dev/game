# ai

`game/ai` provides deterministic game AI building blocks stepped cleanly on `game.Loop`: grid `AStar` pathfinding, 2D `NavMesh` navigation with funnel smoothing, steering behaviors, and behavior trees.

```go
import "github.com/nsgo-dev/game/packages/game/ai"
```

Nothing in `ai` uses background goroutines or hidden global randomness.

## Grid A* pathfinding

`AStar` searches any `Grid` or weighted `CostGrid` (`BoolGrid`, `CostMap`, or the adapter `GridFunc`), reusing its internal buffers across calls:

```go
grid := ai.GridFunc{
	Cols: levelMap.Width,
	Rows: levelMap.Height,
	Walk: func(col, row int) bool {
		return levelMap.FlagsAt(col, row)&tilemap.TileSolid == 0
	},
	Cost: func(col, row int) float64 {
		if isMud(col, row) {
			return 2.5
		}
		return 1
	},
}

astar := ai.NewAStar()
astar.Diagonal = true // NoCornerCutting is true by default

path, cost, ok := astar.Find(grid, ai.Cell{Col: 1, Row: 1}, ai.Cell{Col: 18, Row: 10})
_, _ = path, cost
_ = ok
```

Use `astar.FindInto(dst, grid, start, goal)` to reuse a caller-owned `[]ai.Cell` buffer with zero allocations per search. `NewBoolGrid` and `NewCostMap` provide flat-slice grids when you do not already have a tilemap.

## 2D NavMesh with funnel smoothing

For open arenas or non-grid worlds, build a `NavMesh` from convex 2D polygons. `BuildNavMesh` normalizes polygon winding, automatically connects adjacent polygons along shared edges (including partial collinear overlaps), and `FindPath` runs portal-graph A* followed by Simple Stupid Funnel Algorithm string-pulling:

```go
mesh := ai.BuildNavMesh([][]core.Point{
	{{X: 0, Y: 0}, {X: 120, Y: 0}, {X: 120, Y: 80}, {X: 0, Y: 80}},
	{{X: 120, Y: 20}, {X: 240, Y: 20}, {X: 240, Y: 100}, {X: 120, Y: 100}},
})

waypoints, ok := mesh.FindPath(core.Point{X: 16, Y: 16}, core.Point{X: 220, Y: 85})
_ = waypoints
_ = ok
```

Inspect mesh geometry with `Contains(p)`, `FindPolygon(p)`, `NearestPolygon(p)`, `Neighbors(index)`, `Polygon(index)`, and `PolygonCount()`.

## Steering behaviors

`Agent` integrates smooth 2D steering forces (`MaxSpeed`, `MaxForce`, `Radius`) on fixed steps via `Agent.Step(force, dt)`:

```go
agent := ai.Agent{
	Pos:      core.Point{X: 20, Y: 20},
	MaxSpeed: 120,
	MaxForce: 300,
	Radius:   10,
}
var followState ai.PathFollowState

// Inside game.Loop.Update:
force := ai.Combine(agent.MaxForce,
	ai.WeightedForce{Force: ai.FollowPath(agent, waypoints, &followState, 8, false), Weight: 1.0},
	ai.WeightedForce{Force: ai.Separation(agent, nearbyAgents, 24), Weight: 1.5},
)
agent.Step(force, dt)
```

Built-in steering functions:

| Function | Behavior |
| --- | --- |
| `Seek(a, target)` | Accelerates toward `target` at `MaxSpeed`. |
| `Arrive(a, target, slowRadius)` | Scales desired speed smoothly down to zero inside `slowRadius`. |
| `Flee(a, threat, panicRadius)` | Accelerates away from `threat` while closer than `panicRadius` (or always if `<= 0`). |
| `FollowPath(a, path, &state, radius, loop)` | Follows waypoints, using `Arrive` on the final waypoint when `loop` is `false`. |
| `Separation(a, neighbors, desiredSep)` | Pushes away from close neighbors (defaults to `a.Radius + neighbor.Radius` when `<= 0`). |
| `Wander(a, &state, radius, dist, jitter, nextRand)` | Smooth random wandering using your deterministic `nextRand() float32` in `[0, 1)`. |
| `Combine(maxForce, forces...)` | Sums weighted forces and clamps magnitude to `maxForce`. |

## Behavior trees

Compose enemy decision logic from `Node` trees returning `StatusSuccess`, `StatusFailure`, or `StatusRunning` on each `Tick(dt)`:

```go
tree := ai.Selector(
	ai.Guard(playerInRange, ai.Sequence(
		ai.Cooldown(1500*time.Millisecond, ai.Action(attackPlayer)),
		ai.Wait(300*time.Millisecond),
	)),
	ai.Sequence(
		ai.Condition(hasPatrolPath),
		ai.Action(patrolStep),
	),
)

// Inside game.Loop.Update:
tree.Tick(dt)
```

Available nodes:

- **Composites**: `Sequence`, `Selector` (preempts and resets lower-priority running branches when a higher-priority branch succeeds or runs), `Parallel(policy, ...)` (`RequireAll` or `RequireOne`).
- **Decorators**: `Guard(cond, child)`, `Cooldown(d, child)`, `Inverter`, `Succeeder`, `Failer`, `Repeater(count, child)`, `UntilFail`.
- **Leaves**: `Action(fn)`, `Condition(fn)`, `Wait(d)`.
