# Agent Note: 2D and 3D Physics Gravity, Damping, and Buoyancy Areas

Status: implemented

## Problem

Both `packages/game/physics` (2D) and `packages/game/physics3d` (3D) previously applied a single uniform world gravity vector and each body's static linear/angular damping directly inside `Step`, with no per-body gravity scale, no spatial gravity or damping override volumes, and no buoyancy/fluid drag. Games implementing water, low-gravity zones, planetary point gravity, wind tunnels, or weightless pickups had to track overlaps and apply manual forces from trigger callbacks every frame (#819).

Adding area overrides and per-body gravity scale must keep zero-initialized `RigidBody`, `physics3d.Body`, `CharacterBody`, and `CharacterController` structs backward-compatible (defaulting gravity scale to `1` without requiring callers to update existing struct literals), avoid introducing any non-determinism, and preserve `game/physics`'s Stable API contract (no Experimental imports).

## Decision

### Per-body and per-character gravity scale (`physics/area.go`, `physics/rigid.go`, `physics/character.go`, `physics3d/area.go`, `physics3d/world.go`, `physics3d/character.go`)

- `RigidBody`, `CharacterBody`, `physics3d.Body`, and `physics3d.CharacterController` each expose `GravityScale float32`, `SetGravityScale(scale float32)`, and `EffectiveGravityScale() float32` backed by an unexported `gravityScaleSet bool`.
- Constructors (`NewRigidBox`, `NewRigidCircle`, `NewCharacterBody`, `NewDynamicBody`, `NewCharacterController`) initialize `GravityScale = 1` with `gravityScaleSet = true`, and `RigidWorld.AddBody` / `physics3d.World.AddBody` normalize unset zero values to `1` and mark `gravityScaleSet = true`. Zero-initialized struct literals therefore retain normal gravity (`1`), while `SetGravityScale(0)` (or mutating `GravityScale = 0` after construction or `AddBody`) produces exact zero gravity.

### Shared priority and override resolution (`physics/area.go`, `physics3d/area.go`)

- Both packages define `AreaSettings` with `Priority`, `AreaOverrideMode` (`AreaOverrideNone`, `AreaOverrideCombine`, `AreaOverrideReplace`, `AreaOverrideAdd`), directional gravity or radial `PointGravity` with `GravityFalloff` (`GravityFalloffNone`, `GravityFalloffLinear`, `GravityFalloffInverse`, `GravityFalloffInverseSquare`), linear/angular damping overrides, and fluid parameters (`FluidDensity`, `FluidLinearDrag`, `FluidAngularDrag`, fluid velocity).
- Active area triggers are collected once per step and stable-sorted by descending `Priority`, breaking equal-priority ties deterministically by slice insertion order.
- Gravity and damping resolution walks matched areas in priority order: `Add` accumulates and continues, `Combine` accumulates equal-priority `Combine`/`Add` areas and drops lower priorities and the world/body default, and `Replace` replaces equal- and lower-priority areas and defaults while preserving any higher-priority `Add` areas already accumulated.
- 2D hooks `AreaSettings` onto `Trigger.Area` (used by both `RigidWorld.Triggers` and `World.Triggers`); 3D hooks `AreaSettings` onto `Body.Area` for trigger bodies (`IsTrigger == true`) and wakes sleeping bodies when active areas are added, moved over them, or exited.

### Buoyancy and fluid drag from submerged bounds

- For overlapping fluid areas (`FluidDensity > 0`), submerged fraction is computed from the AABB intersection volume (3D) or area (2D) divided by the body's AABB volume/area, capped so cumulative submerged fraction across overlapping fluid volumes never exceeds `1`.
- Displaced fluid mass equals `FluidDensity * usedFraction * shapeVolume` (or `shapeArea` in 2D), yielding buoyancy acceleration `-effectiveGravity * (displacedMass * invMass)` scaled by the body's `GravityScale`. Equilibrium therefore sits directly at submerged fraction `(Mass / Volume) / FluidDensity` (or `(Mass / Area) / FluidDensity` in 2D).
- Fluid linear drag acts relative to the drag-weighted fluid flow velocity using unconditionally stable implicit Euler damping `v' = v_fluid + (v - v_fluid) / (1 + drag*dt)`, and fluid angular drag adds into the implicit angular damping step.

### Character controllers

- 2D `CharacterBody` adds an optional `World *World`; when nil, `Move` remains completely unchanged for manual gravity callers, and when non-nil, `Move` integrates area-resolved gravity (scaled by `GravityScale`), fluid drag, and overridden linear damping before movement.
- 3D `CharacterController.Move` resolves overlapping area triggers (honoring `IgnoreBodyID` and `Mask`), applies scaled area gravity, fluid drag, and linear damping, preserves horizontal velocity accumulation when no horizontal wish input is given under horizontal/point gravity, and passes effective `gravity.Y` into steep-slope sliding.

## Alternatives considered

### Separate `Area2D` / `Area3D` container types instead of extending triggers

Introducing dedicated `Area` structs and parallel world lists would isolate area bookkeeping from triggers, but would duplicate shape overlap, layer/mask filtering, and lifecycle management that 2D `Trigger` and 3D `Body{IsTrigger: true}` already provide. Embedding `Area AreaSettings` (with a zero-cost inactive zero value) onto existing triggers keeps one unified volume model and lets a single trigger emit enter/exit callbacks while applying water or gravity effects.

### Exact clipped polyhedron/sphere-cap submerged volume for buoyancy

Computing exact analytical submerged volume for arbitrarily rotated OBBs, spheres, and capsules against arbitrary trigger shapes would improve submerged-curve accuracy for tilted shapes, at the cost of significantly more geometry code and edge cases. Multiplying the body's exact shape volume/area by the submerged fraction of its axis-aligned bounds is exact for axis-aligned boxes, exact at 0%/50%/100% for spheres/circles/capsules, stays bounded in `[0, 1]`, and directly matches the bounds-based buoyancy design in #819.

### Representing default gravity scale with a sentinel or pointer (`*float32`)

A `*float32` field would distinguish unset from `0` without an unexported flag, but makes ergonomics worse for every game author (`b.GravityScale = ptr(0.5)`) and adds heap allocations. Keeping a plain `float32` field with an unexported `gravityScaleSet` flag normalized by constructors and `AddBody` gives clean direct field access while keeping zero-initialized struct literals backward-compatible.

## Consequences

- **Benefits**: 2D and 3D games get deterministic water buoyancy, fluid currents, planetary point gravity with falloff, low/anti-gravity zones, custom damping zones, and per-body gravity scaling across rigid bodies and character controllers with zero cgo and identical semantics in 2D and 3D.
- **Costs**: Buoyancy acts through the body's center of mass from its bounds overlap fraction rather than computing a shifted center of buoyancy or hydrostatic righting torque on tilted shapes; games needing self-righting boats can add a small angular spring or keep `LockRotation` enabled. In 2D, `CharacterBody` only integrates world/area gravity automatically when `CharacterBody.World` is explicitly assigned so existing manual-gravity callers are unaffected.
