# Agent Note: Editor MCP edit tools take their target from the id or the turn's pin, never the live selection

Status: implemented

## Problem

`object_set_field` (`pkg/editor/editormcp/inspector.go`) and `object_duplicate`, `object_group`, `object_move_under` and `object_visibility` (`pkg/editor/editormcp/object_menu.go`) defaulted an omitted `id` to the selected object, and read `ed.SelectedID` (or `ed.SelectedIDs()`) twice in two separate `Call.Editor` calls: once to choose the object to hold for a parallel task, and again as the write target. A click between the two left the hold on one object and the write on another, which no task's hold protected. Even without that race, an agent that omitted `id` after the person moved the selection mid-turn edited whatever was selected at the moment of the call, not the object the request was about. The person and the agent share one editor, and clicking is the most common thing the person does while an agent works, so a click could silently redirect an agent's write (#1926, part of #1923).

## Decision

The selection is context, never a write target. Each of the five tools resolves its target once, before it takes holds, with `Call.resolveObjectTarget` (`pkg/editor/editormcp/target.go`), and passes the same IDs to `acquireObjectHolds` and to the write; the result reports them (`object`, `sourceId`, `children`, `id`). Resolution, in order:

- The call's explicit `id` (or `ids` for `object_group`).
- Otherwise the selection pinned by the open agent turn the call belongs to, which `Editor.OpenAgentTurnPin(taskID)` (`pkg/editor/agent.go`) returns. A task session uses its task's own turn (`AgentTask.TurnID`); any other session uses the most recent turn the person sent, skipping the `External` turns the editor records for a terminal agent's tool calls and `Source: "task"` turns. A turn is open while its status is `pending` or `running`.
- Otherwise the call is refused with an `EditError` and nothing is held or changed. Outside a turn the fix reads `pass id; selection_get shows the selected object`; in a turn whose request pinned nothing it points at `agent_turn_get`; a tool that edits one object refuses a pin of several.

A pinned target that is no longer in the open scene (checked by `objectTarget.missing` both before the hold and again inside the write, with `FindObject` and `FindResolvedObject`) is refused with the problem `the object the request was about was removed or renamed` and a fix to read the scene with `scene_read`, distinct from the tool's own unknown-object error for an explicit `id`. A pin whose turn was sent from another scene (`AgentTurnPin.SceneOpen` false) is refused the same way, naming that scene.

Read-only tools keep the selection default: `object_locate_yaml`, `object_copy_yaml`, `object_find_usages`, and `object_move_under` with `listCandidates: true`, since none of them can write to the wrong place.

### object_paste_yaml's destination layer

`object_paste_yaml` places a new object rather than editing an existing one, but it had the same shape: without `layer` it held `layer:` plus `ed.ActiveLayer`, read in one `Call.Editor` call, then pasted in a second through `Editor.PasteObjectYAML(yaml, "")` or `Editor.PasteObject()`, which picked the selected object's layer at write time (#1982). It now resolves the destination once with `Call.resolvePasteLayer` (`pkg/editor/editormcp/target.go`), before the hold, and passes that layer to the `layer:<name>` hold, to `PasteObjectYAML` and to the result's `layer`. Resolution, in order:

- The call's explicit `layer`.
- Otherwise the layer of the objects the open turn pinned (`OpenAgentTurnPin`, looked up with `FindResolvedObject`). A pin spread over several layers is refused and asks for `layer`; a pinned object no longer in the open scene, or a pin from another scene, is refused as for the object tools, with fixes that name `layer`.
- Otherwise the layer a copied scene YAML names, when the open scene has it (`Editor.ObjectYAMLLayer`). This is part of the call's own input, not state the person changes, so it is safe to default to.
- Otherwise an `EditError` whose fix reads `pass layer; scene_read lists the layers`. The live selection and `ActiveLayer` are never read.

The clipboard is read once, in the same call, with `Editor.ObjectClipboardText`, and that text is what gets pasted, so the YAML whose layer was resolved is the YAML placed. `Editor.PasteObjectYAML` keeps its selection-then-active-layer default for an empty `layerName`, since the person's own paste in the editor should land by the selection; the MCP tool simply never passes an empty one. When the YAML has a top-level `id`, the hold stays on that ID as before, and only the layer the paste lands on changed.

The holds come from [the background task list](../feature/2026-11-26-editor-background-task-list.md), the object tools from [the object context menu](../feature/2026-11-26-editor-object-context-menu.md), and the pin from [ask in place](../feature/2026-11-26-editor-ask-in-place.md).

### Which turns pin a selection

Only the in-place `⌘L` request (`submitAgentTurn` in `pkg/editor/ask_in_place.go`) records `AgentTurnContext.SelectionIDs` today. Agent panel requests, background tasks and automations open turns with an empty pin, so inside those turns the five tools need `id`; their requests already carry the selected ID in `AgentRequest.SelectedID`, which the agent can pass back explicitly.

## Alternatives considered

- **Read the live selection once, before the hold, and use that value for the write**: the smallest change, and it closes the hold-versus-write race. Rejected because it still edits whatever is selected when the call arrives, so a click after the person sent the request redirects the write, which the issue names as the second half of the bug.
- **Keep the selection default outside a turn**: preserves terminal agents that say "edit the selected object" without a turn. Rejected because the same click race applies outside a turn, and the refusal costs one round trip: `selection_get` returns the ID to pass.
- **Opaque handles minted by reads, as opengame's studio uses**: makes a stale target detectable by construction. Rejected for now because object IDs are already stable within the open scene and readable by the person, and the removed-or-renamed check covers staleness; the issue puts handles out of scope.
- **Default `object_paste_yaml` to the active layer read once before the hold**: closes the race between hold and paste with the smallest change. Rejected for the same reason as the live selection: the active layer is the person's state, and changing it after sending the request would still redirect the paste.
- **Pin the selection for Agent panel, task and automation turns too**: would let id-less edits work in every turn. Not done here because a non-empty `SelectionIDs` also adds a selection chip to those turns in the Agent panel thread (`agentTurnContextChips`), a visible change outside this fix; it is a follow-up.

## Consequences

- **Benefits**: the object a parallel task holds is the object it writes, whatever the person clicks; an id-less edit inside a `⌘L` turn edits what the request was about; a request about a since-deleted object fails with an error that says so instead of editing the current selection.
- **Costs and known limits**: agents that omitted `id` outside a turn, or inside a panel, task or automation turn, now get an `EditError` and must pass `id`. A `⌘L` turn the agent never closes with `agent_turn_reply` stays open, so a later id-less edit from the same non-task session still targets its pin until the person sends a newer request. `object_paste_yaml` without `layer` outside a turn, or in a turn that pinned nothing, now refuses a bare object mapping instead of pasting onto the selection's or the active layer; a copied scene YAML still pastes onto the layer it names.

## Verification

`pkg/editor/editormcp/selection_target_test.go`: `TestEditToolsWithoutIDTargetTheTurnsPinnedSelection` (each of the five tools, selection moved after send and again while the call waits on its hold via `Server.SetHoldBlockedHook`), `TestObjectSetFieldHoldAndWriteUseOneResolvedTarget`, `TestEditToolsWithoutIDOutsideATurnAreRefused` (scene, undo history and file unchanged) and `TestEditToolsRefuseARemovedPinnedTarget`. `pkg/editor/editormcp/paste_layer_target_test.go`: `TestObjectPasteYAMLHoldAndWriteUseOneResolvedLayer` (a pinned turn; the hold-blocked hook selects an object on another layer and makes that layer active, and the paste still lands on and reports the held layer), `TestObjectPasteYAMLReportsTheLayer`, `TestObjectPasteYAMLWithoutLayerOutsideATurnIsRefused` and `TestObjectPasteYAMLRefusesARemovedPinnedTarget`. Run `go tool nsgo test ./pkg/editor/editormcp/`.
