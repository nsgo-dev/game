### Added

- `game/physics`: convex `Polygon` (up to 8 vertices, optionally rounded), `Capsule`, `Segment` and static `Chain` shapes in `Shape`, with overlap contacts for every pair, raycasts, `World` queries, triggers, `MoveAndSlide`, debug drawing, `RigidBody` narrowphase with up to two clipped contact points, mass and inertia from the shape (`NewRigidBody`, `Shape.Area`, `Shape.Centroid`, `Shape.MomentOfInertia`), and chains that never catch bodies on their interior vertices (#803).
- `game/tilemap`: `Object.PhysicsShape` and `ObjectLayer.PhysicsBodies` turn Tiled rectangle, ellipse, polygon and polyline objects into static physics shapes (#803).
- `game/mcp`: `game_screenshot` outlines polygon, capsule, segment and chain bodies, counted as `physics_polygons`, `physics_capsules` and `physics_lines` (#803).
