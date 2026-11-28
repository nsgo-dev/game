# 从 RPG Maker 迁移

把 RPG Maker 的数据库、地图、事件、消息和插件逐一对应到这个游戏层，给出讲解每一项的页面，并列出这里还没有的部分。

最大的区别在于这里没有内置的 RPG。RPG Maker 在你写下第一行代码之前就提供了数据库、地图编辑器、事件解释器和战斗系统；在这里，游戏是 NSGo 组件里的 Go 代码，它读取的数据是 YAML，可以手写，也可以由[编辑器](editor.md)保存，下面这些包是由你组合起来的部件。[rpg2d 模板](template-rpg2d.md)把它们组合成了一个能运行的小型 RPG，所以从它开始最合适。

## 速览

| RPG Maker | 这里 | 指南 |
| --- | --- | --- |
| 地图、图块组、自动图块 | 通过 `tilemap.LoadTiled` 读取 Tiled 地图，用 `SetTerrain` 绘制地形 | [tilemap](tilemap.md) |
| 场所移动（Transfer Player） | 带过渡效果的 `scene.Manager.Switch` | [scene](scene.md) |
| 数据库：角色与敌人的能力值、状态 | 带加、乘和覆盖修正值的 `stats` 属性表，在固定步长上计时 | [stats](stats.md) |
| 数据库：物品、武器、防具 | `inventory` 的物品定义、可堆叠容器和类型化装备槽 | [inventory](inventory.md) |
| 数据库：技能 | 带消耗、冷却、充能和施法时间的 `ability` 技能集 | [ability](ability.md) |
| 敌人掉落物品 | `loot` 加权掉落表 | [loot](loot.md) |
| 任务（RPG Maker 中需要插件） | 带目标、前置条件和奖励的 `quest` 任务册 | [quest](quest.md) |
| 显示文字、显示选项、消息中的变量 | 运行 Yarn Spinner 脚本、带变量和条件的 `dialogue` | [dialogue](dialogue.md) |
| 消息中的脸图和图片 | `vn` 的角色立绘、名牌和打字机文本框 | [vn](vn.md) |
| 播放 BGM、播放 SE | [sound](sound.md) | [sound](sound.md) |
| 播放影像 | `cutscene` 视频过场 | [cutscene](cutscene.md) |
| 存档与读档 | 带迁移的版本化 JSON `save` 存档槽 | [save](save.md) |
| 插件 | 编译进游戏的 `gameplugin` 插件 | [插件](plugins.md) |
| 部署 | `go tool game release` | [构建与发布](shipping.md) |

## 这里还没有的部分

- **数据库：角色、职业、敌人和敌群**，作为带等级曲线和经验值的单一版本化 YAML 数据库：由 [#1756](https://github.com/nsgo-dev/game/issues/1756) 跟踪。
- **地图事件**，包括事件页、条件、触发方式（确定键、接触、自动执行、并行处理）、开关、独立开关、变量，以及由可恢复解释器运行的指令列表：由 [#1755](https://github.com/nsgo-dev/game/issues/1755) 跟踪。在此之前，地图对象上的 `trigger2d` 组件配合场景的 `connections`、Go 处理函数和 `dialogue` 变量即可实现接触与确定键事件（[编辑器与场景格式](editor.md#连接490)）。
- **战斗**：带我方队伍与敌群、行动顺序、技能范围与公式的确定性回合制战斗运行器由 [#1757](https://github.com/nsgo-dev/game/issues/1757) 跟踪。rpg2d 模板自带一个用 Go 写的战斗场景，可以从它起步。

## 第一个项目

用 RPG 模板创建项目：`go run github.com/nsgo-dev/game/cmd/game@latest new -template rpg2d mygame`，其中有可以行走的小镇和草地、带分支对话的人物、带装备的背包、一个任务、回合制战斗和三个存档槽（[指南](template-rpg2d.md)）。用 `go tool game editor` 打开它：它的 2D 视口代替地图编辑器，它保存的文件就是游戏加载的那份 YAML。
