# Agent Note: Editor Agent panel failure, recovery, and offline states

Status: implemented

## Problem

Issue #1024 specifies how the NSGo Game Editor's Agent panel behaves when an agent request does not simply succeed (`docs/design/editor/AgentStates.dc.html`): partial completion, getting stuck after three consecutive failures of the same tool, losing the MCP connection or model mid-request, stopping and resuming mid-plan, a prose summary disagreeing with the actual scene diff, and working offline with a request queue. Built on top of the [agent connect and status chip architecture](../architecture/2026-11-26-editor-agent-connect.md), these states need a shared in-memory turn diff/review contract, deterministic MCP failure/disconnect tracking, and viewport pointing hooks while keeping manual tools working in every state.

## Decision

### Turn baseline diffing and inline review (`AgentTurnReview`)

- Every composer turn captures a cloned scene baseline (`baselineDoc`, `baselineYAML`, and `historyStart`) when submitted, and `editormcp.Server` calls `Editor.PrepareExternalAgentBaseline()` before executing `ActionEdit` tools so terminal/scripted MCP sessions also get an exact baseline before the very first mutation.
- `Editor.refreshTurnReview` reuses `diffSceneDocs` from the [Changes panel](./2026-11-26-editor-changes-panel.md) to compute added, changed, and removed `SceneObjectChange` lists and YAML `+`/`−` line counts without requiring a Git repository. `KeepAgentTurnDiffReview` keeps only the actual review diff (never the agent's prose summary), while `DiscardAgentTurnReview` cleanly unwinds unkept undo steps back to `historyStart`.
- Per-step object mutations (`agentObjectMutation`) are recorded as steps advance so the mismatch detector can explain transient edits (for example, `"slime_03 was added at step 3 and removed again at step 5."`).

### Consecutive failure pause (`Stuck`) and MCP integration

- `editormcp.Server.handler` passes structured `AgentToolCallRecord`s (including concise `scene_edit` call summaries like `add Door.lock` and concise `EditError.Problem` messages) to `Editor.RecordAgentToolCall`.
- Three consecutive failures (`AgentStuckFailureThreshold = 3`) of the same normalized tool name cancel any active composer `AgentRunner` run, transition the status chip to `AgentStatusWaitingForYou`, collapse the trailing failures into one summary step, and attach `AgentStuckState` with the three refused calls, their errors, a hint input (`ContinueAgentWithHint`), and `StopStuckAgentTurn`. Because `scene_edit` executes inside `History.Group` and rolls back on error, refused edits never touch the open scene or disk files; the stateless `scene_edit` handler itself still returns its normal `EditError` so MCP error-handling tests remain unaffected.

### Mid-request disconnect (`Lost`), `Stopped` resume, `Partial` pointing, `Mismatch`, and `Offline`

- **Lost**: `NotifyAgentMCPDisconnected` calls `markActiveTurnLost` when a composer run or recent external MCP turn is in progress, marking any running step `AgentStepInterrupted` (`"cut off, nothing written"`), finalizing the unkept `AgentTurnReview`, and preserving the disk file untouched. When a client reconnects (`NotifyAgentMCPConnected`), `markLostTurnReconnected` opens an ask-before-pickup confirmation (`AskResumePending`) instead of auto-resuming. Model errors (such as HTTP 529 overloaded) share the same `AgentLostState` card with **Retry now** and fallback switching.
- **Stopped**: `StopAgentRun` populates `AgentStoppedState` with `DONE` and `NOT DONE` steps (including remaining `PlannedSteps`) and leaves changes unkept until reviewed. `ResumeAgentTurn` skips all `AgentStepDone` steps and passes `ResumeFromStep`, `CompletedSteps`, and `RemainingSteps` on `AgentRequest`.
- **Partial**: `AgentPartialState` provides **Keep these**, **Let me point** (`ToggleAgentPointing`, intercepted in `HandleCanvasPressWithModifiers` ahead of normal tools to call `PickAgentPointWorld` and canceled cleanly on `Esc`), and **Try another way**.
- **Mismatch**: `CheckAgentSummaryAgainstDiff` runs automatically on every turn summary (`AgentEventDone`, `finishAgentRun`, and `RecordAgentTurnSummary`), comparing numeric count claims and `"untouched"` assertions against `AgentTurnReview` and rendering the `WHAT · SUMMARY · REVIEW` table when they disagree.
- **Offline**: `SetAgentOffline(true)` sets `AgentStatusOffline` (`Disconnected · offline`), queues composer submissions with timestamps (`AgentQueuedRequest`), and keeps Select, Move, Inspector, `CheckProblems`, and Play working. `SetAgentOffline(false)` never auto-sends queued items; each item waits for explicit `SendQueuedAgentRequest` or `RemoveQueuedAgentRequest`.

## Alternatives considered

### Require Git `ChangesPanelView` for all agent Review buttons

Reusing `OpenChangesPanel` directly for **Review N changes** / **Open review** would avoid an inline turn review drawer. It was rejected because agent edits via `scene_edit` live in memory (unsaved, undoable via `History`) until the author saves, and projects or test fixtures may not have a Git repository; an in-memory `AgentTurnReview` anchored to the turn's scene baseline works identically whether or not Git is initialized and guarantees Keep applies the exact in-memory review diff.

### Reject a 4th MCP `scene_edit` call inside `editormcp.Server` while stuck-paused

Having `editormcp.Server` refuse subsequent tool calls while `Stuck` is active would enforce the pause at the transport layer as well as the runner layer. It was rejected because stateless MCP clients and conformance tests (such as `TestSceneEditRefusalChangesNothing`) intentionally issue many failing `scene_edit` calls in sequence and assert each call's exact structured `EditError`; canceling the active `AgentRunner` context and pausing the UI turn stops autonomous guessing without breaking stateless MCP validation semantics.

### Rely on the agent to report summary-vs-diff mismatches

Asking the model to self-audit its own summary adds latency and fails whenever the model itself forgets a retried/deleted edit. Deterministic client-side parsing in `CheckAgentSummaryAgainstDiff` against the editor's own `diffSceneDocs` snapshot catches count and untouched disagreements locally and immediately.

## Consequences

- **Benefits**: All six failure and offline states from `AgentStates.dc.html` are first-class, Spec-token-compliant in Dark and Light modes, wired to both `AgentRunner` and scripted MCP clients, and verifiable deterministically without real network calls or sleeps. Manual editing tools remain unblocked across every state.
- **Costs**: `AgentTurn` holds transient scene baselines and mutation logs while a turn is active (cleared on project reset), and natural-language mismatch parsing in `CheckAgentSummaryAgainstDiff` focuses on English count and `"untouched"` claims and may need additional patterns as localized summaries evolve.

## Testing

- `packages/game/editor/agent_states_test.go`: UI and state-machine tests for Partial (Keep, Let me point + Esc cancel, Try another way), Stuck (3 consecutive failures pause + hint + stop), Lost (mid-request disconnect, post-reconnect ask, 529 model error), Stopped (done/not-done + Resume skipping done steps + unkept review), Mismatch (summary claiming 3 vs diff 2 + Keep applying review), Offline (manual tools + queue + manual send back online), and Light/Dark Spec tokens.
- `packages/game/editor/editormcp/agent_states_test.go`: scripted MCP client acceptance tests for 3 failed `scene_edit` calls pausing the request, dropped MCP connection leaving a review and untouched disk file, Resume after Stop skipping done steps, and summary-vs-diff mismatch with Keep applying only the review.
- `packages/game/editor/editorcmd/agent_states_screenshot_test.go`: real native macOS `nsgotest` screenshots for all six states inspected beside `docs/design/editor/AgentStates.dc.html`.
