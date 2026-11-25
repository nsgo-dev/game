### Added

- `game/physics`: `RigidWorld` finds contact pairs with a deterministic sort-and-sweep broadphase that reports the same contacts, in the same order, as the old pass over every pair (#804).
- `game/physics`: contact impulses are cached per pair and feature across steps and warm-start the solver, and manifolds keep points of an overlapping pair up to 1 px apart, so stacks stand and settle with the default iterations (#804).
- `game/physics`: islands of rigid bodies connected by contacts and joints sleep after resting for `RigidWorld.TimeToSleep` below `SleepLinearThreshold` and `SleepAngularThreshold`, and wake on `RigidBody.Wake`, forces, impulses, edits to their pose or velocity, contact or a joint with an awake body, removal or movement of a static body they rest on, and changes to their area's gravity, damping or fluid, and bodies a `MouseJoint` holds or a joint motor drives stay awake; `RigidBody.Sleeping` reports the state and `RigidWorld.DisableSleep` turns it off (#804).
- `game/physics`: fast rigid bodies, and every body with `RigidBody.Bullet` set, are swept against static bodies by time of impact and stop at the first surface instead of passing through thin walls (#804).
- `game/physics`: `RigidWorldSnapshot` saves sleep state, sleep settings and the warm-start contact cache (#804).

### Changed

- `game/physics`: `RigidWorld` trajectories, and so `RigidWorld.Checksum` values and recorded replays, differ from earlier versions (#804).
