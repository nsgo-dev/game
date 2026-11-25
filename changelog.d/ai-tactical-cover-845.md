### Added

- `game/ai`: add EQS-style tactical position queries (`TacticalQuery`, `CoverPointGenerator`, `RingGenerator`, `GridGenerator`, `NavMeshGenerator`, `DistanceTest`, `LOSTest`, `AngleTest`, `PathDistanceTest`, `InfluenceTest`, `CoverHeightTest`, `CustomTest`), cover points with tilemap/grid/2D collider generators, deterministic `PointReservations` with timeout and explicit release, and per-step `QueryBudget` limits with zero-allocation buffer reuse (#845).
- `game/sceneformat`: add the built-in `cover_point` scene YAML component (`CoverPointComponent`, `RegisterCoverPoint`, `ExtractCoverPoints`, `ExtractBuiltCoverPoints`) for authoring cover points in scenes (#845).
- `game/physics3d`: add 3D collider-edge cover point generation (`World.GenerateCoverPoints`, `GenerateCoverPointsFromBodies`) projecting static box, oriented box, sphere, and capsule faces onto the XZ plane (#845).
