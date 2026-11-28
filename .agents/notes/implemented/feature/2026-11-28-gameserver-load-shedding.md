# Agent Note: Refuse new admissions when capacity is exhausted

Status: implemented

## Problem

Gameserver rooms had per-room capacity but no process-wide room/member cap or busy admission hook, so a host could keep accepting work after its matches fell behind (#1696).

## Decision

`Rooms.SetCapacity` configures maximum open rooms, maximum members, and an optional busy callback. `Create` and new-member `Join` return `ErrServerBusy` when a limit or callback refuses admission. Existing members remain in their room and can leave or resume normally.

## Alternatives considered

- Reject all operations while busy: rejected because existing matches must continue draining.
- Infer load from room count alone: rejected because tick overruns and external capacity signals also need to refuse admission.

## Consequences

Matchmakers can route new work away from overloaded hosts while preserving running rooms. Capacity state is process-local and deterministic, and callers can connect it to Runner overrun windows or an external load signal.
