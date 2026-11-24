package net

import (
	"slices"
	"testing"
	"time"
)

func unreliable(seq uint32) Packet { return Packet{Channel: ChannelUnreliable, Seq: seq} }
func reliable(seq uint32) Packet   { return Packet{Channel: ChannelReliableOrdered, Seq: seq} }

// #546: an undrained unreliable inbox stays at its cap and keeps the newest packets.
func TestInboxUnreliableCapKeepsNewest(t *testing.T) {
	var q packetQueue
	q.setLimits(InboxLimits{Unreliable: 8})
	for i := range uint32(100) {
		q.push(unreliable(i + 1))
		if n := q.stats().Unreliable; n > 8 {
			t.Fatalf("unreliable inbox holds %d packets after push %d, cap 8", n, i+1)
		}
	}
	st := q.stats()
	if st.Unreliable != 8 || st.UnreliableDropped != 92 || st.Reliable != 0 {
		t.Fatalf("stats = %+v, want 8 queued and 92 dropped", st)
	}
	for want := uint32(93); want <= 100; want++ {
		p, ok := q.pop()
		if !ok || p.Seq != want {
			t.Fatalf("pop = %+v, %v; want seq %d", p, ok, want)
		}
	}
	if _, ok := q.pop(); ok {
		t.Fatal("pop on a drained inbox succeeded")
	}
}

func TestInboxDefaultUnreliableCap(t *testing.T) {
	var q packetQueue
	for i := range uint32(DefaultUnreliableInbox + 10) {
		q.push(unreliable(i + 1))
	}
	if st := q.stats(); st.Unreliable != DefaultUnreliableInbox || st.UnreliableDropped != 10 {
		t.Fatalf("stats = %+v, want the default cap %d and 10 dropped", st, DefaultUnreliableInbox)
	}
	if p, _ := q.pop(); p.Seq != 11 {
		t.Fatalf("oldest kept seq = %d, want 11", p.Seq)
	}
}

// Dropping at the cap pops the FIFO's front, so a flood past the cap still moves at most one packet per packet pushed.
func TestInboxUnreliableFloodStaysLinear(t *testing.T) {
	var q packetQueue
	q.setLimits(InboxLimits{Unreliable: 100})
	const n = 1 << 15
	for i := range uint32(n) {
		q.push(unreliable(i))
	}
	f := q.unrel[0]
	if moved := f.moved + q.order.moved; moved > 2*n {
		t.Fatalf("a flood of %d packets moved %d, want at most one move per packet in each of the peer's FIFO and the arrival order", n, moved)
	}
	if cap(f.buf) > 4*100 {
		t.Fatalf("a capped inbox grew its buffer to %d for a cap of 100", cap(f.buf))
	}
	// The dropped packets' arrival entries are compacted away, so the arrival order stays near the cap too.
	if q.order.len() > 2*100+64 || cap(q.order.buf) > 8*100 {
		t.Fatalf("the arrival order holds %d entries in a buffer of %d for 100 queued packets", q.order.len(), cap(q.order.buf))
	}
}

// #546: nothing is lost on the reliable channel, however far the inbox falls behind, and Recv still returns packets in arrival order across both channels.
func TestInboxReliableNeverDropsAndOrderIsKept(t *testing.T) {
	var q packetQueue
	q.setLimits(InboxLimits{Unreliable: 4, ReliableWarning: 16})
	const n = 10000
	for i := range uint32(n) {
		q.push(reliable(i + 1))
		q.push(unreliable(i + 1))
		q.push(unreliable(i + 1))
	}
	if st := q.stats(); st.Reliable != n || st.Unreliable != 4 || st.UnreliableDropped != 2*n-4 {
		t.Fatalf("stats = %+v, want %d reliable, 4 unreliable, %d dropped", q.stats(), n, 2*n-4)
	}
	var got []Packet
	for {
		p, ok := q.pop()
		if !ok {
			break
		}
		got = append(got, p)
	}
	// Every reliable packet comes out in order; the four unreliable packets kept are the newest, each where it arrived.
	want := make([]Packet, 0, n+4)
	for i := range uint32(n - 1) {
		want = append(want, reliable(i+1))
	}
	want = append(want, unreliable(n-1), unreliable(n-1), reliable(n), unreliable(n), unreliable(n))
	if len(got) != len(want) {
		t.Fatalf("popped %d packets, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Channel != want[i].Channel || got[i].Seq != want[i].Seq {
			t.Fatalf("packet %d = %s seq %d, want %s seq %d", i, got[i].Channel, got[i].Seq, want[i].Channel, want[i].Seq)
		}
	}
}

// #546: the reliable warning fires once per crossing, not once per packet, and rearms once the backlog has drained to half the threshold.
func TestInboxReliableWarningOncePerCrossing(t *testing.T) {
	var q packetQueue
	q.setLimits(InboxLimits{ReliableWarning: 10})
	push := func(k int) {
		for range k {
			q.push(reliable(0))
		}
	}
	pop := func(k int) {
		for range k {
			if _, ok := q.pop(); !ok {
				t.Fatal("pop on an empty inbox")
			}
		}
	}
	push(10)
	if st := q.stats(); st.ReliableWarnings != 0 || st.ReliableBacklog {
		t.Fatalf("at the threshold: %+v, want no warning yet", st)
	}
	push(1)
	if st := q.stats(); st.ReliableWarnings != 1 || !st.ReliableBacklog {
		t.Fatalf("past the threshold: %+v, want one warning", st)
	}
	push(500)
	if st := q.stats(); st.ReliableWarnings != 1 {
		t.Fatalf("still past the threshold: %d warnings, want 1", st.ReliableWarnings)
	}
	// Hovering just under and over the threshold is the same crossing.
	pop(q.rel.len() - 9)
	push(5)
	pop(5)
	push(5)
	if st := q.stats(); st.ReliableWarnings != 1 || !st.ReliableBacklog {
		t.Fatalf("hovering at the threshold: %+v, want still one warning", st)
	}
	pop(q.rel.len() - 6)
	if st := q.stats(); !st.ReliableBacklog {
		t.Fatalf("at 6 of 10: %+v, want the backlog flag kept until half", st)
	}
	pop(1)
	if st := q.stats(); st.ReliableBacklog {
		t.Fatalf("at 5 of 10: %+v, want the warning rearmed", st)
	}
	push(6)
	if st := q.stats(); st.ReliableWarnings != 2 || !st.ReliableBacklog {
		t.Fatalf("second crossing: %+v, want two warnings", st)
	}
}

func TestInboxSetLimits(t *testing.T) {
	var q packetQueue
	q.setLimits(InboxLimits{Unreliable: -1, ReliableWarning: -1})
	for i := range uint32(DefaultUnreliableInbox * 2) {
		q.push(unreliable(i))
		q.push(reliable(i))
	}
	if st := q.stats(); st.UnreliableDropped != 0 || st.ReliableWarnings != 0 || st.Unreliable != 2*DefaultUnreliableInbox {
		t.Fatalf("unbounded inbox: %+v, want nothing dropped and no warning", st)
	}
	// Lowering the cap drops the oldest unreliable packets at once; lowering the threshold below the backlog counts a crossing.
	q.setLimits(InboxLimits{Unreliable: 3, ReliableWarning: 100})
	st := q.stats()
	if st.Unreliable != 3 || st.UnreliableDropped != 2*DefaultUnreliableInbox-3 || st.ReliableWarnings != 1 || !st.ReliableBacklog {
		t.Fatalf("after lowering the limits: %+v", st)
	}
	// Raising the threshold far above the backlog rearms the warning.
	q.setLimits(InboxLimits{Unreliable: 3, ReliableWarning: 1 << 20})
	if st := q.stats(); st.ReliableBacklog || st.ReliableWarnings != 1 {
		t.Fatalf("after raising the threshold: %+v", st)
	}
}

// Raising the threshold above the backlog, but not to twice it, clears the backlog flag, so the first crossing of the new threshold is counted.
func TestInboxRaisedWarningCountsItsFirstCrossing(t *testing.T) {
	var q packetQueue
	q.setLimits(InboxLimits{ReliableWarning: 100})
	for range 150 {
		q.push(reliable(0))
	}
	if st := q.stats(); st.ReliableWarnings != 1 || !st.ReliableBacklog {
		t.Fatalf("past 100: %+v, want one warning", st)
	}
	q.setLimits(InboxLimits{ReliableWarning: 200})
	if st := q.stats(); st.ReliableBacklog {
		t.Fatalf("150 queued under a threshold of 200: %+v, want the backlog flag cleared", st)
	}
	for range 60 {
		q.push(reliable(0))
	}
	if st := q.stats(); st.ReliableWarnings != 2 || !st.ReliableBacklog {
		t.Fatalf("past the raised threshold: %+v, want a second warning", st)
	}
}

// #546: the unreliable cap is per peer, so a peer that floods the inbox evicts only its own packets and every other peer's survive, in arrival order.
func TestInboxUnreliableCapIsPerPeer(t *testing.T) {
	var q packetQueue
	q.setLimits(InboxLimits{Unreliable: 4})
	from := func(peer PeerID, seq uint32) Packet {
		return Packet{From: peer, Channel: ChannelUnreliable, Seq: seq}
	}
	q.push(from(2, 1))
	for i := range uint32(100) {
		q.push(from(1, i+1))
		if i == 50 {
			q.push(from(2, 2))
			q.push(from(3, 1))
		}
	}
	if st := q.stats(); st.Unreliable != 7 || st.UnreliableDropped != 96 {
		t.Fatalf("stats = %+v, want 7 queued (peer 1's newest 4, peer 2's 2, peer 3's 1) and 96 of peer 1's dropped", st)
	}
	want := []Packet{from(2, 1), from(2, 2), from(3, 1), from(1, 97), from(1, 98), from(1, 99), from(1, 100)}
	for i, w := range want {
		p, ok := q.pop()
		if !ok || p.From != w.From || p.Seq != w.Seq {
			t.Fatalf("pop %d = %+v, %v; want %+v", i, p, ok, w)
		}
	}
	if _, ok := q.pop(); ok || q.len() != 0 || len(q.unrel) != 0 {
		t.Fatalf("drained inbox still holds %d packets and %d peer FIFOs", q.len(), len(q.unrel))
	}
}

// Many peers each flooding past the cap, with the consumer popping now and then, keep arrival order and never hold more than the cap per peer.
func TestInboxPerPeerCapKeepsArrivalOrderUnderChurn(t *testing.T) {
	var q packetQueue
	q.setLimits(InboxLimits{Unreliable: 3})
	var seq uint32
	for round := range 2000 {
		peer := PeerID(round%7 + 1)
		for range round % 5 {
			seq++
			q.push(Packet{From: peer, Channel: ChannelUnreliable, Seq: seq})
		}
		if round%3 == 0 {
			q.pop()
		}
		for id, f := range q.unrel {
			if f.len() > 3 {
				t.Fatalf("peer %d holds %d packets, cap 3", id, f.len())
			}
		}
		if q.order.len() > 2*q.stats().Unreliable+64 {
			t.Fatalf("the arrival order holds %d entries for %d queued packets", q.order.len(), q.stats().Unreliable)
		}
	}
	var last uint32
	for {
		p, ok := q.pop()
		if !ok {
			break
		}
		if p.Seq <= last {
			t.Fatalf("pop returned seq %d after %d: arrival order broken", p.Seq, last)
		}
		last = p.Seq
	}
}

// #546: admit, used by the transports whose peers cannot be trusted, refuses a reliable packet past its peer's cap without touching another peer's; push never refuses one.
func TestInboxReliableCapRefusesPastIt(t *testing.T) {
	var q packetQueue
	q.setLimits(InboxLimits{Reliable: 3})
	rel := func(peer PeerID, seq uint32) Packet {
		return Packet{From: peer, Channel: ChannelReliableOrdered, Seq: seq}
	}
	for i := range uint32(3) {
		if !q.admit(rel(1, i+1)) {
			t.Fatalf("admit %d under the cap refused", i+1)
		}
	}
	if q.admit(rel(1, 4)) {
		t.Fatal("admit past the cap accepted")
	}
	if !q.admit(rel(2, 1)) {
		t.Fatal("another peer's packet was refused")
	}
	if st := q.stats(); st.Reliable != 4 || st.ReliableRefused != 1 {
		t.Fatalf("stats = %+v, want 4 queued and 1 refused", st)
	}
	if p, _ := q.pop(); p.From != 1 || p.Seq != 1 {
		t.Fatalf("pop = %+v, want peer 1's first", p)
	}
	if !q.admit(rel(1, 4)) {
		t.Fatal("admit refused after Recv made room")
	}
	q.push(rel(1, 5))
	if st := q.stats(); st.Reliable != 5 || st.ReliableRefused != 1 {
		t.Fatalf("push past the cap: %+v, want it queued", st)
	}
	q.setLimits(InboxLimits{Reliable: -1})
	for range DefaultReliableInbox + 1 {
		if !q.admit(rel(3, 0)) {
			t.Fatal("admit refused with the cap removed")
		}
	}
}

func TestInboxDefaultReliableCap(t *testing.T) {
	var q packetQueue
	for range DefaultReliableInbox {
		if !q.admit(reliable(0)) {
			t.Fatal("admit refused under the default cap")
		}
	}
	if q.admit(reliable(0)) {
		t.Fatalf("admit accepted packet %d, past the default cap", DefaultReliableInbox+1)
	}
}

// #546: a RelayTransport whose peer is past its reliable cap stops acknowledging it, and the peer's messages arrive later, all of them and in order, as Recv makes room.
func TestRelayTransportReliableCapHoldsBackThePeer(t *testing.T) {
	clock := newFakeClock()
	r := newMemRelay(2, 3)
	a := NewRelayTransport(r.clients[2], RelayTransportConfig{Now: clock.Now})
	b := NewRelayTransport(r.clients[3], RelayTransportConfig{Now: clock.Now, Inbox: InboxLimits{Reliable: 2}})
	r.announce(2, 3)
	_ = a.Peers()
	_ = b.Peers()
	msgs := []string{"a", "b", "c", "d", "e"}
	for _, m := range msgs {
		if err := a.Send(3, ChannelReliableOrdered, []byte(m)); err != nil {
			t.Fatalf("send %q: %v", m, err)
		}
	}
	_ = b.Peers() // ingest without Recv
	if st := b.InboxStats(); st.Reliable != 2 || st.ReliableRefused != 1 {
		t.Fatalf("InboxStats = %+v, want 2 queued and c refused", st)
	}
	var got []string
	for range 50 {
		if p, ok := b.Recv(); ok {
			got = append(got, string(p.Payload))
			if st := b.InboxStats(); st.Reliable > 2 {
				t.Fatalf("b holds %d reliable messages, cap 2", st.Reliable)
			}
			continue
		}
		_ = drainRelay(a) // a reads b's acks
		clock.Advance(relayMaxRTO)
		_ = drainRelay(a) // and resends what b has not acknowledged
	}
	if !slices.Equal(got, msgs) {
		t.Fatalf("b got %v, want %v", got, msgs)
	}
}

func TestLoopbackInboxLimits(t *testing.T) {
	hub := NewLoopbackHub(LoopbackConfig{Inbox: InboxLimits{Unreliable: 4, ReliableWarning: 8}})
	a := hub.AddPeer(1)
	b := hub.AddPeer(2)
	for i := range 10 {
		if err := a.Send(2, ChannelUnreliable, []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 20 {
		if err := a.Send(2, ChannelReliableOrdered, []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	st := b.InboxStats()
	if st.Unreliable != 4 || st.UnreliableDropped != 6 || st.Reliable != 20 || st.ReliableWarnings != 1 {
		t.Fatalf("InboxStats = %+v", st)
	}
	for i := 6; i < 10; i++ {
		p, ok := b.Recv()
		if !ok || p.Channel != ChannelUnreliable || p.Payload[0] != byte(i) {
			t.Fatalf("Recv = %+v, %v; want unreliable %d", p, ok, i)
		}
	}
	for i := range 20 {
		p, ok := b.Recv()
		if !ok || p.Channel != ChannelReliableOrdered || p.Payload[0] != byte(i) {
			t.Fatalf("Recv = %+v, %v; want reliable %d", p, ok, i)
		}
	}
	// A delayed packet lands in the inbox through Advance and is bounded the same way; SetInboxLimits changes one peer.
	b.SetInboxLimits(InboxLimits{Unreliable: 2})
	hub.SetConfig(LoopbackConfig{Latency: time.Millisecond})
	for i := range 5 {
		_ = a.Send(2, ChannelUnreliable, []byte{byte(i)})
	}
	hub.Advance(time.Millisecond)
	if st := b.InboxStats(); st.Unreliable != 2 || st.UnreliableDropped != 9 {
		t.Fatalf("after Advance: %+v, want 2 queued and 9 dropped in total", st)
	}
	if st := a.InboxStats(); st.UnreliableDropped != 0 || st.Unreliable != 0 {
		t.Fatalf("the sender's inbox changed: %+v", st)
	}
}

func TestRelayTransportInboxLimits(t *testing.T) {
	rt := NewRelayTransport(NewLoopbackHub(LoopbackConfig{}).AddPeer(1), RelayTransportConfig{Inbox: InboxLimits{Unreliable: 2}})
	for i := range uint32(5) {
		rt.inbox.push(unreliable(i))
	}
	if st := rt.InboxStats(); st.Unreliable != 2 || st.UnreliableDropped != 3 {
		t.Fatalf("InboxStats = %+v", st)
	}
	rt.SetInboxLimits(InboxLimits{Unreliable: 1})
	if st := rt.InboxStats(); st.Unreliable != 1 || st.UnreliableDropped != 4 {
		t.Fatalf("after SetInboxLimits: %+v", st)
	}
}

var (
	_ InboxReporter = (*LoopbackTransport)(nil)
	_ InboxReporter = (*RelayTransport)(nil)
)
