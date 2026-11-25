### Added

- `game/editor`: interactive 2D viewport resize and move handles for `camera.bounds` (with live preview, Escape cancel, and single-step Undo/Redo) and side-by-side `*.rooms.yaml` room-map editing over the 2D scene viewport with tile-grid snapping, room creation/move/resize/delete handles, inspector editing for room `id`, `name`, and `transition`, and deterministic round-trip saving via `rooms.Encode` (#884).
