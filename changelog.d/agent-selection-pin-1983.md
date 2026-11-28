### Changed

- `pkg/editor`: Agent panel requests, background tasks and automation runs pin the objects selected in the open scene when their turn starts, as the `⌘L` composer does, so an id-less `object_set_field`, `object_duplicate`, `object_group`, `object_move_under` or `object_visibility` call in those turns edits them even after the selection changes, and `agent_turn_get` and the turn's selection chip show them. A task whose scene is not the open one pins no selection (#1983).
