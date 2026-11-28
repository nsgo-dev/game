# Agent evaluation catalog

`tools/agenteval` evaluates versioned task YAML files. A task may list `required_tools` under `checks` to declare the editor, runtime MCP and safety surfaces it exercises. Unknown or duplicate names fail task loading. `editor-workflow` covers scene and layer inspection, scene editing and `game_check` alongside the creation tasks.
