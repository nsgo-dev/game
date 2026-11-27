# rpg2d 模板

`rpg2d` 是一个名叫 Hollowbrook 的小型俯视角 RPG：一座小镇和镇门外的草地、三个可以交谈的人物、装着物品与装备的背包、一个任务、回合制战斗和三个存档槽。它是[模板](templates.md)之一，也是这类游戏的起点。

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -template rpg2d mygame
cd mygame
go tool nsgo dev
go tool game playtest
```

`nsgo init -template github.com/nsgo-dev/game/template/rpg2d -module example.com/mygame mygame` 会写出同样的项目。

## 它展示了什么

Hollowbrook 神龛里的灯熄灭了。治疗师 Mira 需要从 Whisper Meadow 采来三朵发光菇（glowcap）才能把它重新点亮，而一头名叫 Thornhide 的老荆棘野猪让采集的人不敢靠近。起始的木棍打不过 Thornhide：玩家要从苔藓史莱姆身上赚够买 Bram 铁剑的金币，在草地里找到皮背心，装备好两者，赶走 Thornhide，再把发光菇带回去就能获胜。战斗中 HP 耗尽则游戏结束，可以读取存档或开始新游戏。

| 类型要素 | 模板的做法 | 包 |
| --- | --- | --- |
| 地图 | `data/world.yaml` 中用字符行画出的两张地图，构建成标记了实心碰撞图块的瓦片地图，镜头跟随玩家 | [tilemap](tilemap.md) |
| 行走 | 固定步长循环上逐格移动，地图之间的出口，走上去即可拾取的物品，以及巡逻的敌人 | [loop](loop.md) |
| 对话 | Yarn Spinner 脚本：带分支选项、根据玩家所持物品的条件，以及给予和收回物品、开始任务、治疗的命令，配合立绘和打字机效果播放 | [vn](vn.md)、[dialogue](dialogue.md) |
| 物品与装备 | 由格子组成的背包，以及武器、护甲和护符槽，其加成来自具名效果 | [inventory](inventory.md)、[stats](stats.md) |
| 任务 | 一个任务，目标为采集、击败和回报，完成时发放奖励 | [quest](quest.md) |
| 战斗 | 回合制战斗，可以攻击、喝药和逃跑，首领战无法逃跑 | |
| 存档 | 应用存储中的三个槽位，保存背包、装备、任务、对话状态和所在地图 | [save](save.md) |
| 画面 | 标题、地图、战斗、失败和胜利场景，之间以淡入淡出切换 | [scene](scene.md) |

键盘和手柄操作绑定在 `game.yaml` 的输入映射里：键盘上的方向键、WASD、Enter、Escape 和 Tab，手柄上的十字键、左摇杆、South、East 和 Start。屏幕触控控件由 `input.TouchControls` 从 `controls.touch.yaml` 读取：左下角驱动 `moveX`/`moveY` 的浮动摇杆，以及右下角的 A（确认）和 B（取消）按钮，以视图坐标布局在安全区内。它们在手机和平板上一开始就显示，在桌面上默认隐藏直到屏幕被触摸，再次使用键盘或手柄时会重新隐藏。菜单是原生按钮，点一下即可选中条目，键盘和手柄则移动其上的选中框。根作用域为 480×270 的虚拟分辨率在手机上请求横屏方向（`cx.RequestOrientation(core.OrientationLandscape)`）。

测试通过应用完整地玩这个游戏：用键盘玩到胜利画面，用手柄和屏幕触控控件玩到失败画面，途中还会存档和读档。

## 它的组织方式

| 文件 | 内容 |
| --- | --- |
| `game.yaml` | 项目文件：窗口、480×270 的虚拟分辨率、输入映射和导出预设 |
| `controls.touch.yaml` | 屏幕触控布局：驱动 `moveX`/`moveY` 的浮动摇杆以及 A、B 按钮，由 `input.TouchControls` 读取 |
| `data/world.yaml` | 图块、敌人的属性、新游戏的起点，以及每张地图的字符行、出口、人物、带巡逻路线的敌人和物品。格式定义在 `world.go` 中：带版本号，拒绝未知字段，读入再写回不会有任何改变 |
| `data/items.yaml`、`data/stats.yaml`、`data/quests.yaml` | 物品目录、玩家属性与装备效果，以及任务书 |
| `data/cast.yaml`、`data/dialogue.yarn` | 人物的名字、颜色和立绘，以及他们说的话 |
| `data.go` | 读入数据文件并检查它们彼此一致：它们提到的每个物品、敌人、人物、对话节点、效果和图片都存在 |
| `game.go`、`menu.go`、`save.go` | 规则、菜单和存档槽，不涉及绘制和输入设备，测试可以直接驱动 |
| `input.go`、`draw.go`、`art.go`、`ui.go` | 把输入映射读成一步的意图、各画面的绘制、启动时绘出的像素画，以及带 HUD 的应用 |
| `playtest.yaml`、`playtest_test.go` | [`topdown-explorer`](playtest.md) 自动化试玩配置与由 `go tool game playtest` 驱动的 `TestPlaytest` 适配器 |

无法读入的数据文件会在窗口中显示错误而不是游戏，方便在 `go tool nsgo dev` 下修正。

## 在哪里修改

- **地图**：在 `data/world.yaml` 的 `maps` 中加一张，并从另一张地图加一个通往它的出口。新的图块或敌人种类放进 `tiles` 或 `foes`，图片写进 `art.go`。
- **人物**：加到某张地图的 `people` 中，在 `data/cast.yaml` 中加上其立绘，并在 `data/dialogue.yarn` 中给他一个节点。
- **物品**：加到 `data/items.yaml`。装备还需要在 `data/stats.yaml` 中有一个同名效果。
- **任务**：加到 `data/quests.yaml`，并在对话中用 `<<start id>>` 开始它。`data.go` 中的 `mainQuest` 是完成即获胜的任务。
- **平衡**：敌人的属性在 `data/world.yaml` 中，玩家的在 `data/stats.yaml` 中。`TestThornhideNeedsTheIronSword` 检查整个游戏围绕的那场战斗。
- **操作**：键盘和手柄编辑 `game.yaml` 中的输入映射，屏幕触控控件编辑 `controls.touch.yaml`。
- **美术**：把 `art.go` 中的 `newAtlas` 换成 `sprite.LoadAtlas`，保留数据文件使用的帧名。
