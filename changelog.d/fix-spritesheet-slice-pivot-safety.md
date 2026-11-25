### Fixed

- `game/tooling/importsettings`, `game/editor`: guard `Slice.Cells` against `cell+spacing` and `cols*rows` integer overflows, add zero-allocation `Slice.GridCounts` / `Slice.CellRect` and `MaxSliceCells` cap, bound sprite-sheet import preview drawing and cell picker on dense grids (such as 1×1 cells on a 1024×1024 sheet), finish or cancel image pivot drags cleanly on outside release / `PlayIn` / non-finite coordinates, and enforce project-root symlink containment in `loadImportPreviewImage` (#880).
