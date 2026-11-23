### Added

- `game/net`: a `Relay` that forwards peer-to-peer lockstep and rollback sessions between players who cannot reach each other, over WebSocket or UDP. Each player connects with a `RelayTransport`; `RelayConfig.Join` puts a player who passed the connection handshake into a session, frames go only between members of the same session, and `ChannelReliableOrdered` is reliable end to end with acknowledgements and resends, so rollback runs over UDP too. The latency a local relay adds and the decision to defer WebRTC data channels are documented in the server guide (#553).
