# Agent Note: Close never-joined server rooms on EmptyTimeout and guard Join against concurrent Tick close

Status: implemented

## Problem

`server.Rooms` (`pkg/online/server/rooms.go`) had two room-lifecycle defects (#1599):

1. `Create` left `Room.emptyAt` at the zero `time.Time`, while `Tick` only closed rooms whose `emptyAt` was non-zero. A room created and never joined stayed open indefinitely even with `EmptyTimeout == 0`, leaking memory and keeping its private code reserved for the lifetime of the process.
2. `Join` and `JoinCode` looked up `*Room` under `rs.mu`, released `rs.mu`, and then locked `r.mu` in `join`. `Tick` could lock `rs.mu` and `r.mu` in that window, observe `len(r.members) == 0`, and delete the room from `rs.rooms` and `rs.codes`, after which `join` still admitted the peer into the removed `*Room` — stranding the peer in a ghost room that `List` hid and `Leave` could no longer reach.

## Decision

- `Rooms.Create` captures `now := rs.now()` once and initializes both `created` and `emptyAt` to `now`, so a newly created room starts its empty-room timer immediately. `Tick` closes any room with `len(r.members) == 0 && now.Sub(r.emptyAt) >= rs.EmptyTimeout` (removing the `!r.emptyAt.IsZero()` guard so even a zero-value mock clock works consistently). Joining a room clears `emptyAt`, and `Leave` sets `emptyAt = rs.now()` only when the last actual member leaves.
- `Room` carries a `closed bool` field guarded by `r.mu` alongside `emptyAt`. Before deleting a timed-out room from `rs.rooms` and `rs.codes`, `Tick` sets `r.closed = true` while still holding `r.mu`.
- `Rooms.join` checks `r.closed` first under `r.mu` (before `r.state == RoomEnded`) and returns `ErrRoomNotFound`, matching the error a caller sees if lookup occurs after `Tick` deletes the room. `Room.Start` also checks `r.closed` under `r.mu` and returns `ErrRoomNotFound`.
- Lock order stays strictly `rs.mu` before `r.mu` (never `r.mu` before `rs.mu`), and `Join`/`JoinCode` continue to release `rs.mu` before acquiring `r.mu` and invoking `rs.Snapshot`.

## Alternatives considered

- **Hold `rs.mu` across lookup and `r.mu` in `Join` and `JoinCode`**: prevents `Tick` from deleting the room between lookup and admission without adding a `closed` flag on `Room`. Rejected because holding the manager-wide `rs.mu` while contending on per-room `r.mu` serializes joins across unrelated rooms, and does not protect callers holding a `*Room` reference directly (such as `Room.Start`) from observing a closed room.
- **Check `rs.rooms[r.id] == r` inside `join` under `r.mu`**: would require acquiring `rs.mu` while holding `r.mu`, inverting the `rs.mu -> r.mu` lock order used by `Tick` and `List` and introducing a deadlock.

## Consequences

- **Benefits**: Rooms nobody joins time out and release their IDs and private codes identically to rooms everyone leaves. `Join` and `JoinCode` are race-free against `Tick`: either `Tick` closes the room and every racing join gets `ErrRoomNotFound`, or a join admits a peer and `Tick` keeps the room open. No new exported API surface is added.
- **Costs and known limits**: A caller that sets `EmptyTimeout = 0` and calls `Tick()` between `Create` and the first `Join` will now see the empty room closed immediately, matching the documented `Tick` contract. An unexported `beforeJoin` hook field exists on `Rooms` (nil in production) so internal tests can deterministically pin the lookup-to-lock race window.

## Testing

- `pkg/online/server/rooms_test.go`: `TestNeverJoinedRoomClosesAfterTimeout`, `TestNeverJoinedRoomClosesImmediatelyWithZeroTimeout`, `TestJoinedRoomCancelsIdleCloseAndReemptyingClosesAfterTimeout`, `TestEndedEmptyRoomClosesAfterTimeout`, and `TestConcurrentJoinAndTickNeverStrandsPeer` (concurrent `Join`/`JoinCode`/`Tick`/`List` invariant check under `-race`).
- `pkg/online/server/rooms_internal_test.go`: `TestJoinAfterTickClosedReturnsNotFound` and `TestJoinInterleavedWithTickCloseReturnsNotFound` (deterministic barrier between lookup and `r.mu.Lock` for both `Join` and `JoinCode`).
- Verified with `go tool nsgo test ./pkg/online/server/...`, `go tool nsgo test -env GOFLAGS=-race ./pkg/online/server/...`, `go tool nsgo check -target macos_arm64,linux_x64,windows_x64 -jobs 2 ./pkg/online/server`, `(cd tools && go run ./changelog check)`, and `bun run verify-notes`.
