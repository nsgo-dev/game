### Security

- `game/online/netplay`: a `UDPServer` now admits a client only after it echoes a stateless cookie sent to its source address, so hellos from spoofed or silent addresses no longer take session slots or run `Auth.Verify`, a captured hello replayed from another address is refused, and a hello replayed from the client's own address cannot join a second time. This raises the UDP handshake to version 3: upgrade servers and clients together, since older clients now get a version rejection; `DialUDP` and `Reconnect` handle the extra round trip themselves (GHSA-6qf7-v93j-cwg6).
