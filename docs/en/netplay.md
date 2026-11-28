# netplay

`game/online/netplay` provides cross-target multiplayer networking for NSGo games: a unified `Transport` across WebTransport (HTTP/3 QUIC, native + WebAssembly), WebSocket (native + WebAssembly), native UDP, and simulated loopback; server-authoritative delta snapshot replication with client prediction and interpolation; and deterministic lockstep and rollback netcode.

```go
import "github.com/nsgo-dev/game/pkg/online/netplay"
```

## Cross-target transports (`Transport`)

Every transport implements the same non-blocking `Transport` interface (`Send`, `Recv`, `LocalID`, `Peers`, `Close`) with `ChannelReliableOrdered` and `ChannelUnreliable` channels and `PeerBroadcast` (`PeerID(0)`):

| Transport | Constructor | Targets & use case |
| --- | --- | --- |
| `WebTransportServer` / `WebTransportTransport` | `ListenWebTransport`, `ListenWebTransportFiles`, `DialWebTransport` | Native + browser (`js/wasm`); sends `ChannelUnreliable` as QUIC datagrams (no TCP head-of-line blocking) and `ChannelReliableOrdered` over one bidirectional QUIC stream, encrypted with TLS 1.3. |
| `WebSocketServer` / `WebSocketTransport` | `ListenWebSocket`, `ListenWebSocketTLS`, `DialWebSocket` | Native + browser (`js/wasm`); works across NATs, TCP-only networks, and older browsers. Both channels share one ordered TCP stream. Clients authenticate with a token and the server assigns their IDs (see below). |
| `UDPServer` / `UDPClient` | `ListenUDPServer`, `DialUDP` | Native targets (desktop/mobile) for low-latency encrypted datagrams to a server, using a Noise NK key exchange (handshake version 2) and ChaCha20-Poly1305 AEAD. |
| `UDPTransport` | `ListenUDP` + `AddPeerKey` / `AddPeer` | Native peer-to-peer datagrams between endpoints that know each other's addresses: `AddPeerKey` authenticates and encrypts with a pre-shared key; plain `AddPeer` is unauthenticated for LAN use and tests. |
| `Relay` / `RelayTransport` | `NewRelay`, `NewRelayTransport` | Peer-to-peer sessions between players who cannot reach each other: each connects to a relay over WebTransport, WebSocket, or UDP, and the relay forwards between members of the same session (see [the server guide](gameserver.md#peer-to-peer-relay)). |
| `LoopbackHub` / `LoopbackTransport` | `NewLoopbackHub` + `AddPeer` | In-memory deterministic tests with configurable `Latency`, `Jitter`, `LossRate`, and `Seed`. |

UDP has no retransmission or ordering, so both UDP transports refuse `ChannelReliableOrdered` with `ErrUnsupportedChannel`; lockstep and rollback need a WebTransport, WebSocket, or loopback transport, or a `RelayTransport`, which retransmits end to end and so runs them over UDP too.

`DialWebSocket` speaks netplay's own handshake and framing, so it connects only to a `WebSocketServer`. To talk to a WebSocket server that is not part of this repository — a third-party backend, a chat service, a custom protocol — use [wsclient](wsclient.md) instead.

A transport has a single consumer. `Recv` hands each packet to one caller, and `ServerWorld`, `ClientWorld`, the lockstep and rollback sessions, `Relay` and `RelayTransport` all drain every waiting packet and discard the ones that are not theirs, so two of them on one transport steal each other's packets. Give each consumer its own transport, or put a demultiplexer in front of them that implements `Transport` for each. `Recv` costs the same however many packets are queued, so draining a burst stays linear.

If the socket under a `UDPTransport` or `UDPServer` fails, `Err` returns `ErrReceiveFailed` wrapping the socket's error and `Send` returns it too; close the transport and listen again; after `Close`, `Err` returns nil. Errors about a single datagram, such as an ICMP port unreachable for an earlier send, do not stop it. A `UDPClient` likewise keeps reading after an ICMP error, for example while its server restarts, and leaves it to its idle timeout to decide whether the server is gone.

```go
hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{
	Latency:  40 * time.Millisecond,
	Jitter:   10 * time.Millisecond,
	LossRate: 0.02,
	Seed:     7,
})
serverTx := hub.AddPeer(1)
clientTx := hub.AddPeer(2)
_ = serverTx.Send(2, netplay.ChannelReliableOrdered, []byte("hello"))
hub.Advance(60 * time.Millisecond)
pkt, ok := clientTx.Recv()
_, _ = pkt, ok
```

### WebTransport (unreliable browser transport)

Over WebSocket, `ChannelUnreliable` still rides on TCP, so a single lost packet stalls every newer snapshot or rollback input behind it until TCP retransmits. `WebTransportServer` and `WebTransportTransport` run over HTTP/3 and QUIC:

- `ChannelUnreliable` packets travel as independent QUIC datagrams (RFC 9221 / RFC 9297), so a lost packet never blocks newer unreliable updates and stale datagrams are not retransmitted. Each unreliable payload must fit inside the QUIC datagram MTU (typically ~1100–1200 bytes; `Send` returns `ErrPayloadTooLarge` past it). When replicating over WebTransport, keep `ServerWorld.SetMaxSnapshotSize` around 1000 bytes.
- `ChannelReliableOrdered` packets and the connection handshake travel on one client-opened bidirectional QUIC stream using 4-byte big-endian length-prefixed frames (up to 4 MiB each). Both channels work side by side without a relay, and `WebTransportServer` implements `RelayServer` so a `Relay` can serve browser players over QUIC datagrams too.
- On `js/wasm`, `WebTransportTransport` calls the browser's `WebTransport` API directly through `syscall/js` and does not link `quic-go` or `webtransport-go` into the WebAssembly binary.

#### Browser support

WebTransport reached Baseline Newly Available across all major engines in March 2026:

| Browser | Minimum version |
| --- | --- |
| Chrome / Chromium | 97+ |
| Edge | 98+ |
| Firefox | 114+ |
| Safari / iOS Safari | 26.4+ |

Older Safari releases, embedded WebViews, and corporate networks that block UDP port 443 still need WebSocket as a fallback.

#### Certificates and local development

WebTransport always encrypts with TLS 1.3 (`https://`), so the handshake token and resume secret are protected in transit.

- **Production**: pass a CA-signed certificate (for example from Let's Encrypt) to `ListenWebTransport` or `ListenWebTransportFiles`, and leave `WebTransportOptions.ServerCertificateHashes` empty.
- **Local development**: browsers accept self-signed certificates when `WebTransportOptions.ServerCertificateHashes` carries the 32-byte SHA-256 digest (`sha256.Sum256(leafCertDER)`) of the server's leaf X.509 certificate. Per the W3C WebTransport specification, self-signed pinned certificates must use an ECDSA (such as P-256) or Ed25519 key (or RSA) and have a validity period of at most 14 days.

```go
srv, err := netplay.ListenWebTransportFiles(":4433", 1, auth, "cert.pem", "key.pem",
	[]string{"https://play.example.com"})
```

```go
// Local dev with a pinned self-signed cert hash (leave ServerCertificateHashes empty in production):
cli, err := netplay.DialWebTransport("https://localhost:4433/game", token, netplay.WebTransportOptions{
	ServerCertificateHashes: [][]byte{certSHA256[:]},
})
```

#### Fallback to WebSocket

Use `WebTransportSupported` or `DialWebTransportWithFallback` to feature-detect WebTransport and fall back to WebSocket automatically:

```go
tx, err := netplay.DialWebTransportWithFallback(
	"https://play.example.com:4433/game",
	"wss://play.example.com/game",
	token,
	netplay.WebTransportOptions{},
)
```

- When `WebTransportSupported()` is false (such as an older browser), `DialWebTransportWithFallback` dials `wsURL` with `DialWebSocket` immediately.
- On native targets, a connection failure (other than `ErrRejected` or `ErrTokenTooLarge`) falls back to `DialWebSocket`.
- In browsers, `DialWebTransport` returns immediately with `State() == ConnConnecting`. If UDP/443 is blocked by a firewall, the session moves to `ConnDisconnected` with `LocalID() == 0` and an error that does not match `ErrRejected`; close it and call `DialWebSocket(wsURL, token)` as a runtime fallback.

### Inbox limits

A transport keeps each packet that arrives until `Recv` takes it, and bounds what it keeps by channel and by peer. Each peer's `ChannelUnreliable` packets are capped at `DefaultUnreliableInbox` (4096): when one arrives from a peer that already has that many queued, that peer's oldest queued unreliable packet is dropped, so a game that falls behind keeps the newest state, a peer that floods a server evicts only its own packets, and `InboxStats().UnreliableDropped` counts the loss.

`ChannelReliableOrdered` packets are never dropped, since that would break the channel's guarantee. A transport whose peers cannot be trusted caps each peer's reliable packets at `DefaultReliableInbox` (16384) instead and refuses the one that goes past it, counting it in `InboxStats().ReliableRefused`: a `WebSocketServer` or `WebTransportServer` ends that client's session, as if it had left, so the packets it did deliver stay in order, and a `RelayTransport` stops acknowledging that peer, which resends its messages once `Recv` has made room. The other transports receive reliable packets only from their server or from the same process and keep them all. Every transport also counts `InboxStats().ReliableWarnings` once each time its whole reliable backlog passes `DefaultReliableInboxWarning` (4096), and `ReliableBacklog` stays true until the backlog falls to half of it, so a queue hovering at the threshold does not warn on every packet. `Recv` still returns packets in arrival order across both channels and every peer. `UDPTransport` never sends reliable packets, so it drops a datagram that claims to be one.

Every transport implements `InboxReporter`: `SetInboxLimits` changes the limits at any time (lowering the unreliable cap drops each peer's oldest unreliable packets at once), and `InboxStats` reads the counters, which only grow, so a game or a debug overlay compares them with the previous frame's. `LoopbackConfig.Inbox` sets the limits of the peers a hub adds, and `RelayTransportConfig.Inbox` those of a `RelayTransport`. In `InboxLimits`, zero uses the default and a negative value removes the cap or turns the warning off. A `RelayTransport`'s inner transport has its own limits, and every relayed peer arrives on it from the relay, so they share its per-peer cap; a reliable message it drops there is sent again.

```go
tx.SetInboxLimits(netplay.InboxLimits{Unreliable: 512, ReliableWarning: 1024})

var lastWarnings, lastDropped uint64

// Each frame, after draining Recv:
st := tx.InboxStats()
if st.ReliableWarnings != lastWarnings {
	log.Printf("reliable backlog of %d packets", st.Reliable)
}
if st.UnreliableDropped != lastDropped {
	log.Printf("dropped %d stale unreliable packets", st.UnreliableDropped-lastDropped)
}
lastWarnings, lastDropped = st.ReliableWarnings, st.UnreliableDropped
```

## Connection handshake and reconnect

`WebSocketServer`, `WebTransportServer`, and `UDPServer` admit a client only after a handshake: the client presents an app-defined token, `Auth.Verify` checks it, and the server assigns the client's `PeerID` and sends it back, or refuses with a reason. A refused client never reaches `Recv` or `Peers`.

```go
auth := netplay.Auth{
	// Check a signed token locally; the error text is sent to the client.
	Verify: func(peer netplay.PeerID, token []byte) error {
		if !validToken(token) {
			return errors.New("invalid token")
		}
		return nil
	},
	GracePeriod: 15 * time.Second, // a dropped client keeps its ID this long
}
srv, _ := netplay.ListenWebSocket(":8080", 1, auth) // or netplay.ListenUDPServer(":7777", 1, auth)

// Each server tick:
for ev, ok := srv.NextEvent(); ok; ev, ok = srv.NextEvent() {
	switch ev.Kind {
	case netplay.PeerJoined: // new ID
	case netplay.PeerDropped: // connection lost; ID and state kept for GracePeriod
	case netplay.PeerResumed: // same ID back within GracePeriod
	case netplay.PeerLeft: // closed, GracePeriod ran out, or Kick removed it (then ev.Reason says why)
		rooms.Leave(roomOf(ev.Peer), ev.Peer)
		guard.Reset(ev.Peer)
	}
}
```

```go
cli, err := netplay.DialWebSocket("wss://play.example.com/game", token) // or netplay.DialUDP(addr, serverPubKey, token)
if errors.Is(err, netplay.ErrRejected) {
	// err is a *netplay.RejectError carrying the server's reason
}
me := cli.LocalID() // assigned by the server

// After a network drop (State() == netplay.ConnDisconnected):
_ = cli.Reconnect() // cli.Resumed() reports whether it kept its ID
```

- `NextEvent` checks grace periods and idle timeouts when it is called, so a server polls it every tick alongside `Recv`. Queued lifecycle events (`PeerJoined`, `PeerDropped`, `PeerResumed`, `PeerLeft`) drain in FIFO order in constant amortized time per event without silently dropping any. While a peer is inside its grace period, `Send` to it returns `ErrDisconnected` and `Peers` leaves it out.
- `Close` on a client is a goodbye: the server reports `PeerLeft` at once instead of holding the session. `Reconnect` is not, even while the connection is still open: the browser closes its old socket with a code the server counts as a drop.
- `WebSocketServer.Kick(peer, reason)` and `UDPServer.Kick(peer, reason)` end one peer's session now and report `PeerLeft` with the reason in `PeerEvent.Reason`: the session's resume secret is gone, so the client's `Reconnect` cannot resume it and joins as a new peer only if `Verify` accepts its token again. A connected WebSocket client receives a close frame with the application close code 4001 and the reason, a connected UDP client an encrypted kick datagram sent three times (UDP may drop one); both then report a `*KickError`, matching `netplay.ErrKicked`, from `Err` — in the browser too. A peer inside its grace period gets no message; its next handshake simply cannot resume. `Kick` returns `ErrUnknownPeer` for an ID with no session.
- A server holds at most `Auth.MaxPeers` sessions (1024 by default), counting peers inside their grace period; past it, clients are rejected with the reason `server full`. Without `Verify` anyone may join, so set it on any server reachable from the internet.
- A resume proves itself with a secret from the previous welcome, which the server replaces on every handshake; an ID alone cannot take over a session. Past the grace period, `Reconnect` joins again as a new peer when the token is still valid.
- In the browser, `DialWebSocket` and `DialWebTransport` return at once: `State` is `ConnConnecting` and `LocalID` is 0 until the welcome arrives, and packets sent before then are queued. A refusal moves them to `ConnDisconnected` with the `*RejectError` in `Err`.
- A `WebTransportServer` derives its QUIC `MaxIdleTimeout` from `Auth.IdleTimeout` (10 seconds by default, never less than 3) and sends QUIC keepalive pings every third of it; a native `WebTransportTransport` pings every 2 seconds with a 10-second idle timeout. Closing with `Close` sends a goodbye (`PeerLeft` at once); `Reconnect` closes with code `4000` so the server keeps the session for `Auth.GracePeriod`.
- A `WebSocketServer` pings each client every third of `Auth.IdleTimeout` (10 seconds by default, never less than 3) and drops a client from which no byte arrives, not even a pong, for that long, so a client whose network vanished without closing the TCP connection does not hold its session and ID forever. Browsers answer pings on their own. A native `WebSocketTransport` pings the server every 2 seconds and moves to `ConnDisconnected` when no byte arrives for 10 seconds. A large frame that arrives slowly is read whole as long as its bytes keep coming. These timeouts use the wall clock, not `Auth.Now`. `WebSocketServer.Close` sends every client its close frame at once and waits at most a second for clients that do not read.
- A WebSocket frame's payload is read as it arrives rather than allocated from the length the frame announces, and a hello may be no longer than a valid one, so a client that announces a large frame and then trickles it cannot reserve memory it has not sent. Control frames (`Close`, `Ping`, `Pong`) follow RFC 6455 §5.5: fragmented control frames, control frames over 125 payload bytes, and reserved control opcodes (`0xB`–`0xF`) are rejected.
- UDP sessions run handshake version 2 using `Noise_NK_25519_ChaChaPoly_SHA256`: `DialUDP(addr, serverKey, token)` authenticates the server against its static X25519 public key without certificates, encrypts the client's token and resume secret in message 1, and receives the assigned ID and fresh resume secret encrypted in message 2 (the welcome carries no packet key). Version-1 hellos receive the existing version rejection.
- Distribute the server's static X25519 public key out of band: either set a persistent `Auth.StaticKey` (`*ecdh.PrivateKey` from `ecdh.X25519()`) when clients load the key from configuration or reconnect across server restarts, or leave `Auth.StaticKey` nil to generate a fresh key pair per server and ship `srv.PublicKey().Bytes()` in matchmaking tickets (parsed on the client with `ecdh.X25519().NewPublicKey`). Missing or non-X25519 keys fail with `ErrInvalidKey`.
- After the UDP handshake, every datagram is encrypted and authenticated with ChaCha20-Poly1305 using the two directional keys from the Noise split: a 10-byte cleartext header (`type`, `peer`, `seq`, `channel`) is authenticated as associated data, followed by ciphertext and a 16-byte Poly1305 tag (26 bytes of overhead), with an implicit `direction || peer || seq` nonce and a 64-entry replay window. Sending or receiving refuses to wrap the 32-bit sequence counter (`ErrSequenceExhausted`); calling `Reconnect` runs a fresh Noise NK handshake to rekey the session and reset sequence numbers. A `UDPClient` pings every second while idle; a client silent for `Auth.IdleTimeout` (10 seconds by default, never less than 3) counts as dropped. The server answers only hellos of at least 128 bytes and never replies with more than it received.
- On `UDPTransport`, `AddPeerKey(id, addr, key)` protects direct peer-to-peer datagrams with a 32-byte pre-shared key using the same 26-byte ChaCha20-Poly1305 framing, replay window, reflection protection and sequence-wrap refusal (re-registering with a fresh key rekeys the peer); plaintext datagrams claiming a keyed peer's ID are dropped. Plain `AddPeer` remains unauthenticated and unencrypted for trusted LAN use and tests.
- The WebSocket handshake (version 1) is not encrypted at the application layer, so use `wss://` (`ListenWebSocketTLS` / `ListenWebSocketTLSFiles`) in production.

## Server-authoritative delta replication

Use `ServerWorld` and `ClientWorld` for action, co-op, or arena games where the server owns authoritative entity state. `ServerWorld.Step(tick)` tracks per-peer acknowledged snapshots and sends compact field-level deltas (falling back to full snapshots when needed):

```go
// Server:
server := netplay.NewServerWorld(serverTx)
heroID := server.Spawn(netplay.EntityState{
	ComponentState: netplay.ComponentState{Kind: 1, X: 100, Y: 50},
})
server.Update(heroID, updatedState)
_ = server.Step(tick)

// Client:
client := netplay.NewClientWorld(clientTx, serverID)
client.OnSpawn = func(e netplay.EntityState) { /* attach visuals */ }
client.OnDespawn = func(id netplay.EntityID) { /* remove visuals */ }

client.Poll()
client.Predict(heroID, localCmd, applyInput)
renderEntities := client.Interpolate(renderTick) // smooth remote entity motion
_ = renderEntities
```

Inspect bandwidth and reconciliation behavior with `server.Stats()`, `client.PredictedState(id)`, `client.Reconciliations()`, and `client.LastReconcileError()`.

- `Step` never passes `Transport.Send` a payload larger than `DefaultMaxSnapshotSize` (60000 bytes, which fits UDP, relay and WebSocket packets), or the size set with `server.SetMaxSnapshotSize(n)`. A larger full snapshot is split into parts that `ClientWorld` reassembles before applying, and a delta larger than the limit is replaced by a split full snapshot. A lost part costs that snapshot only: the client does not acknowledge it, so the next `Step` sends a whole one again. `Stats().PacketsSent` counts parts, `SnapshotsSent` whole snapshots. An entity that does not fit in a part on its own makes `Step` return `ErrPayloadTooLarge` naming it.
- Ticks are `uint32` and compared in serial-number order, so replication keeps working when the counter wraps after 2^32 ticks (about 2.27 years at 60 Hz). `Interpolate` reads `renderTick` relative to the latest snapshot's tick, modulo 2^32, so `float64(latest) - delay` works on either side of the wrap. A server that starts its ticks over in the same epoch sends ticks the client takes as stale: call `client.Reset()`, which forgets every snapshot, prediction and the epoch and reports each known entity through `OnDespawn`, before the new match's first snapshot, or start the match with `server.SetEpoch` so clients follow on their own.

### Replication epoch

Every snapshot carries the server's replication epoch, a number `NewServerWorld` picks at random, so a restarted server's snapshots are told apart from the previous run's even when their ticks overlap. A `ClientWorld` that receives a complete full snapshot of a new epoch forgets every snapshot, partial snapshot and prediction of the old one, reports each entity it knew through `OnDespawn`, and continues from the new epoch, without the game calling `Reset`. The snapshot is parsed whole first, so a corrupt packet cannot wipe the world. A delta of a new epoch cannot be applied to anything the client holds, so it only makes the client ask the server for a full snapshot of that epoch. The client remembers the last 16 epochs it left and ignores their snapshots, so a snapshot of the old run that arrives late cannot pull the client back. `client.Epoch()` returns the epoch the client follows.

- A server that keeps running but starts a new match with its ticks back at zero calls `server.SetEpoch(server.Epoch() + 1)`: the next `Step` sends every peer a full snapshot, and clients drop the old match as they would after a restart. A server that numbers its runs itself, or a test that needs fixed epochs, sets one the same way. An epoch should not repeat, because clients ignore the epochs they have left until 32 of their snapshots arrive in a row.
- Random epochs cannot be ordered, so a late snapshot of an epoch the client never saw, arriving after the live epoch's first one, moves the client to that dead epoch. A left epoch that keeps sending, 32 snapshots in a row with none of the current epoch between them, is therefore taken as the live one and followed again; a late burst of a finished run is far shorter. The same recovers a restart that draws one of the 16 left epochs.
- Two runs pick the same random epoch with a chance of one in 2^32; such a restart looks like a server that went back in ticks, which `Reset` or `SetEpoch` handles.
- Acknowledgments carry the epoch too, and the server ignores those of another epoch, so a client's ack of the old run never becomes a delta baseline in the new one.

### Wire format

Replication speaks version 2 of its wire protocol (`ReplicationVersion`); version 2 added the epoch. Every message starts with one byte holding the protocol version in its high four bits and the message type in its low four, then the epoch as 4 little-endian bytes. `uv` is an unsigned varint as `encoding/binary` writes it.

| Message | Type | Direction | Layout after the 5-byte header |
| --- | --- | --- | --- |
| Full snapshot | 1 | server to client | `tick(uv) count(uv) entity...` |
| Delta snapshot | 2 | server to client | `tick(uv) baselineTick(uv) despawnCount(uv) id(uv)... updateCount(uv) entity...` |
| Acknowledgment | 3 | client to server | `tick(uv)` |
| Resync request | 4 | client to server | nothing |
| Full snapshot part | 5 | server to client | `tick(uv) index(uv) total(uv) count(uv) entity...` |

An entity is `id(uv) mask(1)` followed by the fields whose mask bit is set, in bit order: kind (uv), x, y, vx, vy and rotation (4-byte little-endian floats), flags (uv), and props (a uv length, then the bytes). A full snapshot sets the bit of every field that is not zero; a delta sets the bits of the fields that changed since the baseline, and of every non-zero field of an entity the baseline did not have.

Version 1, from releases before the epoch, had no header: its messages start with the bare type 1 to 5, which reads as version 1. Peers of different versions cannot replicate to each other, and neither misreads the other's messages. A client refuses snapshots of another version and reports it through `client.Err()`, a server refuses acknowledgments and resync requests of another version and reports it through `server.PeerErr(peer)`; both errors wrap `ErrReplicationVersion` and name the two versions, so a game can tell the player to update instead of waiting for a world that never arrives.

Every `pkg/online/netplay` wire format—transport frames, UDP sealed datagrams, connection handshake, relay frames and control bodies, replication v2, lockstep/rollback inputs, and mid-match membership—is also specified as a versioned YAML schema under `pkg/online/netplay/wire/` (`*.wire.yaml`), with zero-allocation Go codecs (`AppendX` / `DecodeX`, plus `DiffX` / `ApplyX` for delta masks) and dependency-free TypeScript codecs (`encodeX` / `decodeX`, plus `diffX` / `applyX`) generated by `game wire`. Games can define their own schemas for typed `EntityState.Props` or custom messages with bit packing and integer-only `qfloat`/`fixed` quantization; see [netplay/wire](netplay-wire.md).

## Cross-language clients

Web dashboards, spectator views, bots, and ports rendered in another engine can speak every `pkg/online/netplay` protocol directly in TypeScript without hand-writing binary parsers. `game wire -lang ts` generates deterministic, dependency-free TypeScript modules backed by a `DataView` over a caller-owned `Uint8Array` that run identically in browsers, Node, and Bun:

```sh
go run ./cmd/game wire -lang ts -in pkg/online/netplay/wire -out pkg/online/netplay/wire/ts
```

The committed TypeScript codecs for all seven built-in protocols live under `pkg/online/netplay/wire/ts/` and are checked in CI with `-check`. Every message `X` generates `createX()`, `encodeX(m: X, w: Writer): void`, and `decodeX(r: Reader, m: X): void` (filling a reusable object so a 60 Hz client avoids per-packet allocations), and delta `mask` groups generate `diffX(base, cur)` and `applyX(base, delta)`:

```ts
import {
  Reader,
  ReplicationMessageTagFull,
  Writer,
  createReplicationMessage,
  decodeReplicationMessage,
  encodeReplicationMessage,
} from "./pkg/online/netplay/wire/ts/index.ts";

const msg = createReplicationMessage();
const reader = new Reader(packetBytes);
decodeReplicationMessage(reader, msg);
if (msg.tag === ReplicationMessageTagFull) {
  console.log(msg.full.epoch, msg.full.tick, msg.full.entities.length);
}
```

- 64-bit unsigned and signed fields (`u64`, `i64`, 64-bit `uvarint`/`svarint`, and `bits` wider than 32) map to `bigint`, so values above `Number.MAX_SAFE_INTEGER` such as `InputFrame.bits`, `ChecksumMsg.checksum`, and `RelayAck.mask` remain exact.
- `qfloat` and `fixed` quantize and dequantize with pure `BigInt` integer arithmetic matching Go's `wire.QuantizeFloat` / `wire.DequantizeFloat` / `wire.QuantizeFixed` bit for bit, and `diffX` compares IEEE-754 float bit patterns (`-0.0` differs from `+0.0`, `NaN` equals `NaN`).
- Shared YAML golden vectors under `pkg/online/netplay/wire/testdata/vectors/` are verified in both Go and TypeScript (`bun test pkg/online/netplay/wire/testdata/ts`) on every PR, alongside a live end-to-end test where a Go `ServerWorld` sends full, delta, and split snapshots to the TypeScript decoder.
- See `examples/ts-replication-client/` for a complete TypeScript client that decodes a replication v2 stream (`SnapshotFull`, `SnapshotDelta`, and `SnapshotFullPart`) and encodes `SnapshotAck` / `ResyncRequest` replies.

## Deterministic lockstep and rollback netcode

For fighting games, RTS titles, or tight arcade co-op, implement `SimState` (`Step`, `Save`, `Load`, `Checksum`) and drive it with `LockstepSession` or `RollbackSession`:

```go
session := netplay.NewRollbackSession(netplay.RollbackConfig{
	Transport:        peerTx,
	Sim:              sim,
	Peers:            []netplay.PeerID{1, 2},
	MaxRollback:      8,
	InputDelay:       1,
	ChecksumInterval: 30,
	OnDesync: func(ev netplay.DesyncEvent) {
		log.Printf("desync at tick %d: local=%x remote=%x", ev.Tick, ev.LocalChecksum, ev.RemoteChecksum)
	},
})

// Each fixed step in game.Loop.Update:
if err := session.Poll(); err != nil {
	return err
}
if err := session.Step(netplay.InputFrame{Tick: session.Tick(), Peer: peerTx.LocalID(), Bits: buttons}); err != nil {
	return err
}
```

- `RollbackSession` predicts missing remote inputs, saves snapshots per tick, and automatically rewinds and resimulates when a late remote input disagrees with prediction (returning `ErrRollbackWindowExceeded` if a peer falls beyond `MaxRollback`).
- `LockstepSession` (`SubmitInput`, `Poll`, `Step`) advances only when every peer's input for the current tick has arrived, ideal for RTS or turn-synchronized play. `SubmitInput` ignores an input for a tick already run or more than 256 ticks ahead.
- Both sessions exchange periodic checksums (use `ChecksumBytes` helper) to surface determinism bugs immediately via `OnDesync` and `Desyncs()`.
- Both sessions compare `uint32` ticks in serial-number order (RFC 1982), including membership windows and pending joins/leaves, so long matches continue seamlessly across the 2^32 tick wraparound. `RollbackSession` keeps unconfirmed history until the first tick confirms, so a peer whose first input arrives more than `MaxRollback + 2` ticks late can still confirm and advance.

### Players joining and leaving mid-match

A match's peers are not fixed when the session is created. One peer, the host (`Host` in the config, the lowest peer ID by default), admits and removes peers while the match runs, and every peer applies each change at the same tick:

```go
// On the host, when its transport reports a new player (PeerJoined, or a new ID in RelayTransport.Peers):
joinTick, err := session.AddPeer(newPeer)

// On the new player's machine; Step it every frame as usual, it only polls until Active reports true:
session := netplay.NewRollbackSession(netplay.RollbackConfig{Transport: peerTx, Sim: sim, Host: hostID, Join: true})

// On the host, once its transport reports the player gone (PeerLeft, or the ID missing from Peers):
leaveTick, err := session.RemovePeer(oldPeer)
```

- The join tick is the one the host's next own input is for: the later of `Tick` and the tick after its latest submitted input for lockstep, `Tick + InputDelay` for rollback. No peer can have run (lockstep) or confirmed (rollback) that tick yet, since that takes the host's input for it, and the change travels on the reliable channel ahead of that input. A rollback peer that has already predicted past the tick rolls back to it.
- Once the host's simulation stands at the start of the join tick (lockstep), or every tick before it is confirmed (rollback), the host sends the newcomer `Sim.Save()` at that tick together with the tick, the peers and, for each peer already leaving, the inputs the host declared for it from the join tick to its leave, in parts of 48000 bytes. The newcomer loads the state into its own `Sim`, its `Tick` starts at the join tick, and from then on it plays like the others, including the ticks of a peer the host removed before the newcomer had its state. A state larger than `MaxJoinStateSize` (16 MiB) makes `AddPeer` return `ErrPayloadTooLarge`, and one that has grown past it by the join tick turns the join into a leave, so nobody waits for the newcomer.
- Inputs before the join tick are not expected from the newcomer. In rollback its first `InputDelay` ticks are zero inputs on every peer, as every peer's are at the start of a match. Each peer sends the newcomer the inputs it had already sent for the join tick and later, and an input from the newcomer that reaches a peer before the host's announcement is kept until the announcement arrives.
- The leave tick is the host's next own input tick, or the peer's join tick if that is later. The change carries the inputs the host holds from the leaving peer for the ticks before it, and a zero input for each tick the host never received from it, so every peer runs those ticks alike; inputs arriving straight from the leaving peer are ignored from then on. Remove a peer after it has stopped sending: one removed while still sending may have reached another peer with an input the host never saw, which shows up as a desync. The removed peer's own session stops at the leave tick, and its `Active` reports false.
- `Members` lists the peers taking part in the current tick. Only the host may call `AddPeer` and `RemovePeer` (`ErrNotHost` elsewhere), the host cannot leave (`ErrHostLeave`), and a peer that left comes back under a new ID (`ErrPeerRejoin`, however long ago it left), as relays and servers assign one. `RemovePeer` of a peer that left returns its leave tick.
- A message the transport cannot deliver yet, because a relay has not told the sender of the newcomer or a peer is reconnecting, waits in the session and is sent again by the next `Poll`, in order with the messages that follow it. A session keeps up to 4096 such messages per peer; past that the peer has missed part of the match, every later message to it is dropped, and the next `SubmitInput` (lockstep) or `Step` or `Poll` (rollback) returns `ErrSessionBacklog` once for it, so the host removes it or the match ends.
- A joining session finds its host in `Host`, or else as the lowest other ID in `Peers`; when neither names another peer it takes the lowest other ID its transport reports, on each `Poll`, until its join state arrives. Over a relay, whose peer list may still be empty or include other newcomers, set `Host` explicitly.

## Packet capture and inspection

`netplay.NewCaptureTransport(inner, w, opts)` wraps any `Transport` and records each successful `Send` and each packet returned by `Recv` as one versioned JSON line (`version: 1`):

```json
{"version":1,"t":16000000,"dir":"send","peer":2,"ch":1,"seq":1,"payload":"IwEAAAAB"}
```

```go
f, _ := os.Create("session.capture.jsonl")
defer f.Close()

tr := netplay.NewCaptureTransport(inner, f, netplay.CaptureOptions{
	Channels:    []netplay.Channel{netplay.ChannelUnreliable},
	Peers:       []netplay.PeerID{2},
	SampleEvery: 1,
})
```

- **Plaintext above encryption.** Because `NewCaptureTransport` wraps the `Transport` interface, wrapping a `UDPClient` or `UDPServer` captures plaintext payloads after authentication and decryption, so offline inspection never needs session keys.
- **Filtering and sampling.** `CaptureOptions` can restrict capture to specific `Channels` or `Peers`, stride-sample one in every `SampleEvery` matching packets, or deterministically sample with `SampleRate` and `SampleSeed`. Capture write errors are reported via `OnError` and never fail `Send` or `Recv`.
- **InboxReporter passthrough.** The wrapper forwards `SetInboxLimits` and `InboxStats` to the underlying transport.
- **Privacy warning.** A packet capture contains raw player inputs, entity positions, and game state. Never ship capture files, commit player sessions, or collect captures from players without explicit consent.

### Decoding captures with `game net`

The `game net` CLI subcommand uses the [wire schemas](netplay-wire.md) to decode capture logs layer by layer (transport header, relay frame and control body, then replication, lockstep/rollback, membership, handshake, or custom game messages) and to compute traffic summaries:

```sh
# Decode a capture log layer by layer as YAML or JSON
go tool game net decode session.capture.jsonl
go tool game net decode --format json session.capture.jsonl

# Decode custom EntityState.Props using your game's *.wire.yaml schemas
go tool game net decode --schema ./proto session.capture.jsonl

# Track per-peer replication baselines so SnapshotDelta frames also print full resolved entity state
go tool game net decode --resolve-deltas session.capture.jsonl

# Decode a single hex or base64 frame from standard input
echo "230100000001" | go tool game net decode -

# Print packet and byte totals per message type, per peer, and mean delta size
go tool game net stats session.capture.jsonl
```

### Live network debug overlay

For live in-game inspection without per-frame allocations, wrap your transport with `gamedebug.NewNetMonitor` and pass `mon.Snapshot` to `gamedebug.Options.Net`. The overlay panel displays bytes/s and packets/s per channel and per peer, `InboxStats` counters (queued, dropped, refused, warnings), and the most recent decoded message types using reused `wire` codec buffers. See [gamedebug](gamedebug.md#network-inspector).
