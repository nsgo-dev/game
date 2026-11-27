package netplay

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestHandshakeMessagesRoundTrip(t *testing.T) {
	h := hello{resumePeer: 7, token: []byte("token-abc")}
	h.nonce[0], h.resumeSecret[15] = 1, 2
	b := encodeHello(h, udpMinHello)
	if len(b) != udpMinHello {
		t.Fatalf("padded hello = %d bytes, want %d", len(b), udpMinHello)
	}
	got, err := decodeHello(b)
	if err != nil || got.nonce != h.nonce || got.resumePeer != 7 || got.resumeSecret != h.resumeSecret || !bytes.Equal(got.token, h.token) {
		t.Fatalf("hello round trip = %+v, %v", got, err)
	}

	w := welcome{nonce: h.nonce, peer: 3, server: 1, resumed: true}
	w.secret[0], w.key[31] = 9, 8
	gw, err := decodeServerHandshake(encodeWelcome(w))
	if err != nil || gw != w {
		t.Fatalf("welcome round trip = %+v, %v", gw, err)
	}

	_, err = decodeServerHandshake(encodeReject(h.nonce, "bad token", 0))
	var rej *RejectError
	if !errors.As(err, &rej) || rej.Reason != "bad token" || !errors.Is(err, ErrRejected) {
		t.Fatalf("reject = %v", err)
	}
}

func TestHandshakeRejectsMalformedMessages(t *testing.T) {
	good := encodeHello(hello{token: []byte("t")}, 0)
	for name, b := range map[string][]byte{
		"empty":       nil,
		"wrong type":  append([]byte{hsWelcome}, good[1:]...),
		"truncated":   good[:hsHelloHeader-1],
		"token past":  good[:len(good)-1],
		"token large": func() []byte { b := encodeHello(hello{}, 0); b[38], b[39] = 0x10, 0x01; return b }(),
	} {
		if _, err := decodeHello(b); err == nil {
			t.Errorf("%s: decodeHello accepted %x", name, b)
		}
	}
	old := bytes.Clone(good)
	old[1] = hsVersion + 1
	if _, err := decodeHello(old); !errors.Is(err, errHandshakeVersion) {
		t.Fatalf("other version = %v, want errHandshakeVersion", err)
	}

	w := encodeWelcome(welcome{peer: 2, server: 1})
	for name, b := range map[string][]byte{
		"short":       w[:len(w)-1],
		"peer zero":   encodeWelcome(welcome{peer: 0, server: 1}),
		"same as srv": encodeWelcome(welcome{peer: 1, server: 1}),
		"bad flag":    func() []byte { b := bytes.Clone(w); b[26] = 2; return b }(),
		"reject len":  func() []byte { b := encodeReject([16]byte{}, "x", 0); b[19] = 5; return b }(),
	} {
		if _, err := decodeServerHandshake(b); !errors.Is(err, ErrInvalidPacket) {
			t.Errorf("%s: decodeServerHandshake = %v, want ErrInvalidPacket", name, err)
		}
	}
}

func TestRejectNeverOutgrowsTheHello(t *testing.T) {
	long := strings.Repeat("r", 1000)
	if b := encodeReject([16]byte{}, long, udpMinHello); len(b) != udpMinHello {
		t.Fatalf("limited reject = %d bytes, want %d", len(b), udpMinHello)
	}
	if b := encodeReject([16]byte{}, long, 0); len(b) != hsRejectHeader+hsMaxReason {
		t.Fatalf("unlimited reject = %d bytes, want reason capped at %d", len(b), hsMaxReason)
	}
	if hsWelcomeSize > udpMinHello {
		t.Fatalf("welcome (%d bytes) is larger than the smallest answered hello (%d)", hsWelcomeSize, udpMinHello)
	}
}

func TestSessionTableGraceAndResume(t *testing.T) {
	clock := newFakeClock()
	tab := newSessionTable(Auth{GracePeriod: 5 * time.Second, Now: clock.Now}, 1, false)
	id := mustReserve(t, tab)
	if id != 2 {
		t.Fatalf("first ID = %d, want 2", id)
	}
	if got := tab.connectedIDs(); len(got) != 0 {
		t.Fatalf("a reserved ID is listed as connected: %v", got)
	}
	s := tab.commit(id, clock.Now())
	secret := s.secret

	tab.drop(s, clock.Now())
	if err := tab.sendable(id); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("sendable in grace = %v", err)
	}
	clock.Advance(4 * time.Second)
	tab.expire(clock.Now())

	// A wrong secret does not resume, the right one does and rotates it.
	if tab.resume(hello{resumePeer: id}, clock.Now()) != nil {
		t.Fatal("resumed with a zero secret")
	}
	r := tab.resume(hello{resumePeer: id, resumeSecret: secret}, clock.Now())
	if r != s || !r.connected || r.secret == secret {
		t.Fatalf("resume = %+v", r)
	}
	if tab.resume(hello{resumePeer: id, resumeSecret: secret}, clock.Now()) != nil {
		t.Fatal("an old resume secret was accepted twice")
	}

	tab.drop(s, clock.Now())
	clock.Advance(5 * time.Second)
	tab.expire(clock.Now())
	if tab.sessions[id] != nil {
		t.Fatal("session outlived its grace period")
	}
	var kinds []PeerEventKind
	for ev, ok := tab.popEvent(); ok; ev, ok = tab.popEvent() {
		if ev.Peer != id {
			t.Fatalf("event for peer %d", ev.Peer)
		}
		kinds = append(kinds, ev.Kind)
	}
	want := []PeerEventKind{PeerJoined, PeerDropped, PeerResumed, PeerDropped, PeerLeft}
	if !slicesEqual(kinds, want) {
		t.Fatalf("events = %v, want %v", kinds, want)
	}
}

func TestSessionTableWithoutGraceLeavesAtOnce(t *testing.T) {
	tab := newSessionTable(Auth{}, 1, false)
	s := tab.commit(mustReserve(t, tab), time.Now())
	tab.drop(s, time.Now())
	if tab.sessions[s.id] != nil {
		t.Fatal("dropped session kept without a grace period")
	}
	if ev, _ := tab.popEvent(); ev.Kind != PeerJoined {
		t.Fatalf("first event = %v", ev)
	}
	if ev, _ := tab.popEvent(); ev.Kind != PeerLeft {
		t.Fatalf("second event = %v, want left", ev)
	}
}

func TestSessionTableSkipsServerAndLiveIDs(t *testing.T) {
	tab := newSessionTable(Auth{}, 3, false)
	tab.next = 0
	var got []PeerID
	for range 4 {
		got = append(got, mustReserve(t, tab))
	}
	if want := []PeerID{1, 2, 4, 5}; !slicesEqual(got, want) {
		t.Fatalf("IDs = %v, want %v", got, want)
	}
	tab.release(2)
	tab.next = 1
	if id := mustReserve(t, tab); id != 2 {
		t.Fatalf("after release reserve = %d, want the freed 2", id)
	}
}

func mustReserve(t *testing.T, tab *sessionTable) PeerID {
	t.Helper()
	id, ok := tab.reserve()
	if !ok {
		t.Fatalf("reserve refused with %d sessions", len(tab.sessions))
	}
	return id
}

func TestSessionTableCapsSessions(t *testing.T) {
	clock := newFakeClock()
	tab := newSessionTable(Auth{MaxPeers: 2, GracePeriod: time.Second, Now: clock.Now}, 1, false)
	a := tab.commit(mustReserve(t, tab), clock.Now())
	pending := mustReserve(t, tab)
	if _, ok := tab.reserve(); ok {
		t.Fatal("reserved past MaxPeers with a pending handshake counted")
	}
	tab.release(pending)

	// A peer inside its grace period still holds its place until the grace period ends.
	tab.commit(mustReserve(t, tab), clock.Now())
	tab.drop(a, clock.Now())
	if _, ok := tab.reserve(); ok {
		t.Fatal("reserved past MaxPeers with a peer in its grace period counted")
	}
	clock.Advance(time.Second)
	tab.expire(clock.Now())
	mustReserve(t, tab)

	if got := newSessionTable(Auth{}, 1, false).maxPeers; got != defaultMaxPeers {
		t.Fatalf("default MaxPeers = %d, want %d", got, defaultMaxPeers)
	}
}

func TestSessionTableEventQueueFIFOAndLinear(t *testing.T) {
	clock := newFakeClock()
	const burstPeers = 1000
	tab := newSessionTable(Auth{
		GracePeriod: 5 * time.Second,
		MaxPeers:    burstPeers + 16,
		Now:         clock.Now,
	}, 1, false)

	var want []PeerEvent
	// Queue 4000 lifecycle events (join, drop, resume, leave) in one burst
	// without draining between them; none may be dropped.
	for range burstPeers {
		id := mustReserve(t, tab)
		s := tab.commit(id, clock.Now())
		want = append(want, PeerEvent{Kind: PeerJoined, Peer: id})
		secret := s.secret
		tab.drop(s, clock.Now())
		want = append(want, PeerEvent{Kind: PeerDropped, Peer: id})
		if tab.resume(hello{resumePeer: id, resumeSecret: secret}, clock.Now()) == nil {
			t.Fatalf("peer %d failed to resume", id)
		}
		want = append(want, PeerEvent{Kind: PeerResumed, Peer: id})
		tab.leave(s)
		want = append(want, PeerEvent{Kind: PeerLeft, Peer: id})
	}

	var got []PeerEvent
	for ev, ok := tab.popEvent(); ok; ev, ok = tab.popEvent() {
		got = append(got, ev)
	}
	if !slicesEqual(got, want) {
		t.Fatalf("drained %d events, want %d (first mismatch or loss)", len(got), len(want))
	}
	if tab.events.moved > len(want) {
		t.Fatalf("compaction moved %d events for %d queued, want at most one move per event", tab.events.moved, len(want))
	}
	if c := cap(tab.events.buf); c > packetFIFOKeep {
		t.Fatalf("drained event queue kept capacity %d, want at most %d", c, packetFIFOKeep)
	}

	// Interleaved push and pop rounds exercise mid-queue compaction without losing or reordering events.
	const rounds = 1000
	want = want[:0]
	got = got[:0]
	for range rounds {
		id := mustReserve(t, tab)
		s := tab.commit(id, clock.Now())
		tab.drop(s, clock.Now())
		tab.leave(s)
		want = append(want,
			PeerEvent{Kind: PeerJoined, Peer: id},
			PeerEvent{Kind: PeerDropped, Peer: id},
			PeerEvent{Kind: PeerLeft, Peer: id},
		)
		for range 2 {
			ev, ok := tab.popEvent()
			if !ok {
				t.Fatal("popEvent returned empty while events remained")
			}
			got = append(got, ev)
		}
	}
	for ev, ok := tab.popEvent(); ok; ev, ok = tab.popEvent() {
		got = append(got, ev)
	}
	if !slicesEqual(got, want) {
		t.Fatalf("interleaved drain got %d events, want %d", len(got), len(want))
	}
	if tab.events.moved > 4*burstPeers+len(want) {
		t.Fatalf("interleaved compaction moved %d events for %d total queued", tab.events.moved, 4*burstPeers+len(want))
	}
}

func TestSessionTableRaisesShortIdleTimeouts(t *testing.T) {
	for _, tc := range []struct {
		set, want time.Duration
	}{
		{0, 10 * time.Second}, {time.Second, udpMinIdle}, {udpMinIdle, udpMinIdle}, {5 * time.Second, 5 * time.Second},
	} {
		if got := newSessionTable(Auth{IdleTimeout: tc.set}, 1, true).idle; got != tc.want {
			t.Errorf("IdleTimeout %v gives %v, want %v", tc.set, got, tc.want)
		}
	}
	if got := newSessionTable(Auth{IdleTimeout: time.Second}, 1, false).idle; got != 0 {
		t.Errorf("a WebSocket table has idle timeout %v", got)
	}
}

// TestAcceptServerHandshake covers the reply check the browser client runs on its first message, which has no runtime browser test.
func TestAcceptServerHandshake(t *testing.T) {
	var nonce, other [hsNonceSize]byte
	nonce[0], other[0] = 1, 2
	w := welcome{nonce: nonce, peer: 5, server: 1, resumed: true}
	w.secret[3] = 4
	if got, err := acceptServerHandshake(encodeWelcome(w), nonce); err != nil || got != w {
		t.Fatalf("welcome = %+v, %v", got, err)
	}
	w.nonce = other
	if _, err := acceptServerHandshake(encodeWelcome(w), nonce); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("welcome for another hello = %v, want ErrInvalidPacket", err)
	}
	var rej *RejectError
	if _, err := acceptServerHandshake(encodeReject(nonce, reasonServerFull, 0), nonce); !errors.As(err, &rej) || rej.Reason != reasonServerFull {
		t.Fatalf("reject = %v", err)
	}
	if _, err := acceptServerHandshake(encodeWireFrame(ChannelReliableOrdered, 1, []byte("game")), nonce); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("a game frame before the welcome = %v, want ErrInvalidPacket", err)
	}
}

func slicesEqual[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func FuzzDecodeHello(f *testing.F) {
	f.Add(encodeHello(hello{token: []byte("token")}, 0))
	f.Add(encodeHello(hello{resumePeer: 9}, udpMinHello))
	f.Add([]byte{hsHello, hsVersion})
	f.Fuzz(func(t *testing.T, b []byte) {
		h, err := decodeHello(b)
		if err != nil {
			return
		}
		if len(h.token) > MaxTokenSize {
			t.Fatalf("token of %d bytes accepted", len(h.token))
		}
		again, err := decodeHello(encodeHello(h, 0))
		if err != nil || again.nonce != h.nonce || again.resumePeer != h.resumePeer || again.resumeSecret != h.resumeSecret || !bytes.Equal(again.token, h.token) {
			t.Fatalf("re-encoded hello differs: %+v vs %+v (%v)", again, h, err)
		}
	})
}

func FuzzDecodeServerHandshake(f *testing.F) {
	f.Add(encodeWelcome(welcome{peer: 2, server: 1}))
	f.Add(encodeReject([16]byte{1}, "no", 0))
	f.Fuzz(func(t *testing.T, b []byte) {
		w, err := decodeServerHandshake(b)
		if err != nil {
			return
		}
		if again, err := decodeServerHandshake(encodeWelcome(w)); err != nil || again != w {
			t.Fatalf("re-encoded welcome differs: %+v vs %+v (%v)", again, w, err)
		}
	})
}
