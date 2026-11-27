//go:build !js

package net

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	stdnet "net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// relayJoin admits the token "<session>/<player>" to <session> and refuses "bad".
func relayJoin(_ PeerID, token []byte) (string, error) {
	if string(token) == "bad" {
		return "", errors.New("bad token")
	}
	session, _, _ := strings.Cut(string(token), "/")
	return session, nil
}

// relayClient is a RelayTransport with its inner transport, whose change signal a test waits on.
type relayClient struct {
	*RelayTransport
	inner   Transport
	changed <-chan struct{}
}

// listenRelay listens on addr ("127.0.0.1:0" for any port) for a relay over proto ("websocket" or "udp") and returns the server and the address clients dial.
func listenRelay(t testing.TB, proto, addr string, auth Auth) (RelayServer, string) {
	t.Helper()
	switch proto {
	case "websocket":
		ws, err := ListenWebSocket(addr, 1, auth)
		if err != nil {
			t.Fatalf("ListenWebSocket: %v", err)
		}
		return ws, "ws://" + ws.Addr().String() + "/relay"
	case "udp":
		us, err := ListenUDPServer(addr, 1, auth)
		if err != nil {
			t.Fatalf("ListenUDPServer: %v", err)
		}
		return us, us.Addr().String()
	}
	t.Fatalf("unknown protocol %q", proto)
	return nil, ""
}

// dialRelay connects a RelayTransport over proto to the relay at addr.
func dialRelay(proto, addr, token string, now func() time.Time) (*relayClient, error) {
	var inner Transport
	var changed <-chan struct{}
	switch proto {
	case "websocket":
		ws, err := DialWebSocket(addr, []byte(token))
		if err != nil {
			return nil, err
		}
		inner, changed = ws, ws.changed
	default:
		uc, err := DialUDP(addr, []byte(token))
		if err != nil {
			return nil, err
		}
		inner, changed = uc, uc.changed
	}
	return &relayClient{RelayTransport: NewRelayTransport(inner, RelayTransportConfig{Now: now}), inner: inner, changed: changed}, nil
}

// serveRelay runs relay.Serve on srv until the returned stop is called, which reports Serve's error.
func serveRelay(relay *Relay, srv RelayServer) (stop func() error) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- relay.Serve(ctx, srv) }()
	return func() error {
		cancel()
		return <-done
	}
}

// startRelay serves a relay over proto ("websocket" or "udp") and returns a function that connects clients to it. The relay stops and the server closes when the test ends.
func startRelay(t testing.TB, proto string, cfg RelayConfig, base Auth) (*Relay, func(token string, now func() time.Time) (*relayClient, error)) {
	t.Helper()
	if cfg.Join == nil {
		cfg.Join = relayJoin
	}
	relay, err := NewRelay(cfg)
	if err != nil {
		t.Fatalf("NewRelay: %v", err)
	}
	srv, addr := listenRelay(t, proto, "127.0.0.1:0", relay.Auth(base))
	stop := serveRelay(relay, srv)

	var mu sync.Mutex
	var clients []Transport
	t.Cleanup(func() {
		mu.Lock()
		for _, c := range clients {
			_ = c.Close()
		}
		mu.Unlock()
		if err := stop(); !errors.Is(err, context.Canceled) {
			t.Errorf("Serve returned %v, want context.Canceled", err)
		}
		_ = srv.Close()
	})

	dial := func(token string, now func() time.Time) (*relayClient, error) {
		c, err := dialRelay(proto, addr, token, now)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		clients = append(clients, c.inner)
		mu.Unlock()
		return c, nil
	}
	return relay, dial
}

func mustDialRelay(t testing.TB, dial func(string, func() time.Time) (*relayClient, error), token string) *relayClient {
	t.Helper()
	c, err := dial(token, nil)
	if err != nil {
		t.Fatalf("dial %q: %v", token, err)
	}
	return c
}

// awaitPeers waits until c's session lists exactly want besides itself.
func awaitPeers(t testing.TB, c *relayClient, want ...PeerID) {
	t.Helper()
	slices.Sort(want)
	awaitChangeTB(t, c.changed, "the session's members", func() bool { return slices.Equal(c.Peers(), want) })
}

// awaitChangeTB is awaitChange for tests and benchmarks.
func awaitChangeTB(t testing.TB, changed <-chan struct{}, what string, cond func() bool) {
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

// awaitEither waits on two clients' change signals until cond holds.
func awaitEither(t testing.TB, a, b <-chan struct{}, what string, cond func() bool) {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for !cond() {
		select {
		case <-a:
		case <-b:
		case <-timeout.C:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func recvRelay(t testing.TB, c *relayClient) Packet {
	t.Helper()
	var pkt Packet
	awaitChangeTB(t, c.changed, "a relayed packet", func() bool {
		var ok bool
		pkt, ok = c.Recv()
		return ok
	})
	return pkt
}

// relaySim is a deterministic simulation whose state depends on every input and on their order.
type relaySim struct {
	tick uint32
	acc  uint64
}

func (s *relaySim) Step(inputs []InputFrame) {
	for _, in := range inputs {
		s.acc = (s.acc ^ uint64(in.Peer)<<40 ^ in.Bits ^ uint64(uint16(in.Axes[0]))<<16) * 1099511628211
	}
	s.tick++
}

func (s *relaySim) Save() []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint32(b[0:4], s.tick)
	binary.BigEndian.PutUint64(b[4:12], s.acc)
	return b
}

func (s *relaySim) Load(b []byte) error {
	if len(b) != 12 {
		return ErrInvalidPacket
	}
	s.tick = binary.BigEndian.Uint32(b[0:4])
	s.acc = binary.BigEndian.Uint64(b[4:12])
	return nil
}

func (s *relaySim) Checksum() uint64 { return ChecksumBytes(s.Save()) }

func relayInput(peer PeerID, tick uint32) InputFrame {
	return InputFrame{Tick: tick, Peer: peer, Bits: uint64(tick*7+uint32(peer)) % 5, Axes: [2]int16{int16(tick%9) - 4, int16(peer)}}
}

// TestRelayRunsARollbackSession runs a rollback session between two clients that only know the relay's address: neither listens for the other, so every input and checksum goes through the relay.
func TestRelayRunsARollbackSession(t *testing.T) {
	for _, proto := range []string{"websocket", "udp"} {
		t.Run(proto, func(t *testing.T) {
			relay, dial := startRelay(t, proto, RelayConfig{}, Auth{})
			a := mustDialRelay(t, dial, "match/alice")
			b := mustDialRelay(t, dial, "match/bob")
			awaitPeers(t, a, b.LocalID())
			awaitPeers(t, b, a.LocalID())

			simA, simB := &relaySim{}, &relaySim{}
			sa := NewRollbackSession(RollbackConfig{Transport: a, Sim: simA, MaxRollback: 8, ChecksumInterval: 5})
			sb := NewRollbackSession(RollbackConfig{Transport: b, Sim: simB, MaxRollback: 8, ChecksumInterval: 5})
			const ticks = 60
			for tick := range uint32(ticks) {
				if err := sa.Step(relayInput(a.LocalID(), tick)); err != nil {
					t.Fatalf("a step %d: %v", tick, err)
				}
				if err := sb.Step(relayInput(b.LocalID(), tick)); err != nil {
					t.Fatalf("b step %d: %v", tick, err)
				}
				awaitEither(t, a.changed, b.changed, "both peers to confirm the tick", func() bool {
					if err := sa.Poll(); err != nil {
						t.Fatalf("a poll: %v", err)
					}
					if err := sb.Poll(); err != nil {
						t.Fatalf("b poll: %v", err)
					}
					return sa.ConfirmedTick() >= int64(tick) && sb.ConfirmedTick() >= int64(tick)
				})
			}
			if simA.Checksum() != simB.Checksum() || simA.tick != ticks {
				t.Fatalf("states differ after %d ticks: a=%+v b=%+v", ticks, simA, simB)
			}
			if d := append(sa.Desyncs(), sb.Desyncs()...); len(d) != 0 {
				t.Fatalf("desyncs: %+v", d)
			}
			if sa.Stats().Rollbacks+sb.Stats().Rollbacks == 0 {
				t.Fatal("no rollback happened; the inputs should have been mispredicted")
			}
			if st := relay.Stats(); st.Sessions != 1 || st.Peers != 2 || st.Forwarded == 0 || st.Dropped != 0 {
				t.Fatalf("relay stats = %+v, want one session of two and nothing dropped", st)
			}
		})
	}
}

func TestRelayForwardsOnlyWithinASession(t *testing.T) {
	relay, dial := startRelay(t, "websocket", RelayConfig{}, Auth{})
	a := mustDialRelay(t, dial, "one/a")
	b := mustDialRelay(t, dial, "one/b")
	c := mustDialRelay(t, dial, "two/c")
	d := mustDialRelay(t, dial, "two/d")
	awaitPeers(t, a, b.LocalID())
	awaitPeers(t, b, a.LocalID())
	awaitPeers(t, c, d.LocalID())
	awaitPeers(t, d, c.LocalID())

	if err := a.Send(c.LocalID(), ChannelReliableOrdered, []byte("cross")); !errors.Is(err, ErrUnknownPeer) {
		t.Fatalf("send to another session's peer = %v, want ErrUnknownPeer", err)
	}
	if err := a.Send(PeerBroadcast, ChannelReliableOrdered, []byte("ours")); err != nil {
		t.Fatalf("broadcast: %v", err)
	}
	if pkt := recvRelay(t, b); pkt.From != a.LocalID() || string(pkt.Payload) != "ours" || pkt.Channel != ChannelReliableOrdered {
		t.Fatalf("b got %+v, want a's broadcast", pkt)
	}

	// A modified client addresses a peer of another session, and the relay itself (0), directly on its connection; the relay drops both. A frame c sends its own session afterwards proves the relay has handled them.
	before := relay.Stats()
	for _, to := range []PeerID{a.LocalID(), 0, c.LocalID()} {
		if err := c.inner.Send(PeerBroadcast, ChannelUnreliable, encodeRelayFrame(to, encodeRelayData(relayBodyUnreliable, 1, []byte("sneak")))); err != nil {
			t.Fatalf("raw send: %v", err)
		}
	}
	if err := c.inner.Send(PeerBroadcast, ChannelUnreliable, []byte{9, 9}); err != nil {
		t.Fatalf("raw send: %v", err)
	}
	if err := c.Send(d.LocalID(), ChannelUnreliable, []byte("after")); err != nil {
		t.Fatalf("send: %v", err)
	}
	if pkt := recvRelay(t, d); pkt.From != c.LocalID() || string(pkt.Payload) != "after" {
		t.Fatalf("d got %+v, want c's packet", pkt)
	}
	if got := relay.Stats().Dropped - before.Dropped; got != 4 {
		t.Fatalf("relay dropped %d frames, want 4: to another session, to 0, to the sender and malformed", got)
	}
	for _, rc := range []*relayClient{a, b, c} {
		if pkt, ok := rc.Recv(); ok {
			t.Fatalf("peer %d got %+v", rc.LocalID(), pkt)
		}
	}

	// Leaving updates the other member's view.
	_ = d.Close()
	awaitPeers(t, c)
	if st := relay.Stats(); st.Sessions != 2 || st.Peers != 3 {
		t.Fatalf("stats after d left = %+v, want 2 sessions and 3 peers", st)
	}
}

func TestRelayEnforcesTheHandshake(t *testing.T) {
	for _, proto := range []string{"websocket", "udp"} {
		t.Run(proto, func(t *testing.T) {
			base := Auth{Verify: func(_ PeerID, token []byte) error {
				if strings.HasSuffix(string(token), "/banned") {
					return errors.New("banned")
				}
				return nil
			}}
			relay, dial := startRelay(t, proto, RelayConfig{MaxSessionPeers: 2}, base)
			for token, reason := range map[string]string{"bad": "bad token", "duel/banned": "banned", "/nobody": "no session"} {
				_, err := dial(token, nil)
				var rej *RejectError
				if !errors.As(err, &rej) || rej.Reason != reason {
					t.Fatalf("dial %q = %v, want a rejection %q", token, err, reason)
				}
			}
			a := mustDialRelay(t, dial, "duel/a")
			b := mustDialRelay(t, dial, "duel/b")
			_, err := dial("duel/c", nil)
			var rej *RejectError
			if !errors.As(err, &rej) || rej.Reason != reasonSessionFull {
				t.Fatalf("third player in a session of two = %v, want %q", err, reasonSessionFull)
			}
			awaitPeers(t, a, b.LocalID())
			if st := relay.Stats(); st.Sessions != 1 || st.Peers != 2 {
				t.Fatalf("stats = %+v, want only the admitted pair", st)
			}
		})
	}
}

// TestRelayResendsAcrossAReconnect drops a client's connection, sends it a reliable message while it is away, and checks the message arrives exactly once after the client resumes its session.
func TestRelayResendsAcrossAReconnect(t *testing.T) {
	for _, proto := range []string{"websocket", "udp"} {
		t.Run(proto, func(t *testing.T) {
			clock := newFakeClock()
			_, dial := startRelay(t, proto, RelayConfig{}, Auth{GracePeriod: time.Minute})
			a, err := dial("room/a", clock.Now)
			if err != nil {
				t.Fatalf("dial a: %v", err)
			}
			b := mustDialRelay(t, dial, "room/b")
			awaitPeers(t, a, b.LocalID())
			awaitPeers(t, b, a.LocalID())

			id := b.LocalID()
			var reconnect func() error
			var resumed func() bool
			switch inner := b.inner.(type) {
			case *WebSocketTransport:
				cutWS(inner)
				awaitChangeTB(t, b.changed, "b to notice the drop", func() bool { return inner.State() == ConnDisconnected })
				reconnect, resumed = inner.Reconnect, inner.Resumed
			case *UDPClient:
				cutUDP(inner)
				reconnect, resumed = inner.Reconnect, inner.Resumed
			}
			if err := a.Send(id, ChannelReliableOrdered, []byte("while away")); err != nil {
				t.Fatalf("send: %v", err)
			}
			if err := reconnect(); err != nil || !resumed() || b.LocalID() != id {
				t.Fatalf("reconnect = %v, resumed %v, id %d; want the session back as %d", err, resumed(), b.LocalID(), id)
			}
			clock.Advance(relayMaxRTO)
			_, _ = a.Recv()
			pkt := recvRelay(t, b)
			if pkt.From != a.LocalID() || string(pkt.Payload) != "while away" || pkt.Seq != 1 {
				t.Fatalf("b got %+v, want a's message resent", pkt)
			}
			// The next message is the next one b gets, so the resent one arrived once.
			if err := a.Send(id, ChannelReliableOrdered, []byte("after")); err != nil {
				t.Fatalf("send: %v", err)
			}
			if pkt := recvRelay(t, b); string(pkt.Payload) != "after" || pkt.Seq != 2 {
				t.Fatalf("b got %+v, want a's next message", pkt)
			}
			if got := a.Peers(); !slices.Equal(got, []PeerID{id}) {
				t.Fatalf("a.Peers = %v, want b kept through its reconnect", got)
			}
		})
	}
}

// TestRelayRestartStartsANewSession restarts the relay on the same address. The clients reconnect as new peers and get their old IDs back, and must still take the new relay's membership and number their messages from 1.
func TestRelayRestartStartsANewSession(t *testing.T) {
	for _, proto := range []string{"websocket", "udp"} {
		t.Run(proto, func(t *testing.T) {
			auth := Auth{GracePeriod: time.Minute}
			old, err := NewRelay(RelayConfig{Join: relayJoin})
			if err != nil {
				t.Fatalf("NewRelay: %v", err)
			}
			srv, addr := listenRelay(t, proto, "127.0.0.1:0", old.Auth(auth))
			listenAddr := srv.(interface{ Addr() stdnet.Addr }).Addr().String()
			stop := serveRelay(old, srv)
			var clients []*relayClient
			defer func() {
				for _, c := range clients {
					_ = c.Close()
				}
			}()
			for _, token := range []string{"room/a", "room/b", "room/c"} {
				c, err := dialRelay(proto, addr, token, nil)
				if err != nil {
					t.Fatalf("dial %q: %v", token, err)
				}
				clients = append(clients, c)
			}
			a, b, c := clients[0], clients[1], clients[2]
			awaitPeers(t, a, b.LocalID(), c.LocalID())
			awaitPeers(t, b, a.LocalID(), c.LocalID())
			if err := a.Send(b.LocalID(), ChannelReliableOrdered, []byte("before")); err != nil {
				t.Fatalf("send: %v", err)
			}
			if pkt := recvRelay(t, b); string(pkt.Payload) != "before" {
				t.Fatalf("b got %+v, want a's message", pkt)
			}

			if err := stop(); !errors.Is(err, context.Canceled) {
				t.Fatalf("Serve returned %v", err)
			}
			_ = srv.Close()
			relay, err := NewRelay(RelayConfig{Join: relayJoin})
			if err != nil {
				t.Fatalf("NewRelay: %v", err)
			}
			srv2, _ := listenRelay(t, proto, listenAddr, relay.Auth(auth))
			stop2 := serveRelay(relay, srv2)
			defer func() {
				_ = stop2()
				_ = srv2.Close()
			}()

			// a and b come back, c does not.
			for _, rc := range []*relayClient{a, b} {
				id := rc.LocalID()
				inner := rc.inner.(interface {
					Reconnect() error
					Resumed() bool
				})
				if err := inner.Reconnect(); err != nil || inner.Resumed() || rc.LocalID() != id {
					t.Fatalf("reconnect = %v, resumed %v, id %d; want a new session as %d", err, inner.Resumed(), rc.LocalID(), id)
				}
			}
			awaitPeers(t, a, b.LocalID())
			awaitPeers(t, b, a.LocalID())
			if err := a.Send(b.LocalID(), ChannelReliableOrdered, []byte("after")); err != nil {
				t.Fatalf("send: %v", err)
			}
			if pkt := recvRelay(t, b); pkt.From != a.LocalID() || string(pkt.Payload) != "after" || pkt.Seq != 1 {
				t.Fatalf("b got %+v, want the new session's first message", pkt)
			}
		})
	}
}

// TestRelayServesAgainOnTheSameServer stops Serve and starts it again on the same server: the connected clients keep their session.
func TestRelayServesAgainOnTheSameServer(t *testing.T) {
	relay, err := NewRelay(RelayConfig{Join: relayJoin})
	if err != nil {
		t.Fatalf("NewRelay: %v", err)
	}
	srv, addr := listenRelay(t, "udp", "127.0.0.1:0", relay.Auth(Auth{}))
	defer func() { _ = srv.Close() }()
	stop := serveRelay(relay, srv)
	var clients []*relayClient
	defer func() {
		for _, c := range clients {
			_ = c.Close()
		}
	}()
	for _, token := range []string{"s/a", "s/b"} {
		c, err := dialRelay("udp", addr, token, nil)
		if err != nil {
			t.Fatalf("dial %q: %v", token, err)
		}
		clients = append(clients, c)
	}
	a, b := clients[0], clients[1]
	awaitPeers(t, a, b.LocalID())
	awaitPeers(t, b, a.LocalID())
	if err := stop(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Serve returned %v", err)
	}

	other, _ := listenRelay(t, "udp", "127.0.0.1:0", relay.Auth(Auth{}))
	defer func() { _ = other.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := relay.Serve(ctx, other); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("Serve on another server = %v, want a refusal", err)
	}

	stop = serveRelay(relay, srv)
	defer func() { _ = stop() }()
	if err := a.Send(b.LocalID(), ChannelReliableOrdered, []byte("again")); err != nil {
		t.Fatalf("send: %v", err)
	}
	if pkt := recvRelay(t, b); pkt.From != a.LocalID() || string(pkt.Payload) != "again" {
		t.Fatalf("b got %+v, want a's message through the second Serve", pkt)
	}
	if st := relay.Stats(); st.Sessions != 1 || st.Peers != 2 {
		t.Fatalf("stats = %+v, want the session kept", st)
	}
}

// TestRelayEpochCountsMembershipChanges checks that a session's epoch moves only when its members change, not on the periodic resend, and that clients follow it across the wrap.
func TestRelayEpochCountsMembershipChanges(t *testing.T) {
	relay, dial := startRelay(t, "udp", RelayConfig{}, Auth{})
	relay.mu.Lock()
	relay.sessions["e"] = &relaySession{epoch: math.MaxUint32 - 1}
	relay.mu.Unlock()
	epoch := func() uint32 {
		relay.mu.Lock()
		defer relay.mu.Unlock()
		return relay.sessions["e"].epoch
	}
	a := mustDialRelay(t, dial, "e/a")
	b := mustDialRelay(t, dial, "e/b") // the epoch wraps to 0
	awaitPeers(t, a, b.LocalID())
	awaitPeers(t, b, a.LocalID())
	if got := epoch(); got != 0 {
		t.Fatalf("epoch after two joins = %d, want 0", got)
	}
	for range 3 {
		relay.markAllDirty()
		relay.flushMembers()
	}
	if got := epoch(); got != 0 {
		t.Fatalf("epoch after resending = %d, want still 0", got)
	}
	c := mustDialRelay(t, dial, "e/c")
	awaitPeers(t, a, b.LocalID(), c.LocalID())
	_ = c.Close()
	awaitPeers(t, a, b.LocalID())
	awaitPeers(t, b, a.LocalID())
	if got := epoch(); got != 2 {
		t.Fatalf("epoch after a join and a leave = %d, want 2", got)
	}
}

func TestRelayServeOnce(t *testing.T) {
	relay, dial := startRelay(t, "udp", RelayConfig{}, Auth{})
	// Two members learning of each other proves the first Serve is running.
	a := mustDialRelay(t, dial, "s/a")
	b := mustDialRelay(t, dial, "s/b")
	awaitPeers(t, a, b.LocalID())
	srv, err := ListenUDPServer("127.0.0.1:0", 1, relay.Auth(Auth{}))
	if err != nil {
		t.Fatalf("ListenUDPServer: %v", err)
	}
	defer func() { _ = srv.Close() }()
	if err := relay.Serve(context.Background(), srv); err == nil {
		t.Fatal("a second concurrent Serve succeeded")
	}
	if _, err := NewRelay(RelayConfig{}); err == nil {
		t.Fatal("NewRelay without Join succeeded")
	}
}

// BenchmarkRelayRoundTrip measures the round trip of a small message on this machine, directly and through a local relay, so the latency the relay adds is the difference. "direct" is a client and its server (UDPClient and UDPServer, or WebSocketTransport and WebSocketServer), one hop each way; "relay" is two RelayTransports through a Relay on the same protocol, two hops each way, sending reliably as rollback does.
func BenchmarkRelayRoundTrip(b *testing.B) {
	payload := make([]byte, 48)
	b.Run("udp/direct", func(b *testing.B) {
		srv, err := ListenUDPServer("127.0.0.1:0", 1, Auth{})
		if err != nil {
			b.Fatal(err)
		}
		defer func() { _ = srv.Close() }()
		cli, err := DialUDP(srv.Addr().String(), nil)
		if err != nil {
			b.Fatal(err)
		}
		defer func() { _ = cli.Close() }()
		pingPong(b, cli, srv, 1, cli.LocalID(), ChannelUnreliable, payload, cli.changed, srv.changed)
	})
	b.Run("websocket/direct", func(b *testing.B) {
		srv, err := ListenWebSocket("127.0.0.1:0", 1, Auth{})
		if err != nil {
			b.Fatal(err)
		}
		defer func() { _ = srv.Close() }()
		cli, err := DialWebSocket("ws://"+srv.Addr().String()+"/", nil)
		if err != nil {
			b.Fatal(err)
		}
		defer func() { _ = cli.Close() }()
		pingPong(b, cli, srv, 1, cli.LocalID(), ChannelReliableOrdered, payload, cli.changed, srv.changed)
	})
	for _, proto := range []string{"udp", "websocket"} {
		b.Run(proto+"/relay", func(b *testing.B) {
			_, dial := startRelay(b, proto, RelayConfig{}, Auth{})
			x := mustDialRelay(b, dial, "bench/x")
			y := mustDialRelay(b, dial, "bench/y")
			awaitPeers(b, x, y.LocalID())
			awaitPeers(b, y, x.LocalID())
			pingPong(b, x, y, y.LocalID(), x.LocalID(), ChannelReliableOrdered, payload, x.changed, y.changed)
		})
	}
}

// pingPong bounces payload between x and y b.N times: x sends to y (yID) and y, on its own goroutine, answers x (xID). Each side blocks on its transport's change signal, so the measurement is the network path rather than a polling interval.
func pingPong(b *testing.B, x, y Transport, yID, xID PeerID, ch Channel, payload []byte, xChanged, yChanged <-chan struct{}) {
	stop := make(chan struct{})
	echoed := make(chan struct{})
	go func() {
		defer close(echoed)
		for {
			for {
				if _, ok := y.Recv(); !ok {
					break
				}
				_ = y.Send(xID, ch, payload)
			}
			select {
			case <-stop:
				return
			case <-yChanged:
			}
		}
	}()
	defer func() {
		close(stop)
		<-echoed
	}()
	timeout := time.NewTimer(time.Hour)
	defer timeout.Stop()
	b.ResetTimer()
	for range b.N {
		if err := x.Send(yID, ch, payload); err != nil {
			b.Fatalf("send: %v", err)
		}
		timeout.Reset(5 * time.Second)
		for {
			if _, ok := x.Recv(); ok {
				break
			}
			select {
			case <-xChanged:
			case <-timeout.C:
				b.Fatal("no answer within 5 seconds")
			}
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N), "µs/rtt")
}
