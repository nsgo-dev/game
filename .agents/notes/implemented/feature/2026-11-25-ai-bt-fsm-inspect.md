# Agent Note: Behavior Tree and State Machine Inspection and Debug Visualization

Status: implemented

## Problem

Behavior trees in `game/ai` were opaque once constructed, and `flow.Machine` kept only `Current()` and `Previous()` with no transition history. Debugging unexpected AI behavior required ad-hoc print statements, games had no deterministic text dump of AI decisions for `statedump` diffs, and `game/debug` had no overlay panel for live AI trees/machines or world-space steering guides.

## Decision

We split AI state inspection cleanly along package stability boundaries:

1. **Stable read-only inspection in `game/ai`** (`packages/game/ai/bt_inspect.go`):
   - `Named(name, child)` attaches an optional label without altering tree topology or blackboard observer-abort semantics (`unwrapNamed` keeps `Named(BlackboardCondition(...))` transparent inside `Sequence` preemption checks). `BuildBehaviorTree` automatically wraps YAML nodes that declare `name:` so authored `*.bt.yaml` names carry into runtime inspection.
   - Built-in behavior tree nodes embed a value-typed `btTraceState` (`lastStatus`, `ticked`) updated in place on `Tick` and cleared on `Reset`, and implement an unexported `btInspectable` interface.
   - `BTInspector` precomputes a flat pre-order entry table and structural paths (`"root"`, `"root/0"`, …) once in `NewBTInspector(root)`. Subsequent `WalkInto(dst)` and `RunningPathInto(dst, nodes)` calls read `btInspectable` directly with zero heap allocations once `dst` is warmed up.
   - `BTInspector.Format()` and `FormatBTTrace(nodes)` emit a canonical indented text form (`* ` prefix on active running-path nodes, `  ` otherwise) for `statedump.Dump.SetValue` and golden diffs.

2. **Stable bounded transition history in `game/flow`** (`packages/game/flow/machine.go`):
   - `Machine[S]` tracks an `Update` step counter (`Step()`) and retains the most recent `DefaultHistoryLimit` (32) transitions in a pre-allocated ring buffer of `TransitionRecord[S]{From, To, Step, Transition}`, tunable with `SetHistoryLimit` (`0` disables history) and cleared with `ClearHistory()`.
   - `AddNamedTransition` and `AddNamedAnyTransition` attach explicit transition labels; unnamed transitions default to `"from->to"` / `"*->to"`, and `SetState` records `"SetState"`.
   - `HistoryInto(dst)` reads retained transitions in chronological order with zero allocations once warmed up.
   - Every `*Machine[S]` implements the type-erased `MachineInspector` interface (`Inspect() MachineInspection`, `FormatState() string`) so generic debug overlays can inspect any state machine without knowing `S`.

3. **Release-stripped visualization in `game/debug`** (`packages/game/debug/ai.go`, `packages/game/debug/ai_release.go`, `packages/game/debug/types.go`, `packages/game/debug/overlay.go`):
   - `Overlay` supports an AI inspector section via pull (`Options.AI`) or push (`SetAI` / `ClearAI` / `RefreshAI`) using `AISnapshot` (with `BehaviorTreeSnapshot` and `StateMachineSnapshot` helpers), highlighting the active BT running path and the FSM's current state and recent transition history.
   - `ShapeOverlay` and `Overlay` provide scale-invariant steering debug helpers (`DrawSteering`, `DrawVector`, `DrawPath`, `DrawTarget`) drawing body radius, velocity, desired velocity, target reticle, and waypoint path.
   - All AI overlay UI and steering drawing code lives behind `//go:build !release` with empty no-op stubs in `ai_release.go` (`//go:build release`), verified by a symbol-stripping test in `debug_release_test.go`.

### Related notes

- Extends behavior tree execution and YAML authoring in [ai-bt-yaml-blackboard](./2026-11-25-ai-bt-yaml-blackboard.md) and [ai-bt-timeouts-random-services](./2026-11-25-ai-bt-timeouts-random-services.md).
- Visualizes steering agents from [ai-steering-extensions](./2026-11-25-ai-steering-extensions.md) and follows the `-tags release` stripping pattern used in [ai-influence-maps](./2026-11-25-ai-influence-maps.md).
- Keeps `flow.Machine` transition history separate from UML shallow history in [flow-hierarchical-state-machines](./2026-11-25-flow-hierarchical-state-machines.md).

## Alternatives considered

1. **Adding debug drawing or overlay helpers directly in `game/ai` or `game/flow`**:
   - Strongest argument: colocates visualization right next to `BTInspector` and `Machine`.
   - Why rejected: `game/ai` and `game/flow` are stable packages (`TestStabilityTable` forbids stable packages from importing experimental `game/debug` or `game/statedump`), and placing UI overlays inside `game/debug` keeps all `-tags release` stripping in one uniform place.
2. **Wrapping every built-in BT node in a separate tracing proxy struct**:
   - Strongest argument: avoids adding a `btTraceState` field to each built-in node struct.
   - Why rejected: wrapping breaks direct pointer identity, doubles node allocations, and complicates composite active-child queries (`s.cur`, `s.running`, `p.status[i]`). An embedded 2-byte `btTraceState` costs virtually nothing and keeps zero-alloc `Tick` and `WalkInto`.
3. **Exporting an event-callback hook instead of a ring buffer on `flow.Machine`**:
   - Strongest argument: callers could store unlimited history or format custom logs.
   - Why rejected: callers would have to wire boilerplate per machine just to inspect recent transitions in the overlay or `statedump`, whereas a bounded ring buffer works out of the box with zero per-transition allocations after warm-up and can be disabled with `SetHistoryLimit(0)`.

## Consequences

- **Benefits**:
  - Games and tests can inspect any behavior tree's running path and node statuses with zero per-tick allocations once warmed up.
  - Every `flow.Machine` automatically records a bounded chronological transition history with step numbers and transition labels, inspectable via type-erased `MachineInspector`.
  - Both formats produce deterministic canonical text suitable for `statedump` `Values` and line-level `statedump.Diff`.
  - `game/debug` provides a live AI panel and zoom-invariant steering draw helpers that strip completely under `-tags release`.
- **Costs**:
  - Built-in behavior tree nodes carry an extra 2-byte `btTraceState` field and record their status on every `Tick`.
  - `flow.Machine` allocates a 32-element ring buffer on its first transition unless `SetHistoryLimit(0)` is set.
  - `game/debug/types.go` imports `game/ai` and `game/flow` (both stable, init-free packages) to define `AISnapshot` and `SteeringDebug`.
