### Fixed

- `pkg/refs`, `pkg/editor`: preserve and repair data table cell references to missing or hand-moved files listed in `game.index.yaml` or matching asset extensions during `Check` and `PlanIndex`, and classify `.tsx` files as tileset assets only via `Project.IsAsset` when they parse as Tiled XML (`<tileset>`) or are referenced by a map while excluding mod entry scripts declared in `mod.yaml` (#1608).
