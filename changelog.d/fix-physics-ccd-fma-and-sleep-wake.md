### Fixed

- `game/physics`: prevent arm64 FMA contraction in `sweepHullAgainst` (`rigid_ccd.go`), wake sleeping partners immediately on `RigidWorld.RemoveBody`, `AddJoint`, `RemoveJoint`, and moved/removed static joint anchors (preserving `Save`/`Restore` rollback determinism), and ensure bodies with `NaN` velocities do not fall asleep (#804).
