//go:build !js

package netplay

import (
	"encoding/binary"
	stdnet "net"
	"testing"
)

var (
	_ InboxReporter = (*UDPTransport)(nil)
	_ InboxReporter = (*UDPServer)(nil)
	_ InboxReporter = (*UDPClient)(nil)
	_ InboxReporter = (*WebSocketTransport)(nil)
	_ InboxReporter = (*WebSocketServer)(nil)
)

// #546: a UDPTransport nobody drains keeps the newest unreliable datagrams up to its cap.
func TestUDPTransportUnreliableInboxIsCapped(t *testing.T) {
	conn := newScriptedPacketConn()
	u := newUDPTransport(conn, 1)
	defer func() { _ = u.Close() }()
	u.SetInboxLimits(InboxLimits{Unreliable: 3})
	peer := &stdnet.UDPAddr{IP: stdnet.IPv4(127, 0, 0, 1), Port: 9000}
	if err := u.AddPeer(2, peer.String()); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	const sent = 10
	for i := range uint32(sent) {
		dgram := make([]byte, udpHeaderSize)
		binary.BigEndian.PutUint32(dgram[0:4], 2)
		dgram[4] = byte(ChannelUnreliable)
		binary.BigEndian.PutUint32(dgram[5:9], i+1)
		conn.feed(t, scriptedRead{data: dgram, from: peer})
	}
	waitFor(t, "every datagram to reach the inbox", func() bool {
		st := u.InboxStats()
		return uint64(st.Unreliable)+st.UnreliableDropped == sent
	})
	if st := u.InboxStats(); st.Unreliable != 3 || st.UnreliableDropped != sent-3 {
		t.Fatalf("InboxStats = %+v, want 3 queued and %d dropped", st, sent-3)
	}
	for want := uint32(sent - 2); want <= sent; want++ {
		p, ok := u.Recv()
		if !ok || p.Seq != want {
			t.Fatalf("Recv = %+v, %v; want seq %d", p, ok, want)
		}
	}
}

// #546: over WebSocket the client's unreliable inbox is capped while every reliable packet is kept and the backlog warning counts once.
func TestWebSocketInboxLimits(t *testing.T) {
	srv, err := ListenWebSocket("127.0.0.1:0", 1, Auth{})
	if err != nil {
		t.Fatalf("ListenWebSocket: %v", err)
	}
	defer func() { _ = srv.Close() }()
	cli, err := DialWebSocket("ws://"+srv.Addr().String()+"/game", nil)
	if err != nil {
		t.Fatalf("DialWebSocket: %v", err)
	}
	defer func() { _ = cli.Close() }()
	cli.SetInboxLimits(InboxLimits{Unreliable: 4, ReliableWarning: 10})
	waitFor(t, "the server to see the client", func() bool { return len(srv.Peers()) == 1 })

	const unrel, rel = 30, 50
	for i := range unrel {
		if err := srv.Send(cli.LocalID(), ChannelUnreliable, []byte{byte(i)}); err != nil {
			t.Fatalf("Send unreliable: %v", err)
		}
	}
	for i := range rel {
		if err := srv.Send(cli.LocalID(), ChannelReliableOrdered, []byte{byte(i)}); err != nil {
			t.Fatalf("Send reliable: %v", err)
		}
	}
	waitFor(t, "every packet to reach the client's inbox", func() bool {
		st := cli.InboxStats()
		return st.Reliable == rel && uint64(st.Unreliable)+st.UnreliableDropped == unrel
	})
	if st := cli.InboxStats(); st.Unreliable != 4 || st.UnreliableDropped != unrel-4 || st.ReliableWarnings != 1 || !st.ReliableBacklog {
		t.Fatalf("InboxStats = %+v", st)
	}
	for i := unrel - 4; i < unrel; i++ {
		if p, ok := cli.Recv(); !ok || p.Channel != ChannelUnreliable || p.Payload[0] != byte(i) {
			t.Fatalf("Recv = %+v, %v; want unreliable %d", p, ok, i)
		}
	}
	for i := range rel {
		if p, ok := cli.Recv(); !ok || p.Channel != ChannelReliableOrdered || p.Payload[0] != byte(i) {
			t.Fatalf("Recv = %+v, %v; want reliable %d", p, ok, i)
		}
	}
	if st := cli.InboxStats(); st.ReliableBacklog || st.ReliableWarnings != 1 {
		t.Fatalf("after draining: %+v", st)
	}
}

// #546: a WebSocketServer caps each client's unreliable packets separately, so one client's flood leaves another's queued packets alone, and a client that floods the reliable channel past its cap loses its session instead of growing the server's inbox.
func TestWebSocketServerInboxLimitsPerClient(t *testing.T) {
	srv, url := listenWS(t, Auth{})
	srv.SetInboxLimits(InboxLimits{Unreliable: 4, Reliable: 5})
	flood, err := DialWebSocket(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = flood.Close() }()
	quiet, err := DialWebSocket(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = quiet.Close() }()
	_ = nextEvent(t, srv, srv.changed)
	_ = nextEvent(t, srv, srv.changed)

	for i := range 2 {
		if err := quiet.Send(srv.LocalID(), ChannelUnreliable, []byte{byte(i)}); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	awaitChange(t, srv.changed, "the quiet client's packets", func() bool { return srv.InboxStats().Unreliable == 2 })
	const unrel = 30
	for i := range unrel {
		if err := flood.Send(srv.LocalID(), ChannelUnreliable, []byte{byte(i)}); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	awaitChange(t, srv.changed, "the flood", func() bool {
		st := srv.InboxStats()
		return uint64(st.Unreliable)+st.UnreliableDropped == 2+unrel
	})
	if st := srv.InboxStats(); st.Unreliable != 6 || st.UnreliableDropped != unrel-4 {
		t.Fatalf("InboxStats = %+v, want the quiet client's 2 and the flood's newest 4 queued", st)
	}

	for i := range 10 {
		if err := flood.Send(srv.LocalID(), ChannelReliableOrdered, []byte{byte(i)}); err != nil {
			break // the server may already have closed the connection
		}
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerLeft, Peer: flood.LocalID()}) {
		t.Fatalf("event = %+v, want the flooding client to leave", ev)
	}
	if st := srv.InboxStats(); st.Reliable != 5 || st.ReliableRefused != 1 {
		t.Fatalf("InboxStats = %+v, want 5 reliable queued and 1 refused", st)
	}
	if got := srv.Peers(); len(got) != 1 || got[0] != quiet.LocalID() {
		t.Fatalf("Peers = %v, want only the quiet client", got)
	}

	// Recv returns the quiet client's packets, the flood's newest unreliable ones, then the reliable ones it delivered, in order.
	for i := range 2 {
		if p, ok := srv.Recv(); !ok || p.From != quiet.LocalID() || p.Payload[0] != byte(i) {
			t.Fatalf("Recv = %+v, %v; want the quiet client's packet %d", p, ok, i)
		}
	}
	for i := unrel - 4; i < unrel; i++ {
		if p, ok := srv.Recv(); !ok || p.From != flood.LocalID() || p.Channel != ChannelUnreliable || p.Payload[0] != byte(i) {
			t.Fatalf("Recv = %+v, %v; want the flood's unreliable %d", p, ok, i)
		}
	}
	for i := range 5 {
		if p, ok := srv.Recv(); !ok || p.Channel != ChannelReliableOrdered || p.Payload[0] != byte(i) {
			t.Fatalf("Recv = %+v, %v; want reliable %d", p, ok, i)
		}
	}
}

// #546: UDPServer and UDPClient cap the unreliable datagrams they receive.
func TestUDPServerAndClientInboxLimits(t *testing.T) {
	srv := listenUDPServer(t, Auth{})
	srv.SetInboxLimits(InboxLimits{Unreliable: 3})
	cli, err := DialUDP(srv.Addr().String(), srv.PublicKey(), nil)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer func() { _ = cli.Close() }()
	cli.SetInboxLimits(InboxLimits{Unreliable: 2})
	_ = nextEvent(t, srv, srv.changed)

	const sent = 10
	for i := range sent {
		if err := cli.Send(srv.LocalID(), ChannelUnreliable, []byte{byte(i)}); err != nil {
			t.Fatalf("client Send: %v", err)
		}
		if err := srv.Send(cli.LocalID(), ChannelUnreliable, []byte{byte(i)}); err != nil {
			t.Fatalf("server Send: %v", err)
		}
	}
	awaitChange(t, srv.changed, "the client's datagrams", func() bool {
		st := srv.InboxStats()
		return uint64(st.Unreliable)+st.UnreliableDropped == sent
	})
	awaitChange(t, cli.changed, "the server's datagrams", func() bool {
		st := cli.InboxStats()
		return uint64(st.Unreliable)+st.UnreliableDropped == sent
	})
	if st := srv.InboxStats(); st.Unreliable != 3 || st.UnreliableDropped != sent-3 {
		t.Fatalf("server InboxStats = %+v", st)
	}
	if st := cli.InboxStats(); st.Unreliable != 2 || st.UnreliableDropped != sent-2 {
		t.Fatalf("client InboxStats = %+v", st)
	}
	for i := sent - 3; i < sent; i++ {
		if p, ok := srv.Recv(); !ok || p.Payload[0] != byte(i) {
			t.Fatalf("server Recv = %+v, %v; want %d", p, ok, i)
		}
	}
	for i := sent - 2; i < sent; i++ {
		if p, ok := cli.Recv(); !ok || p.Payload[0] != byte(i) {
			t.Fatalf("client Recv = %+v, %v; want %d", p, ok, i)
		}
	}
}

// #546: a UDPTransport never sends ChannelReliableOrdered, so it drops a datagram claiming it rather than queue it where no cap applies.
func TestUDPTransportDropsForgedReliableDatagrams(t *testing.T) {
	conn := newScriptedPacketConn()
	u := newUDPTransport(conn, 1)
	defer func() { _ = u.Close() }()
	peer := &stdnet.UDPAddr{IP: stdnet.IPv4(127, 0, 0, 1), Port: 9000}
	if err := u.AddPeer(2, peer.String()); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	for i, ch := range []Channel{ChannelReliableOrdered, ChannelUnreliable} {
		dgram := make([]byte, udpHeaderSize)
		binary.BigEndian.PutUint32(dgram[0:4], 2)
		dgram[4] = byte(ch)
		binary.BigEndian.PutUint32(dgram[5:9], uint32(i+1))
		conn.feed(t, scriptedRead{data: dgram, from: peer})
	}
	waitFor(t, "the unreliable datagram", func() bool { return u.InboxStats().Unreliable == 1 })
	if st := u.InboxStats(); st.Reliable != 0 {
		t.Fatalf("InboxStats = %+v, want the reliable datagram dropped", st)
	}
}

// #546: SetInboxLimits and InboxStats run while the receive loop pushes packets; run with -race, this checks the inbox is only touched under the transport's lock. Lowering the cap counts what it drops, so queued plus dropped always adds up to what arrived.
func TestWebSocketServerInboxLimitsChangeWhileReceiving(t *testing.T) {
	srv, url := listenWS(t, Auth{})
	cli, err := DialWebSocket(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cli.Close() }()
	_ = nextEvent(t, srv, srv.changed)
	const sent = 500
	done := make(chan error, 1)
	go func() {
		for i := range sent {
			if err := cli.Send(srv.LocalID(), ChannelUnreliable, []byte{byte(i)}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	caps := []int{2, 50, 7, -1}
	for i := 0; ; i++ {
		srv.SetInboxLimits(InboxLimits{Unreliable: caps[i%len(caps)]})
		st := srv.InboxStats()
		if uint64(st.Unreliable)+st.UnreliableDropped == sent {
			break
		}
		if i > 1<<22 {
			t.Fatalf("InboxStats = %+v after %d rounds, want %d packets accounted for", st, i, sent)
		}
	}
	if err := <-done; err != nil {
		t.Fatalf("Send: %v", err)
	}
}
