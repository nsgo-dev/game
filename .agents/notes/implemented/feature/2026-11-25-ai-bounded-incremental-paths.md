# Agent Note: Bounded and incremental path searches with partial paths

Status: implemented

## Problem

In real-time games, clicking an unreachable or walled-off target should move units as close as possible instead of silently dropping the command, and many simultaneous long path searches across grids or navigation meshes must not blow a fixed simulation step budget. Previously, `ai.AStar`, `ai.NavMesh`, and `world3d.NavMesh` only supported unbounded single-shot searches returning `ok == false` whenever the goal was unreachable or off-mesh.

Additionally, `game/ai` is a **Stable** package and must not import Experimental packages (`TestStabilityTable`), whereas `game/world3d` is **Experimental** and benefits from sharing the same budget options and request queue without duplicating scheduling logic.

## Decision

Implement bounded expansion budgets, closest-explored partial paths, deterministic incremental searches, and a shared budgeted request queue across `packages/game/ai` and `packages/game/world3d`:

### 1. Shared options, status, and `IncrementalSearch` contract (`packages/game/ai/pathsearch.go`)

- `PathOptions{MaxExpansions, AllowPartial}` configures node expansion caps and partial-path fallback across `ai.AStar`, `ai.NavMesh`, and `world3d.NavMesh`.
- `SearchStatus` (`SearchInProgress`, `SearchComplete`, `SearchPartial`, `SearchFailed`) and `IncrementalSearch` (`Step(maxExpansions) (SearchStatus, int)`, `Status() SearchStatus`, `Expansions() int`) live in Stable `game/ai`.
- `world3d` imports Stable `game/ai` so `*world3d.NavMeshSearch` implements `ai.IncrementalSearch` directly and plugs into `ai.PathQueue` alongside `*ai.AStarSearch` and `*ai.NavMeshSearch`, keeping `game/ai` free of Experimental imports.

### 2. Identical single-shot and incremental search engines

- **Grid A\*** (`packages/game/ai/astar.go`): `AStar` embeds `astarState` (preserving internal field access for `topology.go` and zero-allocation `FindInto`/`FindWithOptionsInto` buffer reuse). Both single-shot `FindWithOptionsInto` (stack-allocated `astarRunner`) and incremental `AStarSearch` (independent buffers, reusable via `Reset`) execute the exact same `astarRunner.step` loop, guaranteeing bit-for-bit identical paths, costs, partial flags, and expansion counts across any split of step budgets. When `AllowPartial` is enabled, even blocked or out-of-bounds goals run A* from a walkable start and select the closest expanded cell by `(h, g, id)`.
- **2D NavMesh** (`packages/game/ai/navmesh.go`): `FindPathWithOptions` delegates directly to `NavMeshSearch.step(-1)`. For partial paths (disconnected regions, off-mesh goals, or exhausted budgets), each explored polygon evaluates its exact closest 2D point (`closestPointInPoly`) to the goal by `(h, g, portalID)` and runs the existing funnel string-puller from `start` through the portal corridor to that closest point.
- **3D NavMesh** (`packages/game/world3d/navmesh.go`): replaces `container/heap` with a canonical indexed binary heap ordered strictly by `(f, h, poly)` (eliminating stale pops and boxing allocations), delegates `FindPath` and `FindPathWithOptions` to `NavMeshSearch.step(-1)`, and computes exact closest 3D points on explored convex polygons (`closestPointOnPoly3D`, orthogonal plane projection clamped to boundary edges) before running 3D funnel smoothing.

### 3. Deterministic budgeted `PathQueue` (`packages/game/ai/pathsearch.go`)

- `PathQueue` stores active requests in a slice ordered by descending priority and FIFO submission order (zero map iteration, zero goroutines).
- `Step(budget)` strictly enforces `0 <= used <= budget`, finishes already-terminal searches at 0 expansion cost, supports both `PathQueueFIFO` and `PathQueueRoundRobin` (with configurable `SliceSize` or automatic even sharing within the active priority tier), supports `Cancel` before or mid-search, and removes completed entries before invoking `onDone` callbacks so re-entrant `Submit`/`Cancel` is safe and long-running queues never retain completed search buffers.
- Typed helpers `SubmitGrid` and `SubmitNavMesh` return `(PathRequestID, *Search)` so callers can either poll `search.Status()`/`search.Result()` directly or receive typed results via `onDone`.

## Alternatives considered

### Separate path queues in `game/ai` and `game/world3d`

Duplicating `PathQueue` in `world3d` would have avoided importing `game/ai` from `world3d`, but games using both 2D and 3D navigation would have to split and manually rebalance two separate per-frame budgets. Because `world3d` is Experimental and `ai` is Stable, importing `game/ai` from `world3d` passes `TestStabilityTable` and gives callers one unified `PathQueue` budget.

### Tracking the closest partial node on discovery (open-set push) instead of on expansion (pop)

Updating the best partial candidate when neighbors are first discovered would advance one hop further on a 1-expansion budget, but frontier nodes on weighted grids (`CostMap`, `EdgeCostGrid`) have not yet settled their optimal `gScore` and could return suboptimal partial paths through expensive terrain. Tracking the best candidate on pop guarantees settled optimal path costs to the chosen partial endpoint while visiting the exact same reachable component when a goal is walled off.

### Storing completed search results inside `PathQueue` for ID-based polling

Keeping a `completed map[PathRequestID]result` until callers drain it risks unbounded memory growth when games use callbacks or benchmarks submit thousands of requests. Returning `(PathRequestID, *AStarSearch)` / `(PathRequestID, *NavMeshSearch)` lets callers poll directly on their handle while allowing `PathQueue` to drop finished searches immediately.

## Consequences

- **Benefits**: 100% backward-compatible existing `Find`/`FindInto`/`FindPath` APIs; zero-allocation `AStar.FindInto` and `AStar.FindWithOptionsInto` preserved; partial paths work for walled-off, blocked, and off-mesh targets across 2D grids, 2D NavMeshes, and 3D NavMeshes; incremental searches are provably identical to single-shot searches; `PathQueue` guarantees strict per-step expansion budgets deterministically with no background goroutines.
- **Costs**: `AStarSearch`, `NavMeshSearch`, and `world3d.NavMeshSearch` each own independent search buffers while in flight (callers can reuse `AStarSearch` via `Reset`); `world3d` now depends on Stable `game/ai`.

## Verification

- `packages/game/ai/pathsearch_test.go` verifies walled-off, blocked, out-of-bounds, and boxed-in partial paths, `MaxExpansions` budgets, `0 allocs/op` for `FindWithOptionsInto`, exact equivalence between single-shot and incremental searches at step budgets `1, 5, 17, 64` (square 4-way, 8-way, hex, weighted `CostMap`, and 2D `NavMesh`), `PathQueue` FIFO/round-robin/priority/cancel/re-entrant callbacks/determinism, and `BenchmarkPathQueueBudgetedStep`.
- `packages/game/world3d/navmesh_search_test.go` verifies 3D partial paths on multi-level ramps/bridges and off-mesh goals, budget exhaustion, exact equivalence between single-shot and incremental 3D searches at step budgets `1, 5, 17`, `ai.PathQueue` integration, determinism, and `BenchmarkNavMesh3DPathQueueBudgetedStep`.
- Verified via `env -u GOROOT go tool nsgo test ./ai/...`, `env -u GOROOT go tool nsgo test ./world3d/...`, `env -u GOROOT go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid'`, `env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web`, `env -u GOROOT go run ./tools/changelog check`, and `bun run verify-notes`.
