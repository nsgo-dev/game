# Agent Note: Stabilize long rigid joint chains with substeps

Status: implemented

## Problem

Long 2D and 3D rigid joint chains accumulated solver drift under gravity and high mass ratios: on `origin/main` a 20-link chain hanging for 10 s stretched by about 50% and a chain released horizontally came apart (#1711). The 3D `World` did not substep at all, and the 2D `RigidWorld` substepped contacts but projected joints with a non-linear position pass after every substep.

## Decision

- The 3D `World.Step` runs `World.Substeps` (default `DefaultSubsteps` = 4) substeps of the existing pipeline, each with the full `SolverIterations`. Contact events, sleep and breakable-joint checks run once per outer step: contact pairs are collected over every substep, sleep is judged on the whole step's displacement, and joints break after the last substep.
- The 2D `RigidWorld` solves `RevoluteJoint` anchors that form a tree exactly in O(N) over a spanning forest (`pkg/physics/joint_tree.go`), solves other joints sequentially in alternating order, closes `RevoluteJoint` and `DistanceJoint` drift through push-out pseudo-velocities like contacts, and adds a relax pass. `JointSoftness` on `DistanceJoint` and `RevoluteJoint` gives a spring-damper alternative to a rigid joint.
- The non-linear joint position pass becomes `EnablePositionCleanup` in both worlds, off by default, and runs only over joints prepared this step.
- In a substepped 3D step a joint's reaction is the largest per-substep impulse divided by the substep length, so `ReactionForce`, `ReactionTorque` and the break thresholds stay in Newtons and N*m. The peak is cleared by `beginStepReaction` at the start of every `World.Step`, never by `resetStepReaction`, because `World.Step` prepares each joint twice per substep (once while waking islands, once before the solve) and a reset there would drop the peak of the substep before.
- The 3D sleep check in a substepped step compares positions with the start of the whole step and allows an end-of-step speed up to the sleep threshold plus `|Gravity| * dt`. A body held up by an impulse applied once per step, such as a `vehicle3d` car on its suspension, ends every step with a velocity of about `1.5 * |Gravity| * dt / Substeps` downward even though it does not move, and would otherwise never sleep.
- `World.Substeps`, `EnablePositionCleanup` and the reaction peaks are saved in 3D `WorldSnapshot` version 5; `EnablePositionCleanup` and joint `Softness` are saved in 2D `RigidWorldSnapshot` version 5. The 2D checksum hashes the two new settings only when they are set, so checksums of worlds that do not use them are unchanged.

## Alternatives considered

- Raise solver iterations without substeps: rejected because the long-chain cost scales linearly with iterations and 40 iterations still left 12% stretch in 2D.
- Keep the final positional cleanup on by default: rejected because it moves bodies outside the velocity solve, injecting support impulses into breakable bridges and changing reaction thresholds.
- Evaluate breakable joints after every substep: rejected because the reaction of a single substep is a quarter-length impulse, and comparing its sum across substeps with the threshold made resting bridges break at their normal load.
- Keep the reaction peak across steps once a world has substepped: this is what the first version did, and it made `ReactionForce` report the largest load the joint had ever seen instead of the last step's.
- Apply external impulses per substep in `vehicle3d` instead of changing the sleep check: rejected because `physics3d` has only an impulse API, so every caller that pushes bodies once per step would hit the same sawtooth.

## Consequences

Golden trajectories of `pkg/physics`, `pkg/physics3d`, `pkg/vehicle3d` and the netplay rollback soaks change with the solver. A 3D world now costs about `Substeps` times as much per step at the same `SolverIterations`; a game that needs the old cost sets `Substeps` to 1. Long-chain, heavy-end, ragdoll, bridge, torque-break, reaction-readout and snapshot round-trip tests cover the behavior. `JointSoftness` is not yet available on 3D joints.
