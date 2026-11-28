# 从 RPG Maker 迁移

NSGo 支持数据优先的 RPG 工作流，同时让运行时使用 Go。

## 地图和事件

使用 `tilemap` 与场景文件保存地图、图层和对象。使用 `pkg/flow` 事件文档保存页面、开关、变量、触发器和可恢复命令。`pkg/dialogue`、`pkg/quest` 与 `pkg/inventory` 可共享同一存档状态。

## RPG 数据与工具

使用 `pkg/rpg` 的版本化 YAML 数据库存放角色、职业、技能、敌人和队伍。使用 `pkg/save` 持久化带迁移的存档和设置，使用 `l10n` 与 `text` 处理本地化和字体，发布前运行 `go tool nsgo check`。

