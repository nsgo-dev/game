//go:build !js

package net

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	stdnet "net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// awaitChange blocks on a transport's change signal until cond holds, so tests wait for the event they need instead of sleeping. The timeout only bounds a broken test.
func awaitChange(t *testing.T, changed <-chan struct{}, what string, cond func() bool) {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for !cond() {
		select {
		case <-changed:
		case <-timeout.C:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// nextEvent pops one event, waiting for it to arrive.
func nextEvent(t *testing.T, src PeerEvents, changed <-chan struct{}) PeerEvent {
	t.Helper()
	var ev PeerEvent
	awaitChange(t, changed, "a peer event", func() bool {
		var ok bool
		ev, ok = src.NextEvent()
		return ok
	})
	return ev
}

func expectNoEvent(t *testing.T, src PeerEvents) {
	t.Helper()
	if ev, ok := src.NextEvent(); ok {
		t.Fatalf("unexpected event %v for peer %d", ev.Kind, ev.Peer)
	}
}

func recvOn(t *testing.T, tr Transport, changed <-chan struct{}) Packet {
	t.Helper()
	var pkt Packet
	awaitChange(t, changed, "a packet", func() bool {
		var ok bool
		pkt, ok = tr.Recv()
		return ok
	})
	return pkt
}

// tokenAuth accepts exactly the token "good" and records the IDs Verify saw.
func tokenAuth(clock *fakeClock, grace time.Duration, seen *[]PeerID) Auth {
	return Auth{
		Verify: func(peer PeerID, token []byte) error {
			if seen != nil {
				*seen = append(*seen, peer)
			}
			if string(token) != "good" {
				return errors.New("bad token")
			}
			return nil
		},
		GracePeriod: grace,
		Now:         clock.Now,
	}
}

func listenWS(t *testing.T, auth Auth) (*WebSocketServer, string) {
	t.Helper()
	srv, err := ListenWebSocket("127.0.0.1:0", 1, auth)
	if err != nil {
		t.Fatalf("ListenWebSocket: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv, "ws://" + srv.Addr().String() + "/game"
}

// cutWS breaks a client's TCP connection without a close frame, as a network drop would.
func cutWS(wt *WebSocketTransport) {
	wt.mu.Lock()
	conn := wt.conn
	wt.mu.Unlock()
	_ = conn.Close()
}

func TestWebSocketRejectsAnInvalidToken(t *testing.T) {
	clock := newFakeClock()
	srv, url := listenWS(t, tokenAuth(clock, 0, nil))

	_, err := DialWebSocket(url, []byte("forged"))
	var rej *RejectError
	if !errors.As(err, &rej) || rej.Reason != "bad token" || !errors.Is(err, ErrRejected) {
		t.Fatalf("dial with a bad token = %v, want a rejection with the reason", err)
	}

	// A raw client that skips the handshake and sends a game frame straight away is cut off too.
	conn, br := rawUpgrade(t, srv)
	if err := writeWSFrame(conn, wsOpBinary, true, encodeWireFrame(ChannelReliableOrdered, 1, []byte("sneak"))); err != nil {
		t.Fatalf("write: %v", err)
	}
	for {
		_, op, _, _, err := readWSFrame(br)
		if err != nil || op == wsOpClose {
			break
		}
	}
	_ = conn.Close()

	good, err := DialWebSocket(url, []byte("good"))
	if err != nil {
		t.Fatalf("dial with a good token: %v", err)
	}
	defer func() { _ = good.Close() }()
	if ev := nextEvent(t, srv, srv.changed); ev.Kind != PeerJoined || ev.Peer != good.LocalID() {
		t.Fatalf("first event = %+v, want only the good client joining", ev)
	}
	if err := good.Send(PeerBroadcast, ChannelReliableOrdered, []byte("hi")); err != nil {
		t.Fatalf("send: %v", err)
	}
	if pkt := recvOn(t, srv, srv.changed); pkt.From != good.LocalID() || string(pkt.Payload) != "hi" {
		t.Fatalf("first packet = %+v, want the good client's", pkt)
	}
	if got := srv.Peers(); len(got) != 1 || got[0] != good.LocalID() {
		t.Fatalf("Peers = %v, want only %d", got, good.LocalID())
	}
}

// rawUpgrade performs only the HTTP upgrade against srv.
func rawUpgrade(t *testing.T, srv *WebSocketServer) (stdnet.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := stdnet.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req := "GET / HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade response = %v, %v", resp, err)
	}
	_ = resp.Body.Close()
	return conn, br
}

func TestWebSocketServerAssignsIDsAndVerifySeesThem(t *testing.T) {
	clock := newFakeClock()
	var seen []PeerID
	srv, err := ListenWebSocket("127.0.0.1:0", 10, tokenAuth(clock, 0, &seen))
	if err != nil {
		t.Fatalf("ListenWebSocket: %v", err)
	}
	defer func() { _ = srv.Close() }()
	url := "ws://" + srv.Addr().String()

	a, err := DialWebSocket(url, []byte("good"))
	if err != nil {
		t.Fatalf("dial a: %v", err)
	}
	defer func() { _ = a.Close() }()
	b, err := DialWebSocket(url, []byte("good"))
	if err != nil {
		t.Fatalf("dial b: %v", err)
	}
	defer func() { _ = b.Close() }()

	if a.LocalID() != 11 || b.LocalID() != 12 {
		t.Fatalf("assigned IDs = %d, %d; want 11, 12 after server 10", a.LocalID(), b.LocalID())
	}
	if len(seen) != 2 || seen[0] != 11 || seen[1] != 12 {
		t.Fatalf("Verify saw %v, want [11 12]", seen)
	}
	if p := a.Peers(); len(p) != 1 || p[0] != 10 {
		t.Fatalf("client Peers = %v, want the server 10", p)
	}
	if a.State() != ConnOpen || a.Resumed() || a.Err() != nil {
		t.Fatalf("client state = %v resumed=%v err=%v", a.State(), a.Resumed(), a.Err())
	}
}

func TestWebSocketResumesWithinTheGracePeriod(t *testing.T) {
	clock := newFakeClock()
	srv, url := listenWS(t, tokenAuth(clock, 5*time.Second, nil))

	cli, err := DialWebSocket(url, []byte("good"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cli.Close() }()
	id := cli.LocalID()
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerJoined, id}) {
		t.Fatalf("event = %+v, want joined", ev)
	}

	cutWS(cli)
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerDropped, id}) {
		t.Fatalf("event = %+v, want dropped", ev)
	}
	awaitChange(t, cli.changed, "the client to notice the drop", func() bool { return cli.State() == ConnDisconnected })
	if err := cli.Send(PeerBroadcast, ChannelReliableOrdered, []byte("x")); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("send while dropped = %v, want ErrDisconnected", err)
	}
	if err := srv.Send(id, ChannelReliableOrdered, []byte("x")); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("server send in grace = %v, want ErrDisconnected", err)
	}
	if len(srv.Peers()) != 0 {
		t.Fatalf("Peers in grace = %v, want none", srv.Peers())
	}

	clock.Advance(4 * time.Second)
	if err := cli.Reconnect(); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if cli.LocalID() != id || !cli.Resumed() {
		t.Fatalf("after reconnect ID=%d resumed=%v, want %d resumed", cli.LocalID(), cli.Resumed(), id)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerResumed, id}) {
		t.Fatalf("event = %+v, want resumed", ev)
	}
	if err := srv.Send(id, ChannelReliableOrdered, []byte("welcome back")); err != nil {
		t.Fatalf("server send after resume: %v", err)
	}
	if pkt := recvOn(t, cli, cli.changed); string(pkt.Payload) != "welcome back" {
		t.Fatalf("client got %q", pkt.Payload)
	}

	// Past the grace period the session ends, and a reconnect is a new peer.
	cutWS(cli)
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerDropped, id}) {
		t.Fatalf("event = %+v, want dropped", ev)
	}
	clock.Advance(5 * time.Second)
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerLeft, id}) {
		t.Fatalf("event = %+v, want left after the grace period", ev)
	}
	if err := srv.Send(id, ChannelReliableOrdered, []byte("x")); !errors.Is(err, ErrUnknownPeer) {
		t.Fatalf("server send after leave = %v, want ErrUnknownPeer", err)
	}
	awaitChange(t, cli.changed, "the client to notice the drop", func() bool { return cli.State() == ConnDisconnected })
	if err := cli.Reconnect(); err != nil {
		t.Fatalf("late reconnect: %v", err)
	}
	if cli.LocalID() == id || cli.Resumed() {
		t.Fatalf("late reconnect kept ID %d (resumed=%v)", cli.LocalID(), cli.Resumed())
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerJoined, cli.LocalID()}) {
		t.Fatalf("event = %+v, want a new join", ev)
	}
}

func TestWebSocketCloseLeavesAtOnce(t *testing.T) {
	clock := newFakeClock()
	srv, url := listenWS(t, tokenAuth(clock, time.Minute, nil))
	cli, err := DialWebSocket(url, []byte("good"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	id := cli.LocalID()
	_ = nextEvent(t, srv, srv.changed)
	if err := cli.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerLeft, id}) {
		t.Fatalf("event = %+v, want left without waiting for the grace period", ev)
	}
	if err := cli.Reconnect(); !errors.Is(err, ErrClosed) {
		t.Fatalf("reconnect after close = %v, want ErrClosed", err)
	}
}

func TestWebSocketResumeNeedsTheSecret(t *testing.T) {
	clock := newFakeClock()
	srv, url := listenWS(t, tokenAuth(clock, time.Minute, nil))
	victim, err := DialWebSocket(url, []byte("good"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = victim.Close() }()
	_ = nextEvent(t, srv, srv.changed)

	// Another client claims the victim's ID with a guessed secret: it is admitted, if at all, only as a new peer.
	conn, br := rawUpgrade(t, srv)
	defer func() { _ = conn.Close() }()
	h, _ := newHello([]byte("good"), resumeState{peer: victim.LocalID()})
	if err := writeWSFrame(conn, wsOpBinary, true, encodeHello(h, 0)); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	_, _, payload, _, err := readWSFrame(br)
	if err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	w, err := decodeServerHandshake(payload)
	if err != nil || w.peer == victim.LocalID() || w.resumed {
		t.Fatalf("hijack attempt got %+v, %v", w, err)
	}
	if victim.State() != ConnOpen {
		t.Fatalf("victim state = %v", victim.State())
	}
}

func TestWebSocketRejectsAnotherHandshakeVersion(t *testing.T) {
	srv, _ := listenWS(t, Auth{})
	conn, br := rawUpgrade(t, srv)
	defer func() { _ = conn.Close() }()
	h := encodeHello(hello{}, 0)
	h[1] = hsVersion + 1
	if err := writeWSFrame(conn, wsOpBinary, true, h); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, payload, _, err := readWSFrame(br)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := decodeServerHandshake(payload); !strings.Contains(errString(err), hsReasonVersion) {
		t.Fatalf("reply = %v, want a version rejection", err)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func listenUDPServer(t *testing.T, auth Auth) *UDPServer {
	t.Helper()
	srv, err := ListenUDPServer("127.0.0.1:0", 1, auth)
	if err != nil {
		t.Fatalf("ListenUDPServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// cutUDP closes a client's socket without a goodbye, as a lost network would, and stops its pings.
func cutUDP(c *UDPClient) {
	c.mu.Lock()
	conn := c.detachLocked()
	c.state = ConnDisconnected
	c.mu.Unlock()
	_ = conn.Close()
}

func TestUDPServerRejectsAnInvalidToken(t *testing.T) {
	clock := newFakeClock()
	srv := listenUDPServer(t, tokenAuth(clock, 0, nil))
	addr := srv.Addr().String()

	_, err := DialUDP(addr, []byte("forged"))
	var rej *RejectError
	if !errors.As(err, &rej) || rej.Reason != "bad token" {
		t.Fatalf("dial with a bad token = %v, want a rejection with the reason", err)
	}

	// Sealed data from a peer with no session never reaches Recv.
	raw, err := stdnet.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial raw: %v", err)
	}
	defer func() { _ = raw.Close() }()
	var key [hsKeySize]byte
	if _, err := raw.Write(sealUDP(&key, udpData, 2, 1, ChannelUnreliable, []byte("sneak"))); err != nil {
		t.Fatalf("write: %v", err)
	}

	good, err := DialUDP(addr, []byte("good"))
	if err != nil {
		t.Fatalf("dial with a good token: %v", err)
	}
	defer func() { _ = good.Close() }()
	if err := good.Send(PeerBroadcast, ChannelUnreliable, []byte("hi")); err != nil {
		t.Fatalf("send: %v", err)
	}
	if pkt := recvOn(t, srv, srv.changed); pkt.From != good.LocalID() || string(pkt.Payload) != "hi" {
		t.Fatalf("first packet = %+v, want the good client's", pkt)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerJoined, good.LocalID()}) {
		t.Fatalf("event = %+v, want only the good client joining", ev)
	}
	expectNoEvent(t, srv)
}

func TestUDPServerIgnoresShortHellos(t *testing.T) {
	srv := listenUDPServer(t, Auth{})
	raw, err := stdnet.Dial("udp", srv.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = raw.Close() }()
	// An unpadded hello could make the server send more than it received, so it gets no answer and no session.
	if _, err := raw.Write(encodeHello(hello{}, 0)); err != nil {
		t.Fatalf("write: %v", err)
	}
	// A padded one is answered; its reply proves the short one was processed first and ignored.
	h := hello{}
	h.nonce[0] = 7
	if _, err := raw.Write(encodeHello(h, udpMinHello)); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = raw.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 512)
	n, err := raw.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	w, err := decodeServerHandshake(buf[:n])
	if err != nil || w.nonce != h.nonce || w.peer != 2 {
		t.Fatalf("reply = %+v, %v; want the padded hello's welcome as the only session", w, err)
	}
}

func TestUDPPacketsAreBoundToTheAuthenticatedPeer(t *testing.T) {
	clock := newFakeClock()
	srv := listenUDPServer(t, tokenAuth(clock, time.Minute, nil))
	cli, err := DialUDP(srv.Addr().String(), []byte("good"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cli.Close() }()
	id := cli.LocalID()

	attacker, err := stdnet.Dial("udp", srv.Addr().String())
	if err != nil {
		t.Fatalf("dial attacker: %v", err)
	}
	defer func() { _ = attacker.Close() }()

	cli.mu.Lock()
	key := cli.key
	cli.mu.Unlock()
	var zero [hsKeySize]byte
	// From another address: a forged tag, then a correctly sealed datagram as if the key had leaked.
	_, _ = attacker.Write(sealUDP(&zero, udpData, id, 1000, ChannelUnreliable, []byte("forged")))
	_, _ = attacker.Write(sealUDP(&key, udpData, id, 1001, ChannelUnreliable, []byte("moved")))

	// From the client's own address: a forged tag, and a replay of a datagram already delivered.
	cli.mu.Lock()
	conn := cli.conn
	cli.mu.Unlock()
	_, _ = conn.Write(sealUDP(&zero, udpData, id, 2000, ChannelUnreliable, []byte("forged")))
	legit := sealUDP(&key, udpData, id, 2001, ChannelUnreliable, []byte("legit"))
	_, _ = conn.Write(legit)
	_, _ = conn.Write(legit)
	_, _ = conn.Write(sealUDP(&key, udpData, id, 2002, ChannelUnreliable, []byte("last")))

	var got []string
	awaitChange(t, srv.changed, "the client's packets", func() bool {
		for pkt, ok := srv.Recv(); ok; pkt, ok = srv.Recv() {
			if pkt.From != id {
				t.Fatalf("packet from %d", pkt.From)
			}
			got = append(got, string(pkt.Payload))
		}
		return len(got) > 0 && got[len(got)-1] == "last"
	})
	if strings.Join(got, ",") != "legit,last" {
		t.Fatalf("server delivered %v, want only [legit last]", got)
	}

	// The server still answers the client at its own address, not the attacker's.
	if err := srv.Send(id, ChannelUnreliable, []byte("to you")); err != nil {
		t.Fatalf("server send: %v", err)
	}
	if pkt := recvOn(t, cli, cli.changed); string(pkt.Payload) != "to you" {
		t.Fatalf("client got %q", pkt.Payload)
	}
}

func TestUDPClientResumesWithinTheGracePeriod(t *testing.T) {
	clock := newFakeClock()
	auth := tokenAuth(clock, 5*time.Second, nil)
	auth.IdleTimeout = 3 * time.Second
	srv := listenUDPServer(t, auth)
	cli, err := DialUDP(srv.Addr().String(), []byte("good"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cli.Close() }()
	id := cli.LocalID()
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerJoined, id}) {
		t.Fatalf("event = %+v, want joined", ev)
	}

	// A silent client counts as dropped once the idle timeout passes.
	cutUDP(cli)
	clock.Advance(3 * time.Second)
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerDropped, id}) {
		t.Fatalf("event = %+v, want dropped after the idle timeout", ev)
	}
	if err := srv.Send(id, ChannelUnreliable, []byte("x")); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("server send in grace = %v, want ErrDisconnected", err)
	}

	clock.Advance(4 * time.Second)
	if err := cli.Reconnect(); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if cli.LocalID() != id || !cli.Resumed() {
		t.Fatalf("after reconnect ID=%d resumed=%v, want %d resumed", cli.LocalID(), cli.Resumed(), id)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerResumed, id}) {
		t.Fatalf("event = %+v, want resumed", ev)
	}
	if err := cli.Send(PeerBroadcast, ChannelUnreliable, []byte("back")); err != nil {
		t.Fatalf("send: %v", err)
	}
	if pkt := recvOn(t, srv, srv.changed); pkt.From != id || string(pkt.Payload) != "back" {
		t.Fatalf("server got %+v", pkt)
	}

	// Past the grace period the server reports a leave.
	cutUDP(cli)
	clock.Advance(3 * time.Second)
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerDropped, id}) {
		t.Fatalf("event = %+v, want dropped", ev)
	}
	clock.Advance(5 * time.Second)
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerLeft, id}) {
		t.Fatalf("event = %+v, want left after the grace period", ev)
	}
	if err := cli.Reconnect(); err != nil {
		t.Fatalf("late reconnect: %v", err)
	}
	if cli.LocalID() == id || cli.Resumed() {
		t.Fatalf("late reconnect kept ID %d (resumed=%v)", cli.LocalID(), cli.Resumed())
	}
}

func TestUDPClientCloseLeavesAtOnce(t *testing.T) {
	clock := newFakeClock()
	srv := listenUDPServer(t, tokenAuth(clock, time.Minute, nil))
	cli, err := DialUDP(srv.Addr().String(), []byte("good"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	id := cli.LocalID()
	_ = nextEvent(t, srv, srv.changed)
	if err := cli.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerLeft, id}) {
		t.Fatalf("event = %+v, want left without waiting for the grace period", ev)
	}
	if err := cli.Send(PeerBroadcast, ChannelUnreliable, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("send after close = %v", err)
	}
}

func TestUDPTransportIgnoresSpoofedSenders(t *testing.T) {
	u1, err := ListenUDP("127.0.0.1:0", 1)
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer func() { _ = u1.Close() }()
	u2, err := ListenUDP("127.0.0.1:0", 2)
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer func() { _ = u2.Close() }()
	if err := u1.AddPeer(2, u2.Addr().String()); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	// An unregistered socket claims to be peer 2 and peer 3.
	spoof, err := stdnet.Dial("udp", u1.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = spoof.Close() }()
	for _, from := range []byte{2, 3} {
		dgram := append([]byte{0, 0, 0, from}, encodeWireFrame(ChannelUnreliable, 1, []byte("spoof"))...)
		_, _ = spoof.Write(dgram)
	}
	if err := u2.Send(1, ChannelUnreliable, nil); !errors.Is(err, ErrUnknownPeer) {
		t.Fatalf("u2 send before registering u1 = %v", err)
	}
	if err := u2.AddPeer(1, u1.Addr().String()); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	if err := u2.Send(1, ChannelUnreliable, []byte("real")); err != nil {
		t.Fatalf("send: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var pkt Packet
	for ok := false; !ok; pkt, ok = u1.Recv() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for u2's packet")
		}
		<-time.After(time.Millisecond)
	}
	if pkt.From != 2 || !bytes.Equal(pkt.Payload, []byte("real")) {
		t.Fatalf("first packet = %+v, want u2's real one", pkt)
	}
	if p := u1.Peers(); len(p) != 1 || p[0] != 2 {
		t.Fatalf("Peers = %v, want only the registered 2", p)
	}
}

func TestReplayWindow(t *testing.T) {
	var w replayWindow
	for _, tc := range []struct {
		seq  uint32
		want bool
	}{
		{0, false}, {1, true}, {1, false}, {3, true}, {2, true}, {2, false},
		{100, true}, {36, false}, {37, true}, {37, false}, {200, true}, {100, false},
	} {
		if got := w.accept(tc.seq); got != tc.want {
			t.Fatalf("accept(%d) = %v, want %v", tc.seq, got, tc.want)
		}
	}
}

func BenchmarkSealUDP(b *testing.B) {
	var key [hsKeySize]byte
	payload := make([]byte, 256)
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		dgram := sealUDP(&key, udpData, 2, 1, ChannelUnreliable, payload)
		if !openUDP(&key, dgram) {
			b.Fatal("tag mismatch")
		}
	}
}
