# Agent Note: Server-side Kick on the WebSocket and UDP session servers

Status: implemented

## Problem

A server could not drop one peer. `WebSocketServer` and `UDPServer` ended a session only internally (a client past its reliable inbox cap), and a client could only say goodbye itself (`udpBye`). `server.Guard` with `LimitDisconnect` only flipped its own flag and returned `GuardDisconnect`: the transport session lived on, kept its `Auth.MaxPeers` slot, and every packet still cost a `Check`. `PeerEvent` carried no reason, so a server could not say why a peer left and a client had no way to learn it was removed rather than disconnected.

## Decision

Add `Kick(peer PeerID, reason string) error` to both session servers, and a `Reason` field on `PeerEvent` that is set on the `PeerLeft` a kick produces and empty otherwise. The session is removed from the table immediately (`sessionTable.leaveReason`), so its resume secret is gone and a `Reconnect` joins as a new peer only if `Auth.Verify` accepts the token again. Both transports deliver the reason to a connected client in a structured way, and the client reports it as a `*KickError` (matching `ErrKicked`) from `Err`:

- **WebSocket**: the server writes a close frame with the new application close code `wsCloseKicked = 4001` (next to `wsCloseReconnect = 4000`), followed by the reason as the close frame's text. The payload is a 2-byte code plus the reason truncated with `truncateReason` to fit the 125-byte control-frame limit, cutting on a rune boundary. The native client's read loop parses the close payload; the browser client reads `code` and `reason` from the close event.
- **UDP**: a new sealed server-to-client datagram type `udpKick = 0xB7` (next to `udpPeerData = 0xB6`) whose payload is the reason, sent `udpKickCopies = 3` times since UDP may drop one. Each copy uses a fresh sequence number so the client's 64-entry replay window does not refuse the retransmissions; the client exits its read loop after the first copy. A client that receives none of them still learns from its idle timeout, as before.

`Kick` truncates the reason to `hsMaxReason` (256 bytes, the same cap as a handshake rejection) before storing and sending it, returns `ErrUnknownPeer` for an ID with no session (including a handshake still in `Verify`), and returns `ErrClosed` once the server is closed. A kick of a peer inside its grace period removes the session without a message: there is no link to send on, and the client's next handshake simply cannot resume. This complements the UDP AEAD framing in [UDP AEAD and Noise NK](../architecture/2026-11-27-udp-aead-noise-nk.md) and the wire schema in [net wire schema](../architecture/2026-11-27-net-wire-schema.md): the new datagram type reuses the existing sealed framing, so it needs no handshake or datagram version bump, and old clients ignore it silently and time out as before.

`pkg/online/server`'s `Guard` docs show acting on `GuardDisconnect` by calling `Kick`, closing the loop that made `LimitDisconnect` only a flag flip.

## Alternatives considered

- **Kick only via `Guard` in `pkg/online/server`, with no transport API** — Keeps `netplay` unchanged, but the guard is transport-agnostic and has no handle on the session, so the peer would stay connected while its packets are refused, exactly the behavior the issue calls out. Rejected because ending the session is a transport operation.
- **A new WebSocket binary message type carrying the reason instead of the close frame's payload** — A browser cannot receive further frames once the socket closes, and RFC 6455 close frames already carry an application code plus a short reason text, so the close payload alone is sufficient and costs no new frame type. The datagram-type approach on UDP mirrors it within the existing sealed framing.
- **Bumping the UDP handshake or datagram version for the new type** — The sealed format (`type(1) peer(4) seq(4) channel(1) ciphertext tag(16)`) is unchanged; only a new type value is added, and receivers already drop unknown types. A version bump would force old and new peers apart for no layout change.

## Consequences

- **Benefits**: bans, cheat detection, guard refusals, lobby removals and ordered shutdowns can end one session with a reason both sides can read; the freed slot is immediate; kicked clients cannot silently resume under their old ID.
- **Costs and known limits**: `PeerEvent` gains a field, which breaks positional struct literals in downstream code that copies the test style. A UDP kick is best-effort by nature (three copies, then the idle timeout as the fallback), and a kick during the grace period reaches the client only implicitly, as a failed resume.
- The same change repairs the stale `udp_sealed.wire.yaml` spec and `FuzzWireUDPSealedDatagram`, which still described and compared against the pre-AEAD HMAC framing after [UDP AEAD and Noise NK](../architecture/2026-11-27-udp-aead-noise-nk.md) landed and broke the package's build on main.
