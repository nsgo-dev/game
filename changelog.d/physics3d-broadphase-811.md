### Added

- `game/physics3d`: deterministic broadphase with separate static and dynamic AABB trees (`World.SyncBroadphase`), accelerating body-pair detection, raycasts, overlap queries, CCD sweeps, `WakeInBox`, and `CharacterController` moves across all 3D shapes, triangle meshes, and heightfields while remaining bit-identical to brute-force scans (#811).
