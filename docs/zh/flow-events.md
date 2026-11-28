# YAML 地图事件

`flow.ParseMapEvent` 校验带版本的地图事件 YAML，支持按顺序选择页面以及 `action`、`touch`、`autorun`、`parallel` 触发器。`EventRunner` 在固定步中执行 `say`、`wait`、`set` 和 `self` 命令，`EventSnapshot`/`Restore` 可以将中途事件保存为文本。未知字段和命令会在加载时失败。
