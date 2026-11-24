### Changed

- `game/net`: every transport caps each peer's `ChannelUnreliable` packets in its inbox (`DefaultUnreliableInbox`, 4096) and drops that peer's oldest when a newer one arrives, so one flooding peer cannot evict another's packets. `WebSocketServer` and `RelayTransport` also cap each peer's `ChannelReliableOrdered` packets (`DefaultReliableInbox`, 16384): a WebSocket client past the cap has its session ended, and a relay peer past it is no longer acknowledged until `Recv` makes room. Every transport counts a warning once each time its reliable backlog passes `DefaultReliableInboxWarning`. `SetInboxLimits` and `InboxStats` on every transport (the new `InboxReporter` interface), and `LoopbackConfig.Inbox` and `RelayTransportConfig.Inbox`, set the limits and read the drop, refusal and warning counters (#546).

### Fixed

- `game/net`: `UDPTransport` drops datagrams that claim `ChannelReliableOrdered`, which it never sends, instead of queueing them without a limit (#546).
