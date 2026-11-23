### Added

- `game/net`: an authenticated connection handshake on WebSocket and the new `UDPServer`/`UDPClient`: `Auth.Verify` checks the client's token, the server assigns its ID and tells the client (the browser too), a dropped client resumes with the same ID within `Auth.GracePeriod`, `NextEvent` reports joins, drops, resumes and leaves, UDP datagrams are bound to the session's address and key, and `Auth.MaxPeers` (1024 by default) caps the sessions a server holds (#550).

### Changed

- `game/net`: `ListenWebSocket`, `ListenWebSocketTLS` and `ListenWebSocketTLSFiles` take an `Auth` after the server ID, and `DialWebSocket` takes a token instead of a chosen `PeerID`; the client's ID comes from the server's welcome, and the `X-NSGo-Peer-ID` header is gone. `UDPTransport` no longer learns peers from incoming datagrams: both sides register each other with `AddPeer`, and a datagram is accepted only from the address registered for the ID it claims (#550).
