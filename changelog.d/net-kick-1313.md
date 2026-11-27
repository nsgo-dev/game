### Added

- `netplay`: `WebSocketServer.Kick(peer, reason)` and `UDPServer.Kick(peer, reason)` end one peer's session for good; `PeerEvent.Reason` carries the reason, and the client learns it as a `*KickError` (matching `ErrKicked`) from `Err` — over a WebSocket close frame with code 4001, over an encrypted kick datagram on UDP, and in the browser too (#1313).
