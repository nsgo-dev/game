# Agent Note: Editor connections panel in the inspector

Status: implemented

## Problem

Scene files support declarative event-to-handler connections checked against `sceneformat.ComponentRegistry` (`sceneformat.ValidateConnections` and `sceneformat.Connect`), but `packages/game/editor` could only preserve them on open and remove/restore them when deleting an object or component. Wiring, editing, inspecting, or removing connections still required hand-editing YAML. Issue #872 asks for a Connections UI for the selected object supporting target picking from the tree or viewport, typed handler params, `once`, editing, removal, jumping to the other end, and undoable mutations refused before history when `ValidateConnections` fails.

## Decision

- Add a dedicated Connections model and command layer on `Editor` (`packages/game/editor/connections.go`): `ObjectEvents`, `ObjectHandlers`, `ObjectConnections`, `SceneObjectIDs`, `AddConnection`, `UpdateConnection`, `RemoveConnection`, `JumpToConnectionOtherEnd`, and session-only target pick mode (`BeginConnectionPick`, `PickingConnectionTarget`, `CancelConnectionPick`).
- Validate every `AddConnection` and `UpdateConnection` candidate on a shallow copy of `Doc` (with `sceneformat.Validate` and, when a component registry is set, `sceneformat.ValidateConnections`, resolving first through `SceneResolver` when `Doc.RefersToScenes()` is true) before pushing a command onto `History`. Refusals call `e.report(err)` so both Go callers and UI actions surface the exact reason in `Status` and the Output log while leaving `Doc` and `History` untouched.
- Render the Connections section inside `InspectorView` (`packages/game/editor/inspector_connections.go`) directly below Components rather than adding another docked panel. Declared events and outgoing/incoming connections react to `rev`, while the inline Connect/Edit form memoizes its param controls by target+handler schema so typing into typed param inputs (`registeredFieldControl`) preserves focus.
- Qualify `ObjectEvents` and `ObjectHandlers` `Ref` automatically (`component.name`) whenever two registered components on the same object declare the same bare name, while keeping bare names when unique.
- Intercept clicks in 2D (`HandleCanvasPress`), 3D (`HandleViewport3DPress`), and the scene tree (`SceneTreeView`) while `connPick != nil` so picking a target delivers its ID to the active form without changing `SelectedID`, cancelling pick mode on Escape, Play, scene open, or selection change.
- Related inspector work: [Editor inspector adds and removes components and properties, and edits sprite, origin, flip and material](2026-11-25-editor-inspector-components-sprite.md).

## Alternatives considered

- **Separate bottom/side dock for Connections (like Godot's Signals tab)**: gives connections their own tab, but splits an object's properties/components from its wiring and competes for space with the existing bottom panels (Animation, Problems, Search, Output, Mixer, Input). Keeping Connections as a section in `InspectorView` follows the selected object automatically and keeps all per-object authoring in one scrollable column.
- **Restrict the Handler field to a strict `ui.Select` dropdown**: prevents typing a nonexistent handler in the UI, which makes testing invalid-handler refusal from the UI impossible and blocks editing scenes without a component registry. Combining a text `ui.Input` with quick-pick buttons for the target's registered handlers (matching `addComponentControl`) keeps happy-path selection one click away while allowing arbitrary handler names to hit `ValidateConnections` refusal cleanly.
- **Execute invalid connections as a `fallibleCommand` inside `History`**: still avoids recording on failure, but validating synchronously in `AddConnection` / `UpdateConnection` before creating a command keeps the command implementations trivial and guarantees no history hook fires on refusal.

## Consequences

- **Benefits**: Authors can wire, inspect, edit, toggle `once`, jump between endpoints, and remove connections entirely in the editor with full Undo/Redo, deterministic YAML round-tripping, and live `sceneformat.Connect` compatibility. Invalid wiring never enters `History` or corrupts the open scene.
- **Costs**: Picking the already-selected object itself from the scene tree while in pick mode does not trigger a cursor-change event in `ui.Tree`, so self-connections pick the target via the searchable Target dropdown or the viewport. In instancing/inheriting scenes, `AddConnection` / `UpdateConnection` resolve the scene once per mutation to run `ValidateConnections`.
- **Verification**: `packages/game/editor/connections_test.go` verifies the full `nsgotest` UI flow (connect `button.pressed` -> `door.open` with `speed: 2.5`, save/reload byte-identical YAML, execute via `sceneformat.Connect` + `Emit`, Undo back to exact pre-connect bytes, Redo), UI and Go API refusals before `History` (missing handler, target without handlers, wrong param type, out-of-range param, unknown arg, missing event, missing object, duplicate canonical connection, invalid update), 2D viewport + 3D viewport + scene tree target picking, editing + `once` + handler-switch arg cleanup + jumping both ways + removing + Undo/Redo, ambiguous event qualification, `ErrPlaying` guards, and no-registry behavior.
