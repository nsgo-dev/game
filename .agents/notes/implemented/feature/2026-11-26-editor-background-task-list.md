# Agent Note: Editor background task list with queued and parallel agent requests

Status: implemented

## Problem

The editor previously had no background task model or Agent Tasks UI to track multiple requests, plan steps, and automations together, no way to queue a request so it waits until the running turn is kept or reverted before building on the reviewed scene, and no concurrency control in `editormcp` when two parallel agent sessions attempt to edit the same scene object at once (#1022). Downstream notifications and Dock badges (#1023) also need canonical urgency and badge counts (`NeedsYou + ReadyToReview + Failed`, excluding `Running` and `Queued`).

## Decision

Implement a background task model, plain-text thread persistence, Spec-token UI, numbered viewport/tree overlays, and FIFO per-object holds in `editormcp`:

### Task lifecycle, scheduling, and thread YAML persistence (`pkg/editor/tasks.go`)
- Model tasks across five visible groups (`TaskNeedsYou`, `TaskRunning`, `TaskReadyToReview`, `TaskQueued`, `TaskFailed`) plus terminal states (`TaskDone`, `TaskStopped`), unified across `TaskSourceUser`, `TaskSourcePlan`, and `TaskSourceAutomation`.
- Persist the task list in plain-text YAML under `<project>/.nsgo/threads/default.thread.yaml` (`AgentThreadVersion = 1`) via `EncodeAgentThread` / `ParseAgentThread`, refusing unknown fields (`KnownFields(true)`), duplicate keys, aliases, anchors, and merge keys, with deterministic round-trip output.
- Enforce queue semantics strictly: `CompleteTaskTurn` transitions a running task to `TaskReadyToReview` **without** starting queued tasks. Queued tasks (`TaskStartQueue`) advance only when the blocking turn is explicitly kept (`KeepTask`), reverted (`RevertTask`), stopped (`StopTask`), or dismissed (`DismissTasks`).
- Run tasks through `Editor`'s default `AgentTaskRunner` when `Editor.AgentTaskRunner` is nil (routing through `resolveRouteAndRunner` in a fresh session and `AgentTurn`, refusing disconnected submissions with the Connect dialog); see [default AgentTaskRunner](../../implemented/bug-fix/2026-11-28-editor-default-agent-task-runner.md).
- Keep bulk dismissal non-destructive: `DismissTasks` / `DismissSelectedTasks` marks selected `TaskReadyToReview` or `TaskFailed` entries `Dismissed = true` without modifying `Editor.Doc` or `Editor.History`, guaranteeing nothing that was kept is ever reverted.
- Compute `TaskSummary` and `TaskDockBadgeCount` (`NeedsYou + ReadyToReview + Failed`, never counting `Running` or `Queued`) and `TaskStatusParts` (`No tasks`, single-task elapsed `1 running · M:SS` using injectable `Editor.Now`, or urgent-first mixed counts with zero counts dropped).

### Per-object MCP holds and separate reviews (`pkg/editor/editormcp/holds.go`, `tasks.go`, `server.go`, `edit.go`, `inspector.go`)
- Bind each parallel agent session to its task ID via `Server.ConnectTask(ctx, transport, taskID)` (or `BindSessionTask`, automatically via `NSGO_EDITOR_TASK_ID` through `Proxy` or `WithTaskToolContext` for the built-in runner), releasing holds automatically if the session disconnects or when `Editor.StopTask` / `CompleteTaskTurn` fires the registered hold-release hook.
- Acquire target object holds in sorted, all-or-nothing FIFO order before invoking `c.Editor(...)` in `scene_edit` and `object_set_field` so the editor's UI goroutine is never blocked while a second parallel task waits.
- While waiting on a held object, update the editor task with `SetTaskHoldState` (`HoldingObjects`, `BlockedOn`, `BlockedByTaskID`, `BlockedByTaskNumber`, step `"waiting on <object>"`). When the holding task completes its turn (`Server.CompleteTaskTurn`), it attaches its own distinct `TaskReview` (with recorded edits and scene snapshots) and releases holds, waking the waiting task to finish with its own separate `TaskReview`.
- Register `tasks_list`, `tasks_submit`, and `tasks_control` MCP tools so every task action is reachable over MCP as required by `Spec.dc.html`.

### Spec-token UI and numbered overlays (`pkg/editor/tasks_ui.go`, `viewport.go`, `scenetree.go`, `ui.go`, `commands.go`, `menubar.go`)
- Mount `TasksPanelView` in the right column (matching `docs/design/editor/Tasks.dc.html` and using exclusively `SpecPalette` tokens in Dark and Light appearances) and toggle it from the right-side status bar button (`status-tasks`), `View > Background Tasks`, and `CmdToggleTasks` (`mod+shift+t`), adding zero buttons to `editor-toolbar` so the 2D toolbar stays `<= 2` rows at `1440×900`.
- Draw solid violet (`SpecPalette.Agent`) outlines + numbered pills (`1 placing the jump arc`) for editing objects and 45%-opacity dashed violet outlines + clock glyph + `"2 waiting on slime_02"` pills for holding objects in `drawAgentTaskOverlays`, plus a bottom-left legend (`viewport-task-legend`) and `"1 · editing"` / `"2 · holding"` badges on scene tree rows (`scene-tree-task-<id>`).

## Alternatives considered

1. **Serializing all parallel MCP sessions behind a global server mutex**: simpler to implement, but it would block independent tasks editing disjoint objects (`slime_01` vs `door`) and could not report which specific object and task caused a hold. Per-object sorted all-or-nothing FIFO holds allow true parallelism on disjoint objects while preventing deadlocks and surfacing `"waiting on <object>"`.
2. **Starting queued tasks as soon as the running task reaches `Ready to review`**: natural for a generic job queue, but `Tasks.dc.html` explicitly specifies that Queue *"starts when the running one is kept or reverted, so it builds on the reviewed scene"*. Gating queue advancement on `KeepTask` / `RevertTask` preserves the reviewed-scene invariant.
3. **Adding a Tasks button to `editor-toolbar`**: discoverable at top, but `editor-toolbar` is budgeted to `<= 2` rows (`<= 80 pt` at `1440×900`) and `Tasks.dc.html` places the live tasks toggle in the status bar. Keeping it in the status bar + View menu + `mod+shift+t` matches the artboard and protects toolbar compactness.

## Consequences

- **Benefits**: queued and parallel agent requests coexist safely without clobbering shared objects; every parallel turn yields an independent review; bulk dismissal never reverts kept work; task state survives restarts in reviewable plain-text YAML; and downstream #1023 notifications/Dock badges can read `TaskSummary()` directly.
- **Costs**: `editormcp.Server` holds an in-memory `objectHoldManager` and session-to-task map that must stay synchronized with editor task lifecycle hooks (`SetTaskHoldReleaseHook`), and `scene_edit` / `object_set_field` extract target object IDs before acquiring the editor goroutine.
- **Verification**: deterministic unit and `nsgotest` UI tests in `packages/game/editor/tasks_test.go`, parallel two-session MCP hold and separate-review tests in `packages/game/editor/editormcp/tasks_test.go`, updated tool-list tests in `server_test.go` / `proxy_test.go` / `example_test.go`, real macOS `nsgotest` screenshots in `packages/game/editor/editorcmd/tasks_screenshot_test.go`, `nsgo check` (host and `-target ios`), `tools/changelog check`, and `bun run verify-notes`.
