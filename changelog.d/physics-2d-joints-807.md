### Added

- `game/physics`: add `PrismaticJoint`, `WeldJoint`, `WheelJoint`, and `MouseJoint`, angle limits and motors on `RevoluteJoint`, rope/max-distance (`NewRopeJoint`, `MaxOnly`) and range (`MinLength`/`MaxLength`) modes on `DistanceJoint`, connected-body collision filtering (`CollideConnected`), reaction force/torque readouts (`ReactionForce`, `ReactionTorque`, `Broken`), break thresholds (`BreakForce`, `BreakTorque`, `JointBreakEvent`, `RigidWorld.OnJointBreak`, `RigidWorld.JointBreakEvents`), and full JSON snapshot/checksum support (#807).
