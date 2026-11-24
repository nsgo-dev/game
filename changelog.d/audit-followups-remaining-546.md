### Added

- `game/net`: added `Peers`, `AddPeer`, and `RemovePeer` to `LockstepSession` and `RollbackSession` so late joiners can enter an active session and departed peers can be removed without stalling (#546).

### Fixed

- `game/net`: compared `uint32` ticks in serial-number order across wraparound in `LockstepSession` and `RollbackSession` (#546).
- `game/net`: capped unreliable transport inboxes (`UDPTransport`, `UDPServer`, `UDPClient`, and `ChannelUnreliable` on `LoopbackTransport`, `RelayTransport`, and browser `WebSocketTransport`) and server peer-event queues at 4096 unread entries with O(1) drain, added `sync.Cond`-backed TCP backpressure at 4096 unread packets on native `WebSocketTransport` and `WebSocketServer` without dropping reliable frames, and enforced RFC 6455 control-frame length and `FIN` limits (#546).
