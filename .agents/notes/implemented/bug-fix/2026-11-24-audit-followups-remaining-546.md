# Agent Note: Remaining net audit follow-ups for dynamic session peers, wraparound, and bounded inbox backpressure

Status: implemented

## Problem

After the earlier #546 follow-up commits landed on `main` (WebSocket keepalive and incremental frame reads, linear `packetQueue` drain, split snapshots, `ClientWorld.Reset`, replication tick wraparound, UDP socket error reporting, plus the `physics3d`, `assets`, and `modding` audit fixes), three `game/net` findings from issue #546 remained open:

1. **Frozen peer sets in lockstep and rollback sessions (`packages/game/net/rollback.go`)**: `LockstepSession` and `RollbackSession` snapshotted `peers` at construction, preventing late joiners from entering and leaving sessions stalled when a peer disconnected.
2. **Tick wraparound in lockstep and rollback sessions (`packages/game/net/rollback.go`)**: while `replication.go` switched to serial-number tick comparisons, `LockstepSession` and `RollbackSession` still used raw `<`/`>` and `int64(tick)` comparisons that break across `uint32` wraparound (`0xFFFFFFFF -> 0`).
3. **Unbounded inboxes, O(n^2) peer-event drain, missing WebSocket backpressure, and RFC 6455 control-frame validation (`transport.go`, `session.go`, `ws_native.go`, `udp_native.go`, `udp_session_native.go`, `loopback.go`, `relay.go`, `ws_js.go`)**:
   - `packetQueue` drained linearly but still grew without limit on unpolled consumers or flooding peers.
   - `sessionTable.events` still used `slices.Delete(t.events, 0, 1)` (unbounded and O(n^2)).
   - Native `WebSocketTransport` and `WebSocketServer` read frames into memory indefinitely with no TCP backpressure.
   - `readWSFrameLimit` did not reject fragmented (`FIN=0`) or oversized (`> 125` bytes) RFC 6455 control frames.

## Decision

- **Dynamic peer membership (`rollback.go`)**: Added `Peers() []PeerID`, `AddPeer(id PeerID)`, and `RemovePeer(id PeerID)` to both `LockstepSession` and `RollbackSession`, keeping peer lists deterministically sorted, pre-confirming zero inputs during `InputDelay` when `stepsExecuted == 0`, and advancing `RollbackSession` confirmation immediately when a departed peer is removed.
- **Modular tick arithmetic (`replication.go`, `rollback.go`)**: Added `tickDelta(a, b uint32) int32` and `tickBefore(a, b uint32) bool` alongside `tickAfter`, and updated all tick comparisons, rollback depth calculations, checksum checks, and history pruning in `LockstepSession` and `RollbackSession` (tracked with `stepsExecuted` so pruning works past wraparound).
- **Bounded unreliable queues and WebSocket TCP backpressure (`transport.go`, `session.go`, `ws_native.go`, `udp_*.go`, `loopback.go`, `relay.go`, `ws_js.go`)**:
  - Extended `packetQueue` with `pushBounded(p Packet, maxLen int) bool` (`defaultMaxInboxPackets = 4096`) and added an O(1) bounded `eventQueue` with matching compaction and post-burst shrink for `sessionTable.events`.
  - Applied `pushBounded(..., defaultMaxInboxPackets)` to `UDPTransport`, `UDPServer`, `UDPClient`, `sessionTable.events`, and `ChannelUnreliable` deliveries on `LoopbackTransport`, `RelayTransport`, and browser `WebSocketTransport`, while leaving `ChannelReliableOrdered` lossless (note `RelayTransport` already caps reliable in-flight messages at `relayWindow = 1024`).
  - Added a lazily initialized `sync.Cond` (`inboxCond`) on native `WebSocketTransport` and `WebSocketServer` so `deliverBinary` and `deliverFromClient` pause once `inbox.len() >= wsInboxLimit()` (`4096`), applying TCP flow-control backpressure without dropping reliable frames, and wake on `Recv`, `Reconnect`, `Close`, resume replacement, or client disconnect.
  - Enforced RFC 6455 §5.5 in `readWSFrameLimit` by rejecting control frames (`opcode >= 0x8`) with `!fin` or `plen > 125` with `ErrInvalidPacket`.

## Alternatives considered

### Evicting oldest packets on WebSocket inbox overflow instead of `sync.Cond` backpressure

Using `pushBounded` uniformly on `WebSocketTransport` and `WebSocketServer` would avoid adding a condition variable. It was rejected because WebSocket transports carry `ChannelReliableOrdered` traffic (such as lockstep and rollback inputs): silently dropping an older packet would violate reliable-ordered semantics and cause silent desyncs, whereas pausing socket reads lets TCP flow control throttle the peer losslessly.

### Bounding `LoopbackTransport` and `RelayTransport` reliable channels with drop-oldest

Evicting unread `ChannelReliableOrdered` packets in `LoopbackTransport`, `RelayTransport`, or `ws_js.go` would likewise violate `ChannelReliableOrdered`'s documented guarantee ("never dropped and always arrive in send order"). Applying drop-oldest only to `ChannelUnreliable` preserves reliable delivery everywhere while bounding unreliable floods; `RelayTransport` already bounds reliable in-flight packets via `ErrRelayBacklog`.

## Consequences

- **Benefits**:
  - Closes the remaining `game/net` audit follow-ups from #546 without overlapping or conflicting with the `net`, `physics3d`, `assets`, and `modding` fixes already merged on `main`.
  - Lockstep and rollback sessions handle late joiners, departed peers, and multi-year `uint32` tick wraparound cleanly.
  - Unreliable inboxes and server peer-event queues cannot OOM under floods, and native WebSocket transports apply real TCP backpressure without losing reliable packets.
- **Costs & limits**:
  - Native `WebSocketServer` and `WebSocketTransport` pause reading once 4096 unread packets accumulate; callers must still drain `Recv` periodically so TCP flow control does not stall the peer past `wsWriteTimeout`.
  - Callers adding a late joiner mid-match to `LockstepSession` or `RollbackSession` must still synchronize the joiner's `SimState` via `Save`/`Load` at the matching tick.

## Verification

- `git ls-files -z -- '*.go' | xargs -0 -r gofmt -l`
- `(cd packages/game && env -u GOROOT go run ./cmd/gameschema -check)`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./physics3d ./assets ./modding ./net)`
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestFormatPackagesRunConformance|TestFormatsPage|TestStabilityTable')`
- `env -u GOROOT go run ./tools/changelog check`
- `bun run verify-notes`
