//go:build !js

package netplay

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	stdnet "net"
	"sync"
	"testing"
	"time"
)

// udpSniffer proxies UDP datagrams between a client and a UDPServer while recording every datagram sent in each direction so tests can assert wire confidentiality and capture live packets for replay or reflection.
type udpSniffer struct {
	pc     stdnet.PacketConn
	server stdnet.Addr
	mu     sync.Mutex
	client stdnet.Addr
	c2s    [][]byte
	s2c    [][]byte
	closed bool
	done   chan struct{}
}

func startUDPSniffer(t *testing.T, serverAddr stdnet.Addr) *udpSniffer {
	t.Helper()
	pc, err := stdnet.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket sniffer: %v", err)
	}
	s := &udpSniffer{pc: pc, server: serverAddr, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		buf := make([]byte, 65535)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			cp := bytes.Clone(buf[:n])
			s.mu.Lock()
			if sameUDPAddr(from, s.server) {
				s.s2c = append(s.s2c, cp)
				dst := s.client
				s.mu.Unlock()
				if dst != nil {
					_, _ = pc.WriteTo(cp, dst)
				}
			} else {
				s.client = from
				s.c2s = append(s.c2s, cp)
				s.mu.Unlock()
				_, _ = pc.WriteTo(cp, s.server)
			}
		}
	}()
	t.Cleanup(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		_ = pc.Close()
		<-s.done
	})
	return s
}

func (s *udpSniffer) addr() string { return s.pc.LocalAddr().String() }

func (s *udpSniffer) captured() (c2s, s2c [][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.c2s...), append([][]byte(nil), s.s2c...)
}

func TestUDPSessionEncryptedRoundTripAndWireConfidentiality(t *testing.T) {
	staticKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	token := []byte("super-secret-matchmaking-ticket-token")
	srv := listenUDPServer(t, Auth{
		StaticKey: staticKey,
		Verify: func(_ PeerID, got []byte) error {
			if !bytes.Equal(got, token) {
				return errors.New("bad token")
			}
			return nil
		},
	})
	if !srv.PublicKey().Equal(staticKey.PublicKey()) {
		t.Fatal("UDPServer.PublicKey does not match Auth.StaticKey")
	}

	sniff := startUDPSniffer(t, srv.Addr())
	cli, err := DialUDP(sniff.addr(), srv.PublicKey(), token)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer func() { _ = cli.Close() }()
	_ = nextEvent(t, srv, srv.changed)

	cli.mu.Lock()
	resumeSecret := cli.resume.secret
	cli.mu.Unlock()

	payloads := [][]byte{
		{},
		[]byte("hidden-hand-of-cards-ace-king"),
		bytes.Repeat([]byte("fog-of-war-"), 512),
	}
	for i, want := range payloads {
		if err := cli.Send(srv.LocalID(), ChannelUnreliable, want); err != nil {
			t.Fatalf("cli.Send(%d): %v", i, err)
		}
		got := recvOn(t, srv, srv.changed)
		if got.From != cli.LocalID() || !bytes.Equal(got.Payload, want) {
			t.Fatalf("c2s payload %d mismatch: got len=%d want len=%d", i, len(got.Payload), len(want))
		}
		if err := srv.Send(cli.LocalID(), ChannelUnreliable, want); err != nil {
			t.Fatalf("srv.Send(%d): %v", i, err)
		}
		reply := recvOn(t, cli, cli.changed)
		if reply.From != srv.LocalID() || !bytes.Equal(reply.Payload, want) {
			t.Fatalf("s2c payload %d mismatch: got len=%d want len=%d", i, len(reply.Payload), len(want))
		}
	}

	c2s, s2c := sniff.captured()
	if len(c2s) < 4 || len(s2c) < 4 {
		t.Fatalf("expected handshake + data datagrams, got c2s=%d s2c=%d", len(c2s), len(s2c))
	}
	for _, d := range append(c2s, s2c...) {
		if bytes.Contains(d, token) {
			t.Fatal("wire datagram contained auth token in cleartext")
		}
		if bytes.Contains(d, resumeSecret[:]) {
			t.Fatal("wire datagram contained resume secret in cleartext")
		}
		if bytes.Contains(d, []byte("hidden-hand-of-cards")) {
			t.Fatal("wire datagram contained game payload in cleartext")
		}
		if bytes.Contains(d, []byte("fog-of-war-")) {
			t.Fatal("wire datagram contained large game payload in cleartext")
		}
	}
}

func TestUDPSessionRejectsTamperedHeaderPayloadAndTag(t *testing.T) {
	srv := listenUDPServer(t, Auth{})
	cli, err := DialUDP(srv.Addr().String(), srv.PublicKey(), nil)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer func() { _ = cli.Close() }()
	_ = nextEvent(t, srv, srv.changed)

	cli.mu.Lock()
	c2s := cli.sendCipher
	cliConn := cli.conn
	cliID := cli.localID
	cli.mu.Unlock()

	// Craft a valid client->server datagram and tamper with header (type, peer, seq, channel), ciphertext, and tag.
	base := c2s.seal(nil, udpDirClientToServer, udpData, cliID, 10, ChannelUnreliable, []byte("authentic-payload"))
	tamperOffsets := []struct {
		name string
		idx  int
	}{
		{"type", 0},
		{"peer", 4},
		{"seq", 8},
		{"channel", 9},
		{"payload", udpCleartextHeader + 3},
		{"tag", len(base) - 1},
	}
	for _, tc := range tamperOffsets {
		bad := bytes.Clone(base)
		bad[tc.idx] ^= 0x01
		_, _ = cliConn.Write(bad)
	}
	// A tampered packet claiming seq=MaxUint32 must NOT advance the server's replay window.
	badHigh := c2s.seal(nil, udpDirClientToServer, udpData, cliID, math.MaxUint32, ChannelUnreliable, []byte("dos"))
	badHigh[len(badHigh)-1] ^= 0xFF
	_, _ = cliConn.Write(badHigh)

	// Send a legitimate datagram with seq=11; it must be the ONLY packet delivered.
	if err := cli.Send(srv.LocalID(), ChannelUnreliable, []byte("good-after-tamper")); err != nil {
		t.Fatalf("cli.Send: %v", err)
	}
	pkt := recvOn(t, srv, srv.changed)
	if string(pkt.Payload) != "good-after-tamper" {
		t.Fatalf("server delivered %q, want good-after-tamper", pkt.Payload)
	}
	if extra, ok := srv.Recv(); ok {
		t.Fatalf("server delivered unexpected packet %+v", extra)
	}

	// Now test tampering on the server->client direction.
	srv.mu.Lock()
	link := srv.table.sessions[cliID].link.(*udpLink)
	s2c := link.sendCipher
	cliAddr := link.addr
	srv.mu.Unlock()

	srvBase := s2c.seal(nil, udpDirServerToClient, udpServerData, cliID, 10, ChannelUnreliable, []byte("server-secret"))
	for _, idx := range []int{0, 4, 8, 9, udpCleartextHeader + 3, len(srvBase) - 1} {
		bad := bytes.Clone(srvBase)
		bad[idx] ^= 0x01
		_, _ = srv.conn.WriteTo(bad, cliAddr)
	}
	if err := srv.Send(cliID, ChannelUnreliable, []byte("good-from-server")); err != nil {
		t.Fatalf("srv.Send: %v", err)
	}
	cpkt := recvOn(t, cli, cli.changed)
	if string(cpkt.Payload) != "good-from-server" {
		t.Fatalf("client delivered %q, want good-from-server", cpkt.Payload)
	}
	if extra, ok := cli.Recv(); ok {
		t.Fatalf("client delivered unexpected packet %+v", extra)
	}
}

func TestUDPSessionRejectsReplaysBothDirections(t *testing.T) {
	srv := listenUDPServer(t, Auth{})
	cli, err := DialUDP(srv.Addr().String(), srv.PublicKey(), nil)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer func() { _ = cli.Close() }()
	_ = nextEvent(t, srv, srv.changed)

	cli.mu.Lock()
	c2s := cli.sendCipher
	cliConn := cli.conn
	cliID := cli.localID
	cli.mu.Unlock()

	// Replay seq 1 and out-of-window seq 2 after seq 100 on client->server.
	d1 := c2s.seal(nil, udpDirClientToServer, udpData, cliID, 1, ChannelUnreliable, []byte("one"))
	d2 := c2s.seal(nil, udpDirClientToServer, udpData, cliID, 2, ChannelUnreliable, []byte("two-old"))
	d100 := c2s.seal(nil, udpDirClientToServer, udpData, cliID, 100, ChannelUnreliable, []byte("hundred"))
	_, _ = cliConn.Write(d1)
	_, _ = cliConn.Write(d1)   // exact replay
	_, _ = cliConn.Write(d100) // advances window top to 100
	_, _ = cliConn.Write(d2)   // outside 64-entry window
	d101 := c2s.seal(nil, udpDirClientToServer, udpData, cliID, 101, ChannelUnreliable, []byte("done"))
	_, _ = cliConn.Write(d101)

	var srvGot []string
	awaitChange(t, srv.changed, "server packets", func() bool {
		for pkt, ok := srv.Recv(); ok; pkt, ok = srv.Recv() {
			srvGot = append(srvGot, string(pkt.Payload))
		}
		return len(srvGot) > 0 && srvGot[len(srvGot)-1] == "done"
	})
	if got := bytes.Join(stringsToBytes(srvGot), []byte(",")); string(got) != "one,hundred,done" {
		t.Fatalf("server got %v, want [one hundred done]", srvGot)
	}

	// Replay on server->client direction.
	srv.mu.Lock()
	link := srv.table.sessions[cliID].link.(*udpLink)
	s2c := link.sendCipher
	cliAddr := link.addr
	srv.mu.Unlock()

	s1 := s2c.seal(nil, udpDirServerToClient, udpServerData, cliID, 1, ChannelUnreliable, []byte("s-one"))
	s100 := s2c.seal(nil, udpDirServerToClient, udpServerData, cliID, 100, ChannelUnreliable, []byte("s-hundred"))
	s2 := s2c.seal(nil, udpDirServerToClient, udpServerData, cliID, 2, ChannelUnreliable, []byte("s-old"))
	s101 := s2c.seal(nil, udpDirServerToClient, udpServerData, cliID, 101, ChannelUnreliable, []byte("s-done"))
	_, _ = srv.conn.WriteTo(s1, cliAddr)
	_, _ = srv.conn.WriteTo(s1, cliAddr)
	_, _ = srv.conn.WriteTo(s100, cliAddr)
	_, _ = srv.conn.WriteTo(s2, cliAddr)
	_, _ = srv.conn.WriteTo(s101, cliAddr)

	var cliGot []string
	awaitChange(t, cli.changed, "client packets", func() bool {
		for pkt, ok := cli.Recv(); ok; pkt, ok = cli.Recv() {
			cliGot = append(cliGot, string(pkt.Payload))
		}
		return len(cliGot) > 0 && cliGot[len(cliGot)-1] == "s-done"
	})
	if got := bytes.Join(stringsToBytes(cliGot), []byte(",")); string(got) != "s-one,s-hundred,s-done" {
		t.Fatalf("client got %v, want [s-one s-hundred s-done]", cliGot)
	}
}

func stringsToBytes(ss []string) [][]byte {
	out := make([][]byte, len(ss))
	for i, s := range ss {
		out[i] = []byte(s)
	}
	return out
}

func TestUDPSessionRejectsReflectedDatagrams(t *testing.T) {
	srv := listenUDPServer(t, Auth{})
	cli, err := DialUDP(srv.Addr().String(), srv.PublicKey(), nil)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer func() { _ = cli.Close() }()
	_ = nextEvent(t, srv, srv.changed)

	cli.mu.Lock()
	c2s := cli.sendCipher
	cliConn := cli.conn
	cliID := cli.localID
	cli.mu.Unlock()

	srv.mu.Lock()
	link := srv.table.sessions[cliID].link.(*udpLink)
	s2c := link.sendCipher
	cliAddr := link.addr
	srv.mu.Unlock()

	// Reflect a valid client->server datagram back to the client (verbatim, and with its type byte rewritten to udpServerData).
	c2sDgram := c2s.seal(nil, udpDirClientToServer, udpData, cliID, 5, ChannelUnreliable, []byte("reflected-to-cli"))
	_, _ = srv.conn.WriteTo(c2sDgram, cliAddr)
	c2sFlipped := bytes.Clone(c2sDgram)
	c2sFlipped[0] = udpServerData
	_, _ = srv.conn.WriteTo(c2sFlipped, cliAddr)

	if err := srv.Send(cliID, ChannelUnreliable, []byte("real-s2c")); err != nil {
		t.Fatalf("srv.Send: %v", err)
	}
	if pkt := recvOn(t, cli, cli.changed); string(pkt.Payload) != "real-s2c" {
		t.Fatalf("client accepted reflected datagram: %+v", pkt)
	}

	// Reflect a valid server->client datagram back to the server (verbatim, and with its type byte rewritten to udpData).
	s2cDgram := s2c.seal(nil, udpDirServerToClient, udpServerData, cliID, 6, ChannelUnreliable, []byte("reflected-to-srv"))
	_, _ = cliConn.Write(s2cDgram)
	s2cFlipped := bytes.Clone(s2cDgram)
	s2cFlipped[0] = udpData
	_, _ = cliConn.Write(s2cFlipped)

	if err := cli.Send(srv.LocalID(), ChannelUnreliable, []byte("real-c2s")); err != nil {
		t.Fatalf("cli.Send: %v", err)
	}
	if pkt := recvOn(t, srv, srv.changed); string(pkt.Payload) != "real-c2s" {
		t.Fatalf("server accepted reflected datagram: %+v", pkt)
	}
}

func TestUDPSessionRejectsWrongServerKeyAndInvalidKeys(t *testing.T) {
	// Invalid curve keys must be refused immediately.
	p256Key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ListenUDPServer("127.0.0.1:0", 1, Auth{StaticKey: p256Key}); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("ListenUDPServer with P256 key = %v, want ErrInvalidKey", err)
	}
	if _, err := DialUDP("127.0.0.1:1", nil, nil); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("DialUDP with nil key = %v, want ErrInvalidKey", err)
	}
	if _, err := DialUDP("127.0.0.1:1", p256Key.PublicKey(), nil); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("DialUDP with P256 key = %v, want ErrInvalidKey", err)
	}

	// Dialing a real server with another X25519 public key: server drops the hello silently and client times out without connecting.
	srv := listenUDPServer(t, Auth{})
	wrongKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dialUDP(srv.Addr().String(), wrongKey.PublicKey(), []byte("tok"), 150*time.Millisecond); err == nil {
		t.Fatal("dialUDP with wrong server key succeeded")
	}
	if peers := srv.Peers(); len(peers) != 0 {
		t.Fatalf("server admitted peers %v after wrong-key hello", peers)
	}
	expectNoEvent(t, srv)

	// MITM server that hands out a cookie and then replies with a forged version-3 welcome encrypted with a different key: client must reject it and not open.
	mitmConn, err := stdnet.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mitmConn.Close() }()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := mitmConn.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < udpHelloHeader || buf[0] != hsHello {
				continue
			}
			// Hand out a cookie first, so the client sends its Noise hello and the forged welcome below answers it.
			if [udpCookieSize]byte(buf[udpHelloPrefixSize:udpHelloHeader]) == ([udpCookieSize]byte{}) {
				_, _ = mitmConn.WriteTo(encodeUDPCookie([hsNonceSize]byte(buf[2:udpHelloPrefixSize]), testCookie), from)
				continue
			}
			// Send a forged welcome echoing the client's nonce with random Noise bytes.
			fake := make([]byte, udpWelcomeSize)
			fake[0] = hsWelcome
			fake[1] = udpHandshakeVersion
			copy(fake[2:udpHelloPrefixSize], buf[2:udpHelloPrefixSize])
			_, _ = rand.Read(fake[udpHelloPrefixSize:])
			_, _ = mitmConn.WriteTo(fake, from)
		}
	}()
	if _, err := dialUDP(mitmConn.LocalAddr().String(), srv.PublicKey(), []byte("tok"), 150*time.Millisecond); err == nil {
		t.Fatal("dialUDP accepted MITM forged welcome")
	}
}

func TestUDPSessionRefusesSequenceWrapAndRekeysOnReconnect(t *testing.T) {
	clock := newFakeClock()
	srv := listenUDPServer(t, Auth{GracePeriod: time.Minute, Now: clock.Now})
	cli, err := DialUDP(srv.Addr().String(), srv.PublicKey(), nil)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer func() { _ = cli.Close() }()
	cliID := cli.LocalID()
	_ = nextEvent(t, srv, srv.changed)

	// Advance client sendSeq to MaxUint32-1: one more send succeeds at MaxUint32, then the next refuses to wrap.
	cli.mu.Lock()
	cli.sendSeq = math.MaxUint32 - 1
	oldSendCipher := cli.sendCipher
	cliConn := cli.conn
	cli.mu.Unlock()

	if err := cli.Send(srv.LocalID(), ChannelUnreliable, []byte("last-valid")); err != nil {
		t.Fatalf("Send at MaxUint32: %v", err)
	}
	if pkt := recvOn(t, srv, srv.changed); pkt.Seq != math.MaxUint32 || string(pkt.Payload) != "last-valid" {
		t.Fatalf("server got %+v, want seq MaxUint32", pkt)
	}
	if err := cli.Send(srv.LocalID(), ChannelUnreliable, []byte("would-wrap")); !errors.Is(err, ErrSequenceExhausted) {
		t.Fatalf("Send past MaxUint32 = %v, want ErrSequenceExhausted", err)
	}

	// Even if a buggy or hostile sender wraps seq to 0 or 1 with the same key, the server refuses it.
	_, _ = cliConn.Write(oldSendCipher.seal(nil, udpDirClientToServer, udpData, cliID, 0, ChannelUnreliable, []byte("wrap-0")))
	_, _ = cliConn.Write(oldSendCipher.seal(nil, udpDirClientToServer, udpData, cliID, 1, ChannelUnreliable, []byte("wrap-1")))

	// Advance server sendSeq to MaxUint32 for cliID and verify unicast and broadcast refusal.
	cli2, err := DialUDP(srv.Addr().String(), srv.PublicKey(), nil)
	if err != nil {
		t.Fatalf("DialUDP cli2: %v", err)
	}
	defer func() { _ = cli2.Close() }()
	_ = nextEvent(t, srv, srv.changed)

	srv.mu.Lock()
	srv.table.sessions[cliID].link.(*udpLink).sendSeq = math.MaxUint32
	srv.mu.Unlock()

	if err := srv.Send(cliID, ChannelUnreliable, []byte("srv-wrap")); !errors.Is(err, ErrSequenceExhausted) {
		t.Fatalf("srv.Send unicast at MaxUint32 = %v, want ErrSequenceExhausted", err)
	}
	if err := srv.Send(PeerBroadcast, ChannelUnreliable, []byte("broadcast-ok-for-cli2")); !errors.Is(err, ErrSequenceExhausted) {
		t.Fatalf("srv.Send broadcast with exhausted peer = %v, want ErrSequenceExhausted", err)
	}
	// cli2 still received the broadcast while cli did not.
	if pkt := recvOn(t, cli2, cli2.changed); string(pkt.Payload) != "broadcast-ok-for-cli2" {
		t.Fatalf("cli2 got %+v", pkt)
	}
	if extra, ok := srv.Recv(); ok {
		t.Fatalf("server accepted wrapped packet %+v", extra)
	}

	// Reconnecting runs a fresh Noise NK handshake, rekeying both directions and resetting sequence counters.
	if err := cli.Reconnect(); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	if !cli.Resumed() || cli.LocalID() != cliID {
		t.Fatalf("Reconnect resumed=%v id=%d, want true %d", cli.Resumed(), cli.LocalID(), cliID)
	}
	if err := cli.Send(srv.LocalID(), ChannelUnreliable, []byte("after-rekey")); err != nil {
		t.Fatalf("cli.Send after Reconnect: %v", err)
	}
	if pkt := recvOn(t, srv, srv.changed); pkt.Seq != 1 || string(pkt.Payload) != "after-rekey" {
		t.Fatalf("server got %+v, want seq 1 after-rekey", pkt)
	}
	if err := srv.Send(cliID, ChannelUnreliable, []byte("srv-after-rekey")); err != nil {
		t.Fatalf("srv.Send after Reconnect: %v", err)
	}
	if pkt := recvOn(t, cli, cli.changed); pkt.Seq != 1 || string(pkt.Payload) != "srv-after-rekey" {
		t.Fatalf("client got %+v, want seq 1 srv-after-rekey", pkt)
	}
}

func TestUDPTransportAddPeerKeySecurity(t *testing.T) {
	u1, err := ListenUDP("127.0.0.1:0", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = u1.Close() }()
	u2, err := ListenUDP("127.0.0.1:0", 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = u2.Close() }()

	// Validation: all-zero key, peer 0, or peer == localID must be refused.
	var zero [32]byte
	if err := u1.AddPeerKey(2, u2.Addr().String(), zero); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("AddPeerKey zero key = %v, want ErrInvalidKey", err)
	}
	var key [32]byte
	_, _ = rand.Read(key[:])
	if err := u1.AddPeerKey(0, u2.Addr().String(), key); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("AddPeerKey peer 0 = %v, want ErrInvalidPacket", err)
	}
	if err := u1.AddPeerKey(1, u2.Addr().String(), key); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("AddPeerKey self = %v, want ErrInvalidPacket", err)
	}

	if err := u1.AddPeerKey(2, u2.Addr().String(), key); err != nil {
		t.Fatalf("u1.AddPeerKey: %v", err)
	}
	if err := u2.AddPeerKey(1, u1.Addr().String(), key); err != nil {
		t.Fatalf("u2.AddPeerKey: %v", err)
	}

	// Round trip both ways.
	recvUDP := func(u *UDPTransport) Packet {
		t.Helper()
		var pkt Packet
		waitFor(t, "udp transport packet", func() bool {
			var ok bool
			pkt, ok = u.Recv()
			return ok
		})
		return pkt
	}
	if err := u1.Send(2, ChannelUnreliable, []byte("secret-p2p-1")); err != nil {
		t.Fatalf("u1.Send: %v", err)
	}
	if pkt := recvUDP(u2); pkt.From != 1 || pkt.Seq != 1 || string(pkt.Payload) != "secret-p2p-1" {
		t.Fatalf("u2 got %+v", pkt)
	}
	if err := u2.Send(1, ChannelUnreliable, []byte("secret-p2p-2")); err != nil {
		t.Fatalf("u2.Send: %v", err)
	}
	if pkt := recvUDP(u1); pkt.From != 2 || pkt.Seq != 1 || string(pkt.Payload) != "secret-p2p-2" {
		t.Fatalf("u1 got %+v", pkt)
	}

	// Send tampered, replayed, reflected, and plaintext downgrade datagrams directly from u2's socket to u1.
	c := newUDPCipher(key)
	// Plaintext downgrade claiming from=2 must be dropped for keyed peer 2.
	plain := make([]byte, udpHeaderSize+4)
	binary.BigEndian.PutUint32(plain[0:4], 2)
	plain[4] = byte(ChannelUnreliable)
	binary.BigEndian.PutUint32(plain[5:9], 50)
	copy(plain[udpHeaderSize:], "down")
	_, _ = u2.conn.WriteTo(plain, u1.Addr())

	// Reflected u1->u2 packet sent back to u1 must be dropped (wrong peer + wrong direction nonce).
	reflected := c.seal(nil, uint32(2), udpPeerData, 1, 51, ChannelUnreliable, []byte("reflect"))
	_, _ = u2.conn.WriteTo(reflected, u1.Addr())
	// Even if an attacker rewrites header.peer from 1 to 2, AD and nonce fail.
	reflectedRewritten := bytes.Clone(reflected)
	binary.BigEndian.PutUint32(reflectedRewritten[1:5], 2)
	_, _ = u2.conn.WriteTo(reflectedRewritten, u1.Addr())

	// Tampered header, payload, and tag from u2 -> u1.
	good := c.seal(nil, uint32(1), udpPeerData, 2, 10, ChannelUnreliable, []byte("ok-ten"))
	for _, idx := range []int{0, 4, 8, 9, udpCleartextHeader, len(good) - 1} {
		bad := bytes.Clone(good)
		bad[idx] ^= 0x01
		_, _ = u2.conn.WriteTo(bad, u1.Addr())
	}
	// Replay of seq 10: delivered once, second copy dropped.
	_, _ = u2.conn.WriteTo(good, u1.Addr())
	_, _ = u2.conn.WriteTo(good, u1.Addr())

	// Sentinel packet seq 11.
	good11 := c.seal(nil, uint32(1), udpPeerData, 2, 11, ChannelUnreliable, []byte("ok-eleven"))
	_, _ = u2.conn.WriteTo(good11, u1.Addr())

	if pkt := recvUDP(u1); pkt.Seq != 10 || string(pkt.Payload) != "ok-ten" {
		t.Fatalf("u1 first = %+v, want seq 10 ok-ten", pkt)
	}
	if pkt := recvUDP(u1); pkt.Seq != 11 || string(pkt.Payload) != "ok-eleven" {
		t.Fatalf("u1 second = %+v, want seq 11 ok-eleven", pkt)
	}
	if extra, ok := u1.Recv(); ok {
		t.Fatalf("u1 received unexpected packet %+v", extra)
	}

	// Sequence-wrap refusal on keyed peer, and rekey via AddPeerKey.
	u1.mu.Lock()
	u1.peers[2].sendSeq = math.MaxUint32
	u1.mu.Unlock()
	if err := u1.Send(2, ChannelUnreliable, []byte("wrap")); !errors.Is(err, ErrSequenceExhausted) {
		t.Fatalf("u1.Send at MaxUint32 = %v, want ErrSequenceExhausted", err)
	}
	var key2 [32]byte
	_, _ = rand.Read(key2[:])
	if err := u1.AddPeerKey(2, u2.Addr().String(), key2); err != nil {
		t.Fatal(err)
	}
	if err := u2.AddPeerKey(1, u1.Addr().String(), key2); err != nil {
		t.Fatal(err)
	}
	if err := u1.Send(2, ChannelUnreliable, []byte("rekeyed")); err != nil {
		t.Fatalf("u1.Send after rekey: %v", err)
	}
	if pkt := recvUDP(u2); pkt.Seq != 1 || string(pkt.Payload) != "rekeyed" {
		t.Fatalf("u2 after rekey got %+v", pkt)
	}
}

// TestRelayLockstepOverEncryptedUDP verifies a deterministic LockstepSession running end to end over RelayTransport on top of Noise-NK + ChaCha20-Poly1305 encrypted UDPClient/UDPServer.
func TestRelayLockstepOverEncryptedUDP(t *testing.T) {
	staticKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	relay, dial := startRelay(t, "udp", RelayConfig{}, Auth{StaticKey: staticKey})
	a := mustDialRelay(t, dial, "lockstep/alice")
	b := mustDialRelay(t, dial, "lockstep/bob")
	awaitPeers(t, a, b.LocalID())
	awaitPeers(t, b, a.LocalID())

	simA, simB := &relaySim{}, &relaySim{}
	sa := NewLockstepSession(LockstepConfig{Transport: a, Sim: simA, ChecksumInterval: 5})
	sb := NewLockstepSession(LockstepConfig{Transport: b, Sim: simB, ChecksumInterval: 5})

	const ticks = 40
	var sentA, sentB uint32
	awaitEither(t, a.changed, b.changed, "lockstep session over encrypted UDP to finish", func() bool {
		if sa.Tick() < ticks && sentA <= sa.Tick() {
			if err := sa.SubmitInput(relayInput(a.LocalID(), sa.Tick())); err != nil {
				t.Fatalf("sa.SubmitInput: %v", err)
			}
			sentA = sa.Tick() + 1
		}
		if sb.Tick() < ticks && sentB <= sb.Tick() {
			if err := sb.SubmitInput(relayInput(b.LocalID(), sb.Tick())); err != nil {
				t.Fatalf("sb.SubmitInput: %v", err)
			}
			sentB = sb.Tick() + 1
		}
		for sa.Tick() < ticks && sa.Step() {
		}
		for sb.Tick() < ticks && sb.Step() {
		}
		return sa.Tick() >= ticks && sb.Tick() >= ticks
	})

	if simA.tick != ticks || simB.tick != ticks || simA.Checksum() != simB.Checksum() {
		t.Fatalf("lockstep states differ after %d ticks: a=%+v b=%+v", ticks, simA, simB)
	}
	if d := append(sa.Desyncs(), sb.Desyncs()...); len(d) != 0 {
		t.Fatalf("desyncs: %+v", d)
	}
	if st := relay.Stats(); st.Sessions != 1 || st.Peers != 2 || st.Forwarded == 0 || st.Dropped != 0 {
		t.Fatalf("relay stats = %+v", st)
	}
}

func BenchmarkUDPCipherSeal(b *testing.B) {
	c := newUDPCipher([32]byte{1, 2, 3, 4})
	payload := make([]byte, 256)
	dst := make([]byte, 0, udpSealedHeader+len(payload))
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	var seq uint32
	for b.Loop() {
		seq++
		dst = c.seal(dst[:0], udpDirClientToServer, udpData, 2, seq, ChannelUnreliable, payload)
	}
}

func BenchmarkUDPCipherOpen(b *testing.B) {
	key := [32]byte{1, 2, 3, 4}
	sealer := newUDPCipher(key)
	opener := newUDPCipher(key)
	payload := make([]byte, 256)
	dgram := sealer.seal(nil, udpDirClientToServer, udpData, 2, 1, ChannelUnreliable, payload)
	plain := make([]byte, 0, len(payload))
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for b.Loop() {
		var ok bool
		plain, ok = opener.open(plain[:0], udpDirClientToServer, dgram)
		if !ok {
			b.Fatal("open failed")
		}
	}
}
