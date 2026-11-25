### Added

- `game/physics`: kinematic rigid bodies (`RigidBody.Kinematic`, `SetTargetPose`) that push and carry dynamic bodies, sensor rigid bodies (`RigidBody.Sensor`, `RigidWorld.SensorEvents`, `OnSensor`), solid and one-way tile collision for rigid bodies without seam snags (`RigidWorld.Tiles`, `TileLayer`, `TileMask`, `TileFriction`, `TileRestitution`), raycast, point and overlap queries on `RigidWorld` with `QueryMask`/`LayerMask` filtering (`Raycast`, `RaycastFirst`, `QueryPoint`, `QueryOverlap`, `QuerySensors`, `RigidBody.WorldShape`, `RigidBody` on `RaycastHit` and `OverlapHit`), and deterministic begin/end contact events with point, normal and approach speed (`RigidWorld.ContactEvents`, `OnContact`), all saved in `RigidWorldSnapshot` (#805).

### Fixed

- `game/physics`: `GridSnapshot` keeps a `Grid`'s origin, so `World.Restore` no longer moves a grid with a non-zero `OriginX` or `OriginY` back to (0, 0) (#805).
