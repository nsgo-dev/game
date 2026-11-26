### Added

- Editor: multi-file pending agent turns (`StartAgentTurn`, `EndAgentTurn`, `StopAgentTurn`, `KeepAgentTurn`, `RevertAgentTurn`, `UndoRevertAgentTurn`, per-change and per-file revert/restore) with derived structural diffs across scenes, prefabs, data tables, `game.yaml`, and clips, `AuthorAgent` history tagging for single-step multi-file Undo/Redo, Spec Undo toasts, Checkpoints `⌘Z` rules, and the Agent panel summary card (`Main.dc.html`).
- Editor MCP (`editormcp`): `turn_start`, `turn_end`, read-only `turn_status`, and multi-file in-memory `file_edit` tools, plus optional `why` explanations and active-turn multi-scene editing on `scene_edit` and `object_set_field`.
