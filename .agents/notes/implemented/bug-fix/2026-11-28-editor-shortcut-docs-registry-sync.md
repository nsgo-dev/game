# Agent Note: Keep editor shortcut docs synced with Editor.Commands and clarify CmdProblemsPanel

Status: implemented

## Problem

The shortcut and menu reference in `docs/en/editor.md` and `docs/zh/editor.md` was maintained by hand and drifted from `Editor.Commands` and `Editor.ApplicationMenu` (`nsgo-dev/game#1612`):

- **2D `e` / `r` mismatch**: `docs/*/editor.md` listed `e` as Erase Tile in 2D and omitted `e` (Rotate), `r` (Scale), `w` (Move), and `space` (Pan). In `pkg/editor/shortcuts.go`, `ScopeViewport` scores 3 in every view while `ScopeTile` scores 4 only when a tile tool is already active (`isTileTool(e.Tool)`) and 1 otherwise, so pressing `e` from Select in 2D activates Rotate, not Erase Tile.
- **Missing `Help` menu and unlinked `mod+n` exception**: the menu bar list omitted `Help` (`Replay tips`, `Keyboard Shortcuts`), and `File > New Project...` (`mod+n`) did not cross-reference `ScopeAgent`'s `CmdNewAgentThread` override when the right-rail Agent tab is active.
- **Mislabelled `CmdProblemsPanel`**: `CmdProblemsPanel` was labelled `"Problems"` (`"Toggle the Problems panel"`), even though it toggles the right-rail Agent problem-fix panel (`SetProblemAgentPanelOpen`) rather than the bottom `PanelProblems` dock panel.

## Decision

1. **Keep `ScopeViewport` precedence in 2D and document scope priority**: `ToolRotate` (`e`), `ToolScale` (`r`), `ToolMove` (`w` / `m`), `ToolSelect` (`v`), and `ToolPan` (`space`) match the 44 px header tool group (`main_layout_ui.go`) and 3D transform gizmo keys. Pressing `e` from Select in 2D stays Rotate; once any tile tool (`b`, `g`, `t`, `u`, `l`, `c`, `n`) is active, `ScopeTile` outranks `ScopeViewport` and `e` switches to Erase Tile.
2. **Generate and test-verify bilingual shortcut blocks from `Editor.Commands`**: `pkg/editor/shortcuts_reference_test.go` (`TestShortcutDocsMatchRegistry`) renders the scope-grouped shortcut list directly from `Editor.Commands()` and `scopeDisplayOrder` in both English and Simplified Chinese between `BEGIN/END GENERATED EDITOR SHORTCUTS` markers in `docs/en/editor.md` and `docs/zh/editor.md` (updatable via `EDITOR_UPDATE_SHORTCUT_DOCS=1`). It also verifies that every built-in command shortcut and every top-level `Editor.ApplicationMenu()` label (`File`, `Edit`, `View`, `Project`, `Help`) is present on both language pages.
3. **Rename and localize `CmdProblemsPanel` (and unmapped shortcut commands)**: `CmdProblemsPanel` is now `"Agent Problem Panel"` / `"智能体问题面板"` (`"Toggle the right-rail Agent problem-fix panel"` / `"切换右侧栏的智能体问题修复面板"`), matching `status-problems-pill` (`problems.openPanelLabel`) and `SetProblemAgentPanelOpen`. Remaining built-in shortcut commands are mapped in `builtinCommandKeys` and `pkg/editor/locales/{en,zh}.yaml` so both the editor UI and the generated Chinese docs are fully localized.

## Alternatives considered

- **Give `ScopeTile` precedence over `ScopeViewport` in 2D (so `e` activates Erase Tile from Select)**: makes `e` immediately enter Erase Tile without choosing a tile brush first, but contradicts the 44 px main header toolbar (`Rotate · E`, `Scale · R`, `Move · W`), breaks muscle memory between 2D and 3D `W`/`E`/`R` transform tools, and conflicts with existing 2D transform tests (`gizmo3d_transform_test.go`).
- **Make `CmdProblemsPanel` toggle `PanelProblems` instead of `ProblemAgentPanelOpen`**: gives the command palette a direct toggle for the bottom dock panel, but breaks the AI-first main layout contract (`main_layout_test.go`) where `status-problems-pill` and `CmdProblemsPanel` both open the right-rail Agent problem-fix panel, while `PanelProblems` remains toggleable under `View > Panels`.
- **Keep hand-written prose lists and only add a spot-check test**: lighter diff, but allows newly added commands or scope changes to drift out of the docs unnoticed, whereas marker-bounded generation from `Editor.Commands()` mirrors `internal/gamecli/reference_test.go` and catches every drift automatically.

## Consequences

- **Benefits**: Adding, renaming, or re-scoping any built-in shortcut or top-level menu causes `TestShortcutDocsMatchRegistry` to fail until both `docs/en/editor.md` and `docs/zh/editor.md` are regenerated, preventing future drift. Command Palette and ⌘/ Shortcuts Sheet labels accurately describe `CmdProblemsPanel` and localize every built-in shortcut command in Chinese.
- **Costs**: Changing built-in shortcuts in `pkg/editor/commands.go` requires running `EDITOR_UPDATE_SHORTCUT_DOCS=1 go tool nsgo test ./pkg/editor -run TestShortcutDocsMatchRegistry` to refresh the generated doc blocks in the same PR.
