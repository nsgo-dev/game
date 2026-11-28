# Agent Note: Split navigation steering from movement

Status: implemented

## Problem

`NavAgent` computed its corridor-following velocity and immediately moved, so an avoidance solver could not adjust the desired velocity without duplicating corridor repair and surface movement logic (#1718).

## Decision

The 2D and 3D agents expose `DesiredVelocity()` and `StepWithVelocity(v, dt)`. The first computes the current funnel steering result, while the second runs the normal pre-step corridor checks and advances through the existing surface movement path using the externally selected, max-speed-clamped velocity.

## Alternatives considered

- Add an ORCA solver directly inside `NavAgent`: rejected because callers may use different avoidance policies and need to inspect or combine desired velocities.
- Expose mutable corridor internals: rejected because it would let avoidance code bypass corridor repair and mesh revision handling.

## Consequences

Avoidance integrations can preserve navigation invariants while changing the final velocity. The full multi-agent crowd ownership and mesh-boundary extraction remain separate work for the tiled-crowd layer.
