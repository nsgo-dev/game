### Changed

- `game/online/netplay`: breaking: `UDPServer` and `UDPClient` now use handshake version 2 (`Noise_NK_25519_ChaChaPoly_SHA256`) and per-datagram ChaCha20-Poly1305 AEAD with directional keys, a 26-byte framing overhead (10-byte associated-data header plus 16-byte tag), an implicit `direction || peer || seq` nonce, and sequence-wrap refusal (`ErrSequenceExhausted`; call `Reconnect` to rekey). `DialUDP` now takes the server's static X25519 `*ecdh.PublicKey` before the token, and version-1 UDP hellos receive the existing version rejection (#1299).

### Added

- `game/online/netplay`: `Auth.StaticKey` and `UDPServer.PublicKey` configure and expose a UDP server's static X25519 key for the Noise NK handshake, `UDPTransport.AddPeerKey` adds optional per-peer pre-shared-key ChaCha20-Poly1305 AEAD framing, and `ErrInvalidKey` and `ErrSequenceExhausted` report invalid keys and exhausted 32-bit sequence counters (#1299).
