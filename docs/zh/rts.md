# rts

`game/rts` 是即时战略游戏的单位操控层：在 `ai.Grid` 上把大量单位引向目标的流场（flow field）、带分离与贴墙滑动的单位群 `Crowd`、以带版本号的 YAML 定义的阵型、框选与点选，以及数字编队。

```go
import "github.com/nsgo-dev/game/packages/game/rts"
```

一切都是确定性的，在 `game.Loop` 的固定步长上推进，没有后台 goroutine：同一张网格上的同一组命令，在同一种 CPU 架构上每次运行都把同样的单位送到同样的位置。位置是 `float32`，而 Go 在 arm64 上可能把一次乘法和一次加法融合成一条指令、在 x64 上则不会，所以同样的命令在不同架构上的结果可能相差几个 ulp；跨架构的帧同步多人游戏应由权威主机下发状态，而不是让每个对端各自推进自己的 `Crowd`。

## 流场

`FlowField` 为网格的每个格子保存两样东西：到最近目标格的最便宜路径代价（积分场），以及站在该格的单位沿路径前进的方向（方向场）。构建一次就是在网格上做一次 Dijkstra 搜索；之后任意数量的单位都以常数时间读取方向，所以一千个前往同一处的单位只花一次搜索，而不是一千次。

```go
grid := ai.NewCostMap(160, 128) // 或任意 ai.Grid / ai.CostGrid，例如包装 tilemap 的 ai.GridFunc
field := rts.NewFlowField(math2d.Vec2{}, 16) // 每格 16 个世界单位宽，格 (0, 0) 位于原点
field.Build(grid, ai.Cell{Col: 140, Row: 64})

dir, ok := field.Sample(unitPos) // 单位向量，由 unitPos 周围四格混合而来
```

- 默认 8 方向移动（`Diagonal`），斜向一步代价为 sqrt(2)，且不会切过阻挡格的角。走进一个格子的代价是它的 `ai.CostGrid` `MoveCost`，因此格子的 `Cost` 等于 `ai.AStar` 从该格到最近目标找到的代价。
- `Build` 可一次接受多个目标，每个格子都通向其中最近的一个。被阻挡或越界的目标会被忽略，`Build` 返回是否存在有效目标。
- `Sample` 混合某点周围可到达格子的方向，单位因而平滑转向，而不是按 45 度一档地转，也不会被拉进墙里。`Cost`、`Reachable`、`Direction`、`CellAt` 与 `CellCenter` 按格读取流场。
- 重建同尺寸的流场不分配内存。

`FieldCache` 在一张网格上保留最近使用的流场，以目标集合为键，让前往相同目标的单位共用一个流场。改动网格后（造了墙、炸了桥）调用 `Rebuild`，就地重新积分所有缓存的流场；沿这些流场移动的单位在下一步就会看到变化。`Builds` 统计搜索次数，测试或性能分析可据此判断流场是否被共用。

## 单位群与移动命令

`Crowd` 在网格上移动单位。用 `Add` 添加单位，用 `Move` 下令，并在循环的 `Update` 里调用 `Step`：

```go
crowd := rts.NewCrowd(grid, rts.CrowdOptions{CellSize: 16})
crowd.Add(rts.UnitSpec{Pos: math2d.V2(40, 650), Radius: 5, MaxSpeed: 96, Kind: kindRifleman})

crowd.Move(selection.IDs(), rts.MoveOrder{
	Target:    clickWorld,
	Formation: rts.Formation{Shape: rts.ShapeBox, Spacing: 14},
	KeepPace:  true, // 以最慢单位的速度行进
})

loop.Update = func(dt time.Duration) { crowd.Step(dt) }
```

`Move` 在目标周围排开阵型；除非设置了 `Facing`，阵型朝向从队伍指向目标的方向。落在阻挡格、或队伍无法到达的格子上的阵位，会移到最近的可到达空格。若单位根本到不了目标（例如目标在孤岛上），阵型改以它们能到达的最近格子为中心。单位与阵位的匹配保证路径不交叉，整队共用一个以各阵位所在格为目标的流场。每个单位沿该流场前进，直到在 `DirectRange` 内能看到自己的阵位，再径直走过去；到达阵型区域却与自己的阵位隔着墙的单位，会在这道命令余下的时间里沿自己的 `ai.AStar` 路径走过去，被人群挤离路径时就重新规划。每个单位只在每八步中的一步（按 ID 错开）搜索路径，因此即使很多单位都需要绕路，开销也有上限。

`Step` 以线性时间运行：邻居来自每步重建且不分配内存的分桶网格，身体重叠的单位相互推开（`Separation`、`Gap`），身体会沿阻挡格和网格边缘滑动。每个单位的受力都根据本步开始时的位置计算，所有单位算完才移动，因此结果与单位的添加顺序无关。

每个 `Unit` 报告自己的 `State`：下令前和 `Stop` 之后为 `Idle`，途中为 `Moving`，进入阵位 `ArriveRadius` 范围后为 `Arrived`。已到达的单位守住阵位，被推远的会走回去。`Units` 返回全部单位供绘制，`Unit(id)` 返回其中一个，`Remove` 删除一个。

在 Apple M1 Max 上，`BenchmarkCrowdStep1000` 推进 1000 个挤过迷宫的单位一步约 0.4 毫秒，不到 60 fps 一帧的 3%。只有当某个单位自己的绕行路径超出它在之前搜索中保留的缓冲区时，一步才会分配内存，因此每个单位都规划过它最长的绕行路径后，分配就会停止。命令本身开销更大：在 `FieldCache` 为空时，`BenchmarkMove1000` 在同一迷宫上为 1000 个单位排布阵型、分配槽位并构建两个流场约需 6.5 毫秒，所以应在输入时下达命令，而不是每一步都下达。`TestCrowdThousandUnitsReachTargetWithinFrameBudget` 让 1000 个单位以方阵穿过 160×128 的迷宫，只要有单位未到达、停在墙里，或平均每步超过 60 fps 的一帧，测试就失败。

## 阵型

`Formation` 把单位排成横贯朝向的若干行，第 0 行在最前：

| 形状 | 排法 | `Width` |
| --- | --- | --- |
| `line` | 并排 | 每行阵位数；0 表示一行 |
| `column` | 前后纵列 | 每行阵位数；0 表示一个 |
| `box` | 实心方阵 | 每行阵位数；0 表示正方形 |
| `wedge` | 尖端一个，之后每行多一个 | 忽略 |

`Spacing` 是相邻阵位中心的距离，应不小于最大单位半径的两倍。`Layout` 返回围绕阵型中心的阵位，`Place` 按中心与朝向把它们换算成世界坐标，`Assign` 以 O(n log n) 把单位匹配到阵位，让前面的单位占前排、左边的单位占左侧。`Move` 会用到这三者；也可以自己调用它们，在光标下预览阵型。

游戏的阵型放在 YAML 文件里，策划可以直接编辑并审阅差异：

```yaml
version: 1
formations:
  - name: line
    shape: line
    spacing: 1.5
  - name: block
    shape: box
    spacing: 1.2
    width: 8
```

`ParseFormations` 拒绝未知字段、第二个 YAML 文档、非 1 的版本、空名或重名、未知形状、非正有限数的间距以及负宽度。`Encode` 以固定字段顺序和两空格缩进输出，文件读入再保存会逐字节不变。`Get` 按名字查找阵型。

## 选择

`Selection` 是按选中顺序排列的已选单位集合，提供 `Set`、`Add`、`Remove`、`Toggle` 与 `Retain`（用于剔除阵亡单位）。`BoxSelector` 把世界坐标下的指针输入变成选择：

```go
sel := &rts.Selection{}
box := &rts.BoxSelector{
	Picker:    crowd, // Crowd 实现了 rts.Picker
	Selection: sel,
	Filter:    func(id rts.UnitID) bool { return owner[id] == me },
}

view := core.View{
	OnPressDown: func(ev core.PressEvent) { box.Press(toWorld(ev.Position), ev.Modifiers, ev.Clicks) },
	OnMouseMove: func(ev core.MouseEvent) { box.Drag(toWorld(ev.Position)) },
	OnPressUp:   func(ev core.PressEvent) { box.Release(toWorld(ev.Position)) },
}
```

- 按下到松开移动不足 `DragThreshold` 时，选中指针下的单位（容差 `ClickSlop`）；点在空地上会清空选择。
- 拖动时选中矩形碰到的所有单位。拖动过程中 `Box` 返回该矩形，`DrawSelectionBox` 负责绘制。
- 双击选中 `View`（屏幕上的世界区域）内与被点单位 `Kind` 相同的所有单位。
- `ModeFor` 读取按下时的修饰键：Shift 追加到选择，Control（macOS 上为 Command）切换单位的选中状态，不按修饰键则替换选择。

## 编队

`ControlGroups` 保存绑定到数字键的十个编队。`HandleKey` 对 `core.KeyEvent` 应用常见按键约定：

| 按键 | 动作 |
| --- | --- |
| Control 或 Command + 数字 | `GroupAssigned`：编队设为当前选择 |
| Shift + 数字 | `GroupAdded`：当前选择加入编队，并选中整个编队 |
| 数字 | `GroupRecalled`：选择替换为该编队 |
| 在 `DoubleTapSteps` 内连按两次数字 | `GroupFocused`：游戏把镜头对准该编队（`Crowd.Centroid`） |

双击按游戏步数而非挂钟时间计算，回放时会召回同样的编队；每次调用都传入同一个步数计数，例如 `Crowd.Steps`。`Forget` 与 `Retain` 从所有编队中剔除阵亡单位。

## 调试绘制

`DrawFlowField` 在 `core.Drawing` 当前的相机变换下，以一条路径描出每个可到达格子的方向，并在每个目标格画一个叉；放进调试叠加层，就能看到单位将如何移动。
