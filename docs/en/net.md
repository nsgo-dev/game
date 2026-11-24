# net

`game/net` provides cross-target multiplayer networking for NSGo games: a unified `Transport` across WebSocket (native + WebAssembly), native UDP, and simulated loopback; server-authoritative delta snapshot replication with client prediction and interpolation; and deterministic lockstep and rollback netcode.

```go
import "github.com/nsgo-dev/game/packages/game/net"
```

## Cross-target transports (`Transport`)

Every transport implements the same non-blocking `Transport` interface (`Send`, `Recv`, `LocalID`, `Peers`, `Close`) with `ChannelReliableOrdered` and `ChannelUnreliable` channels and `PeerBroadcast` (`PeerID(0)`):

| Transport | Constructor | Targets & use case |
| --- | --- | --- |
| `WebSocketServer` / `WebSocketTransport` | `ListenWebSocket`, `DialWebSocket` | Native + browser (`js/wasm`); works across NATs and web builds. Clients authenticate with a token and the server assigns their IDs (see below). |
| `UDPServer` / `UDPClient` | `ListenUDPServer`, `DialUDP` | Native targets (desktop/mobile) for low-latency datagrams to a server, with the same handshake as WebSocket and every datagram bound to its session. |
| `UDPTransport` | `ListenUDP` + `AddPeer` | Native peer-to-peer datagrams between endpoints that know each other's addresses: a datagram is accepted only from the address registered for the ID it claims. |
| `Relay` / `RelayTransport` | `NewRelay`, `NewRelayTransport` | Peer-to-peer sessions between players who cannot reach each other: each connects to a relay over WebSocket or UDP, and the relay forwards between members of the same session (see [the server guide](server.md#peer-to-peer-relay)). |
| `LoopbackHub` / `LoopbackTransport` | `NewLoopbackHub` + `AddPeer` | In-memory deterministic tests with configurable `Latency`, `Jitter`, `LossRate`, and `Seed`. |

UDP has no retransmission or ordering, so both UDP transports refuse `ChannelReliableOrdered` with `ErrUnsupportedChannel`; lockstep and rollback need a WebSocket or loopback transport, or a `RelayTransport`, which retransmits end to end and so runs them over UDP too.

A transport has a single consumer. `Recv` hands each packet to one caller, and `ServerWorld`, `ClientWorld`, the lockstep and rollback sessions, `Relay` and `RelayTransport` all drain every waiting packet and discard the ones that are not theirs, so two of them on one transport steal each other's packets. Give each consumer its own transport, or put a demultiplexer in front of them that implements `Transport` for each. `Recv` costs the same however many packets are queued, so draining a burst stays linear.

If the socket under a `UDPTransport` or `UDPServer` fails, `Err` returns `ErrReceiveFailed` wrapping the socket's error and `Send` returns it too; close the transport and listen again; after `Close`, `Err` returns nil. Errors about a single datagram, such as an ICMP port unreachable for an earlier send, do not stop it. A `UDPClient` likewise keeps reading after an ICMP error, for example while its server restarts, and leaves it to its idle timeout to decide whether the server is gone.

```go
hub := net.NewLoopbackHub(net.LoopbackConfig{
	Latency:  40 * time.Millisecond,
	Jitter:   10 * time.Millisecond,
	LossRate: 0.02,
	Seed:     7,
})
serverTx := hub.AddPeer(1)
clientTx := hub.AddPeer(2)
_ = serverTx.Send(2, net.ChannelReliableOrdered, []byte("hello"))
hub.Advance(60 * time.Millisecond)
pkt, ok := clientTx.Recv()
_, _ = pkt, ok
```

### Inbox limits

A transport keeps each packet that arrives until `Recv` takes it, and bounds what it keeps by channel and by peer. Each peer's `ChannelUnreliable` packets are capped at `DefaultUnreliableInbox` (4096): when one arrives from a peer that already has that many queued, that peer's oldest queued unreliable packet is dropped, so a game that falls behind keeps the newest state, a peer that floods a server evicts only its own packets, and `InboxStats().UnreliableDropped` counts the loss.

`ChannelReliableOrdered` packets are never dropped, since that would break the channel's guarantee. A transport whose peers cannot be trusted caps each peer's reliable packets at `DefaultReliableInbox` (16384) instead and refuses the one that goes past it, counting it in `InboxStats().ReliableRefused`: a `WebSocketServer` ends that client's session, as if it had left, so the packets it did deliver stay in order, and a `RelayTransport` stops acknowledging that peer, which resends its messages once `Recv` has made room. The other transports receive reliable packets only from their server or from the same process and keep them all. Every transport also counts `InboxStats().ReliableWarnings` once each time its whole reliable backlog passes `DefaultReliableInboxWarning` (4096), and `ReliableBacklog` stays true until the backlog falls to half of it, so a queue hovering at the threshold does not warn on every packet. `Recv` still returns packets in arrival order across both channels and every peer. `UDPTransport` never sends reliable packets, so it drops a datagram that claims to be one.

Every transport implements `InboxReporter`: `SetInboxLimits` changes the limits at any time (lowering the unreliable cap drops each peer's oldest unreliable packets at once), and `InboxStats` reads the counters, which only grow, so a game or a debug overlay compares them with the previous frame's. `LoopbackConfig.Inbox` sets the limits of the peers a hub adds, and `RelayTransportConfig.Inbox` those of a `RelayTransport`. In `InboxLimits`, zero uses the default and a negative value removes the cap or turns the warning off. A `RelayTransport`'s inner transport has its own limits, and every relayed peer arrives on it from the relay, so they share its per-peer cap; a reliable message it drops there is sent again.

```go
tx.SetInboxLimits(net.InboxLimits{Unreliable: 512, ReliableWarning: 1024})

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

`WebSocketServer` and `UDPServer` admit a client only after a handshake: the client presents an app-defined token, `Auth.Verify` checks it, and the server assigns the client's `PeerID` and sends it back, or refuses with a reason. A refused client never reaches `Recv` or `Peers`.

```go
auth := net.Auth{
	// Check a signed token locally; the error text is sent to the client.
	Verify: func(peer net.PeerID, token []byte) error {
		if !validToken(token) {
			return errors.New("invalid token")
		}
		return nil
	},
	GracePeriod: 15 * time.Second, // a dropped client keeps its ID this long
}
srv, _ := net.ListenWebSocket(":8080", 1, auth) // or net.ListenUDPServer(":7777", 1, auth)

// Each server tick:
for ev, ok := srv.NextEvent(); ok; ev, ok = srv.NextEvent() {
	switch ev.Kind {
	case net.PeerJoined: // new ID
	case net.PeerDropped: // connection lost; ID and state kept for GracePeriod
	case net.PeerResumed: // same ID back within GracePeriod
	case net.PeerLeft: // closed, or GracePeriod ran out
		rooms.Leave(roomOf(ev.Peer), ev.Peer)
		guard.Reset(ev.Peer)
	}
}
```

```go
cli, err := net.DialWebSocket("wss://play.example.com/game", token) // or net.DialUDP(addr, token)
if errors.Is(err, net.ErrRejected) {
	// err is a *net.RejectError carrying the server's reason
}
me := cli.LocalID() // assigned by the server

// After a network drop (State() == net.ConnDisconnected):
_ = cli.Reconnect() // cli.Resumed() reports whether it kept its ID
```

- `NextEvent` checks grace periods and idle timeouts when it is called, so a server polls it every tick alongside `Recv`. While a peer is inside its grace period, `Send` to it returns `ErrDisconnected` and `Peers` leaves it out.
- `Close` on a client is a goodbye: the server reports `PeerLeft` at once instead of holding the session. `Reconnect` is not, even while the connection is still open: the browser closes its old socket with a code the server counts as a drop.
- A server holds at most `Auth.MaxPeers` sessions (1024 by default), counting peers inside their grace period; past it, clients are rejected with the reason `server full`. Without `Verify` anyone may join, so set it on any server reachable from the internet.
- A resume proves itself with a secret from the previous welcome, which the server replaces on every handshake; an ID alone cannot take over a session. Past the grace period, `Reconnect` joins again as a new peer when the token is still valid.
- In the browser, `DialWebSocket` returns at once: `State` is `ConnConnecting` and `LocalID` is 0 until the welcome arrives, and packets sent before then are queued. A refusal moves it to `ConnDisconnected` with the `*RejectError` in `Err`.
- A `WebSocketServer` pings each client every third of `Auth.IdleTimeout` (10 seconds by default, never less than 3) and drops a client from which no byte arrives, not even a pong, for that long, so a client whose network vanished without closing the TCP connection does not hold its session and ID forever. Browsers answer pings on their own. A native `WebSocketTransport` pings the server every 2 seconds and moves to `ConnDisconnected` when no byte arrives for 10 seconds. A large frame that arrives slowly is read whole as long as its bytes keep coming. These timeouts use the wall clock, not `Auth.Now`. `WebSocketServer.Close` sends every client its close frame at once and waits at most a second for clients that do not read.
- A WebSocket frame's payload is read as it arrives rather than allocated from the length the frame announces, and a hello may be no longer than a valid one, so a client that announces a large frame and then trickles it cannot reserve memory it has not sent.
- After the UDP handshake, each client is bound to the address it shook hands from and to a per-session key: every datagram carries a truncated HMAC-SHA256 tag and a sequence number checked against a replay window, so a spoofed or replayed datagram is dropped and cannot move the peer. Datagrams are authenticated, not encrypted. A `UDPClient` pings every second while idle; a client silent for `Auth.IdleTimeout` (10 seconds by default, never less than 3) counts as dropped. The server answers only hellos of at least 128 bytes and never replies with more than it received.
- The handshake is not encrypted: the hello carries the token, and the welcome carries the resume secret and, for UDP, the session key. Use `wss://` for WebSocket in production. UDP has no such layer, so its binding stops only attackers who cannot see the traffic: anyone who observes a handshake (shared Wi-Fi, any hop on the path) can forge that session's datagrams and resume it from their own address. Issue UDP tokens that are short-lived and single-use, and prefer WebSocket over `wss://` where an on-path observer is a concern.

## Server-authoritative delta replication

Use `ServerWorld` and `ClientWorld` for action, co-op, or arena games where the server owns authoritative entity state. `ServerWorld.Step(tick)` tracks per-peer acknowledged snapshots and sends compact field-level deltas (falling back to full snapshots when needed):

```go
// Server:
server := net.NewServerWorld(serverTx)
heroID := server.Spawn(net.EntityState{
	ComponentState: net.ComponentState{Kind: 1, X: 100, Y: 50},
})
server.Update(heroID, updatedState)
_ = server.Step(tick)

// Client:
client := net.NewClientWorld(clientTx, serverID)
client.OnSpawn = func(e net.EntityState) { /* attach visuals */ }
client.OnDespawn = func(id net.EntityID) { /* remove visuals */ }

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

## Deterministic lockstep and rollback netcode

For fighting games, RTS titles, or tight arcade co-op, implement `SimState` (`Step`, `Save`, `Load`, `Checksum`) and drive it with `LockstepSession` or `RollbackSession`:

```go
session := net.NewRollbackSession(net.RollbackConfig{
	Transport:        peerTx,
	Sim:              sim,
	Peers:            []net.PeerID{1, 2},
	MaxRollback:      8,
	InputDelay:       1,
	ChecksumInterval: 30,
	OnDesync: func(ev net.DesyncEvent) {
		log.Printf("desync at tick %d: local=%x remote=%x", ev.Tick, ev.LocalChecksum, ev.RemoteChecksum)
	},
})

// Each fixed step in game.Loop.Update:
if err := session.Poll(); err != nil {
	return err
}
if err := session.Step(net.InputFrame{Tick: session.Tick(), Peer: peerTx.LocalID(), Bits: buttons}); err != nil {
	return err
}
```

- `RollbackSession` predicts missing remote inputs, saves snapshots per tick, and automatically rewinds and resimulates when a late remote input disagrees with prediction (returning `ErrRollbackWindowExceeded` if a peer falls beyond `MaxRollback`).
- `LockstepSession` (`SubmitInput`, `Poll`, `Step`) advances only when every peer's input for the current tick has arrived, ideal for RTS or turn-synchronized play.
- Both sessions exchange periodic checksums (use `ChecksumBytes` helper) to surface determinism bugs immediately via `OnDesync` and `Desyncs()`.
