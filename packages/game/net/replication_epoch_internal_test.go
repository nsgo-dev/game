package net

import (
	"bytes"
	"errors"
	"testing"
)

// scriptTransport delivers queued packets from the server and records what the client sends.
type scriptTransport struct {
	in   []Packet
	sent [][]byte
}

func (s *scriptTransport) Send(_ PeerID, _ Channel, payload []byte) error {
	s.sent = append(s.sent, bytes.Clone(payload))
	return nil
}

func (s *scriptTransport) Recv() (Packet, bool) {
	if len(s.in) == 0 {
		return Packet{}, false
	}
	p := s.in[0]
	s.in = s.in[1:]
	return p, true
}

func (s *scriptTransport) LocalID() PeerID { return 2 }
func (s *scriptTransport) Peers() []PeerID { return []PeerID{1} }
func (s *scriptTransport) Close() error    { return nil }

func (s *scriptTransport) deliver(payloads ...[]byte) {
	for _, p := range payloads {
		s.in = append(s.in, Packet{From: 1, Channel: ChannelUnreliable, Payload: p})
	}
}

// The header layout is documented in docs/en/net.md; these bytes pin it.
func TestReplicationWireHeader(t *testing.T) {
	if got, want := encodeSnapshotAck(0x01020304, 300), []byte{0x23, 0x04, 0x03, 0x02, 0x01, 0xac, 0x02}; !bytes.Equal(got, want) {
		t.Fatalf("ack = % x, want % x", got, want)
	}
	if got, want := encodeFullSnapshot(7, 1, nil), []byte{0x21, 7, 0, 0, 0, 1, 0}; !bytes.Equal(got, want) {
		t.Fatalf("empty full snapshot = % x, want % x", got, want)
	}
	if got, want := encodeDeltaSnapshot(7, 2, 1, nil, nil), []byte{0x22, 7, 0, 0, 0, 2, 1, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("empty delta = % x, want % x", got, want)
	}
}

// An ack of an earlier epoch names a tick of the old run; the new run's history may hold the same tick with other contents, so it must not become a delta baseline.
func TestServerWorldIgnoresAcksOfAnotherEpoch(t *testing.T) {
	tr := &scriptTransport{}
	w := NewServerWorld(tr)
	w.SetEpoch(2)
	w.Spawn(EntityState{})
	if err := w.Step(5); err != nil {
		t.Fatal(err)
	}
	tr.deliver(encodeSnapshotAck(1, 5))
	w.drainAcks()
	if ps := w.peers[1]; ps != nil && ps.hasAck {
		t.Fatal("an ack of epoch 1 became the baseline of epoch 2")
	}
	tr.deliver(encodeSnapshotAck(2, 5))
	w.drainAcks()
	if ps := w.peers[1]; ps == nil || !ps.hasAck || ps.ackedTick != 5 {
		t.Fatalf("the ack of the current epoch was not recorded: %+v", ps)
	}
}

// A restarted server sends full snapshots, but if the first packet of a new epoch to arrive is a delta, the client still drops the old run and asks for a full snapshot of the new one.
func TestClientWorldResyncsWhenANewEpochStartsWithADelta(t *testing.T) {
	tr := &scriptTransport{}
	c := NewClientWorld(tr, 1)
	var despawned []EntityID
	c.OnDespawn = func(id EntityID) { despawned = append(despawned, id) }
	tr.deliver(encodeFullSnapshot(1, 10, map[EntityID]EntityState{3: {ID: 3}}))
	if c.Poll() != 1 {
		t.Fatal("the first full snapshot did not apply")
	}
	tr.sent = nil
	tr.deliver(encodeDeltaSnapshot(2, 11, 10, nil, nil))
	if c.Poll() != 0 {
		t.Fatal("a delta of a new epoch applied against the old epoch's baseline")
	}
	if e, ok := c.Epoch(); !ok || e != 2 {
		t.Fatalf("Epoch = %d, %v; want 2", e, ok)
	}
	if len(despawned) != 1 || despawned[0] != 3 {
		t.Fatalf("despawned %v, want [3]", despawned)
	}
	want := appendReplHeader(nil, replMsgResync, 2)
	if len(tr.sent) != 1 || !bytes.Equal(tr.sent[0], want) {
		t.Fatalf("client sent % x, want one resync % x", tr.sent, want)
	}
}

// The client remembers the last maxRetiredEpochs epochs it left, and no more.
func TestClientWorldRetiresAtMostMaxRetiredEpochs(t *testing.T) {
	tr := &scriptTransport{}
	c := NewClientWorld(tr, 1)
	for epoch := uint32(1); epoch <= maxRetiredEpochs+2; epoch++ {
		tr.deliver(encodeFullSnapshot(epoch, 1, nil))
		if c.Poll() != 1 {
			t.Fatalf("epoch %d did not apply", epoch)
		}
	}
	if len(c.retired) != maxRetiredEpochs || c.retired[0] != 2 {
		t.Fatalf("retired = %v, want the %d epochs before the current one", c.retired, maxRetiredEpochs)
	}
	tr.deliver(encodeFullSnapshot(2, 5, nil))
	if c.Poll() != 0 {
		t.Fatal("a late snapshot of a retired epoch applied")
	}
	// Reset forgets the current epoch without retiring it, and keeps the retired ones.
	c.Reset()
	tr.deliver(encodeFullSnapshot(3, 1, nil), encodeFullSnapshot(maxRetiredEpochs+2, 1, nil))
	if c.Poll() != 1 {
		t.Fatal("after Reset, want the retired epoch refused and the current one accepted")
	}
}

func replicationSeeds() [][]byte {
	entities := map[EntityID]EntityState{
		1: {ID: 1, ComponentState: ComponentState{Kind: 2, X: 1, Y: -1, Props: []byte("a")}},
		2: {ID: 2, ComponentState: ComponentState{VX: 3, Rotation: 1, Flags: 9}},
	}
	moved := map[EntityID]EntityState{1: {ID: 1, ComponentState: ComponentState{X: 4}}, 5: {ID: 5}}
	seeds := [][]byte{
		encodeFullSnapshot(9, 4, entities),
		encodeDeltaSnapshot(9, 5, 4, entities, moved),
		encodeDeltaSnapshot(8, 6, 4, entities, moved),
		encodeSnapshotAck(9, 4),
		appendReplHeader(nil, replMsgResync, 9),
		{replMsgSnapshotFull, 4, 0},
		{0x31, 9, 0, 0, 0, 4, 0},
		{0x21},
	}
	parts, err := encodeFullSnapshotParts(9, 7, map[EntityID]EntityState{
		1: {ID: 1, ComponentState: ComponentState{Props: make([]byte, 200)}},
		2: {ID: 2, ComponentState: ComponentState{Props: make([]byte, 200)}},
	}, minSnapshotSize)
	if err != nil {
		panic(err)
	}
	return append(seeds, parts...)
}

// FuzzClientWorldPoll feeds arbitrary server packets to a client holding a snapshot of epoch 9: none may panic, and the client's epoch and snapshot state stay consistent.
func FuzzClientWorldPoll(f *testing.F) {
	for _, s := range replicationSeeds() {
		f.Add(s)
	}
	base := encodeFullSnapshot(9, 4, map[EntityID]EntityState{1: {ID: 1}, 2: {ID: 2}})
	f.Fuzz(func(t *testing.T, payload []byte) {
		tr := &scriptTransport{}
		c := NewClientWorld(tr, 1)
		tr.deliver(base, payload, payload)
		c.Poll()
		if !c.hasEpoch {
			t.Fatal("the client lost its epoch")
		}
		if c.hasLatest {
			if _, ok := c.byTick[c.latestTick]; !ok {
				t.Fatalf("latest tick %d has no snapshot", c.latestTick)
			}
		} else if len(c.snapshots) != 0 || len(c.byTick) != 0 {
			t.Fatalf("no latest tick but %d snapshots", len(c.snapshots))
		}
		// Only one new epoch can arrive, and only by leaving epoch 9, which then stays left.
		if len(c.retired) > 1 || (len(c.retired) == 1) != (c.epoch != 9) || (len(c.retired) == 1 && c.retired[0] != 9) {
			t.Fatalf("retired %v with epoch %d", c.retired, c.epoch)
		}
		if c.err != nil && (!errors.Is(c.err, ErrReplicationVersion) || payload[0]>>4 == ReplicationVersion) {
			t.Fatalf("Err = %v for a packet starting % x", c.err, payload[:1])
		}
	})
}

// FuzzServerWorldDrainAcks feeds arbitrary client packets to a server: none may panic, and an acknowledged baseline is always a tick of the server's history.
func FuzzServerWorldDrainAcks(f *testing.F) {
	for _, s := range replicationSeeds() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, payload []byte) {
		tr := &scriptTransport{}
		w := NewServerWorld(tr)
		w.SetEpoch(9)
		w.Spawn(EntityState{})
		for tick := uint32(3); tick <= 5; tick++ {
			if err := w.Step(tick); err != nil {
				t.Fatal(err)
			}
		}
		tr.deliver(payload)
		w.drainAcks()
		ps := w.peers[1]
		if ps == nil {
			return
		}
		if ps.hasAck {
			if _, ok := w.history[ps.ackedTick]; !ok {
				t.Fatalf("acknowledged tick %d is not in the history", ps.ackedTick)
			}
		}
		if ps.err != nil && !errors.Is(ps.err, ErrReplicationVersion) {
			t.Fatalf("PeerErr = %v", ps.err)
		}
	})
}
