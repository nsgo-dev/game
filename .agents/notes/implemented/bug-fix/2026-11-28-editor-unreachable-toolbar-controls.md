# Agent Note: Restore unreachable editor controls and remove dead toolbarView

Status: implemented

## Problem

When the AI-first main layout ([main layout note](../feature/2026-11-26-editor-ai-first-main-layout.md)) replaced `toolbarView` with the compact 44 px header (`mainHeaderView`) and floating viewport bars, `toolbarView` and its helpers (`view2DToolsView`, `place3DToolsView`, `statusBarView`, `sceneFileButtons`) were left behind as unreferenced dead code. Eleven editor controls existed only inside that unbuilt subtree and had no registered commands or live UI triggers: GridMap tools (`Paint`, `Erase`, `Pick`, `Box`), 3D viewport overlays (`LOD`, `Occluders`, `Render Stats`), 3D `Place` options (`Snap`, `Align`, `Height`), the 2D `Pick` tile tool (`ToolPickTile`), and the 2D Snap Settings popover (including grid step, `Auto` reset, rotation step, and scale step).

## Decision

Every affected control is now reachable through both the command registry (and therefore Command Palette, desktop menus, and `Settings › Shortcuts`) and the live 2D/3D viewport chrome, and all dead legacy toolbar builders are deleted.

### Command and menu surface

- Registered 11 new built-in commands in `pkg/editor/commands.go` (alongside existing `CmdToggleSnapGrid`): `CmdToolPickTile`, `CmdGridMapPaint`, `CmdGridMapErase`, `CmdGridMapPick`, `CmdGridMapBoxFill`, `CmdTogglePlaceSnap3D`, `CmdTogglePlaceAlign3D`, `CmdToggleLODOverlay`, `CmdToggleOccluders`, `CmdToggleRenderStats`, and `CmdSnapSettings`.
- All 11 commands default to empty `Shortcut: ""` so they never collide with existing 2D/3D single-key bindings while remaining searchable in `⌘K` and rebindable in `Settings › Shortcuts`.
- `menubar.go` exposes them under **View > Tools** (`Pick Tile`, `GridMap` tools, `3D Place` options), **View > Overlays** (`LOD`, `Occluders`, `Render Stats`), and **View** (`Snap Settings...`), and includes their state in `bindApplicationMenu`'s change digest.
- Invoking a GridMap command switches to 3D mode if currently in 2D and toggles back to `GridMapToolNone` when already active; selecting any standard tool via `SetTool` exits GridMap mode even if `e.Tool` already matches the requested tool.

### Live viewport chrome and dead-code removal

- **2D viewport bottom pill (`pkg/editor/overlays_ui.go`)**: hosts `view-snap-settings` (`snapSettingsPopover`) beside `viewport-bar-snap`, using a controlled `snapSettingsPopoverOpen` boolean so `CmdSnapSettings` and the button stay synchronized in both 2D and 3D (and reset on `SetViewMode`).
- **Tile palette (`pkg/editor/tilepalette.go`)**: `tileBrushBar` includes `Pick` (`tool-pick`) beside `Paint`, `Erase`, `Rect`, `Fill`, and `Line`.
- **3D viewport chrome (`pkg/editor/viewport3d_ui.go`)**:
  - Bottom-center `view3d-bottom-bar` adds a `view3d-overlays` popover toggling `LOD`, `Occluders`, `Render Stats`, and `3D NavMesh` beside `view3d-snap-settings` (mirroring 2D's bottom pill without widening the top `view3d-bar`), plus the compact `Paint / Erase / Pick / Box` GridMap segment group before `view3d-focus`, keeping the single-row 38 pt height required for toast clearance and staying within the `508 px` viewport at `1120 × 680`.
  - Bottom-left contextual `view3d-place-bar` (`Left: 16, Bottom: 60`, symmetric to `viewport3d-render-stats-panel` on the right) appears only while `!Playing && Tool == ToolPlace && GridMapTool == GridMapToolNone` to expose `Snap`, `Align`, and `Height`.
- **Dead code removed**: deleted `toolbarView`, `view2DToolsView`, and `statusBarView` (`pkg/editor/ui.go`), `place3DToolsView` (`pkg/editor/models_ui.go`), and `sceneFileButtons` (`pkg/editor/scenefile_ui.go`).

## Alternatives considered

- **Expanding the 44 px top header (`mainHeaderView`) with GridMap, 3D overlay, and 3D Place controls**: rejected because the header contract in [main layout note](../feature/2026-11-26-editor-ai-first-main-layout.md) fixes `editor-toolbar` at a single 44 px row across `1120 × 680` and `1440 × 900`, and 3D-only / tool-specific controls belong on the 3D viewport chrome where they do not clutter 2D editing.
- **Assigning default single-key shortcuts to GridMap tools and 3D overlays**: rejected because single-letter keys (`P`, `E`, `B`, `L`, `O`, etc.) are already claimed or sensitive across 2D/3D/Sketch scopes; leaving default shortcuts blank avoids regressions while still exposing full user rebinding in `Settings › Shortcuts`.
- **Putting 3D Place options (`Snap`, `Align`, `Height`) inside `view3d-bottom-bar` permanently**: rejected because adding an always-visible `NumberInput` and two switches to `view3d-bottom-bar` would widen or wrap the bottom-center pill at `1120 × 680` and break toast clearance. A contextual bottom-left pill (`view3d-place-bar`) shown only in `ToolPlace` keeps the default 3D viewport clean.

## Consequences

- **Benefits**:
  - Every control formerly trapped inside `toolbarView` is reachable via commands, menus, Command Palette, `Settings › Shortcuts`, and native 2D/3D viewport controls.
  - No unreferenced toolbar builders remain in `pkg/editor`, preventing future drift between dead code and live UI.
  - English and Chinese editor docs and locale catalogs stay synchronized with the live UI and command registry.
- **Costs**:
  - `view3d-bottom-bar` is slightly wider due to the four compact GridMap segment buttons (`Paint`, `Erase`, `Pick`, `Box`), so layout regression tests explicitly guard single-row height and non-overlap at both `1440 × 900` and `1120 × 680`.
