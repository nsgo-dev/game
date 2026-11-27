package netplay_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/netplay"
)

// recorder keeps a copy of every payload sent through it, so a test can deliver one again late.
type recorder struct {
	netplay.Transport
	sent [][]byte
}

func (r *recorder) Send(to netplay.PeerID, ch netplay.Channel, payload []byte) error {
	r.sent = append(r.sent, slices.Clone(payload))
	return r.Transport.Send(to, ch, payload)
}

func moveRight(e *netplay.EntityState, input any) { e.X += input.(float32) }

// #745: a server that restarts sends snapshots of a new epoch, and the client drops the old run's interpolation and prediction state and resyncs without the game calling Reset.
func TestClientWorldFollowsAServerRestartWithoutReset(t *testing.T) {
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{})
	srvT := &recorder{Transport: hub.AddPeer(1)}
	cli := netplay.NewClientWorld(hub.AddPeer(2), 1)
	var spawned []netplay.EntityState
	var despawned []netplay.EntityID
	cli.OnSpawn = func(e netplay.EntityState) { spawned = append(spawned, e) }
	cli.OnDespawn = func(id netplay.EntityID) { despawned = append(despawned, id) }

	old := netplay.NewServerWorld(srvT)
	old.SetEpoch(1)
	hero := old.Spawn(netplay.EntityState{ComponentState: netplay.ComponentState{X: 10}})
	coin := old.Spawn(netplay.EntityState{ComponentState: netplay.ComponentState{X: 99}})
	for tick := uint32(1); tick <= 20; tick++ {
		old.Update(hero, netplay.EntityState{ComponentState: netplay.ComponentState{X: 10 + float32(tick)}})
		if err := old.Step(tick); err != nil {
			t.Fatal(err)
		}
		if got := cli.Poll(); got != 1 {
			t.Fatalf("tick %d: Poll applied %d snapshots, want 1", tick, got)
		}
	}
	if s := old.Stats(); s.DeltaSnapshotsSent == 0 {
		t.Fatalf("the first run sent no deltas: %+v", s)
	}
	lateFull, lateDelta := srvT.sent[0], srvT.sent[len(srvT.sent)-1]
	cli.Predict(hero, float32(5), moveRight)
	if cli.PendingPredictions(hero) != 1 {
		t.Fatal("the prediction was not buffered")
	}

	// The server restarts mid-stream: a new world, with its tick count back at 1 and new entities that reuse the old IDs.
	next := netplay.NewServerWorld(srvT)
	next.SetEpoch(2)
	reborn := next.Spawn(netplay.EntityState{ComponentState: netplay.ComponentState{X: -3}})
	spawned = nil
	if err := next.Step(1); err != nil {
		t.Fatal(err)
	}
	if got := cli.Poll(); got != 1 {
		t.Fatalf("Poll applied %d snapshots of the restarted server, want 1", got)
	}
	if epoch, ok := cli.Epoch(); !ok || epoch != 2 {
		t.Fatalf("Epoch = %d, %v; want 2", epoch, ok)
	}
	if tick, ok := cli.LatestTick(); !ok || tick != 1 {
		t.Fatalf("LatestTick = %d, %v; want 1", tick, ok)
	}
	if !slices.Equal(despawned, []netplay.EntityID{min(hero, coin), max(hero, coin)}) {
		t.Fatalf("despawned %v, want the old run's %d and %d in order", despawned, hero, coin)
	}
	if len(spawned) != 1 || spawned[0].ID != reborn || spawned[0].X != -3 {
		t.Fatalf("spawned %+v, want only entity %d at x -3", spawned, reborn)
	}
	if n := cli.PendingPredictions(hero); n != 0 {
		t.Fatalf("%d predictions of the old run survived the restart", n)
	}
	if got := cli.Interpolate(0.5); len(got) != 1 || got[0].ID != reborn || got[0].X != -3 {
		t.Fatalf("Interpolate after the restart = %+v, want only the new run's entity", got)
	}

	// The new run's acknowledgments are accepted, so it moves on to deltas.
	for tick := uint32(2); tick <= 10; tick++ {
		next.Update(reborn, netplay.EntityState{ComponentState: netplay.ComponentState{X: -3 + float32(tick)}})
		if err := next.Step(tick); err != nil {
			t.Fatal(err)
		}
		if got := cli.Poll(); got != 1 {
			t.Fatalf("tick %d of the new run: Poll applied %d snapshots, want 1", tick, got)
		}
	}
	if s := next.Stats(); s.DeltaSnapshotsSent == 0 || s.FullSnapshotsSent != 1 {
		t.Fatalf("the new run sent %d full and %d delta snapshots, want 1 full then deltas", s.FullSnapshotsSent, s.DeltaSnapshotsSent)
	}

	// Snapshots of the old run that arrive late are ignored, even a full one whose tick is newer.
	spawned, despawned = nil, nil
	for _, late := range [][]byte{lateFull, lateDelta} {
		if err := srvT.Transport.Send(2, netplay.ChannelUnreliable, late); err != nil {
			t.Fatal(err)
		}
	}
	if got := cli.Poll(); got != 0 {
		t.Fatalf("Poll applied %d late snapshots of the old run", got)
	}
	if epoch, _ := cli.Epoch(); epoch != 2 {
		t.Fatalf("a late snapshot moved the client back to epoch %d", epoch)
	}
	if e, ok := cli.Entity(reborn); !ok || e.X != 7 || len(spawned) != 0 || len(despawned) != 0 {
		t.Fatalf("after the late snapshots entity %d = %+v, %v; spawned %v, despawned %v", reborn, e, ok, spawned, despawned)
	}
}

// #745: SetEpoch lets a server that keeps running start a new match with its ticks back at zero, and its clients follow without Reset.
func TestServerWorldSetEpochResyncsClients(t *testing.T) {
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{})
	srv := netplay.NewServerWorld(hub.AddPeer(1))
	cli := netplay.NewClientWorld(hub.AddPeer(2), 1)
	first := srv.Spawn(netplay.EntityState{})
	for tick := uint32(500); tick <= 505; tick++ {
		if err := srv.Step(tick); err != nil {
			t.Fatal(err)
		}
		cli.Poll()
	}
	before, _ := cli.Epoch()
	if before != srv.Epoch() {
		t.Fatalf("client epoch %d, server epoch %d", before, srv.Epoch())
	}

	srv.SetEpoch(srv.Epoch() + 1)
	srv.Despawn(first)
	second := srv.Spawn(netplay.EntityState{ComponentState: netplay.ComponentState{Y: 4}})
	full := srv.Stats().FullSnapshotsSent
	if err := srv.Step(0); err != nil {
		t.Fatal(err)
	}
	if srv.Stats().FullSnapshotsSent != full+1 {
		t.Fatal("SetEpoch did not make the next Step send a full snapshot")
	}
	if got := cli.Poll(); got != 1 {
		t.Fatalf("Poll applied %d snapshots of the new epoch, want 1", got)
	}
	if tick, _ := cli.LatestTick(); tick != 0 {
		t.Fatalf("LatestTick = %d, want 0", tick)
	}
	if _, ok := cli.Entity(first); ok {
		t.Fatal("the old match's entity survived the new epoch")
	}
	if e, ok := cli.Entity(second); !ok || e.Y != 4 {
		t.Fatalf("entity %d = %+v, %v", second, e, ok)
	}
}

// #745: a peer built before the epoch speaks replication protocol version 1. Neither side misreads its messages, and each says why nothing arrives.
func TestReplicationRefusesAnotherProtocolVersion(t *testing.T) {
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{})
	srvT := hub.AddPeer(1)
	cliT := hub.AddPeer(2)
	srv := netplay.NewServerWorld(srvT)
	cli := netplay.NewClientWorld(cliT, 1)

	// A version 1 full snapshot: type 1, tick 7, one entity 1 with x set.
	v1Full := []byte{1, 7, 1, 1, 0b10, 0, 0, 0x80, 0x3f}
	// A version 3 full snapshot, from a newer build.
	v3Full := append([]byte{0x31, 0, 0, 0, 0}, v1Full[1:]...)
	for _, tc := range []struct {
		msg  []byte
		want string
	}{{v1Full, "version 1"}, {v3Full, "version 3"}} {
		if err := srvT.Send(2, netplay.ChannelUnreliable, tc.msg); err != nil {
			t.Fatal(err)
		}
		if got := cli.Poll(); got != 0 {
			t.Fatalf("Poll applied %d snapshots of another version", got)
		}
		err := cli.Err()
		if !errors.Is(err, netplay.ErrReplicationVersion) || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("Err = %v, want ErrReplicationVersion naming %s", err, tc.want)
		}
	}
	if _, ok := cli.Entity(1); ok {
		t.Fatal("the client applied a snapshot of another version")
	}

	// A version 1 acknowledgment: type 3, tick 7.
	if err := cliT.Send(1, netplay.ChannelUnreliable, []byte{3, 7}); err != nil {
		t.Fatal(err)
	}
	srv.Spawn(netplay.EntityState{})
	if err := srv.Step(8); err != nil {
		t.Fatal(err)
	}
	if err := srv.PeerErr(2); !errors.Is(err, netplay.ErrReplicationVersion) || !strings.Contains(err.Error(), "version 1") {
		t.Fatalf("PeerErr = %v, want ErrReplicationVersion naming version 1", err)
	}

	// A snapshot of this version clears the client's error, and its acknowledgment the server's.
	if got := cli.Poll(); got != 1 {
		t.Fatalf("Poll applied %d snapshots of this version, want 1", got)
	}
	if err := cli.Err(); err != nil {
		t.Fatalf("Err after a valid snapshot = %v", err)
	}
	if err := srv.Step(9); err != nil {
		t.Fatal(err)
	}
	if err := srv.PeerErr(2); err != nil {
		t.Fatalf("PeerErr after a valid acknowledgment = %v", err)
	}
}
