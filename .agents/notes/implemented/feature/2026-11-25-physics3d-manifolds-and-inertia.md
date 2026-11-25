# Agent Note: physics3d Contact Manifolds and Inertia Tensors

Status: implemented

## Problem

Single-primitive bodies in `physics3d` previously used a single isotropic scalar `InvInertia`, built-in joints and `game/vehicle` ignored anisotropic inertia tensors, and box-box / capsule-on-face contacts resolved at a single contact point (#810). As a result:

- Elongated bodies such as planks or car chassis rotated with identical resistance around every axis, vehicle braking pitch did not match the chassis `Config` mass distribution, and free-spinning asymmetric bodies could not exhibit Euler's intermediate-axis (Dzhanibekov) instability.
- Flat box stacks and horizontal capsules rested on a single contact point per pair, causing rocking and relying on unconditional shock propagation every step to hold stacks upright.
- Callers had no ergonomic per-body override methods for principal moments of inertia, full inertia tensors, or custom centers of mass that survived snapshot `Save`/`Restore` and `RecomputeMassProperties`.

## Decision

### Body-space inertia tensors, world-space inverse, and gyroscopic torque (`inertia.go`, `world.go`, `joints.go`, `joints_6dof.go`, `vehicle/vehicle.go`)

- `AddBody` computes an exact body-space `InvInertiaTensor SymMat3` for every dynamic `Body` (exact diagonal tensors for axis-aligned boxes, spheres, capsules, and cylinders; parallel-axis-shifted symmetric tensors for offset, rotated, convex-hull, and compound shapes), while still keeping the legacy `InvInertia` scalar populated for backward compatibility with callers and tests inspecting `Body.InvInertia`.
- `Body` gains `SetInertia(Vec3)`, `SetInertiaTensor(SymMat3)`, `SetCenterOfMass(Vec3)`, and `WorldInvInertiaTensor() SymMat3`, plus private override flags (`customInertia`, `customCenterOfMass`) so `AddBody` preserves explicit overrides (including `SetCenterOfMass(Vec3{})` on a compound body) and `RecomputeMassProperties()` resets them cleanly. `DiagSymMat3` and `(SymMat3).Solve` are exported on `SymMat3`.
- Free rotational integration runs an FMA-free float64 implicit midpoint Newton solver (3 iterations, with implicit Euler fallback) for Euler's equation $I\dot{\omega} + \omega \times (I\omega) = 0$ on non-isotropic dynamic bodies. Implicit midpoint conserves both rotational kinetic energy and angular momentum magnitude, producing the Dzhanibekov flip on a spinning plank while leaving isotropic cubes untouched.
- All seven built-in `Joint3D` constraints (`DistanceJoint`, `BallJoint`, `HingeJoint`, `SliderJoint`, `FixedJoint`, `SwingTwistJoint`, `SixDOFJoint`) now use `activeWorldInvInertiaMatrix`, `jointAnchorOffset` about `WorldCenterOfMass()`, 3×3 skew-symmetric effective mass matrices (`pointConstraintMassMatrix`), and 2×2 perpendicular angular locks (`solvePerpAngularLockVelocity`), superseding the scalar-inertia joint limitation noted in [physics3d compound shapes](./2026-11-25-physics3d-compound-shapes.md).
- `game/vehicle` computes suspension/tire lever arms from `Body.WorldCenterOfMass()` and effective mass / angular impulses from `Body.WorldInvInertiaTensor()` and `ApplyImpulseAtPoint`, so vehicle pitch and roll under braking and cornering match `Config`'s `Mass`, `ChassisHalfExtents`, and `ChassisOffset`.

### Multi-point contact manifolds and symmetric block LCP solver (`manifold.go`, `world.go`)

- `buildShapeManifold` and `buildTriangleManifold` generate persistent multi-point manifolds:
  - Box-box face contacts clip the incident face against the reference face's four side planes in float64 Sutherland-Hodgman order, assigning deterministic geometric `featureID`s and per-point depths, then reducing to at most 4 points by maximizing area while keeping the deepest point (`reduceManifoldCandidates`).
  - Convex hull and cylinder face contacts clip features into the reference prism and reduce to up to 4 points with per-point depths; non-face contacts (`cosN < 0.85` or edge-edge) stay single-point so reference planes never disagree with the GJK/EPA separating normal.
  - Capsule-on-face contacts (against boxes, convex hulls, cylinder caps, and triangles) clip the capsule centerline segment to the reference face prism (`clipSegmentToPrism`) to produce up to 2 contact points, collapsing to 1 when standing upright on an end cap.
- `w.cache` stores up to 4 `cachedPoint` entries per `contactKey`, matched across steps by `featureID` first and local-space distance squared (`warmStartMatchTolSq`) second.
- `World.Step` switches to velocity-first symplectic Euler (integrate velocities and gyroscopic torque -> detect manifolds -> warm-start -> iterative velocity solver -> scoped shock velocity -> integrate positions -> CCD -> planar NGS position correction -> residual depth and scoped shock position -> sleep & events), eliminating per-step gravity sag on resting stacks.
- Multi-point (2–4 point) manifolds solve normal impulses simultaneously in FMA-free float64 using a regularized active-set Cholesky LCP (`solveBlockLCP`), followed by per-point 3D Coulomb friction and block-solved planar NGS positional/angular correction. Deactivating or activating violated constraints symmetrically removes corner-order tipping torque on flat resting, separating, and partially lifting faces.

### Scoped shock propagation (`world.go`)

- `computeShockMask` walks vertical contacts top-down and bottom-up to mark only support columns containing a high mass ratio (`upper.Mass > 4 * lower.Mass`) or more than 12 stacked dynamic bodies (`shockStackThreshold = 12`).
- Because column eligibility depends only on contact topology and body masses (never on fluctuating penetration depth or velocity thresholds), shock propagation introduces zero on/off hysteresis on extreme stacks (`heavy_on_light`, `heavy_on_tall_stack`, `tall_uniform_stack`) while doing zero work on ordinary stacks (verified bit-identical to `disableShock = true` on a 10-box tower over 60 simulated seconds).

### Snapshots and cross-architecture determinism (`snapshot.go`, `snapshot_test.go`, `vehicle/determinism_test.go`, `net/rollback_physics_test.go`)

- `BodySnapshot` persists `CustomCenterOfMass` and `CustomInertia`; `CachedImpulseSnapshot` persists `Points []CachedPointSnapshot` (with fallback decoding of legacy single-point snapshots) and hashes all cached manifold points in `World.Checksum()`.
- All new float32/float64 multiply-add expressions wrap products in explicit conversions to prevent arm64 FMA contraction. Because accurate anisotropic inertia tensors and multi-point manifolds intentionally change dynamic trajectories, golden trajectory hashes in `physics3d`, `vehicle`, and `net` rollback tests are updated to the new cross-architecture values.

## Alternatives considered

### Explicit Euler integration for gyroscopic torque

Computing $\Delta\omega = -I^{-1}(\omega \times (I\omega))\Delta t$ explicitly is simpler than implicit midpoint Newton iteration. Rejected because explicit gyroscopic updates inject rotational energy on fast-spinning slender bodies and can diverge at 60 Hz; implicit midpoint is unconditionally energy- and momentum-preserving in 3 fixed Newton iterations.

### Per-point sequential PGS normal solving with alternating sweep order

Solving a 4-point box manifold one corner at a time (reversing order on odd iterations) avoids building a 4×4 matrix. Rejected because on flat box stacks, applying corner 0's impulse before corners 1–3 tilts the box mid-iteration, triggering lateral friction kicks that topple tall stacks; a 4×4 active-set Cholesky LCP on stack-allocated arrays is tiny, symmetric, and exact.

### Depth- or velocity-thresholded shock propagation

Engaging shock propagation dynamically whenever a contact's depth exceeds 3 mm or leftover closing speed exceeds 0.05 m/s avoids tracking stack topology. Rejected because pushing a stack back to 1 mm drops it below the trigger threshold on the next frame, causing an endless on/off sawtooth oscillation that prevents tall or heavy-on-light stacks from sleeping. Topology-based column marking has zero hysteresis and leaves normal 10-box stacks completely untouched.

### Removing shock propagation completely

With multi-point manifolds and warm starting, a 10-box uniform tower settles cleanly without shock propagation. Completely removing shock propagation was rejected because 100:1 mass-ratio stacks (`heavy_on_light`, `heavy_on_tall_stack`) and 20-box stacks still exceed 5 mm residual penetration under 8 PGS iterations; scoping shock propagation to those extreme columns keeps existing residual-penetration guarantees without affecting normal gameplay stacks.

## Consequences

- **Benefits**: Realistic anisotropic rotational dynamics (including Dzhanibekov flips and accurate vehicle braking pitch), rock-solid multi-point face and capsule resting contacts, 10-box uniform towers that stand and sleep for 60 s with zero drift and zero shock intervention, full inertia/COM overrides on `Body`, tensor-aware `Joint3D` constraints, and bit-identical cross-architecture determinism and snapshots.
- **Costs**: `World.Step` adds a small 4×4 float64 Cholesky solve per multi-point manifold and a 3-iteration 3×3 gyroscopic solve per spinning non-isotropic body; `WorldSnapshot` stores up to 4 cached contact points per active pair instead of 1.
- **Compatibility**: Golden checksums for 3D physics trajectories (`physics3d`, `vehicle`, `net`) change due to the new multi-point manifold and inertia tensor dynamics; legacy v1 single-point `WorldSnapshot` payloads continue to restore cleanly.
