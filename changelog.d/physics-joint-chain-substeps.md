### Added

- `pkg/physics3d`: `World.Substeps` (default `DefaultSubsteps` = 4) solves gravity, contacts and joints in deterministic substeps, and `World.EnablePositionCleanup` adds optional joint position passes after the last substep; both are saved in `WorldSnapshot` version 5 (#1711).
- `pkg/physics`: `JointSoftness` (`Hertz`, `DampingRatio`) as `Softness` on `DistanceJoint` and `RevoluteJoint`, and `RigidWorld.EnablePositionCleanup`; both are saved in `RigidWorldSnapshot` version 5 (#1711).

### Fixed

- `pkg/physics`, `pkg/physics3d`: long joint chains, heavy-end chains and ragdolls no longer stretch or come apart at the default settings, and breakable joints keep their force and torque thresholds in Newtons across solver substeps (#1711).
