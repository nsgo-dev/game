# 放置游戏模板

`idle` 是 Hearthkeeper，一个小型放置游戏：守着一堆火，用它产出的余烬购买生产器和升级，游戏关闭期间照样有进度，可以转生重置，买下目标即获胜。用它创建项目：

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -template idle mygame
```

或用 NSGo 自带的命令：`nsgo init -template github.com/nsgo-dev/game/template/idle -module example.com/mygame mygame`。其他模板见[项目模板](templates.md)。

## 演示了什么

- **大数。** 所有数量都是 [`idle.Number`](idle.md)，价格和产量超出 float64 的范围也照常运算；HUD 用 `idle.Format` 以短格式（1.23M）、字母、科学或工程记数法显示，玩家可以切换，存档会记住所选的记数法。
- **离线进度。** 游戏启动或从后台回来时，用 `idle.Offline` 结算自上次在线以来的时间：至少一分钟，最多八小时，按一半速率计算。离开的时间按一秒一步（`idle.Away.Steps`）用和在线时相同的规则重放，所以离开期间没有燃料的火同样会熄灭；把时钟往回调则什么也不结算。
- **生产器、升级和转生。** 生产器的价格是 `Cost × Growth^已拥有数量`（`idle.Number.Pow`）；升级可以按倍数提高某一个生产器、全部生产器或一次添柴的收益，或者为火持续添加燃料；撒灰会重开本局，获得 `⌊√(本局累计 / 阈值)⌋` 灰烬，每份灰烬永久 +10%。
- **胜负。** 买下 Everflame 即获胜，之后可以继续玩。燃料耗尽（无论在线还是离开期间）则本局失败；重新点火会开始新的一局，灰烬保留。卡片显示时火不会燃烧；卡片会随游戏状态变化，所以离开期间火熄灭时，胜利卡片会变成失败卡片。
- **存档。** [`save.Store`](save.md) 在应用存储中保存带版本号的 JSON 存档和一份备份。生产器和升级按 id 保存，数值存为文本；游戏无法使用的存档会被拒绝，而不会被覆盖。
- **一套商店适配所有输入。** 商店是画在画布上的 [`gameui`](gameui.md) 控件树：触摸和鼠标直接按按钮，键盘和手柄通过 [`game.yaml`](project.md) 绑定的动作，借助 `SpatialNavigator` 在按钮间移动焦点。

| | 键盘 | 手柄 | 触摸和鼠标 |
| --- | --- | --- | --- |
| 添柴 | 空格 | West | 点击炉火 |
| 移动焦点 | 方向键、W A S D | 十字键 | |
| 按下有焦点的控件 | 回车 | South | 点击控件 |
| 关闭卡片 | 回车、Esc | South、East | 卡片上的按钮 |
| 切换记数法 | N | North | 记数法按钮 |

## 结构

| 文件 | 内容 |
| --- | --- |
| `hearth.yaml` | 数值平衡：燃料、生产器、升级、转生、目标和离线规则，写在带版本号的 YAML 文件中，拒绝未知字段，游戏启动时校验。 |
| `data.go` | 读取、校验并以确定的格式写出 `hearth.yaml`。 |
| `sim.go` | 游戏规则，不含视图也不读时钟，因此循环、离线重放和测试运行的是同一份代码。 |
| `save.go` | 存档槽和记数法选择。 |
| `game.go` | NSGo 一侧：固定步长循环、输入动作、自动存档、进入后台与返回，以及原生 HUD 和卡片。 |
| `shop.go` | `gameui` 商店和绘制的炉火。 |
| `game.yaml`、`project.go` | 项目文件，包括虚拟分辨率和输入映射。 |
| `sim_test.go`、`data_test.go`、`game_test.go`、`project_test.go` | 规则与数值平衡、数据文件（含模糊测试）、用触摸玩到胜利、用鼠标、键盘和手柄玩到失败、以及每种输入都能玩的流程测试，以及项目文件。 |

## 在哪里修改

- **数值。** 编辑 `hearth.yaml`；新的生产器或升级无需代码就会出现在商店里。`TestBalanceReachesTheGoal` 用一个简单策略玩这个游戏，目标来得太早或太晚都会失败，因此它会告诉你一次修改对节奏的影响。
- **新的升级类型。** 在 `data.go` 中加一个效果常量及其校验规则，并在 `sim.go` 中应用它。
- **要保存的新状态。** 在 `savedGame` 中加字段，提高 `saveVersion`，并为旧存档注册 `save.WithMigration`。
- **离线规则。** `hearth.yaml` 中的 `offline` 设置最短时间、上限、速率和重放步长。
- **外观。** `shop.go` 中的 `drawFlame` 绘制炉火，`newShop` 设置 `gameui` 主题。
