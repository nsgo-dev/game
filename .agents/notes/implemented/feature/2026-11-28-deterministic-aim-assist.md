# Agent Note: Deterministic aim assist

Status: implemented

## Problem

The persisted `AutoAim` option had no shared implementation. Games had to write separate target selection and correction code for mouse, stick, touch, 2D, and 3D input, which made accessibility behavior inconsistent and difficult to replay deterministically.

## Decision

`pkg/input` provides `AimAssist.Evaluate2D` and `Evaluate3D`. Each fixed-step evaluation receives an origin, normalized direction, an ordered target snapshot, and an optional line-of-sight callback. It selects by priority, angular distance, distance, and input order, then applies strength-scaled friction, magnetism, and optional snap. Strength zero returns the original direction. The fps3d template and Starburst use the same evaluator on their fixed simulation steps.

## Alternatives considered

Keeping aim assist inside each game would minimize the shared API, but would duplicate target selection and produce different replay behavior. A rendering-time correction would feel responsive, but would make simulation and replay outcomes depend on frame timing.

## Consequences

Aim assist behavior is reusable and testable for both dimensions, and line-of-sight remains game-owned. Callers build the target slice in a deterministic order, and on a full tie the earlier target wins. The evaluator does not allocate; it filters targets by angle and by the current best candidate before calling the line-of-sight callback, so a raycast runs only for a target that could still be chosen. Starburst reuses one target buffer across steps.

