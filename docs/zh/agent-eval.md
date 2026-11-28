# Agent 评测目录

`tools/agenteval` 用 `tools/agenteval/tasks/` 下带版本的任务 YAML 给编程智能体打分；运行模式、评分细则和运行方法见其 [README](../../tools/agenteval/README.md)。

## 工具覆盖

任务可以在 `checks` 下用 `required_tools` 声明它要覆盖的编辑器和运行时 MCP 工具。名称会与已知目录比对：`game_scenes_list`、`game_layers_list`、`game_entities_list`、`game_entity_inspect`、`game_input_action`、`game_input_pointer`、`scene_edit`、`file_edit`、`datatable_review_propose`、`game_check`、`game_replay_list` 和 `game_replay_play`。未知或重复的名称会使任务加载失败。这个列表是用于审查覆盖范围的元数据，不计分，评测套件目前也不检查智能体是否调用了这些工具。

## 任务

`editor-workflow` 要求做与 `add-menu` 相同的菜单，按同一套评分细则打分；它的提示词要求智能体在编辑前检查场景和图层，通过 `scene_edit` 编辑，并用 `game_check` 校验。

## 试验

运行 `go run ./tools/agenteval -mode=reference -trials 3`，即可在独立工作区重复每个任务。JSON 报告会为每个任务包含 `trials`、`pass_rate`、`mean_score` 和 `stddev`。

报告还会记录暂存文档与技能的确定性 `context_hash` 以及 `commit_sha`。向 `-compare report.json` 传入上一份 JSON 报告后，会打印通过状态发生变化的评分标准。模式或模型不同时会忽略比较，避免模型升级被误报为回归。定时工作流仍负责历史发布与保留期限。
