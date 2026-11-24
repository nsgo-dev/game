### Added

- `game/net`: `ServerWorld.SetMaxSnapshotSize` and `DefaultMaxSnapshotSize` (60000 bytes). A full snapshot larger than the limit is split into parts that `ClientWorld` reassembles, and a delta larger than it is replaced by a split full snapshot (#546).
- `game/net`: `UDPTransport.Err`, `UDPServer.Err` and `ErrReceiveFailed` report a socket failure that stopped a UDP transport from receiving; `Send` returns the same error from then on (#546).

### Changed

- `game/net`: a `WebSocketServer` now pings each client every third of `Auth.IdleTimeout` (10 seconds by default, at least 3) and drops one that sends nothing for that long, instead of ignoring `IdleTimeout`. A native `WebSocketTransport` pings the server every 2 seconds and moves to `ConnDisconnected` after 10 silent seconds (#546).

### Fixed

- `game/net`: a client whose network vanished without closing its TCP connection no longer keeps its WebSocket session and ID forever, and a native client notices a server that went silent (#546).
- `game/net`: a WebSocket frame's payload is read as it arrives instead of allocated up front from the announced length, and the hello frame is capped, so a slow-loris client cannot reserve 4 MiB per connection (#546).
- `game/net`: `Recv` on every transport pops in constant time; draining n queued packets used to copy n²/2 packets (#546).
- `game/net`: a full snapshot larger than the transport's packet limit no longer leaves a new or resyncing peer without the world forever (#546).
- `game/net`: replication compares ticks in serial-number order, so it keeps working when the `uint32` tick counter wraps, and `Interpolate` works across the wrap (#546).
- `game/net`: the UDP receive loops keep reading after an error about a single datagram, such as an ICMP port unreachable, instead of stopping silently (#546).
- `game/net`: the docs and the `Transport` doc comment say that a transport has a single consumer, and why two consumers on one transport lose packets (#546).
