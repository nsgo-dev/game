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
