### Added

- `game/physics`: kinematic rigid bodies (`RigidBody.Kinematic`, `SetTargetPose`) that push and carry dynamic bodies, sensor rigid bodies (`RigidBody.Sensor`, `RigidWorld.SensorEvents`, `OnSensor`), solid and one-way tile collision for rigid bodies without seam snags (`RigidWorld.Tiles`, `TileLayer`, `TileMask`, `TileFriction`, `TileRestitution`), raycast, point, overlap and tile-aware shape-cast queries on `RigidWorld` with `QueryMask`/`LayerMask` filtering (`Raycast`, `RaycastFirst`, `QueryPoint`, `QueryOverlap`, `QuerySensors`, `RigidBody.WorldShape`, `RigidBody` on `RaycastHit` and `OverlapHit`), and deterministic begin/end contact events with point, normal and approach speed (`RigidWorld.ContactEvents`, `OnContact`), all saved in `RigidWorldSnapshot`, which marks tiles it cannot save with `CustomTiles` (#805).

### Changed

- `game/physics`: `SnapshotVersion` is 2; `World.Restore` and `RigidWorld.Restore` still read version 1 snapshots, and builds that only know version 1 refuse the new ones. `RigidWorld.Checksum` gives a different digest than earlier releases for any world whose bodies touch, because it now hashes the contact state, so lockstep peers and replays must run the same release (#805).

### Fixed

- `game/physics`: a fast `RigidBody` stopped by continuous collision loses its velocity into the surface at every point of the face it lands on, so a box dropped from high enough to be swept onto a static floor or tiles comes to rest level and sleeps instead of hovering 0.1 px above it, gaining speed, and a box landing across a tile seam no longer tumbles (#805).
- `game/physics`: `GridSnapshot` keeps a `Grid`'s origin, so `World.Restore` no longer moves a grid with a non-zero `OriginX` or `OriginY` back to (0, 0) (#805).
