# cards2d 模板

`cards2d` 是 Suit Ladders，一个原创设计的单人纸牌游戏。每种花色有一座从 1 爬到 9 的梯子。玩家手里有五张牌，从抽牌堆抽牌，把正好是某座梯子下一级的牌放上它所属花色的梯子，其余的牌丢到弃牌堆；每局可以把弃牌堆翻过来当作新的抽牌堆两次。所有梯子都到 9 时获胜；抽牌堆空了、不能再翻牌，且手里没有能放上梯子的牌时失败。

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -template cards2d mycards
cd mycards
go tool nsgo dev
go tool game playtest
```

`nsgo init -template github.com/nsgo-dev/game/template/cards2d` 写出同样的项目，见[项目模板](templates.md)。

## 演示了什么

- [`game/cards`](cards.md)：从 `board.yaml` 加载的 `Board`，用鼠标或手指拖动卡牌，规则允许的地方放下，其余的飞回原处；棋盘的 `Accept` 是每一次放下、每一次把手牌接上梯子或扔掉都要经过的规则，无论来自按键、按钮、点按还是拖动；摸牌和把弃牌堆翻过来则经过 `canDraw`。
- [`game/random`](determinism.md)：第 *n* 局用以 *n* 为种子的 `random.Stream` 洗牌，所以每位玩家的第 *n* 局都是同一局，测试也能指明它们赢下和输掉的是哪几局。
- [`game/save`](save.md)：进行中的牌局和胜负记录在每一步之后存进 `core.Storage`；读取时会检查，损坏的或不可能出现的存档会重新发一局，由更新版本的游戏写下的存档则原样保留。
- [`game/project`](project.md) 和 [`game/input`](input.md)：`game.yaml` 的输入映射把按键和手柄按钮绑定到动作 `left`、`right`、`play`、`discard`、`draw` 和 `deal`，`Project.BindInput` 把它读进 `input.Actions`。
- [`game/screen`](screen.md)：牌桌在 360×640 点的虚拟分辨率中布局，缩放到任意窗口，在手机上请求竖屏方向（`cx.RequestOrientation(core.OrientationPortrait)`），指针通过同一个视口映射回来。
- 固定步长的 `game.Loop`、在 `core.Canvas` 上用代码绘制的卡牌，以及显示计数和按钮的原生视图。

## 操作

| | 键盘 | 手柄 | 触摸和鼠标 |
| --- | --- | --- | --- |
| 选择一张手牌 | 左、右（A、D） | 十字键 | 点按一张放不上去的牌 |
| 把它放上梯子 | Enter 或上 | South | 点按它，或把它拖到它的梯子上 |
| 把它丢掉 | X 或下 | West | 把它拖到弃牌堆 |
| 抽牌，或把弃牌堆翻过来 | 空格 | North | 点按抽牌堆、把它顶上的牌拖进手牌，或按 Draw |
| 重新发牌 | N | Start | New deal；牌局结束后按 Next deal |

牌局结束前重新发牌算作一次失败。

## 如何组织

| 文件 | 内容 |
| --- | --- |
| `game.yaml` | 项目文件：窗口、360×640 点的显示区域、输入映射和导出预设。 |
| `rules.yaml`、`rules.go` | 牌组：花色及其颜色、最大点数和翻牌次数；读取时拒绝未知字段，并检查往返读写不变。 |
| `board.yaml` | 以 `cards.BoardDoc` 表示的牌桌：每种花色一座名为 `ladder-<花色>`、只接受该花色的梯子，抽牌堆、弃牌堆，以及带容量的手牌。 |
| `table.go` | 棋盘上的规则：发牌、抽牌、上梯、丢牌、翻牌，以及判定胜负。 |
| `save.go` | 存档槽 `records` 和 `table`，以及恢复存档前要通过的检查。 |
| `game.go` | `Root`、循环和输入映射的动作。 |
| `render.go`、`hud.go` | 卡牌图案和通过视口绘制的牌桌，以及叠在上面的原生视图。 |
| `playtest.yaml`、`playtest_test.go` | [`card-ai`](playtest.md) 对局锦标赛自动化试玩配置与由 `go tool game playtest` 驱动的 `TestPlaytest` 适配器。 |
| `*_test.go` | 用键盘、手柄和触摸打到一胜一负的完整对局，规则、存档和数据文件的测试，`rules.yaml` 的模糊测试，以及整局游戏的基准测试。 |

## 在哪里修改

- 牌组和难度在 `rules.yaml`。当测试所用的谨慎玩法在 200 局中赢下不到十分之一或超过十分之九时，`TestSomeDealsAreWon` 会失败；`table_test.go` 中的 `winDeal` 和 `loseDeal` 是完整对局测试赢下和输掉的牌局，改动规则后要重新挑选。
- 牌桌布局在 `board.yaml`，使用显示区域的坐标。缺少规则所需槽位的牌桌会被 `newTable` 拒绝。
- 走法在 `table.go`：`allows` 决定哪张牌可以去哪里，`canDraw` 决定何时可以摸牌或把弃牌堆翻成新的摸牌堆，`judge` 决定牌局何时胜负已分。
- 操作在 `game.yaml` 的输入映射中，用 `go run github.com/nsgo-dev/game/cmd/game project check` 校验。
- 卡牌图案是 `render.go` 中的 `drawCard` 和 `drawSuit`。

## 局限

完整对局测试用 nsgotest 的键盘、手柄和触摸，按照读取牌桌的策略来操作，而不是录制真人玩家的输入；需要录制和回放真实输入时，用 [`game/replay`](replay.md)。
