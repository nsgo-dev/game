package net

import (
	"encoding/binary"
	"slices"
	"sync"
	"testing"
	"time"
)

// memRelay is an in-memory stand-in for a Relay that forwards relay frames between memClients and can drop chosen frames, so the end-to-end reliability of RelayTransport is tested without sockets or timing.
type memRelay struct {
	mu      sync.Mutex
	clients map[PeerID]*memClient
	epoch   uint32
	// drop, when set, decides whether a frame from one client to another is lost.
	drop func(from, to PeerID, body []byte) bool
	// frames counts frames each client sent, by body kind.
	frames map[byte]int
}

type memClient struct {
	r     *memRelay
	id    PeerID
	inbox []Packet
}

func newMemRelay(ids ...PeerID) *memRelay {
	r := &memRelay{clients: make(map[PeerID]*memClient), frames: make(map[byte]int)}
	for _, id := range ids {
		r.clients[id] = &memClient{r: r, id: id}
	}
	return r
}

// announce sends every client the membership members.
func (r *memRelay) announce(members ...PeerID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.epoch++
	frame := encodeRelayMembers(r.epoch, members)
	for _, c := range r.clients {
		c.inbox = append(c.inbox, Packet{From: 1, Channel: ChannelUnreliable, Payload: frame})
	}
}

func (c *memClient) Send(to PeerID, ch Channel, payload []byte) error {
	if to != PeerBroadcast || ch != ChannelUnreliable {
		return ErrUnsupportedChannel
	}
	dst, body, ok := decodeRelayFrame(payload)
	if !ok {
		return ErrInvalidPacket
	}
	c.r.mu.Lock()
	defer c.r.mu.Unlock()
	if len(body) > 0 {
		c.r.frames[body[0]]++
	}
	if c.r.drop != nil && c.r.drop(c.id, dst, body) {
		return nil
	}
	if peer := c.r.clients[dst]; peer != nil {
		peer.inbox = append(peer.inbox, Packet{From: 1, Channel: ChannelUnreliable, Payload: encodeRelayFrame(c.id, body)})
	}
	return nil
}

func (c *memClient) Recv() (Packet, bool) {
	c.r.mu.Lock()
	defer c.r.mu.Unlock()
	if len(c.inbox) == 0 {
		return Packet{}, false
	}
	pkt := c.inbox[0]
	c.inbox = slices.Delete(c.inbox, 0, 1)
	return pkt, true
}

func (c *memClient) LocalID() PeerID { return c.id }
func (c *memClient) Peers() []PeerID { return []PeerID{1} }
func (c *memClient) Close() error    { return nil }

func drainRelay(rt *RelayTransport) []Packet {
	var out []Packet
	for {
		pkt, ok := rt.Recv()
		if !ok {
			return out
		}
		out = append(out, pkt)
	}
}

func payloads(pkts []Packet) []string {
	out := make([]string, len(pkts))
	for i, p := range pkts {
		out[i] = string(p.Payload)
	}
	return out
}

func TestRelayTransportResendsOnlyLostReliableMessages(t *testing.T) {
	clock := newFakeClock()
	r := newMemRelay(2, 3)
	a := NewRelayTransport(r.clients[2], RelayTransportConfig{Now: clock.Now})
	b := NewRelayTransport(r.clients[3], RelayTransportConfig{Now: clock.Now})
	r.announce(2, 3)
	if got := a.Peers(); !slices.Equal(got, []PeerID{3}) {
		t.Fatalf("a.Peers = %v, want [3]", got)
	}
	_ = b.Peers()

	lostOnce := false
	r.drop = func(from, to PeerID, body []byte) bool {
		if !lostOnce && body[0] == relayBodyReliable && binary.BigEndian.Uint32(body[1:5]) == 2 {
			lostOnce = true
			return true
		}
		return false
	}
	for _, msg := range []string{"one", "two", "three"} {
		if err := a.Send(3, ChannelReliableOrdered, []byte(msg)); err != nil {
			t.Fatalf("send %q: %v", msg, err)
		}
	}
	if got := payloads(drainRelay(b)); !slices.Equal(got, []string{"one"}) {
		t.Fatalf("b got %v before the resend, want only [one]: three waits for two", got)
	}
	// a reads b's ack, which reports one received and three buffered.
	_ = drainRelay(a)
	if n := r.frames[relayBodyReliable]; n != 3 {
		t.Fatalf("a sent %d reliable frames before the timeout, want 3", n)
	}

	clock.Advance(relayInitialRTO)
	_ = drainRelay(a)
	if n := r.frames[relayBodyReliable]; n != 4 {
		t.Fatalf("a sent %d reliable frames after the timeout, want 4: only two is resent", n)
	}
	got := drainRelay(b)
	if !slices.Equal(payloads(got), []string{"two", "three"}) {
		t.Fatalf("b got %v after the resend, want [two three]", payloads(got))
	}
	for i, p := range got {
		if p.From != 2 || p.Channel != ChannelReliableOrdered || p.Seq != uint32(i+2) {
			t.Fatalf("packet %d = %+v, want from 2, reliable, seq %d", i, p, i+2)
		}
	}

	// Once everything is acknowledged nothing is resent, however long a waits.
	_ = drainRelay(a)
	clock.Advance(time.Minute)
	_ = drainRelay(a)
	if n := r.frames[relayBodyReliable]; n != 4 {
		t.Fatalf("a sent %d reliable frames after every ack, want still 4", n)
	}
}

func TestRelayTransportDeliversDuplicatesOnce(t *testing.T) {
	clock := newFakeClock()
	r := newMemRelay(2, 3)
	a := NewRelayTransport(r.clients[2], RelayTransportConfig{Now: clock.Now})
	b := NewRelayTransport(r.clients[3], RelayTransportConfig{Now: clock.Now})
	r.announce(2, 3)
	_, _ = a.Peers(), b.Peers()

	// Every ack is lost, so a resends "x" until one gets through.
	acks := 0
	r.drop = func(from, to PeerID, body []byte) bool {
		if body[0] == relayBodyAck {
			acks++
			return acks <= 2
		}
		return false
	}
	if err := a.Send(3, ChannelReliableOrdered, []byte("x")); err != nil {
		t.Fatalf("send: %v", err)
	}
	var got []string
	for range 4 {
		got = append(got, payloads(drainRelay(b))...)
		_ = drainRelay(a)
		clock.Advance(relayMaxRTO)
	}
	if !slices.Equal(got, []string{"x"}) {
		t.Fatalf("b got %v, want x exactly once", got)
	}
	if n := r.frames[relayBodyReliable]; n != 3 {
		t.Fatalf("a sent x %d times, want 3: once, then once per lost ack", n)
	}
}

func TestRelayTransportUnreliableAndMembership(t *testing.T) {
	r := newMemRelay(2, 3, 4)
	a := NewRelayTransport(r.clients[2], RelayTransportConfig{})
	b := NewRelayTransport(r.clients[3], RelayTransportConfig{})
	c := NewRelayTransport(r.clients[4], RelayTransportConfig{})

	if err := a.Send(3, ChannelUnreliable, []byte("early")); err != ErrUnknownPeer {
		t.Fatalf("send before any membership = %v, want ErrUnknownPeer", err)
	}
	r.announce(2, 3, 4)
	if got := a.Peers(); !slices.Equal(got, []PeerID{3, 4}) {
		t.Fatalf("a.Peers = %v, want [3 4]", got)
	}
	_, _ = b.Peers(), c.Peers()
	if err := a.Send(PeerBroadcast, ChannelUnreliable, []byte("hi")); err != nil {
		t.Fatalf("broadcast: %v", err)
	}
	for _, rt := range []*RelayTransport{b, c} {
		got := drainRelay(rt)
		if len(got) != 1 || got[0].From != 2 || got[0].Channel != ChannelUnreliable || string(got[0].Payload) != "hi" || got[0].Seq != 1 {
			t.Fatalf("peer %d got %+v, want one unreliable hi from 2", rt.LocalID(), got)
		}
	}
	if err := a.Send(2, ChannelUnreliable, []byte("self")); err != ErrUnknownPeer {
		t.Fatalf("send to self = %v, want ErrUnknownPeer", err)
	}
	if err := a.Send(3, ChannelUnreliable, make([]byte, MaxRelayPayload+1)); err != ErrPayloadTooLarge {
		t.Fatalf("oversized send = %v, want ErrPayloadTooLarge", err)
	}

	// 4 leaves; an older membership arriving late does not bring it back.
	r.announce(2, 3)
	r.mu.Lock()
	r.clients[2].inbox = append(r.clients[2].inbox, Packet{From: 1, Payload: encodeRelayMembers(1, []PeerID{2, 3, 4})})
	r.mu.Unlock()
	if got := a.Peers(); !slices.Equal(got, []PeerID{3}) {
		t.Fatalf("a.Peers after 4 left = %v, want [3]", got)
	}
	if err := a.Send(4, ChannelReliableOrdered, []byte("gone")); err != ErrUnknownPeer {
		t.Fatalf("send to a peer that left = %v, want ErrUnknownPeer", err)
	}
	// A frame from a peer outside the membership is ignored.
	r.mu.Lock()
	r.clients[2].inbox = append(r.clients[2].inbox, Packet{From: 1, Payload: encodeRelayFrame(4, encodeRelayData(relayBodyUnreliable, 9, []byte("stray")))})
	r.mu.Unlock()
	if got := drainRelay(a); len(got) != 0 {
		t.Fatalf("a got %+v from a non-member", got)
	}
}

func TestRelayTransportBacklog(t *testing.T) {
	clock := newFakeClock()
	r := newMemRelay(2, 3)
	a := NewRelayTransport(r.clients[2], RelayTransportConfig{Now: clock.Now})
	r.announce(2, 3)
	_ = a.Peers()
	r.drop = func(PeerID, PeerID, []byte) bool { return true }
	for i := range relayWindow {
		if err := a.Send(3, ChannelReliableOrdered, []byte{byte(i)}); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if err := a.Send(3, ChannelReliableOrdered, []byte("over")); err != ErrRelayBacklog {
		t.Fatalf("send past the window = %v, want ErrRelayBacklog", err)
	}
	// A long outage resends at most relayMaxRetransmits per Recv.
	before := r.frames[relayBodyReliable]
	clock.Advance(relayMaxRTO)
	_ = drainRelay(a)
	if n := r.frames[relayBodyReliable] - before; n != relayMaxRetransmits {
		t.Fatalf("one Recv resent %d messages, want %d", n, relayMaxRetransmits)
	}
}

func TestRelayFrameCodecs(t *testing.T) {
	frame := encodeRelayMembers(7, []PeerID{2, 5, 9})
	from, body, ok := decodeRelayFrame(frame)
	if !ok || from != 0 {
		t.Fatalf("membership frame header = %d %v", from, ok)
	}
	epoch, members, ok := decodeRelayMembers(body)
	if !ok || epoch != 7 || !slices.Equal(members, []PeerID{2, 5, 9}) {
		t.Fatalf("members = %d %v %v", epoch, members, ok)
	}
	for _, bad := range [][]PeerID{{5, 2}, {2, 2}, {0, 3}} {
		_, body, _ := decodeRelayFrame(encodeRelayMembers(1, bad))
		if _, _, ok := decodeRelayMembers(body); ok {
			t.Fatalf("membership %v accepted, want unsorted, repeated or zero IDs refused", bad)
		}
	}
	if _, _, ok := decodeRelayFrame([]byte{relayVersion + 1, 0, 0, 0, 2}); ok {
		t.Fatal("a frame of another version was accepted")
	}
}

func FuzzRelayTransportIngest(f *testing.F) {
	f.Add(encodeRelayMembers(1, []PeerID{2, 3}))
	f.Add(encodeRelayFrame(3, encodeRelayData(relayBodyReliable, 1, []byte("x"))))
	f.Add(encodeRelayFrame(3, encodeRelayData(relayBodyReliable, 5, []byte("y"))))
	f.Add(encodeRelayFrame(3, encodeRelayAck(2, 1)))
	f.Add(encodeRelayFrame(3, encodeRelayData(relayBodyUnreliable, 1, nil)))
	f.Fuzz(func(t *testing.T, frame []byte) {
		r := newMemRelay(2, 3)
		a := NewRelayTransport(r.clients[2], RelayTransportConfig{Now: newFakeClock().Now})
		r.announce(2, 3)
		_ = a.Send(3, ChannelReliableOrdered, []byte("out"))
		r.mu.Lock()
		r.clients[2].inbox = append(r.clients[2].inbox, Packet{From: 1, Payload: frame}, Packet{From: 1, Payload: frame})
		r.mu.Unlock()
		var lastSeq uint32
		for _, pkt := range drainRelay(a) {
			if pkt.From != 3 {
				t.Fatalf("packet from %d, want only the member 3", pkt.From)
			}
			if pkt.Channel == ChannelReliableOrdered {
				if pkt.Seq != lastSeq+1 {
					t.Fatalf("reliable seq %d after %d", pkt.Seq, lastSeq)
				}
				lastSeq = pkt.Seq
			}
		}
		if p := a.peers[3]; p != nil && len(p.ooo) > relayWindow {
			t.Fatalf("buffered %d messages, past the window", len(p.ooo))
		}
		if slices.Contains(a.Peers(), a.LocalID()) {
			t.Fatal("Peers lists the local ID")
		}
	})
}
