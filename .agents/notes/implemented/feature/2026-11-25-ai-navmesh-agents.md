# Agent Note: 2D and 3D navmesh path-corridor agents

Status: implemented

## Problem

`game/ai.NavMesh` and `game/world3d.NavMesh` previously exposed only stateless one-shot `FindPath` queries. Games had to re-implement waypoint following, corridor recovery when physics or neighbors pushed an agent off course, moving-target pursuit without per-frame A* spikes, arrival deceleration, unreachable-target states, and off-mesh link handoffs — and 3D games had no navigation agent layer at all. Meanwhile, `game/ai` is Stable (`docs/en/stability.md`) and cannot import Experimental packages (`game/math3d` or `game/world3d`), while `game/world3d` must keep agents locked to sloped ramps and vertically separated bridges without snapping between stacked floors.

## Decision

Implement symmetric deterministic path-corridor agents (`NavAgent`, `NavAgentGroup`, `NavAgentState`, `NavLinkEvent`, `NavOffMeshLink`) in `packages/game/ai/navagent.go` (2D over `ai.NavMesh`) and `packages/game/world3d/navagent.go` (3D over `world3d.NavMesh`), backed by corridor, revision, and link primitives added to `packages/game/ai/navmesh.go` and `packages/game/world3d/navmesh.go`:

1. **Active polygon corridor with direct funnel corner queries**:
   - Each `NavAgent` stores its active polygon index slice (`corridor []int`) and directed portal list instead of a static waypoint array.
   - `NextCorner()` runs the Simple Stupid Funnel Algorithm directly from the agent's current position along the remaining corridor, skipping vertices within `1e-3` units and stopping at the first off-mesh link entry point.
   - Steering uses physically scaled acceleration (`clamp((desiredVel - vel)/dt, MaxAcceleration)`), cruising at `MaxSpeed` toward intermediate corners and applying linear `Arrive` deceleration inside `ArrivalRadius` before settling to `NavAgentArrived` within `StoppingDistance`.
   - Boundary overshoots are clamped onto `corridor[0]` or its connected shared-edge neighbors with outward-normal velocity removal so agents glide cleanly around reflex corners without leaving the navmesh.

2. **Bounded local corridor repair before full A* replanning**:
   - When an agent advances or is pushed off its corridor (`SetPosition`), `repairCorridorStart` advances directly if the new polygon lies later in `corridor`, or runs a bounded BFS over non-off-mesh shared edges up to `MaxRepairDepth` (default 2) hops to prepend the shortest bridge back into `corridor` without running A* or incrementing `ReplanCount()`.
   - When `SetTarget` shifts the destination, `repairCorridorEnd` updates in O(1) on the same polygon, truncates on earlier corridor polygons, or extends via bounded BFS up to `MaxRepairDepth` hops (trimming loops). Sustained tail drift (`tailExtensions >= MaxRepairDepth`) keeps the extended corridor active immediately while scheduling a throttled full replan to keep the route optimal around obstacles.
   - Both `NavMesh` types track a monotonic `Revision()` bumped by `AddOffMeshLink`, `SetEdgeFlags`, and `BumpRevision()`, allowing agents to invalidate cached corridors and recover from `NavAgentUnreachable` automatically when mesh connectivity changes.

3. **Budgeted round-robin and staggered group replanning (`NavAgentGroup`)**:
   - Agents attached to a `NavAgentGroup` defer full A* replans out of `SetTarget` into `NavAgentGroup.Step(dt)`, where Phase 1 ticks cooldowns and runs local repairs, Phase 2 executes at most `MaxReplansPerStep` full replans in deterministic round-robin cursor order (optionally filtered by `agentID%StaggerStride == step%StaggerStride` and `ReplanInterval`), and Phase 3 steps movement for every agent.
   - No Go map iteration, goroutines, or hidden randomness appear anywhere in the step loop, guaranteeing bit-identical replay positions across runs.

4. **Off-mesh and flagged link traversal hook (`OnLink`)**:
   - Portals carry `Flags`, `LinkID`, and `OffMesh` metadata (with `NavLinkOffMesh = 1 << 0`), and `NavMesh.AddOffMeshLink` / `SetEdgeFlags` register custom links that A* and funnel smoothing split across cleanly.
   - `NavAgentConfig.OnLink(agent, ev, dt) bool` gives callers frame-by-frame control over jumps, ladders, teleports, or door-opening pauses on flagged shared edges; returning `true` resumes normal corridor steering (advancing onto `ev.End` if the callback did not already place the agent inside `ToPoly`). When `OnLink` is `nil`, off-mesh links traverse linearly at `MaxSpeed`.

5. **3D surface tracking and package split**:
   - `world3d.NavAgent` steers on horizontal XZ while sampling exact `NavPoly.HeightAt(x, z)` along ramps and bridges, checks connected shared-edge neighbors before global `FindPolygon` so stacked floors never falsely snap vertically, and checks arrival distance in full 3D so an agent beneath a bridge never falsely arrives at a target overhead.
   - Keeping the 2D agent in Stable `game/ai` and the 3D agent in Experimental `game/world3d` preserves `TestStabilityTable` (following the stability boundary in [extended steering behaviors](2026-11-25-ai-steering-extensions.md)).

## Alternatives considered

### Placing both 2D and 3D `NavAgent` in `game/ai`

Strongest argument: callers could import a single package (`game/ai`) for both 2D and 3D navigation agents and share one generic implementation parameterized over vector types.

Rejected because `world3d.NavMesh` lives in Experimental `game/world3d` and uses `math3d.Vec3`; importing `game/world3d` from Stable `game/ai` fails `TestStabilityTable`. Abstracting `world3d.NavMesh` behind a large interface in `game/ai` would leak internal portal/funnel/plane invariants and force every 3D caller to wrap `world3d.NavMesh` in an adapter. Placing the 3D agent directly alongside `world3d.NavMesh` keeps zero-overhead direct access to `NavPoly.HeightAt`, `NavPoly.ContainsXZ`, and `NavLink`.

### Re-running `FindPath` on every target move or displacement instead of maintaining polygon corridors

Strongest argument: storing only a `[]Point` / `[]Vec3` waypoint slice and re-running `FindPath` when displaced or when the target moves is simpler to implement.

Rejected because it violates issue #828's core requirement: agents pushed onto an adjacent polygon or chasing targets across polygon boundaries would trigger expensive full A* searches every time, causing frame spikes in crowds and losing Detour-style cheap local corridor repair.

### Running synchronous A* inside `SetTarget` even for grouped agents

Strongest argument: `State()` and `Corridor()` would always be populated immediately after `SetTarget` even when the agent belongs to a `NavAgentGroup`.

Rejected because calling `SetTarget` on dozens of grouped agents in one update frame would bypass `MaxReplansPerStep` and recreate the exact frame spike `NavAgentGroup` exists to prevent. Standalone agents with `ReplanInterval == 0` still plan immediately on `SetTarget`, while grouped agents strictly defer full A* to `NavAgentGroup.Step(dt)` (and callers can invoke `ForceReplan()` explicitly when needed).

## Consequences

- **Benefit**: Both 2D (`game/ai`) and 3D (`game/world3d`) games gain Detour/Godot-style path-corridor navigation agents with cheap local repair, smooth arrival, explicit state machines, off-mesh/flagged link callbacks, and strictly budgeted crowd replanning.
- **Benefit**: `game/ai` remains Stable with zero Experimental imports; `world3d.NavAgent` handles sloped ramps and multi-level bridges natively with exact `HeightAt` tracking.
- **Benefit**: Fixed-step execution is bit-for-bit deterministic across replays in both 2D and 3D.
- **Cost**: Corridor-repair logic is mirrored in `packages/game/ai/navagent.go` and `packages/game/world3d/navagent.go` rather than shared through a generic package, so future corridor algorithm changes must be applied in both files. **Maintenance note**: keep the two implementations and test suites in parity.

## Verification

- `packages/game/ai/navagent_test.go`:
  - `TestNavAgentFollowsCorridorAndSlowsToArrive`, `TestNavAgentChasesMovingTarget`, `TestNavAgentPushedOffCorridorRepairsLocallyWithoutReplan`, `TestNavAgentTargetLocalEndRepairAndTruncation`, `TestNavAgentUnreachableAndMeshRevisionRecovery`, `TestNavAgentOffMeshLinkAndFlaggedEdgeCallbacks`, `TestNavAgentGroupStaggeredReplanBudget`, and `TestNavAgentDeterministicReplayBitIdentical`.
- `packages/game/world3d/navagent_test.go`:
  - `TestNavMesh3DNearestPolygonAndFindCorridor`, `TestNavAgent3DClimbsRampAndMultiLevelBridge`, `TestNavAgent3DChasesMovingTarget`, `TestNavAgent3DPushedOffCorridorRepairsLocallyWithoutReplan`, `TestNavAgent3DUnreachableAndOffMeshLinkCallbacks`, `TestNavAgent3DGroupStaggeredReplanBudget`, and `TestNavAgent3DDeterministicReplayBitIdentical`.
- Verified with `gofmt -w`, `go tool nsgo test ./ai/...`, `go tool nsgo test ./world3d/...`, `go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid'`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
