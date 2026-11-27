# Agent Note: Suppress queued physics3d contact events when RemoveBody runs inside callbacks

Status: implemented

## Problem

`World.reconcileContactEvents` in `pkg/physics3d/layers_triggers.go` previously built the entire step's `ContactEvent` slice directly into `w.contactEvents`, replaced `w.activeContacts` with the step's `current` map up front, and then looped over the pre-built slice to invoke `OnContact` and `OnTrigger`. When a callback called `World.RemoveBody`, `emitRemovedBodyExits` immediately fired `ContactExit` events for the removed body and appended them to `w.contactEvents`, but the outer loop kept iterating the original slice and delivered queued `ContactStay` / `ContactEnter` / `ContactExit` events involving the already-removed body after its `ContactExit`. In addition, pre-swapping `w.activeContacts` caused `RemoveBody` inside an early `ContactEnter` callback to emit an orphan `ContactExit` for another new pair whose `ContactEnter` had not yet been delivered, and left `World.ContactEvents()` out of order relative to what callbacks observed (#1595).

## Decision

`World.reconcileContactEvents` stages the step's transitions into a reusable `pendingEventsBuf` and updates `w.activeContacts` and `w.contactEvents` incrementally as each event is dispatched:

- `w.activeContacts` represents only pairs whose lifecycle is currently open (their `ContactEnter` has been delivered and their `ContactExit` has not yet been delivered). Each `ContactEnter` or `ContactStay` writes its `activeContactRecord` into `w.activeContacts`, and each `ContactExit` deletes its key, immediately before invoking `emitContactEvent`.
- While `w.dispatchingContacts` is true, `World.RemoveBody` records the removed body's ID in `w.removedDuringDispatch` (cleared in a `defer` when dispatch ends) before calling `emitRemovedBodyExits`.
- The dispatch loop skips any queued `pendingContactEvent` whose `key.a` or `key.b` is in `w.removedDuringDispatch`, neither mutating `w.activeContacts` nor appending to `w.contactEvents` nor invoking callbacks.
- `emitRemovedBodyExits` deletes each pair from `w.activeContacts`, appends its `ContactExit` to `w.contactEvents`, and invokes `emitContactEvent` one pair at a time (skipping any key already deleted by a nested `RemoveBody` inside an earlier `ContactExit` callback).

This preserves bit-identical `activeContacts`, `ContactEvents()`, snapshots, and `Checksum()` values whenever callbacks do not remove bodies, while guaranteeing that removing a body inside `OnContact` or `OnTrigger` never delivers events after `ContactExit`, never emits an orphan `ContactExit` without a prior `ContactEnter`, and keeps `World.ContactEvents()` in exact callback delivery order.

## Alternatives considered

- **Defer `RemoveBody` exits until the next `World.Step` (matching 2D `pkg/physics`)**: simpler dispatch loop, and avoids re-entrant exits during `Step`. Rejected because `pkg/physics3d` already documents and tests (`TestTriggerAndContactEventsStrictlyOnce`) that `RemoveBody` emits `ContactExit` immediately before returning, and deferring would break callers relying on synchronous cleanup on removal.
- **Pre-swap `w.activeContacts = current` and only filter `removedDuringDispatch` during the loop**: fixes `Stay` after `Exit` for previously active pairs, but still causes `RemoveBody` during an `Enter` callback to emit an orphan `ContactExit` for another newly overlapping pair whose `Enter` was never delivered, and still lets a nested `RemoveBody` inside an `Exit` callback misorder `w.contactEvents`.

## Consequences

- **Benefits**: `OnContact` and `OnTrigger` maintain strict `Enter -> Stay* -> Exit` lifecycles even under single or chained `RemoveBody` calls inside callbacks; `World.ContactEvents()` always matches callback delivery order for replay and rollback checks; existing determinism and checksum tests remain unchanged.
- **Costs and known limits**: `reconcileContactEvents` updates `w.activeContacts` per dispatched event rather than swapping the map pointer in O(1), and keeps a scratch `pendingEventsBuf` slice on `World` (cleared at the end of dispatch to avoid retaining `*Body` pointers). Re-entrant `World.Step` calls from inside `OnContact` or `OnTrigger` remain unsupported.
