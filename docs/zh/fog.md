# fog

`game/fog` 为策略游戏在正交瓦片网格上追踪战争迷雾：每个队伍此刻能看到哪些格子、以前探索过哪些格子、从未见过哪些格子。它把迷雾画在瓦片地图上方，并随游戏的其余状态一起存档。

```go
import "github.com/nsgo-dev/game/packages/game/fog"
```

## 队伍、视野源与状态

`Fog` 覆盖一张格子网格。每个队伍（`fog.Team`，任意 `int`）通过自己的视野源观察：单位、建筑或守卫，每个视野源有一个格子和以格为单位的半径。对每个队伍而言，一个格子处于三种 `State` 之一：

| 状态 | 含义 | 绘制方式 |
| --- | --- | --- |
| `Unseen` | 队伍从未见过这个格子。 | `Style.Unseen`，默认不透明黑色 |
| `Explored` | 队伍以前见过这个格子，但此刻看不到。 | `Style.Explored`，默认半透明黑色 |
| `Visible` | 队伍的某个视野源此刻能看到这个格子。 | 不遮盖 |

```go
f := fog.NewFromTilemap(level, tilemap.TileSolid) // 墙挡住视线
scout := f.AddSource(fog.Source{Team: player, Col: 4, Row: 6, Radius: 7})
f.Update()
```

- `NewFromTilemap(m, mask)` 采用地图的尺寸和瓦片尺寸，并把瓦片标志包含 `mask` 任一位的格子设为不透明。`New(width, height, tileWidth, tileHeight)` 从空网格开始（尺寸为负、恰好一边尺寸为零或超过 `MaxCells` 个格子时会 panic），之后用 `SetOpaque` 修改单个格子，例如门被打开时。
- 距视野源 `dx` 列、`dy` 行的格子在 `dx*dx+dy*dy <= Radius*Radius+Radius` 时处于范围内，这样圆盘更圆。半径 0 只能看到视野源自己所在的格子，位于网格之外的视野源什么也看不到。
- 不透明格子通过使用精确整数斜率的对称阴影投射（symmetric shadowcasting）阻挡视线：地面格子 A 能看到地面格子 B，当且仅当 B 能看到 A，且结果从不受浮点舍入影响，因此锁步对局中的每个客户端算出的迷雾完全相同。视线到达的不透明格子本身是可见的，所以墙会显示在视野边缘。
- 同一队伍的视野源共享视野；其他队伍的视野源不会为它揭开任何格子。

## 随单位移动更新

在固定步长中移动视野源，并在读取或绘制迷雾前调用一次 `Update`：

```go
loop.Update = func(dt time.Duration) {
	moveUnits(dt)
	for _, u := range units {
		f.MoveTo(u.Vision, u.Pos) // 或 f.Move(id, col, row)
	}
	f.Update()
}
```

- `Update` 只重算自上次调用以来视野源或不透明格子发生变化的队伍，把它们看到的格子加入已探索格子，并报告是否有任何队伍的迷雾发生变化。
- `SetSource` 一次性修改视野源的队伍、格子或半径；单位死亡时用 `RemoveSource` 移除它，队伍保留已探索的格子。
- `State`、`Visible` 和 `Explored` 按格子查询，`VisibleAt(team, p)` 按世界坐标点查询，例如把迷雾中的敌方单位隐藏起来。`CellAt(p)` 依据 `TileWidth` 和 `TileHeight` 把世界坐标点换算成格子。
- `Explore`、`ExploreRect` 和 `ExploreAll` 把格子标记为已探索但不使其可见（侦察报告或揭示的地标），`Forget` 清除队伍已探索的格子中此刻看不到的部分，`ExploredCount` 统计已探索格子数。
- 队伍的可见或已探索格子每次变化时，`Revision(team)` 都会增长，加载迷雾时每个队伍也都会得到新的修订号，因此小地图或缓存的迷雾图像只在内容不同时才重绘。修订号来自程序中所有迷雾共用的一个计数器，所以替换另一份迷雾而加载的迷雾（即使经由 `save.Store.Load`）也绝不会重复缓存已经见过的修订号。

## 在地图上方绘制

在瓦片地图和单位之后、以相同的相机变换绘制迷雾：

```go
d.Save()
cam.Apply(d)
level.Draw(d, cam)
drawUnits(d)
f.Draw(d, player, cam, fog.DefaultStyle())
d.Restore()
```

`Draw` 在未见格子上填充一条路径、在已探索格子上填充另一条路径，每条路径由横向连续的格子段组成，可见格子保持不遮盖。与 `Tilemap.Draw` 一样，它只用相机跳过 `cam.VisibleBounds()` 之外的格子，相机为 nil 时绘制整张网格。`Style` 中透明度为 0 的颜色会让对应格子不被遮盖，例如没有地形记忆的游戏可用 `fog.Style{Unseen: core.RGB(0x000000)}`。

## 存档

`Fog` 序列化为带版本号的 JSON 文本，因此可以放进游戏自身的状态里存入 `save.Store` 的存档槽：

```go
type saved struct {
	Units []unitSave `json:"units"` // 每个单位保存自己的 fog.SourceID
	Fog   *fog.Fog   `json:"fog"`
}

store.Save("slot1", saved{Units: units, Fog: f})

var s saved
store.Load("slot1", &s) // s.Fog 看到的和已探索的与 f 完全相同
```

```json
{"version":1,"width":3,"height":2,"tileWidth":8,"tileHeight":8,"opaque":[".#.","..."],"nextSource":2,"sources":[{"id":1,"team":2,"col":2,"row":1,"radius":1},{"id":2,"team":1,"col":0,"row":0,"radius":0}],"teams":[{"team":1,"explored":["#..","..."]},{"team":2,"explored":[".##",".##"]}]}
```

- 快照包含网格和瓦片尺寸、不透明格子、带 ID 的视野源，以及每个队伍以 `.` 和 `#` 组成的行表示的已探索格子，每个网格行一个字符串，因此阅读存档的人能看到每个队伍的已探索地图。可见格子不存储：加载时根据视野源和不透明格子重新计算。
- 同一份迷雾总是写出相同的字节：字段顺序固定，视野源按 ID 排列，队伍按升序排列，不含时间戳。加载后再保存不会改变任何内容。
- 加载保留视野源 ID 和下一个待分配的 ID，因此单位存下的 `fog.SourceID` 仍能移动它的视野源。
- 加载会拒绝未知字段、缺失的版本号、更新的版本（`ErrFutureVersion`）、与网格尺寸不一致的行、顺序错误或重复的视野源与队伍、ID 为 0 或大于 `nextSource` 的视野源、所属队伍没有已探索网格的视野源、大于 `MaxCells` 的网格，以及恰好一边尺寸为零的网格；其他错误都包装 `ErrInvalidSnapshot`，被拒绝的快照不会改变迷雾。
