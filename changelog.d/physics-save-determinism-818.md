### Added

- `game/physics`: add JSON-serializable `Save`, `Restore`, and `Checksum` snapshots for `World` and `RigidWorld` capturing full private solver, joint, and trigger tracking state (#818).
- `game/physics3d`: add JSON-serializable `Save`, `Restore`, and `Checksum` snapshots for `World` capturing warm-start contact caches, sleep timers, previous poses, active contacts, all 7 joint types, and static/voxel colliders (#818).
- `game/vehicle`: add JSON-serializable `Save`, `Restore`, and `Checksum` snapshots for `Vehicle` (#818).
- `game/physics`, `game/physics3d`, `game/vehicle`: eliminate arm64 FMA contraction drift across stepped simulation paths and add cross-architecture golden trajectory hash tests plus `game/net` rollback `SimState` soak tests (#818).
