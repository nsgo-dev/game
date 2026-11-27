package netplay_test

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
)

// A LoopbackHub connects transports in memory with simulated latency and loss, driven by its own clock, so networked code runs in tests without sockets.
func ExampleLoopbackHub() {
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{Latency: 50 * time.Millisecond, Seed: 1})
	server := hub.AddPeer(1)
	client := hub.AddPeer(2)

	client.Send(1, netplay.ChannelReliableOrdered, []byte("hello"))
	_, ok := server.Recv()
	fmt.Println("at 0ms:", ok)
	hub.Advance(50 * time.Millisecond)
	p, ok := server.Recv()
	fmt.Println("at 50ms:", ok, p.From, string(p.Payload))
	// Output:
	// at 0ms: false
	// at 50ms: true 2 hello
}

// An inbox caps each peer's unreliable packets and keeps the newest when a game falls behind; reliable packets are never dropped, and a backlog past the warning threshold is counted once.
func ExampleInboxStats() {
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{Inbox: netplay.InboxLimits{Unreliable: 2, ReliableWarning: 3}})
	server := hub.AddPeer(1)
	client := hub.AddPeer(2)

	for _, pos := range []string{"x=1", "x=2", "x=3"} {
		server.Send(2, netplay.ChannelUnreliable, []byte(pos))
	}
	for range 5 {
		server.Send(2, netplay.ChannelReliableOrdered, []byte("event"))
	}
	st := client.InboxStats()
	fmt.Println("queued:", st.Unreliable, st.Reliable, "dropped:", st.UnreliableDropped, "warnings:", st.ReliableWarnings)
	p, _ := client.Recv()
	fmt.Println("oldest kept:", string(p.Payload))
	// Output:
	// queued: 2 5 dropped: 1 warnings: 1
	// oldest kept: x=2
}

// tally is a tiny deterministic simulation: each tick adds every player's input bits to a running total.
type tally struct{ total uint64 }

func (s *tally) Step(inputs []netplay.InputFrame) {
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
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{Latency: 30 * time.Millisecond})
	simA, simB := &tally{}, &tally{}
	a := netplay.NewLockstepSession(netplay.LockstepConfig{Transport: hub.AddPeer(1), Sim: simA, Peers: []netplay.PeerID{1, 2}})
	b := netplay.NewLockstepSession(netplay.LockstepConfig{Transport: hub.AddPeer(2), Sim: simB, Peers: []netplay.PeerID{1, 2}})

	for tick := range uint32(3) {
		a.SubmitInput(netplay.InputFrame{Tick: tick, Peer: 1, Bits: 1})
		b.SubmitInput(netplay.InputFrame{Tick: tick, Peer: 2, Bits: 10})
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

// The host admits a peer to a running lockstep match: the newcomer loads the host's state at the join tick, and the others expect its inputs from that tick on. RemovePeer reverses it: from the leave tick nobody waits for the peer.
func ExampleLockstepSession_AddPeer() {
	const latency = 10 * time.Millisecond
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{Latency: latency})
	bits := map[netplay.PeerID]uint64{1: 1, 2: 10, 3: 100}
	sims := map[netplay.PeerID]*tally{1: {}, 2: {}, 3: {}}
	sessions := map[netplay.PeerID]*netplay.LockstepSession{}
	for _, id := range []netplay.PeerID{1, 2} {
		sessions[id] = netplay.NewLockstepSession(netplay.LockstepConfig{Transport: hub.AddPeer(id), Sim: sims[id], Peers: []netplay.PeerID{1, 2}})
	}
	host := sessions[1]
	play := func(ticks int) {
		for range ticks {
			for id, s := range sessions {
				s.SubmitInput(netplay.InputFrame{Tick: s.Tick(), Bits: bits[id]})
			}
			hub.Advance(latency)
			for _, s := range sessions {
				s.Step()
			}
		}
	}
	play(2)

	sessions[3] = netplay.NewLockstepSession(netplay.LockstepConfig{Transport: hub.AddPeer(3), Sim: sims[3], Host: 1, Join: true})
	join, _ := host.AddPeer(3)
	hub.Advance(latency)
	for _, s := range sessions {
		s.Poll()
	}
	fmt.Println("joined at", join, sessions[3].Active(), sims[3].total == sims[1].total)
	play(3)
	fmt.Println(host.Members(), sims[1].total, sims[3].total)

	leave, _ := host.RemovePeer(2)
	delete(sessions, 2)
	play(2)
	fmt.Println("left at", leave, host.Members(), sims[1].total, sims[3].total)
	// Output:
	// joined at 2 true true
	// [1 2 3] 355 355
	// left at 5 [1 3] 557 557
}

// ChecksumBytes hashes a saved state so peers can compare their simulations cheaply.
func ExampleChecksumBytes() {
	a := netplay.ChecksumBytes([]byte("state at tick 120"))
	b := netplay.ChecksumBytes([]byte("state at tick 120"))
	c := netplay.ChecksumBytes([]byte("state at tick 121"))
	fmt.Println(a == b, a == c)
	// Output:
	// true false
}

// A ServerWorld owns the authoritative entities and sends each client a full snapshot, then bitmask deltas once the client acknowledges; a ClientWorld applies them, calls OnSpawn and OnDespawn, and interpolates between snapshots for drawing.
func ExampleServerWorld() {
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{Latency: 16 * time.Millisecond})
	srv := netplay.NewServerWorld(hub.AddPeer(1))
	cli := netplay.NewClientWorld(hub.AddPeer(2), 1)
	cli.OnSpawn = func(e netplay.EntityState) { fmt.Printf("spawn %d %q\n", e.ID, e.Props) }
	cli.OnDespawn = func(id netplay.EntityID) { fmt.Println("despawn", id) }

	hero := srv.Spawn(netplay.EntityState{ComponentState: netplay.ComponentState{Kind: 1, Props: []byte("hero")}})
	coin := srv.Spawn(netplay.EntityState{ComponentState: netplay.ComponentState{Kind: 2, X: 100}})
	for tick := uint32(1); tick <= 3; tick++ {
		srv.Update(hero, netplay.EntityState{ComponentState: netplay.ComponentState{Kind: 1, X: float32(tick * 10), Props: []byte("hero")}})
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
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{Latency: 16 * time.Millisecond})
	srv := netplay.NewServerWorld(hub.AddPeer(1))
	cli := netplay.NewClientWorld(hub.AddPeer(2), 1)
	player := srv.Spawn(netplay.EntityState{})
	_ = srv.Step(1)
	hub.Advance(16 * time.Millisecond)
	cli.Poll()

	move := func(st *netplay.EntityState, in any) { st.X += in.(float32) }
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
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{Latency: 30 * time.Millisecond})
	simA, simB := &tally{}, &tally{}
	peers := []netplay.PeerID{1, 2}
	a := netplay.NewRollbackSession(netplay.RollbackConfig{Transport: hub.AddPeer(1), Sim: simA, Peers: peers, MaxRollback: 8})
	b := netplay.NewRollbackSession(netplay.RollbackConfig{Transport: hub.AddPeer(2), Sim: simB, Peers: peers, MaxRollback: 8})

	// Peer 2 presses 10 for two ticks, then changes to 20, which peer 1 has predicted wrongly by the time it arrives.
	bBits := []uint64{10, 10, 20, 20, 20, 20}
	for tick, bits := range bBits {
		_ = a.Step(netplay.InputFrame{Tick: uint32(tick), Peer: 1, Bits: 1})
		_ = b.Step(netplay.InputFrame{Tick: uint32(tick), Peer: 2, Bits: bits})
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
