# Agent Note: Bound server room inboxes per member and drain them fairly

Status: implemented

## Problem

Every netplay transport bounds its inbox (`netplay.InboxLimits`), but once a game moved a packet into a `server.Room` with `Deliver`, the bound was gone: `Room.inbox` was an unbounded slice per member, only `Recv` and `Leave` shrank it, `End` kept queued packets, and `Rooms.Tick` closed rooms without clearing them. A match that overran its tick budget kept receiving packets at the transports' rate, so its room's memory grew for as long as it stayed slow, up to the `Guard` rate times the number of members (GHSA-c599-xjg6-jhc4). Reading the inbox was also left to each game: `Recv` is per peer and `Members` returned map order, so a naive drain let one flooding member take the whole tick's budget before the others were read.

## Decision

- `Rooms.SetLimits(RoomLimits{PerPeer, Warning})` sets the limits of every open room and of rooms `Create` opens later. Zero uses `DefaultRoomInboxPerPeer` (256) and `DefaultRoomInboxWarning` (1024); a negative value removes the cap or turns the warning off, matching `netplay.InboxLimits`. The zero-value `Rooms` and `NewRooms` both get the defaults, so the bound is on without any call.
- A member's queue never holds more than `PerPeer` packets. One cap covers both channels, so the bound on memory is `PerPeer` times members regardless of channel mix. A packet arriving at a full queue evicts that member's oldest unreliable packet; with only reliable packets queued, an arriving unreliable packet is dropped and an arriving reliable packet is refused with `ErrRoomInboxFull`, which the packet loop answers by disconnecting the peer (`WebSocketServer.Kick`, `UDPServer.Kick`), as a `WebSocketServer` ends a client past its reliable cap. Reliable packets are never silently dropped.
- `RoomInboxStats` (`Queued`, `Dropped`, `Refused`, `Warnings`, `Backlog`) reports the inbox. The warning counts once per crossing and rearms at half the threshold, like `InboxStats.ReliableWarnings`.
- `Room.Drain(budget, fn)` pops round robin in join order, up to `budget` per member, and calls `fn` after releasing `r.mu` so the callback may call back into the room.
- `Room` keeps an `order` slice beside the `members` map; `Members` returns a copy in join order, and a peer that leaves and rejoins moves to the end.
- `End`, `Leave` (for that member) and `Tick` closing a room discard queued packets. Lowering `PerPeer` with `SetLimits` drops queued unreliable packets past the new cap at once and counts them; queued reliable packets are kept.

## Alternatives considered

- **Separate unreliable and reliable caps, as `netplay.InboxLimits` has**: matches the transports exactly. Rejected because a room inbox sits behind a transport that already enforces those caps; a second pair of knobs adds configuration without changing the memory bound, which is what this layer must guarantee. One `PerPeer` cap is the simplest form that bounds memory.
- **Drop the oldest packet of either channel when full**: never needs an error path. Rejected because dropping a reliable packet breaks `ChannelReliableOrdered` for the game without anyone noticing; refusing and kicking keeps what was delivered in order.
- **Call `fn` inside `Drain` while holding `r.mu`**: avoids collecting a batch. Rejected because a callback that delivers, receives or reads stats on the same room would deadlock.

## Consequences

- **Benefits**: a room's memory is bounded by `PerPeer` times its capacity whatever the match's speed; one member's flood evicts only its own packets; `Drain` gives each member the same share of a tick; operators can see backlog and loss through `InboxStats`.
- **Costs and known limits**: `Deliver` can now return `ErrRoomInboxFull`, which callers that ignored its error must handle to disconnect the peer; until they do, that peer's newer reliable packets are lost while it stays connected. `Members` order changed from map order to join order, which only affects callers that depended on no order. Evicting from the middle of a queue copies up to `PerPeer` entries, bounded and small at the default. The counters are not yet exported as Prometheus metrics.

## Testing

- `pkg/online/server/rooms_inbox_test.go`: `TestRoomInboxStalledStaysBoundedByDefault` (failed before the fix with 2560 packets queued for one member), `TestRoomInboxPerPeerLimitAndStats`, `TestRoomSetLimitsTrimsQueuedUnreliable`, `TestRoomDrainInterleavesFlooder`, `TestRoomMembersKeepJoinOrder`, `TestRoomEndAndCloseClearInbox`.
- Verified with `go tool nsgo test ./pkg/online/server/`, `go tool nsgo test -env GOFLAGS=-race ./pkg/online/server/`, `go tool nsgo check ./pkg/online/server/`, `(cd tools && go run ./changelog check)`, and `bun run verify-notes`.
