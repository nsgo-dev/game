# Agent Note: Extended 2D and 3D steering behaviors in game/ai

Status: implemented

## Problem

`game/ai` previously provided only six 2D steering primitives (`Seek`, `Flee`, `Arrive`, `Wander`, `Separation`, `FollowPath`) plus weighted `Combine`. Real-time games need predictive interception and evasion (`Pursue`, `Evade`, `OffsetPursuit`), full flocking (`Alignment`, `Cohesion`, and zero-allocation neighbor queries for hundreds of agents), local obstacle and corridor wall avoidance (`ObstacleAvoidance`, `WallAvoidance`), tactical positioning (`Hide`, `Interpose`), prioritized force budgeting (`CombinePrioritized`), and 3D / XZ-plane steering so grounded 3D agents can follow `world3d.NavMesh` paths — all while keeping `game/ai` deterministic, zero-allocation on hot loops, and free of Experimental package imports (`TestStabilityTable`).

## Decision

Add extended 2D steering in `packages/game/ai/steering_ext.go` and 3D / XZ-plane steering in `packages/game/ai/steering3d.go` around four design choices:

1. **Zero-import stability boundary via layout-compatible `ai.Vec3`**:
   - `game/ai` is Stable (`docs/en/stability.md`), while `game/math3d` and `game/world3d` are Experimental; importing either fails `TestStabilityTable`.
   - Define `type Vec3 struct { X, Y, Z float32 }` in right-handed Y-up space (`+X` right, `+Y` up, `-Z` forward), matching `math3d.Vec3` and `sound.Vec3` field-for-field (following the pattern in [3D positional audio](2026-11-24-sound-3d-positional-audio.md)). Callers convert `world3d.NavMesh.FindPath` waypoints with `ai.Vec3(p)` at zero cost.
   - Expose `Agent3D.PlanarXZ` so the same `*3D` functions serve both full 3D movers (flying/swimming) and grounded agents following sloped 3D navmesh corridors on the XZ plane without vertical steering drift.

2. **Deterministic, zero-allocation spatial hashing (`NeighborGrid`, `NeighborGrid3D`)**:
   - Store bucket heads (`buckets []int`), intrusive linked lists (`next []int`), touched bucket indices (`touched []int`), per-query deduplication stamps (`bucketStamp []uint32`), agent snapshots, and query scratch buffers in flat slices with power-of-two integer cell hashing (`hashCell2D`, `hashCell3D`).
   - `Rebuild` inserts in reverse index order so bucket chains traverse agents in ascending index order; `Query` scans cell coordinates in fixed ascending order and falls back to a linear scan when the query bounding box covers more cells than indexed agents.
   - No Go `map` is used anywhere, guaranteeing both `0 allocs/op` after warm-up and bit-identical positions across runs.

3. **Winding-agnostic `WallAvoidance` with feelers plus direct radius containment**:
   - Combine three forward feeler whiskers (`0°`, `±35°`) with closest-point-on-segment radius clearance (`1.35 * a.Radius`) so agents moving parallel to corridor walls or rounding inner corners never clip into wall endpoints.
   - Compute wall normals dynamically toward the agent's side (`wallNormalToward`) so callers cannot break containment by reversing `Wall{A, B}` endpoints.
   - Apply wall repulsion strictly along the combined outward normal while arresting inward velocity, preserving tangential sliding speed along corridors.

4. **Strict priority budget truncation (`CombinePrioritized`, `CombinePrioritized3D`)**:
   - Spend `maxForce` strictly in slice order and break immediately once a force saturates the remaining budget, so lower-priority steering (`FollowPath`, `Cohesion`) cannot perturb or cancel higher-priority safety forces (`WallAvoidance`, `ObstacleAvoidance`).

## Alternatives considered

### Importing `game/math3d` directly in `game/ai`

Strongest argument: taking `math3d.Vec3` directly would let callers pass `world3d.NavMesh.FindPath` slices straight into `FollowPath3D` without a loop converting elements to `ai.Vec3`.

Rejected because `game/ai` is Stable and `game/math3d` is Experimental; importing `game/math3d` fails `TestStabilityTable` and couples a stable package to experimental math APIs. Defining `ai.Vec3` with identical `X, Y, Z float32` fields matches `sound.Vec3` and allows a backwards-compatible type alias (`type Vec3 = math3d.Vec3`) if `game/math3d` is promoted to Stable later.

### Using a Go `map[[2]int][]int` for `NeighborGrid`

Strongest argument: a Go map keyed by cell coordinates handles arbitrarily sparse worlds without hash collisions or bucket sizing logic.

Rejected because Go map iteration order is randomized (breaking bit-identical determinism when summing neighbor forces), clearing and repopulating per-cell slices invites heap allocations on moving flocks, and hash collisions in the flat bucket table are already filtered cleanly by exact radius checks and `bucketStamp` deduplication at `0 allocs/op`.

### Using `math2d.Circle` instead of `ai.Obstacle`

Strongest argument: `game/math2d` is Stable, so `ObstacleAvoidance` and `Hide` could take `[]math2d.Circle` directly without defining a new `Obstacle` struct.

Rejected because all existing 2D steering and navmesh APIs in `game/ai` use `core.Point` (`core.Points` float32), not `math2d.Vec2`. Defining `Obstacle{Center core.Point, Radius float32}` and `Wall{A, B core.Point}` keeps the entire `game/ai` 2D API uniform on `core.Point` without adding a new package dependency.

## Consequences

- **Benefit**: `game/ai` covers Reynolds' full 2D and 3D steering suite while remaining Stable with zero new package imports.
- **Benefit**: 200 flocking agents step with `0 allocs/op` after warm-up and bit-identical positions across runs in both 2D and 3D.
- **Benefit**: Prioritized blending with `WallAvoidance` keeps an agent's full radius clear of corridor walls across 90-degree bends without stalling forward progress.
- **Cost**: Callers passing `world3d.NavMesh.FindPath` results into `FollowPath3D` convert `[]math3d.Vec3` elements via `ai.Vec3(p)`. **Upgrade trigger**: if `game/math3d` becomes Stable, `type Vec3 = math3d.Vec3` eliminates the conversion without breaking callers.

## Verification

- `packages/game/ai/steering_ext_test.go`:
  - `TestSteeringExtPursueEvadeOffsetTable` and `TestSteeringExtFlockingAndTacticalTable` verify deterministic single-step and multi-step behavior for `Pursue`, `Evade`, `OffsetPursuit`, `Alignment`, `Cohesion`, `ObstacleAvoidance`, `WallAvoidance`, `Hide`, `Interpose`, and `CombinePrioritized`.
  - `TestNeighborGridMatchesBruteForce`, `TestFlocking200AgentsZeroAllocAndDeterministic`, and `BenchmarkFlocking200Agents` verify exact spatial query results, bit-identical positions across runs, and `0 allocs/op` after warm-up for 200 flocking agents.
  - `TestCorridorPathFollowingKeepsRadiusClear` and `TestCorridor3DPathFollowingKeepsRadiusClear` verify an agent keeps its full radius clear of every corridor wall on every step while completing a 90-degree corridor path.
  - `TestSteering3DBehaviorsTable`, `TestSteering3DFollowsWorld3DNavMeshPath`, `TestFlocking3D200AgentsZeroAllocAndDeterministic`, and `BenchmarkFlocking200Agents3D` verify full 3D and XZ-plane steering, real sloped `world3d.NavMesh` path following, and zero-alloc 3D flocking.
- Verified with `gofmt -l`, `go tool nsgo test ./ai/...`, `go tool nsgo test ./ai/... -- -run=^$ -bench=Flock -benchmem`, `go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid'`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
