### Fixed

- `pkg/online/netplay`: the WebTransport test that checks unreliable datagrams are not held behind a lost reliable packet holds back QUIC's retransmission until the datagram arrives, so it no longer fails on loaded runners (#1916).
