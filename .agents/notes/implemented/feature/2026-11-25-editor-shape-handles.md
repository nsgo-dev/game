# Agent Note: Component shape field kinds, 2D viewport shape handles, and plugin tool drag callbacks

Status: implemented

## Problem

World geometry such as colliders, triggers, light occluders, navigation regions, and spawn areas belongs on scene objects as component fields, but `sceneformat.FieldKind` only supported scalar and string-list values. The 2D viewport neither drew component shapes nor offered interactive handles to resize rectangles, adjust circle radii, move points, or edit polygon vertices, and `editor.PluginTool` only received `Press` and `Draw` callbacks, preventing plugins from building their own draggable handles (#882).

## Decision

### Shape field kinds in `sceneformat`

`packages/game/sceneformat/shapes.go` and `components.go` add four 2D shape field kinds (`FieldPoint`, `FieldRect`, `FieldCircle`, `FieldPolygon`, with `FieldKind.IsShape()`) backed by explicit Go types (`Point`, `Rect`, `Circle`, and `[]Point`) in object-local coordinates:

- **Validation**: `ComponentRegistry` refuses `Min`, `Max`, `Step`, and `Enum` on shape kinds, refuses unknown keys inside `{x, y}`, `{x, y, width, height}`, and `{x, y, radius}` mappings, refuses non-finite numbers and negative `width`/`height`/`radius`, and reports exact 1-based line/column positions via `*ComponentError` during `ParseWith`. `Resolve` and `cloneFields` deep-copy `[]Point` slices so callers cannot mutate registry defaults.
- **Byte-for-byte YAML round-trip**: Because `ObjectDoc.Components` is `map[string]any`, YAML decoding produces `map[string]any` whose keys `yaml.Encoder` emits alphabetically (`height, width, x, y` for `Rect`; `radius, x, y` for `Circle`; `x, y` for `Point`). `Point`, `Rect`, and `Circle` implement `MarshalYAML` returning `map[string]any` with `-0.0` normalized to `0.0`, so values written by Go code or `SetComponentField` serialize identically to values loaded and re-encoded from YAML.
- **JSON Schema**: `cmd/gameschema` publishes `Point`, `Rect`, `Circle`, and `Polygon` under `definitions` in both `scene.v1.json` and `scene.v2.json`.

### 2D viewport shape rendering, handles, and grid snapping

`packages/game/editor/shapes2d.go` draws every registered shape field of the selected object in 2D edit mode using `layerAffine(space).Mul(bo.World)` so shapes follow parent hierarchies, object rotation/scale, and world/screen layer spaces. Toggling `Editor.ShowAllShapes` (`SetShowAllShapes`, `CmdToggleAllShapes`, toolbar button `view-show-all-shapes`) draws all visible objects' shape fields while keeping the selected object highlighted on top.

Interactive handles extend the in-place preview + single-commit pattern of [the 2D transform gizmo](./2026-11-25-editor-2d-transform-gizmo.md):

- **Pick priority**: Perimeter handles (`Point`, `Rect` corners/edges, `Circle` cardinal rim handles, `Polygon` vertices) and `Polygon` edge midpoint handles take priority over the transform gizmo; `RectCenter` and `CircleCenter` handles are suppressed whenever they overlap the object's origin move square so moving the object from its origin remains unobstructed.
- **Undoable `SetComponentField` commits**: `BeginShapeDrag2D` snapshots the raw component/field map state, `UpdateShapeDrag2D` updates the field in place with optional `SnapToGrid` snapping (`GridSize`, default `8`) and flips opposite rect edges smoothly without allowing negative extents, and `EndShapeDrag2D` restores the pre-drag snapshot and commits through `Editor.SetComponentField` as one undo step. `SetComponentField` compares normalized values against `e.components.CheckField` so unchanged YAML-decoded shapes never push no-op undo steps.
- **Polygon vertex add/remove**: Clicking an edge midpoint handle inserts a vertex via `AddPolygonVertex` (one `SetComponentField` undo step); dragging a midpoint inserts and moves it in one step; right-clicking (`HandleCanvasSecondaryPress`) or Alt+clicking a vertex handle removes it via `RemovePolygonVertex` (one undo step, refusing removal below 3 vertices).
- **Cancellation**: Escape, touch cancel, a second finger, Play, Undo/Redo, Save, Revert, tool/view changes, Delete, and Duplicate call `CancelShapeDrag2D` and restore the original component state.

### Plugin tool drag and hover callbacks

`editor.PluginTool` gains optional `OnDragBegin(ed, x, y) bool`, `OnDragMove(ed, x, y)`, `OnDragEnd(ed, x, y)`, `OnDragCancel(ed)`, and `OnHover(ed, x, y, active)` callbacks in canvas coordinates. `RegisterPlugin` requires at least one of `Press` or `OnDragBegin`, enabling drag-only custom handle tools. `ViewportView` routes 2D pan gestures and mouse move/leave events through `BeginPluginDrag2D`, `UpdatePluginDrag2D`, `EndPluginDrag2D`, `CancelPluginDrag2D`, `SetPluginHover2D`, and `ClearPluginHover2D`.

## Alternatives considered

### Encoding `Rect` and `Circle` with Go struct field order

Using Go struct field order (`x, y, width, height`) instead of `MarshalYAML` alphabetical order looks slightly nicer when written from Go, but `sceneformat.Parse` decodes `ObjectDoc.Components` into `map[string]any` without type tags; a subsequent `Encode` sorts map keys alphabetically (`height, width, x, y`), breaking byte-for-byte round-trip stability and falsely dirtying `Editor.SceneDirty()`. Matching `map[string]any` key order in `MarshalYAML` keeps round-trips exact in all paths without requiring registry-aware encoding.

### Encoding polygon points as `[x, y]` tuples instead of `{x, y}` mappings

Compact `[x, y]` arrays match `tilemap.TileShapeDoc.Points`, but `FieldPoint` uses `{x, y}` mappings and reusing the exact same `Point` schema for `FieldPolygon` keeps `Point` and `Polygon` uniform across Go types, YAML validation, error messages, and JSON Schema `$ref: "#/definitions/Point"`.

### Dedicated shape editing tool mode

Adding a separate toolbar mode for editing shapes would isolate shape handles from the transform gizmo, but adds mode-switching friction when positioning an object alongside its collider. Offering shape handles directly in Select and Move while protecting the object's origin move square matches Godot's 2D viewport workflow.

## Consequences

- Games can declare typed 2D shape fields (`point`, `rect`, `circle`, `polygon`) on any registered component, edit them directly in the 2D viewport or inspector with optional grid snapping, and save deterministic round-tripping YAML.
- Polygon vertex insertion, movement, and deletion each record as a single undoable `SetComponentField` step.
- Plugin tools can implement custom interactive viewport handles with full drag lifecycle and hover feedback in canvas coordinates.
- YAML files written for `Rect` and `Circle` fields use alphabetical key order (`height, width, x, y` and `radius, x, y`) to stay byte-for-byte identical after reload and re-save.
- Verified by unit and mounted `nsgotest` suites in `packages/game/sceneformat/shapes_test.go`, `packages/game/cmd/gameschema/main_test.go`, and `packages/game/editor/shapes2d_test.go`.
