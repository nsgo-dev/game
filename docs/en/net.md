# net

`game/net` provides cross-target multiplayer networking for NSGo games: a unified `Transport` across WebSocket (native + WebAssembly), native UDP, and simulated loopback; server-authoritative delta snapshot replication with client prediction and interpolation; and deterministic lockstep and rollback netcode.

```go
import "github.com/nsgo-dev/game/packages/game/net"
```

## Cross-target transports (`Transport`)

Every transport implements the same non-blocking `Transport` interface (`Send`, `Recv`, `LocalID`, `Peers`, `Close`) with `ChannelReliableOrdered` and `ChannelUnreliable` channels and `PeerBroadcast` (`PeerID(0)`):

| Transport | Constructor | Targets & use case |
| --- | --- | --- |
| `WebSocketServer` / `WebSocketTransport` | `ListenWebSocket`, `DialWebSocket` | Native + browser (`js/wasm`); works across NATs and web builds. |
| `UDPTransport` | `ListenUDP` + `AddPeer` | Native targets (desktop/mobile) for low-latency datagrams. It has no retransmission or ordering, so it refuses `ChannelReliableOrdered` with `ErrUnsupportedChannel`; lockstep and rollback need a WebSocket or loopback transport. |
| `LoopbackHub` / `LoopbackTransport` | `NewLoopbackHub` + `AddPeer` | In-memory deterministic tests with configurable `Latency`, `Jitter`, `LossRate`, and `Seed`. |

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
