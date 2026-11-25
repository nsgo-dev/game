### Added

- `game/physics3d`: `CharacterController` now tracks moving kinematic and dynamic ground bodies (`GroundBody`, `GroundVelocity`, `GroundYawDelta`, `Yaw`, `ApplyPlatformYaw`), carries the capsule through linear and angular platform motion, inherits or drops platform velocity via `PlatformOnLeave` (`PlatformLeaveAddVelocity`, `PlatformLeaveAddUpwardVelocity`, `PlatformLeaveDoNothing`), pushes out of encroaching kinematic geometry, and reports `Crushed` when squeezed between opposing surfaces (#813).
