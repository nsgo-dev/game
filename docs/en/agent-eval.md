# Agent evaluation catalog

`tools/agenteval` grades coding agents on versioned task YAML files under `tools/agenteval/tasks/`; its [README](../../tools/agenteval/README.md) describes the modes, the rubric and how to run it.

## Tool coverage

A task may list `required_tools` under `checks` to declare the editor and runtime MCP tools it is meant to exercise. The names are checked against a known catalog: `game_scenes_list`, `game_layers_list`, `game_entities_list`, `game_entity_inspect`, `game_input_action`, `game_input_pointer`, `scene_edit`, `file_edit`, `datatable_review_propose`, `game_check`, `game_replay_list` and `game_replay_play`. An unknown or duplicate name fails task loading. The list is metadata for reviewing coverage; it is not scored, and the suite does not yet check that an agent called the tools.

## Tasks

`editor-workflow` asks for the same menu as `add-menu` and is graded by the same rubric, with a prompt that directs the agent to inspect scenes and layers before editing, edit through `scene_edit` and validate with `game_check`.
