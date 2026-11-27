# 塔防模板

`towerdefense2d` [模板](templates.md)是 Bramble Gate，一个小型 2D 塔防游戏：敌人沿着道路从出生点走向城门，玩家在道路旁的草地上建造防御塔，用敌人掉落的金钱升级，并在准备好时召唤每一波敌人。挡住全部五波即为胜利；让价值十条生命的敌人穿过城门即为失败。它可在桌面、移动端和 Web 上用键盘、手柄或触摸操作，所有美术都由代码绘制。

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -template towerdefense2d mygame
cd mygame
go tool nsgo dev
```

`nsgo init -template github.com/nsgo-dev/game/template/towerdefense2d -module example.com/mygame mygame` 会写出同样的项目。

## 它展示了什么

- 关卡及其波次和敌人、防御塔及其升级、以及投射物，是 `game.yaml` [自动加载](project.md)的三个 YAML 文件，游戏启动时会互相校验。
- 道路是在地图的道路格上用 `ai.AStar`（[ai](ai.md)）找到的最短路径；地图是一个 [tilemap](tilemap.md)，每个格子被标记为道路、可建造或实心。
- 防御塔和敌人的数值保存在 [stats](stats.md) 属性表上：升级是永久的属性效果，冰霜塔的减速是作用在敌人速度上的定时修饰符。
- 防御塔向沿道路走得最远的敌人发射 [bullets](bullets.md) 弹幕模式，并按投射物的飞行时间提前瞄准；迫击炮的命中会溅射，冰霜弹会减速。
- 固定步长[循环](loop.md)以每秒 60 步运行世界，最快速度下每次更新最多三步；[screen](screen.md) 视口把 480×320 的地图适配到任意窗口，上面是原生 HUD。
- `game.yaml` 中的[输入](input.md)映射给每个动作绑定按键，除数字键选塔和中途重开外也都绑定了手柄按钮，并把光标绑定到方向键、十字键和左摇杆；点按战场移动光标，再点一次即建造。
- 无头 `nsgotest` 通关测试分别用键盘、手柄和触摸，按一套方案打到胜利，按另一套打到失败。

## 如何组织

| 文件 | 内容 |
| --- | --- |
| `game.yaml` | 项目文件：窗口、虚拟分辨率、输入映射、自动加载的数据文件、导出预设。 |
| `data/level.yaml` | 关卡：格子大小、初始金钱和生命、地图、敌人种类和波次。 |
| `data/towers.yaml` | 防御塔种类：造价、颜色、属性、升级、各自发射的弹幕模式，以及出售时退还的比例。 |
| `data/projectiles.yaml` | 投射物，一个 [bullets](bullets.md) 弹幕模式文件。 |
| `data.go` | 关卡和防御塔的格式、校验，以及 `LoadData`。 |
| `world.go` | 模拟：道路、出生、行走、索敌、命中、金钱、生命和胜负。 |
| `game.go` | 控制器：循环、输入动作、光标和 HUD 状态。 |
| `view.go` | 状态栏、战场画布、工具栏和结束横幅，以及战场的绘制。 |
| `art.go` | 用代码绘制的地面格子和投射物。 |
| `*_test.go` | 格式、模拟和通关测试。 |

`World` 没有自己的时钟或随机数：相同的调用得到相同的游戏，所以它的测试直接驱动它，而通关测试通过 `nsgotest` 驱动整个应用。

## 数据文件

`data/level.yaml` 和 `data/towers.yaml` 是模板自己的格式，各带一个 `version` 字段；它们拒绝未知字段，写回时内容不变，由 `TestDataFilesAreCanonical` 检查。关卡的 `map` 每行一个字符串，每个字符一个格子：

| 字符 | 格子 |
| --- | --- |
| `.` | 草地，可在上面建塔 |
| `=` | 道路 |
| `S` | 出生点，位于道路上；恰好一个 |
| `G` | 城门，位于道路上；恰好一个 |
| `^` | 岩石 |
| `~` | 水 |

每行长度相同，并且必须有道路连接出生点和城门。敌人有 `health`、`speed` 和可选的 `armor` 属性、击败奖励 `reward`，以及漏过时损失的生命 `leak`。一波由并排出生的若干组组成，每组在 `delay` 之后每隔 `interval` 出生一个敌人，共 `count` 个；清完一波后支付 `bonus`。

防御塔需要 `damage`、`range`（游戏点）和 `cooldown`（秒）属性；`splash` 会伤害命中点该半径内的所有敌人，`slow` 加上 `chill` 会在若干秒内削减被命中敌人一定比例的速度。升级由 `cost` 和按 [stats](stats.md) 写法给出的属性 `modifiers` 组成。防御塔的 `pattern` 指向 `data/projectiles.yaml` 中只发射一次的弹幕模式。

## 在哪里修改

- 在 `data/level.yaml` 中绘制新地图、添加敌人或波次，或重新调整经济。地图以游戏点计的尺寸（`tileSize` 乘以列数、`tileSize` 乘以行数）必须等于 `game.yaml` 中 `display` 的 `width` 和 `height`，所以两处要一起改；尺寸不一致时 `LoadData` 会拒绝加载。
- 在 `data/towers.yaml` 中添加防御塔种类或升级，并在 `data/projectiles.yaml` 中添加它的投射物；HUD 会列出每一种，数字键选择前三种。
- 在 `world.go` 的 `World.fire` 或 `World.hit` 中读取新属性，给防御塔新的行为。
- 用 `sprite.LoadAtlas` 和图片文件替换 `art.go` 中绘制的美术。
- 在 `game.yaml` 的 `input` 部分重新绑定操作。

修改之后，`go tool nsgo test ./...` 会显示文件是否仍能加载、`plan_test.go` 中的 `winningPlan` 是否仍能获胜；修改数据文件后，用 `go run github.com/nsgo-dev/game/cmd/game index` 更新 `game.index.yaml`。
