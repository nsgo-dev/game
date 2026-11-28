# Agent Note: Stateless address cookie before UDP session admission

Status: implemented

## Problem

A `UDPServer` on handshake version 2 admitted a client on its first datagram: any padded hello was decrypted (one X25519 DH), reserved a session slot, ran `Auth.Verify` on the receive goroutine and held the slot until `Auth.IdleTimeout`, without the sender ever showing it could receive at its source address. Padding stopped amplification but not slot exhaustion: a trickle of datagrams with spoofed source addresses kept a server at `Auth.MaxPeers` and drove `Verify` work, and a captured hello replayed from another address was admitted again.

## Decision

UDP sessions use handshake version 3 (`pkg/online/netplay/noise.go`, `pkg/online/netplay/udp_session_native.go`), which is version 2 ([2026-11-27-udp-aead-noise-nk.md](2026-11-27-udp-aead-noise-nk.md)) with one stateless cookie round trip in front of Noise message 1.

- **Cookie request**: the client first sends `hsHello(1) | version(1)=3 | nonce(16) | cookie(16)=0`, zero-padded to `udpMinHello` (128 bytes).
- **Cookie**: the server answers any hello whose cookie it does not accept with `hsCookie(1)=0xA4 | version(1) | nonce(16) | cookie(16)`, 34 bytes, always smaller than the hello. The cookie is the first 16 bytes of `HMAC-SHA256(cookieKey, window(8) | nonce(16) | addr)`, where `cookieKey` is 32 random bytes drawn at listen time, `window` is `Auth.Now` divided by `udpCookieWindow` (5 seconds) and `addr` is the source IP and port with IPv4-mapped addresses unmapped. The server keeps no state per cookie.
- **Hello**: `hsHello | version=3 | nonce | cookie | noiseMsg1`. The Noise prologue is `version | nonce | cookie`, so a cookie spliced into a captured hello breaks message 1's AEAD.
- **Admission order in `handleHello`**: size check, version check (cleartext version rejection for versions 1 and 2), the cached-welcome retransmit path (exact hello hash and address), then the cookie check against the current and previous window (one HMAC per window, `hmac.Equal`), and only then `decodeUDPHelloV2`, `reserve`, `Verify` and `commit`. Resume hellos pass the same check.
- **Client**: `udpHandshake` sends the cookie request, builds message 1 when a cookie for its nonce arrives, and rebuilds it when a different cookie for the same nonce arrives (the old one expired while datagrams were lost). Before it has a cookie it accepts only a cleartext version rejection.
- **Rebuilt hello rebinds**: cookie requests are resent every 250 ms, so a client with a slow first reply can get the cookies of two windows and send a second hello, with a new ephemeral key, after the server has already bound the first. In `admit`, a hello for the nonce of a live session from the same address with a different hello hash rebinds that session to the new keys (same ID, no second `reserve`, `Verify` or `resume`) only when its cookie comes from a later window than the bound hello's and its decrypted resume fields and token digest (`helloBody`) match the bound one. Any other hello for that nonce and address, such as a late copy of the replaced hello, gets no answer, so it cannot rekey the session away from the client. Without this a single dial could join twice (a ghost peer holding a slot until `IdleTimeout`), fail under a single-use `Verify`, or lose its resume to the secret rotation of the first hello.
- **Admitted nonces**: the server remembers each bound hello's nonce for two cookie windows (`admitted`, pruned by age once it holds 64 entries). A hello whose nonce is there but whose session has gone or moved on is ignored, so an exact replay from the client's own address while its cookie is valid cannot join again after a bye, kick or drop. A real client draws a new nonce for every dial and `Reconnect`.

## Alternatives considered

- **Track each accepted token's hash and refuse it from another address while in use** (as netcode's connect tokens do). It also catches a token leaked outside the handshake. Not done here: on version 3 the token is encrypted to the server's static key and a captured hello cannot be moved to another address because the cookie is in the prologue, so the remaining case is a token the game itself leaked, which only the game's `Verify` can judge (single use, expiry), and per-token state would be new state an attacker can grow.
- **Rotate a pair of server secrets instead of putting the window in the MAC input**. Equivalent acceptance (one to two windows); the window in the input needs one key and no rotation timer.
- **Keep version 2 and add the cookie inside it**. Old clients would then time out silently instead of getting the version rejection that tells their user to upgrade.
- **Issue the cookie only after decrypting message 1**. It would let the welcome be sent at once for verified addresses, but every spoofed datagram would still cost a DH.

## Consequences

- **Benefits**: a sender that cannot receive at its source address costs the server one HMAC and a 34-byte reply per datagram, never a DH, a session slot or a `Verify` call; replies stay smaller than requests; a hello replayed from another address does not decrypt.
- **Costs and known limits**: one more round trip per handshake and per `Reconnect`; breaking wire change, so servers and clients must be upgraded together. A hello replayed from its own (spoofed) source address within the cookie's lifetime of at most two windows is still decrypted (one DH), but it only reaches the cached welcome or is ignored; it never joins again. The admitted-nonce map holds at most the nonces bound in the last 10 seconds, each of which cost a `Verify`.

## Verification

`go tool nsgo test ./pkg/online/netplay` runs `TestUDPServerHellosFromSilentSocketsTakeNoSlot`, `TestUDPServerAnswersAHelloWithoutAValidCookieWithACookie`, `TestUDPServerRefusesACookieIssuedToAnotherAddress`, `TestUDPServerRefusesAHelloReplayedFromAnotherAddress`, `TestUDPServerAcceptsACookieForTwoWindowsAtMost`, `TestUDPClientConnectsDuringAFloodOfHellosWithoutCookies`, `TestUDPServerRebindsAHelloRebuiltForANewerCookie`, `TestUDPServerRebindsAResumeHelloRebuiltForANewerCookie`, `TestUDPServerIgnoresAnAdmittedHelloReplayedAfterItsSessionLeft`, `TestUDPCookieMessages` and `FuzzDecodeUDPCookie`.
