# rts2d 模板

`rts2d` 是一场 2D 即时战略遭遇战，名为 Shardfront：一张地图上的两座基地、可采的水晶、可建的兵营、三种单位、战争迷雾、框选、编队，以及一个电脑对手。总部先被摧毁的一方落败；双方总部在同一步内被摧毁则为平局。它小到一个下午就能读完，结构也按策略游戏的成长方式组织：规则、地图和对手的优先级都是 YAML 数据文件，模拟层对视图和输入一无所知，玩家和对手通过同一组函数下达命令。

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -template rts2d mygame
cd mygame
go tool nsgo dev
```

也可以用 NSGo 自带的命令：`nsgo init -template github.com/nsgo-dev/game/template/rts2d -module example.com/mygame mygame`（见[项目模板](templates.md)）。

## 它展示了什么

- [rts](rts.md)：一个 `rts.Crowd` 让双方所有单位沿共享的流场在地图网格上移动；`rts.BoxSelector` 和 `rts.Selection` 把点击、拖动、轻触和手柄光标变成选择；`rts.ControlGroups` 保存十个编号编队；成组下令的单位按 `data/formations.yaml` 定义的阵型站位。
- [fog](fog.md)：每一方从自己的单位和建筑看出去，岩石遮挡视线，对手的基地在侦察到之前是隐藏的，探索过的地面保持变暗显示。不在视野内的敌方建筑按最后一次看到时的样子显示，之后新建的建筑在被看到之前保持隐藏。
- [ai](ai.md)：对手每秒用由 `data/opponent.yaml` 构建的效用推理器为各个行动（训练工人、建造兵营、训练士兵或游侠、进攻、防守）打分，并执行得分最高的一个。它和玩家一样只能看到己方战争迷雾之外的东西，但了解地图，因此知道玩家总部的位置。
- [input](input.md) 与[项目文件](project.md)的输入映射：按键和手柄按钮是 `game.yaml` 中的具名动作，手柄摇杆和方向键驱动同一个光标，鼠标和手指则直接作用于战场视图。
- 每秒 60 步的固定步长[游戏循环](loop.md)，通过 `game.yaml` 的 `display` 所设的 [screen](screen.md) 视口在 `core.Canvas` 上用代码绘制的战场，以及用于 HUD 和命令栏的原生 NSGo 按钮。
- 无头测试：通过键盘和鼠标把整场遭遇战打到胜利和失败，测试手柄和触屏操作，并检查每个数据文件都是规范形式。

## 操作

| | 键盘与鼠标 | 手柄 | 触屏 |
| --- | --- | --- | --- |
| 选择 | 点击单位；拖出框；双击选中所有同类单位；Shift 追加，Ctrl 或 Command 切换 | 左摇杆或十字键移动光标；South 选取，按住并移动可拖出框 | 轻触单位；拖出框 |
| 命令 | 右键点击敌人进行攻击，点击水晶进行采集，点击地面前往该处并沿途交战 | 在光标处按 East | 选中单位后轻触 |
| 编队 | Ctrl 或 Command 加数字键绑定，Shift 加数字键追加，数字键召回，连按两次高亮 | 仅限键盘 | 仅限键盘 |
| 训练工人、士兵、游侠 | Q、W、E | West、North、右肩键 | 命令栏 |
| 建造兵营 | B | 左肩键 | 命令栏 |
| 选中军队、工人 | A、S | 左扳机、右扳机 | 命令栏 |
| 清除选择 | Escape | Back | 清除按钮 |
| 暂停、再来一局 | P；结算画面按 Enter | Start | 暂停按钮 |

方向键、Space 和 Enter 也驱动手柄的光标，因此只用键盘也能玩。兵营建在己方基地的空闲建造台上，工人训练出来后会自动采集。闲置的战斗单位会攻击它们看到的敌人，所以留在家里的单位会守卫基地。

## 结构

| 文件 | 内容 |
| --- | --- |
| `game.yaml` | 项目文件：窗口、480×264 的虚拟分辨率、输入映射和导出预设。 |
| `data/rules.yaml` | 单位、建筑与经济：生命值、速度、伤害、射程、视野、价格、训练与建造时间、初始单位、队列与单位上限。 |
| `data/map.yaml` | 用字符行表示的战场：`.` 地面，`#` 岩石，`*` 水晶，`A` 和 `B` 两座总部，`a` 和 `b` 建造台。 |
| `data/formations.yaml` | 成组下令的单位所用的阵型，采用 rts 的格式。 |
| `data/opponent.yaml` | 对手的行动以及为其打分的考量，采用 ai 的效用格式。 |
| `data.go` | 读取并检查数据文件。 |
| `world.go`、`orders.go` | 模拟层，以及双方下达的命令。 |
| `opponent.go` | 电脑玩家。 |
| `control.go` | 玩家的选择、编队、光标，以及鼠标、触屏、键盘和手柄的处理。 |
| `view.go`、`draw.go` | `Root`、HUD、命令栏、暂停与结算画面，以及绘制。 |

`data/rules.yaml` 和 `data/map.yaml` 是模板自己的格式：每个都有 `version`，拒绝未知字段，并写回与读入时相同的字节；`TestDataFilesAreCanonical` 会检查它们，以及另外两个采用各包格式的文件。

## 在哪里修改

- 平衡性：`data/rules.yaml`。对手的优先级和它发起进攻所需的兵力：`data/opponent.yaml`。通关测试会真实地打完遭遇战，因此较大的改动可能需要调整 `playthrough_test.go` 中的策略。
- 地图：`data/map.yaml`，保留一个 `A` 和一个 `B`，让每座建筑的占地都是地面，且任意两块占地不共享格子。
- 新的单位类型：操作、命令栏和对手按名字训练 `worker`、`soldier` 和 `ranger`，所以 `data/rules.yaml` 必须保留这三种（否则加载失败）。要新增一种，先加入 `data/rules.yaml`，再在 `drawUnit` 中给它一个形状，在 `game.yaml` 中给它一个按键和手柄按钮并在 `readActions` 中读取，在 `commandBar` 中加一个按钮。
- 单纯移动，用于撤退：地面命令是攻击移动，单位会沿途交战。给 `command` 加一个修饰键或第二个按钮，改为以 `orderMove` 调用 `moveGroup`，单位在抵达前会无视敌人。
- 美术：`draw.go` 用代码绘制一切；每次把一类东西换成 [sprite](sprite.md) 的精灵即可。
- 比屏幕更大的地图：在 `tilemap.Camera` 下绘制，并把镜头对准连按两次召回的编队，也就是现在 `focus` 高亮的位置。
- 声音、标题画面、存档和回放：世界按确定性的方式步进，所以 [sound](sound.md)、[scene](scene.md)、[save](save.md) 和 [replay](replay.md) 可以围绕 `skirmish.update` 接入，而无需改动模拟层。
