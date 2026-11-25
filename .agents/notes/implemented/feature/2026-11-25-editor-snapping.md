# Agent Note: Editor grid, pixel, rotation, and scale snapping with 2D grid overlay

Status: implemented

## Problem

Object placement, Move clicks, 2D transform gizmo drags, 2D lighting/occluder/navregion handles, and 3D gizmo translate/rotate/scale drags all applied raw pointer coordinates and deltas. Only 2D component shape handles had an isolated `SnapToGrid`/`GridSize` toggle (#882), no grid overlay was drawn outside tile tools, there was no pixel snap, rotation step, scale step, modifier inversion, or whole-selection snapping when multiple objects move together, and snap settings were not persisted per project (#869).

## Decision

Unify all editor snapping and the 2D snap grid overlay in `packages/game/editor/snap.go`, extending the existing `Editor.SnapToGrid` and `Editor.GridSize` fields introduced in [component shape handles](./2026-11-25-editor-shape-handles.md) rather than duplicating them:

- **Unified snap settings**: `Editor` exposes `SnapToGrid`, `GridSize` (`0` = auto-follow active layer's tilemap tile width/height, falling back to `8`), `SnapToPixel` (whole 1-point increments when grid snap is off), `SnapRotation` / `RotationSnapDeg` (default `15°`), `SnapScale` / `ScaleSnapStep` (default `0.25`), and `ShowGrid` (optional 2D grid overlay), plus the snapshot type `SnapSettings` (`SnapSettings()`, `SetSnapSettings`, `ResetGridSize`).
- **Modifier inversion (`Ctrl` / `Cmd`)**: Holding `Ctrl` or `Cmd` (`core.KeyHeld(cx, "ctrl") || core.KeyHeld(cx, "mod")`) during a drag or placement press temporarily inverts snapping via `withSnapInvert` (`SetSnapInvert`): active snapping turns off for free edits, while disabled snapping turns on for that drag. This avoids colliding with `Space` (2D pan), `Shift` (2D uniform scale / 3D pan / multi-select click), and `Alt` (tile pick / polygon vertex delete).
- **2D and 3D coverage**: Snapping applies uniformly to 2D `ToolPlace` (`pressActiveLayer`), 2D `ToolMove` click, 2D asset browser quick-place, 2D transform gizmo (`gizmo2d.go`: body, move square, axis arrows, rotation ring, scale handles with `keepAspect` aspect-ratio preservation), 2D component shape handles (`shapes2d.go`), 2D lighting/occluder/navregion handles (`lights2d_editor.go`, `navregion2d_editor.go`), 3D gizmo translate/rotate/scale (`viewport3d.go`), and 3D ground-plane placement (`models.go`, preserving `PlaceSnap3D`).
- **Multi-object selection moving as a whole**: `Editor` tracks `selectedIDs` alongside primary `SelectedID` (`SelectObjects`, `ToggleSelectObject`, `SelectedIDs`, Shift-click in 2D `ToolSelect`, preserved per tab in `tabs.go`). When multiple objects move together in 2D or 3D, the primary anchor snaps and every other non-descendant selected object translates by the exact same snapped displacement so relative offsets are preserved, committed as a single undoable `multiTransform2DCmd` / `multiTransform3DCmd` step (restored cleanly on Escape).
- **Optional 2D snap grid overlay**: `drawSnapGridOverlay` draws across the visible 2D canvas under the active layer's view transform (following `View2D` pan, zoom, rotation, and parallax) even outside tile tools, doubling rendered line spacing automatically when lines would be closer than 4 canvas points.
- **Compact toolbar UI**: The 2D toolbar keeps the existing `view-snap-grid` magnet toggle and adds a single compact icon-only `ui.Popover` trigger (`view-snap-settings`, mirrored as `view3d-snap-settings` in the 3D viewport bar) holding all toggles, step `NumberInput`s, and the **Auto** tile-size reset button. This keeps `editor-toolbar` within 2 rows at `1120x680` so viewport height and layout-sensitive tests remain unaffected.
- **Per-project persistence in `.game/editor.yaml`**: `viewState` in `viewstate.go` (see [3D viewport navigation](./2026-11-25-editor-3d-viewport-navigation.md)) gains an optional `snap` block (`yaml:"snap,omitempty"`), omitted when all settings match defaults so existing `.game/editor.yaml` files and format conformance examples round-trip byte-for-byte under `version: 1`. `OpenProject` restores project-level snap preferences via `restoreProjectSnapState` (resetting to defaults for projects without saved snap state), while scene switches within the same project keep the project's active snap preferences. Scene files are never modified or dirtied.

## Alternatives considered

### Inline step inputs directly in the 2D toolbar

Placing grid step, pixel snap, rotation step, and scale step controls inline in `editor-toolbar` would expose every value without opening a popover, but pushes the 2D toolbar past 2 rows at `1120x680`, shrinking the viewport canvas and breaking layout-sensitive viewport tests. Keeping the magnet quick-toggle plus one icon-only `ui.Popover` preserves one-click grid snap while keeping all snap controls compact and accessible in both 2D and 3D views.

### Storing snap preferences in per-user `EditorSettings` or inside scene files

Storing snap settings in per-user `EditorSettings` (`editor-settings.v1.json`) would apply a pixel-art project's 16-point grid to a 3D project's scenes; storing them in scene files would pollute version-controlled scene diffs. Persisting them under the optional `snap` key of per-project `.game/editor.yaml` (`.game/` is gitignored) matches #869's requirement and reuses the existing atomic persistence and schema-version guard.

### Snapping each selected object independently during multi-object moves

Snapping each object's own coordinates independently collapses relative offsets between objects that were not already aligned to the same grid phase. Snapping the primary anchor and applying the shared snapped displacement to all non-descendant peers preserves group geometry while aligning the selection as a whole.

## Consequences

- Benefits:
  - Authors get unified grid (auto-following active layer tile size), pixel, rotation, and scale snapping, modifier inversion, a 2D view-following grid overlay, whole-selection multi-object move snapping, and per-project persistence without dirtying scene files.
  - Existing `SnapToGrid`, `GridSize`, `EffectiveGridSize`, `SnapValue`, `view-snap-grid`, `PlaceSnap3D`, and `.game/editor.yaml` `version: 1` compatibility stay intact.
- Costs:
  - `Editor.GridSize` now defaults to `0` (meaning auto-follow active layer tilemap tile size, falling back to `8`) rather than pre-populating `8`, so callers reading the active step must call `EffectiveGridSize()` / `EffectiveGridStep()` rather than reading `GridSize` directly.
- Verification:
  - `packages/game/editor/snap_test.go` verifies both #869 acceptance criteria (16-pt grid place + drag saved YAML multiples of 16; 15° rotation drag saved YAML multiples of 15° in radians), active-layer tilemap auto step, pixel/scale snap, `Ctrl` modifier inversion both ways, multi-selection move as a whole + undo/escape, 3D gizmo translate/rotate/scale, 2D snap grid overlay under `View2D` transforms, per-project `.game/editor.yaml` persistence and project isolation, toolbar popover/command integration, and 2D toolbar height at `1120x680`.
  - `packages/game/editor/navigate3d_internal_test.go` verifies `parseViewState` refuses negative, non-finite, and unknown `snap` fields while keeping `TestViewStateFormatConformance` passing.
