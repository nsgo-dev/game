# Agent Note: World.MoveAndSlide against bodies, shapes, tiles and static rigid bodies

Status: implemented

## Problem

`physics.MoveAndSlide` and `CharacterBody.Move` previously resolved only against a `TileGrid`, caller-supplied `Slope` segments, and `MovingPlatform` lists (#806). Any collider placed as a `Body` in `World.Bodies` (a closed door, pushable block, tilted polygon wall, chain outline, or one-way ledge) or as a static `RigidBody` in `RigidWorld` did not stop a moving character, and top-down games whose walls come from object layers had no shape-sweeping movement at all.

## Decision

`World` gains `MoveAndSlide(body, dt)` and `MoveAndSlideWith(body, dt, opts)` in `packages/game/physics/slide.go`, `Body` gains `OneWay bool`, `World` gains optional `RigidWorld *RigidWorld`, `TileLayer`, and `TileMask`, and `CharacterBody` and `platformer.Controller` integrate with `World` colliders while keeping slopes and moving platforms intact.

- **Exact 2D Minkowski-difference sweep (`slide.go`)**: sweeping any area-having convex shape (`Box`, `Circle`, `Polygon`, or `Capsule`) along `(dx, dy)` against any static shape (`Box`, `Circle`, `Polygon`, `Capsule`, `Segment`, or per-segment of a `Chain`, building on [Convex Polygon, Capsule, Segment and Chain Shapes in physics](2026-11-25-physics-convex-capsule-segment-chain.md)) forms the rounded Minkowski difference `B - A = (coreB + (-coreA)) + disk(rA + rB)` on the stack (at most `2 * MaxPolygonVertices` = 16 vertices, zero heap allocation) by rotating-calipers edge merging ordered in `[0, 2*pi)`, then casts the origin ray `(0, 0) -> (dx, dy)` against it in closed form. Continuous TOI prevents high-speed tunneling even against zero-thickness segments and chains.
- **Pre-sweep depenetration, bounded slide, and 2D crease handling**: before sliding, up to 4 depenetration iterations resolve existing overlap against solid (non-one-way) colliders so `float32` quantization from prior frames never traps a mover against angled surfaces. The slide loop runs up to `MaxSlides` (default `DefaultMaxSlides` = 4) iterations in `float64` working coordinates, projects remaining displacement and velocity onto each contact tangent, and halts when a new slide tangent points back into an earlier contact plane from the same step (preventing corner oscillation).
- **Ghost-collision suppression**: solid tile hits clamp normal components pointing into adjacent `TileSolid` neighbors so boxes, circles, and capsules glide across flat tile seams without snagging; chain segment sweeps reuse the interior-vertex normal cone test from `chainSegmentManifold`.
- **One-way bodies and tiles**: `Body.OneWay` (and `TileOneWay`) blocks only when `!body.OneWayDrop`, the contact normal lies within `FloorMaxAngle` of `Up`, and the mover starts outside (or within `slideOneWayEps` = `1e-3` of the top surface). Depenetration skips one-way colliders so jumping from below or releasing `OneWayDrop` mid-drop never snaps the body upward.
- **Configurable motion mode and surface state**: `MoveOptions` selects `MotionGrounded` (default `Up = (0, -1)`, `FloorMaxAngle = DefaultFloorMaxAngle` = 45°) or `MotionFloating` (top-down, every contact classified as a wall), returning ordered `SlideCollision` records (`Body`, `RigidBody`, `Tile`, `Normal`, `Position`, `Remainder`) plus `OnFloor`, `OnWall`, `OnCeiling`, `OnLeftWall`, `OnRightWall`, and surface normals.
- **CharacterBody and platformer integration**: `CharacterBody.Move` uses `c.World` colliders whenever `c.World` has bodies or static rigid bodies (keeping the original code path untouched when none are present), and `CharacterBody.MoveWorld` / `platformer.Controller.StepWorld` accept a `*physics.World` directly without double-applying `World.GravityY` when `CharacterBody.World` is nil. `MovingPlatform` treats `p.OneWay || p.Body.OneWay` as one-way.
- **Snapshots and cross-architecture determinism**: following [Physics world save/restore snapshots and cross-architecture determinism](2026-11-25-physics-save-determinism.md), `BodySnapshot` persists `OneWay`, `hashBody` hashes `OneWay` only when `true` so existing worlds without one-way bodies preserve their golden `Checksum()` digests, and every multiply-add and transcendental in `slide.go` uses explicit `float64(...)` conversions and `internal/detmath` so arm64 emits zero `FMADD`/`FMSUB`/`FNMADD`/`FNMSUB` instructions.

## Alternatives considered

### Discrete substepping with `TestShapes` push-out

Advancing in small fixed substeps and pushing out via `TestShapes` would reuse the existing overlap narrowphase without writing a Minkowski sweep. It is rejected because discrete substeps still tunnel through zero-thickness `Segment` and `Chain` colliders at high speeds, step-size selection introduces velocity-dependent drift, and repeated push-out along angled walls jitters when `float32` positions alternate across the surface boundary.

### Iterative GJK raycast / conservative advancement

Iterative conservative advancement works for arbitrary implicit convex shapes without constructing the Minkowski difference explicitly. It is rejected because 2D hulls in `physics` have at most 8 vertices, so constructing the exact 2D Minkowski difference on the stack takes O(nA + nB) <= 16 operations and yields a closed-form TOI and exact face normal with no iteration tolerance tuning or cross-architecture convergence sensitivity.

### Always route `CharacterBody.Move` through `World.MoveAndSlide` even without `World` colliders

Unifying tile-only `CharacterBody.Move` onto `World.MoveAndSlide` would remove duplicate tile resolution code. It is rejected because `CharacterBody.Move` has established bit-exact tile/slope/platform behavior and tests (including exact `Tiles` and `Area` trajectories); keeping the tile-only path intact when `World` has no body/rigid colliders guarantees zero regressions while letting worlds with bodies, doors, and angled shapes use the full sweep.

## Consequences

- **Benefits**: characters and top-down movers collide seamlessly with doors, blocks, arbitrary convex/segment/chain bodies, `Body.OneWay` platforms, solid/one-way tiles, and static `RigidBody` colliders, sliding smoothly along non-axis-aligned walls without sticking, jitter, or tunneling, with bit-identical cross-architecture determinism.
- **Costs**: `World.MoveAndSlide` sweeps against all overlapping bodies and static rigid bodies in slice order (broadphase AABB culled, no spatial hash yet), and `CharacterBody` keeps a combined path when `World` bodies are mixed with legacy `Slope` segments and `MovingPlatform` lists.
- **Verification**: `packages/game/physics/slide_test.go` and `packages/game/platformer/controller_test.go` test closed-door blocking and removal, 30-degree wall sliding across 12 wall/mover shape combinations (`Polygon`, `Segment`, `Chain`, rotated static `RigidBody` x `Box`, `Circle`, `Capsule`), `Body.OneWay` landing/drop-through/snapshot round-trip, configurable `Up` and `FloorMaxAngle`, `Layer`/`Mask` filtering, tile seam smoothness, anti-tunneling, corner creases, `CharacterBody.MoveWorld`, `platformer.Controller.StepWorld`, and bit-identical determinism; `GOARCH=arm64 go build -gcflags=-S ./physics` confirms zero FMA instructions in the new and modified files.
