# ai

`game/ai` 提供可直接运行在 `game.Loop` 固定步长上的确定性游戏 AI 组件：网格 `AStar` 寻路、带漏斗平滑的 2D 导航网格 `NavMesh`、转向行为（steering）以及行为树。

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

- **组合节点**：`Sequence`、`Selector`（当高优先级分支成功或运行时会自动重置被抢占的低优先级运行中分支）、`Parallel(policy, ...)`（`RequireAll` 或 `RequireOne`）。
- **修饰节点**：`Guard(cond, child)`、`Cooldown(d, child)`、`Inverter`、`Succeeder`、`Failer`、`Repeater(count, child)`、`UntilFail`。
- **叶节点**：`Action(fn)`、`Condition(fn)`、`Wait(d)`。
