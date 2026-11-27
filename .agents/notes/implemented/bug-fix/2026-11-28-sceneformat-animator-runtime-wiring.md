# Agent Note: Built-in `animator` component runtime events and handlers

Status: implemented

## Problem

`sceneformat.AnimatorComponent()` declared two events (`stateChanged`, `clipEvent`) and two handlers (`setState`, `trigger`), so the editor's connection UI and `ValidateConnections` accepted scene connections targeting them, but nothing at run time implemented them: the handlers had `Func == nil` (causing `sceneformat.Connect` and `SpawnConnected` to fail with `"has no Func"` on the default registry) and `animclip.StateMachine` never emitted `animator.stateChanged` or `animator.clipEvent` through `sceneformat.Signals` (#1592, part of #1479).

## Decision

Back the built-in `animator` component's events and handlers across `pkg/sceneformat` and `pkg/animclip` without introducing an import cycle (`animclip` imports `sceneformat`):

- **Runtime interfaces and payloads in `pkg/sceneformat/animator.go`**: Export `AnimatorController` (`SetState(name string) bool`, `Trigger(name string)`), `AnimatorProvider` (`Animator(objectID string) AnimatorController`), `AnimatorStateChangedEvent{ObjectID, From, To}`, `AnimatorClipEvent{ObjectID, State, Name}`, and the member name constants (`AnimatorEventStateChanged`, `AnimatorEventClipEvent`, `AnimatorHandlerSetState`, `AnimatorHandlerTrigger`).
- **Real handler `Func`s on `AnimatorComponent()`**: `setState` and `trigger` resolve the target object's `AnimatorController` first from `c.Signals.Animator(c.Target.ID)` (bound via `Signals.BindAnimator`), then from `c.Context.(AnimatorProvider)` (implemented nil-safely by `*BuiltScene` via `BuiltScene.SetAnimator`/`Animator`). `sceneformat.Connect` therefore succeeds with `AnimatorComponentRegistry()` and `RegisterAnimatorComponent` out of the box.
- **Signal wiring in `pkg/animclip/states_player.go`**: `animclip.Connect(sm, signals, objectID)` binds `sm` on `signals` and wires `sm` so state transitions (from `Update` transitions or `SetState` when switching to a different state) emit `animator.stateChanged` and active-clip cues emit `animator.clipEvent`, alongside any Go `OnTransition`/`OnEvent` callbacks rather than overwriting them. `NewAnimator` also calls `built.SetAnimator(objectID, sm)` so scenes connected with `built` as `Call.Context` (including editor Play mode) drive `setState` and `trigger` automatically. Triggers pulsed by signal handlers during `StateMachine.Update` are preserved for the next step unless consumed by an `onEnd` transition in the same step.

### Related notes

- Extends [sprite state machines in `*.states.yaml`](../feature/2026-11-25-sprite-state-machine-yaml.md).

## Alternatives considered

- **Removing the `stateChanged`, `clipEvent`, `setState`, and `trigger` declarations from `AnimatorComponent()`**: Strongest argument is zero new cross-package surface if wiring is left to games. Rejected because `animator` connections are first-class in the editor alongside physics trigger events, and authors expect scene-authored wiring to work without re-registering internal component types.
- **Wrapping `sm.OnTransition` and `sm.OnEvent` closures inside `animclip.Connect`**: Strongest argument is no new fields on `StateMachine`. Rejected because any caller setting `sm.OnTransition` or `sm.OnEvent` after `animclip.Connect` would silently break signal emission; keeping `signals`/`objectID` inside `StateMachine` mirrors `sceneformat.PhysicsScene.Signals`.
- **Requiring callers to pass a custom map in `Call.Context` only**: Strongest argument is no methods on `Signals` or `BuiltScene`. Rejected because games often pass their own context struct or `nil` to `sceneformat.Connect`; supporting `Signals.BindAnimator` first and `AnimatorProvider` (`*BuiltScene`) second covers both patterns cleanly.

## Consequences

- **Benefits**:
  - `sceneformat.Connect` and `SpawnConnected` succeed with the default `animator` component registration.
  - Scene connections on `animator.stateChanged` and `animator.clipEvent` fire once per transition or cue with typed payloads, and `animator.setState` and `animator.trigger` drive the target `*animclip.StateMachine` at run time.
  - Go callbacks (`OnTransition`, `OnEvent`) and scene `Signals` coexist cleanly regardless of setup order.
- **Costs**:
  - `sceneformat` exports small runtime interface and payload types (`AnimatorController`, `AnimatorProvider`, `AnimatorStateChangedEvent`, `AnimatorClipEvent`) and `BindAnimator`/`SetAnimator` lookup maps on `Signals` and `BuiltScene`.
  - `StateMachine.SetState` now fires `OnTransition` and `animator.stateChanged` when switching to a different state (no-op when setting the already-active state or before callbacks/signals are attached).

## Verification

- `go tool nsgo test ./pkg/sceneformat` (`TestAnimatorComponentConnectDefaultRegistryAndHandlers`, `TestAnimatorHandlersResolveContextAndPrecedence`)
- `go tool nsgo test ./pkg/animclip` (`TestAnimatorSignalsEventsAndHandlers`, `TestNewAnimatorBuiltSceneContextHandlersWithoutExplicitConnect`, `TestAnimatorSelfWiredClipEventToTriggerAndSpawnConnected`)
- `go tool nsgo test ./pkg/editor`
- `go tool nsgo check -target host -jobs 2`
- `(cd tools && go run ./changelog check) && bun run verify-notes`
