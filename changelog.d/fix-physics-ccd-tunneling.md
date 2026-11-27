### Fixed

- `game/physics`: `RigidBody.Bullet` bodies now sweep against dynamic and kinematic bodies as well as static bodies and tiles so fast projectiles stop at thin dynamic targets instead of tunnelling through them (#1712).
- `game/physics3d`: replaced the 32-step position-sampling CCD with GJK conservative advancement with interpolated rotation and added `Body.Bullet` (persisted in `SnapshotVersion` 4) so arbitrarily fast bodies, spinning shapes, and 3D bullets do not tunnel through thin static or dynamic geometry (#1712).
