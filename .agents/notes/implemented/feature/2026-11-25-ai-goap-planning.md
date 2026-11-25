# Agent Note: Goal-oriented action planning (GOAP) in game/ai

Status: implemented

## Problem

`game/ai` already provides grid A*, 2D navigation meshes, steering behaviors, behavior trees, and utility AI scoring, but has no symbolic planner over world states. Survival, simulation, and immersive-sim games need agents that dynamically chain actions toward prioritized goals (such as fetching an axe, chopping wood, and building a fire), replan when an action fails or external events invalidate the plan, cap planning work with a node budget, compose cleanly with behavior trees, and optionally author action sets in strict versioned YAML—all while keeping `game/ai` deterministic, reusable across calls without heap allocations, and free of Experimental package imports.

## Decision

Implement GOAP in new files `packages/game/ai/goap.go` and `packages/game/ai/goap_yaml.go` with four core boundaries:

### 1. Fixed-size comparable `WorldState`, `Conditions`, and `Effects`

Represent `WorldState` as a 48-byte pointer-free value type holding `[2]uint64` (up to `MaxBoolFacts = 128` boolean facts) and `[MaxIntFacts]int16` (up to `MaxIntFacts = 16` small integer facts), mapped from human-readable names through `FactSchema`. Represent `Conditions` using `[2]uint64` boolean masks/values plus `uint16` integer mask and closed `[lo, hi]` `int16` intervals (`[MaxIntFacts]intBounds`), maintaining canonical zeroed unused bits and slots so `Conditions` is directly comparable (`==`) and hashable with zero heap allocations.

Integer comparisons (`eq`, `gte`, `lte`, `gt`, `lt`, and `RequireIntRange`) all normalize into closed intervals `[lo, hi]` (with an inverted interval `[1, 0]` encoding unsatisfiable contradictions). Regressing `[lo, hi]` backwards through `SetInt(v)` verifies `v in [lo, hi]` and clears the constraint; regressing through `AddInt(delta)` shifts bounds to `[lo - delta, hi - delta]` with `int16` saturation/bounds checking and merges action preconditions via interval intersection.

### 2. Regressive A* `GOAPPlanner` with admissible heuristic, node budget, and buffer reuse

`GOAPPlanner` searches backward from `goal.Target` toward `start WorldState`:

- **Usability & procedural checks**: Each `GOAPAction.Check(start)` is evaluated once at the start of a plan into a reusable `usable []bool` slice, keeping procedural checks out of the A* inner loop and letting the heuristic reflect only currently usable actions.
- **Admissible heuristic & dead-end pruning**: Precomputes minimum achieving cost per `(boolFact, value)` and per integer fact (`SetInt` and cost-per-unit `AddInt`), taking `max(max_f c_f, sum_f c_f / maxEffectsPerAction)` across unsatisfied facts so multi-effect actions never break admissibility. Any unsatisfied fact with no usable achieving action prunes the branch immediately.
- **Determinism & node budget**: Actions iterate in declaration order; heap ties break strictly by `(f, h, depth, creationID)`. Expanding more than `NodeBudget` (default `DefaultGOAPNodeBudget = 1024`) nodes returns `ErrBudgetExceeded` instead of searching unboundedly.
- **Zero-allocation buffer reuse**: Search nodes, binary heap indices, and a generation-stamped open-addressing hash table (`buckets`, `bucketGen`, `gen`) live on `GOAPPlanner` and reset in O(1) via `gen++`, so `PlanInto` and `PlanBestGoalInto` perform `0 allocs/op` after warm-up.

### 3. Auto-replanning `PlanRunner` implementing `ai.Node`

`PlanRunner` directly implements `Node` (`Tick(dt) Status`, `Reset()`) so it runs standalone on `game.Loop` or embeds inside any behavior tree composite (`Selector`, `Sequence`, `Guard`). On each tick it validates the remaining suffix of the active plan by forward-simulating preconditions, procedural checks, and goal reachability from `world()`. When an action returns `StatusFailure`, `PlanRunner` resets that action node, marks it in a per-attempt `failed` mask, and immediately replans to take an alternate route (clearing `failed` whenever an action succeeds, the world invalidates the plan, or planning exhausts all routes). Higher-priority unsatisfied goals that have a valid plan preempt lower-priority running plans cleanly via a reusable `preemptBuf` slice.

### 4. Versioned YAML format (`GOAPVersion = 1`, `ai#goap`)

`ParseGOAP` and `EncodeGOAP` decode and write `GOAPConfig` with `KnownFields(true)`, trailing-document rejection, declared `facts` validation (catching unknown fact references, duplicate facts, bool/int name collisions, out-of-range `int16` values, zero `add` deltas, and contradictory bounds), and deterministic 2-space YAML output. Integer conditions use word operators (`eq`, `gte`, `lte`, `gt`, `lt`) and integer effects use `set`/`add` so YAML never needs scalar quoting for `>=`/`<=`. `BuildGOAP` compiles a validated `GOAPConfig` plus optional `GOAPBindings.Checks` into a `GOAPSet` (`Schema`, `Planner`, `Goals`). Fixtures live under `packages/game/ai/testdata/goap/*.yaml` so they do not collide with Utility AI's `testdata/*.yaml` glob.

## Alternatives considered

### Forward A* search from `start` to `goal`

Forward search over concrete `WorldState`s is simpler to implement for arbitrary integer mutations, because actions apply directly to concrete numbers. Rejected because forward search branches on every applicable action regardless of goal relevance, explodes quickly in worlds with many unrelated facts and actions, and contradicts the issue's regressive-from-goal specification. Interval regression keeps integer facts exact while preserving regressive goal-directed pruning.

### Using Go's built-in `map[Conditions]int` for the A* closed/open index

A standard Go map is fewer lines of code than a custom open-addressing hash table. Rejected because clearing and re-populating a map of ~100-byte struct keys is slower than an O(1) generation increment and risks runtime bucket growth/allocations during repeated planning; a 50-line generation-stamped open-addressing table mirrors `AStar`'s generation-stamped slices in `packages/game/ai/astar.go` and guarantees `0 allocs/op` on `PlanInto`.

### Symbolic comparison operators (`>=`, `<=`, `==`) in YAML

Using punctuation strings like `">="` looks familiar from code, but `>` starts a YAML folded block scalar and requires quotes on every occurrence. Rejected in favor of unquoted word operators (`eq`, `gte`, `lte`, `gt`, `lt`), matching the unquoted enum style of `utility_yaml.go` (`linear`, `step`, etc.).

## Consequences

- **Benefits**: `game/ai` gains deterministic, optimal regressive GOAP over both 128-bit boolean bitsets and up to 16 small integer facts; repeated planning via `PlanInto` allocates 0 times; `PlanRunner` handles action failures, world-state invalidation, and goal preemption automatically and plugs directly into behavior trees as a `Node`; YAML action sets pass full `formattest.Run` conformance without touching any Experimental packages.
- **Costs**: Fact capacity is bounded at compile time to 128 booleans and 16 `int16` integers per `WorldState` (sufficient for game agents, while keeping states compact and stack-allocated); integer preconditions/effects support constant bounds, sets, and additive deltas rather than arbitrary expressions between two integer facts.

## Verification

- `packages/game/ai/goap_test.go` covers `FactSchema` limits and collisions, `WorldState`/`Conditions`/`Effects` across both 64-bit bool words and `int16` clamping, a multi-goal survival camp reference domain with deterministic tie-breaking, `SetInt` + `AddInt` and side-effect restoration, `PlanBestGoal` priority/fallback, `NodeBudget` clean failure with `ErrBudgetExceeded`, `PlanRunner` action-failure replanning to an alternate route inside `game.Loop`, world-state invalidation, higher-priority goal preemption, behavior tree `Selector`/`Guard` integration, `testing.AllocsPerRun` (`0 allocs/op` for `PlanInto` and `PlanBestGoalInto`), `BenchmarkGOAPPlan`, and `formattest.Run` + YAML validation tests.
- Verified via `go tool nsgo test ./ai/...`, `go tool nsgo test . -- -run 'TestFormatPackagesRunConformance|TestFormatsPage|TestStabilityTable|TestAgentSkillValid'`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`.
