### Added

- `game/editor`, `game/editor/editormcp`: add a background agent task list (`TasksPanelView`, `AgentTask`, `.nsgo/threads/*.thread.yaml` persistence) with Queue and Run in parallel scheduling, per-object MCP holds (`ConnectTask`, `"waiting on <object>"` auto-resume, separate turn reviews), numbered viewport and scene tree overlays, bulk dismiss without reverting kept edits, urgent-first status bar counts, Dock badge summary counts, and `tasks_list` / `tasks_submit` / `tasks_control` MCP tools (#1022).
