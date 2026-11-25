# Agent Note: Tactical position queries and cover points in game/ai

Status: implemented

## Problem

Cover shooters, stealth games, and tactics games built on `game/ai` need to answer spatial positioning questions ("where should I stand?"): finding full or half cover shielding an agent from a threat, picking flanking angles with unobstructed line of fire, or retreating to low-threat reachable positions without two agents crowding the same spot. Previously `game/ai` provided pathfinding (`AStar`, `NavMesh`), steering, behavior trees, utility AI, GOAP, squads, and influence maps ([influence maps](2026-11-25-ai-influence-maps.md)), but no EQS-style candidate generation, scoring/filtering pipeline, cover representation, reservation tracker, or per-step raycast/path query budget.

The design must satisfy five constraints simultaneously:

1. Keep `game/ai` **Stable** with zero Experimental package imports (`TestStabilityTable` forbids `game/ai` importing `game/sceneformat` or `game/physics3d`).
2. Keep candidate ordering, tie-breaking, reservations, and budget consumption strictly deterministic across runs.
3. Perform zero heap allocations per query after warm-up across `Best`, `RunInto`, `RunAndReserve`, `PointReservations.Step`, grid `AStar` path tests, `NavMesh` path tests, and `InfluenceLayer` scoring.
4. Keep expensive raycast (`LOSFunc`) and pathfinding tests strictly within a configurable per-step `QueryBudget` shared across agents.
5. Support cover points authored as plain-text scene YAML components as well as generated from tilemap solid edges and 2D/3D physics collider edges.

## Decision

Split the feature cleanly along the Stable/Experimental package boundary:

- **`packages/game/ai/cover.go` & `packages/game/ai/tactical.go` (Stable)**: core `CoverPoint` / `CoverHeight` (`CoverHalf`, `CoverFull`), tilemap/grid/2D-physics cover generators (`GenerateCoverFromTilemap`, `GenerateCoverFromGrid`, `GenerateCoverFromBoxes`, `GenerateCoverFromWorld`, `GenerateCoverFromSegments`), `PointReservations`, `QueryBudget`, `TacticalQuery`, candidate generators (`CoverPointGenerator`, `RingGenerator`, `GridGenerator`, `NavMeshGenerator`), and ordered tests (`DistanceTest`, `LOSTest`, `AngleTest`, `PathDistanceTest`, `InfluenceTest`, `CoverHeightTest`, `CustomTest`).
- **`packages/game/ai/navmesh.go` (Stable)**: internal `navMeshScratch` and `findPathLength` reusing portal-graph A* and funnel buffers so `PathDistanceTest` over `*ai.NavMesh` performs zero heap allocations after warm-up.
- **`packages/game/sceneformat/cover.go` (Experimental)**: built-in `"cover_point"` component schema (`CoverPointComponent`, `RegisterCoverPoint`) and deterministic extraction (`ExtractCoverPoints`, `ExtractBuiltCoverPoints`) composing parent 2D transforms and validating `height` (`"half"`/`"full"`) and non-zero facing vectors.
- **`packages/game/physics3d/cover.go` (Experimental)**: `World.GenerateCoverPoints` and `GenerateCoverPointsFromBodies` projecting near-vertical faces of 3D AABBs, OBBs, spheres, and capsules onto the XZ plane, classifying `CoverHalf`/`CoverFull` from vertical height while ignoring low floor slabs, and pruning points overlapping neighboring colliders.

### Key invariants and conventions

- **Facing convention**: `CoverPoint.Facing` is a unit vector pointing from `Pos` into the cover obstacle (the incoming threat direction blocked by the cover; the obstacle's outward normal is `Facing.Neg()`). This makes `AngleCoverToTarget` intuitive: `0` radians means directly shielded against `TargetPos`, and `pi` radians means completely exposed.
- **Natural origin defaults (`OriginDefault`)**: `QueryOrigin` starts with `OriginDefault = 0` so zero-valued structs resolve to their natural reference point (`OriginQuerier` for `CoverPointGenerator`, `GridGenerator`, `NavMeshGenerator`, `DistanceTest`, `PathDistanceTest`; `OriginTarget` for `RingGenerator`, `LOSTest`, `AngleTest`) while allowing explicit overrides to `OriginQuerier`, `OriginTarget`, or `OriginCustom`.
- **Budgeted test priority & soundness**: before running an expensive test (`LOSTest` or `PathDistanceTest`) when `ctx.Budget != nil`, surviving candidates are sorted deterministically in place (non-allocating heapsort) by descending preliminary score (ties by generation index) so finite raycast/path budgets evaluate the best candidates first. When budget runs out during a filtering test (`TestFilterOnly` or `TestFilterAndScore`), unverified remaining candidates are dropped so an unchecked spot can never slip past a required LOS or reachability check; in `TestScoreOnly` mode, remaining candidates are kept with zero score contribution from that test.
- **Reservation exclusivity**: `PointReservations` checks reservations before tests run so agents do not waste raycast/path budget on spots already held by peers. Each agent holds at most one reservation: `Reserve` / `RunAndReserve` atomically replaces the agent's prior reservation on success and leaves it untouched on conflict. `Step(dt)` deterministically expires timed reservations in place with zero allocations.

## Alternatives considered

### Putting 3D collider and YAML cover parsing inside `game/ai`

Putting scene YAML component decoding or `physics3d.World` traversal directly inside `game/ai` would keep every cover entry point in one package. Rejected because `game/ai` is Stable while `game/sceneformat` and `game/physics3d` are Experimental, so importing either from `game/ai` fails `TestStabilityTable` and violates the repository's stability boundary. Exposing `CoverSegment` / `CoverPoint` in `game/ai` and placing the thin adapters in `sceneformat` and `physics3d` preserves clean one-way dependencies.

### Storing `CoverPoint.Normal` as outward wall normal instead of threat-facing `Facing`

Storing the obstacle's outward surface normal (pointing away from the wall into open space) matches physics contact normals, but requires negating the vector for every cover-vs-threat angle comparison (`dot(normal, toThreat) < 0` for good cover) and makes YAML authoring counter-intuitive (authors specify where the cover points away from rather than what direction it defends). Using `Facing` pointing into the obstacle aligns directly with `AngleCoverToTarget` (smaller angle = better protection) and matches issue #845's wording ("position, facing and height").

### Using `slices.SortFunc` and `NavMesh.FindPath` directly inside `TacticalQuery`

Calling `NavMesh.FindPath` and closure-based sorting would save ~250 lines of scratch code, but `NavMesh.FindPath` allocates multiple per-search slices and closure captures risk heap escapes on hot loops. Dedicated `navMeshScratch.findPathLength` and in-place heapsort over `q.order` guarantee `0 allocs/op` across the entire query pipeline, verified by `testing.AllocsPerRun == 0`.

## Consequences

- **Benefits**: `game/ai` stays Stable with no Experimental dependencies; reference cover queries are deterministic and `0 allocs/op` after warm-up across cover, ring, grid, and navmesh queries; multiple agents never reserve the same cover point; raycast and path queries stay strictly within `QueryBudget` per step; scene YAML `"cover_point"` components round-trip byte-for-byte unchanged via existing `sceneformat.ParseWith`/`Encode`.
- **Costs**: `navMeshScratch.findPathLength` duplicates the A*/funnel loop from `NavMesh.FindPath` inside `packages/game/ai/navmesh.go`, so future funnel-algorithm changes must keep both paths in sync; `PointReservations` uses linear scans over active reservations, which is allocation-free and fast for typical tactical squads (tens of agents) but would need spatial hashing if scaled to thousands of simultaneous reservations.

## Verification

- `packages/game/ai/tactical_test.go`: verifies reference-level best cover determinism across 20 repeated and fresh runs; multi-agent exclusive reservation, conflict rejection, re-query replacement, explicit death release, `Refresh`, `Step(dt)` timeout expiry, and `MinSeparation`; strict `QueryBudget` enforcement for raycasts and path queries across 3 agents plus `Reset()`; all generators (`CoverPointGenerator`, `RingGenerator`, `GridGenerator`, `NavMeshGenerator`) and tests (`DistanceTest`, `LOSTest`, `AngleTest`, `PathDistanceTest` with callback/`AStar`/`NavMesh`, `InfluenceTest`, `CoverHeightTest`, `CustomTest`); tilemap, grid, and 2D physics box/world cover generation; and `testing.AllocsPerRun == 0` after warm-up plus `BenchmarkTacticalQueryCover`.
- `packages/game/sceneformat/cover_test.go`: verifies `"cover_point"` registration, byte-for-byte YAML round-trip, parent rotation transform composition, validation errors, and integration with `ai.TacticalQuery`.
- `packages/game/physics3d/cover_test.go`: verifies 3D AABB, OBB, and capsule cover generation, floor slab and dynamic-body exclusion, determinism, and XZ-plane integration with `ai.TacticalQuery`.
