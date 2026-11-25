# physics

`game/physics` provides 2D collision shapes (`Box`, `Circle`, convex `Polygon`, `Capsule`, `Segment` and `Chain`), exact overlap contacts, anti-tunneling tile resolution with `MoveAndSlide`, and deterministic spatial queries and raycasts on `World`.

```go
import "github.com/nsgo-dev/game/packages/game/physics"
```

Coordinates follow NSGo's screen space: X increases to the right and Y increases downward.

## Shapes and overlap tests

`Box{X, Y, W, H}` (top-left origin) and `Circle{X, Y, R}` (centered) unify under `Shape` (`BoxShape(b)`, `CircleShape(c)`). Test pairs directly with `TestBoxBox`, `TestCircleCircle`, `TestBoxCircle`, `TestCircleBox`, or `TestShapes`:

```go
playerBox := physics.Box{X: 32, Y: 48, W: 14, H: 20}
coin := physics.Circle{X: 44, Y: 54, R: 6}

if c := physics.TestBoxCircle(playerBox, coin); c.Hit {
	// Normal points from B (coin) toward A (playerBox);
	// translating A by (NormalX*Depth, NormalY*Depth) separates them.
	_ = c
}
```

Shapes that only touch along an edge or corner with zero penetration report `Hit == false`. Empty shapes (`W <= 0`, `H <= 0`, or `R <= 0`) never overlap anything.

## Polygons, capsules, segments and chains

Ramps, wedges, rotated props, characters and terrain drawn as lines need more than boxes and circles. Four more shapes, modelled on Box2D's, share `Shape` and work everywhere a box does: `TestShapes`, `Body`, `Trigger`, `World` queries and raycasts, `RigidBody` and debug drawing.

| Shape | Constructor | Use |
| --- | --- | --- |
| `Polygon` | `NewPolygon(points, radius)`, `NewBoxPolygon(cx, cy, w, h, angle)`, then `PolygonShape(p)` | Convex polygon of 3 to `MaxPolygonVertices` (8) vertices, optionally rounded by `Radius` so it slides over corners |
| `Capsule` | `CapsuleShape(Capsule{X1, Y1, X2, Y2, R})` | A segment swept by a radius, for characters that should not snag on tile seams or step edges |
| `Segment` | `SegmentShape(Segment{X1, Y1, X2, Y2})` | A line with no thickness, for ledges and walls |
| `Chain` | `ChainShape(points, loop)` | A polyline, or a closed loop, for static terrain |

`NewPolygon` computes the convex hull of any points, merges near duplicates, drops collinear vertices and winds the result clockwise on screen; it reports `false` for fewer than 3 or more than 8 hull vertices. Write a `Polygon` by hand only when it is already convex and wound that way.

```go
ramp, _ := physics.NewPolygon([]core.Point{{X: 0, Y: 200}, {X: 300, Y: 200}, {X: 300, Y: 50}}, 0)
hero := physics.CapsuleShape(physics.Capsule{X1: 120, Y1: 100, X2: 120, Y2: 130, R: 8})
if c := physics.TestShapes(hero, physics.PolygonShape(ramp)); c.Hit {
	// Push the hero out along the slope's normal.
	_ = c
}
```

Every pair reports a hit with its normal and depth. Pairs of boxes and circles keep their own tests; every other pair runs the separating axis test on the shapes' cores (a polygon, a segment or a point) plus their rounding radii, and reports the contact point midway between the two surfaces. Segments and chains have no area: they collide with every shape that has one, from either side, but never with each other, and they contain no point, so `QueryPoint` never returns them.

A chain reports its deepest segment contact. At an interior vertex it only uses the normals its neighbouring segments allow, so a box sliding or a ball rolling along a flat run of segments never catches on the seams between them, the ghost collisions Box2D's chains also remove. Its open ends behave like a lone segment's ends.

`Shape.Area`, `Shape.Centroid` and `Shape.MomentOfInertia(mass)` give the mass properties a rigid body uses, approximating a rounded polygon by pushing its vertices out as Box2D does.

`MoveAndSlide` and `CharacterBody` resolve tiles against a body's bounding box, so a polygon or capsule `Body` moves as its bounds do and keeps its shape. `DrawShape` outlines polygons (with rounded corners), capsules with their axis, segments and chains, in `PolygonColor`, `CapsuleColor` and `SegmentColor`.

Like any shape here, a segment or chain cannot stop a body whose center crosses it within one step, since there is no continuous collision detection: keep fast bodies under one radius of movement per step, or step the world more often.

## MoveAndSlide and TileGrid

`MoveAndSlide` advances a `*Body` by `(VX*dt, VY*dt)` against any `TileGrid` on each fixed `game.Loop` step, resolving X before Y, zeroing velocity along blocked axes, and subdividing large displacements so fast bodies cannot tunnel through single-tile walls or platforms:

```go
grid := physics.NewGrid(40, 20, 16, 16)
grid.FillRect(0, 18, 40, 2, physics.TileSolid)
grid.FillRect(10, 14, 6, 1, physics.TileOneWay)

player := physics.NewBoxBody(32, 200, 12, 16)
player.Tag = "player"

// Inside game.Loop.Update:
player.VY += 900 * float32(dt.Seconds()) // gravity
player.OneWayDrop = actions.Pressed("down") && actions.JustPressed("jump")

res := physics.MoveAndSlide(player, grid, dt)
if res.Grounded && actions.JustPressed("jump") {
	player.VY = -320
}
```

- **Contact flags**: `MoveAndSlide` updates both the returned `MoveResult` and the body's `Grounded`, `OnCeiling`, `OnLeftWall`, and `OnRightWall` flags, plus per-tile `Contacts` (`Col`, `Row`, `Flags`, `NormalX`, `NormalY`).
- **One-way platforms**: tiles marked `TileOneWay` block downward motion only when the body starts at or above the tile's top edge, unless `Body.OneWayDrop` is `true`.
- **Tilemaps**: a [`tileshape.View`](tileshape.md) of a [`tilemap.Tilemap`](tilemap.md) is a `TileGrid`, so pass it to `MoveAndSlide`, `CharacterBody.Move` and `World` directly, together with the `Slope` segments it derives from per-tile collision shapes.
- **Custom `TileGrid`**: implement `TileSize() (float32, float32)`, `GridSize() (int, int)`, and `TileFlagsAt(col, row int) TileFlags` to plug any other map into `MoveAndSlide` and `World`.
- **Slopes and tiles together**: `CharacterBody.Move` lifts a character onto a walkable slope before testing tile walls, so the top of a slope that meets a solid tile's top walks onto the tile, and when the character walks off the lower end of a slope it snaps down onto a tile top within `SnapDist`.

## World queries and raycasts

`World` groups movable `Bodies` and an optional `Tiles` (`TileGrid`) for deterministic point, overlap, and raycast queries:

```go
world := &physics.World{Tiles: grid}
world.Add(player, enemy, trigger)

// Point query (bodies in World.Bodies order)
clicked := world.QueryPoint(mouseWorld.X, mouseWorld.Y)

// Shape overlap against bodies and solid tiles
hitbox := physics.BoxShape(physics.Box{X: attackX, Y: attackY, W: 24, H: 16})
hits := world.QueryOverlapMask(hitbox, physics.QueryBodies|physics.QueryTiles)
for _, h := range hits {
	if h.Body != nil && h.Body.Tag == "enemy" {
		damageEnemy(h.Body)
	}
}

// Raycast for line of sight or hitscan weapons
if hit, ok := world.RaycastFirst(gunPos, targetPos, physics.QueryAll); ok {
	spawnImpactSpark(hit.Point, hit.NormalX, hit.NormalY)
}
```

`QueryMask` flags (zero value means `QueryAll`):

| Mask | Targets included |
| --- | --- |
| `QueryBodies` | All bodies in `World.Bodies` (returned in slice order). |
| `QueryTiles` | Solid tiles (`TileSolid`) in `World.Tiles`. |
| `QueryOneWay` | One-way tiles (`TileOneWay`); raycasts hit them only when entering the top edge moving downward. |
| `QueryAll` | `QueryBodies \| QueryTiles \| QueryOneWay`. |

`World.Raycast` returns all hits along the segment sorted by ascending normalized distance `T` in `[0, 1]` (bodies before tiles on ties), while `RaycastFirst` returns the closest hit. A ray hits a segment or chain from either side with the normal facing its start, and misses one it runs along. Remove bodies cleanly with `world.Remove(b)`.

## Level geometry from Tiled

`tilemap.Object.PhysicsShape` turns a Tiled rectangle, ellipse, polygon or polyline object into a shape, and `ObjectLayer.PhysicsBodies` returns a static `Body` for each object of a layer, so terrain drawn in Tiled collides without a hand-written converter:

```go
for _, ground := range m.ObjectLayer("collision").PhysicsBodies() {
	world.Add(ground)                                      // kinematic queries and triggers
	rigid.AddBody(physics.NewStaticRigidBody(ground.Shape)) // rigid bodies
}
```

See [tilemap](tilemap.md) for how each object maps to a shape.

## Ropes and Soft Bodies (XPBD)

`SoftWorld` provides a deterministic, pure-Go Extended Position-Based Dynamics (XPBD) particle and constraint solver for 2D ropes, hanging chains, and deformable soft bodies:

- **Particles & constraints**: `Particle` (position, velocity, `InvMass`, `Radius`, `Friction`, `Restitution`, `Pinned`, `Layer`/`Mask`), bilateral & unilateral `DistanceConstraint` (`AddDistance`, `AddTether`), 3-particle angular `BendingConstraint` (`AddBending`), pressurized polygon `AreaConstraint` (`AddArea`), and 2D rigid `ShapeMatchConstraint` (`AddShapeMatch`).
- **Two-way rigid-body coupling**: `AttachRigidBody` couples any particle to a `*RigidBody` at a local anchor (`Compliance == 0` for rigid pins, `> 0` for elastic attachments), smoothly substepping attached poses and feeding linear and angular reaction impulses back into the `RigidBody`.
- **World & tile collisions**: set `SoftWorld.World`, `SoftWorld.RigidWorld`, and/or `SoftWorld.Tiles` to collide particles against `Box`, `Circle`, oriented `RigidBody` shapes, `TileSolid`, and `TileOneWay` tiles with `LayerMask` filtering (`TileLayer`/`TileMask`).
- **Builders & debug drawing**: `AddRope(RopeConfig)` builds inextensible ropes with automatic long-range tethers so heavy attached crates swing without over-stretching; `AddSoftBody(SoftBodyConfig)` builds pressurized or shape-matched soft rings; `DrawSoftWorld(d, sw, style)` renders debug overlays (compiled to a no-op under `-tags release`).

```go
rw := &physics.RigidWorld{GravityY: 600}
crate := physics.NewRigidBox(120, 12, 24, 24, 10)
rw.AddBody(crate)

sw := physics.NewSoftWorld()
sw.RigidWorld = rw
sw.GravityY = 600

rope := sw.AddRope(physics.RopeConfig{
	StartX: 0, StartY: 0, EndX: 120, EndY: 0,
	Segments: 12, TotalMass: 0.5, PinStart: true,
})
sw.AttachRigidBody(rope[len(rope)-1], crate, core.Point{Y: -12}, 0)

// Inside game.Loop.Update:
rw.Step(dt)
sw.Step(dt)
```

## Gravity Scale and Area Overrides

`RigidBody` and `CharacterBody` support a per-body `GravityScale` (default `1`, set `0` with `SetGravityScale(0)` for weightless bodies, or negative values to invert gravity).

Any `Trigger` (registered via `RigidWorld.AddTrigger` for rigid bodies or `World.AddTrigger` for queries and character controllers) can configure `Trigger.Area` (`AreaSettings`) to override gravity, damping, and fluid buoyancy/drag for overlapping bodies during `RigidWorld.Step` and `CharacterBody.Move`:

- **Gravity overrides**: set `GravityMode` to `AreaOverrideCombine`, `AreaOverrideReplace`, or `AreaOverrideAdd` with `Priority` (higher priority wins; equal priority breaks ties by trigger slice order). Use directional `(GravityX, GravityY)` or set `PointGravity: true` with `(GravityPointX, GravityPointY)`, `GravityStrength`, `GravityFalloff` (`GravityFalloffNone`, `GravityFalloffLinear`, `GravityFalloffInverse`, `GravityFalloffInverseSquare`), and `GravityUnitDistance`.
- **Damping overrides**: `LinearDampingMode` / `LinearDamping` and `AngularDampingMode` / `AngularDamping` override a `RigidBody`'s base damping using the same priority and combine/replace/add rules.
- **Buoyancy and fluid drag**: set `FluidDensity` (mass per unit area) to apply Archimedes buoyancy from the submerged fraction of the body's bounds so a body of density `Mass / Area` floats at submerged fraction `(Mass / Area) / FluidDensity`, paired with `FluidLinearDrag`, `FluidAngularDrag`, and optional current velocity `(FluidVelX, FluidVelY)`.
- **Character controllers**: set `CharacterBody.World` (with `World.GravityX` / `World.GravityY` and `World.Triggers`) so `CharacterBody.Move` automatically integrates area-resolved gravity (scaled by `CharacterBody.GravityScale`), fluid drag, and linear damping. Query resolved fields directly with `GravityAt(x, y)` and `AreaEffectAtShape(shape, layer, mask)`.

```go
water := &physics.Trigger{
	Shape: physics.BoxShape(physics.Box{X: 0, Y: 100, W: 400, H: 200}),
	Area: physics.AreaSettings{
		FluidDensity:     1.0,
		FluidLinearDrag:  4.0,
		FluidAngularDrag: 2.0,
		FluidVelX:        30,
	},
}
rigidWorld.AddTrigger(water)
```

## Snapshots, Rollback, and Cross-Architecture Determinism

`World` and `RigidWorld` support full state snapshots and bit-exact determinism across CPU architectures (amd64 and arm64):

- **`Save() / Restore(snap) / Checksum()`**: `World.Save()` returns a versioned `WorldSnapshot` (`SnapshotVersion`) capturing gravity, tile grid, bodies, triggers, and per-trigger enter/stay/exit tracking state. `RigidWorld.Save()` returns a `RigidWorldSnapshot` capturing gravity, solver settings, bodies (including pending force/torque accumulators, gravity-scale flags, and the local-space geometry the solver rotates for `Polygon`, `Capsule`, `Segment`, and `Chain` shapes), triggers/areas, joint-break events, and all built-in joints (`DistanceJoint` including rope/range mode, `SpringJoint`, `RevoluteJoint`, `PrismaticJoint`, `WeldJoint`, `WheelJoint`, and `MouseJoint`, with their limits, motors, springs, reaction impulses, and break thresholds; see [2D Rigid Bodies & Joints](physics2d-rigid.md)).
- **JSON-serializable plain text**: `WorldSnapshot` and `RigidWorldSnapshot` marshal cleanly with `encoding/json` without `encoding/gob` or custom binary layouts.
- **In-place pointer & callback preservation**: `Restore` mutates existing `*Body`, `*RigidBody`, `*Trigger`, and `Joint` instances in place when counts/kinds match, preserving gameplay entity references, `UserData`, and trigger callbacks (`OnEnter`/`OnStay`/`OnExit`) across rollbacks.
- **Cross-architecture determinism**: all stepped solver, integration, joint, narrowphase, and area paths avoid arm64 fused multiply-add (`FMADD`/`FMSUB`) contraction and FMA-using standard library transcendentals, so saving at step 100, stepping to 200, restoring, and re-stepping produces bit-identical positions, velocities, contacts, events, and `Checksum()` values on both amd64 and arm64.

```go
snap := rigidWorld.Save()
hashAtSave := rigidWorld.Checksum()

// Persist or transmit as plain-text JSON (or keep in a rollback ring buffer):
raw, _ := json.Marshal(snap)

// Later, restore and re-simulate bit-identically:
var restored physics.RigidWorldSnapshot
_ = json.Unmarshal(raw, &restored)
if err := rigidWorld.Restore(restored); err != nil {
	return err
}
_ = hashAtSave
```
