### Added

- Sprite state machines in plain-text `*.states.yaml` files (`animclip.StateMachine`, `animclip.ParseStateMachine`, `animclip.EncodeStateMachine`, `animclip.NewAnimator`, `animclip.InputDriver`, `animclip.SimulateStateMachine`) with published JSON Schema `states.v1.json`, built-in `animator` scene component (`sceneformat.AnimatorComponent`), `refs` path tracking, and `game check` validation with file and line diagnostics (#1054)
- Editor State Machine graph panel with state node cards, sprite thumbnails, directed transition arrows, `Any state` interrupts, live active-state highlighting, **Drive with keys** preview through project input actions, `LEAVING <STATE>` transition inspection, and editor MCP tools `states_write` and `states_test` (#1054)
