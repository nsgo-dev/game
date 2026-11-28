### Added

- `game/sceneformat`: `Partition` grid math for streaming a large scene in cells, with validated cell size and load/unload radii, `Cells` assigning objects by their bounds (and `AlwaysLoaded` objects to no cell), and `Needed`/`Retained` returning the cells around streaming sources in deterministic order (part of #1728).
