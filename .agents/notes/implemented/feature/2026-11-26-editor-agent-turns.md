# Agent Note: Multi-file pending agent turns with derived structural diffs and single-step undo

Status: implemented

## Problem

Issue #1011 requires every edit an agent makes through the editor's MCP server between the start and end of one request to become **one pending turn** across every touched file (scenes, prefabs, data tables, `game.yaml`, and animation clips), shown live in memory with zero disk writes until the person keeps and saves it. Before this change, [the editor MCP server](../architecture/2026-11-25-editor-mcp-server.md) recorded each `scene_edit` call as a separate undo step on the single open scene, could not stage edits across multiple files without writing to disk, had no per-change revert/restore model, did not tag history commands by author (`AuthorPerson` vs `AuthorAgent`), and did not enforce the `Checkpoints.dc.html` `⌘Z` scoping rules while an agent works or while a review is open.

## Decision

`packages/game/editor` owns the pending turn state machine, structural diff/synthesis engine, history author metadata, and right-panel Agent UI, while `packages/game/editor/editormcp` exposes turn lifecycle and multi-file staging tools without ever allowing an agent to keep or revert its own turn.

### Multi-file in-memory staging and single-step Keep/Revert

- **Turn lifecycle**: `Editor.StartAgentTurn`, `RecordAgentTurnStep`, `EndAgentTurn`, and `StopAgentTurn` (`⌘.` bound with `core.KeyAlways`) manage an `agentTurnState` holding `beforeYAML`, `agentYAML`, and `currentYAML` per touched project-relative file.
- **Zero disk writes until Keep + Save**: `RunAgentSceneEdit` and `RecordAgentFileEdit` mutate only the live open `Editor.Doc`, `Editor.Project`, and `Editor.stagedFiles` map. Opening another scene reads from `stagedFiles` first (`openFileWithLayers`), and `editormcp`'s `scene_read` reads `Editor.StagedFileYAML` before falling back to disk. `Save` and `SaveAll` refuse while a turn is pending, `HasOpenReview` pauses autosave while a turn is open, and `RequestOpenScene`, `RequestOpenProject`, window close, and quit prompt **Keep / Revert / Cancel** (`pendingTurnDialog`).
- **Single-step Keep (`AuthorAgent`)**: `KeepAgentTurn` commits all remaining non-reverted file snapshots into `Editor.History` as **one** `agentTurnCmd` tagged `AuthorAgent` / `HistoryAuthorAgent` / `HistoryActorAgent` (via `commandAuthorSource`, `filesCommand`, `authoredCommand`, and `historyMetaCommand`, exposed through `History.Entries()`, `History.UndoAuthor()`, `History.RedoAuthor()`, and `History.UndoActor()`). Undoing that one command restores every touched file byte-for-byte; redoing reapplies the kept state. Both `KeepAgentTurn` and `RevertAgentTurn` display a Spec toast (`Editor.ShowToast`, sharing ID `"agent-turn"` so lifecycle toasts replace one another cleanly) with an **Undo (`⌘Z`)** action; Undo after Revert calls `UndoRevertAgentTurn` to bring the turn back as pending.
- **`Checkpoints.dc.html` `⌘Z` rules**: `Editor.Undo` routes through `undoForAgentTurn()`. While `AgentTurnWorking()`, `⌘Z` undoes only the person's own edits (agent edits bypass `History` until Keep); while `AgentTurnReviewOpen()`, `⌘Z` undoes person edits made inside the review first and then reverts the review; once idle or kept, `⌘Z` operates on the ordinary `History` stack.

### Derived structural diff and lossless revert/restore synthesis

- **Derived, never a command log**: `recomputeAgentTurnDiffs` structurally diffs each touched file between its pre-turn `beforeYAML` and `agentYAML` (per object ID and field/component/property/layer/parent for scenes/prefabs via `diffTurnSceneDocs`, per record for data tables via `datatable.Parse`, per dotted key for `game.yaml`, per header/track/event for clips via `resource.DecodeAnimation`, and top-level key fallback for generic YAML). Each `AgentTurnChange` stores `Kind` (`added`/`changed`/`removed`), `Title`, `Detail`, `File`, 1-based YAML `Line`, optional `Why` reason (from MCP `why` arguments), and `Reverted` state, plus exact LCS `+added −removed` line counts.
- **Lossless per-change revert/restore**: `RevertAgentTurnChange`, `RestoreAgentTurnChange`, `RevertAgentTurnFile`, and `RestoreAgentTurnFile` toggle `reverted[changeID]` and resynthesize the file from `(beforeYAML, agentYAML, reverted)` (`synthesizeSceneDoc`, `synthesizeDataTableDoc`, `synthesizeProjectFile`, `synthesizeClipDoc`, `synthesizeYAMLByKeys`). For the live open scene, `applySceneDelta` applies only the delta for touched objects so concurrent person edits on untouched objects survive intact.

### MCP boundary and Spec-token UI

- **MCP tools (`packages/game/editor/editormcp/turn.go`)**: `turn_start`, `turn_end`, read-only `turn_status` (reporting derived changes and which ones the person reverted), and `file_edit` (strictly validating scenes, prefabs, data tables, `game.yaml`, and clips before staging in memory). `scene_edit` and `object_set_field` accept optional `why` strings and route through `RunAgentSceneEdit` across any project scene/prefab while `AgentTurnWorking()` (while preserving the existing refusal of non-open scene paths outside a turn). There is **intentionally no MCP tool** to keep or revert a turn; only the person decides.
- **Right-panel Agent UI (`packages/game/editor/agent_ui.go`, `agent_turn_ui.go`)**: integrates into `RightRailView` / `AgentPanelView` (with `CmdRightTabAgent` `mod+shift+1` and `CmdRightTabInspector` `mod+shift+2` keeping the 2D `editor-toolbar` untouched and avoiding a Windows/Linux `ctrl+1` clash with `view.3d.back`) and renders the small summary card (`Main.dc.html`) using exclusively `SpecPalette` tokens from [Spec Light theme](./2026-11-26-editor-spec-light-theme.md) (`p.Accent.OnFill` on `p.Accent.Fg` for WCAG AA primary buttons).

## Alternatives considered

### Record a raw MCP command log and replay/reverse commands for per-change revert

Recording every MCP edit call and inverting individual operations on revert is simpler to start with and avoids per-format synthesizers. It is rejected because an agent commonly touches the same object multiple times in one request (creating noisy duplicate rows instead of one net change), reversing an earlier command in a log breaks when later commands depend on it (such as adding then modifying a component), and the issue explicitly requires changes to be **derived** by diffing pre-turn and current file state per object/record/key.

### Write multi-file agent edits to disk immediately and rely on Git or backups to revert

Writing directly to disk lets `game_check` and external tools read files without an in-memory layer. It is rejected because the issue requires edits to stay unsaved in memory until kept and saved, reverting must restore every file byte-for-byte without touching disk, and half-finished or stopped turns must never leave dirty files on disk if the editor closes.

### Expose `turn_keep` and `turn_revert` MCP tools

Mirroring every UI button in MCP keeps parity with tools like `tile_review_keep`. It is rejected because the acceptance criteria for #1011 explicitly forbid any MCP tool from keeping or reverting an agent turn: deciding whether to keep or discard an agent's work belongs exclusively to the person.

## Consequences

- **Benefits**: multi-file agent requests become atomic, reviewable, zero-disk-write turns with crisp structural diffs, `why` provenance, per-change/per-file revert and restore, one-step `AuthorAgent` Undo/Redo, and Spec Undo toasts. Downstream History (#1017), live viewport overlays (#1013), and the full Review workspace (#1014) can consume `History.Entries()`, `CurrentAgentTurn()`, and `AgentTurnReviewOpen()` directly.
- **Costs**: `agent_turn_diff.go` maintains structural diff/synthesis adapters per editable YAML format (scenes/prefabs, data tables, `game.yaml`, clips, generic YAML); adding a new first-class editable YAML type later requires extending the switch in `recomputeAgentTurnDiffs` and `synthesizeTurnFile` (falling back to top-level YAML key diffing until then). While a turn is pending, `game_check` still inspects files on disk as before until the turn is kept and saved.
- **Verification**: `packages/game/editor/editormcp/turn_test.go` verifies multi-scene add/change/remove + per-change revert + Keep + single-step Undo/Redo byte-for-byte, multi-file (`game.yaml`, data table, clip, scene) Revert + toast Undo back to pending, working-turn `⌘Z` scoping to person edits only, stopped turn (`⌘.`) remaining pending, and `turn_status` reporting reverted changes with no MCP keep/revert tool. `packages/game/editor/agent_turn_test.go` verifies summary card UI, shortcuts (`⌘↵`, `⌘⌫`, `⌘.`), review `⌘Z`, and pending-turn navigation prompts. `packages/game/editor/editorcmd/agent_turn_screenshot_test.go` renders real native macOS Dark and Light screenshots for visual inspection against `Main.dc.html` and `Spec.dc.html`.
