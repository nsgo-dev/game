# server

`github.com/nsgo-dev/game/packages/game/server` runs a game's fixed-step updates in a plain Go process, without an NSGo app, window, [`core.Scope`](loop.md) or cgo.

The `Update` function a game hands to [`game.Loop`](loop.md) runs unchanged on the server: both step it with the same fixed `1/Rate` duration, so a dedicated server and the client share one simulation.

## Runner

```go
r := &server.Runner{
	Matches: []*server.Match{
		{Name: "match-1", Rate: 60, Update: update},
		{Name: "match-2", Rate: 30, Update: update},
	},
	OnOverrun: func(m *server.Match, lag time.Duration, missed int) {
		log.Printf("%s overran by %v (%d ticks missed)", m.Name, lag, missed)
	},
}
if err := r.Run(ctx); err != nil { ... }
```

`Runner.Run` blocks until its context is canceled, then waits for each match's in-flight tick and runs its `OnStop` before returning. Each match runs on its own goroutine at its own rate.

## Ticks and overruns

A match's step is `1/Rate` seconds (60 Hz when `Rate` is zero or negative). When `Update` runs longer than one step, `Runner` calls `OnOverrun` with the lateness and the number of whole steps it covers. Missed ticks are **not** caught up: the next tick is scheduled from the moment the slow update returns, so a slow update slows the match rather than producing a burst.

## Stopping

A dedicated server usually runs under `signal.NotifyContext`:

```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
```

On SIGTERM/SIGINT the context ends; the runner lets the current tick finish, then calls `OnStop` (for example to notify peers and flush state) and returns. `Update` must return promptly for the stop to stay bounded.

The runnable example is `server/cmd/dedicatedserver`:

```sh
go build ./server/cmd/dedicatedserver
```

It builds with the plain Go toolchain and no NSGo runtime.


## Matchmaking

`Matchmaker` queues `Ticket`s by mode and region and fills rooms of `RoomSize` players:

```go
mm := server.NewMatchmaker(4)
mm.BaseWindow = 100       // rating difference allowed at enqueue time
mm.WindowGrowthPerSecond = 10
mm.MaxWindow = 400
mm.Enqueue(server.Ticket{ID: 7, Mode: "coop", Region: "eu", Rating: 1200, Party: []string{"ada", "lin"}})
rooms := mm.Match()       // []server.Matchup with TicketIDs and Players
```

A ticket's party is placed whole or not at all. The default rule fills rooms oldest-first and adds a ticket only when its rating difference from the room's oldest ticket fits within **both** tickets' current windows: `BaseWindow + WindowGrowthPerSecond * waited`, capped by `MaxWindow`. Matches never mix beyond the newer ticket's window, and an old ticket's window widening lets it meet a later arrival. `Cancel(id)` removes a waiting ticket without disturbing the rest; `Waiting` lists the queue oldest-first.

Set `Rule` to replace the matching function; it receives a group of tickets with the same mode and region, the room size and a `window` function, and returns `Matchup`s. The matchmaker removes the tickets listed in each returned room.

## Rooms

`Rooms` groups peers into independent rooms with a per-match lifecycle and its own packet inboxes:

```go
rs := server.NewRooms()
rs.EmptyTimeout = 30 * time.Second
rs.Snapshot = func(r *server.Room) []byte { return encodeWorldState(r) }
room, _ := rs.Create(4, true)             // capacity 4, private with a code
join, _ := rs.JoinCode(room.Code(), peer) // or rs.Join(room.ID(), peer)
room.Start()                              // epoch 1, tick 0
room.Advance()                            // tick 1 while running
room.End()
room.Start()                              // epoch 2, tick 0: a fresh match
rs.Leave(room.ID(), peer)
rs.Tick()                                 // close rooms empty for EmptyTimeout
```

- A room's `Deliver`/`Recv` only accept and expose packets of its own members, so two rooms never see each other's traffic.
- `Join` on a running match calls `Snapshot` and returns it with the current epoch and tick; the joiner applies that full state before deltas.
- `Start` after `End` advances the epoch and resets the tick, so clients can drop the old match's tick state without waiting for the counter to wrap.
- A private room's `Code` is accepted by `JoinCode`; a full room returns `ErrRoomFull`, an unknown code `ErrRoomNotFound`.
- A room that loses its last member closes after `EmptyTimeout` when `Tick` runs.

## Rate limits and input validation

`Guard` enforces a per-peer policy before packets reach a room or simulation:

```go
g := server.NewGuard(server.Limits{
	PacketsPerSecond:   60,
	BytesPerSecond:     64 << 10,
	MaxMessageSize:     8 << 10,
	MaxPendingReliable: 256 << 10,
}, server.LimitDisconnect)
g.Validate = func(peer net.PeerID, p net.Packet) error { return checkInput(p) }
action, err := g.Check(peer, packet)
```

- Token buckets for packets and bytes with optional bursts; `LimitThrottle`/`LimitDrop` refuse the packet and keep the peer connected, `LimitDisconnect` refuses the peer from then on (`GuardDisconnect`).
- `MaxMessageSize` rejects an oversized packet before it is buffered; `ReserveReliable`/`ReleaseReliable` bound reserved reliable bytes.
- `Validate` runs only for an admitted packet; a non-nil error drops it with `ErrInvalidInput`, increments that peer's `Violations`, and never reaches the simulation.
- `Stats(peer)` reports packets, bytes, dropped, violations and pending reliable bytes per peer; `Reset` forgets a reconnecting peer. Per-peer state and O(1) checks leave other peers' ticks unaffected.

The guard is transport-agnostic: call `Check` at the packet loop and act on the returned `GuardAction`.

## Peer-to-peer relay

Lockstep and rollback sessions are peer-to-peer, but players behind NAT or in a browser cannot reach each other. `net.Relay` forwards their packets: each player connects to the relay with a `net.RelayTransport`, and the relay passes each frame to the member of the player's session it is addressed to.

```go
relay, err := gamenet.NewRelay(gamenet.RelayConfig{
	// Check the player's token and name the match it joins; the error text is the rejection reason.
	Join: func(peer gamenet.PeerID, token []byte) (string, error) {
		return matchOf(token)
	},
	MaxSessionPeers: 2,
})
auth := relay.Auth(gamenet.Auth{GracePeriod: 15 * time.Second})
srv, err := gamenet.ListenWebSocketTLSFiles(":443", 1, auth, "cert.pem", "key.pem", nil) // or gamenet.ListenUDPServer(":7777", 1, auth)
go relay.Serve(ctx, srv)
```

```go
cli, err := gamenet.DialWebSocket("wss://relay.example.com/", token) // or gamenet.DialUDP(addr, token)
peerTx := gamenet.NewRelayTransport(cli, gamenet.RelayTransportConfig{})
// Once peerTx.Peers() lists the opponent:
session := gamenet.NewRollbackSession(gamenet.RollbackConfig{Transport: peerTx, Sim: sim})
```

- The relay runs on a `WebSocketServer` or `UDPServer`, so every player passes [the connection handshake](net.md#connection-handshake-and-reconnect) first. `relay.Auth` runs the base `Auth.Verify`, when set, and then `Join`; an empty session name is rejected with `no session`, and a session that already holds `MaxSessionPeers` (8 by default) with `session full`.
- Frames go only between members of one session. A frame addressed to another session's peer, to the relay, to its sender, or malformed is dropped and counted in `Stats().Dropped`; `RelayTransport.Send` refuses an ID outside the session with `ErrUnknownPeer`.
- `Peers` lists the other members, including members inside the grace period, so a session does not lose a player who is reconnecting. The relay sends the membership again every second, so a client whose update was lost on UDP catches up.
- `ChannelReliableOrdered` is reliable end to end: each message is numbered per peer, the receiver acknowledges it (with a selective acknowledgement of the next 64), and the sender resends what is missing after a timeout adapted to the measured round trip. It works over UDP, and a message the relay could not deliver while a player reconnected with the inner transport's `Reconnect` arrives once the player is back. At most 1024 messages may wait for acknowledgement per peer; past that `Send` returns `ErrRelayBacklog`. A receiver holds at most 4 MB of one peer's messages that arrived ahead of a missing one and drops the rest until they are resent. `ChannelUnreliable` packets are sent once.
- When the inner transport's `Reconnect` admits the player as a new peer instead of resuming, because the grace period passed or the relay restarted, the `RelayTransport` starts over: it forgets the old members and unacknowledged messages, even when the relay hands out the same ID again, and waits for the new session's membership.
- Acknowledgements and resends happen inside `Recv`, which the sessions call every tick through `Poll`.
- The relay never reads payloads. It writes to each member on that member's own goroutine through a queue of `QueueSize` frames (256 by default), so a slow player delays only itself; frames past the queue are dropped, and reliable ones are resent by their sender.
- A relay serves one server for its lifetime, so WebSocket and UDP players cannot share a session; run one relay per protocol. `Serve` may be called again on the same server after it returns, and the players still connected keep their sessions. A payload is at most `MaxRelayPayload` bytes.
- The relay sees payloads in the clear unless the connection is `wss://`; the handshake caveats for UDP in [the net guide](net.md#connection-handshake-and-reconnect) apply.

### Relay latency

`BenchmarkRelayRoundTrip` in `game/net` measures the round trip of a 48-byte message on one machine over the loopback interface. "Direct" is a client and its server, one hop each way; "relay" is two `RelayTransport`s through a local relay, two hops each way, sending reliably as rollback does:

| Path | Linux x86-64, 24 cores | macOS arm64, 10 cores |
| --- | --- | --- |
| UDP direct | 63 µs | 34 µs |
| UDP through the relay | 188 µs | 94 µs |
| WebSocket direct | 44 µs | 33 µs |
| WebSocket through the relay | 187 µs | 87 µs |

A local relay adds roughly 55 to 145 µs to a round trip, well under a millisecond and under a tenth of a 60 Hz tick. Over the internet the relay's cost is the extra network distance of the detour, so run relays in the regions where players are. Reproduce the numbers with:

```sh
go tool nsgo test ./net -run '^$' -bench BenchmarkRelayRoundTrip
```

### WebRTC data channels

Decision: `game/net` does not build WebRTC data channels with STUN now; the relay is the path for every peer-to-peer session.

- WebRTC needs ICE with STUN and TURN, DTLS and SCTP. Browsers provide them through `RTCPeerConnection`, but native targets would need all of them in Go; NSGo does not expose a platform WebRTC stack, and the only complete Go implementation is a large third-party stack that every native build would carry.
- A relay is needed anyway: STUN fails behind symmetric NATs, common on mobile carriers, and TURN is itself a relay. Signaling also needs a server.
- Rollback traffic is small, and a nearby relay adds little latency (see above).

Revisit it when measurements show players near each other but far from every relay region, or when NSGo exposes WebRTC on native targets. The design then is a WebRTC transport behind the same `Transport` interface that tries a direct path with STUN and falls back to `RelayTransport`, so sessions do not change.

## TLS, origins, health and metrics

A browser on an HTTPS page needs `wss://`. `net.ListenWebSocketTLS` wraps the WebSocket listener in TLS and checks the browser Origin during the upgrade; `ListenWebSocketTLSFiles` loads a PEM certificate and key. Both take the same `Auth` as `ListenWebSocket` (see [the connection handshake](net.md#connection-handshake-and-reconnect)):

```go
srv, err := gamenet.ListenWebSocketTLSFiles(":443", 1, auth, "cert.pem", "key.pem",
	[]string{"https://game.example.com"})
```

An `Origin` outside the allow-list is refused with `403` before the WebSocket handshake; an empty allow-list accepts any Origin and `"*"` accepts all, while an empty Origin (native game clients) is allowed. Each entry is a `scheme://host` Origin such as `https://game.example.com`; a trailing slash is ignored, and an entry that names a path or query makes the listener fail at start-up.

`server.Health` serves deployment endpoints:

```go
health := server.NewHealth(func() bool { return worldReady() }, func() server.Metrics {
	return server.Metrics{Peers: peers, Rooms: rooms, BytesIn: in, BytesOut: out}
})
http.ListenAndServe(":8080", health)
```

- `GET /healthz` — process liveness;
- `GET /readyz` — 200 while `Ready` returns true, else 503;
- `GET /metrics` — Prometheus gauges/counters for peers, rooms, last tick duration and bytes.

Observe each server tick with `health.ObserveTick(time.Since(start))`.

### Container and draining

Build the example as a static, cgo-free binary and run it as a small container:

```dockerfile
FROM golang:1.27 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./packages/game/server/cmd/dedicatedserver

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
```

Run it with `SIGTERM` as the stop signal (`docker stop` sends SIGTERM by default). The example uses `signal.NotifyContext`; `Runner.Run` lets the current tick finish, runs each match's `OnStop` to notify peers and flush state, then exits, so the container drains instead of losing a tick.

## Player tokens

A dedicated server learns who a player is from a token the player's device got from the [account service](accounts.md). It loads the service's key set file once and verifies each token offline with `accounts/token`, which links none of the account service's password hashing or database drivers:

```go
ks, err := token.ParseKeySet(keysYAML)
v, err := token.NewVerifier(ks)
claims, err := v.Verify(tok) // claims.Subject is the account ID
```
