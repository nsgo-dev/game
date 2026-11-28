# 从 GameMaker 迁移

NSGo 保留 GameMaker 熟悉的快速迭代和数据驱动流程，游戏逻辑使用 Go 编写。

## 项目和房间

使用 `game.yaml` 定义项目、窗口、显示、输入、资源和导出预设。房间对应 `scene` 或 `sceneformat` 文档。场景和资源使用可审查、可合并的 YAML。

## 对象和事件

使用 `sceneformat` 组件与连接描述对象数据和可复用行为。大量对象使用 `pkg/entities` 的系统和查询。`pkg/input` 的命名 action 替代直接检查键盘常量，`game.Loop` 提供固定步长更新和插值绘制。

## 绘制、资源和存档

使用 `screen.View`、`gameui`、`sprite` 和 `tilemap` 构建界面与 2D 内容，`pkg/assets` 管理图集、音频和热重载。`pkg/save` 负责带版本和迁移的 JSON 设置与存档。

