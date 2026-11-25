### Added

- `game/tooling/datatable`, `game/editor`: untyped round-trip YAML data table editing (`ParseDocument`, `Document`, `RowType`, `Registry`) and a Data Table editor panel (`DataTablePanelView`, `OpenDataTableFile`, `RegisterDataTableType`) that opens tables from the file browser as a sortable, filterable grid with inferred value kinds, undo/redo, lossless saves, and per-pattern Go row-type validation (#918).
