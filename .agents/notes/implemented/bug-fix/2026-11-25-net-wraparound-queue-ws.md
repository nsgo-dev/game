# Agent Note: Net tick wraparound, O(1) peer-event queue, and WebSocket control-frame checks

Status: implemented

## Problem

Three gaps remained in `packages/game/net` after dynamic session membership and bounded inboxes landed:

1. `LockstepSession`, `RollbackSession`, and `match` (`rollback.go`, `membership.go`) compared raw `uint32` ticks or widened them to `int64`, and used `noLeave = ^uint32(0)` as a sentinel for staying members. A session crossing the `2^32` tick wraparound (`0xFFFFFFFF -> 0`), or starting/joining near it, misordered inputs, rollbacks, pending joins, checksums, and pruning, and could not represent a leave on tick `0xFFFFFFFF`. In addition, naive `uint32` rewrites of `RollbackSession.pruneHistory` risk pruning tick `start` before anything is confirmed (`ConfirmedTick() == -1`), stalling a peer whose first input arrives more than `MaxRollback + 2` ticks late.
2. `sessionTable` (`session.go`) popped peer lifecycle events (`PeerJoined`, `PeerDropped`, `PeerResumed`, `PeerLeft`) with `slices.Delete(t.events, 0, 1)`, making bursts O(n^2).
3. `readWSFrameLimit` (`ws_native.go`) did not enforce RFC 6455 §5.5 on control frames (`opcode >= 0x8`): fragmented control frames (`FIN=0`), control payloads over 125 bytes, and reserved control opcodes `0xB`–`0xF` were accepted instead of rejected.

## Decision

1. **Serial-number tick arithmetic (`replication.go`, `membership.go`, `rollback.go`)**:
   - `tickDelta`, `tickBefore`, `tickAfter`, `tickAtOrAfter`, `tickAtOrBefore`, `tickCompare`, `tickMax`, and `tickMin` implement RFC 1982 signed 32-bit difference (`int32(a - b)`) for all tick comparisons in `LockstepSession`, `RollbackSession`, and `match`.
   - `matchMember` carries an explicit `hasLeave bool` instead of overloading `until == ^uint32(0)` (`noLeave`), and the internal `joinState` member wire record carries a 1-byte `hasLeave` flag (`joinMemberSize = 13`, canonical `until = 0` when `hasLeave == 0`). Tick `0xFFFFFFFF` is therefore a normal leave tick.
   - `RollbackSession` tracks confirmation with `confirmedTick uint32` and `hasConfirmed bool` (exposing `ConfirmedTick() int64` as `-1` when `!hasConfirmed`), using `match.start` as the explicit first unconfirmed tick (`nextToConfirm`). On `join`, `hasConfirmed` is set when `js.tick > 0` or any member's `from` precedes `js.tick` in serial order (covering joins right at wrapped tick `0`).
   - `pruneMembers` advances `match.start` and staying members' `from` once they fall more than `1<<29` ticks behind `tick` (keeping `1<<20` ticks of margin) so multi-year matches never cross the `2^31` serial-number half-space against initial anchors.
2. **Unconfirmed history retention (`rollback.go`)**:
   - `RollbackSession.pruneHistory` returns early when `!s.hasConfirmed`, and otherwise never advances `floor` past `confirmedTick - 1` or before `start`, and prunes checksums below `checksumFloor()`, which is `tickMin(currentTick-256, floor)` clamped to `start`. A session with a silent peer stalls `MaxRollback` ticks past the first unconfirmed tick (see `2026-11-28-netplay-bounded-session-state.md`). The first unconfirmed tick and its inputs/checksums are never pruned before confirmation.
3. **Amortized O(1) lossless peer-event queue (`session.go`)**:
   - `sessionTable.events` reuses the package's head-indexed `fifo[PeerEvent]` (`transport.go`), which advances a head index, compacts only when the consumed prefix is at least 64 entries and at least as long as the remainder, releases burst capacity past `packetFIFOKeep` once drained, and never drops lifecycle events.
4. **RFC 6455 §5.5 control-frame validation (`ws_native.go`)**:
   - `readWSFrameLimit` checks `opcode >= wsOpClose` before reading extended length bytes and returns `ErrInvalidPacket` if `opcode > wsOpPong` (`0xB`–`0xF`), `!fin`, or the 7-bit length field exceeds `wsMaxControlPayload` (125).

## Alternatives considered

- **Keep `noLeave = ^uint32(0)` and special-case it**: avoids changing the internal 12-byte `joinState` member record, but makes tick `0xFFFFFFFF` impossible to use as a leave tick right at the wraparound boundary. Adding a 1-byte `hasLeave` flag to the internal `joinState` record removes the ambiguity cleanly.
- **Widen internal session ticks to `int64`**: avoids modular comparisons, but breaks whenever a session joins mid-match near `2^32` because wire frames (`InputFrame`, `memberChange`, `joinState`) carry 32-bit ticks. RFC 1982 serial-number arithmetic works identically across local stepping and wire messages, matching `ServerWorld`/`ClientWorld`.
- **Use a fixed-capacity ring buffer for `sessionTable.events` that drops oldest events**: bounds memory strictly, but silently dropping `PeerJoined` or `PeerLeft` corrupts server session accounting and room membership. `fifo[PeerEvent]` is lossless and already releases burst capacity when empty.

## Consequences

- **Benefits**: Lockstep and rollback matches (including mid-match joins, leaves, rollbacks, checksums, and pruning) survive `2^32` tick wraparound; late-starting rollback peers confirm cleanly beyond `MaxRollback + 2` ticks; `NextEvent` drains large bursts in linear time with zero dropped lifecycle events; native WebSocket endpoints reject invalid RFC 6455 control frames immediately on the header.
- **Costs**: `joinState` member entries grow from 12 to 13 bytes (an internal session-sync format between peers of the same build). Serial-number ordering assumes active history windows stay well within `2^31` ticks, which `pruneMembers` enforces for `start` and `from` anchors.
