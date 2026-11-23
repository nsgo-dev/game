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

The scope is intentionally focused: axis-aligned boxes, spheres, and capsules, exact overlap and raycast queries, translational rigid bodies, and a capsule character controller. Static triangle meshes (`TriMeshCollider`, added with `World.AddTriMesh`) and heightfields (`HeightfieldCollider`, added with `World.AddHeightfield`) collide with bodies and the character controller; articulated joints and rotational inertia tensors are outside this initial package.

## Coordinate Convention

`physics3d` uses a right-handed, Y-up coordinate system (`+X` right, `+Y` up, `+Z` toward the viewer). `NewWorld()` sets default gravity to `(0, -9.81, 0)`.

## Shapes, Overlaps, and Raycasts

Three primitives are wrapped by the tagged `Shape` value (`BoxShape`, `SphereShape`, `CapsuleShape`):

- `Box` — axis-aligned box defined by `Center` and `HalfExtents` (or constructed via `BoxFromMinMax(min, max)`).
- `Sphere` — `Center` and `Radius`.
- `Capsule` — line segment from `A` to `B` swept by `Radius` (or `VerticalCapsule(center, radius, height)`).

Overlap tests return a `Contact` whose unit `Normal` points from `B` toward `A` (the direction to translate `A` by `Depth` to separate the shapes):

- `TestBoxBox`, `TestSphereSphere`, `TestCapsuleCapsule`, `TestBoxSphere`, `TestBoxCapsule`, `TestSphereCapsule`, and `TestShapes(a, b)`.
- `world.QueryOverlap(shape)` returns all overlapping bodies in deterministic insertion order.

Raycasts test a `Ray{Origin, Dir, MaxDist}` against standalone shapes (`RaycastBox`, `RaycastSphere`, `RaycastCapsule`, `RaycastShape`) or cast a segment through a world (`world.Raycast(from, to)` and `world.RaycastFirst(from, to)`), returning `RaycastHit` entries sorted by ascending `T` in `[0, 1]` with world-space `Point`, `Distance`, and outward unit `Normal`.

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

## Character Controller

`CharacterController` moves a kinematic capsule (`Pos` at the feet, `Radius`, `Height`) through a `World` via `Move(world, wishMove, dt)`:

- Walks across flat ground and glides smoothly up and down slopes up to `MaxSlopeAngle` (default 45°), staying grounded on descents.
- Prevents climbing slopes steeper than `MaxSlopeAngle` and slides down them under gravity.
- Automatically steps up stairs up to `StepHeight` after verifying headroom clearance, forward progress, and a walkable landing tread.
- Stops cleanly and slides tangentially along vertical walls without penetration.

```go
cc := physics3d.NewCharacterController(physics3d.Vec3{Y: 0}, 0.3, 1.8)
loop := &game.Loop{
	Update: func(dt time.Duration) {
		res := cc.Move(world, physics3d.Vec3{X: 3.5}, dt)
		_ = res.Pos
	},
}
```
