# Agent Note: Split navigation steering from movement

Status: implemented

## Problem

`NavAgent` computed its corridor-following velocity inside `stepMovement` and immediately accelerated toward it and moved, so an avoidance solver could not adjust the desired velocity without duplicating corridor repair, surface movement, link traversal and arrival logic (#1718). `SetVelocity` only overwrites the current velocity, which the next `Step` accelerates away from again.

## Decision

The 2D (`pkg/pathfind`) and 3D (`pkg/pathfind3d`) agents expose `DesiredVelocity()` and `StepWithVelocity(v, dt)`.

- `DesiredVelocity()` returns the velocity `Step` would steer toward: `MaxSpeed` toward the next funnel corner, slowed inside `ArrivalRadius` on the final segment (by 3D distance in `pathfind3d`, as `Step` does). It is zero when the agent is idle, unreachable, resting within `StoppingDistance` of its target, or traversing a link, because those states are not steered.
- `StepWithVelocity` runs the same pre-step and replan as `Step`, then the same movement routine with the desired velocity replaced by `v`. Both call one shared `stepMovementWith(dt, override)`, and the corridor velocity is computed by one shared `steerVelocity` helper, so the two paths cannot drift: link entry, link traversal, arrival checks, the `MaxAcceleration` integration and the `MaxSpeed` clamp all apply to external velocities. The per-step overshoot guard at the goal and at off-mesh link entries also caps the external velocity's magnitude. Non-finite velocities are treated as zero. In 3D `v.Y` is ignored; elevation follows the walked polygon.
- Feeding `DesiredVelocity()` back unchanged moves the agent as `Step` would (tests compare the two trajectories step by step in 2D and 3D).

## Alternatives considered

- Move directly by `v * dt` through `advancePosition` (the first version of this change): rejected because it skipped off-mesh link entry, active link traversal and the arrival transition, so an agent driven this way stalled at a link entry and never reported `NavAgentArrived`, and it ignored `MaxAcceleration`.
- Add an ORCA solver directly inside `NavAgent`: rejected because callers may use different avoidance policies and need to inspect or combine desired velocities.
- Expose mutable corridor internals: rejected because it would let avoidance code bypass corridor repair and mesh revision handling.

## Consequences

Avoidance integrations can change the final velocity while keeping navigation invariants. `StepWithVelocity` is for standalone agents; a `NavAgentGroup` still moves its agents in `group.Step`, so a crowd built on groups needs the crowd layer. The `NavCrowd` types, navmesh boundary extraction as ORCA obstacles, separation, the 64-agent and benchmark acceptance tests, and the example from #1718 remain separate work.
