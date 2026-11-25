### Added

- `game/physics`: add deterministic 2D shape casts (`ShapeCast`, `ShapeCastAll`, `ShapeCastLayer`, `ShapeCastAllLayer`, `ShapeCastWith`, `ShapeCastAllWith`, `ShapeCastShape`) on `World` and `RigidWorld` with analytic Minkowski and moving-SAT time-of-impact against bodies, oriented rigid bodies, and `TileGrid` / `TileGridOrigin` tiles (#812).
- `game/physics3d`: add deterministic 3D shape casts (`ShapeCast`, `ShapeCastAll`, `ShapeCastLayer`, `ShapeCastAllLayer`, `ShapeCastWith`, `ShapeCastAllWith`, `ShapeCastShape`, `TriMeshCollider.ShapeCast`, `HeightfieldCollider.ShapeCast`) returning `RaycastHit` against bodies, triangle meshes, heightfields, and voxel colliders (#812).
