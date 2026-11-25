# physics

`game/physics` provides 2D collision shapes (`Box` and `Circle`), exact overlap contacts, anti-tunneling tile resolution with `MoveAndSlide`, and deterministic spatial queries and raycasts on `World`.

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
- **Custom `TileGrid`**: implement `TileSize() (float32, float32)`, `GridSize() (int, int)`, and `TileFlagsAt(col, row int) TileFlags` to plug a [`tilemap.Tilemap`](tilemap.md) or any custom map directly into `MoveAndSlide` and `World`.

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

`World.Raycast` returns all hits along the segment sorted by ascending normalized distance `T` in `[0, 1]` (bodies before tiles on ties), while `RaycastFirst` returns the closest hit. Remove bodies cleanly with `world.Remove(b)`.

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
