### Added

- `game/ai`, `game/world3d`: add BVH-accelerated 2D and 3D navigation mesh spatial queries (`NearestPoint`, `Raycast`, `MoveAlongSurface`, deterministic area-weighted `RandomPoint` and reachable `RandomPointAround`, all with `NavFilter` support) plus reusable `NavMeshQuery` and zero-allocation `FindPathInto` / `FindCorridorInto` APIs (#827).
