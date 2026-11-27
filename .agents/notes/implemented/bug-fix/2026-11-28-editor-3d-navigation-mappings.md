# Agent Note: Route every 3D viewport mouse gesture through Navigation3D.Resolve

Status: implemented

## Problem

Settings › Viewport › 3D navigation persisted `Navigation3DDefault`, `Navigation3DBlender`, and `Navigation3DMaya` (`nsgo-dev/game#1005`), but `viewport3DGestures` only checked `ed.Navigation3D() == Navigation3DBlender` on middle-button press. Primary drags unconditionally called `BeginViewport3DDrag(..., shiftHeld())`, so Blender mode still orbited on plain left-drag, Maya mode never read `Alt` and behaved identically to Default, and Blender middle-drag called `BeginViewport3DDrag(..., false)` which let gizmo handles intercept a middle-button orbit (`nsgo-dev/game#1509`).

## Decision

Centralize every 3D viewport mouse mapping decision in `Navigation3D.Resolve` (`pkg/editor/navigate3d.go`) and drive all mouse gesture paths in `viewport3DGestures` (`pkg/editor/viewport3d_ui.go`, `pkg/editor/viewport.go`) and `beginViewport3DDragAction` (`pkg/editor/viewport3d.go`) from it, complementing [3D viewport navigation](../feature/2026-11-25-editor-3d-viewport-navigation.md) and [unified Settings window](../feature/2026-11-26-editor-settings-window.md):

- **Single mapping function (`Navigation3D.Resolve`)**: takes `Navigation3DInput` (`PrimaryDrag`, `MiddleDrag`, `RightDrag`, `Wheel`) and `core.Modifiers`, returning `Navigation3DAction` (`None`, `Orbit`, `Pan`, `Fly`, `Dolly`).
  - `Default` (and empty/unknown fallback): left-drag orbits, `Shift+left-drag` or middle-drag pans, right-drag flies, wheel dollies.
  - `Blender`: middle-drag orbits, `Shift+middle-drag` pans, right-drag flies, wheel dollies, and left-drag returns `Navigation3DActionNone` so it stays on selection, tools, and gizmos.
  - `Maya`: `Alt+left-drag` orbits, middle-drag (with or without `Alt`) pans, `Alt+right-drag` or wheel dollies, plain right-drag flies, and plain left-drag returns `Navigation3DActionNone`.
- **Tool/gizmo priority vs. camera override (`beginViewport3DDragAction`)**: keeps `BeginViewport3DDrag(sx, sy, w, h, pan)` backwards-compatible while separating `action` from `allowTools`. Middle- and right-button drags pass `allowTools = false` so camera drags never hijack gizmo handles or terrain strokes. Primary drags pass `allowTools = action == Navigation3DActionNone || (!mods.Shift && !mods.Alt)` so plain left-drag always reaches tools and gizmos first (falling back to orbit only in Default mode, and staying idle on empty space in Blender and Maya modes), while explicit modifier overrides (`Shift+left-drag` in Default, `Alt+left-drag` in Maya) always navigate the camera.
- **Modifier capture and touch/trackpad preservation**: `viewport3DGestures` combines `MouseButtonEvent.Modifiers` with live `core.KeyHeld` (`shift` and `alt`) so both real desktop mouse events and `nsgotest.DragAt` (which drives `OnTouch` + `OnPan` without `OnMouseDown`) resolve modifiers identically. Touchpad scrolling, pinch zoom, and two-finger touch panning bypass `Navigation3D.Resolve` and stay unchanged in every mode. `view3d-help` updates its keycap strip live when `Navigation3D` changes.

## Alternatives considered

- **Branch on `ed.Navigation3D()` separately inside `pan`, `mouseDown`, and `wheel`**: avoids introducing `Navigation3DInput` / `Navigation3DAction`, but scatters the mode matrix across three handlers, makes modifier interactions easy to drift out of sync, and cannot be exhaustively verified with a pure table test.
- **Keep `BeginViewport3DDrag(sx, sy, w, h, pan bool)` as the only drag entry point**: avoids adding `beginViewport3DDragAction`, but a single `pan bool` cannot distinguish "allow gizmos then do nothing on miss" (Blender/Maya left-drag) from "allow gizmos then orbit on miss" (Default left-drag) or "orbit without letting gizmos intercept" (Blender middle-drag and Maya `Alt+left-drag`), nor can it represent drag dolly (`drag3DDolly`).

## Consequences

- **Benefits**: Every mouse navigation preset behaves according to its documented bindings without leaking left-drag orbits into Blender/Maya or letting gizmo handles steal middle-drag or `Alt+left-drag` camera orbits; the mapping is covered by both a full mode × input × modifier table test and end-to-end `nsgotest` drag tests.
- **Costs**: Adds two small exported enum types (`Navigation3DInput`, `Navigation3DAction`) and `Navigation3D.Resolve` to `pkg/editor`, plus a `drag3DDolly` case in `drag3DKind` and `rightDollyMoved` context-menu suppression for Maya `Alt+right-drag`.
