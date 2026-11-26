# Agent Note: Live agent edit overlays, scene tree markers, read-only step lock, and interactive legend (#1013)

Status: implemented

## Problem

When an AI agent edits an open scene through `editormcp`, the scene changes landed immediately on `Editor.Doc` with no visual distinction in the viewport or scene tree between objects the agent added, changed, removed, or is actively working on. Users could also accidentally drag or edit the exact object an agent step was mutating mid-turn, had no interactive way to filter live overlay categories, and had no header indicator showing whether the agent was actively working, waiting for input, or awaiting Keep/Revert review.

## Decision

Add a self-contained pending agent turn, step-lock, and live overlay subsystem on `Editor` (`packages/game/editor/agent_live.go` and `agent_live_ui.go`), wire `editormcp` mutations and checks into it automatically, and upgrade the Spec top-left overlay legend (`spec_feedback_ui.go`) into interactive per-user switches persisted in `EditorSettings`.

### Key design choices

- **Structural baseline diff on pending turns (`BeginAgentTurn`, `KeepAgentTurn`, `RevertAgentTurn`)**: Opening a turn snapshots a deep clone of `e.Doc` plus a `sceneformat.Build` baseline and records `History.Index()`. After every `afterMutate()`, `syncAgentTurnAfterMutate` diffs baseline vs current objects deterministically to compute `Added`, `Changed` (with `"<field> <old> → <new>"` diffs synced into `e.inspectorReviewChanges`), and `Removed` (preserving `RemovedDoc` and pre-turn layer/parent placement so viewport ghosts and struck-through `DEL` scene tree rows render in place). `KeepAgentTurn` collapses the turn's `History` commands into a single `groupCmd` undo step and clears overlays; `RevertAgentTurn` undoes back to `historyStart` and clears overlays. Tab switches preserve the active `agentTurn` on `sceneTab`.
- **Inspectable overlay list shared by rendering and tests (`ViewportLiveOverlays`, `VisibleViewportLiveOverlays`)**: Both 2D (`drawAgentLiveOverlays`) and 3D (`drawAgentLiveOverlays3D`) viewports render directly from `VisibleViewportLiveOverlays()`, using only `SpecPalette` tokens (`Added`, `Changed`, `Removed`, `Agent`, `Problem`, `PillBg`, `TextSecondary`) and tracking placed pill rectangles (`resolvePillRect`) so adjacent or stacked pills never overlap. When an object is both selected and actively touched by the agent, Selection keeps its 2D/3D handles (`OutsetPx: 0`) and Agent working renders 3 px further out (`OutsetPx: 6`). Reduced motion (`cx.Env().ReduceMotion()` or `SetReducedMotionOverride`) holds the violet outline solid at 100% instead of running the 1.6 s pulse loop.
- **Step-scoped read-only lock (`BeginAgentStep`, `EndAgentStep`, `ErrAgentTouchingObject`)**: Manual edits are allowed on untouched objects while the agent works, but any manual mutation targeting a currently touched object (2D/3D gizmo drags, shape/handle drags, `MoveObject`/`RotateObject`/`ScaleObject`/`SetObject*`/`DeleteObject`, `BeginInspectorScrub`, inspector fields/banner, scene tree rename/reparent, and object context menu) returns `ErrAgentTouchingObject`. Calls wrapped in `Editor.RunAsAgent` (used by `editormcp`'s `asAgent`) bypass the lock so the agent itself can keep mutating the object until `EndAgentStep` releases it.
- **Per-user legend persistence in `EditorSettings` (`hiddenOverlayKinds`)**: Rather than mutating scene or project YAML, toggling any of the six legend rows (`selection`, `agent`, `added`, `changed`, `removed`, `problem`) updates `EditorSettings.HiddenOverlayKinds` in `editor.yaml` (strictly validated against the canonical kind set, sorted deterministically, round-trip tested, and reflected in `apps/website/public/schemas/editor-settings.v1.json`). The top-left legend appears automatically whenever `HasAgentLiveOverlays()` or `OverlayLegendOpen()` is true.
- **Camera-never-moves invariant and header pill**: Neither `editormcp` nor `Editor`'s agent turn/step APIs touch `View2D` or `Camera3D`. The toolbar header pill (`toolbarAgentPillView`) reflects `AgentStateWorking` (`"Agent is <activity> · manual edits stay available"`), `AgentStateWaiting`, and `AgentStatePendingReview` (with `Keep` and `Revert` buttons), and hides when `AgentStateIdle`.

## Alternatives considered

- **Waiting for a separate agent-panel turn runner before implementing live viewport/tree state**: rejected because issue #1013 explicitly scopes live viewport overlays, scene tree badges, header pill, interactive legend, and MCP step tracking independently of the Agent panel's step list and permission cards. Exposing clean `BeginAgentTurn`/`KeepAgentTurn`/`RevertAgentTurn`/`BeginAgentStep`/`EndAgentStep`/`SetAgentState` hooks on `Editor` and `editormcp.Server` lets the future Agent panel plug straight in without rewriting overlays or tree rows.
- **Blocking all manual edits globally while the agent works**: rejected because `docs/design/editor/Working.dc.html` explicitly promises `"manual edits stay available"` and only the specific object(s) the agent's current step is touching should be read-only.
- **Storing overlay legend toggles in `.scene.yaml` or `game.yaml`**: rejected because visibility filters are personal workspace preferences, matching how `viewportOverlays` is stored in `EditorSettings` (`editor.yaml`) outside the project.

## Consequences

- **Benefits**: Authors immediately see every added, changed, removed, and actively-edited object live in both the 2D/3D viewport and scene tree, can keep editing untouched objects safely, can filter any overlay kind per-user, and can Keep (one undo step) or Revert directly from the header pill.
- **Costs**: `afterMutate()` performs a lightweight object-list diff while an agent turn is pending (no overhead when idle), and any future object-mutation entry point added to `Editor` should call `e.checkAgentTouchLock(id)` so the step read-only invariant stays airtight.
