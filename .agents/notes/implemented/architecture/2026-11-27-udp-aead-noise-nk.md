# Agent Note: Authenticated encryption and Noise NK handshake for UDP sessions

Status: implemented

## Problem

`UDPServer` and `UDPClient` previously sent handshakes and game datagrams in cleartext, authenticating datagrams only with an 8-byte truncated HMAC-SHA256 tag whose 32-byte key crossed the network in the unencrypted welcome (`pkg/online/netplay/session.go`, `pkg/online/netplay/udp_session_native.go`). Any on-path observer on shared Wi-Fi or mobile networks could read client tokens, resume secrets, and hidden game state (such as fog of war or card hands), forge datagrams, or hijack sessions. `UDPTransport` (`ListenUDP` + `AddPeer`) had no cryptography at all and could be spoofed off-path, and `RelayTransport` over UDP inherited the unencrypted `UDPClient` link.

## Decision

UDP sessions (`UDPServer` / `UDPClient`) use handshake version 2 with a pure-Go `Noise_NK_25519_ChaChaPoly_SHA256` key exchange (`pkg/online/netplay/noise.go`) and per-datagram ChaCha20-Poly1305 AEAD (`golang.org/x/crypto/chacha20poly1305`), while `UDPTransport` adds an optional per-peer pre-shared-key method (`AddPeerKey`) sharing the same AEAD framing.

### Handshake version 2 wire contract

Handshake version 3 keeps this contract and adds a stateless address cookie in front of message 1 and in the prologue ([2026-11-28-udp-hello-address-cookie.md](2026-11-28-udp-hello-address-cookie.md)); a version 3 server sends the version rejection to version 2 hellos too.

- **Key distribution**: `Auth.StaticKey` (`*ecdh.PrivateKey`, X25519) configures a persistent server static key; when nil, `ListenUDPServer` generates a fresh X25519 key pair exposed via `UDPServer.PublicKey()`. `DialUDP(addr, serverKey, token)` requires the server's X25519 `*ecdh.PublicKey`, authenticating the server without certificates. Non-X25519 or nil keys fail with `ErrInvalidKey`.
- **Prologue and transcript**: Every handshake datagram carries the 18-byte cleartext prefix `type(1) | version(1)=2 | nonce(16)`. Bytes `version(1) || nonce(16)` are fed to Noise as the prologue, binding both fields into the handshake hash.
- **Message 1 (`-> e, es`)**: Client hello encrypts `resumePeer(4) | resumeSecret(16) | tokenLen(2) | token | zeroPadding` inside Noise message 1, padded to at least `udpMinHello` (128 bytes) so replies never amplify spoofed requests and short tokens are length-padded.
- **Message 2 (`<- e, ee`)**: Server welcome (92 bytes `<= 128`) encrypts `hsWelcome(1) | peer(4) | server(4) | resumed(1) | secret(16)` with no packet key; post-authentication rejections encrypt `hsReject(1) | reasonLen(2) | reason` truncated to the hello length. Client `readMessage2` validates on a value copy of the symmetric state so forged packets cannot poison an in-flight handshake.
- **Version 1 compatibility**: Any UDP hello whose version byte is not 2 (including version 1) receives the existing cleartext `hsReasonVersion` rejection, never longer than the hello. WebSocket (`ws_native.go`, `ws_js.go`) remains on handshake version 1 over TLS (`wss://`).

### Per-datagram AEAD wire contract

- **Framing (26-byte overhead)**: `type(1) | peer(4) | seq(4) | channel(1) | ciphertext(N) | tag(16)`. The 10-byte cleartext header is authenticated as AEAD associated data.
- **Keys and implicit nonce**: Noise `Split()` yields distinct client-to-server (`c2s`) and server-to-client (`s2c`) 32-byte keys. The 12-byte nonce `direction(4) || peer(4) || seq(4)` (big-endian) is implicit and never transmitted (`direction = 1` for client-to-server, `2` for server-to-client). For `UDPTransport.AddPeerKey(id, addr, key)`, datagrams use `type = 0xB6` and `nonce = uint32(destPeerID) || uint32(srcPeerID) || seq`, preventing reflection and nonce reuse even if the same pre-shared key is used across peers.
- **Replay and sequence-wrap refusal**: Receivers check `replayWindow.wouldAccept(seq)` read-only before running AEAD open, and mutate the 64-entry replay window only after authentication succeeds. Both `Send` and `replayWindow` refuse to wrap the `uint32` sequence counter (`ErrSequenceExhausted` once `sendSeq == math.MaxUint32`); calling `UDPClient.Reconnect()` runs a fresh Noise NK handshake that rekeys both directions and resets sequence counters.
- **Zero-allocation hot path**: `udpCipher` holds the `cipher.AEAD` and its 12-byte scratch nonce array inside the heap-resident peer/link struct so passing the nonce slice across the `cipher.AEAD` interface does not escape on every packet. `TestUDPCipherSealOpenZeroAllocs` guards both `seal` and `open` with `debug.AssertZeroAllocs`.

## Alternatives considered

- **HMAC-SHA256 only (keeping the existing framing)**: Adds zero handshake complexity and keeps the 18-byte header, but leaves every token, resume secret, session key, and game payload in plaintext on shared Wi-Fi and mobile networks, forcing security-sensitive games off UDP onto `wss://`. Rejected because confidentiality and real key exchange are the core requirements.
- **DTLS 1.2/1.3**: Standardized and interoperable with WebRTC stacks, with built-in epoch rekeying and certificate authentication. Rejected because Go's standard library has no DTLS implementation, adding a full DTLS stack pulls in a large third-party dependency, requires certificate management and multi-RTT handshakes with fragmentation/cookie state machines, and is heavier than needed when clients already receive server endpoints from matchmaking or config.
- **AES-256-GCM**: Hardware-accelerated via AES-NI on x86-64 and ARMv8 Crypto Extensions, with broad FIPS ecosystem support. Rejected because ChaCha20-Poly1305 (`golang.org/x/crypto/chacha20poly1305`, already a direct dependency in `go.mod`) runs in constant time without hardware AES support on low-end mobile devices and WebAssembly, pairs directly with Noise `ChaChaPoly`, and outperforms the previous HMAC-SHA256 path at 0 allocations per packet.
- **Adding `github.com/flynn/noise` to `go.mod`**: Battle-tested Noise library with many patterns. Rejected to avoid adding a new third-party dependency without maintainer approval when `UDPServer`/`UDPClient` only need the single 2-message `Noise_NK_25519_ChaChaPoly_SHA256` pattern, which fits cleanly in ~180 lines of pure Go over stdlib `crypto/ecdh`, `crypto/sha256`, `crypto/hmac`, and existing `golang.org/x/crypto/chacha20poly1305`, verified step-by-step against Noise Revision 34 equations.

## Consequences

- **Benefits**: UDP handshakes authenticate the server without certificates and encrypt tokens, resume secrets, welcomes, and rejections; every UDP session datagram and `UDPTransport.AddPeerKey` datagram is encrypted and authenticated with zero heap allocations on the seal/open hot path and higher throughput than the old HMAC-SHA256 path; reflection, tampering, replay, wrong server keys, and nonce-wrapping sequence exhaustion are all rejected; `RelayTransport` over UDP inherits encrypted client-to-relay links automatically.
- **Costs**: Breaking wire change (UDP handshake version 2) and breaking Go API change on `DialUDP` (now requires `serverKey *ecdh.PublicKey`). Per-datagram overhead grows by 8 bytes (from 18 to 26 bytes). Servers must distribute their static X25519 public key out of band (via matchmaking tickets or configuration) and set `Auth.StaticKey` if clients must reconnect across server restarts. End-to-end encryption between `RelayTransport` peers remains out of scope.
