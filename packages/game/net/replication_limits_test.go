package net_test

import (
	"bytes"
	"errors"
	"math"
	"testing"

	gamenet "github.com/nsgo-dev/game/packages/game/net"
)

// packetLimit refuses payloads above max, as UDP and relay transports do, and can drop the next packets it is asked to send.
type packetLimit struct {
	gamenet.Transport
	max     int
	drop    int
	largest int
}

func (p *packetLimit) Send(to gamenet.PeerID, ch gamenet.Channel, payload []byte) error {
	if len(payload) > p.max {
		return gamenet.ErrPayloadTooLarge
	}
	p.largest = max(p.largest, len(payload))
	if p.drop > 0 {
		p.drop--
		return nil
	}
	return p.Transport.Send(to, ch, payload)
}

func bigEntity(i int, fill byte) gamenet.EntityState {
	return gamenet.EntityState{ComponentState: gamenet.ComponentState{
		Kind:  uint16(i % 7),
		X:     float32(i),
		Y:     float32(-i),
		Props: bytes.Repeat([]byte{fill}, 100),
	}}
}

func checkClientMatches(t *testing.T, srv *gamenet.ServerWorld, cli *gamenet.ClientWorld) {
	t.Helper()
	for _, want := range srv.Entities() {
		got, ok := cli.Entity(want.ID)
		if !ok || got.X != want.X || got.Kind != want.Kind || !bytes.Equal(got.Props, want.Props) {
			t.Fatalf("client entity %d = %+v, %v; want %+v", want.ID, got, ok, want)
		}
	}
}

// #546: a full snapshot larger than the transport's packet limit failed to send on every Step, so a new or resyncing peer never received the world again.
func TestReplicationSplitsAFullSnapshotLargerThanAPacket(t *testing.T) {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{})
	srvT := &packetLimit{Transport: hub.AddPeer(1), max: 2000}
	cliT := hub.AddPeer(2)
	srv := gamenet.NewServerWorld(srvT)
	srv.SetMaxSnapshotSize(2000)
	cli := gamenet.NewClientWorld(cliT, 1)
	spawned := 0
	cli.OnSpawn = func(gamenet.EntityState) { spawned++ }

	const n = 300 // about 35 KB in one full snapshot
	for i := range n {
		srv.Spawn(bigEntity(i, 'a'))
	}
	if err := srv.Step(1); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if srvT.largest > 2000 {
		t.Fatalf("largest packet = %d bytes, above the 2000-byte limit", srvT.largest)
	}
	if got := cli.Poll(); got != 1 {
		t.Fatalf("Poll applied %d snapshots, want the reassembled full snapshot once", got)
	}
	if tick, ok := cli.LatestTick(); !ok || tick != 1 || spawned != n {
		t.Fatalf("latest tick = %d, %v; spawned %d; want tick 1 and %d spawns", tick, ok, spawned, n)
	}
	checkClientMatches(t, srv, cli)
	st := srv.Stats()
	if st.FullSnapshotsSent != 1 || st.SnapshotsSent != 1 || st.PacketsSent < 10 {
		t.Fatalf("stats = %+v, want one full snapshot sent as many packets", st)
	}

	// A delta that rewrites every entity is as large as the full snapshot; it is sent as a split full snapshot instead.
	for _, e := range srv.Entities() {
		srv.Update(e.ID, bigEntity(int(e.ID), 'b'))
	}
	if err := srv.Step(2); err != nil {
		t.Fatalf("Step 2: %v", err)
	}
	if got := cli.Poll(); got != 1 {
		t.Fatalf("Poll applied %d snapshots after the oversized delta", got)
	}
	checkClientMatches(t, srv, cli)
	if st := srv.Stats(); st.FullSnapshotsSent != 2 || st.DeltaSnapshotsSent != 0 {
		t.Fatalf("stats = %+v, want the oversized delta replaced by a full snapshot", st)
	}

	// A small change goes back to a single delta packet.
	first := srv.Entities()[0]
	first.X = 1000
	srv.Update(first.ID, first)
	if err := srv.Step(3); err != nil {
		t.Fatalf("Step 3: %v", err)
	}
	if got := cli.Poll(); got != 1 {
		t.Fatalf("Poll applied %d snapshots for the small delta", got)
	}
	checkClientMatches(t, srv, cli)
	if st := srv.Stats(); st.DeltaSnapshotsSent != 1 {
		t.Fatalf("stats = %+v, want one delta", st)
	}
}

func TestReplicationRecoversFromALostSnapshotPart(t *testing.T) {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{})
	srvT := &packetLimit{Transport: hub.AddPeer(1), max: 1000}
	cli := gamenet.NewClientWorld(hub.AddPeer(2), 1)
	srv := gamenet.NewServerWorld(srvT)
	srv.SetMaxSnapshotSize(1000)
	for i := range 50 {
		srv.Spawn(bigEntity(i, 'c'))
	}

	srvT.drop = 1 // lose the first part of tick 1
	if err := srv.Step(1); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if got := cli.Poll(); got != 0 {
		t.Fatalf("Poll applied %d snapshots with a part missing", got)
	}
	if _, ok := cli.LatestTick(); ok {
		t.Fatal("client has a snapshot although a part was lost")
	}
	// No ack came back, so the next Step sends the whole snapshot again.
	if err := srv.Step(2); err != nil {
		t.Fatalf("Step 2: %v", err)
	}
	if got := cli.Poll(); got != 1 {
		t.Fatalf("Poll applied %d snapshots after the resend", got)
	}
	if tick, _ := cli.LatestTick(); tick != 2 {
		t.Fatalf("latest tick = %d, want 2", tick)
	}
	checkClientMatches(t, srv, cli)
}

func TestReplicationReportsAnEntityTooLargeForAPacket(t *testing.T) {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{})
	hub.AddPeer(2)
	srv := gamenet.NewServerWorld(hub.AddPeer(1))
	srv.SetMaxSnapshotSize(512)
	srv.Spawn(gamenet.EntityState{ComponentState: gamenet.ComponentState{Props: make([]byte, 1000)}})
	srv.Spawn(gamenet.EntityState{ComponentState: gamenet.ComponentState{Props: make([]byte, 1000)}})
	if err := srv.Step(1); !errors.Is(err, gamenet.ErrPayloadTooLarge) {
		t.Fatalf("Step = %v, want ErrPayloadTooLarge naming the entity", err)
	}
}

// #546: ticks are uint32 and were compared as plain integers, so after the counter wrapped every snapshot looked older than the last one and the client froze.
func TestReplicationSurvivesTickWraparound(t *testing.T) {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{})
	srv := gamenet.NewServerWorld(hub.AddPeer(1))
	cli := gamenet.NewClientWorld(hub.AddPeer(2), 1)
	id := srv.Spawn(gamenet.EntityState{})

	start := uint32(math.MaxUint32 - 3)
	for i := range uint32(8) {
		tick := start + i // wraps from 4294967295 to 0
		srv.Update(id, gamenet.EntityState{ComponentState: gamenet.ComponentState{X: float32(i * 10)}})
		if err := srv.Step(tick); err != nil {
			t.Fatalf("Step(%d): %v", tick, err)
		}
		if got := cli.Poll(); got != 1 {
			t.Fatalf("Poll at tick %d applied %d snapshots, want 1", tick, got)
		}
		if latest, _ := cli.LatestTick(); latest != tick {
			t.Fatalf("latest tick = %d, want %d", latest, tick)
		}
		if e, _ := cli.Entity(id); e.X != float32(i*10) {
			t.Fatalf("tick %d: X = %v, want %v", tick, e.X, i*10)
		}
	}
	if st := srv.Stats(); st.DeltaSnapshotsSent != 7 || st.FullSnapshotsSent != 1 {
		t.Fatalf("stats = %+v, want deltas to continue across the wrap", st)
	}

	// Interpolate between tick 4294967295 (X=30) and tick 0 (X=40), named either way round the wrap.
	for _, at := range []float64{float64(math.MaxUint32) + 0.5, -0.5} {
		got := cli.Interpolate(at)
		if len(got) != 1 || got[0].X != 35 {
			t.Fatalf("Interpolate(%v) = %+v, want X 35", at, got)
		}
	}
	// A render time past the latest snapshot holds the latest state.
	if got := cli.Interpolate(10); len(got) != 1 || got[0].X != 70 {
		t.Fatalf("Interpolate(10) = %+v, want the latest X 70", got)
	}
}

func TestClientWorldRejectsOlderTicksAcrossTheWrap(t *testing.T) {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{})
	srv := gamenet.NewServerWorld(hub.AddPeer(1))
	cli := gamenet.NewClientWorld(hub.AddPeer(2), 1)
	srv.Spawn(gamenet.EntityState{})
	if err := srv.Step(5); err != nil {
		t.Fatal(err)
	}
	cli.Poll()
	// Tick 4294967290 is ten ticks before tick 5 once wraparound is accounted for: a late, stale snapshot.
	if err := srv.Step(math.MaxUint32 - 5); err != nil {
		t.Fatal(err)
	}
	if got := cli.Poll(); got != 0 {
		t.Fatalf("Poll applied %d stale snapshots", got)
	}
	if latest, _ := cli.LatestTick(); latest != 5 {
		t.Fatalf("latest tick = %d, want 5", latest)
	}
}

// #546: a server that starts its tick count over in the same epoch, for a new match without SetEpoch, sends ticks the client takes as stale. Reset lets the client start over. (A restarted ServerWorld picks a new epoch, which the client follows on its own: see replication_epoch_test.go.)
func TestClientWorldResetAcceptsATickCountThatStartsOver(t *testing.T) {
	hub := gamenet.NewLoopbackHub(gamenet.LoopbackConfig{})
	cli := gamenet.NewClientWorld(hub.AddPeer(2), 1)
	var spawned, despawned []gamenet.EntityID
	cli.OnSpawn = func(e gamenet.EntityState) { spawned = append(spawned, e.ID) }
	cli.OnDespawn = func(id gamenet.EntityID) { despawned = append(despawned, id) }

	srv := gamenet.NewServerWorld(hub.AddPeer(1))
	a := srv.Spawn(gamenet.EntityState{})
	b := srv.Spawn(gamenet.EntityState{})
	if err := srv.Step(100000); err != nil {
		t.Fatal(err)
	}
	if got := cli.Poll(); got != 1 {
		t.Fatalf("Poll applied %d snapshots of the first match, want 1", got)
	}

	// The next match starts on the same world, at tick 0, in the same epoch.
	srv.Despawn(a)
	srv.Despawn(b)
	c := srv.Spawn(gamenet.EntityState{ComponentState: gamenet.ComponentState{X: 7}})
	if err := srv.Step(0); err != nil {
		t.Fatal(err)
	}
	if got := cli.Poll(); got != 0 {
		t.Fatalf("Poll applied %d snapshots at tick 0 before Reset; serial-number order should take them as stale", got)
	}

	spawned = nil
	cli.Reset()
	if _, ok := cli.LatestTick(); ok {
		t.Fatal("LatestTick reports a tick after Reset")
	}
	if len(despawned) != 2 || despawned[0] != min(a, b) || despawned[1] != max(a, b) {
		t.Fatalf("Reset despawned %v, want %d and %d in order", despawned, a, b)
	}
	if _, ok := cli.Epoch(); ok {
		t.Fatal("Epoch reports an epoch after Reset")
	}
	if err := srv.Step(1); err != nil {
		t.Fatal(err)
	}
	if got := cli.Poll(); got != 1 {
		t.Fatalf("Poll applied %d snapshots after Reset, want 1", got)
	}
	if tick, ok := cli.LatestTick(); !ok || tick != 1 {
		t.Fatalf("latest tick = %d, %v; want 1", tick, ok)
	}
	if e, ok := cli.Entity(c); !ok || e.X != 7 || len(spawned) != 1 || spawned[0] != c {
		t.Fatalf("after Reset entity %d = %+v, %v; spawned %v", c, e, ok, spawned)
	}
}
