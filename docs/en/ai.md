# ai

`game/ai` provides deterministic game AI building blocks stepped cleanly on `game.Loop`: steering behaviors, velocity-based local avoidance (ORCA) in 2D and 3D XZ, behavior trees, utility AI scoring, goal-oriented action planning (GOAP), attack tokens and squads, spatial influence maps, EQS-style tactical position queries with cover points and reservations, and perception with sight, hearing and memory. Grids, A*, graphs, waypoints and 2D navigation meshes are in [pathfind](pathfind.md).

```go
import "github.com/nsgo-dev/game/pkg/ai"
```

Nothing in `ai` uses background goroutines or hidden global randomness.

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

Built-in 2D steering functions:

| Function | Behavior |
| --- | --- |
| `Seek(a, target)` | Accelerates toward `target` at `MaxSpeed`. |
| `Arrive(a, target, slowRadius)` | Scales desired speed smoothly down to zero inside `slowRadius`. |
| `Flee(a, threat, panicRadius)` | Accelerates away from `threat` while closer than `panicRadius` (or always if `<= 0`). |
| `Pursue(a, target, maxLookAhead)` | Intercepts a moving `target` by predicting its position from `target.Vel`, capped to `maxLookAhead` seconds when positive. |
| `Evade(a, pursuer, panicRadius, maxLookAhead)` | Flees the predicted position of `pursuer` while closer than `panicRadius` (or always if `<= 0`). |
| `OffsetPursuit(a, leader, localOffset, slowRadius, maxLookAhead)` | Follows `leader` at a heading-frame offset (`X` forward, `Y` left) using `Arrive`. |
| `FollowPath(a, path, &state, radius, loop)` | Follows waypoints, using `Arrive` on the final waypoint when `loop` is `false`. |
| `Separation(a, neighbors, desiredSep)` | Pushes away from close neighbors (defaults to `a.Radius + neighbor.Radius` when `<= 0`). |
| `Alignment(a, neighbors, radius)` | Steers toward the average heading of neighbors within `radius`. |
| `Cohesion(a, neighbors, radius)` | Steers toward the center of mass of neighbors within `radius`. |
| `ObstacleAvoidance(a, obstacles, maxAhead)` | Deflects around the nearest circular `Obstacle` intersecting the forward detection corridor. |
| `WallAvoidance(a, walls, feelerLength)` | Keeps clear of `Wall` line segments using three forward feelers and direct radius containment. |
| `Hide(a, pursuer, obstacles, standoff, slowRadius)` | Picks the closest hiding spot behind a circular `Obstacle` opposite `pursuer` (falls back to `Evade` if none exist). |
| `Interpose(a, targetA, targetB, slowRadius)` | Intercepts the predicted midpoint between two moving agents. |
| `Wander(a, &state, radius, dist, jitter, nextRand)` | Smooth random wandering using your deterministic `nextRand() float32` in `[0, 1)`. |
| `Combine(maxForce, forces...)` | Sums weighted forces and clamps magnitude to `maxForce`. |
| `CombinePrioritized(maxForce, forces...)` | Spends `maxForce` in priority order (e.g. `WallAvoidance` before `FollowPath`), ignoring lower-priority forces once the budget is saturated. |

### Zero-allocation flocking with `NeighborGrid`

For large flocks, index agents once per step in a `NeighborGrid` so `Rebuild`, `Query`, and `QueryInto` reuse internal buffers with zero heap allocations after warm-up:

```go
grid := ai.NewNeighborGrid(48)

// Inside game.Loop.Update:
grid.Rebuild(agents)
for i := range agents {
	neighbors := grid.Query(agents[i].Pos, 48)
	forces[i] = ai.CombinePrioritized(agents[i].MaxForce,
		ai.WeightedForce{Force: ai.Separation(agents[i], neighbors, 18), Weight: 1.6},
		ai.WeightedForce{Force: ai.Alignment(agents[i], neighbors, 45), Weight: 1.0},
		ai.WeightedForce{Force: ai.Cohesion(agents[i], neighbors, 48), Weight: 0.9},
	)
}
for i := range agents {
	agents[i].Step(forces[i], dt)
}
```

### 3D and XZ-plane steering

`Agent3D`, `Vec3`, `Obstacle3D`, `Wall3D`, `WeightedForce3D`, and `NeighborGrid3D` provide the matching 3D steering suite (`Seek3D`, `Arrive3D`, `Flee3D`, `Pursue3D`, `Evade3D`, `OffsetPursuit3D`, `FollowPath3D`, `Separation3D`, `Alignment3D`, `Cohesion3D`, `ObstacleAvoidance3D`, `WallAvoidance3D`, `Hide3D`, `Interpose3D`, `Combine3D`, `CombinePrioritized3D`).

`ai.Vec3` shares the exact `X, Y, Z float32` layout of `math3d.Vec3` (right-handed Y-up: `+X` right, `+Y` up, `-Z` forward), so values convert with `ai.Vec3(v)` at zero cost while keeping `game/ai` free of experimental imports. Set `Agent3D.PlanarXZ = true` for grounded agents following `pathfind3d.NavMesh` paths on the horizontal XZ plane:

```go
rawPath, ok := navMesh3D.FindPath(start, goal)
if ok {
	path := make([]ai.Vec3, len(rawPath))
	for i, p := range rawPath {
		path[i] = ai.Vec3(p)
	}
	agent := ai.Agent3D{Pos: ai.Vec3(start), MaxSpeed: 12, MaxForce: 40, Radius: 0.5, PlanarXZ: true}
	var st ai.PathFollowState
	force := ai.FollowPath3D(agent, path, &st, 0.8, false)
	agent.Step(force, dt)
}
```

## Velocity-based local avoidance (ORCA)

Push-apart separation reacts only after bodies overlap, causing head-on deadlocks and jostling in doorways. `ORCASolver` implements Optimal Reciprocal Collision Avoidance (RVO2-style velocity obstacles with 2D linear programming and minimum-penetration infeasible fallback) so agents steer smoothly around neighbors and static obstacle edges before contact:

```go
solver := ai.NewORCASolver(ai.ORCAConfig{
	TimeHorizon:     2.0, // seconds to look ahead for agents
	TimeHorizonObst: 1.0, // seconds to look ahead for static walls
	MaxNeighbors:    16,
})

walls := []ai.ORCAObstacle{
	{A: math2d.V2(-12, 0), B: math2d.V2(-1.5, 0)},
	{A: math2d.V2(1.5, 0), B: math2d.V2(12, 0)},
}

// Inside game.Loop.Update:
for i := range agents {
	agents[i].PrefVel = desiredVelocity(agents[i], goals[i])
}
// Step updates Vel and Pos synchronously, or call Solve(out, agents, walls, dt)
// to obtain collision-free velocities without mutating agent positions.
solver.Step(agents, walls, dt)
```

- **2D and 3D XZ-plane sharing**: use `Solve`, `SolveAgent`, and `Step` with `ORCAAgent` / `ORCAObstacle` (`math2d.Vec2`) in 2D, or `SolveXZ`, `SolveAgentXZ`, and `StepXZ` with `ORCAAgent3D` / `ORCAObstacle3D` (`ai.Vec3`) so 2D and grounded 3D navmesh agents share the same solver while preserving `Pos.Y` and zeroing `Vel.Y`.
- **Static obstacle edges**: `ORCAObstacle` line segments are treated as two-sided (winding-agnostic) and act as hard constraints during LP3 infeasible fallback so overcrowded agents stay clear of walls and doorway jambs.
- **Layers, masks, and priority**: set `Layer` (zero defaults to `ORCALayerDefault`, bit 0) and `Mask` (zero defaults to `ORCAMaskAll`; obstacle `Layer` 0 also defaults to `ORCAMaskAll`) to separate movement domains such as ground vs. air. When an agent avoids a non-cooperative neighbor whose `Mask` ignores it, or a `Stationary` / zero-`MaxSpeed` neighbor, it automatically takes full avoidance responsibility (`1.0`). Among cooperative neighbors, higher `Priority` smoothly yields less (`pB / (pA + pB)`) so holding-position units hold their ground while lower-priority units steer around them.
- **Deterministic and zero-allocation**: internal spatial-hash neighbor lookups and candidate/obstacle sorting canonicalize every constraint by distance and intrinsic agent properties before solving the LP, so shuffling neighbor or obstacle order produces bit-identical velocities and replays produce identical positions every step. Reusing `ORCASolver` and output buffers allocates `0 B/op` after warm-up.

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

- **Composites**: `Sequence`, `Selector` (preempts and resets lower-priority running branches when a higher-priority branch succeeds or runs), `Parallel(policy, ...)` (`RequireAll` or `RequireOne`), `RandomSelector(stream, ...)`, `RandomSequence(stream, ...)`, `WeightedSelector(stream, options...)` (using `WeightedNode{Node, Weight}`), `UtilitySelector(reasoner, ctx, rng, subtrees)`.
- **Decorators**: `Guard(cond, child)`, `BlackboardCondition(bb, key, op, value, abort, child)`, `Cooldown(d, child)` (tracks elapsed tree time even while its branch is not ticked and survives `Reset()` from interruptions; call `ResetCooldowns(root)` or `bt.ResetCooldowns()` to clear active cooldowns explicitly, such as on respawn), `TimeLimit(d, child)`, `Retry(n, child)`, `Service(interval, fn, child)`, `WithAttackToken(pool, agentID, priority, child)`, `Inverter`, `Succeeder`, `Failer`, `Repeater(count, child)`, `UntilFail`.
- **Leaves**: `Action(fn)`, `Condition(fn)`, `Wait(d)`, `HasAttackToken(pool, agentID)`, `SquadHasOrder(squad, kind)`, `SquadHasRole(squad, agentID, role)`, `SquadStateBool(squad, key)`, `SquadOrderAction(squad, fn)`.

### Blackboard and observer aborts

`Blackboard` holds typed keys (`bool`, `int`, `float64`, `string`, 2D vectors via `math2d.Vec2` / `core.Point`, 3D vectors via `ai.Vec3`, and `uint64` entity IDs) scoped per agent with an optional shared parent blackboard for a squad or team (`NewChildBlackboard(parent)`). Reads fall back to the parent when a key is not set locally; `Set*` writes locally and `SetShared*` targets the parent. `KeyRevision(key)` and `Revision()` track mutations monotonically without heap allocations, and `Watch(key, fn)` delivers synchronous change notifications (propagating non-shadowed parent updates) with a returned cancel function.

`BlackboardCondition(bb, key, op, value, abort, child)` compares `key` using `BBOpEq`, `BBOpNe`, `BBOpLt`, `BBOpLte`, `BBOpGt`, `BBOpGte`, `BBOpSet`, or `BBOpUnset` (omitting `child` uses it as a condition leaf). Its `AbortMode` controls how key changes interrupt running branches:

- `AbortNone` (`none`): checks the condition only on entry.
- `AbortSelf` (`self`): resets its own running child (or an enclosing `Sequence`'s later running child when used as a leaf) on the next tick if the watched key changes and the condition becomes false.
- `AbortLower` (`lower`): aborts a running lower-priority sibling in an enclosing `Selector` on the next tick when the watched key changes and the condition becomes true.
- `AbortBoth` (`both`): combines `AbortSelf` and `AbortLower`.

A `Selector` uses key-driven observer preemption only for higher-priority branches whose gating is governed solely by `BlackboardCondition` nodes (such as a `BlackboardCondition` wrapping actions, or a `Sequence` whose gating prefix consists solely of `BlackboardCondition` nodes). When a higher-priority branch mixes `BlackboardCondition` with plain `Condition`, `Guard`, `WithAttackToken`, or another non-blackboard gating node, `Selector` re-evaluates that branch on every tick so non-blackboard conditions still preempt running lower-priority siblings, while `AbortSelf` and `AbortBoth` continue to self-abort when the watched key changes.

```go
squadBB := ai.NewBlackboard()
agentBB := ai.NewChildBlackboard(squadBB)
agentBB.SetBool("enemy_seen", false)

tree := ai.Selector(
	ai.BlackboardCondition(agentBB, "enemy_seen", ai.BBOpEq, ai.BBBool(true), ai.AbortBoth,
		ai.Action(chaseTarget),
	),
	ai.Action(patrol),
)
```

### Behavior trees in YAML

Author behavior trees in versioned `*.bt.yaml` files (`version: 1`, JSON Schema `https://game.nsgo.dev/schemas/bt.v1.json`) with `ParseBehaviorTree` / `ParseBehaviorTreeWith`, `LoadBehaviorTreeFS`, `EncodeBehaviorTree`, `BuildBehaviorTree`, and `BuildBehaviorTreeFS`:

```go
doc, err := ai.LoadBehaviorTreeFS(assetsFS, "ai/guard.bt.yaml")
if err != nil {
	return err
}
tree, err := ai.BuildBehaviorTree(doc, ai.BTBindings{
	Blackboard: agentBB,
	Stream:     rng,
	FS:         assetsFS,
	Actions: map[string]func(ai.BTNodeContext) (ai.Node, error){
		"attack": func(ctx ai.BTNodeContext) (ai.Node, error) {
			dmg, _ := ctx.Params.Int("damage")
			return ai.Action(func(dt time.Duration) ai.Status { return doAttack(dmg) }), nil
		},
	},
})
```

- Supports every built-in composite and decorator (`sequence`, `selector`, `parallel`, `random_selector`, `random_sequence`, `weighted_selector`, `guard`, `cooldown`, `time_limit`, `retry`, `service`, `inverter`, `succeeder`, `failer`, `repeater`, `until_fail`, `blackboard_condition`), `wait`, game-registered `action` / `condition` leaves with typed `params`, and `subtree` references by `path` with `${param}` parameter substitution and `keys` blackboard key remapping.
- `ParseBehaviorTreeWith`, `LoadBehaviorTreeFS`, and `BuildBehaviorTree` refuse unknown fields, unknown node types, invalid operators or abort modes, trailing YAML documents, and circular subtree references, reporting file, line, column, field path, and fix hints via `*loaderr.Error`.
- `EncodeBehaviorTree` writes canonical 2-space YAML prefixed with `BTSchemaComment` in fixed key order so load-and-save round trips change nothing.

### Timeouts, retries, deterministic random choices, and services

- **`TimeLimit(d, child)`** fails and calls `child.Reset()` if `child` is still `StatusRunning` once `d` of game time has elapsed.
- **`Retry(n, child)`** reruns a failing child up to `n` additional times (`1+n` total attempts), returning `StatusRunning` between attempts and `StatusSuccess` as soon as `child` succeeds.
- **`RandomSelector(stream, children...)`** and **`RandomSequence(stream, children...)`** shuffle child order at the start of each run using a caller-supplied `*random.Stream` and keep that order across `StatusRunning` ticks until completion or `Reset()`.
- **`WeightedSelector(stream, options...)`** draws `WeightedNode` children without replacement proportional to `Weight` using `stream` (skipping non-positive, `NaN`, or infinite weights and `nil` nodes), keeping a running child across ticks and remaining deterministic for a given seed.
- **`Service(interval, fn, child)`** invokes `fn` immediately on entry into `child` and then every `interval` of game time while `child` continues returning `StatusRunning`, stopping cleanly when `child` finishes or is reset.

```go
rng := random.New(42)
combat := ai.Service(250*time.Millisecond, refreshTarget,
	ai.WeightedSelector(rng,
		ai.WeightedNode{Node: ai.TimeLimit(2*time.Second, chargeAttack), Weight: 2},
		ai.WeightedNode{Node: ai.Retry(2, rangedShot), Weight: 3},
	),
)
```

### Saving and restoring behavior tree state

Save mid-run behavior tree state (for save slots, rollback netcode, or state dumps) with `SnapshotBehaviorTree` / `RestoreBehaviorTree`, or wrap a root `Node` in `NewBehaviorTree(root)` to call `bt.Snapshot()` and `bt.Restore(snap)`:

```go
bt := ai.NewBehaviorTree(tree)
bt.Tick(dt)

// Capture every built-in stateful node by deterministic structural path ("0/1/2").
snap, err := bt.Snapshot()
if err != nil {
	return err
}
data, err := yaml.Marshal(snap) // plain struct: marshals to YAML or JSON

// Later, restore into a fresh tree of the exact same shape:
var loaded ai.BehaviorTreeSnapshot
if err := yaml.Unmarshal(data, &loaded); err != nil {
	return err
}
if err := freshBT.Restore(loaded); err != nil {
	return err // refuses mismatched tree shapes without mutating freshBT
}
```

- **Complete built-in coverage**: captures progress for `Sequence`, `Selector`, `Parallel`, `Repeater`, `Cooldown`, `Wait`, `TimeLimit`, `Retry`, `RandomSelector`, `RandomSequence`, `WeightedSelector`, `Service`, `BlackboardCondition`, `Guard`, `WithAttackToken`, and `UtilitySelector`, plus wrapper/leaf kinds (`Inverter`, `Succeeder`, `Failer`, `UntilFail`, `Action`, `Condition`) so shape mismatches are always detected.
- **Transactional shape validation**: `RestoreBehaviorTree` checks `Version`, every node's structural path and kind, `Parallel` policies, and state bounds before applying any change. Mismatched trees return an error wrapping `ErrInvalidBTSnapshot` naming the exact path and expected vs got kind without mutating the target tree.
- **Custom and shared nodes**: custom `Node` implementations can participate by implementing `SnapshotNode` (`SnapshotKind`, `SnapshotState`, `RestoreState`), and custom composites or decorators can expose their children via `ChildNodes` (`Children() []Node`). If a tree uses `RandomSelector`, `RandomSequence`, or `WeightedSelector`, also save and restore the associated `*random.Stream` state for identical subsequent draws.

### Inspecting behavior trees

Attach optional labels with `Named(name, child)` (or set `name:` on any node in `*.bt.yaml`, which `BuildBehaviorTree` preserves automatically) and inspect live execution with a reusable `BTInspector`:

```go
tree := ai.Named("root", ai.Selector(
	ai.Named("attack", ai.Sequence(
		ai.Named("in_range", ai.Condition(playerInRange)),
		ai.Named("swing", ai.Action(attackPlayer)),
	)),
	ai.Named("patrol", ai.Action(patrolStep)),
))

inspector := ai.NewBTInspector(tree)
traceBuf := make([]ai.BTNodeTrace, 0, inspector.Len())
pathBuf := make([]ai.BTNodeTrace, 0, inspector.Len())

// Inside game.Loop.Update (zero heap allocations once warmed up):
tree.Tick(dt)
traceBuf = inspector.WalkInto(traceBuf)
pathBuf = ai.RunningPathInto(pathBuf, traceBuf)
_ = pathBuf
```

- **Read-only pre-order snapshots**: `WalkInto(dst)` (or one-shot `Walk()` / `InspectBehaviorTree(root)`) fills `BTNodeTrace` records (`Path`, `Depth`, `Name`, `Kind`, `LastStatus`, `Ticked`, `Active`, `StatusString()`) without mutating execution state or allocating per tick once `dst` has capacity `inspector.Len()`.
- **Running path**: `Active` is `true` for every node along the currently running branch; extract that path with `RunningPathInto(dst, nodes)` or `inspector.RunningPath()`.
- **Deterministic text for `statedump`**: `inspector.Format()` (or `FormatBTTrace(nodes)`) renders a canonical indented tree prefixed with `* ` on active nodes so a game can store it in [`statedump`](statedump.md) `Values` (`dump.SetValue("guard_bt", inspector.Format())`) and diff AI decisions across runs.
- **Live overlay & steering guides**: pass `gamedebug.BehaviorTreeSnapshot("guard", inspector)` to [`game/pkg/tooling/gamedebug`](gamedebug.md) to show the tree with its running path highlighted, and call `overlay.DrawSteering` to draw velocity, desired velocity, target, and path guides (all stripped under `-tags release`).
## Utility AI

When agents weigh continuous factors (distance, health, ammo, threat) instead of fixed boolean priorities, score candidate `UtilityAction`s with a `Reasoner[C]` using Dave Mark's Infinite Axis Utility System (IAUS):

```go
type UnitState struct {
	HP       float64
	EnemyDst float64
	Potions  float64
}

reasoner := ai.NewReasoner(
	ai.UtilityAction[UnitState]{
		Name:   "attack",
		Weight: 1.2,
		Considerations: []ai.Consideration[UnitState]{
			{
				Name:  "close",
				Input: func(s UnitState) float64 { return s.EnemyDst },
				Min:   0, Max: 200,
				Curve: ai.LinearCurve(-1, 0, 1),
			},
			{
				Name:  "healthy",
				Input: func(s UnitState) float64 { return s.HP },
				Min:   0, Max: 100,
				Curve: ai.LogisticCurve(1, 10, 0.35, 0),
			},
		},
	},
	ai.UtilityAction[UnitState]{
		Name:   "heal",
		Weight: 1.1,
		Considerations: []ai.Consideration[UnitState]{
			{
				Name:  "low_hp",
				Input: func(s UnitState) float64 { return s.HP },
				Min:   0, Max: 100,
				Curve: ai.PolynomialCurve(-1, 2, 0, 1),
			},
			{
				Name:  "has_potion",
				Input: func(s UnitState) float64 { return s.Potions },
				Curve: ai.StepCurve(1, 0, 1),
			},
		},
	},
)
reasoner.Momentum = 0.1 // bonus for the currently active action

idx, score := reasoner.Choose(state, nil)
_, _ = reasoner.ActionName(idx), score
```

### Considerations and response curves

Each `Consideration[C]` reads a number from `C`, optionally normalizes `[Min, Max]` into `[0, 1]` (or clamps directly to `[0, 1]` when `Max <= Min`), and maps it through a `ResponseCurve` whose output is clamped to `[0, 1]`:

| Kind / Constructor | Formula |
| --- | --- |
| `CurveLinear` / `LinearCurve(slope, shiftX, shiftY)` | `slope*(x - shiftX) + shiftY` |
| `CurvePolynomial` / `PolynomialCurve(slope, exponent, shiftX, shiftY)` | `slope*(x - shiftX)^exponent + shiftY` |
| `CurveLogistic` / `LogisticCurve(slope, steepness, midpoint, shiftY)` | `slope / (1 + exp(-steepness*(x - midpoint))) + shiftY` |
| `CurveLogit` / `LogitCurve(slope, midpoint, shiftY)` | `slope*ln(u/(1 - u)) + shiftY`, `u = x - midpoint + 0.5` |
| `CurveStep` / `StepCurve(threshold, low, high)` | `low` for `x < threshold`, `high` for `x >= threshold` |
| `CurveResource` / `ResourceCurve(eval)` | Delegates to a `CurveEvaluator` (`Eval(float32) float32`), satisfied directly by `*resource.Curve` |
| `CustomCurve(fn)` | Wraps a `func(float64) float64` as a `CurveEvaluator` |

### Scoring, momentum, and selection

- **Compensation factor**: an action's `N` considerations are multiplied together (any `0` vetoes the action immediately) and compensated with `modFactor = 1 - 1/N`, `score = score + (1 - score)*modFactor*score`, then multiplied by `Weight` (defaulting to `1` when `0`; negative disables the action).
- **Momentum bonus**: while an action is currently active and its base score is positive, `Reasoner.Momentum + UtilityAction.Momentum` is added so the agent does not oscillate between two nearly equal options every step. A vetoed action (`0`) never receives momentum.
- **Top-N weighted choice**: set `Reasoner.TopN > 1` and pass a deterministic `*random.Stream` to pick among the top `N` positive-scoring actions weighted by score. Leaving `TopN <= 1` or passing a `nil` stream picks the highest-scoring action deterministically. Ties always break in fixed action declaration order.
- **Zero-allocation multi-agent sharing**: call `reasoner.ChooseWithCurrent(ctx, agent.ActiveAction, rng)` to score many agents with one shared `Reasoner` and zero heap allocations after warm-up.

### Behavior tree integration

Use `UtilitySelector(reasoner, ctx, rng, subtrees)` to embed utility scoring as a behavior tree `Node`. Each `UtilitySelector` tracks its own active action for momentum and calls `Reset()` on an interrupted running subtree when another action overtakes it:

```go
node := ai.UtilitySelector(reasoner, func() UnitState { return unit.State() }, rng, map[string]ai.Node{
	"attack": attackSubtree,
	"heal":   healSubtree,
})
```

### YAML configuration

Define actions, considerations, and curves in a versioned YAML file (`version: 1`) parsed with `ParseUtility` and encoded deterministically with `EncodeUtility`. Bind Go input functions and an optional `resource` curve loader (`LoadCurve`) via `BuildReasoner`:

```go
cfg, err := ai.ParseUtility(yamlBytes)
if err != nil {
	return err
}
reasoner, err := ai.BuildReasoner(cfg, ai.UtilityBindings[UnitState]{
	Inputs: map[string]func(UnitState) float64{
		"target_distance": func(s UnitState) float64 { return s.EnemyDst },
		"health_ratio":    func(s UnitState) float64 { return s.HP / 100 },
	},
	LoadCurve: func(path string) (ai.CurveEvaluator, error) {
		return resource.LoadAs[*resource.Curve](resLib, path)
	},
})
```

Unknown fields, unsupported versions, and trailing YAML documents are refused; canonical documents round-trip byte-for-byte unchanged (see [File Formats](formats.md#utility-ai)).

## Goal-oriented action planning (GOAP)

When agents need to assemble multi-step action chains dynamically toward goals (fetch an axe, chop wood, build a fire) without hand-wiring every transition, define a `FactSchema`, `GOAPAction`s, and prioritized `GOAPGoal`s for a `GOAPPlanner`:

```go
schema := ai.NewFactSchema()
hasAxe := schema.MustBool("has_axe")
hasFire := schema.MustBool("has_fire")
wood := schema.MustInt("wood")

planner, err := ai.NewGOAPPlanner(
	ai.GOAPAction{
		Name:    "fetch_axe",
		Cost:    1,
		Effects: ai.Effects{}.SetBool(hasAxe, true),
		Check:   func(ai.WorldState) bool { return camp.AxeAvailable() },
	},
	ai.GOAPAction{
		Name:          "chop_wood",
		Cost:          1,
		Preconditions: ai.Conditions{}.RequireBool(hasAxe, true),
		Effects:       ai.Effects{}.AddInt(wood, 1),
	},
	ai.GOAPAction{
		Name:    "gather_sticks",
		Cost:    3,
		Effects: ai.Effects{}.AddInt(wood, 1),
	},
	ai.GOAPAction{
		Name:          "build_fire",
		Cost:          1,
		Preconditions: ai.Conditions{}.RequireIntGte(wood, 2),
		Effects:       ai.Effects{}.SetBool(hasFire, true).AddInt(wood, -2),
	},
)
if err != nil {
	return err
}

goal := ai.GOAPGoal{
	Name:     "light_campfire",
	Priority: 10,
	Target:   ai.Conditions{}.RequireBool(hasFire, true),
}

plan, cost, err := planner.PlanNames(ai.WorldState{}, goal)
// plan == ["fetch_axe", "chop_wood", "chop_wood", "build_fire"], cost == 4
_, _, _ = plan, cost, err
```

### World state, conditions, and effects

- **`WorldState`** is a compact comparable value type packing up to `MaxBoolFacts` (128) named boolean facts into a `[2]uint64` bitset and up to `MaxIntFacts` (16) small `int16` integer facts. Register names with `FactSchema.Bool` / `FactSchema.Int` (or `MustBool` / `MustInt`).
- **`Conditions`** express required boolean values (`RequireBool`) and integer bounds (`RequireIntEq`, `RequireIntGte`, `RequireIntLte`, `RequireIntGt`, `RequireIntLt`, `RequireIntRange`) for action preconditions and goal targets.
- **`Effects`** express boolean assignments (`SetBool`) and integer updates (`SetInt`, `AddInt`).
- **`GOAPAction.Check`** is an optional procedural precondition (`func(WorldState) bool`) evaluated against the current world state so actions can verify runtime conditions (such as reachability or entity availability) outside the symbolic bitset.

### Regressive A* search and node budget

- `GOAPPlanner` runs deterministic regressive A* from `goal.Target` back to the current `WorldState`, using an admissible heuristic over usable actions to guarantee the cheapest plan.
- Ties break in a fixed order (lowest `f`, lowest `h`, fewest actions, lowest discovery index in action declaration order), producing identical plans across runs.
- `GOAPPlanner.NodeBudget` (default `DefaultGOAPNodeBudget`, 1024) caps how many search nodes are expanded per plan; exceeding it fails cleanly with `ErrBudgetExceeded` instead of searching unboundedly. Unreachable goals return `ErrNoPlan`.
- Internal node, heap, and open-addressing hash-table buffers are reused across calls: use `PlanInto(dst, start, goal)` or `PlanBestGoalInto(dst, start, goals)` for zero heap allocations after warm-up.

### Plan runner and behavior tree integration

`NewPlanRunner(planner, worldFn, goals, executors)` executes planned actions in order by ticking each action's bound `Node` (`executors[action.Name]`):

- When an action returns `StatusFailure`, `PlanRunner` resets it, temporarily excludes the failed action, and replans to reach the goal by another route (returning `StatusFailure` if no route exists).
- When `worldFn()` invalidates a remaining precondition or procedural check, or when a higher-priority goal becomes unsatisfied and achievable, `PlanRunner` resets the running action and replans automatically.
- `*PlanRunner` implements `ai.Node` directly, so it can run standalone on `game.Loop` or sit inside any behavior tree:

```go
runner := ai.NewPlanRunner(planner, camp.WorldState, goals, map[string]ai.Node{
	"fetch_axe":     fetchAxeNode,
	"chop_wood":     chopWoodNode,
	"gather_sticks": gatherSticksNode,
	"build_fire":    buildFireNode,
})

tree := ai.Selector(
	ai.Guard(camp.InDanger, fleeNode),
	runner,
)
```

### GOAP YAML configuration

Author facts, actions, node budgets, and prioritized goals in a versioned YAML file (`version: 1`) decoded with `ParseGOAP`, encoded deterministically with `EncodeGOAP`, and compiled with `BuildGOAP`:

```go
cfg, err := ai.ParseGOAP(yamlBytes)
if err != nil {
	return err
}
set, err := ai.BuildGOAP(cfg, ai.GOAPBindings{
	Checks: map[string]func(ai.WorldState) bool{
		"axe_available": func(ai.WorldState) bool { return camp.AxeAvailable() },
	},
})
```

Unknown fields, unsupported versions, undefined fact references, invalid operators, and trailing YAML documents are refused; canonical documents round-trip byte-for-byte unchanged (see [File Formats](formats.md#goap-action-set)).

## Attack tokens, engagement slots, and squads

Coordinate groups of enemies in action games, brawlers, and shooters so only a bounded number attack at once, surrounding enemies spread around moving targets without crossing paths, and squads share state, tactical roles, and orders.

### Attack token pools

`TokenPool` arbitrates a fixed number of attack tokens for one target, and `TokenManager` manages one `TokenPool` per target ID. Agents submit or refresh requests with `Request(agentID, priority)` during a step, and `Step(dt)` advances cooldowns and hold timeouts while resolving pending requests deterministically (highest priority first, breaking ties by lowest `AgentID` regardless of submission order):

```go
tokens := ai.NewTokenPool(ai.TokenPoolConfig{
	Capacity:        2,                      // at most 2 attackers at once
	Cooldown:        400 * time.Millisecond, // pause before a returned token is reused
	PreemptMargin:   1.5,                    // advantage needed to take a token from another attacker
	MaxHoldDuration: 3 * time.Second,        // auto-release if an attacker stalls
})

// Each enemy's behavior tree requests a token and falls back to circling while waiting:
enemyTree := ai.Selector(
	ai.WithAttackToken(tokens, enemyID, enemyPriority, attackSequence),
	circleTargetNode,
)

// On fixed step: tick behavior trees, then resolve token grants/preemptions:
enemyTree.Tick(dt)
tokens.Step(dt)

// When an enemy dies or disengages:
tokens.Remove(enemyID)
```

- **Priority preemption**: when no free tokens are available, a waiting requester whose priority beats the weakest holder by at least `PreemptMargin` (or strictly exceeds it when `PreemptMargin <= 0`) takes that holder's token directly without entering cooldown. `WithAttackToken` resets an interrupted child and returns `StatusFailure` on the next tick so a parent `Selector` falls back cleanly.
- **Per-target pools**: use `NewTokenManager(defaultCfg)` when enemies engage multiple targets; call `ConfigureTarget(targetID, cfg)` for custom limits, `RemoveAgent(agentID)` when an enemy dies across all targets, and `Step(dt)` to step every target deterministically in ascending target ID order.

### Engagement slots

`SlotAssigner` places agents onto concentric `EngagementRing`s around a target using optimal minimum-cost bipartite matching (the Hungarian algorithm):

```go
assigner := ai.NewSlotAssigner(
	ai.EngagementRing{Radius: 45, Slots: 4}, // inner melee ring fills first
	ai.EngagementRing{Radius: 90, Slots: 6}, // outer circling ring
)
assigner.StayBias = 10 // world-unit hysteresis suppressing boundary oscillation

var slots []ai.SlotAssignment
// Inside game.Loop.Update as targetPos moves:
slots = assigner.Assign(slots, targetPos, []ai.SlotAgent{
	{ID: 1, Pos: enemy1.Pos},
	{ID: 2, Pos: enemy2.Pos},
})
```

- **Non-crossing & ring priority**: inner rings fill completely before outer rings are used, and minimizing total Euclidean distance prevents straight-line agent-to-slot paths from crossing. When there are more agents than slots, the optimal subset is assigned and extra agents receive `Assigned: false` (`Ring: -1`, `Slot: -1`).
- **Stability & determinism**: setting `StayBias > 0` gives each agent a cost bonus for keeping its previous slot as the target moves. Input agents are sorted by ascending `ID` internally, so assignments are identical across runs regardless of slice order and reuse internal scratch buffers with zero allocations after warm-up.

### Squads

`Squad` groups enemy IDs in deterministic ascending order with a shared key-value `SquadState`, score-driven tactical roles (`AssignRoles`), and squad-level `SquadOrder`s readable by behavior trees:

```go
squad := ai.NewSquad(1, 10, 20, 30)
squad.State().SetBool("player_spotted", true)
squad.State().SetPoint("last_known_pos", playerPos)

squad.AssignRoles(
	ai.SquadRoleSpec{Name: "flanker", Count: 1, Score: flankScore},
	ai.SquadRoleSpec{Name: "suppressor", Count: 2, Score: suppressScore},
)
squad.SetOrder(ai.SquadOrder{Kind: "pinch", TargetID: playerID, Position: playerPos})

// Inside a squad member's behavior tree:
flankBranch := ai.Sequence(
	ai.SquadStateBool(squad, "player_spotted"),
	ai.SquadHasOrder(squad, "pinch"),
	ai.SquadHasRole(squad, memberID, "flanker"),
	ai.SquadOrderAction(squad, moveToFlank),
)
```

- **`AssignRoles`** solves maximum-weight bipartite matching over `SquadRoleSpec`s, maximizing the number of filled eligible roles first and total score second. Return `NaN` or `±Inf` from `Score` to mark a member ineligible for a role.
- **`SquadOrder.Revision`** increments on every `SetOrder` and `ClearOrder` call so behavior trees can detect newly issued directives even when `Kind` is unchanged.
- **`SquadState`** provides typed helpers (`Bool`, `Int`, `Float`, `String`, `Point`, `ID`), generic `SquadStateGet[T]`, and deterministic sorted `Keys`.

## Influence maps

`InfluenceLayer` stores a 2D scalar influence grid matching a `pathfind.Grid` (and using the same `Origin` / `CellSize` world mapping as `rts.FlowField`) so strategy and tactics AI can reason about threat, control, tension, and safe positioning:

```go
friendly := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
enemy := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
control := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
tension := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)

squadID := friendly.AddSource(ai.InfluenceSource{
	Cell:     pathfind.Cell{Col: 2, Row: 3},
	Strength: 10,
	Radius:   5,
	Falloff:  ai.FalloffLinear,
})
enemy.AddSource(ai.InfluenceSource{
	Cell:        pathfind.Cell{Col: 12, Row: 3},
	Strength:    12,
	Radius:      6,
	Falloff:     ai.FalloffExponential,
	FalloffRate: 0.45,
})

// Incremental update (0 allocs/step after warm-up; matches Rebuild bit-for-bit):
friendly.MoveSource(squadID, pathfind.Cell{Col: 3, Row: 3})
friendly.Update(grid)
enemy.Spread(grid, ai.SpreadOptions{Decay: 0.2, Momentum: 0.5, Iterations: 2, Diagonal: true})

// Combine into tactical layers:
control.Subtract(friendly, enemy) // positive = friendly control, negative = enemy control
tension.Add(friendly, enemy)      // total presence from both sides
control.Normalize()               // signed [-1, 1] preserving 0 as neutral

// Find the least threatened reachable cell within 4 cells of a unit:
safeCell, threatVal, ok := enemy.LowestInRadius(pathfind.Cell{Col: 5, Row: 3}, 4, ai.InfluenceQuery{
	Grid:      grid,
	Reachable: true,
})
// Sample bilinearly or follow the downhill/uphill gradient in world coordinates:
val, inBounds := enemy.Sample(unitPos)
grad := enemy.Gradient(unitPos)
_, _, _, _, _ = safeCell, threatVal, ok, val, inBounds
_ = grad
```

### Sources, falloffs, and obstacle-aware spreading

- **Falloffs**: each `InfluenceSource` stamps cells within Euclidean cell `Radius` using `FalloffLinear` (`max(0, 1 - dist/Radius)`), `FalloffExponential` (`exp(-FalloffRate * dist)`), or `FalloffCustom` (`CustomFalloff(dist, radius)`). Blocked cells (`!grid.Walkable(col, row)`) always stay `0`.
- **Incremental updates**: register moving units with `AddSource` and update them via `MoveSource`, `SetSource`, `RemoveSource`, `MarkDirtyCell`, or `MarkDirtyRect`. `Update(grid)` recomputes only dirty footprints in fixed source-slot order so results are deterministic, match `Rebuild(grid)` bit-for-bit, and perform zero heap allocations per step after warm-up. For one-off static stamps, call `Stamp(grid, src)`.
- **Propagation with decay and momentum**: `Spread(grid, opts)` propagates influence around blocked cells (4-way or 8-way with no corner-cutting around walls, using `(1 - Decay)` on cardinal steps and `(1 - Decay)^sqrt(2)` on diagonals) and blends against previous values with `Momentum` (`next = prev*Momentum + target*(1 - Momentum)`). When tracked sources are present, `Spread` syncs dirty source changes incrementally before propagating so moving or removed sources decay smoothly.

### Layer combinations and spatial queries

- **Combining layers**: `Add`, `Subtract`, `Multiply`, `Combine(terms...)`, `AddScaled`, `Scale`, `Abs`, `Clamp`, `Normalize`, and `CopyFrom` operate in place without allocating on pre-sized destination layers and support self-aliasing.
- **Queries**: `HighestInRadius` and `LowestInRadius` find extrema within a cell radius, optionally filtering by walkability and BFS reachability (`InfluenceQuery{Grid: grid, Reachable: true, Diagonal: ...}`) with deterministic tie-breaking (closer cell, then smaller row, then smaller column). `Highest(grid)` and `Lowest(grid)` scan the full layer; `Sample(worldPos)` returns bilinearly interpolated values; `Gradient(worldPos)` and `GradientAtCell(col, row)` return uphill `math2d.Vec2` gradients in influence per world unit.

### Debug heatmap

Draw any layer over a `tilemap.Camera` for debugging:

```go
d.Save()
cam.Apply(d)
control.DrawHeatmap(d, cam, ai.HeatmapOptions{DrawGrid: true})
d.Restore()
```

`DrawHeatmap` culls cells outside `cam.VisibleBounds()`, tints positive values with `PositiveColor` (default green) and negative values with `NegativeColor` (default red), and scales opacity by `|value| / MaxAbs`. Under `-tags release`, `DrawHeatmap` compiles to a no-op stub and `InfluenceHeatmapEnabled()` returns `false`.

## Tactical position queries and cover points

Cover shooters, stealth games, and tactics games use `CoverPoint`, `TacticalQuery`, `PointReservations`, and `QueryBudget` to answer "where should I stand" (behind cover from a threat, at a flanking angle with clear line of fire, or in a low-threat reachable cell) with deterministic ordering and zero heap allocations after warm-up:

```go
// Generate cover points from a tilemap's solid edges (or physics 2D/3D colliders, or scene YAML):
covers := ai.GenerateCoverFromTilemap(levelMap, ai.TileCoverOptions{
	DefaultHeight: ai.CoverFull,
})

reservations := ai.NewPointReservations(ai.ReservationConfig{
	DefaultTimeout: 3 * time.Second,
})
budget := ai.NewQueryBudget(16, 8) // at most 16 LOS raycasts and 8 path queries per step

query := ai.NewTacticalQuery(
	[]ai.CandidateGenerator{
		&ai.CoverPointGenerator{Points: covers, MaxRadius: 240},
	},
	&ai.AngleTest{
		Mode:   ai.AngleCoverToTarget,
		Filter: ai.AtMost(math.Pi / 3), // cover faces within 60 degrees of the threat
		Weight: 2.0,
		Invert: true,
	},
	&ai.CoverHeightTest{Prefer: ai.CoverFull, Weight: 1.0},
	&ai.InfluenceTest{Layer: threatLayer, Weight: 1.5, Invert: true},
	&ai.LOSTest{LOS: hasLineOfSight, WantClear: false, Weight: 2.0}, // hidden from threat
	&ai.PathDistanceTest{Grid: navGrid, CellSize: 16, Weight: 2.0, Invert: true},
)

// Inside game.Loop.Update:
budget.Reset()
reservations.Step(dt)

spot, ok := query.RunAndReserve(ai.TacticalContext{
	QuerierID:    agentID,
	QuerierPos:   agentPos,
	TargetPos:    threatPos,
	Reservations: reservations,
	Budget:       budget,
}, 0)
_, _ = spot, ok

// When an agent dies or leaves cover:
reservations.Release(agentID)
```

### Cover points and generators

Each `CoverPoint` records a world standing position (`Pos`), a unit `Facing` vector pointing from `Pos` into the cover obstacle (the incoming threat direction the cover blocks; the obstacle's outward normal is `Facing.Neg()`), a `CoverHeight` (`CoverHalf` or `CoverFull`), optional 3D `Elevation`, `ID`, and `Tags`.

- **Tilemap and grid generation**: `GenerateCoverFromTilemap(m, opts)` merges contiguous exposed solid tile edges (reading optional `HalfCoverMask`, `HeightFunc`, or a tile's `"cover": "half"|"full"` property) and verifies generated positions lie in open tiles. `GenerateCoverFromGrid(g, origin, cellW, cellH, opts)` generates cover directly from any `pathfind.Grid`.
- **2D and 3D collider generation**: `GenerateCoverFromBoxes`, `GenerateCoverFromWorld` (supporting `physics.Box`, `physics.Circle` pillars, and `World.Tiles`), and `GenerateCoverFromSegments` generate 2D cover points while pruning candidates embedded inside neighboring colliders. For 3D scenes, `physics3d.World.GenerateCoverPoints(opts)` projects vertical side faces of axis-aligned boxes, oriented boxes, spheres, capsules, and cylinders standing on an end onto the XZ plane (convex hulls and cylinders lying on their side generate no cover), classifying `CoverHalf` vs `CoverFull` from collider height and preserving floor `Elevation`.
- **Scene YAML authoring**: register the built-in `"cover_point"` component on a `sceneformat.ComponentRegistry` via `sceneformat.RegisterCoverPoint(reg)` (`height`, `facingX`, `facingY`, `tags`), then extract world-transformed `[]ai.CoverPoint` values with `sceneformat.ExtractCoverPoints(doc)` or `sceneformat.ExtractBuiltCoverPoints(built)`.

### Candidate generators and ordered tests

`TacticalQuery` runs one or more `CandidateGenerator`s, excludes points reserved by other agents, evaluates `TacticalTest`s in declaration order, and returns surviving `TacticalCandidate`s sorted deterministically by descending `Score` (breaking ties by `CoverID`, `Pos.Y`, `Pos.X`, `Facing`, and generation index):

- **Generators**: `CoverPointGenerator` (with optional `MaxRadius`, `MinHeight`, `RequiredTag`), `RingGenerator` (concentric rings or arcs around a target or querier with optional `FaceCenter`), `GridGenerator` (world-space grid or walkable `pathfind.Grid` cells), and `NavMeshGenerator` (polygon centroids or spaced samples contained in an `*pathfind.NavMesh`).
- **Tests**: each test sets `Purpose` (`TestFilterAndScore`, `TestFilterOnly`, or `TestScoreOnly`), optional `Filter` (`AtLeast`, `AtMost`, `Between`), `Weight`, `Invert`, and optional fixed `NormMin`/`NormMax` (defaulting to dynamic normalization across surviving candidates):
  - `DistanceTest`: Euclidean distance to `OriginQuerier`, `OriginTarget`, or `OriginCustom`.
  - `AngleTest`: angle in radians (`AngleCoverToTarget`, `AngleFlank`, `AngleToDirection`, `AngleFromTargetToDir`).
  - `InfluenceTest`: bilinearly sampled value from an `*ai.InfluenceLayer`.
  - `CoverHeightTest`: filters by `MinHeight` and scores preferred `CoverHalf`/`CoverFull`.
  - `LOSTest`: line of sight to or from a target via a game-supplied `LOSFunc` (`WantClear: true` for firing lines, `false` for hidden cover).
  - `PathDistanceTest`: travel distance via a `PathCost` callback, zero-allocation grid `AStar`, or zero-allocation 2D `NavMesh` funnel path length.
  - `CustomTest`: caller-supplied `Eval(ctx, candidate)` callback.

### Reservations and per-step budgets

- **`PointReservations`**: guarantees two agents never reserve the same `CoverID` or non-cover positions closer than `MinSeparation`. `RunAndReserve` selects and reserves the best unreserved candidate while atomically replacing that agent's prior reservation; `Release(agentID)` frees a spot on death, `ReleaseCover(coverID)` frees destroyed cover, `Refresh` extends a timeout, and `Step(dt)` expires timed reservations deterministically.
- **`QueryBudget`**: caps `MaxRaycasts` (for `LOSTest`) and `MaxPathQueries` (for `PathDistanceTest`) per simulation step across all agents sharing the budget. Before running a budgeted test, surviving candidates are ordered deterministically by preliminary score so the highest-ranked spots consume the step budget first. Call `Reset()` once per step, and use `Best`, `RunInto(dst, ctx)`, or `RunAndReserve` for zero heap allocations after warm-up.

## Perception

A `Perception` gives one agent sight, hearing and a memory of what it sensed, so a guard can see the player right now, go to where it last heard a noise, and give up after a while. A `PerceptionSystem` steps the perceptions of many agents together on `game.Loop`'s fixed steps:

```go
walls := []lights2d.Occluder{lights2d.RectOccluder(wallBounds)}

guard := &ai.Perception{
	Self:   guardID, // never senses itself
	Team:   teamGuards,
	Facing: core.Point{X: 1},
	Sight: ai.SightConfig{
		Range:           160,
		HalfAngle:       math.Pi / 4, // a 90° cone
		PeripheralRange: 24,          // all around, unless PeripheralHalfAngle narrows it
	},
	Hearing:     ai.HearingConfig{Range: 120},
	ForgetAfter: 5 * time.Second,
}

senses := ai.PerceptionSystem{
	LineOfSight: lights2d.LineOfSight(&walls),
	Budget:      32, // line-of-sight checks per step, shared by every agent
}
senses.Add(guard)

// Inside game.Loop.Update:
guard.Pos = guardAgent.Pos
if guardAgent.Vel != (core.Point{}) {
	guard.Facing = guardAgent.Vel
}
if player.Running {
	senses.PostNoise(ai.Noise{Source: playerID, Team: teamPlayer, Pos: player.Pos, Loudness: 1})
}
senses.Step(dt, []ai.Target{{ID: playerID, Team: teamPlayer, Pos: player.Pos, Vel: player.Vel}})

for _, ev := range guard.Events() {
	switch ev.Kind {
	case ai.EventGained, ai.EventUpdated:
		chase(ev.Memory.Pos)
	case ai.EventLost:
		search(ev.Memory.Pos, ev.Memory.Vel)
	case ai.EventForgotten:
		resumePatrol()
	}
}
```

### Sight

A target is in view when it lies within `Range` of the eye and within `HalfAngle` (radians) of `Facing`, or within the shorter `PeripheralRange` and within `PeripheralHalfAngle`, which is zero for all around; a negative `PeripheralHalfAngle` disables the peripheral range. The eye is `Pos + EyeOffset`, in world space; hearing measures from the same point. A target in view is seen when the system's `LineOfSight` reports the segment from the eye to the target clear; a nil `LineOfSight` lets every target in view be seen. Three adapters cover the usual occluders:

| Adapter | Occluders |
| --- | --- |
| `ai.PhysicsLineOfSight(world, query, layers)` | Bodies and tiles of a `physics.World`, through `RaycastFirstLayer`. |
| `lights2d.LineOfSight(&occluders)` | The `lights2d` occluders that cast shadows, through `lights2d.Visible`; it reads the slice on every call, so the level can change it. |
| `perception3d.PhysicsLineOfSight(world, layers)` | Bodies and static colliders of a `physics3d.World`, for 3D agents. |

A ray that starts inside the agent's body or ends inside the target's hits that body, so keep agents and targets off the layers that block sight, for example by putting walls on a layer of their own.

### Hearing

`PostNoise` queues a `Noise` with a position, a `Loudness` and its source's ID and team, and the next `Step` hears it: an agent hears a noise within `Hearing.Range × Loudness` of its eye, with no line of sight needed. Noises are heard in the order they were posted, and a step consumes them.

### Memory and events

Each agent keeps one `Memory` per `StimulusID`, so a target seen and later heard refreshes one memory. A memory holds the last known `Pos` and `Vel` (zero when heard), the `Sense` that last refreshed it, its `Age` since it was last sensed, whether it is `Visible` now, and a `Strength` that is 1 when seen, is raised to at least `1 - distance/reach` when heard (1 for an infinite reach), and falls linearly to 0 over `ForgetAfter`. A memory is forgotten once `Age` reaches `ForgetAfter`. Read memories with `Memories`, `AppendMemories`, `Memory(id)` and `Sees(id)`, and drop them with `Forget(id)` and `Clear`. `Remove` takes an agent out of the system but keeps its memories, with none of them visible and no events, so `Sees` reports false until the agent is added and stepped again. A copy of a `Perception` that was added, such as a configured prototype, is a separate agent: `Add` gives it its own copy of the memories.

After each `Step`, `Events` lists what changed:

| Kind | When |
| --- | --- |
| `EventGained` | A stimulus is sensed with no memory of it before. |
| `EventUpdated` | A remembered stimulus is seen again after being out of sight, is seen moving, or is heard. A still target in sight makes no events. |
| `EventLost` | A stimulus in sight leaves it; the memory stays. |
| `EventForgotten` | A memory reaches `ForgetAfter` and is dropped. |

### Teams

`Team` is a small team number. The system's `Attitude` function, `DefaultAttitude` when nil, decides whether an agent regards a stimulus as `Hostile`, `Neutral` or `Friendly`: the same team is friendly, `NoTeam` on either side is neutral, and other teams are hostile. `SightConfig.Detect` and `HearingConfig.Detect` choose the attitudes each sense notices, with `DetectHostile`, `DetectNeutral`, `DetectFriendly` and `DetectAll`; zero detects hostile stimuli only.

### Budget and determinism

With hundreds of agents, casting a ray for every agent and target on every step is too much. `Budget` caps the line-of-sight checks of a step: the pairs in view are ordered by the order agents were added and then by target ID, a step checks `Budget` of them starting after the last pair checked, and the next step carries on from there, so every agent gets its turn in a fixed order. A pair not yet rechecked keeps its previous result. `Stats` reports a step's candidates, checks and deferred pairs. A nil `LineOfSight` casts no rays, so the budget does not apply and every pair in view is seen at once. With a deterministic `LineOfSight`, every run with the same inputs produces the same memories and events.

### 3D

The experimental package `game/ai/perception3d` runs the same engine on `math3d.Vec3`, so its senses, memory, events, teams and budget behave exactly as above. Its `Perception` points the cone along `Forward` (zero faces -Z), with angles measured in 3D, and `EyeOffset` is typically head height on +Y; `System`, `Target`, `Noise`, `Memory`, `Event` and `LineOfSight` mirror the 2D types, and it reuses `ai.SightConfig`, `ai.HearingConfig`, `ai.StimulusID`, `ai.Team` and the event kinds:

```go
import "github.com/nsgo-dev/game/pkg/ai/perception3d"

guard := &perception3d.Perception{
	Team:        teamGuards,
	Forward:     math3d.Vec3{Z: -1},
	EyeOffset:   math3d.Vec3{Y: 1.7},
	Sight:       ai.SightConfig{Range: 40, HalfAngle: math.Pi / 4},
	ForgetAfter: 5 * time.Second,
}
senses := perception3d.System{LineOfSight: perception3d.PhysicsLineOfSight(world, layerWalls), Budget: 64}
senses.Add(guard)

// Inside game.Loop.Update:
senses.Step(dt, []perception3d.Target{{ID: playerID, Team: teamPlayer, Pos: playerPos}})
```
