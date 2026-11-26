# Agent Note: Anchored ⌘L In-Place Agent Composer and MCP Turn Delivery

Status: implemented

## Problem

Authors editing a scene in `game/editor` previously had no anchored in-place way to ask an attached MCP agent about selected objects right inside the viewport. Triggering `mod+l` or `"Ask the agent about this"` from the object context menu (`[object context menu](2026-11-26-editor-object-context-menu.md)`) only fired a single-object `AgentPromptRequest` draft callback with no UI composer, no multi-selection or scene-level ask flow, no attached viewport image, no Agent panel thread, and no `editormcp` resource/tool to deliver the turn context to an external agent.

## Decision

The editor provides an anchored `⌘L` in-place composer (`AskInPlaceComposerView`), a tabbed right panel combining Inspector and Agent (`RightPanelView` / `AgentPanelView`), and MCP turn delivery in `editormcp` (`[editor MCP server](../architecture/2026-11-25-editor-mcp-server.md)`).

### Shortcut, focus, and placement contracts

- `CmdAskAgent` (`"agent.askInPlace"`) is registered in the unified command registry under `ScopeGlobal` with default shortcut `"mod+l"`, bound with `core.KeyNotEditing` so pressing `⌘L` while typing in the Inspector or any text field never opens the composer. The duplicate ad-hoc `mod+l` listener in `context_menu_ui.go` is removed so rebinding `CmdAskAgent` updates the shortcut everywhere, including the context menu item.
- `AskAgentAboutObject(id)` preserves `AgentPromptRequest` and `Editor.OnAskAgent` for backward compatibility and also opens the anchored composer targeting the full current multi-selection when `id` is part of it (or `[id]` otherwise). Both the viewport context menu and the Scene Tree context menu route through this path.
- `OpenAskInPlace()` captures `core.FocusedRef(e.scope)` before focusing the composer's real labelled multi-line `ui.Input` (`ask-in-place-input`). Pressing `Esc` calls `CloseAskInPlace()`, which closes without sending, leaves the selection intact (viewport Escape is suppressed while `AskInPlaceOpen()` is true), and restores keyboard focus to the captured `core.Ref`.
- The composer mounts inside `editor-viewport-container` and clamps its coordinates via `ComputeAskInPlacePlacement` against the laid-out viewport dimensions (preferring below the selection bounds and flipping above when clipped by the bottom margin or hint pill). Because `editor-viewport-container` terminates at the left edge of `editor-right-panel`, clamping within `[margin, vw-margin]` guarantees the composer stays on screen and never overlaps the Inspector or Agent panel.

### Suggestions, right panel, and MCP turn delivery

- Contextual suggestions (capped at 2: e.g., `"Make them a prefab variant"` and `"Space them evenly"` for multi-selection, tailored pairs for single objects or the open scene) start unselected (`selectedSuggestion = -1`). Arrow keys cycle `-1 <-> 0 <-> 1`, `Enter` sends the highlighted suggestion (or the typed draft when `-1`), `⇧↵` inserts a newline, `⌘↵` sends, and any text change resets `selectedSuggestion = -1` so suggestions never block free typing.
- `RightPanelView` defaults to `RightPanelInspector` so existing inspector workflows and tests remain unchanged, while exposing `Agent` (`⌘1`, `CmdAgentTab`) and `Inspector` (`⌘2`, `CmdInspectorTab`) tabs. Submitting from the in-place composer creates an `AgentTurn` with `Source: "in_place"` and `SkipPlan: true` (matching `Plan.dc.html`), attaches `SelectionIDs`, `SelectionNames`, `ScenePath`, `SceneYAML`, and a viewport PNG rendered via `RenderViewportPNG` (`DrawViewport` + `gamemcp.RenderPNG`), switches the right panel to `RightPanelAgent`, and invokes `Editor.OnAgentTurn`.
- `editormcp` exposes the turn over both the change-stream resource `editor://agent/turn` (JSON payload + `image/png` blob, notified on `AgentTurnSeq()` changes with `nsgo.dev/by`, `nsgo.dev/revision`, `nsgo.dev/turnId`, and `nsgo.dev/skipPlan` metadata) and the `agent_turn_get` (`ActionRead`, returning structured metadata + `*mcp.ImageContent` PNG) and `agent_turn_reply` (`ActionEdit`, updating status and reply text in the Agent panel) tools.

## Alternatives considered

- **Floating modal dialog centered over the full window (like `CommandPaletteView`)**: simpler layout and focus trapping, with no selection-projection math. Rejected because `AskInPlace.dc.html` anchors the composer directly beside the selected objects inside the viewport so the author can inspect what they are referencing while typing, and must never overlap the right panel.
- **Replacing `InspectorView` with an Agent-only right panel by default**: matches `Main.dc.html` when starting in Agent mode. Rejected because existing editor workflows and tests assume the Inspector fields (`inspector-name`, `inspector-delete`, etc.) are mounted immediately on launch; defaulting `RightPanelView` to `RightPanelInspector` and switching automatically to `RightPanelAgent` when a turn is sent (plus `⌘1`/`⌘2` tabs) preserves backward compatibility while surfacing sent turns immediately.
- **Delivering only object IDs over `editor://selection` without a dedicated turn resource/tool**: avoids adding a new MCP resource and two tools. Rejected because `editor://selection` changes on every click and carries neither the user's prompt, `skipPlan` intent, nor the viewport PNG snapshot captured at send time.

## Consequences

- **Benefits**: authors can ask about one object, multiple objects, or the open scene in place with `⌘L` or right-click `"Ask the agent about this"` without leaving the viewport; every sent turn carries deterministic selection, scene YAML, `skipPlan`, and PNG context to both the Agent panel and connected MCP agents; all UI uses exclusively `SpecPalette` tokens in Dark and Light appearances.
- **Costs**: `Editor` imports `packages/game/mcp` for `RenderPNG` (acyclic, since `packages/game/mcp` has no dependency on `editor`), and `editormcp` now registers 41 built-in tools and 3 resources (`editor://scene`, `editor://selection`, `editor://agent/turn`).
- **Verification**: covered by `TestAskInPlaceSelectTwoObjectsSendAndShowsInAgentPanel`, `TestAskInPlaceInspectorTextFieldGuardAndEscapeRestoresFocus`, `TestAskInPlaceNoSelectionContextMenuSuggestionsAndClamping`, `TestComputeAskInPlacePlacementClampingEdgeCases`, `editormcp.TestAskInPlaceDeliversSelectionSceneAndViewportImageOverMCP`, and native macOS `nsgotest` screenshots in `editorcmd.TestAskInPlaceMacOSScreenshots`.
