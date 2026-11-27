### Fixed

- `game/pkg/online/netplay`, `game/pkg/online/netplay/wire`: align `UDPSealedDatagram` wire schema, generated codec, and differential fuzz test with ChaCha20-Poly1305 AEAD datagram framing (`UDPDatagramTypePeerData` `0xB6`, 10-byte cleartext header + `sealed` ciphertext and 16-byte Poly1305 tag).
