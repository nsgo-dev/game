# ai

`game/ai` 提供可直接运行在 `game.Loop` 固定步长上的确定性游戏 AI 组件：网格 `AStar` 寻路、带漏斗平滑的 2D 导航网格 `NavMesh`、转向行为（steering）、行为树、效用 AI（Utility AI）评分以及目标导向行动规划（GOAP）。

```go
import "github.com/nsgo-dev/game/packages/game/ai"
```

`ai` 不启动后台 goroutine，也不含隐藏的全局随机源。

## 网格 A* 寻路

`AStar` 可在任意 `Grid` 或带权重 `CostGrid`（`BoolGrid`、`CostMap` 或适配器 `GridFunc`）上搜索最低代价路径，并在多次调用间复用内部缓冲区：

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
astar.Diagonal = true // NoCornerCutting 默认为 true，防止斜向穿角

path, cost, ok := astar.Find(grid, ai.Cell{Col: 1, Row: 1}, ai.Cell{Col: 18, Row: 10})
_, _ = path, cost
_ = ok
```

使用 `astar.FindInto(dst, grid, start, goal)` 可把结果写入调用方提供的 `[]ai.Cell` 切片，实现每次寻路零堆分配。若项目尚未接入瓦片地图，可直接使用扁平切片网格 `NewBoolGrid` 或 `NewCostMap`。

### 六边形与交错等距拓扑

通过 `ai.TopologyFromTilemap(m)` 自动匹配任意 `tilemap.Tilemap` 的网格拓扑，或手动指定 `astar.Topology`（`TopologySquare`、`TopologySquare4`、`TopologySquare8`、`TopologyHexPointyOdd`、`TopologyHexPointyEven`、`TopologyHexFlatOdd`、`TopologyHexFlatEven`、`TopologyStaggeredYOdd`、`TopologyStaggeredYEven`、`TopologyStaggeredXOdd`、`TopologyStaggeredXEven`）：

```go
astar := ai.NewAStar()
astar.Topology = ai.TopologyFromTilemap(hexMap)

path, cost, ok := astar.Find(grid, ai.Cell{Col: 1, Row: 1}, ai.Cell{Col: 6, Row: 4})
_, _, _ = path, cost, ok
```

- 六边形拓扑使用 6 邻接轴坐标与精确立方距离启发式，与 `tilemap.Tilemap.HexNeighbors` 和 `HexDistance` 完全一致。
- 交错等距拓扑将偏移坐标映射回底层菱形晶格（默认 4 向沿边移动；将 `Diagonal` 设为 `true` 时开放受 `NoCornerCutting` 约束的 8 向顶点对角移动）。
- 当移动代价同时取决于起点与终点（例如高度差、渡河或控制区惩罚）时，可实现 `EdgeCostGrid`（`EdgeCost(fromCol, fromRow, toCol, toRow) float64`）；返回负值、`NaN` 或无穷大表示禁止该边通行。
- 可通过 `topo.Neighbors(c)`、`topo.NeighborsInto(dst, c)` 和 `topo.Distance(a, b)` 直接查询邻格与开阔地步距。

### 移动范围查询

战棋与战术游戏可用 Dijkstra 搜索计算从 `start` 在行动点预算内可到达的所有格子及其最小代价：

```go
reachable := astar.MovementRange(costMap, ai.Cell{Col: 2, Row: 2}, 4)
for _, rc := range reachable {
	// rc.Cell 可达，且最小移动代价 rc.Cost <= 4
	_ = rc
}
```

- 返回结果顺序完全确定：按 `Cost` 升序排列，同代价按行主序索引打破平局。
- 使用 `astar.MovementRangeInto(dst, grid, start, budget)` 可复用切片实现零堆分配；使用 `astar.MovementRangeWithCost(dst, grid, start, budget, edgeCost)` 或便捷函数 `ai.ReachableCells(grid, topo, start, budget, edgeCost)` 可传入自定义 `func(from, to ai.Cell) float64` 边代价回调（支持小于 1 的道路减耗及负值禁行边）。

## 带漏斗平滑的 2D NavMesh

对于开放式竞技场或非网格地形，可用凸多边形构建 `NavMesh`。`BuildNavMesh` 会自动统一绕序、沿共享边（含部分共线重叠）连接相邻多边形；`FindPath` 通过门户图 A* 加上 Simple Stupid Funnel Algorithm 拉直路径：

```go
mesh := ai.BuildNavMesh([][]core.Point{
	{{X: 0, Y: 0}, {X: 120, Y: 0}, {X: 120, Y: 80}, {X: 0, Y: 80}},
	{{X: 120, Y: 20}, {X: 240, Y: 20}, {X: 240, Y: 100}, {X: 120, Y: 100}},
})

waypoints, ok := mesh.FindPath(core.Point{X: 16, Y: 16}, core.Point{X: 220, Y: 85})
_ = waypoints
_ = ok
```

可通过 `Contains(p)`、`FindPolygon(p)`、`NearestPolygon(p)`、`Neighbors(index)`、`Polygon(index)` 和 `PolygonCount()` 查询网格几何信息。

## 转向行为（Steering）

`Agent` 封装位置、速度、最大速度、最大转向力与半径，在固定步长中通过 `Agent.Step(force, dt)` 积分移动：

```go
agent := ai.Agent{
	Pos:      core.Point{X: 20, Y: 20},
	MaxSpeed: 120,
	MaxForce: 300,
	Radius:   10,
}
var followState ai.PathFollowState

// 在 game.Loop.Update 中：
force := ai.Combine(agent.MaxForce,
	ai.WeightedForce{Force: ai.FollowPath(agent, waypoints, &followState, 8, false), Weight: 1.0},
	ai.WeightedForce{Force: ai.Separation(agent, nearbyAgents, 24), Weight: 1.5},
)
agent.Step(force, dt)
```

内置 2D 转向函数：

| 函数 | 行为 |
| --- | --- |
| `Seek(a, target)` | 以 `MaxSpeed` 全速朝 `target` 加速。 |
| `Arrive(a, target, slowRadius)` | 进入 `slowRadius` 后平滑减速直至停稳。 |
| `Flee(a, threat, panicRadius)` | 距离小于 `panicRadius`（或 `<= 0` 时始终）远离 `threat`。 |
| `Pursue(a, target, maxLookAhead)` | 按 `target.Vel` 预测目标未来位置进行拦截；`maxLookAhead > 0` 时限制最大提前量（秒）。 |
| `Evade(a, pursuer, panicRadius, maxLookAhead)` | 距离小于 `panicRadius`（或 `<= 0` 时始终）规避 `pursuer` 的预测位置。 |
| `OffsetPursuit(a, leader, localOffset, slowRadius, maxLookAhead)` | 以 `leader` 朝向为局部坐标系（`X` 向前、`Y` 向左）跟随指定偏移位，并用 `Arrive` 平滑就位。 |
| `FollowPath(a, path, &state, radius, loop)` | 沿航点前进；`loop` 为 `false` 时在终点自动使用 `Arrive`。 |
| `Separation(a, neighbors, desiredSep)` | 推开过近的邻居（`<= 0` 时默认取 `a.Radius + neighbor.Radius`）。 |
| `Alignment(a, neighbors, radius)` | 对齐 `radius` 范围内邻居的平均朝向。 |
| `Cohesion(a, neighbors, radius)` | 朝 `radius` 范围内邻居的质心聚拢。 |
| `ObstacleAvoidance(a, obstacles, maxAhead)` | 绕开进入前方探测走廊的最近圆形 `Obstacle`。 |
| `WallAvoidance(a, walls, feelerLength)` | 通过三条前向触须与半径间隙约束避开 `Wall` 墙体线段。 |
| `Hide(a, pursuer, obstacles, standoff, slowRadius)` | 在圆形 `Obstacle` 背离 `pursuer` 的一侧选取最近藏匿点（无可用障碍物时回退到 `Evade`）。 |
| `Interpose(a, targetA, targetB, slowRadius)` | 预测两个移动智能体的中点位置并切入拦截。 |
| `Wander(a, &state, radius, dist, jitter, nextRand)` | 使用你传入的确定性 `nextRand() float32`（`[0, 1)`）平滑漫游。 |
| `Combine(maxForce, forces...)` | 加权求和并限制合力模长不超过 `maxForce`。 |
| `CombinePrioritized(maxForce, forces...)` | 按优先级顺序分配 `maxForce` 预算（例如优先满足 `WallAvoidance` 再执行 `FollowPath`），预算用尽后忽略低优先级转向力。 |

### 使用 `NeighborGrid` 实现零分配集群（Flocking）

大规模集群场景中，每步通过 `NeighborGrid` 重建一次空间索引，`Rebuild`、`Query` 与 `QueryInto` 均复用内部缓冲区，预热后每步零堆分配：

```go
grid := ai.NewNeighborGrid(48)

// 在 game.Loop.Update 中：
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

### 3D 与 XZ 平面转向

`Agent3D`、`Vec3`、`Obstacle3D`、`Wall3D`、`WeightedForce3D` 和 `NeighborGrid3D` 提供与 2D 对称的 3D 转向行为（`Seek3D`、`Arrive3D`、`Flee3D`、`Pursue3D`、`Evade3D`、`OffsetPursuit3D`、`FollowPath3D`、`Separation3D`、`Alignment3D`、`Cohesion3D`、`ObstacleAvoidance3D`、`WallAvoidance3D`、`Hide3D`、`Interpose3D`、`Combine3D`、`CombinePrioritized3D`）。

`ai.Vec3` 与 `math3d.Vec3` 具有完全相同的 `X, Y, Z float32` 布局（右手 Y-up：`+X` 向右、`+Y` 向上、`-Z` 向前），可通过 `ai.Vec3(v)` 零成本转换，同时保持稳定的 `game/ai` 不依赖实验性包。将 `Agent3D.PlanarXZ` 设为 `true` 时，地面智能体在水平 XZ 平面上跟踪 `world3d.NavMesh` 路径：

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

## 行为树

用实现 `Node` 接口的节点组合敌人 AI 决策，每次 `Tick(dt)` 返回 `StatusSuccess`、`StatusFailure` 或 `StatusRunning`：

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

// 在 game.Loop.Update 中：
tree.Tick(dt)
```

可用节点类型：

- **组合节点**：`Sequence`、`Selector`（当高优先级分支成功或运行时会自动重置被抢占的低优先级运行中分支）、`Parallel(policy, ...)`（`RequireAll` 或 `RequireOne`）、`UtilitySelector(reasoner, ctx, rng, subtrees)`。
- **修饰节点**：`Guard(cond, child)`、`Cooldown(d, child)`、`Inverter`、`Succeeder`、`Failer`、`Repeater(count, child)`、`UntilFail`。
- **叶节点**：`Action(fn)`、`Condition(fn)`、`Wait(d)`。

## 效用 AI

当角色需要根据连续态势（距离、生命值、弹药、威胁度等）而非固定布尔优先级做决策时，可使用基于 Dave Mark 无限轴效用模型（IAUS）的 `Reasoner[C]` 对候选 `UtilityAction` 评分：

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
reasoner.Momentum = 0.1 // 当前动作的动量加成

idx, score := reasoner.Choose(state, nil)
_, _ = reasoner.ActionName(idx), score
```

### 考量项与响应曲线

每个 `Consideration[C]` 从上下文 `C` 读取输入值，在 `Max > Min` 时将 `[Min, Max]` 线性归一化到 `[0, 1]`（否则直接截断到 `[0, 1]`），再经 `ResponseCurve` 映射为 `[0, 1]` 区间得分：

| 曲线类型 / 构造函数 | 公式 |
| --- | --- |
| `CurveLinear` / `LinearCurve(slope, shiftX, shiftY)` | `slope*(x - shiftX) + shiftY` |
| `CurvePolynomial` / `PolynomialCurve(slope, exponent, shiftX, shiftY)` | `slope*(x - shiftX)^exponent + shiftY` |
| `CurveLogistic` / `LogisticCurve(slope, steepness, midpoint, shiftY)` | `slope / (1 + exp(-steepness*(x - midpoint))) + shiftY` |
| `CurveLogit` / `LogitCurve(slope, midpoint, shiftY)` | `slope*ln(u/(1 - u)) + shiftY`，其中 `u = x - midpoint + 0.5` |
| `CurveStep` / `StepCurve(threshold, low, high)` | `x < threshold` 返回 `low`，否则返回 `high` |
| `CurveResource` / `ResourceCurve(eval)` | 委托给 `CurveEvaluator`（`Eval(float32) float32`），`*resource.Curve` 可直接满足 |
| `CustomCurve(fn)` | 将 `func(float64) float64` 包装为 `CurveEvaluator` |

### 评分、动量加成与选择策略

- **补偿因子**：一个动作的 `N` 个考量项相乘（任意一项为 `0` 即立即否决动作），再按 Dave Mark 补偿公式 `modFactor = 1 - 1/N`、`score = score + (1 - score)*modFactor*score` 修正，最后乘以 `Weight`（`0` 默认按 `1` 处理，负值禁用动作）。
- **动量加成**：当前正在执行的动作在基础得分大于 `0` 时额外加上 `Reasoner.Momentum + UtilityAction.Momentum`，防止智能体在两个得分相近的动作间每帧抖动切换；被否决（得分为 `0`）的动作不会因动量而被保留。
- **Top-N 加权随机**：设置 `Reasoner.TopN > 1` 并传入确定性 `*random.Stream`，按得分权重在前 `N` 个正分动作中随机抽取；`TopN <= 1` 或随机流为 `nil` 时确定性地选取最高分动作。同分始终按动作声明顺序打破平局。
- **多智能体共享与零分配**：调用 `reasoner.ChooseWithCurrent(ctx, agent.ActiveAction, rng)` 可让大量智能体共享同一个 `Reasoner`，预热后每次决策零堆分配。

### 行为树集成

通过 `UtilitySelector(reasoner, ctx, rng, subtrees)` 将效用评分直接作为行为树节点嵌入。每个 `UtilitySelector` 独立维护当前动作用于动量计算，并在新动作抢占正在运行的子树时先对其调用 `Reset()`：

```go
node := ai.UtilitySelector(reasoner, func() UnitState { return unit.State() }, rng, map[string]ai.Node{
	"attack": attackSubtree,
	"heal":   healSubtree,
})
```

### YAML 配置格式

可在带版本号的 YAML 文件（`version: 1`）中声明动作、考量项与响应曲线，用 `ParseUtility` 解析、`EncodeUtility` 确定性写回，并通过 `BuildReasoner` 绑定输入函数和可选的 `resource` 曲线加载器（`LoadCurve`）：

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

解析器会拒绝未知字段、不支持的版本号以及多余的 YAML 文档，规范格式文件读取再写回后字节完全一致（详见 [文件格式](formats.md#效用-ai)）。

## 目标导向行动规划（GOAP）

当智能体需要朝着目标动态组合多步动作链（例如取斧头 → 砍木头 → 生火），而不必手工连线每一条状态迁移时，可使用 `FactSchema`、`GOAPAction`、带优先级的 `GOAPGoal` 以及 `GOAPPlanner`：

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

### 世界状态、条件与效果

- **`WorldState`** 是紧凑的可比较值类型，将最多 `MaxBoolFacts`（128）个命名布尔事实打包进 `[2]uint64` 位集合，并附带最多 `MaxIntFacts`（16）个小型 `int16` 整数事实。通过 `FactSchema.Bool` / `FactSchema.Int`（或 `MustBool` / `MustInt`）注册事实名。
- **`Conditions`** 用于表达动作前置条件与目标状态，支持布尔约束（`RequireBool`）与整数区间约束（`RequireIntEq`、`RequireIntGte`、`RequireIntLte`、`RequireIntGt`、`RequireIntLt`、`RequireIntRange`）。
- **`Effects`** 用于表达动作完成后的世界状态变化，支持布尔赋值（`SetBool`）与整数更新（`SetInt`、`AddInt`）。
- **`GOAPAction.Check`** 是可选的过程化前置检查（`func(WorldState) bool`），针对当前世界状态求值，用于检验符号位集合之外的运行时条件（如实体是否存在或路径是否可达）。

### 逆向 A* 搜索与节点预算

- `GOAPPlanner` 从 `goal.Target` 逆向搜索回当前 `WorldState`，基于可用动作的可采纳启发式保证返回最低代价计划。
- 同分始终按固定顺序打破平局（先比 `f`、再比 `h`、再比步数、最后按动作声明顺序的发现序号），多次运行结果完全一致。
- `GOAPPlanner.NodeBudget`（默认 `DefaultGOAPNodeBudget`，1024）限制单次规划最多展开的搜索节点数；超出预算时干净地返回 `ErrBudgetExceeded` 而非无限搜索；不可达目标返回 `ErrNoPlan`。
- 内部节点池、二叉堆和开放寻址哈希表缓冲区在多次调用间复用；调用 `PlanInto(dst, start, goal)` 或 `PlanBestGoalInto(dst, start, goals)` 在预热后每次规划零堆分配。

### 计划执行器与行为树集成

`NewPlanRunner(planner, worldFn, goals, executors)` 按顺序依次驱动计划中每个动作绑定的 `Node`（`executors[action.Name]`）：

- 当某个动作返回 `StatusFailure` 时，`PlanRunner` 会重置该动作节点、临时排除失败动作并自动重规划以寻找替代路线（若无可用路线则返回 `StatusFailure`）。
- 当 `worldFn()` 返回的世界状态使剩余计划的前置条件或过程化检查失效，或更高优先级目标变为未满足且可达时，`PlanRunner` 会重置正在运行的动作并自动重规划。
- `*PlanRunner` 直接实现 `ai.Node` 接口，既可在 `game.Loop` 中独立步进，也可直接嵌入任意行为树：

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

### GOAP YAML 配置格式

可在带版本号的 YAML 文件（`version: 1`）中声明事实表、动作集、节点预算与优先级目标，用 `ParseGOAP` 解析、`EncodeGOAP` 确定性写回，并通过 `BuildGOAP` 编译为 `GOAPSet`：

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

解析器会拒绝未知字段、不支持的版本号、未声明事实引用、无效运算符以及多余的 YAML 文档，规范格式文件读取再写回后字节完全一致（详见 [文件格式](formats.md#goap-动作集)）。
