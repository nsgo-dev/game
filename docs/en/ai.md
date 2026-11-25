# ai

`game/ai` provides deterministic game AI building blocks stepped cleanly on `game.Loop`: grid `AStar` pathfinding, 2D `NavMesh` navigation with funnel smoothing, steering behaviors, behavior trees, utility AI scoring, goal-oriented action planning (GOAP), attack token pools, concentric engagement slot assignment, and squad coordination.

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

- **Composites**: `Sequence`, `Selector` (preempts and resets lower-priority running branches when a higher-priority branch succeeds or runs), `Parallel(policy, ...)` (`RequireAll` or `RequireOne`), `UtilitySelector(reasoner, ctx, rng, subtrees)`.
- **Decorators**: `Guard(cond, child)`, `Cooldown(d, child)`, `WithAttackToken(pool, agentID, priority, child)`, `Inverter`, `Succeeder`, `Failer`, `Repeater(count, child)`, `UntilFail`.
- **Leaves**: `Action(fn)`, `Condition(fn)`, `Wait(d)`, `HasAttackToken(pool, agentID)`, `SquadHasOrder(squad, kind)`, `SquadHasRole(squad, agentID, role)`, `SquadStateBool(squad, key)`, `SquadOrderAction(squad, fn)`.

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
