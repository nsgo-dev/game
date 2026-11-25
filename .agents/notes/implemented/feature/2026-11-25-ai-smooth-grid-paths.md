# Agent Note: Line-of-sight grid path smoothing and Theta* in game/ai

Status: implemented

## Problem

Top-down and tactics games using `ai.AStar` on tile grids receive cell-by-cell staircase paths that look robotic when followed with `ai.FollowPath` or direct waypoint steering. Naively collapsing collinear cells leaves 45°/90° jaggedness, while naive zero-width raycasts (like Bresenham grid lines) clip wall corners when an agent has a non-zero body radius.

Meanwhile, `game/ai` is a **Stable** package (`TestStabilityTable` forbids importing Experimental packages), and hot pathfinding loops require deterministic output and zero heap allocations when reusing caller-owned buffers.

## Decision

Add radius-aware world-space path smoothing in `packages/game/ai/smooth.go` alongside an optional Theta* any-angle mode on `AStar` in `packages/game/ai/astar.go`:

1. **Exact swept-circle-to-AABB grid clearance (`GridLineOfSight`, `SmoothGridPath`, `SmoothGridPathInto`, `GridPathOptions`)**:
   - `GridPathOptions` configures world mapping (`Origin`, `CellSize`, optional `CellWidth` / `CellHeight` overrides) and circular body `Radius`.
   - `gridSegmentClear` sweeps along the segment's major axis, computing the exact parametric `[t0, t1]` band of candidate columns/rows padded by `Radius` (in `O(length * (1 + Radius/cellSize))` with zero allocations), and for each blocked cell (`!g.Walkable(col, row)`) computes the exact squared Euclidean distance between the segment and the cell's closed AABB (`segmentBoxDistSq`: Liang-Barsky slab test plus minimum over endpoints-to-box and box-corners-to-segment).
   - For `Radius > 0`, `SmoothGridPathInto` checks `d2 >= r*r` against the exact `float32`-rounded `core.Point` waypoints being emitted, mathematically guaranteeing every returned segment stays at least `Radius` away from every blocked cell. For `Radius <= 0`, it tests exact origin-canceled `float64` cell centers with a closed-box touch rejection (`d2 > zeroEpsSq`) so fractional `Origin` rounding cannot mask corner cuts.
   - Greedy backward lookahead from `n - 1` down to `anchor + 2` collapses open-ground staircases in a single LOS check while handling non-monotonic visibility around obstacles safely. If a narrow corridor has `Radius > cellSize/2`, no shortcut passes clearance and the original grid steps are conservatively preserved.
   - `SmoothGridPathInto` writes into `dst[:0]` with `0 B/op, 0 allocs/op` (verified by `testing.AllocsPerRun` and `BenchmarkSmoothGridPathInto`).

2. **Optional Theta* any-angle mode (`AStar.AnyAngle`)**:
   - Enabled on square topologies (`TopologySquare`, `TopologySquare4`, `TopologySquare8`) when the grid is not an `EdgeCostGrid`.
   - Uses an admissible Euclidean distance heuristic (`math.Hypot`) and relaxes neighbors directly from `parent[cur]` whenever `thetaLineOfSight` confirms every intersected cell is walkable and unit-cost (`MoveCost <= 1`), respecting `NoCornerCutting` (closed-box vs open-interior intersection).
   - Reuses all existing `AStar` scratch slices with zero allocations on `FindInto` after warm-up.
   - Docs explicitly recommend pairing positive body `Radius` smoothing with standard cell-by-cell `AStar` (`AnyAngle: false`) so corner waypoints remain available when a wide body cannot clear a corner directly.

3. **CryptQuest integration (`examples/cryptquest/game.go`)**:
   - `Enemy` keeps a reused `SmoothPath []core.Point` slice populated by `ai.SmoothGridPathInto` using `Radius = enemyHitboxSize * 0.5 * sqrt(2)` (the 10×10 hitbox circumradius, `< TileSize/2`), so enemies navigate doorways and pillars smoothly without snagging or allocating.

## Alternatives considered

### Bresenham / supercover line traversal with two offset rays

Casting two parallel rays offset by `±Radius` perpendicular to the segment is a common shortcut in tile engines. Rejected because parallel offset rays miss obstacle corners that poke into the interior of the swept capsule between the two rays (especially when `Radius` is comparable to `cellSize/2`) and fail to enforce circular end-cap clearance around endpoints. Exact segment-to-AABB distance across the padded axis sweep handles every corner and end-cap with negligible cost and zero allocations.

### Binary search for the farthest visible waypoint

Binary-searching `j in [anchor+2, n-1]` reduces LOS checks on long paths with monotonic visibility. Rejected because visibility along a path around pillars or U-shaped walls is non-monotonic (an intermediate cell may be occluded while a farther cell is visible, or vice versa), causing binary search to either miss valid shortcuts or pick suboptimal ones. Reverse linear scan from `n - 1` succeeds in 1 LOS check on open ground and always finds the true farthest clear waypoint.

### Embedding `CellSize` and `Radius` directly on `AStar`

Adding world-space parameters to `AStar` would let `Find` return `[]core.Point` directly. Rejected because `AStar` operates purely in unitless grid coordinates across square, hexagonal, and staggered topologies; keeping `AStar` cell-based and providing `SmoothGridPath` / `SmoothGridPathInto` + `GridPathOptions` keeps concerns cleanly separated and lets callers smooth paths from any source.

## Consequences

- **Benefits**: `game/ai` stays Stable with zero new dependencies; grid paths collapse to straight segments on open ground and maintain exact Euclidean clearance `>= Radius` around walls and corners; both `SmoothGridPathInto` and `AStar{AnyAngle: true}.FindInto` achieve `0 B/op, 0 allocs/op` on reused buffers; `examples/cryptquest` demonstrates real-world usage.
- **Costs**: `SmoothGridPath` evaluates `Grid.Walkable` only and does not preserve `CostGrid` terrain detours across high-cost walkable tiles (documented in GoDoc and docs); reverse linear scan is `O(k)` LOS calls per kept waypoint in worst-case dense mazes.
- **Verification**: `packages/game/ai/smooth_test.go` verifies open-ground collapse, independent exact+dense-sampled clearance assertions across 7 wall/corridor/rectangular-tile reference maps, `Radius > cellSize/2`, fractional `Origin`, Theta* optimality/mud-avoidance/determinism, `0 allocs/op` via `testing.AllocsPerRun` and benchmarks; `examples/cryptquest/game_test.go` verifies smoothed enemy pathing, clearance, and buffer reuse.
