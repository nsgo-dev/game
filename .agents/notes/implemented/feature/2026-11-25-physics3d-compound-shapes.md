# Agent Note: physics3d Compound Shapes

Status: implemented

## Problem

A `physics3d.Body` held only a single primitive shape (#809). Multi-part rigid bodies — such as a table with four legs and a top, a vehicle chassis with a cabin, or a character body carrying an attached hurtbox — had to be approximated by one oversized primitive or stitched together with fixed joints, which adds solver instability and per-part body overhead. In addition, non-symmetric assemblies need an accurate local center of mass and anisotropic 3×3 inertia tensor so they tip, pivot, and rotate naturally without wobbling around the body origin, and queries and contact events must identify which child collider was struck.

## Decision

### Shape representation (`compound.go`, `shape.go`, `obb.go`)

- `ShapeKind` adds `ShapeCompound`, and `Shape` gains `Children []CompoundChild`. `CompoundShape(children...)` deep-copies child slices via `cloneShape` so caller mutations never alias stored shapes.
- Each `CompoundChild` carries a child `Shape` (any primitive including `ConvexHull` and `Cylinder`, or a nested `ShapeCompound`), local `Offset` and `Rot`, optional `Mass` or `Density`, `Trigger` flag, and optional `Layer`/`Mask` overrides (zero inherits the parent `Body.Layer`/`Body.Mask`).
- `Shape.Bounds()` computes the union AABB of all children. `Shape.Translated`, `Shape.Transformed`, and `boxFrame.localShape` transform every child cleanly into the target frame while flattening child local offsets/rotations on the transformed copy.

### Mass, center of mass, and parallel-axis inertia (`compound.go`, `world.go`)

- `SymMat3` represents a symmetric 3×3 matrix with FMA-free `MulVec`, `QuadForm`, `Rotated`, and float64 `Inverse` methods.
- `ComputeCompoundMassProperties(children, totalMass)` flattens non-trigger solid leaves (ignoring `Trigger` children), resolves per-leaf masses from explicit `Mass`, `Density * shapeVolume3D`, or volume-weighted distribution of `totalMass`, computes the combined local `CenterOfMass`, and accumulates each child's local inertia tensor (exact formulas for boxes, spheres, capsules, convex hulls via `hullGeom.inertia`, and cylinders) shifted via the parallel-axis theorem into `Body.InvInertiaTensor` and isotropic fallback `ScalarInvI`.
- `Body` gains `CenterOfMass Vec3`, `InvInertiaTensor SymMat3`, `WorldCenterOfMass()`, `RecomputeMassProperties()`, and `ApplyImpulseAtPoint(impulse, worldPoint)`. Single-primitive bodies leave `CenterOfMass` and `InvInertiaTensor` zero and keep the existing scalar inertia path unchanged.
- In `World.Step`, bodies with non-zero `CenterOfMass` integrate translation and rotation about `WorldCenterOfMass()` and then recover `Pos`, conserving center-of-mass momentum in free rotation. Contact solver arms (`rA`, `rB`), effective mass (`contactEffectiveMassBodies`), and angular impulses (`applyContactImpulse`) use `WorldCenterOfMass()` and the world-rotated `InvInertiaTensor` when present.

### Per-child contacts, warm-starting, queries, and subsystems (`world.go`, `layers_triggers.go`, `raycast.go`, `ccd.go`, `area.go`, `character.go`, `cover.go`, `xpbd.go`, `debugdraw.go`)

- Warm-start cache (`w.cache`) and active contact tracking (`w.activeContacts`) key on `contactKey{a, b, childA, childB}` instead of `pairKey{a, b}`, so each contacting child pair (such as all four legs of a table) maintains independent warm-start impulses and deterministic `ContactEnter`/`ContactStay`/`ContactExit` events.
- `ContactEvent` gains `ChildIndexA`/`ChildIndexB`, and `RaycastHit` and `OverlapHit` gain `ChildIndex`, reporting the zero-based child index for `ShapeCompound` and `-1` for non-compound shapes and static colliders.
- Compound children that are `ShapeConvexHull` or `ShapeCylinder` still receive up to four manifold contact points via `shapeManifold` and `shapeTriangleContacts`, matching [physics3d convex hull and cylinder shapes](./2026-11-25-physics3d-convex-hull-and-cylinder-shapes.md).
- Trigger children (`CompoundChild.Trigger`) emit `OnTrigger` events without impulse/positional resolution, and are skipped by CCD sweeps (`ccd.go`), area buoyancy/overlap checks (`area.go`), `CharacterController` sweeps/overlaps/raycasts (`character.go`), static cover generation (`cover.go`), and XPBD soft-body collisions (`xpbd.go`).
- `DrawShape` recurses into every child in deterministic index order so each child renders in its primitive's color, and `DrawWorld` draws contacts per colliding child pair.

### Snapshots and cross-architecture determinism (`snapshot.go`)

- `BodySnapshot` persists `CenterOfMass`, `InvInertiaTensor`, and a deep-cloned `Shape`; `CachedImpulseSnapshot`, `ActiveContactSnapshot`, and `ContactEventSnapshot` persist child indices; `hashShape3D` hashes `ShapeCompound` recursively.
- To preserve the existing cross-architecture golden hash (`TestGoldenTrajectory3D`) for worlds without compound bodies (see [physics save determinism](./2026-11-25-physics-save-determinism.md)), `World.Checksum()` hashes `CenterOfMass`, `InvInertiaTensor`, and child indices only when non-zero / non-negative (`>= 0`).
- Every multiply-add in `compound.go` wraps products in explicit `float32(...)` / `float64(...)` conversions; an arm64 `go tool objdump` audit verifies zero `FMADD`/`FMSUB`/`FNMADD`/`FNMSUB` instructions in `compound.go` and all stepped physics3d files.

## Alternatives considered

### Separate child `Body` instances joined by `FixedJoint`

Representing compound objects as multiple bodies connected by `FixedJoint` requires no new shape kind, and existing joint serialization works as-is. Rejected because iterative fixed-joint chains flex under heavy loads or high mass ratios, multiply broadphase and solver cost, break single-body gameplay handles and `Body.Pos`/`Rot`, and cannot guarantee rigid relative child poses.

### Replacing scalar `InvInertia` on every body with `SymMat3`

Switching boxes, capsules, hulls, and cylinders to full 3×3 inertia tensors would unify the solver path. Rejected because changing inertia behavior on existing single-primitive bodies alters trajectories that existing tests (including `TestGoldenTrajectory3D`) and games depend on; keeping `InvInertiaTensor` zero for single primitives isolates the behavioral change to compound bodies.

### Auto-recentering `Body.Pos` to the compound's center of mass

Shifting child offsets automatically so `Body.Pos` always coincides with the center of mass avoids storing `Body.CenterOfMass`. Rejected because game code places `Body.Pos` at an authored pivot (such as a table's floor contact or a character's feet) and expects `Body.Pos` and child `Offset` values to remain in the author's coordinate frame across `AddBody`, rendering, and `Save`/`Restore`.

## Consequences

- **Benefits**: Multi-part rigid bodies (tables, vehicles, characters with hurtboxes) simulate stably as one body with accurate parallel-axis inertia, per-child layers/triggers, exact child-index reporting on contacts/raycasts/overlaps, full debug wireframes, and bit-identical JSON save/restore across amd64 and arm64.
- **Costs**: `Shape` grows by one slice header (`Children []CompoundChild`), `Body` grows by `CenterOfMass` (12 bytes) and `InvInertiaTensor` (24 bytes), and `contactKey` adds two ints to `w.cache`/`w.activeContacts` keys. Non-compound bodies take fast paths in `World.Step` and `World.Checksum`, keeping single-primitive overhead minimal.
- **Limitation**: Built-in `Joint3D` constraints still read scalar `InvInertia` and `Body.Pos` rather than `InvInertiaTensor` and `WorldCenterOfMass`; compound bodies work with joints via their isotropic `ScalarInvI` fallback, and upgrading joint constraints to full tensor/COM math can be done independently later.
