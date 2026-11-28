# Agent Note: Agent panel, background task and automation turns pin the selection when they start

Status: implemented

## Problem

Since [the edit-target fix](../bug-fix/2026-11-28-editor-mcp-edit-target.md), an editor MCP edit tool called without `id` writes to the selection pinned by the open agent turn (`AgentTurnContext.SelectionIDs`, read through `Editor.OpenAgentTurnPin`), and is refused when the turn pinned none. Only the in-place `⌘L` composer recorded a selection (`submitAgentTurn` in `pkg/editor/ask_in_place.go`). Turns started from the Agent panel (`StartAgentRequest` in `pkg/editor/agent_live_steps.go`, `submitAgentTaskPromptInternal` in `pkg/editor/agent_builtin.go`) and from background tasks and automations (`launchDefaultTaskRunner` in `pkg/editor/tasks.go`) pinned nothing, so an agent in those turns that left out `id` was refused, although the person's request ("make this one faster") was about the selection at the moment it was sent (#1983).

## Decision

Those three turn constructors fill `AgentTurn.Context` with `Editor.captureAgentTurnContext(scenePath)` (`pkg/editor/agent.go`): the scene path the turn is about, the open scene's name, `SelectedIDs()` with the display names of `SelectedObjects()`, the active layer and the view mode, taken once when the turn starts. A turn started with nothing selected pins nothing, so id-less edits stay refused there, as before.

When `scenePath` names a scene other than the open one (`effectiveScenePath`), which a background task queued from another scene can do, only the scene path is recorded and no selection: the open scene's selection is not about the task's scene, and pinning its IDs would make an id-less edit hit a same-named object once that scene opens.

The capture deliberately leaves out the scene YAML and the viewport PNG that the `⌘L` turn carries: those turns already send the scene through `AgentRequest`, and a PNG per background turn would grow the retained turn history.

## Alternatives considered

- **Record the selection when a background task is queued rather than when it starts**: closer to the person's intent for a task that waits in the queue. Rejected for now because `AgentTask` stores no selection, and the request the runner sends already reads `e.SelectedID` at launch (`req.SelectedID`), so pinning at launch keeps the pin and the request text consistent. Storing a selection on the task is a separate format change.
- **Have the agent copy the selected ID out of the request text**: works without a pin, but relies on the agent copying it correctly and leaves the target invisible in the turn's context chips.

## Consequences

Selection changes stay free for the person while an agent works. `agent_turn_get` (`selectionIds`, `selectionNames`) and the Agent panel's selection chip (`agentTurnContextChips` in `pkg/editor/agent_ui.go`) show the pin for these turns too. The background task list does not show a selection chip; that part of #1983 is not done here. `TestAgentPanelTurnPinsTheSelectionItStartedWith` (`pkg/editor/editormcp/selection_target_test.go`) covers an Agent panel turn, and `TestCaptureAgentTurnContextSkipsTheSelectionOfAnotherScene` (`pkg/editor/agent_turn_context_test.go`) covers the other-scene guard.
