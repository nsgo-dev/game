package netplay

import (
	"bytes"
	"errors"
	"slices"
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

// The header layout is documented in docs/en/netplay.md; these bytes pin it.
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

// A restarted server sends full snapshots, but if the first packet of a new epoch to arrive is a delta, the client keeps the world it has, which the delta cannot be applied to, and asks for a full snapshot of the new epoch; it follows the server into the new epoch when that arrives.
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
	if e, ok := c.Epoch(); !ok || e != 1 || len(despawned) != 0 {
		t.Fatalf("after the delta: Epoch = %d, %v, despawned %v; want epoch 1 and its world kept", e, ok, despawned)
	}
	want := appendReplHeader(nil, replMsgResync, 2)
	if len(tr.sent) != 1 || !bytes.Equal(tr.sent[0], want) {
		t.Fatalf("client sent % x, want one resync % x", tr.sent, want)
	}
	tr.deliver(encodeFullSnapshot(2, 1, map[EntityID]EntityState{4: {ID: 4}}))
	if c.Poll() != 1 {
		t.Fatal("the full snapshot of the new epoch did not apply")
	}
	if e, ok := c.Epoch(); !ok || e != 2 || len(despawned) != 1 || despawned[0] != 3 {
		t.Fatalf("after the full snapshot: Epoch = %d, %v, despawned %v; want epoch 2 and [3]", e, ok, despawned)
	}
}

// A corrupt snapshot of a new epoch is refused before the client leaves its epoch, so it cannot wipe the world.
func TestClientWorldKeepsItsEpochOnACorruptSnapshotOfANewOne(t *testing.T) {
	tr := &scriptTransport{}
	c := NewClientWorld(tr, 1)
	despawns := 0
	c.OnDespawn = func(EntityID) { despawns++ }
	tr.deliver(encodeFullSnapshot(1, 10, map[EntityID]EntityState{3: {ID: 3}}))
	if c.Poll() != 1 {
		t.Fatal("the first full snapshot did not apply")
	}
	full := encodeFullSnapshot(2, 1, map[EntityID]EntityState{4: {ID: 4, ComponentState: ComponentState{Props: []byte("abc")}}})
	parts, err := encodeFullSnapshotParts(2, 1, map[EntityID]EntityState{
		4: {ID: 4, ComponentState: ComponentState{Props: make([]byte, 200)}},
		5: {ID: 5, ComponentState: ComponentState{Props: make([]byte, 200)}},
	}, minSnapshotSize)
	if err != nil {
		t.Fatal(err)
	}
	tr.deliver(full[:len(full)-2], parts[0], appendReplHeader(nil, replMsgSnapshotFullPart, 2))
	if c.Poll() != 0 {
		t.Fatal("a corrupt or incomplete snapshot of a new epoch applied")
	}
	if e, _ := c.Epoch(); e != 1 || despawns != 0 {
		t.Fatalf("Epoch = %d after %d despawns; want epoch 1 and its world kept", e, despawns)
	}
	if _, ok := c.Entity(3); !ok {
		t.Fatal("the world of epoch 1 was dropped")
	}
}

// Late parts of a retired epoch that arrive while a split full snapshot of the current epoch is assembled neither apply nor disturb the assembly.
func TestClientWorldIgnoresRetiredPartsDuringAnAssembly(t *testing.T) {
	tr := &scriptTransport{}
	c := NewClientWorld(tr, 1)
	big := map[EntityID]EntityState{
		1: {ID: 1, ComponentState: ComponentState{Props: make([]byte, 200)}},
		2: {ID: 2, ComponentState: ComponentState{Props: make([]byte, 200)}},
	}
	late, err := encodeFullSnapshotParts(1, 50, big, minSnapshotSize)
	if err != nil {
		t.Fatal(err)
	}
	tr.deliver(encodeFullSnapshot(1, 10, nil), encodeFullSnapshot(2, 1, nil))
	if c.Poll() != 2 {
		t.Fatal("the snapshots of epochs 1 and 2 did not apply")
	}
	parts, err := encodeFullSnapshotParts(2, 2, big, minSnapshotSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) < 2 || len(late) < 2 {
		t.Fatalf("got %d and %d parts, want splits", len(parts), len(late))
	}
	tr.deliver(parts[0])
	tr.deliver(late...)
	tr.deliver(parts[1:]...)
	if c.Poll() != 1 {
		t.Fatal("the split snapshot of epoch 2 did not apply once")
	}
	if e, _ := c.Epoch(); e != 2 {
		t.Fatalf("Epoch = %d, want 2", e)
	}
	if tick, _ := c.LatestTick(); tick != 2 {
		t.Fatalf("LatestTick = %d, want 2", tick)
	}
}

// A client that left the live epoch by mistake, because a late snapshot of an epoch it had never seen reached it after the live epoch's first one, follows the live epoch again once it has sent epochRecoveryPackets snapshots in a row, and late snapshots of the dead epoch are then ignored.
func TestClientWorldRecoversTheLiveEpochAfterAReorderedRestart(t *testing.T) {
	srvTr, cliTr := &scriptTransport{}, &scriptTransport{}
	w := NewServerWorld(srvTr)
	c := NewClientWorld(cliTr, 1)
	w.SetEpoch(1)
	hero := w.Spawn(EntityState{})
	if err := w.Step(1); err != nil {
		t.Fatal(err)
	}
	lateFull := srvTr.sent[len(srvTr.sent)-1]
	srvTr.sent = nil

	// The server starts a new epoch before the client has seen the old one, and the old epoch's snapshot arrives after the new one's.
	w.SetEpoch(2)
	pump := func(tick uint32) int {
		if err := w.Step(tick); err != nil {
			t.Fatal(err)
		}
		cliTr.deliver(srvTr.sent...)
		srvTr.sent = nil
		n := c.Poll()
		srvTr.deliver(cliTr.sent...)
		cliTr.sent = nil
		return n
	}
	if pump(1) != 1 {
		t.Fatal("the first snapshot of epoch 2 did not apply")
	}
	cliTr.deliver(lateFull)
	if c.Poll() != 1 {
		t.Fatal("the late snapshot of the unseen epoch 1 did not apply")
	}
	if e, _ := c.Epoch(); e != 1 {
		t.Fatalf("Epoch = %d, want the client misled into epoch 1", e)
	}

	recovered := uint32(0)
	for tick := uint32(2); tick < 2+epochRecoveryPackets+4; tick++ {
		w.Update(hero, EntityState{ComponentState: ComponentState{X: float32(tick)}})
		if pump(tick) == 1 {
			if e, _ := c.Epoch(); e == 2 {
				recovered = tick
				break
			}
		}
	}
	if recovered == 0 {
		t.Fatal("the client never followed the live epoch 2 again")
	}
	if recovered < 1+epochRecoveryPackets {
		t.Fatalf("the client followed the retired epoch 2 back at tick %d, before %d snapshots of it", recovered, epochRecoveryPackets)
	}
	for tick := recovered + 1; tick <= recovered+3; tick++ {
		w.Update(hero, EntityState{ComponentState: ComponentState{X: float32(tick)}})
		if pump(tick) != 1 {
			t.Fatalf("tick %d of the recovered epoch did not apply", tick)
		}
	}
	if e, ok := c.Entity(hero); !ok || e.X != float32(recovered+3) {
		t.Fatalf("entity %d = %+v, %v", hero, e, ok)
	}
	cliTr.deliver(lateFull)
	if c.Poll() != 0 {
		t.Fatal("the late snapshot of epoch 1 applied again")
	}
}

// A snapshot of the current epoch between the snapshots of a retired one starts the count over, so a late burst shorter than epochRecoveryPackets never brings a retired epoch back.
func TestClientWorldCountsRetiredSnapshotsInARow(t *testing.T) {
	tr := &scriptTransport{}
	c := NewClientWorld(tr, 1)
	tr.deliver(encodeFullSnapshot(1, 1, nil), encodeFullSnapshot(2, 1, nil))
	if c.Poll() != 2 {
		t.Fatal("the snapshots of epochs 1 and 2 did not apply")
	}
	tick := uint32(1)
	for range 3 {
		for range epochRecoveryPackets - 1 {
			tick++
			tr.deliver(encodeFullSnapshot(1, tick, nil))
		}
		tick++
		tr.deliver(encodeFullSnapshot(2, tick, nil))
	}
	if got := c.Poll(); got != 3 {
		t.Fatalf("Poll applied %d snapshots, want the 3 of epoch 2", got)
	}
	if e, _ := c.Epoch(); e != 2 {
		t.Fatalf("Epoch = %d, want 2", e)
	}
	for range epochRecoveryPackets {
		tick++
		tr.deliver(encodeFullSnapshot(1, tick, nil))
	}
	if got := c.Poll(); got != 1 {
		t.Fatalf("Poll applied %d snapshots, want the one that brings epoch 1 back", got)
	}
	if e, _ := c.Epoch(); e != 1 || !slices.Equal(c.retired, []uint32{2}) {
		t.Fatalf("Epoch = %d, retired %v; want epoch 1 and [2]", e, c.retired)
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
