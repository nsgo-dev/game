# Agent Note: Task keeps, task dismissals and permission answers are person-only

Status: implemented

## Problem

The editor's agent features follow one rule: the agent proposes and the person decides. Turn reviews already enforce it (`KeepAgentTurn`, `RevertAgentTurn` and `KeepAgentTurnReview` refuse an agent actor with `ErrAutomationYouOnly`), and no `turn_*` MCP tool keeps or reverts a turn. Background tasks did not: `tasks_control` forwarded `keep` to `Editor.KeepTask` and `allow` / `deny` to `Editor.ResolveTaskPermission`, and neither checked the caller, apart from `KeepTask` refusing an agent for automation tasks only. Any connected agent, including a task's own session, could keep its own task's changes or answer the **Needs you** prompt an `ask` rule in `permissions.yaml` put in front of it, and could dismiss a Ready to review task, which hides its unkept changes from review. `revert` reached any task, so one session could discard another task's pending work. `stop` reached any task in any state: `StopTask` moved even a Ready to review task to stopped without reverting its edits, which ends the review and lets autosave write the unkept edits, a keep under another name.

## Decision

The check lives in the editor methods, keyed on the per-call `Editor.AgentMutating()` flag (through `taskDecisionByAgent`), which every MCP tool call sets on the editor goroutine through `Server.asAgent` → `Editor.RunAsAgent`. It is not keyed on `AgentActorActive()`: `agentActorDepth` is one process-wide counter, and `startAutomationTask` holds it through `WithAgentActor` for an automation's whole run on a background goroutine, so the person's own Allow, Deny, Keep and Dismiss clicks would be refused while any automation runs, and an automation waiting at an Ask step could never be answered. UI clicks run on the editor goroutine one at a time, so they never see `agentMutating` set. `KeepTask`'s existing `ErrAutomationYouOnly` branch for automation tasks stays on `AgentActorActive()` as before.

- `KeepTask` refuses an agent actor with `ErrTaskDecisionYouOnly` (`ErrAutomationYouOnly` for an automation task, as before).
- `ResolveTaskPermission` refuses an agent actor with `ErrTaskDecisionYouOnly` for allow and deny alike.
- `DismissTasks` skips Ready to review tasks for an agent actor; Failed tasks stay dismissable.
- `StopTask` refuses a Ready to review task for every caller (`ErrTaskDecisionYouOnly` wrapped for an agent): the Tasks panel never offers Stop there, and the person ends a review with Keep or Revert.

`tasks_control` (`pkg/editor/editormcp/tasks.go`) mirrors this with explicit errors so the agent reads why: `keep`, `allow` and `deny` are always refused, `dismiss` of a Ready to review task is refused, and `revert` and `stop` are allowed only when `task_id` is the task the calling session is bound to (`Call.taskID`), and `stop` never once it is Ready to review; an unbound session reverts and stops nothing. The action enum is unchanged so existing clients get a readable refusal rather than a schema error. `retry`, `open` and `focus` stay available to agents.

## Audit of decision paths reachable by an agent

Agents reach the editor only through `editormcp` tools. The editor methods those tools call that end a review or answer an ask:

- `KeepTask`, `ResolveTaskPermission`, `DismissTasks` (Ready to review): person-only, as above.
- `RevertTask`: allowed only for the calling session's own task.
- `StopTask`: allowed only for the calling session's own task, and refused for a Ready to review task for every caller.
- `KeepTileReview` through `tile_review_keep`: stays an agent tool. The tile review is a grouping the agent opts into with `review: true` (it may pass `review: false`), and keeping it only folds the agent's own tile edits into one undo step; the turn or task review that contains those edits is still the person's.
- `ResolveMergeConflict` / `ApplyMergeCustomYAML` through `merge_resolve`: picks sides in a Git conflict the person started; not a review or ask.
- `ApproveAlwaysPermission` in `runManagedToolCall` and `checkPermission`: runs only after the host's `OnPermissionAsk` callback returns the person's **Allow always** answer.

Every other keep, approve or confirm method (`KeepAllReviewChanges`, `KeepTurnReview`, `KeepCutsceneReview`, `KeepDataTableReview`, `KeepCodeReview`, `KeepDocReview`, `KeepSketchReview`, `ApproveAgentPlan`, `AnswerPlanQuestion`, `AllowPermissionOnce`, `AllowPermissionAlways`, `ConfirmReleaseUploads` and the rest) has no MCP tool. `TestNoToolLetsAgentKeepReviewOrResolveAsk` fails when a registered tool's name suggests a decision (keep, approve, accept, allow, confirm, resolve, answer) and is neither refused nor listed with the reason it is not a person-only gate.

## Alternatives considered

- **Refuse only in the MCP tool**: smallest diff, but leaves `KeepTask` and `ResolveTaskPermission` open to any other agent-actor path added later; the existing You-only boundaries (`ErrAgentSettingsYouOnly`, `ErrAutomationYouOnly`) live in the editor methods for that reason.
- **Remove `keep`, `allow` and `deny` from the `tasks_control` schema**: makes the refusal a schema validation error the agent cannot explain to the person, and breaks clients that send the action; refusing in the handler with `ErrTaskDecisionYouOnly` keeps the contract readable.
- **Also refuse `tile_review_keep`**: considered because the tile review has Keep and Revert like a person's review, but an agent can skip the tile review entirely with `review: false`, so refusing its keep would break the documented tool without protecting anything.

## Consequences

- **Benefits**: an agent, or an instruction injected into a file it read, can no longer land a task's changes or pass an `ask` rule without the person; docs and behavior agree that only the person decides.
- **Costs**: agents and scripts that kept tasks or answered prompts through `tasks_control` now get an error and must leave those steps to the person. An unbound MCP session (one not created by `ConnectTask` or the proxy's task binding) can no longer revert background tasks.
- **Testing**: `TestTasksControlRefusesAgentReviewAndPermissionDecisions` covers keep, allow and deny from the task's own session, another task's session and an unbound session, foreign revert and stop, stop and dismissal of a Ready to review task, the editor-level `ErrTaskDecisionYouOnly`, and the person's keep and allow still succeeding. `TestTaskDecisionsStayThePersonsWhileAutomationRuns` answers a permission prompt and keeps a parallel task as the person while an automation holds `WithAgentActor` in its runner, and `TestStopTaskRefusesReadyToReview` checks the Ready to review task keeps its open review after a refused stop. Related: [background task list](../feature/2026-11-26-editor-background-task-list.md).
