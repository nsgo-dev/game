# Agent Note: Substepped Soft-Push-Out Solver and Pinched Mass-Ratio Block for 2D Rigid Bodies

Status: implemented

## Problem

At the default solver settings (`SolverIterations = 10`), `RigidWorld` failed three standard 2D stacking cases (#1710):

1. A 20-box aligned column toppled under default gravity (`600` or `980 px/s²`).
2. A 20-base pyramid (210 boxes) failed to settle and sleep within 5 s.
3. A heavy box resting on a light box at 100:1 or 1000:1 mass ratio sank through the light box.

Two root causes combined in the single-step solver:

- **Translation-only post-step positional correction (`correctPositions` / `correctTilePositions`)** re-ran broadphase and narrowphase four times per step and shifted centers linearly with no angular response at the contact points, which neither restored tilt in tall stacks nor balanced off-center overlap, while dominating the per-step cost on large piles (`66.2 ms/step` on 2000 boxes).
- **Single-step sequential Gauss-Seidel** lacked substeps and relaxation for tall stacks, accumulated one-sided `float32` angular rounding torque between the two points of a flat contact, and had spectral radius $\rho = m_\text{heavy} / (m_\text{heavy} + m_\text{light}) = 0.999$ on a `static — 1 — 1000` sandwich, making single-manifold iterations unable to stop the heavy body from crushing the light one.

## Decision

Replace the single-step solve + post-step `correctPositions` loop in `RigidWorld.Step` (`pkg/physics/rigid.go`, `pkg/physics/rigid_island.go`, `pkg/physics/rigid_tiles.go`, `pkg/physics/joints.go`, `pkg/physics/snapshot.go`) with a **substepped sequential impulse solver with soft contact push-out into pseudo-velocities, bias-free relaxation, and an exact $O(M)$ 2-manifold coupled block pass for pinched high-mass-ratio contacts**.

### Per-step pipeline

1. **Single collision detection**: broadphase + narrowphase runs once per step (`detectContacts` + `detectTileContacts`). Adjacent coplanar tile faces on the same row (horizontal) or column (vertical) merge into a single 2-point manifold (`mergeCoplanarTileManifold` in `pkg/physics/rigid_tiles.go`) while keeping a zero-point sentinel per secondary tile so `storeContacts` / `tileContactChanged` still wakes a sleeping body if any supporting tile is removed.
2. **Substeps (`RigidWorld.Substeps`, defaulting to `DefaultSubsteps = 4`)**: divide `dt` into `subH = dt / substeps`. Track each non-static body's incremental delta `(dx, dy, sinDA, cosDA)` from its start pose so contact lever arms and separations update across substeps without re-running collision detection. Circles keep `(sinDA = 0, cosDA = 1)` because they are rotationally symmetric and rotating lever arms would catch on tile/chain seams.
3. **Per-substep velocity solve (zero penetration bias)**:
   - Integrate gravity, forces, torques and damping over `subH`.
   - Warm-start `sub == 0` from `w.contacts` (scaled by `subH / lastDt`) and refresh later substeps from the previous substep's impulses (`sub > 0` only so `sub == 0` impact spikes do not echo).
   - Run `velIters = ceil(iters / substeps)` sweeps in alternating forward/reverse order (`(sub+iter)&1`) using a 2×2 block LCP (`solve2x2LCP`) for 2-point contacts and `applyImpulsePairRaw` to sum both contact points' linear and angular deltas in `float64` before casting to `float32` once (eliminating one-sided rounding torque on flat columns).
   - Speculative points (`sep > 0`) allow closing by `sep / subH`; overlapping points carry zero velocity bias.
4. **Coupled 2-manifold block for pinched high-mass-ratio bodies (`findPinchPairs`, `solvePinchPairVelocity`, `solvePinchPairPushOut`)**:
   - Once per step before substeps, identify every awake dynamic body `b` pinched between two opposing manifolds (`dot(n0, n1) <= -0.5`) where at least one partner is dynamic and `invM0 + invM1 < 0.5 * b.InvMass` (mass ratio > 2:1 on a pinched body).
   - After each single-manifold velocity and push-out sweep, solve the exact 2×2 Schur-complement normal LCP across the two manifolds together. On 1:1 stacks and piles `len(pinchPairs) == 0`, adding zero cost.
5. **Soft contact push-out into pseudo-velocities (`solveManifoldPushOut`)**:
   - Resolve overlap deeper than `contactSlop = 0.01` px at contact points into per-body linear and angular pseudo-velocities `(pvx, pvy, pav)` using implicit-Euler spring softness (`makeContactSoftness(0.5/subH, 2.0, subH)`) capped at `contactMaxPushSpeed = 300` px/s, running 2 alternating sweeps per substep.
   - Integrate positions using `(v + pv) * subH` and discard `pv` so contact overlap resolves with proper angular correction and zero artificial bounce in body velocities.
6. **Joints, relaxation, restitution and sleep**:
   - Joint reaction accumulators reset once per full step (`stepReactionJoint.resetStepReaction(h)`); each substep calls `preStep(subH)`, `solveVelocity()`, and `solvePosition()`.
   - After position integration, each substep runs 1 bias-free relaxation pass in alternating order (`(sub+iter)&1`) so velocities stay consistent with the updated poses.
   - Restitution is applied once after the final substep (`applyManifoldRestitution`) above `restitutionThresh`.
   - `updateSleep` checks both velocity and substep displacement speed (`dx/h, dy/h, pav`) plus active push-out (`pvx² + pvy² <= 0.04`), and both `updateSleep` and `wakeIslands` union `w.touching` alongside `w.contacts` and joints so a resting box within `contactEndGap` (`0.25` px) remains linked to its island.
   - `chainSegmentManifold` in `pkg/physics/narrow.go` falls back to the hull's centroid side relative to the segment when `|dot(mn, nSeg)| < 0.5` so `1e-5 rad` tilt noise on a flat chain seam never flips a contact to the underside of the chain.
7. **Snapshots**: `SnapshotVersion` bumps from `3` to `4`; `RigidWorldSnapshot` stores `Substeps` (`json:"substeps,omitempty"`), `Checksum` hashes `w.Substeps`, and `Restore` accepts versions 1 through 4. Every new multiply-add is wrapped in explicit `float64(...)` to keep arm64 zero-FMA (`TestNoFusedMultiplyAddOnArm64`).

## Alternatives considered

### Raising global `SolverIterations` (e.g. 40+ iterations in one step)

Increasing iterations is a one-line change and preserves the existing single-step structure. It is rejected because: (1) a single-step solver still re-runs translation-only `correctPositions` with four broadphase+narrowphase passes, leaving tall columns prone to tipping and 2000-box piles slow; (2) on a `1000:1` sandwich, Gauss-Seidel has spectral radius `0.999`, so even 40 iterations only reduce error by `0.999^40 = 0.96` and the heavy box still sinks through the light box.

### Shock propagation (freezing lower layers) or artificial mass clamping

Shock propagation makes tall stacks and extreme mass ratios trivially rigid in one bottom-up pass by temporarily treating already-solved lower bodies as infinite mass. It is rejected because it violates conservation of momentum, breaks symmetry on lateral impacts and bridges, causes noticeable artifacts when a bottom box is pulled out from under a stack, and fails #1710's non-goal of avoiding mass-ratio hacks.

### Pure Box2D v3 Soft Step (bias in real velocity without split push-out)

Box2D v3's Soft Step integrates a damped spring bias directly into body velocities and removes bounce with post-integration relaxation. It is clean and requires no pseudo-velocity state. However, in NSGo's pixel-unit scale (`g = 600..980 px/s²` on 20 px boxes, 3×–5× higher `g / box_size` than Box2D's `9.8 m/s²` on 1 m boxes), Hooke's law equilibrium sag `F / (m_eff * ω²)` at `30 Hz` caused `8.4–13.7 px` compression on a 20-box column and `13.4 px` on a 1000:1 pair (far outside the `< 1 px` requirement), while raising contact frequency to `120–960 Hz` shrank `impulseScale` below `0.002` and triggered warm-start integral windup and launching. Splitting the same implicit-Euler soft push-out into per-substep pseudo-velocities preserves rotational push-out and damping while eliminating Hooke's-law sag and velocity bounce.

## Consequences

- **Benefits**:
  - 20-box aligned columns stand 20 s with top drift `< 1 px` at both `g = 600` and `g = 980` (sleep on and off).
  - 20-base pyramids (210 boxes) settle and sleep within 5 s (and hold stably with sleep disabled).
  - 100:1 and 1000:1 heavy-on-light pairs settle with `< 1 px` overlap (sleep on and off) with zero mass hacking.
  - Eliminating the four post-step broadphase/narrowphase passes made large piles dramatically faster (Apple Silicon arm64, `go tool nsgo test -bench`):
    - `BenchmarkRigidWorld500BodiesPacked`: **26.45 ms/op → 1.62 ms/op** (16.4× faster, `0 B/op`, `0 allocs/op`).
    - `BenchmarkRigidWorld2000Boxes`: **66.20 ms/op → 2.49 ms/op** (26.6× faster, `0 B/op`, `0 allocs/op`).
- **Costs**:
  - Rigid-body trajectories change, updating golden checksums in `TestGoldenTrajectory2D`, `TestGoldenTrajectorySoftWorld2D`, `TestRollbackPhysics2DSoak` and two example outputs (`ExampleRigidWorld_tiles` improves from `320` to `310 px/s`, closer to exact `sqrt(2*600*80) = 309.84 px/s`; `ExampleRigidBody_kinematic` shifts from `159.3` to `159.2`).
  - `SnapshotVersion` bumps from `3` to `4` (older snapshots v1–v3 remain readable).
  - The pinched-pair block handles a body squeezed between two opposing contacts; a 3-body chain of nested 1000:1 ratios (e.g. `static — 1 — 1 — 1000`) would still need a multi-body direct solver if ever required.
- **Testing**: `pkg/physics/rigid_stacking_test.go` tests 20-box columns (`g = 600, 980`, sleep on/off), 20-base pyramids (sleep on/off), 100:1 and 1000:1 heavy-on-light pairs (sleep on/off), off-center angular push-out, and `Substeps` snapshot round-trip. `pkg/physics/rigid_perf_test.go` adds `BenchmarkRigidWorld2000Boxes`. `TestNoFusedMultiplyAddOnArm64`, `TestRigidWorldSaveRestoreBitIdentical`, `TestSnapshotValidation`, and all existing physics, netplay, scene, project, and repocheck tests pass.

See [sleeping, broadphase, warm starting and CCD](../feature/2026-11-25-physics-rigid-sleep-broadphase-warm-start-ccd.md) for the prior solver design this updates and [snapshots and determinism](../feature/2026-11-25-physics-save-determinism.md) for the FMA and snapshot contract.
