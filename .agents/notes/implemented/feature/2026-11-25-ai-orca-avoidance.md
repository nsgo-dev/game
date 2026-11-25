# Agent Note: Velocity-based local avoidance (ORCA) in game/ai and game/rts

Status: implemented

## Problem

Reactive push-apart separation (`ai.Separation`, `rts.Crowd`'s default `AvoidancePush`) acts only after bodies overlap, causing head-on units to collide or deadlock and groups to jostle in narrow doorways. Both 2D games (`math2d.Vec2`) and grounded 3D navmesh agents (`ai.Vec3` on the XZ plane) need predictive, collision-free velocity-obstacle avoidance with static obstacle edges, avoidance layers/masks, priority/stationary yielding, spatial-hash scaling to 1000 agents within a desktop frame budget, zero heap allocations per step, and bit-identical determinism independent of neighbor iteration order — all while keeping `game/ai` Stable (`TestStabilityTable`) with no Experimental package imports.

## Decision

Implement an RVO2-style Optimal Reciprocal Collision Avoidance solver in `packages/game/ai/orca.go` and integrate it as an opt-in avoidance mode in `packages/game/rts/crowd.go` around five design choices:

1. **Shared 2D core for 2D XY and 3D XZ-plane navigation**:
   - Use `math2d.Vec2` for 2D `ORCAAgent` / `ORCAObstacle` and the existing layout-compatible `ai.Vec3` (from [extended steering](2026-11-25-ai-steering-extensions.md)) for 3D `ORCAAgent3D` / `ORCAObstacle3D`, projecting `(X, Z) <-> (X, Y)` through reused scratch slices (`SolveXZ`, `SolveAgentXZ`, `StepXZ`) while preserving `Pos.Y` and zeroing `Vel.Y`.
   - Perform all velocity-obstacle geometry and 2D linear programming (`linearProgram1`, `linearProgram2`, `linearProgram3`) internally in `float64` before converting final velocities to `float32`, preventing cancellation drift in tight multi-agent constraints.

2. **Two-sided static obstacle edges as hard LP3 constraints**:
   - Canonicalize `ORCAObstacle` endpoints (`A <= B`) before sorting and orient each segment relative to the agent (`det(p1, p2) >= 0`), so walls work identically from either side regardless of caller winding order.
   - Place static obstacle half-planes ahead of agent half-planes (`numObstLines`) so `linearProgram3` keeps wall clearance as a hard constraint when overcrowded agents fall back to minimum-penetration projection (relaxing obstacle lines only if the obstacles themselves form an infeasible pinch).

3. **Asymmetric layer/mask filtering, smooth priority weighting, and right-hand symmetry breaking**:
   - Default `ORCAAgent.Layer == 0` to `ORCALayerDefault` (`1 << 0`), `ORCAAgent.Mask == 0` to `ORCAMaskAll`, and `ORCAObstacle.Layer == 0` to `ORCAMaskAll`, so zero-configured agents avoid each other and all default walls while custom layers (e.g. ground vs. air) isolate cleanly.
   - When an agent avoids a non-cooperative neighbor (whose `Mask` ignores it) or a `Stationary` / zero-`MaxSpeed` neighbor, apply full avoidance responsibility (`factor = 1.0`). Between cooperative neighbors, apply `resp = pB / (pA + pB)` inside/on the VO boundary and symmetrically clamped slack `clamp(1 - resp, 0.1, 0.9)` outside the VO so high-priority units hold ground while low-priority units still accelerate smoothly from rest and both sides' factors always sum to `1.0`.
   - Break perfectly symmetric head-on cutoff-circle deadlocks by tilting the supporting tangent slightly toward the agent's right perpendicular within half the valid cutoff arc; because the tilt is antisymmetric across facing agents and strictly inside the arc, head-on pairs and antipodal circles pass cleanly without overlapping or relying on randomness or slice indices.

4. **Order-independent determinism and zero-allocation spatial hashing**:
   - Index agents in a flat power-of-two spatial hash (`buckets`, `next`, `touched`, `bucketStamp`) with cell-AABB circle culling and no Go maps.
   - Maintain a bounded max-heap of at most `MaxNeighbors` lightweight `{idx, distSq}` candidates ordered strictly by ascending `distSq` then intrinsic agent properties (`ID`, `Pos`, `Vel`, `Radius`, `Priority`, `MaxSpeed`, `Layer`, `Mask`, `Stationary`, `PrefVel`), then insertion-sort the `<= MaxNeighbors` entries before LP solving. Shuffling neighbor or obstacle slices yields bit-identical velocities.

5. **Opt-in `rts.AvoidanceORCA` mode in `rts.Crowd`**:
   - Keep `AvoidancePush` as the zero-value default in `CrowdOptions.Avoidance` so existing crowds remain unchanged.
   - Under `AvoidanceORCA`, reuse `ORCASolver` and agent/velocity scratch slices in `Crowd.stepORCA`, set `Radius = u.Radius + 0.5*Gap`, and give `Moving` units priority `3` over `Arrived`/`Idle` units (`1`) with gentle intent signaling when stalled (`|Vel| < 0.2*|PrefVel|`) so settled tight formations politely part for teammates reaching interior slots while still keeping wall sliding (`slide`) and zero-allocation deterministic stepping.

## Alternatives considered

### Using `core.Point` instead of `math2d.Vec2` for `ORCAAgent`

Strongest argument: `ai.Agent` and `ai.NavMesh` use `core.Point`, so matching them would avoid converting `core.Point` in older 2D steering callers.

Rejected because newer `game/ai` systems (`InfluenceLayer`, `TacticalQuery`, `CoverPoint`, `Blackboard`) and all of `game/rts` (`Crowd`, `Unit`, `FlowField`) use `math2d.Vec2`, which is Stable and provides vector arithmetic (`Add`, `Sub`, `Scale`, `Dist`, `Len`). Using `math2d.Vec2` eliminates conversions in `rts.Crowd` and matches the current `game/ai` API direction.

### One-sided (CCW-only) obstacle polygons as in C++ RVO2

Strongest argument: skipping edges where the agent lies on the negative side avoids generating constraints from back-facing walls of closed convex obstacles.

Rejected because games frequently pass standalone wall segments or doorway jambs (`A -> B`) without guaranteed winding, where one-sided checks silently let agents walk through the back side of a wall. Two-sided orientation matches `ai.WallAvoidance` and prevents accidental clipping while back faces of thick boxes remain safely shadowed by near faces.

### Breaking head-on deadlocks via random perturbation or agent ID ordering

Strongest argument: RVO2's sample programs break head-on symmetry by adding a tiny random jitter to preferred velocity each step.

Rejected because random jitter introduces an external RNG dependency into a pure velocity query, and ID-based deflection fails when callers leave `ID == 0` or swap agent IDs. Geometric right-hand cutoff tilting within the valid supporting arc is self-contained, collision-free, and completely independent of IDs and iteration order.

## Consequences

- **Benefit**: `game/ai` provides predictive local avoidance that resolves head-on encounters, 32-agent antipodal circle swaps, and narrow doorways without overlapping or wall clipping in both 2D and 3D XZ.
- **Benefit**: Neighbor-order and obstacle-order independence guarantee bit-identical replays across runs with `0 B/op` and `0 allocs/op` after warm-up.
- **Benefit**: `rts.Crowd` gains predictive local avoidance via `AvoidanceORCA` without breaking existing `AvoidancePush` behavior or performance baselines.
- **Cost**: `ORCASolver` maintains internal scratch slices and is not safe for concurrent use; callers stepping multiple crowds concurrently create one `ORCASolver` per goroutine.
- **Cost**: `rts.Crowd` under `AvoidanceORCA` relies on `Crowd.slide` for grid wall containment rather than extracting tile edges into `ORCAObstacle` slices, keeping step cost independent of maze size at the trade-off that units react to blocked grid cells at contact rather than `TimeHorizonObst` ahead.

## Verification

- `packages/game/ai/orca_test.go`:
  - `TestORCAHeadOnPassWithoutOverlap`, `TestORCACircleSwapWithoutOverlap` (16 and 32 agents), and `TestORCA3DXZPlane` verify smooth collision-free passing and `Pos.Y` preservation.
  - `TestORCAStaticObstacleDoorwayNoClipping` verifies narrow doorway navigation under both segment orientations without clipping.
  - `TestORCALayersAndMasks`, `TestORCAPriorityAndStationaryHolding`, `TestORCAInfeasibleLP3Fallback`, `TestORCADeterminismAndOrderIndependence`, `TestORCAZeroAllocationsAfterWarmup`, and `TestORCAEdgeCases` verify layer/mask filtering, priority/stationary responsibility, LP3 fallback, bit-identical order independence and replay determinism, and `0 allocs/op`.
  - `BenchmarkORCA1000Agents` and `BenchmarkORCA1000AgentsXZ` measure 1000-agent throughput with `0 allocs/op`.
- `packages/game/rts/crowd_test.go`:
  - `TestCrowdORCAHeadOnAndMazeNavigation` verifies head-on avoidance, maze arrival, wall clearance, determinism, and `0 allocs/op` under `AvoidanceORCA`.
  - `BenchmarkCrowdStep1000ORCA` measures 1000-unit ORCA crowd stepping.
- Verified with `go tool nsgo test ./ai/...`, `go tool nsgo test ./rts/...`, `go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid'`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
