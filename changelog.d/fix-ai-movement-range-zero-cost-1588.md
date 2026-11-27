### Fixed

- `game/ai`: preserve documented ascending-cost then index/tie-break ordering in `AStar.MovementRange`, `AStar.MovementRangeInto`, `AStar.MovementRangeWithCost`, `ReachableCells`, and `GraphSearch.Range` / `RangeInto` when zero-cost edges or zero-cost cycles discover lower-ordered cells after higher-ordered cells of equal cost, while keeping buffer-reusing queries zero-allocation (#1588).
