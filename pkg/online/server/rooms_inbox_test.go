package server_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/netplay"
	"github.com/nsgo-dev/game/pkg/online/server"
)

func newInboxRoom(t *testing.T, peers ...netplay.PeerID) (*server.Rooms, *server.Room) {
	t.Helper()
	rs := server.NewRooms()
	room, err := rs.Create(len(peers), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range peers {
		if _, err := rs.Join(room.ID(), p); err != nil {
			t.Fatal(err)
		}
	}
	return rs, room
}

func drainPeer(room *server.Room, peer netplay.PeerID) []netplay.Packet {
	var out []netplay.Packet
	for {
		p, ok := room.Recv(peer)
		if !ok {
			return out
		}
		out = append(out, p)
	}
}

// A match that never reads its inbox must not let a member's queue grow past the default per-member bound.
func TestRoomInboxStalledStaysBoundedByDefault(t *testing.T) {
	_, room := newInboxRoom(t, 1, 2)
	for i := range 10 * server.DefaultRoomInboxPerPeer {
		for _, peer := range []netplay.PeerID{1, 2} {
			if err := room.Deliver(peer, netplay.Packet{From: peer, Channel: netplay.ChannelUnreliable, Seq: uint32(i)}); err != nil {
				t.Fatalf("unreliable deliver %d from %d: %v", i, peer, err)
			}
		}
	}
	for _, peer := range []netplay.PeerID{1, 2} {
		got := drainPeer(room, peer)
		if len(got) != server.DefaultRoomInboxPerPeer {
			t.Fatalf("peer %d queued %d packets, want %d", peer, len(got), server.DefaultRoomInboxPerPeer)
		}
		// The oldest are dropped, so the newest survive in order.
		if first, want := got[0].Seq, uint32(9*server.DefaultRoomInboxPerPeer); first != want {
			t.Fatalf("peer %d oldest kept seq = %d, want %d", peer, first, want)
		}
	}
}

func TestRoomInboxPerPeerLimitAndStats(t *testing.T) {
	rs, room := newInboxRoom(t, 1, 2)
	rs.SetLimits(server.RoomLimits{PerPeer: 4, Warning: 6})
	for i := range 20 {
		if err := room.Deliver(1, netplay.Packet{Channel: netplay.ChannelUnreliable, Seq: uint32(i)}); err != nil {
			t.Fatal(err)
		}
	}
	st := room.InboxStats()
	if st.Queued != 4 || st.Dropped != 16 {
		t.Fatalf("stats after flood = %+v, want 4 queued, 16 dropped", st)
	}
	// Reliable packets evict queued unreliable ones first, then refuse.
	for i := range 4 {
		if err := room.Deliver(1, netplay.Packet{Channel: netplay.ChannelReliableOrdered, Seq: uint32(100 + i)}); err != nil {
			t.Fatalf("reliable %d: %v", i, err)
		}
	}
	if err := room.Deliver(1, netplay.Packet{Channel: netplay.ChannelReliableOrdered, Seq: 104}); !errors.Is(err, server.ErrRoomInboxFull) {
		t.Fatalf("reliable overflow = %v, want ErrRoomInboxFull", err)
	}
	// An unreliable packet arriving at a queue full of reliable ones is the one dropped.
	if err := room.Deliver(1, netplay.Packet{Channel: netplay.ChannelUnreliable, Seq: 200}); err != nil {
		t.Fatal(err)
	}
	st = room.InboxStats()
	if st.Queued != 4 || st.Dropped != 21 || st.Refused != 1 {
		t.Fatalf("stats after reliable overflow = %+v", st)
	}
	got := drainPeer(room, 1)
	var seqs []uint32
	for _, p := range got {
		seqs = append(seqs, p.Seq)
	}
	if !slices.Equal(seqs, []uint32{100, 101, 102, 103}) {
		t.Fatalf("kept seqs = %v, want reliable packets in order", seqs)
	}
	// The other member's queue is separate, and the warning counts once per crossing.
	for i := range 4 {
		_ = room.Deliver(1, netplay.Packet{Channel: netplay.ChannelUnreliable, Seq: uint32(i)})
		_ = room.Deliver(2, netplay.Packet{Channel: netplay.ChannelUnreliable, Seq: uint32(i)})
	}
	st = room.InboxStats()
	if st.Queued != 8 || st.Warnings != 1 || !st.Backlog {
		t.Fatalf("stats after warning = %+v, want 8 queued, 1 warning, backlog", st)
	}
	// Four of six is above half, so the backlog stays raised until the room is drained further.
	drainPeer(room, 1)
	if st = room.InboxStats(); !st.Backlog || st.Queued != 4 {
		t.Fatalf("stats after partial drain = %+v, want backlog still raised", st)
	}
	drainPeer(room, 2)
	if st = room.InboxStats(); st.Backlog || st.Queued != 0 {
		t.Fatalf("stats after drain = %+v, want backlog cleared", st)
	}
	for i := range 4 {
		_ = room.Deliver(1, netplay.Packet{Channel: netplay.ChannelUnreliable, Seq: uint32(i)})
		_ = room.Deliver(2, netplay.Packet{Channel: netplay.ChannelUnreliable, Seq: uint32(i)})
	}
	if st = room.InboxStats(); st.Warnings != 2 {
		t.Fatalf("warnings after rearm = %d, want 2", st.Warnings)
	}
}

func TestRoomSetLimitsTrimsQueuedUnreliable(t *testing.T) {
	rs, room := newInboxRoom(t, 1)
	for i := range 10 {
		_ = room.Deliver(1, netplay.Packet{Channel: netplay.ChannelUnreliable, Seq: uint32(i)})
	}
	rs.SetLimits(server.RoomLimits{PerPeer: 3})
	if st := room.InboxStats(); st.Queued != 3 || st.Dropped != 7 {
		t.Fatalf("stats after lowering = %+v", st)
	}
	if p, _ := room.Recv(1); p.Seq != 7 {
		t.Fatalf("oldest kept = %d, want 7", p.Seq)
	}
	// Rooms created later use the same limits.
	later, err := rs.Create(1, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Join(later.ID(), 9); err != nil {
		t.Fatal(err)
	}
	for i := range 10 {
		_ = later.Deliver(9, netplay.Packet{Channel: netplay.ChannelUnreliable, Seq: uint32(i)})
	}
	if st := later.InboxStats(); st.Queued != 3 {
		t.Fatalf("later room queued = %d, want 3", st.Queued)
	}
}

func TestRoomDrainInterleavesFlooder(t *testing.T) {
	_, room := newInboxRoom(t, 5, 3)
	for i := range 60 {
		_ = room.Deliver(5, netplay.Packet{From: 5, Channel: netplay.ChannelUnreliable, Seq: uint32(i)})
	}
	for i := range 2 {
		_ = room.Deliver(3, netplay.Packet{From: 3, Channel: netplay.ChannelUnreliable, Seq: uint32(i)})
	}
	var order []string
	n := room.Drain(3, func(p netplay.Packet) { order = append(order, fmt.Sprintf("%d:%d", p.From, p.Seq)) })
	want := []string{"5:0", "3:0", "5:1", "3:1", "5:2"}
	if n != len(want) || !slices.Equal(order, want) {
		t.Fatalf("Drain = %d %v, want %d %v", n, order, len(want), want)
	}
	if st := room.InboxStats(); st.Queued != 57 {
		t.Fatalf("queued after drain = %d, want 57", st.Queued)
	}
	if got := room.Drain(0, func(netplay.Packet) { t.Fatal("budget 0 handed a packet") }); got != 0 {
		t.Fatalf("Drain(0) = %d", got)
	}
	// fn runs without the room's lock held, so it may deliver back into the room.
	room.Drain(1, func(p netplay.Packet) { _ = room.Deliver(3, p) })
}

func TestRoomMembersKeepJoinOrder(t *testing.T) {
	empty, err := server.NewRooms().Create(2, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := empty.Members(); got == nil || len(got) != 0 {
		t.Fatalf("Members of an empty room = %#v, want a non-nil empty slice", got)
	}
	rs, room := newInboxRoom(t, 9, 2, 7, 4)
	if got := room.Members(); !slices.Equal(got, []netplay.PeerID{9, 2, 7, 4}) {
		t.Fatalf("Members = %v", got)
	}
	rs.Leave(room.ID(), 2)
	if _, err := rs.Join(room.ID(), 2); err != nil {
		t.Fatal(err)
	}
	if got := room.Members(); !slices.Equal(got, []netplay.PeerID{9, 7, 4, 2}) {
		t.Fatalf("Members after rejoin = %v", got)
	}
}

func TestRoomEndAndCloseClearInbox(t *testing.T) {
	rs, room := newInboxRoom(t, 1, 2)
	if err := room.Start(); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		_ = room.Deliver(1, netplay.Packet{Seq: uint32(i)})
		_ = room.Deliver(2, netplay.Packet{Seq: uint32(i)})
	}
	room.End()
	if st := room.InboxStats(); st.Queued != 0 {
		t.Fatalf("queued after End = %d", st.Queued)
	}
	if _, ok := room.Recv(1); ok {
		t.Fatal("Recv returned a packet after End")
	}
	if err := room.Start(); err != nil {
		t.Fatal(err)
	}
	_ = room.Deliver(1, netplay.Packet{})
	rs.Leave(room.ID(), 1)
	rs.Leave(room.ID(), 2)
	if st := room.InboxStats(); st.Queued != 0 {
		t.Fatalf("queued after Leave = %d", st.Queued)
	}
	if closed := rs.Tick(); len(closed) != 1 {
		t.Fatalf("Tick closed %v", closed)
	}
}
