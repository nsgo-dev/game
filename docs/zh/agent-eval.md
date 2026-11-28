# Agent 评测目录

`tools/agenteval` 运行带版本的任务 YAML。任务可以在 `checks` 下用 `required_tools` 声明覆盖的编辑器、运行时 MCP 和安全面。未知或重复名称会使任务加载失败。`editor-workflow` 与创建任务一起覆盖场景和图层检查、场景编辑以及 `game_check`。
