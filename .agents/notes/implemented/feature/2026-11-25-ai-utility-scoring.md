# Agent Note: Utility AI scoring in game/ai

Status: implemented

## Problem

`game/ai` provides behavior trees, steering, A*, and navigation meshes, which decide actions through fixed boolean priorities. Strategy, survival, sim, and RPG companion AI often need to score competing options continuously against world state (Dave Mark's Infinite Axis Utility System) while staying deterministic, zero-allocation across hundreds of agents per step, embeddable as a behavior tree node, and optionally authorable in strict versioned YAML.

Additionally, `game/ai` is a **Stable** package while `game/resource` is **Experimental** (`TestStabilityTable` forbids stable packages from importing experimental ones), yet considerations must support `resource` curve files alongside analytic response curves.

## Decision

Implement Utility AI in new files `packages/game/ai/utility.go` and `packages/game/ai/utility_yaml.go` with four core boundaries:

### 1. CurveEvaluator interface for resource curve interop

Define `CurveEvaluator` as `interface { Eval(t float32) float32 }`. `*resource.Curve` already implements `Eval(t float32) float32`, so callers pass `*resource.Curve` directly to `ResourceCurve(eval)` and `UtilityBindings.LoadCurve` without importing `game/resource` in `game/ai` or duplicating `resource.Curve`'s YAML schema. Analytic curves (`linear`, `polynomial`, `logistic`, `logit`, `step`) evaluate inline with `[0, 1]` clamping and NaN/Inf guards.

### 2. Generic, zero-allocation Reasoner[C]

Parameterize `Consideration[C]`, `UtilityAction[C]`, and `Reasoner[C]` over the caller's context type `C` so `Input func(C) float64` receives concrete values (such as `*AgentState` or a value struct) with zero interface boxing.

- Action scoring multiplies considerations (short-circuiting on `0` to preserve vetoes), applies Dave Mark's compensation factor (`modFactor = 1 - 1/N`, `score += (1 - score)*modFactor*score`), multiplies by `Weight` (`0` defaults to `1`, negative disables), and adds `Reasoner.Momentum + UtilityAction.Momentum` only when the action is currently active and its base score is strictly positive (a vetoed action is never kept active by momentum).
- `ChooseWithCurrent(ctx, current, rng)` is stateless with respect to `Reasoner.current`, allowing one shared `Reasoner` to score hundreds of agents while reusing an internal `topBuf` slice for top-N weighted random draws from `*random.Stream` (`0 allocs/op` after warm-up). Ties always break in fixed action declaration order (lowest index wins).

### 3. Behavior tree integration via UtilitySelector

`UtilitySelector(reasoner, ctx, rng, subtrees)` resolves `map[string]Node` into a flat `[]Node` slice indexed by action index once at construction so `Tick` performs no map lookups or allocations. Each node tracks `active` (last chosen non-failed action, passed to `ChooseWithCurrent` for momentum) separately from `running` (whether that subtree returned `StatusRunning` and must be `Reset()` when preempted by a different action).

### 4. Versioned YAML format (`UtilityVersion = 1`)

`ParseUtility` and `EncodeUtility` decode and write `UtilityConfig` using `yaml.NewDecoder` with `KnownFields(true)`, trailing-document rejection, strict validation of version/curve/range fields, and deterministic 2-space YAML output. `BuildReasoner` resolves input names and caches loaded resource curves per file path.

## Alternatives considered

### Importing `game/resource` directly from `game/ai`

Directly referencing `*resource.Curve` and `resource.ParseAs` in `game/ai` would let YAML loading parse `.curve.yaml` files without a `LoadCurve` callback. Rejected because `game/ai` is Stable and `game/resource` is Experimental; importing it violates the API stability boundary enforced by `TestStabilityTable` and would expose a stable package to experimental resource format changes.

### Non-generic `Consideration` with `Input func() float64` closures or `func(any) float64`

Using `func() float64` closures would avoid type parameters on `Reasoner`, but scoring 500 agents would require 500 separate `Reasoner` instances (or mutating a shared global pointer). Using `func(any) float64` would box non-pointer contexts and allocate on every evaluation. Generic `Reasoner[C]` avoids both while keeping single-agent (`Choose`) and shared multi-agent (`ChooseWithCurrent`) usage zero-allocation.

### Resetting momentum whenever a subtree returns `StatusSuccess`

Clearing the active action on `StatusSuccess` would only apply momentum to multi-tick `StatusRunning` subtrees. Rejected because many utility actions are single-tick leaves (such as stepping a patrol or firing an attack) that would oscillate between nearly equal options every tick without momentum across `StatusSuccess`. Only `StatusFailure` or vetoing clears `active`.

## Consequences

- **Benefits**: `game/ai` stays Stable with no Experimental dependencies while accepting `*resource.Curve` natively; scoring 50 actions for 500 agents allocates 0 times per step in both deterministic best-action and top-N weighted modes; utility scoring composes cleanly as a `Node` inside existing behavior trees; YAML configs pass the full `formattest` conformance suite.
- **Costs**: Callers loading YAML configs with `kind: resource` curves supply a one-line `LoadCurve` callback (e.g., `resource.LoadAs[*resource.Curve]`), and callers in Go specify the context type argument `C` when constructing `UtilityAction[C]` / `Consideration[C]`.

## Verification

- `packages/game/ai/utility_test.go` covers all 6 curve kinds (including real `*resource.Curve` parsed via `resource.ParseAs`), Dave Mark compensation and veto math, a reference combat agent input table and fixed-order tie-breaking, same-seed top-N determinism with `random.Stream`, momentum anti-oscillation, `UtilitySelector` preemption/reset inside `game.Loop`, `testing.AllocsPerRun` (`0 allocs/op` for 50 actions x 500 agents in best-action and top-3 modes), and `formattest.Run` + YAML validation tests.
- Verified via `go tool nsgo test ./ai/...`, `go tool nsgo test . -- -run 'TestFormatPackagesRunConformance|TestFormatsPage|TestStabilityTable|TestAgentSkillValid'`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`.
