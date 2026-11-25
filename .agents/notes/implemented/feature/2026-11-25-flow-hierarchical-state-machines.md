# Agent Note: Hierarchical state machines (`HSM[S]`) in `game/flow`

Status: implemented

## Problem

`game/flow.Machine[S]` models flat finite state machines where `Current()` returns a single state `S` and every state must declare its own outgoing transitions. As enemy and character controllers grow into composite modes (such as `combat` containing `approach`, `attack`, and `recover`) with shared interrupts (`stunned`, `dead`), callers must either duplicate interrupt transitions onto every sub-state or nest `Machine` instances by hand, leaving outside-in `Enter`, inside-out `Exit`, lowest-common-ancestor cross-branch transitions, shallow history, and event ordering to ad-hoc game code (#838).

Because `game/flow` is a `Stable` package, adding hierarchical state machines must keep `Machine[S]`, `State`, and all existing `flow.Machine` tests unchanged.

## Decision

Add a dedicated `HSM[S comparable]` type and typed event helpers in `packages/game/flow/hsm.go`, reusing `flow.State` for `Enter`/`Update`/`Exit` callbacks and keeping `Machine[S]` completely untouched:

- **Composable nested machines**: `NewHSM(initial)` creates a machine; `AddChild(parent, child)` attaches a nested `*HSM[S]` to `parent` (panicking on cycles or attaching an already-attached child to a second parent; passing `nil` detaches).
- **LCA transition execution**: every active state along root-to-leaf is tracked as an ordered `[]hsmFrame[S]` (`(machine, stateID)` pairs). Target resolution checks the declaring machine's registered states first, then performs a deterministic pre-order DFS over `stateOrder` (never Go map iteration order) to resolve cross-branch targets, falling back to a leaf in the declaring machine for unregistered callback-less states. Transitions exit active frames inside-out down to the divergence index (lowest common ancestor), enter target prefix frames outside-in, and descend into any child machine until a leaf is reached.
- **Innermost-first transition priority**: parent transitions apply while any descendant is active. Both event and polled evaluation walk `activePath` from leaf to root so child states can intercept and override parent transitions while preserving declaration order within each machine level.
- **Deterministic once-per-step event queue and `Bus` integration**: `Send(event)` (and `BindBus`) appends to the root machine's FIFO event queue without mutating HSM state mid-dispatch. At the start of `Update(dt)`, the root machine snapshots and clears its queue, processes each event once in send order (stopping at the first matching transition per event), and runs polled transitions only if no event triggered a transition. Events sent from inside `Enter`, `Exit`, or `Update` callbacks append to the fresh queue and are deferred to the next `Update` step. Value-matched transitions (`AddEventTransition`, `AddAnyEventTransition`) guard comparability via `reflect.Type.Comparable` so non-comparable struct events never panic; typed payload transitions (`OnEvent`, `OnAnyEvent`) match by Go type `E` with an optional `func(E) bool` guard.
- **Optional shallow history**: `SetHistory(true)` on a child `*HSM[S]` records its last active direct child on exit and resumes that direct child (not deeper descendants unless those child machines also enable history) when its parent is re-entered; `ClearHistory()` resets recorded history recursively.
- **Path inspection & re-entrancy guards**: `Current() []S` returns a defensive copy of the root-to-leaf path (or initial path before `Start()`, or `nil` for an inactive child machine), alongside `Leaf()`, `In(state)`, `Previous()`, `TimeInState()`, and `TimeIn(state)`. A monotonic transition generation counter stops parent `Update` loops from invoking `Update` on a child that was just exited by a parent's `SetState`, and re-entrant `SetState` calls during `Enter`/`Exit` are queued and drained after the in-flight transition finishes.

## Alternatives considered

### Extending `Machine[S]` in place with nested states

Strongest argument: callers would only learn one state-machine type (`Machine[S]`) instead of choosing between `Machine[S]` and `HSM[S]`.

Rejected because `game/flow` is a `Stable` package and `Machine[S].Current()` returns a single `S`, whereas hierarchical machines require `Current()` to return the active root-to-leaf path `[]S` (#838). Keeping `Machine[S]` untouched preserves 100% API and allocation compatibility for simple flat machines while giving `HSM[S]` a clean `Current() []S` signature and reusing `flow.State`.

### Delivering `Send` and `Bus` events synchronously on arrival

Strongest argument: transitions would take effect immediately on `Send` or `flow.Publish` without waiting for `Update(dt)`.

Rejected because synchronous dispatch inside `Bus.Publish` or physics/input callbacks causes re-entrant `Exit`/`Enter` mutations mid-step, allows multiple event passes per loop tick, and breaks deterministic step boundaries. Buffering into a root FIFO queue drained once per `Update(dt)` guarantees deterministic send-order processing once per fixed step and prevents same-step callback cascades.

### Deep history by default

Strongest argument: resuming the exact leaf across arbitrarily deep trees requires only one toggle on the root.

Rejected because shallow history per child machine (`child.SetHistory(true)`) matches standard UML/SCXML shallow-history semantics and issue #838's scope directly, and callers who want multi-level resumption can opt in on each nested machine independently without unintended stale sub-states two levels down.

## Consequences

- **Benefits**: Composite AI and player controllers gain outside-in `Enter`, inside-out `Exit`, LCA cross-branch transitions, parent-level interrupts with innermost-first override, deterministic FIFO event + `flow.Bus` transitions, and shallow history with zero goroutines, zero wall-clock reads, zero global state, and zero changes to `Machine[S]`.
- **Costs**: `game/flow` now exports two state machine types (`Machine[S]` for flat FSMs and `HSM[S]` for nested hierarchies); callers driving an `HSM` must call `Update(dt)` on the root machine (child `Update` calls are no-ops).

## Verification

- `packages/game/flow/hsm_test.go` verifies under `nsgotest` (`game.Loop` fixed stepping):
  - Combat machine with `approach`/`attack`/`recover` sub-states and parent-level `stunned` and wildcard `dead` transitions verifying exact outside-in `Enter`, inside-out `Exit`, `Update` order, `Current()`, `Leaf()`, `In()`, and per-level `TimeIn`/`TimeInState`.
  - Cross-branch transitions (`idle -> attack`, `attack -> patrol`) exiting only to the lowest common ancestor (`alive`).
  - Innermost-first priority for both polled conditions and event override/bubbling.
  - Deterministic FIFO event send-order outcomes, once-per-step consumption, callback-deferred events waiting for the next step, guarded and non-comparable events, and `flow.Bus` integration via `BindBus`, `OnEvent`, and `OnAnyEvent`.
  - Three-level shallow history verification (`SetHistory(true)` resumes direct child while nested child without history restarts at initial) and `ClearHistory()`.
  - `SetState` before/after `Start`, child `SetState`, parent `Update` calling `SetState` skipping exited child `Update`, re-entrant `SetState` inside `Enter`, self-transitions, `AddChild` cycle/re-attach guards, defensive copy on `Current()`, and nil/negative-`dt` safety.
- `packages/game/flow/example_test.go` adds runnable `ExampleHSM`, and all pre-existing `flow.Machine` tests in `flow_test.go` pass unchanged.
- Checked with `gofmt`, `go tool nsgo test ./flow/...`, `go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid'`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
