//go:build !js

package netplay

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	stdnet "net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// selfSignedWTCert generates a short-lived (7-day) ECDSA P-256 certificate compliant with browser WebTransport serverCertificateHashes rules, returning the tls.Config, leaf DER bytes, PEM cert/key, and SHA-256 hash.
func selfSignedWTCert(t *testing.T) (*tls.Config, []byte, []byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(7 * 24 * time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []stdnet.IP{stdnet.IPv4(127, 0, 0, 1)},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	sum := sha256.Sum256(der)
	cfg := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	return cfg, sum[:], certPEM, keyPEM
}

func listenWT(t *testing.T, auth Auth, allowedOrigins []string) (*WebTransportServer, string, WebTransportOptions) {
	t.Helper()
	cfg, hash, _, _ := selfSignedWTCert(t)
	srv, err := ListenWebTransport("127.0.0.1:0", 1, auth, cfg, allowedOrigins)
	if err != nil {
		t.Fatalf("ListenWebTransport: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv, "https://" + srv.Addr().String() + "/game", WebTransportOptions{ServerCertificateHashes: [][]byte{hash}}
}

func TestWebTransportStreamDecoder(t *testing.T) {
	f1, err := encodeStreamFrame([]byte("alpha"))
	if err != nil {
		t.Fatal(err)
	}
	f2, err := encodeStreamFrame(nil)
	if err != nil {
		t.Fatal(err)
	}
	f3, err := encodeStreamFrame([]byte("beta-payload"))
	if err != nil {
		t.Fatal(err)
	}

	stream := append(append(f1, f2...), f3...)
	var dec wtStreamDecoder
	var got [][]byte
	// Feed 1 byte at a time to exercise arbitrary chunk boundaries.
	for _, b := range stream {
		dec.write([]byte{b})
		for {
			frame, ok, err := dec.next(wtMaxStreamMessageSize)
			if err != nil {
				t.Fatalf("next: %v", err)
			}
			if !ok {
				break
			}
			got = append(got, frame)
		}
	}
	if len(got) != 3 || string(got[0]) != "alpha" || len(got[1]) != 0 || string(got[2]) != "beta-payload" {
		t.Fatalf("decoded frames = %q, want [alpha \"\" beta-payload]", got)
	}

	// Oversized length header is rejected immediately without waiting for payload bytes.
	dec.write([]byte{0x00, 0x00, 0x10, 0x00})
	if _, _, err := dec.next(1024); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("oversized frame err = %v, want ErrPayloadTooLarge", err)
	}
}

func TestWebTransportHandshakeBothChannelsAndBroadcast(t *testing.T) {
	if !WebTransportSupported() {
		t.Fatal("WebTransportSupported() = false on native")
	}
	srv, rawURL, opts := listenWT(t, Auth{}, nil)

	cliA, err := DialWebTransport(rawURL, nil, opts)
	if err != nil {
		t.Fatalf("DialWebTransport A: %v", err)
	}
	defer func() { _ = cliA.Close() }()

	cliB, err := DialWebTransport(rawURL, nil, opts)
	if err != nil {
		t.Fatalf("DialWebTransport B: %v", err)
	}
	defer func() { _ = cliB.Close() }()

	evA := nextEvent(t, srv, srv.changed)
	evB := nextEvent(t, srv, srv.changed)
	if evA != (PeerEvent{Kind: PeerJoined, Peer: cliA.LocalID()}) || evB != (PeerEvent{Kind: PeerJoined, Peer: cliB.LocalID()}) {
		t.Fatalf("events = %+v, %+v", evA, evB)
	}
	if cliA.LocalID() != 2 || cliB.LocalID() != 3 {
		t.Fatalf("assigned IDs = %d, %d; want 2, 3", cliA.LocalID(), cliB.LocalID())
	}
	if cliA.State() != ConnOpen || cliA.Err() != nil {
		t.Fatalf("cliA state=%v err=%v", cliA.State(), cliA.Err())
	}

	// Reliable channel (including >64 KiB payload) and unreliable datagrams in both directions.
	cases := []struct {
		ch      Channel
		payload []byte
	}{
		{ChannelReliableOrdered, []byte("hello-wt-reliable")},
		{ChannelUnreliable, []byte("hello-wt-datagram")},
		{ChannelReliableOrdered, bytes.Repeat([]byte{0xCD}, 70000)},
		{ChannelUnreliable, bytes.Repeat([]byte{0xAB}, 512)},
	}
	for i, tc := range cases {
		if err := cliA.Send(1, tc.ch, tc.payload); err != nil {
			t.Fatalf("cliA.Send(%d, %v): %v", i, tc.ch, err)
		}
		pkt := recvOn(t, srv, srv.changed)
		if pkt.From != cliA.LocalID() || pkt.Channel != tc.ch || !bytes.Equal(pkt.Payload, tc.payload) {
			t.Fatalf("client->server case %d mismatch: from=%d ch=%v len=%d want=%d", i, pkt.From, pkt.Channel, len(pkt.Payload), len(tc.payload))
		}

		if err := srv.Send(cliA.LocalID(), tc.ch, tc.payload); err != nil {
			t.Fatalf("srv.Send(%d, %v): %v", i, tc.ch, err)
		}
		reply := recvOn(t, cliA, cliA.changed)
		if reply.From != 1 || reply.Channel != tc.ch || !bytes.Equal(reply.Payload, tc.payload) {
			t.Fatalf("server->client case %d mismatch: from=%d ch=%v len=%d want=%d", i, reply.From, reply.Channel, len(reply.Payload), len(tc.payload))
		}
	}

	// Broadcast on both channels reaches both clients.
	for _, ch := range []Channel{ChannelReliableOrdered, ChannelUnreliable} {
		msg := []byte("broadcast-" + ch.String())
		if err := srv.Send(PeerBroadcast, ch, msg); err != nil {
			t.Fatalf("srv broadcast %v: %v", ch, err)
		}
		pA := recvOn(t, cliA, cliA.changed)
		pB := recvOn(t, cliB, cliB.changed)
		if pA.Channel != ch || !bytes.Equal(pA.Payload, msg) || pB.Channel != ch || !bytes.Equal(pB.Payload, msg) {
			t.Fatalf("broadcast %v mismatch: A=%+v B=%+v", ch, pA, pB)
		}
	}

	// Oversized unreliable payload returns ErrPayloadTooLarge.
	if err := cliA.Send(1, ChannelUnreliable, make([]byte, 8192)); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("oversized datagram err = %v, want ErrPayloadTooLarge", err)
	}

	// Wrong certificate hash is rejected during TLS verification.
	badHash := make([]byte, sha256.Size)
	if _, err := DialWebTransport(rawURL, nil, WebTransportOptions{ServerCertificateHashes: [][]byte{badHash}}); err == nil {
		t.Fatal("wrong certificate hash succeeded, want error")
	}
	// Invalid certificate hash length is rejected before dialing.
	if _, err := DialWebTransport(rawURL, nil, WebTransportOptions{ServerCertificateHashes: [][]byte{{1, 2, 3}}}); err == nil {
		t.Fatal("short certificate hash accepted, want error")
	}
}

func TestWebTransportRejectionAndMaxPeers(t *testing.T) {
	clock := newFakeClock()
	var seen []PeerID
	auth := tokenAuth(clock, 0, &seen)
	auth.MaxPeers = 1
	srv, rawURL, opts := listenWT(t, auth, nil)

	// Invalid token is rejected with *RejectError matching ErrRejected.
	if _, err := DialWebTransport(rawURL, []byte("forged"), opts); !errors.Is(err, ErrRejected) {
		t.Fatalf("bad token err = %v, want ErrRejected", err)
	} else {
		var rej *RejectError
		if !errors.As(err, &rej) || rej.Reason != "bad token" {
			t.Fatalf("reject = %+v, want reason %q", rej, "bad token")
		}
	}
	if len(srv.Peers()) != 0 {
		t.Fatalf("rejected peer in Peers: %v", srv.Peers())
	}
	expectNoEvent(t, srv)

	// Oversized token returns ErrTokenTooLarge before dialing.
	if _, err := DialWebTransport(rawURL, make([]byte, MaxTokenSize+1), opts); !errors.Is(err, ErrTokenTooLarge) {
		t.Fatalf("oversized token err = %v, want ErrTokenTooLarge", err)
	}

	// MaxPeers = 1 admits first client and rejects second with "server full".
	good, err := DialWebTransport(rawURL, []byte("good"), opts)
	if err != nil {
		t.Fatalf("DialWebTransport good: %v", err)
	}
	defer func() { _ = good.Close() }()
	_ = nextEvent(t, srv, srv.changed)

	if _, err := DialWebTransport(rawURL, []byte("good"), opts); !errors.Is(err, ErrRejected) {
		t.Fatalf("second client err = %v, want ErrRejected", err)
	} else {
		var rej *RejectError
		if !errors.As(err, &rej) || rej.Reason != reasonServerFull {
			t.Fatalf("second client reject = %+v, want %q", rej, reasonServerFull)
		}
	}
}

func TestWebTransportResumeAndGracePeriod(t *testing.T) {
	clock := newFakeClock()
	srv, rawURL, opts := listenWT(t, tokenAuth(clock, 10*time.Second, nil), nil)

	cli, err := DialWebTransport(rawURL, []byte("good"), opts)
	if err != nil {
		t.Fatalf("DialWebTransport: %v", err)
	}
	defer func() { _ = cli.Close() }()
	id := cli.LocalID()
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerJoined, Peer: id}) {
		t.Fatalf("event = %+v, want PeerJoined", ev)
	}

	// Reconnect while open keeps the same ID (the old session's close may arrive before or after the resume hello).
	if err := cli.Reconnect(); err != nil {
		t.Fatalf("Reconnect while open: %v", err)
	}
	if !cli.Resumed() || cli.LocalID() != id {
		t.Fatalf("after open reconnect: id=%d resumed=%v, want id=%d resumed=true", cli.LocalID(), cli.Resumed(), id)
	}
	// Drain any PeerDropped + PeerResumed pair if the old session closed before the new hello arrived.
	for {
		ev, ok := srv.NextEvent()
		if !ok {
			break
		}
		if ev != (PeerEvent{Kind: PeerDropped, Peer: id}) && ev != (PeerEvent{Kind: PeerResumed, Peer: id}) {
			t.Fatalf("unexpected event after open reconnect: %+v", ev)
		}
	}
	if err := cli.Send(1, ChannelUnreliable, []byte("after-open-reconnect")); err != nil {
		t.Fatalf("Send after reconnect: %v", err)
	}
	if p := recvOn(t, srv, srv.changed); p.From != id || string(p.Payload) != "after-open-reconnect" {
		t.Fatalf("pkt = %+v", p)
	}

	// Simulate a drop (close session with wtCloseReconnect): server reports PeerDropped and holds session.
	cli.mu.Lock()
	sess := cli.sess
	cli.mu.Unlock()
	_ = sess.CloseWithError(wtCloseReconnect, "network drop")

	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerDropped, Peer: id}) {
		t.Fatalf("event = %+v, want PeerDropped", ev)
	}
	if len(srv.Peers()) != 0 {
		t.Fatalf("dropped peer still in Peers: %v", srv.Peers())
	}
	if err := srv.Send(id, ChannelReliableOrdered, []byte("away")); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("Send to dropped peer = %v, want ErrDisconnected", err)
	}

	// Reconnect within grace period resumes with the same ID.
	clock.Advance(5 * time.Second)
	if err := cli.Reconnect(); err != nil {
		t.Fatalf("Reconnect within grace: %v", err)
	}
	if !cli.Resumed() || cli.LocalID() != id {
		t.Fatalf("resumed=%v id=%d, want true %d", cli.Resumed(), cli.LocalID(), id)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerResumed, Peer: id}) {
		t.Fatalf("event = %+v, want PeerResumed", ev)
	}

	// Drop again and advance past grace period: server reports PeerLeft.
	cli.mu.Lock()
	sess = cli.sess
	cli.mu.Unlock()
	_ = sess.CloseWithError(wtCloseReconnect, "drop 2")
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerDropped, Peer: id}) {
		t.Fatalf("event = %+v, want PeerDropped", ev)
	}
	clock.Advance(15 * time.Second)
	if ev, ok := srv.NextEvent(); !ok || ev != (PeerEvent{Kind: PeerLeft, Peer: id}) {
		t.Fatalf("after grace: ev=%+v ok=%v, want PeerLeft", ev, ok)
	}
	if err := srv.Send(id, ChannelReliableOrdered, []byte("gone")); !errors.Is(err, ErrUnknownPeer) {
		t.Fatalf("Send after leave = %v, want ErrUnknownPeer", err)
	}

	// Reconnect after grace period joins as a new peer.
	if err := cli.Reconnect(); err != nil {
		t.Fatalf("Reconnect after grace: %v", err)
	}
	if cli.Resumed() || cli.LocalID() == id {
		t.Fatalf("after grace reconnect: id=%d resumed=%v, want new id and resumed=false", cli.LocalID(), cli.Resumed())
	}
	newID := cli.LocalID()
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerJoined, Peer: newID}) {
		t.Fatalf("event = %+v, want PeerJoined(%d)", ev, newID)
	}

	// Close is a goodbye: server reports PeerLeft immediately without PeerDropped.
	if err := cli.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerLeft, Peer: newID}) {
		t.Fatalf("after Close: ev=%+v, want PeerLeft(%d)", ev, newID)
	}
	if err := cli.Reconnect(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Reconnect after Close = %v, want ErrClosed", err)
	}
}

func TestWebTransportOriginCheckAndFiles(t *testing.T) {
	_, rawURL, opts := listenWT(t, Auth{}, []string{"https://allowed.example/"})

	// Allowed Origin succeeds.
	cli, err := dialWebTransport(rawURL, nil, opts, wtDialConfig{
		headers: http.Header{"Origin": {"https://allowed.example"}},
	})
	if err != nil {
		t.Fatalf("allowed Origin dial failed: %v", err)
	}
	_ = cli.Close()

	// Empty Origin (native client) succeeds.
	cliNative, err := DialWebTransport(rawURL, nil, opts)
	if err != nil {
		t.Fatalf("empty Origin dial failed: %v", err)
	}
	_ = cliNative.Close()

	// Foreign Origin is rejected with 403.
	if _, err := dialWebTransport(rawURL, nil, opts, wtDialConfig{
		headers: http.Header{"Origin": {"https://evil.example"}},
	}); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("foreign Origin err = %v, want 403", err)
	}

	// Malformed allow-list entry fails at start-up.
	cfg, hash, certPEM, keyPEM := selfSignedWTCert(t)
	if _, err := ListenWebTransport("127.0.0.1:0", 1, Auth{}, cfg, []string{"https://allowed.example/play"}); err == nil {
		t.Fatal("path in allowedOrigins accepted, want error")
	}

	// ListenWebTransportFiles loads PEM files and serves clients.
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	fsrv, err := ListenWebTransportFiles("127.0.0.1:0", 1, Auth{}, certFile, keyFile, []string{"*"})
	if err != nil {
		t.Fatalf("ListenWebTransportFiles: %v", err)
	}
	defer func() { _ = fsrv.Close() }()
	fcli, err := DialWebTransport("https://"+fsrv.Addr().String()+"/game", nil, WebTransportOptions{
		ServerCertificateHashes: [][]byte{hash},
	})
	if err != nil {
		t.Fatalf("dial file server: %v", err)
	}
	_ = fcli.Close()
}

func TestWebTransportInboxLimits(t *testing.T) {
	srv, rawURL, opts := listenWT(t, Auth{}, nil)
	srv.SetInboxLimits(InboxLimits{Unreliable: 4, Reliable: 5})

	flood, err := DialWebTransport(rawURL, nil, opts)
	if err != nil {
		t.Fatalf("dial flood: %v", err)
	}
	defer func() { _ = flood.Close() }()
	quiet, err := DialWebTransport(rawURL, nil, opts)
	if err != nil {
		t.Fatalf("dial quiet: %v", err)
	}
	defer func() { _ = quiet.Close() }()
	_ = nextEvent(t, srv, srv.changed)
	_ = nextEvent(t, srv, srv.changed)

	// Client inbox limits: cap unreliable at 4, warn on reliable backlog past 10.
	quiet.SetInboxLimits(InboxLimits{Unreliable: 4, ReliableWarning: 10})
	const cliUnrel, cliRel = 20, 20
	for i := range cliUnrel {
		if err := srv.Send(quiet.LocalID(), ChannelUnreliable, []byte{byte(i)}); err != nil {
			t.Fatalf("srv unreliable: %v", err)
		}
	}
	for i := range cliRel {
		if err := srv.Send(quiet.LocalID(), ChannelReliableOrdered, []byte{byte(i)}); err != nil {
			t.Fatalf("srv reliable: %v", err)
		}
	}
	awaitChange(t, quiet.changed, "client reliable packets", func() bool {
		return quiet.InboxStats().Reliable == cliRel
	})
	if st := quiet.InboxStats(); st.Unreliable > 4 || st.Reliable != cliRel || st.ReliableWarnings != 1 || !st.ReliableBacklog {
		t.Fatalf("client InboxStats = %+v", st)
	}
	for {
		if _, ok := quiet.Recv(); !ok {
			break
		}
	}
	if st := quiet.InboxStats(); st.ReliableBacklog || st.ReliableWarnings != 1 {
		t.Fatalf("client InboxStats after drain = %+v", st)
	}

	// Server per-client unreliable cap: quiet client's 2 datagrams stay intact while flood overflows its own cap.
	for i := range 2 {
		if err := quiet.Send(srv.LocalID(), ChannelUnreliable, []byte{byte(i)}); err != nil {
			t.Fatalf("quiet unreliable: %v", err)
		}
	}
	awaitChange(t, srv.changed, "quiet client datagrams", func() bool {
		return srv.InboxStats().Unreliable == 2
	})
	for i := range 30 {
		if err := flood.Send(srv.LocalID(), ChannelUnreliable, []byte{byte(i)}); err != nil {
			t.Fatalf("flood unreliable: %v", err)
		}
	}
	awaitChange(t, srv.changed, "flood datagrams", func() bool {
		st := srv.InboxStats()
		return st.Unreliable == 6 && st.UnreliableDropped > 0
	})

	// Server per-client reliable cap: flooding client loses its session with PeerLeft, quiet stays connected.
	for i := range 10 {
		if err := flood.Send(srv.LocalID(), ChannelReliableOrdered, []byte{byte(i)}); err != nil {
			break
		}
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerLeft, Peer: flood.LocalID()}) {
		t.Fatalf("event = %+v, want PeerLeft(%d)", ev, flood.LocalID())
	}
	if st := srv.InboxStats(); st.Reliable != 5 || st.ReliableRefused != 1 {
		t.Fatalf("server InboxStats = %+v, want 5 reliable and 1 refused", st)
	}
	if got := srv.Peers(); len(got) != 1 || got[0] != quiet.LocalID() {
		t.Fatalf("Peers = %v, want only quiet client %d", got, quiet.LocalID())
	}
}

func TestWebTransportRelayAndRollback(t *testing.T) {
	relay, err := NewRelay(RelayConfig{Join: relayJoin})
	if err != nil {
		t.Fatalf("NewRelay: %v", err)
	}
	srv, rawURL, opts := listenWT(t, relay.Auth(Auth{}), nil)
	stop := serveRelay(relay, srv)
	t.Cleanup(func() {
		if err := stop(); !errors.Is(err, context.Canceled) {
			t.Errorf("Serve returned %v, want context.Canceled", err)
		}
	})

	// Bad token rejected by relay Join.
	if _, err := DialWebTransport(rawURL, []byte("bad"), opts); !errors.Is(err, ErrRejected) {
		t.Fatalf("bad relay token err = %v, want ErrRejected", err)
	}

	dial := func(token string) *relayClient {
		t.Helper()
		wt, err := DialWebTransport(rawURL, []byte(token), opts)
		if err != nil {
			t.Fatalf("DialWebTransport %q: %v", token, err)
		}
		t.Cleanup(func() { _ = wt.Close() })
		return &relayClient{
			RelayTransport: NewRelayTransport(wt, RelayTransportConfig{}),
			inner:          wt,
			changed:        wt.changed,
		}
	}

	a := dial("match/alice")
	b := dial("match/bob")
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
		awaitEither(t, a.changed, b.changed, "both peers to confirm tick", func() bool {
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
	if len(sa.Desyncs()) != 0 || len(sb.Desyncs()) != 0 {
		t.Fatalf("desyncs: a=%v b=%v", sa.Desyncs(), sb.Desyncs())
	}
	if st := relay.Stats(); st.Sessions != 1 || st.Peers != 2 || st.Forwarded == 0 {
		t.Fatalf("relay stats = %+v", st)
	}
}

// shapingPacketConn wraps a real UDP PacketConn and drops the next N outgoing UDP packets whose size is at least minDropSize, so tests can trigger deterministic QUIC packet loss on a chosen large message without dropping small ACKs or subsequent small datagrams. A drop armed with hold also holds back every later packet of at least minDropSize, which carries QUIC's retransmission of what was dropped, until release writes them, so a test can check what reaches the peer while the lost data is still missing.
type shapingPacketConn struct {
	stdnet.PacketConn
	dropped atomic.Int32

	mu            sync.Mutex
	dropRemaining int32
	minDropSize   int
	holdAfterDrop bool
	holding       bool
	held          []heldPacket
}

// heldPacket is a UDP packet shapingPacketConn holds back until release.
type heldPacket struct {
	data []byte
	addr stdnet.Addr
}

func (s *shapingPacketConn) armDrop(count int32, minSize int, hold bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropRemaining, s.minDropSize, s.holdAfterDrop = count, minSize, hold
}

func (s *shapingPacketConn) WriteTo(p []byte, addr stdnet.Addr) (int, error) {
	s.mu.Lock()
	if s.minDropSize > 0 && len(p) >= s.minDropSize {
		if s.dropRemaining > 0 {
			s.dropRemaining--
			s.holding = s.holdAfterDrop
			s.mu.Unlock()
			s.dropped.Add(1)
			return len(p), nil // silently drop this UDP packet
		}
		if s.holding {
			s.held = append(s.held, heldPacket{data: slices.Clone(p), addr: addr})
			s.mu.Unlock()
			return len(p), nil
		}
	}
	s.mu.Unlock()
	return s.PacketConn.WriteTo(p, addr)
}

// release stops holding packets back and writes the held ones in the order QUIC sent them.
func (s *shapingPacketConn) release() error {
	s.mu.Lock()
	held := s.held
	s.held, s.holding = nil, false
	s.mu.Unlock()
	for _, h := range held {
		if _, err := s.PacketConn.WriteTo(h.data, h.addr); err != nil {
			return err
		}
	}
	return nil
}

// TestWebTransportUnreliableNotBlockedByLostPacket verifies that ChannelUnreliable datagrams bypass a lost QUIC packet without TCP-like head-of-line blocking:
//  1. When a reliable stream packet is dropped on the wire, an unreliable datagram sent after it arrives while the reliable packet is still missing, BEFORE its retransmission.
//  2. When an unreliable datagram is dropped on the wire, subsequent unreliable datagrams arrive promptly and the lost datagram is not retransmitted.
func TestWebTransportUnreliableNotBlockedByLostPacket(t *testing.T) {
	rawPC, err := stdnet.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	shaper := &shapingPacketConn{PacketConn: rawPC}
	cfg, hash, _, _ := selfSignedWTCert(t)
	srv, err := listenWebTransportConn(shaper, 1, Auth{}, cfg, nil)
	if err != nil {
		_ = rawPC.Close()
		t.Fatalf("listenWebTransportConn: %v", err)
	}
	defer func() { _ = srv.Close() }()

	rawURL := "https://" + srv.Addr().String() + "/game"
	cli, err := DialWebTransport(rawURL, nil, WebTransportOptions{ServerCertificateHashes: [][]byte{hash}})
	if err != nil {
		t.Fatalf("DialWebTransport: %v", err)
	}
	defer func() { _ = cli.Close() }()
	_ = nextEvent(t, srv, srv.changed)

	// Subtest 1: drop the first transmission of a 600-byte reliable packet and hold back its retransmissions; wait until the shaper confirms it was dropped so QUIC does not coalesce the following datagram into the same UDP packet. An unreliable datagram sent right after must reach the client while the reliable packet is still held back.
	//
	// Holding the retransmissions back is what makes the order deterministic (#1916). On loopback the client's acknowledgement of the datagram's packet has QUIC declare the reliable packet lost and resend it within a fraction of a millisecond, and the client reads datagrams and its stream on goroutines of their own, so a retransmission let through can be delivered first even though QUIC received the datagram first. A datagram that waited behind the stream would not arrive at all until release.
	relPayload := append([]byte("REL-LOST:"), bytes.Repeat([]byte{'R'}, 600)...)
	shaper.armDrop(1, 500, true)
	if err := srv.Send(cli.LocalID(), ChannelReliableOrdered, relPayload); err != nil {
		t.Fatalf("Send reliable: %v", err)
	}
	dropDeadline := time.Now().Add(2 * time.Second)
	for shaper.dropped.Load() < 1 && time.Now().Before(dropDeadline) {
		time.Sleep(time.Millisecond)
	}
	if shaper.dropped.Load() < 1 {
		t.Fatalf("shaper dropped %d packets, expected at least 1", shaper.dropped.Load())
	}

	// Send one small unreliable datagram right behind the dropped reliable packet (sending just one avoids triggering QUIC's 3-packet fast-retransmit threshold before the client processes the datagram).
	firstUnrel := []byte("UNREL:0")
	if err := srv.Send(cli.LocalID(), ChannelUnreliable, firstUnrel); err != nil {
		t.Fatalf("Send unreliable 0: %v", err)
	}

	// Wait for the unreliable datagram while the reliable packet is held back; then let the retransmission through, send a few more datagrams and wait for the reliable packet.
	var first Packet
	awaitChange(t, cli.changed, "the unreliable datagram while the lost reliable packet is held back", func() bool {
		var ok bool
		first, ok = cli.Recv()
		return ok
	})
	if first.Channel != ChannelUnreliable || !bytes.Equal(first.Payload, firstUnrel) {
		t.Fatalf("first packet after loss = {ch:%v payload:%q}, want unreliable %q before retransmitted reliable packet", first.Channel, first.Payload, firstUnrel)
	}
	if err := shaper.release(); err != nil {
		t.Fatalf("release held packets: %v", err)
	}
	for i := 1; i < 6; i++ {
		msg := []byte("UNREL:" + string(rune('0'+i)))
		if err := srv.Send(cli.LocalID(), ChannelUnreliable, msg); err != nil {
			t.Fatalf("Send unreliable %d: %v", i, err)
		}
	}

	gotRel := false
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for !gotRel {
		for {
			pkt, ok := cli.Recv()
			if !ok {
				break
			}
			if pkt.Channel == ChannelReliableOrdered && bytes.Equal(pkt.Payload, relPayload) {
				gotRel = true
			}
		}
		if gotRel {
			break
		}
		select {
		case <-cli.changed:
		case <-deadline.C:
			t.Fatalf("timed out waiting for retransmitted reliable packet (dropped=%d)", shaper.dropped.Load())
		}
	}

	// Subtest 2: drop a 600-byte unreliable datagram; wait until it is dropped, then send small unreliable datagrams that must arrive promptly while the dropped datagram is never retransmitted.
	droppedDgram := append([]byte("UNREL-DROP:"), bytes.Repeat([]byte{'U'}, 600)...)
	droppedBefore := shaper.dropped.Load()
	shaper.armDrop(1, 500, false)
	if err := srv.Send(cli.LocalID(), ChannelUnreliable, droppedDgram); err != nil {
		t.Fatalf("Send dropped datagram: %v", err)
	}
	dropDeadline = time.Now().Add(2 * time.Second)
	for shaper.dropped.Load() <= droppedBefore && time.Now().Before(dropDeadline) {
		time.Sleep(time.Millisecond)
	}
	if shaper.dropped.Load() <= droppedBefore {
		t.Fatal("expected shaper to drop the large unreliable datagram")
	}
	for i := range 5 {
		if err := srv.Send(cli.LocalID(), ChannelUnreliable, []byte("AFTER:"+string(rune('a'+i)))); err != nil {
			t.Fatalf("Send after-drop datagram %d: %v", i, err)
		}
	}
	if err := srv.Send(cli.LocalID(), ChannelReliableOrdered, []byte("SENTINEL")); err != nil {
		t.Fatalf("Send sentinel: %v", err)
	}

	afterCount := 0
	gotSentinel := false
	deadline.Reset(5 * time.Second)
	for !gotSentinel || afterCount == 0 {
		for {
			pkt, ok := cli.Recv()
			if !ok {
				break
			}
			if bytes.Equal(pkt.Payload, droppedDgram) {
				t.Fatal("dropped unreliable datagram was unexpectedly delivered")
			}
			if pkt.Channel == ChannelUnreliable && bytes.HasPrefix(pkt.Payload, []byte("AFTER:")) {
				afterCount++
			}
			if pkt.Channel == ChannelReliableOrdered && string(pkt.Payload) == "SENTINEL" {
				gotSentinel = true
			}
		}
		if gotSentinel && afterCount > 0 {
			break
		}
		select {
		case <-cli.changed:
		case <-deadline.C:
			t.Fatalf("timed out waiting for after-drop datagrams and sentinel (gotSentinel=%v, afterCount=%d)", gotSentinel, afterCount)
		}
	}
}

func TestWebTransportFallbackToWebSocket(t *testing.T) {
	wsSrv, wsURL := listenWS(t, Auth{})

	// Invalid/unreachable WebTransport URL falls back cleanly to WebSocket.
	tr, err := DialWebTransportWithFallback("http://invalid-scheme.example", wsURL, nil, WebTransportOptions{})
	if err != nil {
		t.Fatalf("DialWebTransportWithFallback: %v", err)
	}
	defer func() { _ = tr.Close() }()
	if _, ok := tr.(*WebSocketTransport); !ok {
		t.Fatalf("fallback returned %T, want *WebSocketTransport", tr)
	}
	if err := tr.Send(wsSrv.LocalID(), ChannelReliableOrdered, []byte("via-ws-fallback")); err != nil {
		t.Fatalf("fallback Send: %v", err)
	}
	if pkt := recvOn(t, wsSrv, wsSrv.changed); string(pkt.Payload) != "via-ws-fallback" {
		t.Fatalf("fallback pkt = %+v", pkt)
	}

	// Auth rejection on WebTransport must NOT fall back to WebSocket.
	clock := newFakeClock()
	_, wtURL, opts := listenWT(t, tokenAuth(clock, 0, nil), nil)
	if _, err := DialWebTransportWithFallback(wtURL, wsURL, []byte("forged"), opts); !errors.Is(err, ErrRejected) {
		t.Fatalf("rejected fallback err = %v, want ErrRejected", err)
	}
}
