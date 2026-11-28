# Agent 评测目录

`tools/agenteval` 用 `tools/agenteval/tasks/` 下带版本的任务 YAML 给编程智能体打分；运行模式、评分细则和运行方法见其 [README](../../tools/agenteval/README.md)。

## 工具覆盖

任务可以在 `checks` 下用 `required_tools` 声明它要覆盖的编辑器和运行时 MCP 工具。名称会与已知目录比对：`game_scenes_list`、`game_layers_list`、`game_entities_list`、`game_entity_inspect`、`game_input_action`、`game_input_pointer`、`scene_edit`、`file_edit`、`datatable_review_propose`、`game_check`、`game_replay_list` 和 `game_replay_play`。未知或重复的名称会使任务加载失败。这个列表是用于审查覆盖范围的元数据，不计分，评测套件目前也不检查智能体是否调用了这些工具。

## 任务

`editor-workflow` 要求做与 `add-menu` 相同的菜单，按同一套评分细则打分；它的提示词要求智能体在编辑前检查场景和图层，通过 `scene_edit` 编辑，并用 `game_check` 校验。

## 重复试验

真实智能体每次运行的结果并不相同，每个任务只跑一次，正常的波动就可能表现为通过或失败。`-trials N`（默认 1）把每个选中的任务运行 N 次，每次都在重新准备的工作区里进行，任何一次试验都看不到其他试验留下的修改或缓存：

```sh
go run ./tools/agenteval -mode=agent -trials 3 -out agent-eval-agent-report.json
```

此后 JSON 报告中的每个任务包含：

- `trials`：运行的试验次数。
- `pass_rate`：达到任务及格分的试验所占比例。
- `mean_score`：各次试验得分的平均值；`score` 是它四舍五入后的值。
- `stddev`：各次得分的总体标准差。

只有每次试验都通过，任务才算通过。任务的 `criteria`、`agent_output` 和 `agent_error` 取自第一次失败的试验，全部通过时取自第一次试验；`duration_ms`、`agent_turns`、`agent_tool_calls` 和各项 token 数是所有试验的总和。试验次数多于一次时，Markdown 摘要会在每个任务的评分明细上方列出这些统计值。
