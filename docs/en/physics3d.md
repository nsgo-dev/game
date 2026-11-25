# physics3d

```go
import "github.com/nsgo-dev/game/packages/game/physics3d"
```

Compact, deterministic 3D collision detection, raycasting, rigid-body dynamics, and a kinematic character controller written in pure Go.

## Architectural Decision (#180)

`physics3d` uses **option (a)**: a self-contained, zero-cgo Go implementation built only on the Go standard library.

Why pure Go instead of wrapping a C/C++ physics engine (such as Jolt, Bullet, or PhysX):

- **Every NSGo target compiles out of the box**: macOS, Windows, Linux, iOS, Android, and WebAssembly build with a plain `go build` / `go tool nsgo check` — no C/C++ toolchains, per-platform static libraries, or Emscripten/Wasm glue.
- **No FFI overhead**: Queries, raycasts, rigid-body steps, and character moves operate directly on Go values with zero cgo boundary crossings.
- **Bit-for-bit determinism**: Stepping a `World` with fixed durations from `game.Loop` runs broadphase, narrowphase, impulse solving, and positional correction in a fixed body and contact order with no map-order iteration or wall-clock input.

The scope is intentionally focused: boxes (axis-aligned or oriented), spheres, capsules, convex hulls, and cylinders, static triangle meshes (`TriMeshCollider`, added with `World.AddTriMesh`), heightfields (`HeightfieldCollider`, added with `World.AddHeightfield`), and voxel grids (`VoxelCollider`, added with `World.AddVoxelCollider`) that collide with bodies and the character controller, exact overlap, raycast, and voxel sweep queries, rigid bodies with linear and angular dynamics, joints, ragdolls, and a capsule character controller. Bodies use a scalar (isotropic) inertia rather than a full inertia tensor. Contacts between boxes, spheres and capsules use one representative point per body pair; a pair that involves a convex hull or a cylinder gets up to four points spread over the touching area.

## Coordinate Convention

`physics3d` uses a right-handed, Y-up coordinate system (`+X` right, `+Y` up, `+Z` toward the viewer). `NewWorld()` sets default gravity to `(0, -9.81, 0)`.

## Shapes, Overlaps, and Raycasts

Five primitives are wrapped by the tagged `Shape` value (`BoxShape`, `SphereShape`, `CapsuleShape`, `ConvexHullShape`, `CylinderShape`):

- `Box` — box defined by `Center` and `HalfExtents` (or constructed via `BoxFromMinMax(min, max)`). It is axis-aligned unless the wrapping `Shape` sets `Rot`, which orients it about its `Center`. `Shape.Transformed(pos, rot)` and `Body.WorldShape()` keep a rotated body's box oriented, so overlap tests, raycasts and debug drawing see its real corners; `Shape.Bounds()` returns the enclosing AABB for broad-phase use.
- `Sphere` — `Center` and `Radius`.
- `Capsule` — line segment from `A` to `B` swept by `Radius` (or `VerticalCapsule(center, radius, height)`).
- `ConvexHull` — a convex polyhedron for props such as rocks, debris, furniture, wedges and low-poly vehicle hulls, built once by `NewConvexHull(points, maxVertices)` and then shared, read-only, by every shape and body that uses it. Its vertices are relative to `Center`, and the wrapping `Shape`'s `Rot` turns it about `Center` as it does a box.
- `Cylinder` — a solid cylinder with flat caps whose axis runs from `A` to `B`, of the given `Radius` (or `VerticalCylinder(center, radius, height)`), for barrels, wheels and pillars. A cylinder whose `A` and `B` coincide or whose `Radius` is not positive has no volume, and collisions, raycasts and areas ignore it.

### Convex hulls

`NewConvexHull` builds the hull with quickhull: it starts from a tetrahedron of extreme points and adds the point furthest outside the hull until none is left or the hull has `maxVertices` vertices (`DefaultHullVertices`, 64, for a limit of 0 or less, at most `MaxHullVertices`, 256, and at least 4, the fewest vertices a hull with volume has, for a limit of 1 to 3). When the limit stops it early, the hull keeps the most prominent corners and lies inside the full hull. Points inside the hull or within a small tolerance of a face are dropped, and coplanar triangles are merged into polygon faces, so a cube gives 8 vertices and 6 square faces. The result depends only on the points and their order, so the same input always gives the same hull. Points that do not span a volume (fewer than four, all on a plane or a line, or not finite) return `ErrDegenerateHull`.

`ConvexHullFromMesh(positions, indices, maxVertices)` builds a hull from a triangle mesh, using only the vertices the indices reference, so a glTF primitive's or a `render3d.Mesh`'s `Positions` and `Indices` go in as they are:

```go
doc, _ := gltf.LoadFS(assets, "rock.gltf")
prim := doc.Meshes[0].Primitives[0]
hull, err := physics3d.ConvexHullFromMesh(prim.Positions, prim.Indices, 32)
if err != nil {
	return err
}
// Move the hull's centroid to the body's origin, so the body rotates about its center of mass.
hull = hull.Recentered()
pos := hull.Center
hull.Center = physics3d.Vec3{}
rock := world.AddBody(physics3d.NewDynamicBody(physics3d.ConvexHullShape(hull), pos, 40))
```

A hull reports its `Vertices`, `Faces` (vertex indices counter-clockwise seen from outside), `FaceNormals`, `Edges`, `Volume`, `Centroid` and `InertiaTensor(mass)` for uniform density. A body's inertia comes from the hull's exact tensor (the mean of its principal moments, as the scalar inertia above), and a cylinder's from the solid-cylinder formula.

Overlap tests return a `Contact` whose unit `Normal` points from `B` toward `A` (the direction to translate `A` by `Depth` to separate the shapes):

- `TestBoxBox`, `TestOrientedBoxBox` (separating axis theorem over the 15 face and edge axes), `TestSphereSphere`, `TestCapsuleCapsule`, `TestBoxSphere`, `TestBoxCapsule`, `TestSphereCapsule`, and `TestShapes(a, b)`, which handles oriented boxes.
- `TestShapes` tests every pair that involves a convex hull or a cylinder, against any other shape, with GJK for the distance and EPA for the penetration depth and normal, in float64 and a fixed iteration order, so the same shapes always give the same contact. `Point` is the center of the touching area (a face resting on a face reports the middle of the overlap, not a corner). The same test runs against each triangle of a `TriMeshCollider` and a `HeightfieldCollider`, and against voxel colliders.
- When a capsule's segment passes through a box, `TestBoxCapsule` takes the axis of least penetration over the box faces and the edges crossed with the segment, so moving the box by `Normal*Depth` clears the whole capsule. Crossing capsules, and a sphere centered on a capsule's axis, get a normal perpendicular to the axes rather than an arbitrary one.
- `world.QueryOverlap(shape)` returns all overlapping bodies in deterministic insertion order.

Raycasts test a `Ray{Origin, Dir, MaxDist}` against standalone shapes (`RaycastBox`, `RaycastSphere`, `RaycastCapsule`, `RaycastConvexHull`, `RaycastCylinder`, `RaycastShape`) or cast a segment through a world (`world.Raycast(from, to)` and `world.RaycastFirst(from, to)`), returning `RaycastHit` entries sorted by ascending `T` in `[0, 1]` with world-space `Point`, `Distance`, and outward unit `Normal`. A ray that starts inside any shape hits it at `T` 0 with `Point` at the origin and `Normal` facing back along the ray (`-Dir`).

## Rigid Bodies

A `World` holds static (`Static: true` or zero mass) and dynamic `Body` values and advances them in `Step(dt)` using symplectic Euler integration, deterministic collision detection, warm-started iterative impulse solving with isotropic 3D Coulomb friction, non-linear Gauss-Seidel positional correction, and automatic sleeping for settled bodies.

```go
world := physics3d.NewWorld()
world.AddBody(physics3d.NewStaticBody(
	physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 1, Z: 20}}),
	physics3d.Vec3{Y: -1},
))
crate := world.AddBody(physics3d.NewDynamicBody(
	physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
	physics3d.Vec3{Y: 3},
	1.0,
))

loop := &game.Loop{
	Update: func(dt time.Duration) {
		world.Step(dt)
		_ = crate.Pos
	},
}
```

Contact impulses act at the contact point, so friction makes a sliding sphere roll and a box standing on an edge tips over onto a face. `LockRotation` keeps a dynamic body from rotating. Inputs that are not finite (a NaN velocity or mass) fall back to defaults in `AddBody`, and a kinematic body without mass stays kinematic.

Settled bodies sleep. A sleeping body wakes when a moving dynamic body or a kinematic body (moving by velocity or by setting `Pos`/`Rot` directly) touches it, when a body jointed to it moves, when one of its contacts ends (its support moved away), when a `VoxelCollider` block near it changes solidity or disappears under it, and on `Wake`, `WakeInBox`, `ApplyImpulse`, and the removal of bodies, meshes, voxel colliders, or joints.

Stacks rest without sinking into each other. After the iterative solver, each step makes one bottom-up pass over the contacts within about 45° of vertical (relative to `-Gravity`) that stops each upper body against the one below and pushes it out until about 1 mm of overlap is left, treating the lower body as immovable (shock propagation). A heavy body on a light one, or a tall stack, settles at that depth instead of the several centimeters iterative solving alone leaves under large mass ratios. A body still sunk more than 5 mm into a neighbor does not fall asleep, and a sleeping body pushed deeper than 1 cm wakes, so sleeping never freezes an overlap in place. With zero gravity there is no "up" and the pass is skipped.

A shape that ends up below a heightfield's surface is pushed out upward, by the depth measured along the upward triangle normal, so it clears the surface in one correction rather than by the shallower distance to the nearest point underneath. A shape beside a cell, such as one at the foot of a step touching the plateau's edge, keeps the depth of its own contact, so it is not lifted onto the plateau.

A dynamic body that moves more than half its smallest dimension in one step is swept in sub-steps against static, kinematic and sleeping bodies, triangle meshes, heightfields, and voxel colliders, and stops at the first new overlap, so fast bodies do not tunnel through thin geometry. Contacts the body already has when the step begins, such as the floor under a fast sliding or rolling body, do not stop it unless the body sinks clearly deeper into them. Fast dynamic bodies are not swept against each other.

## Voxel Colliders

`VoxelCollider` (created with `NewVoxelCollider(grid)` and added with `World.AddVoxelCollider`, or adapted from `game/voxel` via `voxel.NewCollider(w)` / `w.Collider()`) collides bodies, `CharacterController`, CCD, and world raycast/overlap queries directly against any `VoxelGrid` without building a triangle mesh:

- **Direct queries**: `QueryBlocks`, `TestShape`, `CollectContacts`, `Raycast` (3D Amanatides & Woo DDA, accelerated when the grid implements `VoxelBoundsGrid`), and `Sweep` (exact Minkowski-sum swept AABB for axis-aligned boxes; sub-stepped binary-search TOI for spheres, capsules, and oriented boxes) visit solid voxels in deterministic `(y, z, x)` order.
- **Internal-seam filtering**: faces occluded by adjacent solid blocks are filtered out so crates, spheres, capsules, and characters never snag on flat voxel floor or wall seams.
- **Instant edit reflection & wakeup**: block changes take effect immediately with zero rebuild. Grids implementing `VoxelEditNotifier` automatically call `NotifyBlockChanged` / `NotifyReset` (which wake nearby sleeping bodies and stacked contacts via `World.WakeInBox`), while plain `VoxelGrid` implementations still wake bodies whose voxel support disappears on the next `World.Step`.

## glTF, GridMap, and Heightmap Colliders

Build static level geometry directly from glTF scenes, `world3d` GridMaps, and standard Go heightmap images without manually unpacking vertices or cell boxes:

- **glTF triangle meshes**: `NewTriMeshFromGLTFPrimitive(prim, worldMat)`, `NewTriMeshFromGLTFMesh(mesh, worldMat)`, and `NewTriMeshFromGLTFNode(doc, nodeIdx)` build BVH-accelerated `*TriMeshCollider` values from indexed or non-indexed `Triangles`, `TriangleStrip`, and `TriangleFan` primitives, applying full node world transforms and flipping winding automatically when the transform has a negative determinant.
- **glTF collider conventions (`-colonly` & `extras.collider`)**: `World.AddGLTFColliders(doc, opts)` and `BuildGLTFColliders(doc, opts)` recognize Godot-style node name suffixes (`-col`, `-colonly`, `-boxcol`, `-boxcolonly`, `-spherecol`, `-spherecolonly`, `-capsulecol`, `-capsulecolonly`, `-convcol`, `-convcolonly`, case-insensitive and tolerating Blender `.001` duplicate numbering) as well as `node.Extras` (`{"collider": "box" | true | {"shape", "draw", "only", "trigger", "tag", "center", "halfExtents"|"size", "radius", "height", "friction", "restitution", "layer", "mask"}}`). Primitive box and capsule colliders preserve full node rotation as oriented boxes and rotated capsules; `-convcol` / `-convcolonly` build a `TriMeshCollider` until native convex hulls are added. Visual renderers filter out collider-only nodes with `doc.DrawableNodes()` or `node.IsDrawable()` in [`game/gltf`](3d-models-blender.md#collision-nodes--colonly-and-extrascollider). Call `GLTFColliders.RemoveFromWorld(world)` when unloading a streamed level.
- **Greedy 3D box merging & `GridMap` bodies**: `MergeBoxes(boxes, eps)` greedily merges adjacent axis-aligned boxes along X, then Z, then Y within tolerance `eps` (default `1e-4`), preserving the exact solid volume in deterministic order. On a [`world3d.GridMap`](world3d.md#modular-3d-gridmap), `grid.AddCollidersToWorld(world, opts)` extracts cell `ColliderBoxes` and merges adjacent cells into minimal static bodies (set `DisableMerge: true` to emit one body per box).
- **Heightfields from `image.Image`**: `NewHeightfieldFromImage(img, HeightfieldImageOptions{...})` builds a `*HeightfieldCollider` from any 8-bit/16-bit grayscale or color `image.Image` (`HeightmapLuminance`, `HeightmapRed`, `HeightmapGreen`, `HeightmapBlue`, `HeightmapAlpha`), with configurable `CellSize` or `WorldWidth`/`WorldDepth`, `MinHeight`/`MaxHeight`, `Origin`, `FlipZ`, and optional `(Cols, Rows)` resampling (`Bilinear: true` for smooth interpolation). For an existing `world3d.Heightmap`, call `hm.Collider()`.

```go
doc, err := gltf.LoadFS(assetsFS, "levels/courtyard.glb")
if err != nil {
	return err
}
levelCols, err := world.AddGLTFColliders(doc, physics3d.GLTFColliderOptions{})
if err != nil {
	return err
}
// Render only visual nodes (skips -colonly / draw:false collider nodes):
for _, nodeIdx := range doc.DrawableNodes() {
	drawGLTFNode(doc, nodeIdx)
}

hf, err := physics3d.NewHeightfieldFromImage(heightImg, physics3d.HeightfieldImageOptions{
	WorldWidth: 64,
	MinHeight:  -2,
	MaxHeight:  18,
})
if err == nil {
	world.AddHeightfield(hf)
}
_ = levelCols
```

## Gravity Scale and Area Volumes

`Body` and `CharacterController` each support a per-entity `GravityScale` (initialized to `1` by `NewDynamicBody` and `NewCharacterController`; use `SetGravityScale(0)` for weightless objects or negative values to invert gravity).

Any trigger body (`Body.IsTrigger = true`) can configure `Body.Area` (`AreaSettings`) to override gravity, linear/angular damping, and fluid buoyancy/drag for overlapping dynamic bodies during `World.Step` and for `CharacterController.Move`:

- **Gravity modes & priority**: set `GravityMode` to `AreaOverrideCombine`, `AreaOverrideReplace`, or `AreaOverrideAdd` ordered by `Priority` (higher priority first, deterministic insertion-order tie-breaking). Supports both directional `Gravity` and radial `PointGravity` toward `GravityPoint` with `GravityStrength`, `GravityFalloff` (`GravityFalloffNone`, `GravityFalloffLinear`, `GravityFalloffInverse`, `GravityFalloffInverseSquare`), and `GravityUnitDistance`.
- **Damping overrides**: `LinearDampingMode` / `LinearDamping` and `AngularDampingMode` / `AngularDamping` override body damping using the same priority and combine/replace/add rules.
- **Buoyancy and fluid drag**: set `FluidDensity` (kg/m³) to apply Archimedes buoyancy from the submerged fraction of each body's bounds so a body with density `Mass / Volume` settles at submerged fraction `(Mass / Volume) / FluidDensity`, alongside `FluidLinearDrag`, `FluidAngularDrag`, and optional `FluidVelocity` current.
- **Area shapes**: an area can be any shape. A point is inside a convex hull or a cylinder area by its exact shape, not its bounds, and a hull's or cylinder's buoyant volume is its own (`ConvexHull.Volume`, or πr²h).
- **Queries & characters**: call `world.GravityAt(point)` or `world.AreaEffectAtShape(shape, layer, mask)` to inspect the resolved field. `CharacterController.Move` automatically applies overlapping area gravity (scaled by `CharacterController.GravityScale`), fluid drag, and linear damping, and `CharacterController.EffectiveGravity(world)` reports the active gravity vector.

```go
water := physics3d.NewStaticBody(
	physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 5, Z: 20}}),
	physics3d.Vec3{Y: -5},
)
water.IsTrigger = true
water.Area = physics3d.AreaSettings{
	FluidDensity:     1000,
	FluidLinearDrag:  5.0,
	FluidAngularDrag: 2.0,
}
world.AddBody(water)
```

## Joints and Ragdolls

`World.AddJoint` solves `DistanceJoint`, `BallJoint`, `SwingTwistJoint`, `HingeJoint`, `SliderJoint`, `FixedJoint`, and `SixDOFJoint` in deterministic insertion order:

- `HingeJoint.Angle` is tracked across steps, so a hinge that keeps turning reports angles past ±π and `MinAngle`/`MaxAngle` can span more than one turn. Optional spring and position drives (`DriveTargetAngle`, `DriveStiffness`, `DriveDamping`, `DriveMaxTorque`) pull a hinge smoothly toward a target angle (for self-closing doors or active joints), alongside velocity motors (`MotorTargetVel`, `MotorMaxTorque`).
- `SliderJoint` supports optional linear spring and position drives (`DriveTargetPos`, `DriveStiffness`, `DriveDamping`, `DriveMaxForce`) in addition to limits and velocity motors.
- `DistanceJoint` is slack while its distance is inside `[MinDistance, MaxDistance]`; `Stiffness` and `Damping` soften the limits, so for a spring with rest length `L` set both limits to `L`.
- `SwingTwistJoint` constrains a ball-and-socket pivot with a four-quadrant asymmetric elliptic swing cone (`SwingMin1`, `SwingMax1`, `SwingMin2`, `SwingMax2`) and an independent twist range (`MinTwist`, `MaxTwist`), readable via `SwingAngles()` and `TwistAngle()`. `BuildRagdoll` uses `SwingTwistJoint` for non-hinge limbs (shoulders, hips, spine, head) and `HingeJoint` for elbows, knees, and bones with an explicit `HingeAxis`; `SkeletonJoint` accepts optional asymmetric swing limits and `TwistLimit` / `MinTwist` / `MaxTwist` (defaulting to ±π/4 twist when `SwingLimit > 0` so limbs cannot spin freely around their own axis).
- `SixDOFJoint` configures each of the 3 linear (`Linear[0..2]`) and 3 angular (`Angular[0..2]`) axes independently via `AxisConfig`: `Mode` (`AxisFree`, `AxisLocked`, `AxisLimited` with `Min`/`Max`), passive spring `Stiffness`/`Damping`, and `Drive` (`DriveNone`, `DrivePosition`, `DriveVelocity`) toward `TargetPos`/`TargetVel` clamped by `MaxForce`.
- Every built-in joint reports its last-step world-space `ReactionForce()` (N) and `ReactionTorque()` (N·m) applied to `BodyB`, and supports optional `BreakForce` and `BreakTorque` thresholds. When a joint exceeds either threshold during `World.Step`, it is marked `Broken()`, removed from the world before positional correction, wakes dynamic bodies, and is reported deterministically in `World.JointBreakEvents()` and `World.OnJointBreak`.

## Character Controller

`CharacterController` moves a kinematic capsule (`Pos` at the feet, `Radius`, `Height`) through a `World` via `Move(world, wishMove, dt)`:

- Walks across flat ground and glides smoothly up and down slopes up to `MaxSlopeAngle` (default 45°), staying grounded on descents.
- Prevents climbing slopes steeper than `MaxSlopeAngle` and slides down them under gravity.
- Automatically steps up stairs up to `StepHeight` after verifying headroom clearance, forward progress, and a walkable landing tread.
- Stops cleanly and slides tangentially along vertical walls without penetration.
- Pushes dynamic bodies it walks into with `PushForce` newtons; with the default 0 they block it like walls.

```go
cc := physics3d.NewCharacterController(physics3d.Vec3{Y: 0}, 0.3, 1.8)
loop := &game.Loop{
	Update: func(dt time.Duration) {
		res := cc.Move(world, physics3d.Vec3{X: 3.5}, dt)
		_ = res.Pos
	},
}
```

The controller is not a body in the `World`, so dynamic bodies do not collide with it on their own. To let them, add a kinematic body that follows `Pos` and set `IgnoreBodyID` to its ID.

## Debug Drawing

`DrawWorld`, `DrawBody`, `DrawShape`, `DrawCharacter`, `DrawContact` and `DrawRaycast` draw collider wireframes, contacts and rays onto a `core.Drawing` through a view-projection matrix, styled by `DebugStyle`; `DrawLine3D` draws one segment. Convex hulls draw every hull edge in `HullColor` and cylinders a ring around each cap and four side lines in `CylinderColor`; `ConvexHullEdges` and `CylinderEdges` return those segments for other renderers. Release builds compile them to no-ops.

Segments are clipped to the camera frustum before projection. By default the near plane is clip `z >= -w`, which matches this package's `Mat4Perspective` and `Mat4Ortho` (`[-1, 1]` depth). For a `[0, 1]` depth projection, such as `math3d.Perspective`, `math3d.Ortho` or a `render3d` camera, set `DebugStyle.DepthZO` (and use `DrawLine3DZO` for single segments): it clips at `z >= 0`, as `render3d.ProjectSegment` does. Without it an orthographic `[0, 1]` camera, whose `w` is 1 everywhere, draws colliders behind the camera.

```go
style := physics3d.DefaultDebugStyle()
style.DepthZO = true // viewProj comes from math3d.Perspective or math3d.Ortho
physics3d.DrawWorld(d, physics3d.Mat4(viewProj), viewport, world, style)
```

## Ropes, Cloth, and Soft Bodies (XPBD)

`SoftWorld` simulates 3D ropes, cloth meshes, and tetrahedral soft bodies using deterministic Extended Position-Based Dynamics (XPBD):

- **Particles & constraints**: `Particle` (`Pos`, `Vel`, `InvMass`, `Radius`, `Friction`, `Restitution`, `Pinned`, `Layer`/`Mask`), bilateral & unilateral `DistanceConstraint` (`AddDistance`, `AddTether`), singularity-free dihedral `BendingConstraint` (`AddBending`), and tetrahedral `VolumeConstraint` (`AddVolume`).
- **Two-way rigid-body attachments**: `AttachBody` couples particles to dynamic or kinematic `*Body` instances at local anchors (`Compliance == 0` for rigid pins, `> 0` for elastic tethers), returning linear and angular reaction impulses to the attached body and waking sleeping bodies automatically.
- **Collisions & layer filtering**: setting `SoftWorld.World` collides particles against non-trigger `Body` shapes (oriented boxes, spheres, capsules), `TriMeshCollider`, and `HeightfieldCollider`, respecting `Layer`/`Mask` filtering.
- **Builders & debug drawing**: `AddRope(RopeConfig)` builds inextensible 3D ropes with unilateral long-range tethers; `AddCloth(ClothConfig)` builds a triangulated cloth grid with structural, shear, and dihedral bending constraints (`ClothGrid.Index(col, row)`); `AddSoftBox(SoftBoxConfig)` builds an 8-vertex, 5-tetrahedron deformable cube; `Edges()` returns structural wireframe segments, and `DrawSoftWorld(d, viewProj, viewport, sw, style)` renders them (no-op in `-tags release` builds).

```go
world := physics3d.NewWorld()
world.AddBody(physics3d.NewStaticBody(
	physics3d.SphereShape(physics3d.Sphere{Radius: 1.0}),
	physics3d.Vec3{Y: 1.0},
))

sw := physics3d.NewSoftWorld()
sw.World = world
sw.AddCloth(physics3d.ClothConfig{
	Origin:      physics3d.Vec3{X: -1.1, Y: 2.25, Z: -1.1},
	Width:       2.2,
	Height:      2.2,
	Cols:        10,
	Rows:        10,
	PinTopLeft:  true,
	PinTopRight: true,
})

// Inside game.Loop.Update:
world.Step(dt)
sw.Step(dt)
```

## Snapshots, Rollback, and Cross-Architecture Determinism

`World` supports full state snapshots, deterministic FNV-1a 64-bit checksums, and cross-architecture determinism across amd64 and arm64:

- **`Save() / Restore(snap) / Checksum()`**: `World.Save()` captures all public and private solver state into a versioned `WorldSnapshot` (`SnapshotVersion`), including gravity and solver/sleep settings, bodies (with their full shape, including a `ConvexHull`'s geometry and a `Cylinder`, private sleep timers, gravity-scale flags, residual depth, previous interpolation poses, and voxel resting state), sorted warm-start contact impulse cache, active contact/trigger pair sets, last-step contact and joint-break events, ignored body pairs, all 7 built-in `Joint3D` types (`DistanceJoint`, `BallJoint`, `HingeJoint`, `SliderJoint`, `FixedJoint`, `SwingTwistJoint`, `SixDOFJoint`, including unwrapped angle state and last reaction impulses), plus `TriMeshCollider`, `HeightfieldCollider`, and `VoxelGrid` collider bindings.
- **JSON-serializable plain text**: `WorldSnapshot` marshals cleanly with `encoding/json` (no `encoding/gob` or custom binary layout).
- **In-place pointer & callback preservation**: `Restore` updates existing `*Body`, `Joint3D`, and collider instances in place when counts/kinds match, preserving gameplay pointers, `UserData`, and world callbacks (`OnContact`, `OnTrigger`, `OnJointBreak`).
- **Cross-architecture determinism**: all stepped 3D integration, narrowphase (GJK/EPA/SAT, OBB, mesh/heightfield, voxel), sequential impulse solver, joint, area, and `vehicle` suspension/tire paths avoid arm64 FMA contraction and FMA-using standard library transcendentals, ensuring bit-identical trajectories and `Checksum()` values on amd64 and arm64.

```go
snap := world.Save()
hash := world.Checksum()

// Serialize to plain-text JSON (or store in a net.SimState rollback buffer):
raw, _ := json.Marshal(snap)

var restored physics3d.WorldSnapshot
_ = json.Unmarshal(raw, &restored)
if err := world.Restore(restored); err != nil {
	return err
}
_ = hash
```
