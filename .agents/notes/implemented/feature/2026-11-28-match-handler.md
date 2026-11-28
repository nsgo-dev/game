# Agent Note: Match handler lifecycle ordering

Status: implemented

## Problem

Room admission and departure were only capacity and membership mutations. Match code could not reject a join with metadata, distinguish quit from drop or kick, receive external signals on its own lifecycle, announce termination grace, or publish searchable labels.

## Decision

`gameserver.MatchHandler` is an optional room callback boundary. `JoinWithMeta` invokes `JoinAttempt` before taking a seat and invokes `Joined` after the member mutation. `LeaveWithReason` mutates membership before calling `Left`. `Room.Signal` copies the payload before invoking `Signal`, and `Terminate` calls `Terminate(grace)` before ending the room. Labels are copied into `RoomInfo` snapshots. Existing `Join` and `Leave` remain compatible wrappers.

The room owns ordering and membership state. Callback code must stay short and use the server mailbox for longer work. Authentication and account lookup remain caller concerns, so the core room does not import account services.

## Alternatives considered

- Let handlers wrap `Rooms.Join` and `Rooms.Leave`. Wrappers race the room mutation and cannot guarantee rejection before capacity is consumed.
- Pass mutable room maps to callbacks. That would expose locks and let callbacks deadlock or violate deterministic ordering, so callbacks receive values and copied metadata instead.

## Consequences

Matches can enforce game rules at admission and react to lifecycle events without changing existing callers. `Signal` currently executes synchronously on the caller's goroutine and honors an already-canceled context. A future match mailbox can route it onto a dedicated match goroutine without changing the handler interface.
