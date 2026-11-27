### Fixed

- `pkg/sceneformat`, `pkg/animclip`: wired runtime support for the built-in `animator` component's `stateChanged` and `clipEvent` events (`AnimatorStateChangedEvent`, `AnimatorClipEvent`) and `setState` and `trigger` handlers (`AnimatorController`, `AnimatorProvider`, `Signals.BindAnimator`, `BuiltScene.SetAnimator`, `animclip.Connect`) so `sceneformat.Connect` succeeds with the default animator registry and scene connections fire and drive state machines at run time (#1592).
