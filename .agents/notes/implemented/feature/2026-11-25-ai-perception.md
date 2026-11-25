# Agent Note: AI perception with sight, hearing and memory

Status: implemented

## Problem

Stealth, action and strategy games need agents that see in a cone, hear noises, and remember where a stimulus was last sensed, with lost and forgotten events a behavior tree can react to (#834). Hundreds of agents casting a ray to every target on every fixed step is too expensive, so line-of-sight checks need a per-step budget that still serves every agent in a fixed, reproducible order. The issue asks for a 2D API and a 3D variant on `math3d.Vec3`, with line-of-sight adapters for 2D physics, 3D physics and `lights2d` occluders.

`game/ai` is Stable, while `math3d`, `physics3d` and `lights2d` are Experimental, and `TestStabilityTable` fails when a stable package imports an experimental one. A first version that kept everything in `ai` failed that test.

## Decision

### Package topology

- `packages/game/ai/internal/sense` is the only implementation: sight cone and peripheral range, the line-of-sight budget, hearing, memory ageing, linear strength decay and forgetting, all on a float64 3D `Vec`. 2D callers leave Z zero.
- `packages/game/ai` (Stable, additions only) holds the shared types (`StimulusID`, `Team`, `Attitude`, `AttitudeMask`, `AttitudeFunc`, `Sense`, `EventKind`, `SightConfig`, `HearingConfig`, `PerceptionStats`), the 2D front end (`Perception`, `PerceptionSystem`, `Target`, `Noise`, `Memory`, `Event`, `LineOfSight`) and `PhysicsLineOfSight` for the stable `physics.World`.
- `packages/game/ai/perception3d` (Experimental) is a thin front end over the same engine on `math3d.Vec3`, reusing the `ai` shared types, with `PhysicsLineOfSight` for `physics3d`.
- `lights2d.LineOfSight(&occluders)` lives in `lights2d` and returns a `func(from, to core.Point) bool`, the underlying type of `ai.LineOfSight`, so `ai` never imports `lights2d`. It reads the slice through the pointer on every call, so a level that edits its occluders needs no new adapter.

### Budget and order

- Pairs in view are keyed by (agent registration sequence, target ID). Agents are served in the order they were added; targets are sorted by ID and the first of a duplicate ID wins.
- When `Budget > 0` and the pairs in view outnumber it, a step checks `Budget` pairs starting at the first key strictly after the cursor (the key of the last pair checked), wrapping around. The cursor is a key, not an index, so removed agents, despawned targets and agents added mid-round never shift it onto the wrong pair: the round resumes at the next surviving key.
- A deferred pair keeps the visibility its memory had. When the budget covers every pair, all pairs are checked in order. A nil `LineOfSight` sees every pair in view and makes no calls, so the budget does not apply.

### Memory rules

- Sight always wins over hearing: a noise from a stimulus in sight does not move its memory. A heard memory has zero velocity and strength `max(current, 1 - distance/reach)`; strength falls linearly to 0 over `ForgetAfter`, and the memory is forgotten when `Age` reaches it (`ForgetAfter <= 0` forgets it on the step after it stops being sensed).
- Membership travels with the `Perception` value, but a `State` records a pointer to itself on registration. `Register` treats a struct copy of a registered `Perception` as a new agent: it clones the memories and drops the events before claiming it, so a prototype copied after `Add` is stepped and never shares backing arrays with the original.
- `Remove` keeps the memories but marks none visible and clears the events, so a despawned or paused agent never reports a stale sighting; re-adding it and stepping restores sight with `EventUpdated`.

## Alternatives considered

- **Everything in `ai`, including the 3D front end and the `lights2d` adapter** — one import for users and the literal reading of the issue. Rejected because it makes a Stable package import three Experimental ones, which `TestStabilityTable` refuses and which would tie `ai`'s compatibility to packages whose APIs may still change.
- **Two engines, one float32 2D and one `math3d` 3D** — each could use its native vector type without conversion. Rejected because the cone, budget, hearing and memory rules would be written twice and drift; one float64 engine keeps 2D and 3D behavior identical and is precise enough for both.
- **Dropping a deferred pair's visibility (treat unchecked as not seen)** — conservative and simpler to state. Rejected because with a small budget every target would flicker between Gained and Lost as the round passes, flooding events; keeping the last result makes a raised wall hide a target only when the round reaches that pair.
- **A cursor stored as an index into the candidate list** — cheaper than a binary search. Rejected because the candidate list changes every step as agents turn, move, join and leave; an index would skip or repeat pairs, while a key resumes at the right place with a `sort.Search` over the sorted candidates.
- **Tracking membership only in the system (`slices.Contains(s.agents, p)`)** — no hidden field in the value. Rejected because `Add` would become O(n) per call and the cross-system panic would need a global registry; the self pointer detects copies in O(1) while the owner field keeps the per-system check.

## Consequences

- **Benefits**: `ai` stays Stable with no experimental imports; 2D and 3D share every rule and test; budgeted runs are reproducible step for step under `game.Loop`; 300 agents against 100 targets with a budget of 64 take about 0.74 ms per step in `BenchmarkPerceptionSystemStep`.
- **Costs and known limits**: 3D users import `game/ai/perception3d` and `lights2d` users take the adapter from `lights2d` rather than `ai`. A deferred pair can report a target visible for up to `ceil(pairs/Budget)` steps after it went behind cover. If `math3d` or `lights2d` become Stable, moving the front ends into `ai` is worth revisiting.

## Verification

- `packages/game/ai/perception_test.go`: cone, range and occluder cases against `lights2d` and `physics`; `TestPerceptionBudgetRoundRobin` checks every call against the fixed order over 40 steps; `TestPerceptionBudgetChangingCandidates` removes the cursor agent, drops the cursor target and adds agents under a limited budget, and checks every pair is served within `ceil(n/budget)` steps; `TestPerceptionCopyAndRemove` covers copies and removal; `TestPerceptionLoopDeterminism` compares two `game.Loop` runs under nsgotest.
- `packages/game/ai/perception3d/perception3d_test.go` covers the same against `physics3d`.
- Run `go tool nsgo test ./ai` from `packages/game` and `bun run verify-notes` from the root.
