### Fixed

- `pkg/tilemap`: `LoadLDtk`, `LoadLDtkAll`, and `LoadLDtkWorld` now preserve stacked LDtk tiles in the same cell by emitting additional `<layer>#2`, `<layer>#3`, … tile layers in LDtk draw order (counted against the per-load dense tile budget), and convert LDtk entity `px` positions to top-left `Object.X`/`Object.Y` using `__pivot` while recording the normalized anchor in `Object.PivotX`/`Object.PivotY` (#1583).
