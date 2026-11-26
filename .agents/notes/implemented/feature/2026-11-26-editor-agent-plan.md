# Agent Note: Editor agent plan proposals with clarifying questions and planned viewport ghosts

Status: implemented

## Problem

Large agent requests in the NSGo Game Editor (touching Go code, creating files, or editing multiple files under `permissions.yaml`) had no pre-edit planning step. Authors could not review proposed steps, answer clarifying questions, inspect planned scene placements as viewport ghosts and Scene Tree markers, remove or adjust steps before files changed, or stop a plan cleanly before step 1 writes anything (#1035).

## Decision

Add a structured, Spec-token plan lifecycle across `packages/game/editor` (`plan.go`, `plan_ui.go`) and `packages/game/editor/editormcp` (`plan.go`), integrating with `AgentPanelView`, `ViewportView`, `SceneTreeView`, `permissions.yaml`, and `AgentTurn` review checklists:

### Plan data model and non-mutating MCP contract (`packages/game/editor/plan.go`, `editormcp/plan.go`)
- `plan_propose` is registered with `ActionRead` because proposing a plan never writes any scene or project file; even under the `Cautious` permission profile, proposing a plan is allowed so the agent can ask before editing.
- On real MCP calls (`wait` defaults to `true`, forced `false` when `Call.onEditor` is true to avoid deadlocking synchronous automation handlers), `plan_propose` stages the `AgentPlan` in `PlanPhaseDraft` via `Editor.ProposeAgentPlan`, switches the right rail to `RightTabAgent`, and blocks off the editor goroutine on the plan's decision channel until the author approves (`ApproveAgentPlan` / `JustDoItAgentPlan`) or stops (`StopAgentPlan`).
- `ApproveAgentPlan` refuses while any clarifying question is unanswered (`UnansweredPlanQuestions() > 0`) or all steps are removed. `JustDoItAgentPlan` picks each unanswered question's `Suggested` option, records `PickedForYou: true` (`"Picked for you: …"`), and transitions to `PlanPhaseRunning`.
- `plan_step_update` (`ActionRead`) updates step execution progress, reports `stopped: true` when the author stops a run, and on completion (`CompleteAgentPlan`) attaches `AgentPlanReview` to the linked `AgentTurn` so the turn's review lists each plan step and marks which were done or skipped.

### Live planned ghosts, Scene Tree markers, and manual-edit pause (`plan.go`, `plan_ui.go`, `viewport.go`, `overlays_ui.go`, `scenetree.go`, `state.go`)
- Clarifying question options carry optional `StepTitleOverrides` and `GhostOverrides` (moving or replacing specific step ghosts by `GhostID`), and editable numeric step fields (`PlanStepField` with `Min`/`Max`/`Step`) drive `{field}` interpolation and `RepeatField` ghost expansion. Changing an answer or numeric stepper updates viewport ghosts, step titles, summary counts, and Scene Tree planned nodes immediately while leaving all files untouched.
- `drawPlanGhostsOverlay` renders agent-violet (`SpecPalette.Agent`) dashed outlines with step-numbered pills while planned/queued and switches the active running step's ghosts to solid outlines. The floating 2D viewport bar exposes a **Plan ghosts** toggle (`TogglePlanGhosts`), and a top-left legend explains dashed vs solid outlines.
- `SceneTreeView` displays a `"{N} planned"` indicator, step badges on touched existing layers, dashed planned node rows, and a **FILES IN THIS PLAN** footer striking out files whose owning steps were all removed.
- While `PlanPhaseRunning`, manual `MoveObject` / `DeleteObject` calls on planned objects (`IsObjectPausedByPlan`) are refused (agent calls inside `WithAgentActor` remain allowed), and stopping before any step finishes preserves `NothingWritten: true` with **Edit plan** and **Run again** actions.
- `EvaluatePlanPermissions` derives the footer rationale from the active `permissions.yaml` rules (`Editor.Permissions()`) and links directly to **Settings › Permissions** (`SettingsPagePermissions`).

### Memoization and Spec Dark/Light discipline (`plan_ui.go`, `agent_ui.go`)
- `agentPlanCardView` mounts as an independent sibling inside `AgentPanelView`'s scroll container with a memoized key over `(dark, lang, plan.seq, editStep)`. Typing in `plan-step-note-input` increments `plan.noteRev` rather than `plan.seq`, keeping the `ui.Input` mounted across keystrokes and unrelated `rev` ticks.
- Every surface uses exclusively `SpecPalette` tokens (including `Accent.OnFill` on solid accent buttons and `Agent.OnFill` on solid violet step badges) for WCAG 2.2 AA contrast in both Dark and Light appearances.

## Alternatives considered

1. **Returning immediately from `plan_propose` and requiring the agent to poll a separate `plan_status` tool**: simpler server handler, but forces external MCP agents to loop-poll while waiting for human input and mismatches issue #1035's contract that `plan_propose` returns the person's decision directly. Blocking off the editor goroutine on a decision channel (with an opt-out `wait: false` and automatic non-blocking fallback when `onEditor` is true) satisfies both interactive MCP sessions and synchronous automation tests.
2. **Hardcoding Room-2-specific gate/slime logic inside the viewport renderer**: faster to prototype against `Plan.dc.html`, but unusable for real user prompts. Data-driven `GhostOverrides`, `StepTitleOverrides`, `{field}` interpolation, and `RepeatField` ghost expansion let any MCP agent express reactive plans cleanly.
3. **Gating `plan_propose` as `ActionEdit` in `editormcp`**: would cause `Cautious` permission rules to block plan proposals before the author ever sees the Plan card. Registering `plan_propose` and `plan_step_update` as `ActionRead` matches `problem_propose_fix` (`packages/game/editor/editormcp/problems.go`) since neither tool mutates any file.

## Consequences

- **Benefits**: authors see, question, edit, and approve or stop multi-step agent work before any file changes; viewport ghosts and Scene Tree markers stay live and accurate as options and counts change; Stop before step 1 guarantees zero file writes and offers Edit plan / Run again; turn reviews record step completion against the plan.
- **Costs**: adds plan state and UI surfaces across `AgentPanelView`, `ViewportView`, `SceneTreeView`, and `toolbarView` that must stay synchronized on project switch (`resetPlanForProject`) and locale updates (`locales/{en,zh}.yaml`).
- **Verification**: `packages/game/editor/plan_test.go` (ghosts follow answer changes & toggle, nothing written before approval, Just do it, Stop before step 1, Edit plan / Run again, manual-edit pause, Permissions link, Dark + Light UI screenshots), `packages/game/editor/editormcp/plan_test.go` (MCP `plan_propose` 2 questions / 5 steps flow, Approve refusal with 1 answer, step removal & count edit, decision payload, and `plan_step_update` turn review), `TestLocaleCatalogsComplete`, desktop + iOS `nsgo check`, `tools/changelog check`, and `bun run verify-notes`.
