# Agent Note: Editor Agent Panel Live Steps, Permission Cards, and Stop

Status: implemented

## Problem

When an AI agent drives the scene editor through `editormcp`, authors need to watch each step unfold in plain words, approve or deny gated actions (such as headless playtests or edits under a cautious profile) without losing manual editing or keyboard focus, add follow-up instructions to a running request, and stop the agent from anywhere (including inside a modal dialog) while preserving completed edits for review. Although `permissions.yaml` rule evaluation (`#1010`), You-only trust boundaries (`#1015`), and the Agent panel (`#1009`, `#1019`) already exist on `main`, `editormcp.Server` previously returned an immediate `PermissionError` on `Ask` when `Options.OnPermissionAsk` was nil rather than pausing on an interactive Agent panel permission card, lacked `agent_report_step`, per-request denial memory, mid-request `"Add to this request"` follow-ups, and unified `⌘.` Stop cancellation for waiting asks and in-flight tool calls.

## Decision

Integrate live step streaming, interactive permission cards, follow-ups, and Stop directly onto the existing `Editor` agent state (`packages/game/editor/agent.go`, `agent_live_steps.go`, `agent_ui.go`, `agent_live_steps_ui.go`), the existing versioned `permissions.yaml` engine (`packages/game/editor/permissions.go`), and `editormcp.Server` (`packages/game/editor/editormcp/agent.go`, `agent_live.go`, `server.go`):

- **Unified `permissions.yaml` and You-only enforcement**: Reuse `permissions.yaml` (`editor.PermissionsVersion = 1`) and `checkYouOnlyTargets` as the single source of truth—no duplicate permissions schema or file format. Every mutating/running MCP call checks You-only targets first and evaluates `permissions.yaml` before opening any permission card or running a handler.
- **Non-blocking interactive MCP mediation & streaming**: When `Options.OnPermissionAsk` is nil and either a live Agent request is active (`Editor.HasActiveLiveAgentRequest`) or the server runs against a UI loop (`Options.Run != nil`), `editormcp.Server.runManagedToolCall` pauses `Ask` calls on the MCP goroutine (never blocking the editor UI goroutine) and renders a Spec `RoleAlertDialog` (`LiveRegionPolite`) card inside `AgentPanelView`. Headless inline callers without an active request keep returning `*PermissionError` immediately for compatibility with `#1010` callers.
- **Live steps & `agent_report_step`**: Starting a request immediately inserts a gray `AgentStepPlaceholder` step (guaranteed within 300 ms); tool calls stream plain-words steps with at most one active violet spinner and green checks when done. `agent_report_step` lets agents report steps in their own words with touched object IDs and drains queued `"Add to this request"` follow-ups.
- **No automatic retry after denial & safe Allow now**: Denying a card records `(action, target)` on the active request, renders `"You denied … · continued without it"` with `"Allow now"`, records the unchecked item (such as `"Not playtested"`) in the turn's review summary, and returns a non-fatal structured `PermissionDeniedResult` immediately on any repeated identical call in the same request without re-opening a card. Starting a new request clears per-request denials. Clicking `"Allow now"` runs the tool on demand inside a WaitGroup-tracked, server-cancellation-aware, `projectGen`-verified, You-only-checked goroutine.
- **Always persistence with Undo**: Choosing `"Always"` (`⌥↵` when focused) calls `Editor.ApproveAlwaysPermission`, inserting the escaped concrete `Allow` rule ahead of the matching `Ask` rule in `permissions.yaml` and showing the existing `"Always permission rule saved"` toast with **Show** and **Undo**.
- **Focus stability, Spec rule 05, and global Stop (`⌘.`)**: Permission cards never steal focus unless `Editor.ShouldFocusAgentPrompt()` is true; clicking the header `"Waiting for you"` chip focuses the card on demand. While focused, `⌘↵` allows once, `⌥↵` approves Always, `⌘⌫` denies, `↑`/`↓` cycle buttons, and `Esc` leaves the card unanswered while the agent keeps waiting. The working footer (`"Add to this request"` + `Stop`) mounts outside `threadBody`'s dynamic scope so streaming steps never remount or steal focus from the input. `Stop` and global `⌘.` (`OverModals: true`) cancel both CLI runners and live MCP requests, close `liveStopCh`, keep completed steps and edits pending for review (pausing autosave via `HasOpenReview()` until kept or reverted), and show the `"Agent stopped"` toast with **Review**; `Esc` never stops the agent.

## Alternatives considered

### Introducing a separate `.nsgo/permissions.yaml` and parallel `AgentPanelView`

Strongest argument: Keeping `#1012` isolated from `#1009`/`#1010` would avoid touching `agent.go` and `permissions.go`.

Rejected because `main` already ships `permissions.yaml`, `Settings › Permissions`, `You-only` holds, and `RightRailView`/`AgentPanelView`. Duplicating them would create two conflicting permission files, break `Settings › Permissions`, and bypass You-only boundaries.

### Blocking on permission answers inside `Server.Editor` on the UI goroutine

Strongest argument: Evaluating and waiting in one `Server.Editor` closure would simplify state transitions.

Rejected because blocking the UI goroutine freezes the editor so the user could neither click Allow/Deny/Stop nor continue manual scene editing while the agent waits. Waiting on channels on the MCP goroutine keeps the editor responsive.

## Consequences

- **Benefits**: Authors watch every MCP tool call and `agent_report_step` live in plain words, answer Spec permission cards without focus theft, persist `"Always"` rules directly into the project's canonical `permissions.yaml`, steer running requests with `"Add to this request"` without input remounts, edit the scene concurrently, and stop safely with `⌘.` from anywhere including modal dialogs.
- **Costs**: Adds one new built-in read-only MCP tool (`agent_report_step`) to `editormcp.Server` and `editormcp.Proxy`, and couples `AgentTurn` review state into `HasOpenReview()` so unkept scene edits pause autosave until reviewed.
- **Verification**: Verified with `go tool nsgo check`, `gameschema -check`, `tools/changelog check`, `bun run verify-notes`, `-race` unit/UI tests in `packages/game/editor` (placeholder, live steps, input focus stability, Spec rule 05 card focus/shortcuts, `⌘.` inside modal dialogs, `Esc` non-stop/non-deny, autosave pause), scripted MCP client integration tests in `packages/game/editor/editormcp` (Allow once, Always + `permissions.yaml` + Undo, non-retried Deny + Allow now + new-request re-ask, follow-ups, Stop cancellation, You-only enforcement), and real macOS `nsgotest` screenshots in Dark and Light themes.
