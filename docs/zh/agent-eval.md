# Agent 评测目录

`tools/agenteval` 用 `tools/agenteval/tasks/` 下带版本的任务 YAML 给编程智能体打分；运行模式、评分细则和运行方法见其 [README](../../tools/agenteval/README.md)。

## 工具覆盖

任务可以在 `checks` 下用 `required_tools` 声明它要覆盖的编辑器和运行时 MCP 工具。名称会与已知目录比对：`game_scenes_list`、`game_layers_list`、`game_entities_list`、`game_entity_inspect`、`game_input_action`、`game_input_pointer`、`scene_edit`、`file_edit`、`datatable_review_propose`、`game_check`、`game_replay_list` 和 `game_replay_play`。未知或重复的名称会使任务加载失败。这个列表是用于审查覆盖范围的元数据，不计分，评测套件目前也不检查智能体是否调用了这些工具。

## 任务

`editor-workflow` 要求做与 `add-menu` 相同的菜单，按同一套评分细则打分；它的提示词要求智能体在编辑前检查场景和图层，通过 `scene_edit` 编辑，并用 `game_check` 校验。

任务元数据可以声明一个任务族（`create`、`editor`、`debug`、`3d`、`data`、`code` 或 `safety`）及其所需的 MCP 服务器。加载器会拒绝未知族、空白或含空格的服务器名称以及重复项，确保覆盖报告可审查。
