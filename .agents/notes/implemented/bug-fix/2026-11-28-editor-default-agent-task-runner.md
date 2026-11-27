# Agent Note: Default AgentTaskRunner for background tasks and tasks_submit

Status: implemented

## Problem

Background tasks submitted from the Tasks panel composer (`Editor.SubmitComposerTask`), the `tasks_submit` MCP tool, or promoted from the queue by `advanceTaskQueue` stayed `TaskRunning` indefinitely in production (#1507). `launchTaskRunner` (`pkg/editor/tasks.go`) returned early whenever `Editor.AgentTaskRunner` was nil, and nothing outside unit tests assigned `AgentTaskRunner`. Submitting a task with no agent connected also created a stuck `TaskRunning` entry instead of opening the Connect dialog as the agent composer does.

## Decision

Make the default background task runner the nil fallback inside `Editor` (`pkg/editor/tasks.go`), so `editorcmd`, `editormcp` (`tasks_submit`), `internal/gamecli`, and tests with `AgentTaskRunner == nil` all execute background tasks through the same route as the agent composer while keeping `Editor.AgentTaskRunner` available as an explicit test/caller override:

### 1. Connection gate before task creation (`validateTaskAgentRoute`)
- When `SubmitTask` receives a runnable request (`req.InitialState == ""` and `Editor.AgentTaskRunner == nil`), it validates the task's route (`TaskKind`, defaulting to `AgentTaskSceneEdit`) via `resolveRouteAndRunner` before creating any task.
- If no agent is connected (or API key mode has no stored key), it opens the Connect dialog (`OpenConnectDialog` / `OpenConnectDialogForMode(AgentModeAPIKey)`), preserves the Tasks composer draft, returns an error, and creates no task. `RetryTask` applies the same check before leaving `TaskFailed`.
- Seeded tasks with `req.InitialState != ""` (used by automations, restored thread YAML, and UI state tests) skip the connection check and do not launch a runner.

### 2. Default runner execution (`launchDefaultTaskRunner`, `applyDefaultTaskEvent`, `finishDefaultTaskRun`)
- Each launched task resolves its backend via `resolveRouteAndRunner(taskKind)` (CLI runner or `builtInAgentRunner`), captures a pre-task checkpoint and baseline scene YAML, creates its own `AgentTurn` (`Source: "task"`, stored on `AgentTask.TurnID`), and runs in a fresh session (`req.SessionID = ""`, `req.ThreadID = "task-"+task.ID`, `req.TaskID = task.ID`) so parallel tasks never resume or overwrite the foreground thread's session or built-in history.
- Streamed `AgentEvent`s update both the task's `AgentTurn` and `AgentTask` transitions: `AgentEventStep` updates steps (resolving step titles from the turn when `tool_result` omits them) and moves `AgentStepWaiting` to `TaskNeedsYou`; `AgentEventSession` records the CLI session ID on the task/turn without overwriting `Editor.AgentSessionID()`; completion calls `CompleteTaskTurn` (`TaskReadyToReview`, leaving queued tasks waiting until `KeepTask`/`RevertTask`), errors call `FailTask`, and cancellation calls `StopTask`.
- `KeepTask` and `RevertTask` synchronize the task's `AgentTurn.ReviewState` and advance any waiting queued task via `advanceTaskQueue` / `launchTaskRunner`; all scope-less background callbacks run through `Editor`'s single headless executor (`e.postOrRun` / `Editor.RunHeadless` / `Editor.PostOrRunHeadless`).

### 3. `editormcp` session binding, holds, permissions, and review snapshots
- `CLIAgentRunner` sets `NSGO_EDITOR_TASK_ID=<req.TaskID>` in the child environment (and strips any inherited value when `req.TaskID == ""`). `editormcp.Proxy` forwards a validated `NSGO_EDITOR_TASK_ID` in its MCP `Implementation.Version` (`nsgo-mcp-stdio|task=<id>`) so `editormcp.Server.registerOrUpdateSession` automatically calls `BindSessionTask` for spawned CLI agents.
- `builtInAgentRunner` tags tool execution contexts with `editor.WithTaskToolContext(ctx, req.TaskID)`, which `editormcp.Server.runManagedToolCall` reads via `editor.TaskIDFromContext(ctx)`.
- Tool calls carrying a `taskID` enforce You-only boundaries, privacy, and permission rules without clobbering the foreground live request; `PermissionAsk` rules pause the task via `Editor.BeginTaskPermissionAsk` (`TaskNeedsYou`) and resume or return `PermissionDeniedResult` when `ResolveTaskPermission` is called.
- `editormcp.New` registers `Editor.SetTaskReviewSnapshotHook(s.TaskReviewSnapshot)` alongside `SetTaskHoldReleaseHook(s.ReleaseTaskHolds)` so `finishDefaultTaskRun` populates each task's separate `TaskReview` from the edits recorded on its MCP session (falling back to scene YAML diffing when edits did not go through MCP).

Related: [Editor background task list](../../implemented/feature/2026-11-26-editor-background-task-list.md).

## Alternatives considered

1. **Wiring `AgentTaskRunner` only inside `editorcmd.New`**: leaves `editor.New` callers, `editormcp` unit tests, and any embedded editor host without a working runner, and requires duplicating route/turn helpers outside `pkg/editor` where `resolveRouteAndRunner` and `builtInAgentRunner` are unexported. Making the default runner the `nil` behavior of `Editor` keeps one source of truth inside `pkg/editor`.
2. **Reusing `SubmitAgentTaskPrompt` directly for background tasks**: `SubmitAgentTaskPrompt` enforces a single `activeRunCancel` and writes to the foreground thread's `sessionID` and live-request state, which cannot run parallel tasks concurrently. Dedicated per-task turns and fresh sessions let parallel tasks run independently while sharing `resolveRouteAndRunner` and `AssembleAgentRequest`.
3. **Adding a custom MCP extension method to bind CLI proxy sessions to task IDs**: would require protocol changes across proxy and server handshakes. Carrying the validated task ID alongside `proxyStdioMarker` in `Proxy.forwardImpl()` reuses the existing initialize handshake with zero wire-protocol changes.

## Consequences

- **Benefits**: Tasks panel requests, `tasks_submit`, and queued task promotions execute end-to-end on both CLI and built-in agents; parallel tasks get isolated sessions, turns, holds, permission prompts, and separate reviews; disconnected submissions show Connect instead of leaving stuck `Running` entries.
- **Costs**: Headless unit tests that previously called `SubmitTask` without an agent to drive `CompleteTaskTurn`/`RevertTask` manually now install an explicit stub `AgentTaskRunner` (or pass `InitialState`), matching real runtime semantics.
- **Verification**: `go tool nsgo test` with `-race` across `pkg/editor` and `pkg/editor/editormcp` (scripted `claude` CLI on `PATH` with nil `AgentTaskRunner` and nil `AgentRunner`, queued advancement on `KeepTask`, Connect refusal with zero tasks created, built-in parallel holds + `Needs you` permission resolution, `tasks_submit` + `tasks_control`, and `Proxy` `NSGO_EDITOR_TASK_ID` binding), `go tool nsgo check`, `(cd tools && go run ./changelog check)`, and `bun run verify-notes`.
