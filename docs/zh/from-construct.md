# 从 Construct 迁移

Construct 的事件表、布局和行为在 NSGo 中对应显式 Go 系统和文本场景文件。

## 布局、事件和行为

场景与 prefab 保存布局。使用 `sceneformat` 连接和规则描述声明式条件与动作，使用 `pkg/flow` 保存对话、地图事件和可恢复命令。需要类型安全或高性能的模拟逻辑放在 Go 系统中。

## 输入和导出

使用 `sceneformat` 的运动行为处理常见移动，使用 `pkg/input.Actions` 统一键盘、触摸、鼠标和手柄输入。`game.Loop` 提供确定性的固定步长，导出预设写在 `game.yaml`，发布前运行 `go tool nsgo check`。

