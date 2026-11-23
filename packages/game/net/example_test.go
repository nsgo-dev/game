package net_test

import (
	"encoding/binary"
	"fmt"
	"time"

	gamenet "github.com/nsgo-dev/game/packages/game/net"
)

// A LoopbackHub connects transports in memory with simulated latency and loss, driven by its own clock, so networked code runs in tests without sockets.
func ExampleLoopbackHub() {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{Latency: 50 * time.Millisecond, Seed: 1})
	server := hub.AddPeer(1)
	client := hub.AddPeer(2)

	client.Send(1, gamenet.ChannelReliableOrdered, []byte("hello"))
	_, ok := server.Recv()
	fmt.Println("at 0ms:", ok)
	hub.Advance(50 * time.Millisecond)
	p, ok := server.Recv()
	fmt.Println("at 50ms:", ok, p.From, string(p.Payload))
	// Output:
	// at 0ms: false
	// at 50ms: true 2 hello
}

// tally is a tiny deterministic simulation: each tick adds every player's input bits to a running total.
type tally struct{ total uint64 }

func (s *tally) Step(inputs []gamenet.InputFrame) {
	for _, in := range inputs {
		s.total += in.Bits
	}
}

func (s *tally) Save() []byte { return binary.LittleEndian.AppendUint64(nil, s.total) }

func (s *tally) Load(b []byte) error {
	s.total = binary.LittleEndian.Uint64(b)
	return nil
}

func (s *tally) Checksum() uint64 { return s.total }

// A LockstepSession steps a deterministic simulation only once every peer's input for the tick has arrived, so all peers stay in the same state.
func ExampleLockstepSession() {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{Latency: 30 * time.Millisecond})
	simA, simB := &tally{}, &tally{}
	a := gamenet.NewLockstepSession(gamenet.LockstepConfig{Transport: hub.AddPeer(1), Sim: simA, Peers: []gamenet.PeerID{1, 2}})
	b := gamenet.NewLockstepSession(gamenet.LockstepConfig{Transport: hub.AddPeer(2), Sim: simB, Peers: []gamenet.PeerID{1, 2}})

	for tick := range uint32(3) {
		a.SubmitInput(gamenet.InputFrame{Tick: tick, Peer: 1, Bits: 1})
		b.SubmitInput(gamenet.InputFrame{Tick: tick, Peer: 2, Bits: 10})
		stepped := a.Step()
		hub.Advance(30 * time.Millisecond)
		fmt.Println(tick, stepped, a.Step(), b.Step())
	}
	fmt.Println(simA.total, simB.total)
	// Output:
	// 0 false true true
	// 1 false true true
	// 2 false true true
	// 33 33
}

// ChecksumBytes hashes a saved state so peers can compare their simulations cheaply.
func ExampleChecksumBytes() {
	a := gamenet.ChecksumBytes([]byte("state at tick 120"))
	b := gamenet.ChecksumBytes([]byte("state at tick 120"))
	c := gamenet.ChecksumBytes([]byte("state at tick 121"))
	fmt.Println(a == b, a == c)
	// Output:
	// true false
}

// A ServerWorld owns the authoritative entities and sends each client a full snapshot, then bitmask deltas once the client acknowledges; a ClientWorld applies them, calls OnSpawn and OnDespawn, and interpolates between snapshots for drawing.
func ExampleServerWorld() {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{Latency: 16 * time.Millisecond})
	srv := gamenet.NewServerWorld(hub.AddPeer(1))
	cli := gamenet.NewClientWorld(hub.AddPeer(2), 1)
	cli.OnSpawn = func(e gamenet.EntityState) { fmt.Printf("spawn %d %q\n", e.ID, e.Props) }
	cli.OnDespawn = func(id gamenet.EntityID) { fmt.Println("despawn", id) }

	hero := srv.Spawn(gamenet.EntityState{ComponentState: gamenet.ComponentState{Kind: 1, Props: []byte("hero")}})
	coin := srv.Spawn(gamenet.EntityState{ComponentState: gamenet.ComponentState{Kind: 2, X: 100}})
	for tick := uint32(1); tick <= 3; tick++ {
		srv.Update(hero, gamenet.EntityState{ComponentState: gamenet.ComponentState{Kind: 1, X: float32(tick * 10), Props: []byte("hero")}})
		if tick == 3 {
			srv.Despawn(coin)
		}
		_ = srv.Step(tick)
		hub.Advance(16 * time.Millisecond)
		cli.Poll()
		hub.Advance(16 * time.Millisecond) // the acknowledgment reaches the server
	}

	e, _ := cli.Entity(hero)
	latest, _ := cli.LatestTick()
	fmt.Println("tick", latest, "hero x", e.X)
	for _, s := range cli.Interpolate(2.5) {
		fmt.Println("at tick 2.5:", s.ID, s.X)
	}
	stats := srv.Stats()
	fmt.Println(stats.FullSnapshotsSent >= 1, stats.DeltaSnapshotsSent >= 1)
	// Output:
	// spawn 1 "hero"
	// spawn 2 ""
	// despawn 2
	// tick 3 hero x 30
	// at tick 2.5: 1 25
	// true true
}

// ClientWorld.Predict moves the local player at once and keeps the input until the server's snapshot of that tick arrives, then replays the inputs the server has not seen yet on top of it.
func ExampleClientWorld_Predict() {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{Latency: 16 * time.Millisecond})
	srv := gamenet.NewServerWorld(hub.AddPeer(1))
	cli := gamenet.NewClientWorld(hub.AddPeer(2), 1)
	player := srv.Spawn(gamenet.EntityState{})
	_ = srv.Step(1)
	hub.Advance(16 * time.Millisecond)
	cli.Poll()

	move := func(st *gamenet.EntityState, in any) { st.X += in.(float32) }
	for range 3 {
		cli.Predict(player, float32(5), move)
	}
	p, _ := cli.PredictedState(player)
	fmt.Println("predicted x", p.X, "pending", cli.PendingPredictions(player))
	// Output:
	// predicted x 15 pending 3
}

// A RollbackSession steps at once with a prediction of each remote input (the peer's last one) and, when the real input arrives and differs, rolls back and re-simulates, so peers end in the same state without waiting on the network.
func ExampleRollbackSession() {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{Latency: 30 * time.Millisecond})
	simA, simB := &tally{}, &tally{}
	peers := []gamenet.PeerID{1, 2}
	a := gamenet.NewRollbackSession(gamenet.RollbackConfig{Transport: hub.AddPeer(1), Sim: simA, Peers: peers, MaxRollback: 8})
	b := gamenet.NewRollbackSession(gamenet.RollbackConfig{Transport: hub.AddPeer(2), Sim: simB, Peers: peers, MaxRollback: 8})

	// Peer 2 presses 10 for two ticks, then changes to 20, which peer 1 has predicted wrongly by the time it arrives.
	bBits := []uint64{10, 10, 20, 20, 20, 20}
	for tick, bits := range bBits {
		_ = a.Step(gamenet.InputFrame{Tick: uint32(tick), Peer: 1, Bits: 1})
		_ = b.Step(gamenet.InputFrame{Tick: uint32(tick), Peer: 2, Bits: bits})
		hub.Advance(16 * time.Millisecond)
	}
	hub.Advance(100 * time.Millisecond)
	_ = a.Poll()
	_ = b.Poll()

	stats := a.Stats()
	fmt.Println(a.Tick(), simA.total, simB.total)
	fmt.Println(stats.Rollbacks > 0, stats.Mispredictions > 0, len(a.Desyncs()))
	// Output:
	// 6 106 106
	// true true 0
}
