# Agent evaluation catalog

`tools/agenteval` grades coding agents on versioned task YAML files under `tools/agenteval/tasks/`; its [README](../../tools/agenteval/README.md) describes the modes, the rubric and how to run it.

## Tool coverage

A task may list `required_tools` under `checks` to declare the editor and runtime MCP tools it is meant to exercise. The names are checked against a known catalog: `game_scenes_list`, `game_layers_list`, `game_entities_list`, `game_entity_inspect`, `game_input_action`, `game_input_pointer`, `scene_edit`, `file_edit`, `datatable_review_propose`, `game_check`, `game_replay_list` and `game_replay_play`. An unknown or duplicate name fails task loading. The list is metadata for reviewing coverage; it is not scored, and the suite does not yet check that an agent called the tools.

## Tasks

`editor-workflow` asks for the same menu as `add-menu` and is graded by the same rubric, with a prompt that directs the agent to inspect scenes and layers before editing, edit through `scene_edit` and validate with `game_check`.

## Repeated trials

A live agent does not give the same result on every run, so one run per task can turn normal variance into a pass or a failure. `-trials N` (default 1) runs every selected task N times, each in a freshly staged workspace, so no trial sees another's edits or caches:

```sh
go run ./tools/agenteval -mode=agent -trials 3 -out agent-eval-agent-report.json
```

Each task in the JSON report then carries:

- `trials`: the number of trials run.
- `pass_rate`: the fraction of trials that reached the task's passing score.
- `mean_score`: the mean score over the trials; `score` is this value rounded.
- `stddev`: the population standard deviation of the scores.

A task passes only when every trial passes. Its `criteria`, `agent_output` and `agent_error` come from the first failing trial, or from the first trial when all pass, and `duration_ms`, `agent_turns`, `agent_tool_calls` and the token counts are summed over every trial. With more than one trial the Markdown summary prints these statistics above each task's criterion breakdown.
