### Fixed

- `game/ai`, `game/world3d`: eliminate arm64 FMA contractions in `NavMesh.RandomPoint` and `NavMesh.RandomPointAround`, move the 3D navmesh flat portal index into `world3d.NavMeshQuery` to avoid concurrent lazy-mutation races and duplicate `NavLink` allocations, and trim `world3d.NavMesh` build allocations (#827).
