### Added

- `game/ai`: add deterministic HPA* hierarchical pathfinding (`HPA`) with configurable clusters, automatic border entrances, weighted `CostGrid`/`EdgeCostGrid` support, incremental dirty-cluster updates (`UpdateCell`, `UpdateRect`, `RebuildDirty`), abstract/corridor queries, and Jump Point Search (`JPS`) for exact optimal 8-way uniform-cost square grids (#833).
- `game/rts`: add `SectorFlowField` (`BuildAlongHPA`, `BuildSectors`) to integrate flow fields only along an HPA* cluster corridor on large maps (#833).
