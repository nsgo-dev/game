### Added

- `game/online/netplay`: WebTransport transport (`WebTransportServer`, `WebTransportTransport`, `ListenWebTransport`, `ListenWebTransportFiles`, `DialWebTransport`, `WebTransportSupported`, `DialWebTransportWithFallback`, `WebTransportOptions`, `ErrWebTransportUnavailable`) delivering `ChannelUnreliable` as independent QUIC datagrams without TCP head-of-line blocking and `ChannelReliableOrdered` over a bidirectional QUIC stream, with a browser `syscall/js` client, `RelayServer` support, certificate hash pinning for local development, and WebSocket fallback (#1300).
