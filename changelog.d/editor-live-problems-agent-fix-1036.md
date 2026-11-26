### Added

- `game/tooling/assetcheck`: `Problem` now carries 1-based `Column` where the scene or YAML format knows it, plus `Location()` formatting `file:line:column` (#1036).
- `game/editor`: live debounced project checking inspects open unsaved documents on every edit, draws Spec Problem overlays on live objects in the 2D viewport, marks offending scene tree rows with a problem badge, supports `IgnoreProblem` (re-surfacing automatically when the file is edited again), and adds the Agent problem fix panel (`ProposeProblemFix`, `ApplyProposedFix` as one undoable step with Undo toast, `ShowProblem`, and `AskAboutProblem`) (#1036).
- `game/editor/editormcp`: `game_check` checks open unsaved documents and reports 1-based columns, and `problem_propose_fix` lets an agent propose a validated fix with explanation and diff without ever applying it (#1036).
