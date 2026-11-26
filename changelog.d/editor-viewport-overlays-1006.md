### Added

- `game/editor`: unified Spec viewport overlay renderer (`DrawObjectOverlay`) and live overlay API (`SceneOverlayEntry`, `SetSceneOverlays`) supporting all seven Spec overlay kinds (Selection, Agent working, Added, Changed, Removed, Problem, Focused change in Review) with non-color second cues, Selection+Agent 3 px outer pairing, 240 ms change fade-in, 1.6 s Agent pulse, and reduced-motion static rendering (#1006).
- `game/editor`: shared top-left viewport overlay legend (`viewport-overlay-legend`) across 2D and 3D viewports with six `role="switch"` toggles persisted per user under `overlayLegend` in `EditorSettings` (`editor-settings.v1.json`) (#1006).
- `game/tooling/assetcheck`: populate `Problem.Object` on scene object, component, override, and connection diagnostics (and expose `object` in `editormcp`'s `game_check`) so live Problem overlays attach directly to objects in the open scene (#1006).

### Changed

- `game/editor`: switch 2D and 3D selection outlines to the Spec style (1.5 px solid accent, four corner handles, name tag) and move camera frame, camera bounds, tile grid, and snap grid to neutral `SpecPalette` hairline/dotted tokens distinct from all legend kinds (#1006).
