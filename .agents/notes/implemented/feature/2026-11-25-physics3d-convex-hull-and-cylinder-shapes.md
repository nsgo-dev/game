# Agent Note: physics3d Convex Hull and Cylinder Shapes

Status: implemented

## Problem

Dynamic bodies in `packages/game/physics3d` could only be boxes, spheres or capsules (#808). Props such as rocks, debris, furniture, wedges and low-poly vehicle hulls want a convex hull, and barrels, wheels and pillars want a cylinder; a static `TriMeshCollider` cannot move. Every existing pair test in `shape.go` is a hand-written closed form for one pair of primitives, which does not scale to two more shapes against five shapes, triangle meshes, heightfields and voxel grids, and the solver used one contact point per body pair, so a flat face or a cylinder's cap rocked about that point instead of resting.

## Decision

### Shapes (`convex.go`, `cylinder.go`, `shape.go`)

- `ConvexHull` holds a `Center` and a pointer to immutable `hullGeom` (vertices, polygon faces with outward normals and plane offsets, edges, volume, centroid, inertia per unit mass, radius, half width). Copying, translating or rotating a hull never copies its vertices; `Shape.Rot` turns a hull about `Center` exactly as it turns an oriented box.
- `NewConvexHull(points, maxVertices)` runs a quickhull in float64: an initial tetrahedron of extreme points, then the point furthest outside any face, with index tie-breaks, until no point is outside or the vertex limit (`DefaultHullVertices` 64, `MaxHullVertices` 256) is reached. Coplanar triangles merge into polygon faces. Degenerate input returns `ErrDegenerateHull`. `ConvexHullFromMesh` takes `math3d.Vec3` positions and `uint32` indices, the types `gltf.Primitive` and `render3d.Mesh` already use, so `physics3d` does not import either package.
- `Cylinder` is `A`, `B` and `Radius` like `Capsule`, with flat caps.

### One convex narrow phase (`gjk.go`)

- Every pair that involves a hull or a cylinder goes through GJK (distance or overlap) and EPA (depth and normal) on a `cvx` view of the shape: a core (point, segment, triangle, box, hull, cylinder) plus a margin (a sphere's or capsule's radius). Spheres and capsules therefore resolve shallow contacts with GJK alone. The older closed-form pair tests among boxes, spheres and capsules are unchanged.
- Everything runs in float64 in a fixed order with index tie-breaks, so contacts are bit-identical across runs.
- The contact point is the centre of the overlap of the two supporting features (face, edge, vertex, or a 16-sided cap polygon for a cylinder), found by clipping the incident feature against the reference face's side planes.
- Against a triangle of a mesh or heightfield, a shape whose face reaches through the triangle's plane inside its outline uses the triangle's face normal, so internal mesh edges do not produce sideways normals. The face normal is only kept when its depth stays within a small slop (2% of one plus the shape's extent) of the GJK and EPA depth, or the shape's centroid lies over the triangle; otherwise a shape grazing a ledge's open edge from the side would be pushed down or lifted by the whole height of its face (`TestConvexGrazesLedgeEdge`).
- A cylinder with no length or no radius is not a valid core: collisions ignore it, as raycasts, areas and CCD already did (`TestZeroLengthCylinderIsIgnored`).

### Manifolds and solver (`world.go`)

- A body pair (or body and triangle) that involves a convex shape gets up to four contact points from `shapeManifold` / `triangleManifold` (`reduceManifold` keeps the extremes along two directions across the normal), each with the pair's normal and depth. Pairs of boxes, spheres and capsules keep their single point, so their behavior and existing tests do not change.
- The warm-start cache stays keyed per body pair: it stores the sum of the pair's point impulses and shares it out equally next step.

### Mass, CCD, areas and debug drawing

- Bodies keep the package's scalar inertia: a hull uses the mean of its exact principal moments for uniform density, a cylinder `m(6r²+h²)/18`, both plus the parallel-axis term for an offset centroid.
- CCD sweeps use half the hull's smallest width across a face, and `min(radius, height/2)` for a cylinder, as their smallest extent.
- Area volumes test containment with the exact hull planes or cylinder, and buoyancy uses the shape's own volume.
- `DebugStyle` gains `HullColor` and `CylinderColor`; `ConvexHullEdges` and `CylinderEdges` return the wireframes.

## Alternatives considered

### SAT for hull pairs, as for oriented boxes

SAT gives exact depth and a natural face-clipping manifold for polyhedra and is what `TestOrientedBoxBox` already uses. It does not apply to cylinders, whose round side has no finite set of axes, so it would need a second algorithm anyway; hull-versus-hull SAT also costs O(faces² + edges²) per pair. GJK and EPA cover every pair with one implementation and only need a support function per shape.

### Approximating a cylinder as a convex hull

A 16- or 32-sided prism hull would reuse the hull path with no new shape. A barrel rolling on its side then bumps from facet to facet, and its raycasts and bounds are wrong by the facet sag. The cylinder keeps an exact support function and ray test and only uses a cap polygon for the contact patch.

### Full inertia tensors for hulls and cylinders

The hull's exact tensor is computed and exposed (`InertiaTensor`), and a tensor would let a long cylinder spin correctly about its own axis. Every body, joint and the character controller in `physics3d` uses a scalar `InvInertia`; changing that is a solver-wide change outside this issue. The mean of the principal moments keeps hulls and cylinders consistent with the existing shapes.

### Four manifold points for every pair

Applying manifolds to box, sphere and capsule pairs too would make boxes rest more stably, but changes trajectories that existing tests and games depend on. It is limited to pairs with a convex shape, where there was no previous behavior.

## Consequences

- Acceptance is covered by `TestConvexShapePairs` (hit, normal and depth for every new pair), `TestConvexRocksAndBarrelsSettleAndSleepOnHeightfield`, `TestBarrelRollsOnItsSide`, `TestBarrelStandsOnItsEnd` and `TestConvexSimulationIsDeterministic`, with fuzz tests for the hull builder and for contacts separating the shapes.
- A convex pair costs more than a closed-form pair: GJK and EPA plus feature clipping. `BenchmarkTestShapesHullHull`, `BenchmarkTestShapesCylinderBox` and `BenchmarkConvexPileStep` track it.
- The character controller is still a capsule; its sweeps against hull and cylinder bodies go through the same `TestShapes` (`TestCharacterControllerAgainstConvexBodies`).
- AI cover generation (`GenerateCoverPointsFromBodies`) treats a cylinder standing on an end like a capsule pillar. Convex hulls and cylinders lying on their side generate no cover yet, which `docs/en/ai.md` and `docs/zh/ai.md` state.
