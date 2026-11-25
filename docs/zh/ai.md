# ai

`game/ai` 提供可直接运行在 `game.Loop` 固定步长上的确定性游戏 AI 组件：网格 `AStar` 寻路、带漏斗平滑的 2D 导航网格 `NavMesh`、转向行为（steering）、行为树以及效用 AI（Utility AI）评分。

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

内置转向函数：

| 函数 | 行为 |
| --- | --- |
| `Seek(a, target)` | 以 `MaxSpeed` 全速朝 `target` 加速。 |
| `Arrive(a, target, slowRadius)` | 进入 `slowRadius` 后平滑减速直至停稳。 |
| `Flee(a, threat, panicRadius)` | 距离小于 `panicRadius`（或 `<= 0` 时始终）远离 `threat`。 |
| `FollowPath(a, path, &state, radius, loop)` | 沿航点前进；`loop` 为 `false` 时在终点自动使用 `Arrive`。 |
| `Separation(a, neighbors, desiredSep)` | 推开过近的邻居（`<= 0` 时默认取 `a.Radius + neighbor.Radius`）。 |
| `Wander(a, &state, radius, dist, jitter, nextRand)` | 使用你传入的确定性 `nextRand() float32`（`[0, 1)`）平滑漫游。 |
| `Combine(maxForce, forces...)` | 加权求和并限制合力模长不超过 `maxForce`。 |

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
