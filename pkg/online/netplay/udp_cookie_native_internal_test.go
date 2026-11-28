//go:build !js

package netplay

import (
	"bytes"
	stdnet "net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingVerify accepts every token and counts how often the server ran Verify.
func countingVerify(calls *atomic.Int32) func(PeerID, []byte) error {
	return func(PeerID, []byte) error {
		calls.Add(1)
		return nil
	}
}

// requestCookie asks srv for the cookie of the hello with nonce from raw's address.
func requestCookie(t *testing.T, raw stdnet.Conn, nonce [hsNonceSize]byte) [udpCookieSize]byte {
	t.Helper()
	req := encodeUDPCookieRequest(nonce)
	reply := udpExchange(t, raw, req)
	if len(reply) >= len(req) {
		t.Fatalf("cookie reply of %d bytes to a %d-byte request", len(reply), len(req))
	}
	c, ok := decodeUDPCookie(reply, nonce)
	if !ok {
		t.Fatalf("reply %x is not a cookie", reply)
	}
	return c
}

// cookieHello runs the cookie round trip from raw and returns the Noise hello for h that carries the cookie.
func cookieHello(t *testing.T, raw stdnet.Conn, srv *UDPServer, h hello) ([]byte, *noiseNKInitiator) {
	t.Helper()
	dgram, init, err := encodeUDPHelloV2(srv.PublicKey(), h, requestCookie(t, raw, h.nonce))
	if err != nil {
		t.Fatalf("encodeUDPHelloV2: %v", err)
	}
	return dgram, init
}

// expectCookieReply sends dgram from raw and checks that the server answered with a fresh cookie for nonce, smaller than dgram, rather than a welcome.
func expectCookieReply(t *testing.T, raw stdnet.Conn, dgram []byte, nonce [hsNonceSize]byte, stale [udpCookieSize]byte) {
	t.Helper()
	reply := udpExchange(t, raw, dgram)
	if len(reply) > len(dgram) {
		t.Fatalf("reply of %d bytes to a %d-byte hello", len(reply), len(dgram))
	}
	c, ok := decodeUDPCookie(reply, nonce)
	if !ok || c == stale {
		t.Fatalf("reply = %x, want a fresh cookie", reply)
	}
}

func serverSessions(srv *UDPServer) int {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return len(srv.table.sessions)
}

func TestUDPServerHellosFromSilentSocketsTakeNoSlot(t *testing.T) {
	var calls atomic.Int32
	srv := listenUDPServer(t, Auth{MaxPeers: 4, Verify: countingVerify(&calls)})

	// Four sockets each send one complete hello and never read the reply, as a sender with spoofed source addresses would. Such a sender never learns a cookie, so its hellos carry none.
	for range 4 {
		raw := dialRawUDP(t, srv)
		h, _ := newHello([]byte("tok"), resumeState{})
		dgram, _, err := encodeUDPHelloV2(srv.PublicKey(), h, [udpCookieSize]byte{})
		if err != nil {
			t.Fatalf("encodeUDPHelloV2: %v", err)
		}
		if _, err := raw.Write(dgram); err != nil {
			t.Fatalf("write: %v", err)
		}
		_ = raw.Close()
	}

	cli, err := DialUDP(srv.Addr().String(), srv.PublicKey(), []byte("tok"))
	if err != nil {
		t.Fatalf("a real client after four unanswered hellos: %v", err)
	}
	defer func() { _ = cli.Close() }()
	if n := calls.Load(); n != 1 {
		t.Fatalf("Verify ran %d times, want once for the real client", n)
	}
	if p := srv.Peers(); len(p) != 1 || p[0] != cli.LocalID() {
		t.Fatalf("Peers = %v, want only the real client %d", p, cli.LocalID())
	}
}

func TestUDPServerAnswersAHelloWithoutAValidCookieWithACookie(t *testing.T) {
	var calls atomic.Int32
	srv := listenUDPServer(t, Auth{Verify: countingVerify(&calls)})
	raw := dialRawUDP(t, srv)
	h, _ := newHello([]byte("tok"), resumeState{})
	for _, cookie := range [][udpCookieSize]byte{{}, testCookie} {
		dgram, _, err := encodeUDPHelloV2(srv.PublicKey(), h, cookie)
		if err != nil {
			t.Fatalf("encodeUDPHelloV2: %v", err)
		}
		expectCookieReply(t, raw, dgram, h.nonce, cookie)
	}
	if n, s := calls.Load(), serverSessions(srv); n != 0 || s != 0 {
		t.Fatalf("Verify ran %d times and the server holds %d sessions, want none", n, s)
	}
	expectNoEvent(t, srv)
}

func TestUDPServerRefusesACookieIssuedToAnotherAddress(t *testing.T) {
	var calls atomic.Int32
	srv := listenUDPServer(t, Auth{Verify: countingVerify(&calls)})
	a, b := dialRawUDP(t, srv), dialRawUDP(t, srv)
	h, _ := newHello([]byte("tok"), resumeState{})
	cookie := requestCookie(t, a, h.nonce)
	dgram, _, err := encodeUDPHelloV2(srv.PublicKey(), h, cookie)
	if err != nil {
		t.Fatalf("encodeUDPHelloV2: %v", err)
	}
	expectCookieReply(t, b, dgram, h.nonce, cookie)
	if n, s := calls.Load(), serverSessions(srv); n != 0 || s != 0 {
		t.Fatalf("Verify ran %d times and the server holds %d sessions, want none", n, s)
	}
}

func TestUDPServerRefusesAHelloReplayedFromAnotherAddress(t *testing.T) {
	var calls atomic.Int32
	srv := listenUDPServer(t, Auth{Verify: countingVerify(&calls)})
	victim, attacker := dialRawUDP(t, srv), dialRawUDP(t, srv)
	h, _ := newHello([]byte("tok"), resumeState{})
	captured, init := cookieHello(t, victim, srv, h)

	// The attacker obtains a cookie of its own for the captured nonce and splices it into the captured hello. The cookie is part of the Noise prologue, so the hello no longer decrypts and the server answers nothing.
	spliced := bytes.Clone(captured)
	own := requestCookie(t, attacker, h.nonce)
	copy(spliced[udpHelloPrefixSize:udpHelloHeader], own[:])
	if _, err := attacker.Write(spliced); err != nil {
		t.Fatalf("write: %v", err)
	}
	// The next reply the attacker gets answers this later request, which proves the spliced hello was processed and ignored.
	if got := requestCookie(t, attacker, h.nonce); got != own {
		t.Fatalf("cookie changed within one window: %x then %x", own, got)
	}
	if n, s := calls.Load(), serverSessions(srv); n != 0 || s != 0 {
		t.Fatalf("Verify ran %d times and the server holds %d sessions, want none", n, s)
	}

	// Sent from the address its cookie was issued to, the captured hello is admitted.
	if _, _, _, err := acceptUDPServerHandshakeV2(init, h.nonce, udpExchange(t, victim, captured)); err != nil {
		t.Fatalf("the victim's own hello: %v", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("Verify ran %d times, want once", n)
	}
}

func TestUDPServerAcceptsACookieForTwoWindowsAtMost(t *testing.T) {
	clock := newFakeClock()
	var calls atomic.Int32
	srv := listenUDPServer(t, Auth{Verify: countingVerify(&calls), Now: clock.Now})
	raw := dialRawUDP(t, srv)

	// A cookie from the window before still counts, so a window boundary during a handshake does not cost a round trip.
	h, _ := newHello([]byte("tok"), resumeState{})
	dgram, init := cookieHello(t, raw, srv, h)
	clock.Advance(udpCookieWindow)
	if _, _, _, err := acceptUDPServerHandshakeV2(init, h.nonce, udpExchange(t, raw, dgram)); err != nil {
		t.Fatalf("a cookie from the previous window: %v", err)
	}

	// Two windows later it has expired: the hello gets a fresh cookie, takes no slot and does not reach Verify.
	h2, _ := newHello([]byte("tok"), resumeState{})
	cookie := requestCookie(t, raw, h2.nonce)
	dgram2, _, err := encodeUDPHelloV2(srv.PublicKey(), h2, cookie)
	if err != nil {
		t.Fatalf("encodeUDPHelloV2: %v", err)
	}
	clock.Advance(2 * udpCookieWindow)
	expectCookieReply(t, raw, dgram2, h2.nonce, cookie)
	if n, s := calls.Load(), serverSessions(srv); n != 1 || s != 1 {
		t.Fatalf("Verify ran %d times and the server holds %d sessions, want one each", n, s)
	}
}

func TestUDPClientConnectsDuringAFloodOfHellosWithoutCookies(t *testing.T) {
	var calls atomic.Int32
	srv := listenUDPServer(t, Auth{MaxPeers: 4, Verify: countingVerify(&calls)})
	h, _ := newHello([]byte("tok"), resumeState{})
	flood, _, err := encodeUDPHelloV2(srv.PublicKey(), h, testCookie)
	if err != nil {
		t.Fatalf("encodeUDPHelloV2: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		raw := dialRawUDP(t, srv)
		wg.Go(func() {
			// About 2500 hellos a second from each of four sockets, 10000 in all.
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-stop:
					return
				case <-tick.C:
					for range 25 {
						_, _ = raw.Write(flood)
					}
				}
			}
		})
	}
	time.Sleep(50 * time.Millisecond)
	cli, err := DialUDP(srv.Addr().String(), srv.PublicKey(), []byte("tok"))
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatalf("DialUDP during the flood: %v", err)
	}
	defer func() { _ = cli.Close() }()
	if n := calls.Load(); n != 1 {
		t.Fatalf("Verify ran %d times, want once for the real client", n)
	}
	if p := srv.Peers(); len(p) != 1 {
		t.Fatalf("Peers = %v, want only the real client", p)
	}
}

// helloForCookie builds the Noise hello for h that carries cookie.
func helloForCookie(t *testing.T, srv *UDPServer, h hello, cookie [udpCookieSize]byte) ([]byte, *noiseNKInitiator) {
	t.Helper()
	dgram, init, err := encodeUDPHelloV2(srv.PublicKey(), h, cookie)
	if err != nil {
		t.Fatalf("encodeUDPHelloV2: %v", err)
	}
	return dgram, init
}

// expectIgnored sends dgram from raw and checks that the server answered nothing: the next reply raw gets is the cookie for a later request.
func expectIgnored(t *testing.T, raw stdnet.Conn, dgram []byte) {
	t.Helper()
	if _, err := raw.Write(dgram); err != nil {
		t.Fatalf("write: %v", err)
	}
	probe, _ := newHello(nil, resumeState{})
	requestCookie(t, raw, probe.nonce)
}

// rebuiltHellos returns two hellos for h from raw, the first carrying the cookie of one window and the second the cookie of the next, as a client whose cookie requests straddle a window boundary sends them.
func rebuiltHellos(t *testing.T, clock *fakeClock, raw stdnet.Conn, srv *UDPServer, h hello) (a, b []byte, initB *noiseNKInitiator) {
	t.Helper()
	cookieA := requestCookie(t, raw, h.nonce)
	clock.Advance(udpCookieWindow)
	cookieB := requestCookie(t, raw, h.nonce)
	if cookieA == cookieB {
		t.Fatal("cookies of two windows are equal")
	}
	a, _ = helloForCookie(t, srv, h, cookieA)
	b, initB = helloForCookie(t, srv, h, cookieB)
	return a, b, initB
}

func TestUDPServerRebindsAHelloRebuiltForANewerCookie(t *testing.T) {
	clock := newFakeClock()
	var calls atomic.Int32
	srv := listenUDPServer(t, Auth{Verify: countingVerify(&calls), Now: clock.Now})
	raw := dialRawUDP(t, srv)
	h, _ := newHello([]byte("tok"), resumeState{})
	helloA, helloB, initB := rebuiltHellos(t, clock, raw, srv, h)

	// The server binds hello A, but its welcome reaches a client that has already replaced it with hello B.
	udpExchange(t, raw, helloA)
	welcomeB := udpExchange(t, raw, helloB)
	w, _, _, err := acceptUDPServerHandshakeV2(initB, h.nonce, welcomeB)
	if err != nil {
		t.Fatalf("welcome to the rebuilt hello: %v", err)
	}
	if n, s := calls.Load(), serverSessions(srv); n != 1 || s != 1 {
		t.Fatalf("Verify ran %d times and the server holds %d sessions, want one each", n, s)
	}
	if p := srv.Peers(); len(p) != 1 || p[0] != w.peer {
		t.Fatalf("Peers = %v, want only %d", p, w.peer)
	}
	if ev := nextEvent(t, srv, srv.changed); ev.Kind != PeerJoined || ev.Peer != w.peer {
		t.Fatalf("event = %+v, want PeerJoined for %d", ev, w.peer)
	}
	expectNoEvent(t, srv)

	// A late copy of hello A must not rekey the session away from the client, and hello B still gets its welcome.
	expectIgnored(t, raw, helloA)
	if again := udpExchange(t, raw, helloB); !bytes.Equal(again, welcomeB) {
		t.Fatal("hello B got a different welcome after a late hello A")
	}
}

func TestUDPServerRebindsAResumeHelloRebuiltForANewerCookie(t *testing.T) {
	clock := newFakeClock()
	var calls atomic.Int32
	srv := listenUDPServer(t, Auth{Verify: countingVerify(&calls), Now: clock.Now, GracePeriod: time.Minute})
	raw := dialRawUDP(t, srv)
	first, _ := newHello([]byte("tok"), resumeState{})
	dgram, init := cookieHello(t, raw, srv, first)
	joined, _, _, err := acceptUDPServerHandshakeV2(init, first.nonce, udpExchange(t, raw, dgram))
	if err != nil {
		t.Fatalf("first hello: %v", err)
	}

	h, _ := newHello([]byte("tok"), resumeState{peer: joined.peer, secret: joined.secret})
	helloA, helloB, initB := rebuiltHellos(t, clock, raw, srv, h)
	udpExchange(t, raw, helloA)
	w, _, _, err := acceptUDPServerHandshakeV2(initB, h.nonce, udpExchange(t, raw, helloB))
	if err != nil {
		t.Fatalf("welcome to the rebuilt resume hello: %v", err)
	}
	if !w.resumed || w.peer != joined.peer {
		t.Fatalf("welcome = peer %d resumed %v, want peer %d resumed", w.peer, w.resumed, joined.peer)
	}
	if n, s := calls.Load(), serverSessions(srv); n != 1 || s != 1 {
		t.Fatalf("Verify ran %d times and the server holds %d sessions, want one each", n, s)
	}
}

func TestUDPServerIgnoresAnAdmittedHelloReplayedAfterItsSessionLeft(t *testing.T) {
	clock := newFakeClock()
	var calls atomic.Int32
	srv := listenUDPServer(t, Auth{Verify: countingVerify(&calls), Now: clock.Now})
	raw := dialRawUDP(t, srv)
	h, _ := newHello([]byte("tok"), resumeState{})
	dgram, init := cookieHello(t, raw, srv, h)
	w, _, _, err := acceptUDPServerHandshakeV2(init, h.nonce, udpExchange(t, raw, dgram))
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	if err := srv.Kick(w.peer, "bye"); err != nil {
		t.Fatalf("Kick: %v", err)
	}
	// Drain the kick datagrams.
	for range udpKickCopies {
		_ = raw.SetReadDeadline(time.Now().Add(time.Second))
		_, _ = raw.Read(make([]byte, 2048))
	}

	// The hello's cookie is still valid, but its nonce was admitted moments ago, so a replay joins nobody.
	expectIgnored(t, raw, dgram)
	if n, s := calls.Load(), serverSessions(srv); n != 1 || s != 0 {
		t.Fatalf("Verify ran %d times and the server holds %d sessions, want one call and no session", n, s)
	}

	// Once the cookie has expired the replay only earns a fresh cookie.
	clock.Advance(2 * udpCookieWindow)
	expectCookieReply(t, raw, dgram, h.nonce, [udpCookieSize]byte(dgram[udpHelloPrefixSize:udpHelloHeader]))
	if n := calls.Load(); n != 1 {
		t.Fatalf("Verify ran %d times, want once", n)
	}
}
