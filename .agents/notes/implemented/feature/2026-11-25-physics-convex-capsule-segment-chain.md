# Agent Note: Convex Polygon, Capsule, Segment and Chain Shapes in physics

Status: implemented

## Problem

`physics` collided only axis-aligned `Box` and `Circle` shapes, plus oriented boxes inside `RigidWorld` (#803). Ramps, wedges, rotated props, characters with rounded feet and sloped or concave terrain drawn in Tiled had no shape, so games either approximated them with stacks of boxes, which snag at every seam, or left the package. The new shapes have to fit into every place a `Shape` already goes (`World` queries and raycasts, triggers, kinematic `Body`, `RigidWorld`, debug drawing, the MCP screenshot overlay) without changing how existing box and circle scenes behave.

## Decision

`Shape` gains four kinds beside `Box` and `Circle`: `ShapePolygon` (`Polygon`, a convex hull of at most `MaxPolygonVertices` = 8 vertices with an optional rounding `Radius`), `ShapeCapsule`, `ShapeSegment` and `ShapeChain` (an open polyline or closed loop held by pointer, so `Shape` stays comparable with `==`).

- **One narrowphase for the new kinds (`narrow.go`)**: every convex shape is a rounded hull, a core of 1 to 8 points grown by a radius (a circle is a point, a capsule or segment is two points, a box or polygon is its vertices). Overlap is the separating axis test on the cores plus the radii, and the manifold comes from clipping the incident edge against the reference face into at most two points, as Box2D's `b2CollidePolygons` does. Hulls with a coordinate that is not finite never collide. Box and circle pairs keep the existing code paths, so their results are bit-identical to before.
- **Segments and chains have no area**: they collide from both sides with every shape that has area and never with each other. A body holding one is always static.
- **Chain ghost contacts**: segment i of a chain only keeps a contact at an interior vertex when the contact normal lies between the normals of the two segments that meet there (a convex vertex); at a flat or concave vertex the contact is pushed along the segment's own face normal. Two segments sharing a vertex can still report the same contact (a ball on a peak or exactly on a flat seam), so `chainManifolds` drops a point an earlier segment already reported with the same normal (within `chainDuplicateTolerance`). `RigidWorld` builds every manifold before applying position correction, so a duplicate would push the body out twice per iteration.
- **Rigid bodies (`rigid.go`)**: `NewRigidBody(shape, mass)` puts `(X, Y)` at `shape.Centroid()` and takes inertia from `shape.MomentOfInertia(mass)` (Box2D's capsule and rounded-polygon formulas). The body keeps polygon, capsule, segment and chain shapes in local space at angle 0 and writes `Shape` back rotated to its pose after each step. A polygon or capsule assigned to `Shape` in a struct literal or between steps keeps its world position: the next step moves `(X, Y)` to its centroid, because the solver turns the body about `(X, Y)` with inertia taken about the centroid, and replaces an `Inertia` equal to the one the body last derived (`derivedInertia`) with the new shape's; an `Inertia` the caller set to another value is kept.
- **Soft bodies and areas (`xpbd.go`, `area.go`)**: `SoftWorld` particles collide with polygon, capsule, segment and chain rigid bodies through the same hulls, buoyancy takes a body's area from `Shape.Area`, and linear point-gravity falloff uses half the bounds' diagonal when no unit distance is set.
- **Kinematic bodies (`tile.go`)**: `MoveAndSlide` resolves tiles against the bounding box of the shape and translates the whole shape by the resolved movement.
- **Tiled (`tilemap/physics.go`)**: `Object.PhysicsShape` maps a rectangle to a `Box` (or a `Polygon` when rotated), an ellipse to a `Circle` (or an 8-vertex `Polygon`), a polygon to a `Polygon` when it is simple, convex and has at most 8 vertices and to a closed `Chain` otherwise, and a polyline to a `Segment` or an open `Chain`. A polygon counts as convex only when every turn has the same sign and the turns add up to one full revolution, which excludes self-intersecting stars. `(*ObjectLayer).PhysicsBodies` returns nil for a nil layer, which is what `Tilemap.ObjectLayer` returns for a missing name.
- **MCP overlay**: the screenshot overlay draws the new shapes as stored, since they are already rotated; rotating them by the body's angle again would turn them twice. See [MCP screenshots and overlays](2026-11-24-mcp-screenshots-and-overlays.md).

## Alternatives considered

### GJK and EPA for every convex pair

GJK with EPA handles any convex support function, including shapes with more than 8 vertices, with one algorithm. It is rejected because EPA is iterative, harder to make bit-deterministic across platforms, and still needs a separate clipping step to give the two-point manifolds stacking needs; separating axis on rounded hulls with at most 8 vertices gives the manifold directly, allocates nothing (about 214 ns for a polygon pair) and follows the clipping of Box2D's `b2CollidePolygons`.

### Route boxes and circles through the new narrowphase too

One code path for every pair would remove the old box and circle functions. It is rejected for now because existing games depend on the exact contact points and depths those functions produce; changing them would alter replays and recorded inputs. The old paths stay until a change that is allowed to break determinism retires them.

### No vertex cap, or a larger one

Unbounded polygons avoid making authors split shapes. It is rejected because a fixed array keeps `Shape` comparable and allocation-free and bounds the separating axis loop; concave or many-sided terrain is a `Chain`, which is how Tiled outlines arrive anyway.

### One-sided chains as in Box2D's smooth segments

Box2D v3 makes chain segments one-sided and uses ghost vertices to drop contacts from behind. It is rejected because Tiled authors draw polylines in either direction and expect ground to be solid from both sides; two-sided segments with the convex-vertex normal test keep bodies from catching on seams without depending on winding. One-sided chains can come later as an option.

### Give each interior vertex to exactly one segment instead of removing duplicate points

Ownership by index (segment i owns only its start vertex) avoids comparing points. It is rejected because whether a contact belongs to a vertex is not visible from the manifold: when the other shape's face is the reference, the normal can lean toward a vertex while the contact points lie inside the segment, and dropping those would let a body fall through. Comparing points with the same normal only removes contacts that really are reported twice.

### Keep `(X, Y)` where the caller put it and use the parallel-axis inertia

Applying `I + m*d^2` for an off-centroid `(X, Y)` would honour a literal's `(X, Y)`. It is rejected because the body would then orbit a point outside its center of mass under any torque, which no caller wants, and because a `Box` literal already treats `(X, Y)` as the center; moving `(X, Y)` to the centroid gives free shapes the same meaning.

## Consequences

- **Benefits**: ramps, rotated props, capsule characters and Tiled terrain collide in every `physics` API with no allocation per pair. Boxes cross chain seams without snagging, and a ball rolling across 10 px chain segments tracks a single long segment to within 0.5 px. Contacts and simulations are bit-identical across runs.
- **Costs**: there is no continuous collision detection, so a body whose center crosses a segment or chain within one step passes through it; fast bodies must move less than one radius per step. Concave dynamic bodies are out of scope. `Shape` is larger than before and holds a pointer for chains. `MoveAndSlide` treats a polygon or capsule as its bounding box against tiles.
- **Testing**: `TestConvexShapePairs` covers separated, touching, overlapping, containment and NaN cases for every pair in both argument orders and checks that the push-out separates them; `TestChainSharedVertexGivesOneManifold` counts rigid manifolds at a convex peak, a flat seam and a loop's closing vertex; `TestRigidPolygonLiteralTurnsAboutCentroid` covers the struct-literal and reassignment contract; a fuzz test and benchmarks cover the narrowphase.
