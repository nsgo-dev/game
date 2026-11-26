# Agent Note: Editor Viewport Sketch Layer, Reading Card, and Scene Edit Hold Gate

Status: implemented

## Problem

Authors often need to point at empty areas of a 2D level—where no object or tile exists yet—and describe what an agent should build there ("3 spikes", "platform, back and forth", "locked until the lever"). Relying solely on text prompts forces manual coordinate lookup, while letting an agent immediately mutate the scene risks misinterpreting rough intent. At the same time, annotations must never leak into `*.scene.yaml` or pollute scene undo history, must remain accurate across viewport zoom and pan levels, and must not crowd the compact 2D `editor-toolbar`.

## Decision

The editor adds a session-only 2D viewport sketch layer (`packages/game/editor/sketch.go`, `sketch_png.go`, `sketch_ui.go`) paired with `editormcp` tools (`packages/game/editor/editormcp/sketch.go`) and a confirmation hold gate on mutating scene tools:

- **Strict scene-coordinate session state**: Sketch marks (`Pen` polyline, `Arrow` start → end, `Box` rect, `Text` point + label) live exclusively on `Editor.sketch` and per-tab `sceneTab.sketch`, never on `sceneformat.SceneDoc` or `Editor.History`. Pointer drags convert canvas points immediately via `Editor.CanvasToWorld` and re-project during rendering via `Editor.WorldToCanvas`, keeping geometry invariant across zoom and pan changes. Sketch undo uses an isolated mark snapshot stack (`undoStack`) so undoing a mark never alters scene history or marks a scene dirty.
- **Dedicated `ScopeSketch` keyboard scope**: Entering sketch mode (`D` in `ScopeViewport2D`, or `View > Sketch`) activates `ScopeSketch` (priority `5` while sketching in 2D), enabling single-key tool shortcuts (`P` Pen, `A` Arrow, `B` Box, `T` Text, `I` Add mark at cursor, `H` Hide/Show marks, `⌘Z` Undo last mark) without colliding with `ScopeViewport` / `ScopeTile` bindings or adding extra buttons to `editor-toolbar`. `Esc` exits sketch drawing mode while retaining marks until sent, cleared, or kept.
- **Structured payload + rendered PNG**: `Editor.SendSketch` emits a `SketchAgentRequest` carrying `"Sketch · N marks"`, structured marks in scene coordinates, and a software-rasterized PNG (`RenderSketchViewportPNG` via `packages/game/mcp.RenderPNG` plus a 5×7 bitmap text pass for numbered badges and labels).
- **MCP reading card and hold gate**: Sending a sketch transitions the hold state to `SketchHoldPending` (and `SketchHoldReading` once the agent calls `sketch_reading`). While `Editor.SketchHoldActive()` is true, `scene_edit` and other mutating scene MCP tools (`tile_paint`, `tile_autotile`, `tile_erase`, `object_paste_yaml`, `object_duplicate`, `object_group`, `object_move_under`, `object_set_field`) refuse cleanly with a structured `*EditError` and leave the scene and history untouched. `ApproveSketchReading` (**Go ahead**) or `ReplyToSketchReading` releases the hold and captures a pre-edit scene snapshot; `CorrectSketchMark` (**Correct a mark**) reopens sketch mode with that exact mark selected while keeping the hold active.
- **Review overlay and Keep / Revert symmetry**: Approved `scene_edit` calls record changes into `sketchReviewState` (opening the review card with a **Sketch overlay** toggle that renders the sketch as a faint dotted reference over added/changed outlines). Both `KeepSketchReview` and `KeepTileReview` clear the sketch layer; both `RevertSketchReview` and `RevertAllTileReview` restore the scene while preserving the sketch marks so the author can send them again.

### Related notes

- Builds on [Editor MCP Server](../../implemented/architecture/2026-11-25-editor-mcp-server.md) for tool registration, `EditError` refusals, and in-memory MCP testing.
- Complements [Editor Viewport Overlays](../../implemented/feature/2026-11-26-editor-viewport-overlays.md) and [Editor MCP Tile Review](../../implemented/feature/2026-11-26-editor-mcp-tile-review.md) by sharing the Spec dark/light token palette and Keep/Revert lifecycle.
- Uses the scope-aware shortcut registry from [Editor Settings and Shortcuts](../../implemented/feature/2026-11-26-editor-settings-shortcuts.md).

## Alternatives considered

- **Storing sketch marks as temporary objects or metadata inside `SceneDoc`**: Would automatically inherit scene rendering and undo history, but violates the core invariant that every author-created YAML file stays clean and reviewable and risks saving transient marks into git diffs. Rejected in favor of session-only `sketchState` synced per `sceneTab`.
- **Adding a Sketch button directly to `editor-toolbar`**: Matches the top bar in `Sketch.dc.html`, but the current 2D `editor-toolbar` already wraps at 1200 px and has layout tests verifying toolbar compactness. Rejected in favor of keyboard shortcut `D`, `View > Sketch`, Command Palette, Scene Tree `SKETCH LAYER`, and the floating top-center sketch toolbar.
- **Blocking `scene_edit` in `Editor` mutators themselves**: Would prevent manual editor edits while a sketch is pending, even when the user wants to tweak something by hand before confirming. Rejected in favor of enforcing the hold gate specifically on mutating `editormcp` handlers via `Editor.SketchHoldActive()`.

## Consequences

- **Benefits**:
  - Authors can annotate empty regions with four tools and numbered badges at any zoom level without ever dirtying `.scene.yaml` or scene undo history.
  - Agents receive both exact scene coordinates and a visual PNG with numbered badges, and must restate their interpretation via `sketch_reading` before mutating the scene.
  - Review lets authors visually compare added/changed objects against their original sketch and either clear marks on Keep or keep marks ready to re-send on Revert.
- **Costs**:
  - Adding `sketch_read` and `sketch_reading` increases the built-in `editormcp` tool surface (requiring updates to tool-list conformance tests and docs).
  - `packages/game/editor` now contains a lightweight 5×7 bitmap text rasterizer (`sketch_png.go`) parallel to `editormcp/viewport_text.go` so `Editor.RenderSketchViewportPNG` works without introducing a package cycle between `editor` and `editormcp`.
