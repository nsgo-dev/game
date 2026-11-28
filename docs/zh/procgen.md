# procgen

`github.com/nsgo-dev/game/pkg/procgen` 在瓦片网格上生成 2D 关卡，提供四种生成器：以走廊相连的 BSP 房间、元胞自动机洞穴、基于 YAML 规则文件的波函数坍缩，以及在地牢布局图上拼接手工房间模板。

每个生成器都接收一个 [`random.Stream`](determinism.md)，所有随机数都取自它，所以相同的种子与配置在任何操作系统和架构上都生成逐格相同的关卡。BSP、Cave 与 GenerateRooms 的可行走格子总是构成一个 4 连通区域；WFC 也是如此，除非设置了 `AllowDisconnected` 或其规则没有可行走瓦片。

## Grid

`Grid` 是 `Width` × `Height` 个 `uint32` 值，按行存放在 `Cells` 中。BSP 与 Cave 写入 `procgen.Wall`（0）和 `procgen.Floor`（1）；WFC 写入每格瓦片在规则集中的索引。

```go
g.At(x, y)            // 网格外返回 Wall
g.Set(x, y, procgen.Floor)
g.Fill(procgen.Rect{X: 2, Y: 2, W: 5, H: 3}, procgen.Floor)
fmt.Print(g)          // '#' 为 Wall，'.' 为 Floor，更大的值为 'a'...
```

`Regions(passable)` 对 `passable` 接受的格子做洪水填充，按从大到小返回各 4 连通区域。`Connected(passable)` 报告区域是否至多一个。BSP 与 Cave 的关卡用 `procgen.IsFloor`，WFC 的关卡用 `rules.Walkable`；游戏修改关卡后也可以再跑一次同样的检查。

`Paint` 把网格复制到 [tilemap](tilemap.md) 的某一层，并把每个值映射为瓦片 GID：

```go
m := tilemap.New(g.Width, g.Height, 16, 16, tileset)
m.Layers = append(m.Layers, tilemap.TileLayer{Name: "ground", Visible: true})
g.Paint(m, 0, 0, 0, func(v uint32) uint32 {
	if v == procgen.Floor {
		return floorGID
	}
	return wallGID
})
```

## BSP 房间

`BSP` 把关卡切分为若干分区，在每个叶子分区放一个房间，再用 L 形走廊经由最近的一对房间连接兄弟分区，因此每个房间都可到达。

```go
streams := random.NewStreams(runSeed)
d, err := procgen.BSP(streams.Stream("floor-3"), procgen.BSPConfig{
	Width:  80,
	Height: 50,
	Loops:  3, // 额外的走廊，让关卡带环路
})
// d.Grid、d.Rooms（以 Rect 表示的房间）、d.Corridors（From、To、Path）
```

| 字段 | 默认值 | 含义 |
| --- | --- | --- |
| `MinLeaf` | 10 | 分区的最小边长；分区的某条边不小于它的两倍时继续切分 |
| `MinRoom` | 4 | 房间的最小边长 |
| `MaxRoom` | 0 | 房间的最大边长；0 表示房间可占满分区 |
| `Padding` | 1 | 房间与分区边缘之间的墙格数，保证房间不会相互合并 |
| `Loops` | 0 | 在随机房间之间额外加的走廊数 |

关卡最外一圈始终是墙。出生点可放在 `d.Rooms[0]`，出口放在离它最远的房间。

## 元胞自动机洞穴

`Cave` 先按 `WallChance` 把关卡内部随机填成墙，再平滑 `Steps` 次：8 个邻居中至少 `WallAt` 个是墙的格子变为墙，至多 `FloorAt` 个是墙的格子变为地面。剩下的若干洞穴随后合成一个：

- 默认只保留最大的洞穴，其余填成墙。
- 设置 `Join` 时，每个不少于 `MinRegion` 格的洞穴都沿穿过岩石的最短路径打通到最大的洞穴。

```go
g, err := procgen.Cave(random.New(seed), procgen.CaveConfig{
	Width:     64,
	Height:    40,
	Join:      true,
	MinRegion: 6,
})
if errors.Is(err, procgen.ErrNoFloor) {
	// 噪声被平滑成了整块岩石，换下一个种子
}
```

默认值为 `WallChance` 0.45、`Steps` 5、`WallAt` 5、`FloorAt` 3。只有 `WallAt` 也未设置时 `FloorAt` 才取默认值；设置了 `WallAt` 后，`FloorAt` 为 0 表示只有没有墙邻居的格子才变为地面。

格子数超过 `procgen.MaxCells`（2^24）或某条边超过 `procgen.MaxSide` 的关卡，每个生成器都会拒绝，返回包装了 `ErrInvalidConfig` 的错误。

## 波函数坍缩

`WFC` 用简单瓦片模型填充网格：把剩余瓦片最少的格子按权重坍缩为一个瓦片，再把相邻规则传播给邻居；遇到矛盾时重新开始，至多 `Attempts` 次（默认 20），之后返回 `ErrNoSolution`。

瓦片及其规则写在 YAML 文件里：

```yaml
version: 1
tiles:
  - name: floor
    weight: 6
    walkable: true
  - name: pillar
    weight: 1
  - name: water
    weight: 2
neighbors:
  - {left: floor, right: floor}
  - {left: floor, right: pillar}
  - {left: pillar, right: floor}
  - {up: floor, down: floor}
  - {up: floor, down: pillar}
  - {up: pillar, down: floor}
  # ...
```

- `version` 必须为 1，未知字段会被拒绝。
- 每个瓦片有唯一的 `name` 和正数 `weight`（相对出现频率），所有权重之和必须是有限数；`walkable` 标记连通约束要连起来的瓦片。
- `neighbors` 的每一项允许一个有序对：`left` 紧挨在 `right` 左边，或 `up` 紧挨在 `down` 上方。未列出的组合一律禁止，所以对称规则要把两种顺序都写上。

```go
rules, err := procgen.ParseRules(data)
g, err := procgen.WFC(random.New(seed), procgen.WFCConfig{Width: 32, Height: 24, Rules: rules})
// g.At(x, y) 是 rules.Tiles 的索引
```

坍缩过程中，路径约束保持可行走瓦片连通：已无法到达可行走区域的格子会去掉其可行走瓦片，而一旦失去就会把可行走区域分开的格子会去掉其他瓦片。因此 WFC 返回的关卡至少有一个可行走格子，并能通过 `Connected(rules.Walkable)`。设置 `AllowDisconnected` 可关闭该约束。

每次坍缩都会重新扫描网格，开启约束时还要重新搜索可行走区域，所以一次求解的开销大约是格子数的平方：32 × 24 需要几十毫秒，64 × 64 约一秒，128 × 128 则超过十秒。因此 `WFC` 至多接受 `procgen.MaxWFCCells`（64 × 64）个格子；更大的地图请用较小的块拼成，或改用 BSP 或 Cave。

`EncodeRules` 以固定的字段顺序把规则集写回 YAML，瓦片与 neighbors 保持原有顺序，所以解析再编码它写出的文件会逐字节还原。

## 基于地牢图的房间模板

`GenerateRooms` 根据生成的 `LayoutGraph`（包含起点、终点、分支、环路以及锁与钥匙的递进关系）拼接手工设计的房间模板，生成肉鸽风格地牢关卡。

房间模板写在带版本号的 YAML 文件中：

```yaml
version: 1
rooms:
  - id: entry
    weight: 1
    tags:
      - start
    doors:
      - side: north
        offset: 3
      - side: south
        offset: 3
      - side: east
        offset: 2
      - side: west
        offset: 2
    tiles:
      - '###.###'
      - '#.....#'
      - '.......'
      - '#.....#'
      - '###.###'
  - id: boss_vault
    tags:
      - goal
    width: 7
    height: 5
    source: rooms/boss_vault.tmj
    doors:
      - side: north
        offset: 3
      - side: south
        offset: 3
```

- `version` 必须为 1（`RoomTemplatesVersion`），未知字段会被拒绝。
- 每个房间有唯一的 `id`、可选的正数权重 `weight`（默认 1）、可选标签 `tags`（如 `start`、`goal`、`key`、`combat`）、可选的 `allowRotate`（允许按 90° 顺时针旋转）以及至少一个门洞 `doors`（`side` 为 `north`/`south`/`east`/`west`，从 0 开始的 `offset`，可选 `width` 默认为 1，可选 `channel` 限制只有同频道的门洞能对接）。
- 每个房间必须二选一地指定内联 ASCII `tiles`（`#` 表示 `Wall`，`.` 表示 `Floor`，`a`..`z` 映射为 `2..27`）或外部文件引用 `source`（如 `.tmj` 瓦片地图或场景文件）及 `width`、`height`。
- `ParseRoomTemplates` 与 `EncodeRoomTemplates` 会校验并按固定字段顺序以 2 空格缩进做无损往返编码。

### 布局图与锁/钥匙递进

可以用 `GenerateGraph` 单独生成 `LayoutGraph`，或用 `GenerateRooms` 一步完成构图与摆放：

| 字段（`GraphConfig`） | 默认值 | 含义 |
| --- | --- | --- |
| `CriticalPath` | 5 | 从 `Start` 到 `Goal` 的主路径房间数（至少为 2） |
| `Branches` | 0 | 从主路径与已有分支延伸出的支路房间数 |
| `BranchMaxDepth` | 2 | 单条支路的最大延伸深度 |
| `Loops` | 0 | 在同一锁层级内额外添加的目标环路边数 |
| `Locks` | 0 | 放置在主路径上的锁/钥匙对数量（必须小于 `CriticalPath`） |
| `MaxDegree` | 4 | 单个房间最多连接的通道数 |

锁将地牢划分为 `0..Locks` 递进层级。每把锁的钥匙都放在严格早于该锁的层级中，而环路只连接同一层级（且生成树奇偶性相反，保证偶长环）的房间。`graph.VerifyLocks()` 验证从 `Start` 出发无需越过某把锁即可拿到对应的钥匙，且拿到钥匙后可到达 `Goal`。

### 自由拼接与 Spelunky 风格房间网格模式

```go
templates, err := procgen.ParseRoomTemplates(data)
lvl, err := procgen.GenerateRooms(random.New(seed), procgen.RoomsConfig{
	Templates: templates,
	Mode:      procgen.PlacementGrid, // 或 procgen.PlacementStitch（默认）
	GridCols:  4,
	GridRows:  4,
	Graph: procgen.GraphConfig{
		CriticalPath: 6,
		Branches:     4,
		Loops:        2,
		Locks:        2,
	},
})
// lvl.Grid（满足 Connected）、lvl.Rooms（TemplateID、Source、Bounds、GridSlot、Rotation、Role、KeyID）、
// lvl.Doors（FromRoom、ToRoom、Cells、LockID）、lvl.Graph
```

- `PlacementStitch`（默认）通过对齐匹配的门洞自由拼接不同尺寸的房间且不重叠，并在同层级对齐门洞处闭合环路；若一次尝试受阻则从 `random.Stream` 确定性重试，最多 `Attempts` 次（默认 50），失败时返回 `ErrNoLayout`。
- `PlacementGrid` 将等尺寸模板摆放在 `GridCols × GridRows` 的粗粒度房间格中（均为 0 时自动估算尺寸），类似 Spelunky 从第 0 行向下展开并让所有图边对齐相邻槽位。
- `PlaceRooms` 支持将已有的 `LayoutGraph` 按上述任一模式摆放。
- 对于通过 `source` 引用 `.tmj` 或场景文件的模板，可传入 `ResolveGrid` 回调（配合 `procgen.GridFromTilemap` 与 `tilemap.LoadTiled`）把瓦片写入 `Grid`；未提供 `ResolveGrid` 时会自动生成带外墙与地面内部的默认占位。未使用的门洞会被 `Wall` 封闭，已连接的门洞会被打开，保证 `Grid.Connected` 始终成立。

## 程序化放置

放置文件（`*.placement.yaml`）列出若干步骤，每个步骤在一块区域上采样点，并指定在这些点上生成的 prefab：

```yaml
version: 1
seed: 7
steps:
  - id: trees
    sampler: grid
    spacing: 2
    count: 6
    prefab: tree
  - id: lamps
    sampler: grid
    spacing: 4
    prefab: lamp
```

- `version` 必须为 1（`PlacementVersion`），未知字段会被拒绝。
- 每个步骤有唯一的 `id`、一个 `sampler` 和一个 `prefab`。目前只有 `grid`（`SamplerGrid`）一种采样器：它在坐标为 `spacing`（默认 1）整数倍的每个格子上放一个点。`count` 为正数时，从这些点中随机保留这么多个；不写时保留全部格点。
- 每个步骤按自己的 `id` 从 `seed` 派生独立的 `random.Stream`，因此增加、删除或调整步骤顺序不会移动其他步骤的点，同一文件和区域在任何机器上都得到相同的点。

```go
p, err := procgen.ParsePlacement(data)
points := p.Points(width, height) // []PlacementPoint{X, Y, ID, Step, Prefab}
```

`Points` 按步骤顺序返回各步骤的点，每个步骤内部先按 Y 再按 X 排序，ID 形如 `trees-0`。生成 prefab 由游戏自己完成。`ParsePlacement` 与 `EncodePlacement` 以规范的 2 空格缩进 YAML 逐字节往返。

## 种子

从本局的种子为每个关卡派生一个具名流，这样重新生成某一层不会改变其他层：

```go
streams := random.NewStreams(runSeed)
cave, _ := procgen.Cave(streams.Stream("cave-1"), caveCfg)
```

生成器会推进传入的流。要重新生成同一个关卡，传入一个相同种子的新流，或在生成前保存流的 `State()`，之后用 `SetState` 恢复。

## 放置管线

版本 1 的 `*.placement.yaml` 使用带种子的网格采样器生成确定性的预制件点位。`ParsePlacement` 会拒绝未知字段、不支持的采样器、重复步骤 ID 与尾随文档。每个步骤按 ID 派生独立随机流，因此增删或重排其他步骤不会移动现有点位。
