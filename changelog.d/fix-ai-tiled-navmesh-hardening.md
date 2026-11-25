### Fixed

- `game/ai`, `game/world3d`, `game/navbake3d`: synchronize lazy coarse tile BVH construction in `TiledNavMesh` so concurrent read-only queries are race-free; prevent path tightening from shortcutting across implicit higher-cost areas or omitting shortcut tiles from `TiledNavPathResult.Tiles`; add 3D corner-grazing bridging to `world3d.TiledNavMesh.RaycastWithFilter`; clamp tile coordinates and reject oversized obstacle tile spans cleanly; and evaluate 3D obstacle Y overlap on the clipped ramp surface rather than the full polygon AABB (#826).
