# ai

`game/ai` 提供可直接运行在 `game.Loop` 固定步长上的确定性游戏 AI 组件：网格 `AStar` 寻路、带漏斗平滑的 2D 导航网格 `NavMesh`、转向行为（steering）、行为树、效用 AI（Utility AI）评分、目标导向行动规划（GOAP）、攻击令牌与小队协作、空间影响图（Influence Maps）、EQS 风格的战术位置查询、掩体点与占位预订系统，以及带视觉、听觉与记忆的感知。

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

### 视线路径平滑与任意角度搜索（Theta*）

网格 A* 产出的逐格路径在使用 `FollowPath` 跟踪时往往呈阶梯状。可使用 `SmoothGridPath` 或复用缓冲区的 `SmoothGridPathInto` 将格子路径转换为平滑的世界坐标航点：

```go
opts := ai.GridPathOptions{
	Origin:   core.Point{X: 0, Y: 0},
	CellSize: 16,
	Radius:   6, // 保证圆形碰撞半径与所有阻挡格保持安全距离
}

rawCells, _, ok := astar.FindInto(cellBuf, grid, startCell, goalCell)
if ok {
	waypoints = ai.SmoothGridPathInto(waypoints, grid, rawCells, opts)
}
```

- **带半径间隙的视线检查**：只要两个保留航点之间的直线段与任意阻挡格（`!grid.Walkable(col, row)`）的距离都不小于 `Radius`，`SmoothGridPath` / `SmoothGridPathInto` 就会剔除中间冗余格子。矩形瓦片可设置 `CellWidth` / `CellHeight` 覆盖 `CellSize`，也可直接调用 `ai.GridLineOfSight(grid, a, b, opts)` 和 `opts.CellCenter(cell)`。
- **零堆分配缓冲区复用**：`SmoothGridPathInto(dst, grid, cells, opts)` 将结果写入 `dst[:0]`，当 `dst` 容量足够时全程零堆分配。
- **任意角度（Theta*）搜索**：在方形网格拓扑（`TopologySquare`、`TopologySquare4`、`TopologySquare8`）上将 `astar.AnyAngle` 设为 `true`，搜索会在格心视线穿过均匀代价可行走格时直接连接祖先节点（遵守 `NoCornerCutting`，且不会抄近路穿过高代价 `CostGrid` 地形）。若需要正值实体 `Radius` 的安全间隙，建议配合常规逐格 `AStar`（`AnyAngle: false`）再调用 `SmoothGridPathInto`，以保留绕角所需的拐点格。

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

## 有界、部分路径与增量寻路

点击不可达目标时单位应尽量走近目标，同时大量并发长寻路也不应拖垮单帧预算。`AStar`、2D `NavMesh` 以及 [world3d](world3d.md) 的 3D `NavMesh` 共享 `PathOptions{MaxExpansions, AllowPartial}` 与 `IncrementalSearch` 接口：

```go
// 单次有界搜索，并启用部分路径回退：
res := astar.FindWithOptions(grid, start, goal, ai.PathOptions{
	MaxExpansions: 128,
	AllowPartial:  true,
})
if res.Found {
	// 当 res.Partial 为 true 时，res.Path 终止于已探索范围内离目标最近的可行走格子。
	// res.Exhausted 表示搜索是否因触及 MaxExpansions 而提前停止。
	follow(res.Path)
}

// 2D NavMesh 有界 / 部分路径搜索（复用同一 PathOptions）：
navRes := mesh.FindPathWithOptions(startPt, goalPt, ai.PathOptions{
	MaxExpansions: 64,
	AllowPartial:  true,
})
_ = navRes
```

- **部分路径**：开启 `AllowPartial: true` 后，当目标被墙围死、位于障碍格或网格外部、或超出 `MaxExpansions` 预算时，`FindWithOptions` / `FindWithOptionsInto`（预热后零堆分配）以及 `NavMesh.FindPathWithOptions` 会返回通往已探索范围内最近格子或最近多边形边界点的合法路径，并标记 `Partial: true`。设置 `AStar.MaxExpansions > 0` 也会限制 `Find` / `FindInto`，并在 `PathOptions.MaxExpansions == 0` 时作为默认预算（传入负值 `PathOptions.MaxExpansions` 可显式覆盖为不限步数）。
- **确定性增量步进**：通过 `astar.StartSearch(grid, start, goal, opts)`（`*AStarSearch`，支持 `Reset` 复用缓冲区）或 `mesh.StartSearch(start, goal, opts)`（`*NavMeshSearch`）启动跨帧搜索，并每帧调用 `Step(maxExpansions)` 推进。无论按任意正数步长切分（哪怕每次仅展开 1 个节点），最终得到的路径、代价、`Partial` 标记与展开次数均与单次搜索完全一致。
- **预算化寻路请求队列（`PathQueue`）**：使用 `NewPathQueue`、`Submit` / `SubmitGrid` / `SubmitNavMesh`、`Cancel` 和 `Step(budget)` / `StepDefault()`，在严格的单步节点展开预算内统一调度网格、2D `NavMesh` 与 `world3d.NavMesh` 寻路请求：

```go
queue := ai.NewPathQueue(ai.PathQueueConfig{
	DefaultBudget: 64,                     // StepDefault 每步严格不超过该展开次数
	Policy:        ai.PathQueueRoundRobin, // 或 ai.PathQueueFIFO（默认）
	SliceSize:     16,                     // 轮转模式下单次时间片上限（0 表示均分剩余预算）
})

reqID, search := queue.SubmitGrid(astar, grid, start, goal, ai.PathOptions{
	AllowPartial: true,
}, 1, func(id ai.PathRequestID, res ai.GridPathResult) {
	assignUnitPath(res.Path, res.Partial)
})

// 在 game.Loop.Update 中：
_ = queue.StepDefault() // 绝不超过 DefaultBudget 次展开
_ = search.Status()     // 可直接轮询状态，或通过 queue.Cancel(reqID) 取消请求
_ = reqID
```

高优先级请求优先执行，同优先级内按确定性 FIFO 或轮转顺序推进，取消后的请求不会继续步进也不会触发回调。

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

- **组合节点**：`Sequence`、`Selector`（当高优先级分支成功或运行时会自动重置被抢占的低优先级运行中分支）、`Parallel(policy, ...)`（`RequireAll` 或 `RequireOne`）、`RandomSelector(stream, ...)`、`RandomSequence(stream, ...)`、`WeightedSelector(stream, options...)`（配合 `WeightedNode{Node, Weight}`）、`UtilitySelector(reasoner, ctx, rng, subtrees)`。
- **修饰节点**：`Guard(cond, child)`、`Cooldown(d, child)`、`TimeLimit(d, child)`、`Retry(n, child)`、`Service(interval, fn, child)`、`WithAttackToken(pool, agentID, priority, child)`、`Inverter`、`Succeeder`、`Failer`、`Repeater(count, child)`、`UntilFail`。
- **叶节点**：`Action(fn)`、`Condition(fn)`、`Wait(d)`、`HasAttackToken(pool, agentID)`、`SquadHasOrder(squad, kind)`、`SquadHasRole(squad, agentID, role)`、`SquadStateBool(squad, key)`、`SquadOrderAction(squad, fn)`。

### 超时、重试、确定性随机选择与服务节点

- **`TimeLimit(d, child)`**：在累计游戏时间达到 `d` 且 `child` 仍处于 `StatusRunning` 时调用 `child.Reset()` 并返回 `StatusFailure`。
- **`Retry(n, child)`**：在子节点失败后最多重试 `n` 次（总计最多 `1+n` 次尝试），每次失败之间返回 `StatusRunning`，一旦成功立即返回 `StatusSuccess`。
- **`RandomSelector(stream, children...)`** 与 **`RandomSequence(stream, children...)`**：在每次运行开始时使用调用方传入的 `*random.Stream` 打乱子节点顺序，并在子节点返回 `StatusRunning` 期间保持该顺序，直至完成或调用 `Reset()`。
- **`WeightedSelector(stream, options...)`**：使用 `stream` 按 `WeightedNode.Weight` 权重无放回抽取子节点（自动跳过非正数、`NaN`、无穷大权重或 `nil` 节点），在运行中保持当前子节点，相同种子下多次运行结果完全一致。
- **`Service(interval, fn, child)`**：在进入 `child` 分支时立即调用 `fn`，之后在 `child` 持续返回 `StatusRunning` 期间每隔 `interval` 游戏时间触发一次；当分支结束或被 `Reset()` 时自动停止。

```go
rng := random.New(42)
combat := ai.Service(250*time.Millisecond, refreshTarget,
	ai.WeightedSelector(rng,
		ai.WeightedNode{Node: ai.TimeLimit(2*time.Second, chargeAttack), Weight: 2},
		ai.WeightedNode{Node: ai.Retry(2, rangedShot), Weight: 3},
	),
)
```

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

## 攻击令牌、交战槽位与小队

在动作、格斗和射击游戏中协调敌人群体：限制同时进攻的敌人数量，让包围移动目标的敌人分散站位且路径不交叉，并通过小队共享状态、战术角色与指令。

### 攻击令牌池

`TokenPool` 为单个目标仲裁固定数量的攻击令牌，`TokenManager` 按目标 ID 维护多个 `TokenPool`。智能体在步长内通过 `Request(agentID, priority)` 提交或刷新请求，`Step(dt)` 推进冷却与持有超时，并按确定性顺序结算待处理请求（优先级高者优先，同分按 `AgentID` 升序打破平局，与提交顺序无关）：

```go
tokens := ai.NewTokenPool(ai.TokenPoolConfig{
	Capacity:        2,                      // 最多 2 个敌人同时进攻
	Cooldown:        400 * time.Millisecond, // 归还令牌后的冷却时间
	PreemptMargin:   1.5,                    // 从低优先级持有者抢占令牌所需的优势差
	MaxHoldDuration: 3 * time.Second,        // 进攻卡住时自动归还令牌
})

// 每个敌人的行为树申请令牌，未获得时回退到绕圈待命：
enemyTree := ai.Selector(
	ai.WithAttackToken(tokens, enemyID, enemyPriority, attackSequence),
	circleTargetNode,
)

// 固定步长中：先驱动行为树，再统一结算令牌授予与抢占：
enemyTree.Tick(dt)
tokens.Step(dt)

// 敌人死亡或脱离战斗时：
tokens.Remove(enemyID)
```

- **优先级抢占**：无空闲令牌时，若等待请求的优先级高出最弱持有者至少 `PreemptMargin`（`PreemptMargin <= 0` 时严格高于即可），则直接接管其令牌且不进入冷却。`WithAttackToken` 在下一帧发现失去令牌时会自动重置运行中的子节点并返回 `StatusFailure`，使上层 `Selector` 平滑切回绕圈或待命分支。
- **多目标管理**：敌人面向多个目标时使用 `NewTokenManager(defaultCfg)`；可通过 `ConfigureTarget(targetID, cfg)` 为特定目标设置独立配额，敌人死亡时调用 `RemoveAgent(agentID)` 一次性清理所有目标的持有与等待，`Step(dt)` 按目标 ID 升序确定性推进。

### 交战槽位

`SlotAssigner` 使用最小代价二分匹配（匈牙利算法）将智能体分配到目标周围的同心 `EngagementRing` 槽位上：

```go
assigner := ai.NewSlotAssigner(
	ai.EngagementRing{Radius: 45, Slots: 4}, // 内圈近战槽位优先填满
	ai.EngagementRing{Radius: 90, Slots: 6}, // 外圈绕行槽位
)
assigner.StayBias = 10 // 世界单位迟滞偏置，抑制边界抖动换位

var slots []ai.SlotAssignment
// 在 game.Loop.Update 中随 targetPos 移动持续更新：
slots = assigner.Assign(slots, targetPos, []ai.SlotAgent{
	{ID: 1, Pos: enemy1.Pos},
	{ID: 2, Pos: enemy2.Pos},
})
```

- **路径不交叉与内圈优先**：内圈槽位完全填满后才使用外圈，最小化总欧氏距离从几何上避免智能体到槽位的直线路径交叉。智能体数量超过总槽位时，最优子集获得分配，多余智能体返回 `Assigned: false`（`Ring: -1`、`Slot: -1`）。
- **稳定性与确定性**：设置 `StayBias > 0` 为智能体保留上一帧槽位提供代价优惠，使移动目标周围的站位更稳定。内部按 `ID` 升序规范化输入，分配结果与切片传入顺序无关，跨运行完全一致，且复用内部缓冲区实现预热后零堆分配。

### 小队协作

`Squad` 以确定性升序维护成员名单，提供共享键值状态 `SquadState`、基于评分的战术角色分配 `AssignRoles` 以及可供行为树读取的小队级指令 `SquadOrder`：

```go
squad := ai.NewSquad(1, 10, 20, 30)
squad.State().SetBool("player_spotted", true)
squad.State().SetPoint("last_known_pos", playerPos)

squad.AssignRoles(
	ai.SquadRoleSpec{Name: "flanker", Count: 1, Score: flankScore},
	ai.SquadRoleSpec{Name: "suppressor", Count: 2, Score: suppressScore},
)
squad.SetOrder(ai.SquadOrder{Kind: "pinch", TargetID: playerID, Position: playerPos})

// 在小队成员的行为树中读取状态、指令与角色：
flankBranch := ai.Sequence(
	ai.SquadStateBool(squad, "player_spotted"),
	ai.SquadHasOrder(squad, "pinch"),
	ai.SquadHasRole(squad, memberID, "flanker"),
	ai.SquadOrderAction(squad, moveToFlank),
)
```

- **`AssignRoles`**：基于 `SquadRoleSpec` 执行最大权二分匹配，优先最大化可填满的合法角色数量，其次最大化总得分；`Score` 返回 `NaN` 或 `±Inf` 表示该成员不具备此角色资格。
- **`SquadOrder.Revision`**：每次 `SetOrder` 与 `ClearOrder` 都会递增修订号，便于行为树在指令 `Kind` 相同时仍能识别新下达的指令。
- **`SquadState`**：提供强类型存取方法（`Bool`、`Int`、`Float`、`String`、`Point`、`ID`）、泛型 `SquadStateGet[T]` 以及按字典序排序的 `Keys`。

## 影响图（Influence Maps）

`InfluenceLayer` 维护一张与 `ai.Grid` 同尺寸的二维标量影响网格（采用与 `rts.FlowField` 相同的 `Origin` / `CellSize` 世界坐标映射），用于战略与战术 AI 判断威胁、控制权、交战张力与安全走位：

```go
friendly := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
enemy := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
control := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
tension := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)

squadID := friendly.AddSource(ai.InfluenceSource{
	Cell:     ai.Cell{Col: 2, Row: 3},
	Strength: 10,
	Radius:   5,
	Falloff:  ai.FalloffLinear,
})
enemy.AddSource(ai.InfluenceSource{
	Cell:        ai.Cell{Col: 12, Row: 3},
	Strength:    12,
	Radius:      6,
	Falloff:     ai.FalloffExponential,
	FalloffRate: 0.45,
})

// 增量更新（预热后每步零堆分配，结果与 Rebuild 按位一致）：
friendly.MoveSource(squadID, ai.Cell{Col: 3, Row: 3})
friendly.Update(grid)
enemy.Spread(grid, ai.SpreadOptions{Decay: 0.2, Momentum: 0.5, Iterations: 2, Diagonal: true})

// 组合为战术图层：
control.Subtract(friendly, enemy) // 正值为己方控制，负值为敌方控制
tension.Add(friendly, enemy)      // 双方总势力（交战热度）
control.Normalize()               // 归一化至 [-1, 1]，保留 0 为中立

// 在单位周围半径 4 格内查找威胁最低且可达的格子：
safeCell, threatVal, ok := enemy.LowestInRadius(ai.Cell{Col: 5, Row: 3}, 4, ai.InfluenceQuery{
	Grid:      grid,
	Reachable: true,
})
// 在世界坐标下双线性采样或获取上坡/下坡梯度：
val, inBounds := enemy.Sample(unitPos)
grad := enemy.Gradient(unitPos)
_, _, _, _, _ = safeCell, threatVal, ok, val, inBounds
_ = grad
```

### 影响源、衰减曲线与绕障扩散

- **衰减曲线**：每个 `InfluenceSource` 在欧氏格子半径 `Radius` 内按 `FalloffLinear`（`max(0, 1 - dist/Radius)`）、`FalloffExponential`（`exp(-FalloffRate * dist)`）或 `FalloffCustom`（`CustomFalloff(dist, radius)`）写入影响值；不可通行格（`!grid.Walkable(col, row)`）始终保持 `0`。
- **增量更新**：使用 `AddSource` 注册动态单位，并通过 `MoveSource`、`SetSource`、`RemoveSource`、`MarkDirtyCell` 或 `MarkDirtyRect` 标记变更。`Update(grid)` 仅重算脏区域并按固定槽位顺序叠加，确保结果完全确定、与 `Rebuild(grid)` 按位一致，且预热后每步零堆分配。若只需一次性静态写入，可直接调用 `Stamp(grid, src)`。
- **带衰减与动量的绕障扩散**：`Spread(grid, opts)` 沿可通行格绕开障碍物传播影响（支持四向或禁止斜向穿角的八向扩散，正交步保留 `1 - Decay`，对角步保留 `(1 - Decay)^sqrt(2)`），并按 `Momentum` 与上一帧混合（`next = prev*Momentum + target*(1 - Momentum)`）。当图层存在跟踪源时，`Spread` 会先增量同步变更源，使移动或移除后的残余影响按动量平滑消退。

### 图层组合与空间查询

- **图层组合**：`Add`、`Subtract`、`Multiply`、`Combine(terms...)`、`AddScaled`、`Scale`、`Abs`、`Clamp`、`Normalize` 与 `CopyFrom` 均在预分配目标图层上零分配原地执行，并安全支持目标与输入别名相同。
- **空间查询**：`HighestInRadius` 与 `LowestInRadius` 在指定半径内查找极值格，可通过 `InfluenceQuery{Grid: grid, Reachable: true, Diagonal: ...}` 过滤可通行性与 BFS 可达性，并以确定性顺序打破平局（距离中心更近优先，其次行号更小，再次列号更小）。`Highest(grid)` 与 `Lowest(grid)` 查询全图极值；`Sample(worldPos)` 执行双线性插值采样；`Gradient(worldPos)` 与 `GradientAtCell(col, row)` 返回指向高影响方向的世界空间梯度向量（`math2d.Vec2`）。

### 调试热力图

可直接叠加在 `tilemap.Camera` 上绘制任意影响图以供调试：

```go
d.Save()
cam.Apply(d)
control.DrawHeatmap(d, cam, ai.HeatmapOptions{DrawGrid: true})
d.Restore()
```

`DrawHeatmap` 会按 `cam.VisibleBounds()` 裁剪视野外格子，正值使用 `PositiveColor`（默认绿色）、负值使用 `NegativeColor`（默认红色），透明度随 `|value| / MaxAbs` 线性变化。在 `-tags release` 构建下，`DrawHeatmap` 编译为空操作桩函数，`InfluenceHeatmapEnabled()` 返回 `false`。

## 战术位置查询与掩体点

掩体射击、潜行与战术游戏可使用 `CoverPoint`、`TacticalQuery`、`PointReservations` 与 `QueryBudget` 回答“应该站在哪里”（相对威胁处于遮蔽的掩体位、带清晰射界的侧翼点、或低威胁的可达位置），全流程确定性排序且预热后零堆分配：

```go
// 从瓦片地图实心边缘生成掩体点（也可来自 2D/3D 物理碰撞体或场景 YAML）：
covers := ai.GenerateCoverFromTilemap(levelMap, ai.TileCoverOptions{
	DefaultHeight: ai.CoverFull,
})

reservations := ai.NewPointReservations(ai.ReservationConfig{
	DefaultTimeout: 3 * time.Second,
})
budget := ai.NewQueryBudget(16, 8) // 每步最多 16 次视线射线检测与 8 次寻路查询

query := ai.NewTacticalQuery(
	[]ai.CandidateGenerator{
		&ai.CoverPointGenerator{Points: covers, MaxRadius: 240},
	},
	&ai.AngleTest{
		Mode:   ai.AngleCoverToTarget,
		Filter: ai.AtMost(math.Pi / 3), // 掩体朝向与威胁方向夹角不超过 60 度
		Weight: 2.0,
		Invert: true,
	},
	&ai.CoverHeightTest{Prefer: ai.CoverFull, Weight: 1.0},
	&ai.InfluenceTest{Layer: threatLayer, Weight: 1.5, Invert: true},
	&ai.LOSTest{LOS: hasLineOfSight, WantClear: false, Weight: 2.0}, // 对威胁不可见
	&ai.PathDistanceTest{Grid: navGrid, CellSize: 16, Weight: 2.0, Invert: true},
)

// 在 game.Loop.Update 中：
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

// 智能体死亡或离开掩体时释放预订：
reservations.Release(agentID)
```

### 掩体点与自动生成

每个 `CoverPoint` 记录世界空间站位 `Pos`、由 `Pos` 指向掩体障碍物的单位朝向向量 `Facing`（即该掩体所阻挡的来袭威胁方向，障碍物外法线为 `Facing.Neg()`）、高度类型 `CoverHeight`（`CoverHalf` 半身掩体或 `CoverFull` 全身掩体），以及可选的 3D 高度 `Elevation`、`ID` 和 `Tags`。

- **瓦片地图与网格生成**：`GenerateCoverFromTilemap(m, opts)` 合并连续的实心瓦片外露边缘（支持 `HalfCoverMask`、`HeightFunc` 或瓦片属性 `"cover": "half"|"full"`），并校验生成点落在可通行格内；`GenerateCoverFromGrid(g, origin, cellW, cellH, opts)` 可直接基于任意 `ai.Grid` 生成掩体点。
- **2D 与 3D 碰撞体生成**：`GenerateCoverFromBoxes`、`GenerateCoverFromWorld`（支持 `physics.Box`、`physics.Circle` 柱体与 `World.Tiles`）以及 `GenerateCoverFromSegments` 从 2D 碰撞边缘生成掩体点，并剔除嵌入相邻障碍物内部的候选点。在 3D 场景中，`physics3d.World.GenerateCoverPoints(opts)` 将轴对齐盒、定向盒（OBB）、球体与胶囊体的竖直侧面投影到 XZ 平面，按高度自动区分 `CoverHalf`/`CoverFull` 并保留地面 `Elevation`。
- **场景 YAML 编排**：通过 `sceneformat.RegisterCoverPoint(reg)` 注册内置 `"cover_point"` 组件（含 `height`、`facingX`、`facingY`、`tags` 字段），再用 `sceneformat.ExtractCoverPoints(doc)` 或 `sceneformat.ExtractBuiltCoverPoints(built)` 提取已应用父子变换的 `[]ai.CoverPoint`。

### 候选点生成器与有序测试

`TacticalQuery` 按声明顺序执行 `CandidateGenerator`，先排除已被其他智能体预订的位置，再依次运行 `TacticalTest`，最终按 `Score` 降序确定性排序返回存活的 `TacticalCandidate`（同分按 `CoverID`、`Pos.Y`、`Pos.X`、`Facing` 和生成顺序打破平局）：

- **生成器**：`CoverPointGenerator`（支持 `MaxRadius`、`MinHeight`、`RequiredTag`）、`RingGenerator`（围绕目标或查询者的同心圆环或扇形弧，支持 `FaceCenter`）、`GridGenerator`（世界网格或 `ai.Grid` 可通行格）以及 `NavMeshGenerator`（`*ai.NavMesh` 多边形质心或等距网格采样）。
- **测试项**：每个测试项通过 `Purpose`（`TestFilterAndScore`、`TestFilterOnly` 或 `TestScoreOnly`）、可选过滤区间 `Filter`（`AtLeast`、`AtMost`、`Between`）、权重 `Weight`、反转 `Invert` 及可选固定归一化区间 `NormMin`/`NormMax`（默认在存活候选间动态归一化）组合：
  - `DistanceTest`：到 `OriginQuerier`、`OriginTarget` 或 `OriginCustom` 的欧氏距离。
  - `AngleTest`：方向夹角（弧度），支持 `AngleCoverToTarget`、`AngleFlank`、`AngleToDirection` 与 `AngleFromTargetToDir`。
  - `InfluenceTest`：直接对 `*ai.InfluenceLayer` 双线性采样评分。
  - `CoverHeightTest`：按 `MinHeight` 过滤并偏好 `CoverHalf`/`CoverFull`。
  - `LOSTest`：通过游戏传入的 `LOSFunc` 回调检查候选点与目标之间（可双向）的视线通畅性（`WantClear: true` 用于寻找射界，`false` 用于寻找隐蔽掩体）。
  - `PathDistanceTest`：支持自定义 `PathCost` 回调、零分配网格 `AStar` 寻路或零分配 2D `NavMesh` 漏斗路径长度。
  - `CustomTest`：自定义 `Eval(ctx, candidate)` 回调。

### 占位预订与单步预算

- **`PointReservations`**：保证任意两个智能体不会同时预订同一 `CoverID`，也不会预订间距小于 `MinSeparation` 的位置。`RunAndReserve` 自动选取并预订最优未占用点，同时原子替换该智能体的旧预订；死亡时调用 `Release(agentID)`、掩体摧毁时调用 `ReleaseCover(coverID)`、`Refresh` 续期，`Step(dt)` 确定性过期超时预订。
- **`QueryBudget`**：对共享同一预算的所有智能体严格限制单步 `MaxRaycasts`（`LOSTest`）与 `MaxPathQueries`（`PathDistanceTest`）。进入高开销测试前，存活候选会按前置低开销测试得分确定性排序，确保有限预算优先花在最优候选上。每步开始调用一次 `Reset()`；使用 `Best`、`RunInto(dst, ctx)` 或 `RunAndReserve` 在预热后每次查询零堆分配。

## 感知

`Perception` 为一个智能体提供视觉、听觉以及对所感知事物的记忆：守卫能看到眼前的玩家，能走到最后听见声响的地方，也会在一段时间后放弃搜寻。`PerceptionSystem` 在 `game.Loop` 的固定步长上统一推进多个智能体的感知：

```go
walls := []lights2d.Occluder{lights2d.RectOccluder(wallBounds)}

guard := &ai.Perception{
	Self:   guardID, // 不会感知到自己
	Team:   teamGuards,
	Facing: core.Point{X: 1},
	Sight: ai.SightConfig{
		Range:           160,
		HalfAngle:       math.Pi / 4, // 90° 视锥
		PeripheralRange: 24,          // 默认覆盖四周，可用 PeripheralHalfAngle 收窄
	},
	Hearing:     ai.HearingConfig{Range: 120},
	ForgetAfter: 5 * time.Second,
}

senses := ai.PerceptionSystem{
	LineOfSight: lights2d.LineOfSight(&walls),
	Budget:      32, // 每步的视线检测次数上限，由所有智能体共享
}
senses.Add(guard)

// 在 game.Loop.Update 中：
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

### 视觉

目标位于眼睛 `Range` 范围内且与 `Facing` 的夹角不超过 `HalfAngle`（弧度），或位于较短的 `PeripheralRange` 范围内且夹角不超过 `PeripheralHalfAngle`（为零时覆盖四周，为负时关闭周边视野）时，即处于视野中。眼睛位置为 `Pos + EyeOffset`（世界坐标），听觉也从这一点测量距离。处于视野中的目标，在系统的 `LineOfSight` 判定从眼睛到目标的线段畅通时才算被看见；`LineOfSight` 为 nil 时，视野中的目标都会被看见。三个适配器覆盖常见的遮挡来源：

| 适配器 | 遮挡物 |
| --- | --- |
| `ai.PhysicsLineOfSight(world, query, layers)` | `physics.World` 中的刚体与瓦片，通过 `RaycastFirstLayer` 检测。 |
| `lights2d.LineOfSight(&occluders)` | 投射阴影的 `lights2d` 遮挡物，通过 `lights2d.Visible` 检测；每次调用都会读取该切片，因此关卡可以随时修改它。 |
| `perception3d.PhysicsLineOfSight(world, layers)` | `physics3d.World` 中的刚体与静态碰撞体，供 3D 智能体使用。 |

从智能体自身刚体内部出发、或终点落在目标刚体内的射线会击中该刚体，因此应让智能体和目标不处于阻挡视线的层上，例如把墙体单独放在一层。

### 听觉

`PostNoise` 将一个 `Noise`（位置、`Loudness` 响度、声源 ID 与阵营）加入队列，由下一次 `Step` 处理：声源距智能体眼睛不超过 `Hearing.Range × Loudness` 时即被听见，无需视线。噪声按投递顺序处理，并在该步中被消耗。

### 记忆与事件

每个智能体按 `StimulusID` 为每个刺激保存一条 `Memory`，因此先被看见、后被听见的同一目标只刷新同一条记忆。记忆包含最后已知的 `Pos` 与 `Vel`（听到时速度为零）、最近一次刷新它的感官 `Sense`、自上次感知以来的 `Age`、当前是否 `Visible`，以及强度 `Strength`：看见时为 1，听见时至少提升到 `1 - 距离/可听范围`（可听范围为无穷大时为 1），并在 `ForgetAfter` 内线性衰减到 0。`Age` 达到 `ForgetAfter` 时记忆被遗忘。可用 `Memories`、`AppendMemories`、`Memory(id)` 与 `Sees(id)` 读取记忆，用 `Forget(id)` 与 `Clear` 删除记忆。

每次 `Step` 之后，`Events` 列出发生的变化：

| 类型 | 时机 |
| --- | --- |
| `EventGained` | 感知到此前没有记忆的刺激。 |
| `EventUpdated` | 已记住的刺激在离开视野后被再次看见、被看见在移动，或被听见。视野中静止的目标不产生事件。 |
| `EventLost` | 视野中的刺激离开视野，记忆仍保留。 |
| `EventForgotten` | 记忆的 `Age` 达到 `ForgetAfter` 并被删除。 |

### 阵营

`Team` 是一个小整数阵营编号。系统的 `Attitude` 函数（为 nil 时使用 `DefaultAttitude`）决定智能体把刺激视为 `Hostile`、`Neutral` 还是 `Friendly`：同阵营为友方，任一方为 `NoTeam` 时为中立，其余为敌对。`SightConfig.Detect` 与 `HearingConfig.Detect` 用 `DetectHostile`、`DetectNeutral`、`DetectFriendly` 和 `DetectAll` 选择各感官关注的态度；为零时只感知敌对刺激。

### 预算与确定性

智能体多达数百个时，每步为每对智能体与目标都发射射线开销过大。`Budget` 限制一步内的视线检测次数：处于视野中的智能体—目标对先按智能体加入顺序、再按目标 ID 排序，每步从上次检测到的位置之后检测 `Budget` 对，下一步接着继续，因此每个智能体都按固定顺序轮到。尚未重新检测的对沿用上一次的结果。`Stats` 报告一步内的候选数、检测数与延后数。`LineOfSight` 为 nil 时不发射射线，预算不生效，视野中的每一对都会立即被看见。只要 `LineOfSight` 是确定性的，相同输入的每次运行都会得到相同的记忆与事件。

### 3D

实验性包 `game/ai/perception3d` 在 `math3d.Vec3` 上运行同一个引擎，因此其感官、记忆、事件、阵营与预算的行为与上文完全一致。其 `Perception` 的视锥沿 `Forward` 方向（为零时朝向 -Z），角度在 3D 空间中测量，`EyeOffset` 通常为 +Y 方向的头部高度；`System`、`Target`、`Noise`、`Memory`、`Event` 与 `LineOfSight` 与对应的 2D 类型一一对应，并复用 `ai.SightConfig`、`ai.HearingConfig`、`ai.StimulusID`、`ai.Team` 与事件类型：

```go
import "github.com/nsgo-dev/game/packages/game/ai/perception3d"

guard := &perception3d.Perception{
	Team:        teamGuards,
	Forward:     math3d.Vec3{Z: -1},
	EyeOffset:   math3d.Vec3{Y: 1.7},
	Sight:       ai.SightConfig{Range: 40, HalfAngle: math.Pi / 4},
	ForgetAfter: 5 * time.Second,
}
senses := perception3d.System{LineOfSight: perception3d.PhysicsLineOfSight(world, layerWalls), Budget: 64}
senses.Add(guard)

// 在 game.Loop.Update 中：
senses.Step(dt, []perception3d.Target{{ID: playerID, Team: teamPlayer, Pos: playerPos}})
```
