# Agent Note: Editor system notifications, Dock badge, and Play-mode dot for agent tasks

Status: implemented

## Problem

Issue #1023 adds outside-the-editor awareness for background agent tasks (`Tasks.dc.html`, `AgentStates.dc.html`, `Spec.dc.html`) on top of the [background task list](./2026-11-26-editor-background-task-list.md) (#1022) and [agent connect and status chip](../architecture/2026-11-26-editor-agent-connect.md) foundations:

1. Developers switching away from the editor or running Play mode need to know when an agent task asks for permission, finishes a reviewable turn, or fails, without getting spammed by intermediate steps, queued starts, restored thread entries, or agent replies.
2. NSGo's macOS notification center disables Dock badges unless badge permission is granted once any permission check or notification call occurs, so permission requests must be deferred until the first system notification is actually needed.
3. During Play mode no agent notification may pop over the running game, yet urgent task state must remain subtly visible and deferred notifications must arrive as soon as Play stops.

## Decision

### Unified notification pipeline on top of `tasks.go` (`packages/game/editor/agent_notifications.go`, `tasks.go`, `agent.go`)

- Reuses the canonical `AgentTask` and `TaskState` (`TaskNeedsYou`, `TaskRunning`, `TaskReadyToReview`, `TaskQueued`, `TaskFailed`, `TaskStopped`, `TaskDone`) from `tasks.go` (#1022) instead of defining a second task type. All state transitions in `tasks.go` (`SubmitTask`, `RequestTaskPermission`, `ResolveTaskPermission`, `CompleteTaskTurn`, `FailTask`, `StopTask`, `RetryTask`, `KeepTask`, `RevertTask`, `DismissTasks`, `LoadAgentThread`, `resetTasksForProject`) feed into the notification and badge pipeline after releasing `e.tasks.mu`, while direct Agent panel runs in `agent.go` (`SubmitAgentPrompt`, `SetAgentWaitingForYou`, `ClearAgentWaitingForYou`, `StopAgentRun`, `finishAgentRun`) mirror into an ephemeral in-memory slice so they share the same pipeline without polluting `.nsgo/threads/*.thread.yaml`.
- Only `TaskNeedsYou`, `TaskReadyToReview`, and `TaskFailed` (`TaskState.IsNotifiable()`) can emit a system notification or contribute to `AgentDockBadgeCount()`. Finished steps (`AgentStepDone` / `AddTaskStep`), queued starts, restored thread entries on `LoadAgentThread`, and agent replies (`AgentEventMessage`) never notify and never increment the badge.
- Notification text (`Header`, `Title`, `Body`, action hints) strips/collapses control characters and bounds rune lengths so untrusted agent prompts or large tool errors cannot garble or bloat OS banners. Held and pending-permission notification queues are bounded to 64 entries.
- Notification state is mutex-protected (`agentNotifyState.mu`, strictly ordered after `e.tasks.mu` and never held across `core.Scope` calls, `e.notify()`, or timers) with generation tokens (`gen` and per-task `timerSeq`) so synchronous schedulers, cancelled timers, scope cleanups, or project switches cannot leak timers or dispatch stale notifications.
- Notification permission (`cx.RequestNotificationPermission`) is requested lazily on the first notification dispatch, never on startup or badge updates. If an `EditorView` scope is disposed while a permission request is still pending, cleanup clears the pending flag so a remounted scope is never wedged.
- Window background state is read from `cx.Env().Lifecycle() != core.LifecycleActive`. In the background, all three events notify immediately. In the foreground (outside Play), `TaskReadyToReview` and `TaskFailed` stay in the panel without a system notification, while `TaskNeedsYou` schedules a `30 s` timer (`AgentWaitingNotifyDelay`) via `Editor.AgentScheduleAfter` and notifies only if the request is still unanswered when the timer fires.
- Clicking a notification (`cx.OnNotificationPress` -> `Editor.HandleNotificationPress` -> `Editor.OpenAgentTask`) invokes `Editor.OnFocusWindow` when set, stops Play if active, dismisses obstructing panels (Settings, History, Problem agent panel, Ask-in-place, Assets left rail), opens the task's scene strictly confined inside the project root via `resolveProjectFileWithinRoot` (refusing traversal and symlink escapes), opens the Tasks or Agent panel, focuses the task/open request, and closes the shown notification.

### Dock badge and Play-mode dot (`packages/game/editor/play_task_dot_ui.go`, `play.go`, `ui.go`, `viewport.go`)

- `EditorView` syncs `Editor.AgentDockBadgeCount()` (`Waiting + ReadyToReview + Failed`) to `cx.SetBadge` whenever it changes and clears it (along with held notifications before stopping Play) on scope cleanup.
- While `Editor.Playing()` is true, `onAgentTaskStateUpdated` holds eligible notifications in `heldNotifications` instead of calling `cx.ShowNotification`, without breaking non-agent editor toasts (`ToastView`). Calling `Editor.Stop()` flushes held notifications whose tasks are still in a notifiable state.
- `PlayTaskDotView` mounts stably in the top-right corner of `ViewportView` while `Editor.Playing()` and `PlayTaskDot().Visible` are true, updating colors and labels reactively via `StyleFunc` / `LabelFunc` / `ContentFunc` so urgency escalations never unmount `play-task-dot` or drop hover/keyboard focus. It renders a single 8 pt dot with a 3 pt halo inside a 28 × 28 pt hit target colored by the most urgent Spec token (`TaskFailed` `Removed.Fg` > `TaskNeedsYou` `Warning.Fg` > `TaskReadyToReview` `Accent.Fg`). The pill label (e.g. `"1 task needs you · stop Play to open"`) renders strictly on hover or keyboard focus.

## Alternatives considered

### Call `cx.NotificationPermission` on editor startup to cache permission status

Checking permission at startup avoids a callback hop on the first notification. It was rejected because NSGo's macOS implementation touches `UNUserNotificationCenter` as soon as `NotificationPermission` is checked, which causes macOS to suppress Dock badges unless notification badge permission has already been granted; deferring permission until the first actual notification keeps Dock badges working out of the box and matches the spec ("asking for permission the first time one is needed").

### Use custom platform syscalls / AppleScript for macOS notifications and Dock badges

Hand-rolling platform-specific bridges would bypass NSGo's unified driver layer and require separate iOS/Windows/Linux stubs plus mock interfaces for headless tests. It was rejected because `github.com/nsgo-dev/nsgo/core` already provides `cx.ShowNotification`, `cx.RequestNotificationPermission`, `cx.CloseNotification`, `cx.OnNotificationPress`, and `cx.SetBadge`, and `nsgotest.App` exercises all of them deterministically (`app.Notifications`, `app.Badge`, `app.NotificationPermission`, `app.PressNotification`).

### Suppress all `ToastView` editor toasts during Play mode

Blanket-hiding `ToastView` whenever `Editor.Playing()` is true broke `TestToastViewAvoids2DOverlayBarAndMemoizesRevisions` (which specifies that editor action toasts such as save feedback render at `Bottom: 16` during Play when the 2D/3D overlay bar is absent). Keeping `ToastView` intact while holding all agent task notifications until `Editor.Stop()` satisfies both `Toasts.dc.html` and `Tasks.dc.html`.

## Consequences

- **Benefits**: Single canonical `AgentTask`/`TaskState` model shared with #1022; full cross-platform support (macOS, Windows, Linux, Android, iOS, Web) with zero C code; race-safe timers and symlink-safe scene opening on notification click; remount-free Play-mode dot hover/focus stability; deterministic headless testing.
- **Costs**: `core.Notification` carries `ID`, `Title`, and `Body` without native action buttons, so the primary action hint (`Allow once`, `Review`, `Open`) is carried in the notification body while clicking the notification opens the task directly in its scene.

## Testing

- `packages/game/editor/agent_notifications_test.go`: tests scripted background agent notifications and `#1022` background task transitions (`SubmitTask`, `RequestTaskPermission`, `ResolveTaskPermission`, `CompleteTaskTurn`, `FailTask`, `KeepTask`, `RetryTask`, `DismissTasks`), 1 waiting + 2 review + 1 failed = Dock badge 4, 30 s foreground waiting rule and cancellation, denied permission handling, notification click dismissing obstructing panels and opening another scene, symlink escape refusal, control-character sanitization and length bounds, thread restore without notification spam, and Play-mode notification holding, remount-free dot hover/color escalation (Dark & Light), and flush on Stop.
- `packages/game/editor/editorcmd/task_notifications_screenshot_test.go`: captures real macOS `nsgotest` screenshots of live Play mode with the hovered corner dot (waiting amber and failed red) and the Spec-token notifications/Dock-badge/During-Play gallery.
