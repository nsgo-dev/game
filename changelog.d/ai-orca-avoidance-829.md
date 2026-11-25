### Added

- `game/ai`: add deterministic, zero-allocation ORCA/RVO2 velocity-based local avoidance (`ORCASolver`, `ORCAConfig`, `ORCAAgent`, `ORCAObstacle`, `ORCAAgent3D`, `ORCAObstacle3D`) supporting 2D and 3D XZ-plane navigation, two-sided static obstacle edges, avoidance layers/masks, smooth priority and stationary responsibility weighting, spatial-hash neighbor acceleration, and input-order-independent canonical LP solving (#829).
- `game/rts`: add `AvoidanceMode` (`AvoidancePush`, `AvoidanceORCA`) and `CrowdOptions.ORCA` so `Crowd` can use `ai.ORCASolver` velocity obstacles alongside its existing overlap push avoidance (#829).
