# Agent Note: Rigid broadphase regression boundary

Status: implemented

## Problem

The original 2D rigid solver repeatedly rebuilt candidate pairs and reran narrow phase during positional iterations. Large piles paid for the same broadphase work several times per step, and performance regressions were difficult to catch because the benchmark suite covered only a 500-body mixed scene.

## Decision

`RigidWorld.Step` uses one deterministic sort-and-sweep broadphase and one narrow-phase manifold build per step, then reuses those manifolds across configured substeps. This change adds 10,000-body sparse and fixed-body benchmarks beside the existing 2,000-box pile. The benchmark scenarios separate broadphase sorting from narrow-phase contact work and keep allocations visible with `ReportAllocs`.

The current implementation already landed the solver-side change through the substep work that #1714 depends on. This PR records the regression coverage and documents the performance contract without changing contact ordering or solver behavior.

## Alternatives considered

- Add a persistent AABB tree immediately. The 3D world has one, but changing 2D broadphase storage would be a larger behavioral change than the current regression gap and needs separate profiling across moving proxies.
- Keep only the existing 500-body benchmark. It is fast to run, but it does not expose sparse-field sorting or fixed-body rebuild costs at the scale called out by the issue.

## Consequences

CI and local profiling now exercise the issue's 2,000/10,000-body shapes. The broadphase contract is explicit in the docs. A future persistent proxy tree can be evaluated against these baselines without changing the deterministic manifold order. This PR does not alter the already-shipped solver implementation.
