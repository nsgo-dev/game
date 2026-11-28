# Agent Note: Stabilize long rigid joint chains with substeps

Status: implemented

## Problem

Long 2D and 3D rigid joint chains accumulated solver drift under gravity and high mass ratios. Breakable 3D joints also reported the cumulative impulse from a substep batch as though it were a single instantaneous impulse, causing resting bridges to break at their normal support load (#1711).

## Decision

The rigid solvers use deterministic substeps, soft push-out, alternating joint order, and a final relax pass. Joint reaction tracking keeps the per-substep peak for break decisions and does not promote the cumulative batch impulse again at the end of the outer step. Reaction forces divide the selected impulse by the substep duration and batch count, preserving whole-step units while keeping torque-break behavior.

## Alternatives considered

- Raise solver iterations without substeps: rejected because the long-chain cost scales linearly with iterations and still leaves mass-ratio drift.
- Apply unconditional final positional cleanup: rejected because it injects support impulses into breakable bridges and changes reaction thresholds.
- Disable breakable-joint evaluation during substeps: rejected because it delays genuine failures and loses deterministic per-step events.

## Consequences

Long-chain, heavy-end, ragdoll, bridge, and torque-break regressions are covered by deterministic tests. Substep reaction accounting keeps break thresholds in whole-step units without treating the cumulative impulse as a second peak.
