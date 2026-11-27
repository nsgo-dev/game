//go:build !js

package netplay_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
)

func waitPacket(t *testing.T, tr netplay.Transport, timeout time.Duration) netplay.Packet {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pkt, ok := tr.Recv(); ok {
			return pkt
		}
		time.Sleep(1 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for packet on peer %d", tr.LocalID())
	return netplay.Packet{}
}

func waitPeers(t *testing.T, tr netplay.Transport, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(tr.Peers()) >= want {
			return
		}
		time.Sleep(1 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d peers on peer %d (got %v)", want, tr.LocalID(), tr.Peers())
}

func TestWebSocketHandshakeAndBinaryRoundTrip(t *testing.T) {
	srv, err := netplay.ListenWebSocket("127.0.0.1:0", 1, netplay.Auth{})
	if err != nil {
		t.Fatalf("ListenWebSocket: %v", err)
	}
	defer func() { _ = srv.Close() }()

	url := "ws://" + srv.Addr().String() + "/game"
	cliA, err := netplay.DialWebSocket(url, nil)
	if err != nil {
		t.Fatalf("DialWebSocket A: %v", err)
	}
	defer func() { _ = cliA.Close() }()

	cliB, err := netplay.DialWebSocket(url, nil)
	if err != nil {
		t.Fatalf("DialWebSocket B: %v", err)
	}
	defer func() { _ = cliB.Close() }()

	waitPeers(t, srv, 2, 2*time.Second)
	if cliA.LocalID() != 2 || cliB.LocalID() != 3 {
		t.Fatalf("assigned IDs = %d, %d; want 2, 3", cliA.LocalID(), cliB.LocalID())
	}

	// Exercise all three RFC 6455 payload length encodings: 7-bit, 16-bit, and 64-bit.
	payloads := [][]byte{
		[]byte("hello-ws"),
		bytes.Repeat([]byte{0xAB}, 1024),
		bytes.Repeat([]byte{0xCD}, 70000),
	}

	for i, want := range payloads {
		ch := netplay.ChannelReliableOrdered
		if i == 1 {
			ch = netplay.ChannelUnreliable
		}
		if err := cliA.Send(1, ch, want); err != nil {
			t.Fatalf("cliA.Send(%d): %v", i, err)
		}
		pkt := waitPacket(t, srv, 2*time.Second)
		if pkt.From != cliA.LocalID() || pkt.Channel != ch || !bytes.Equal(pkt.Payload, want) {
			t.Fatalf("client->server frame %d mismatch: from=%d ch=%v len=%d (want len=%d)",
				i, pkt.From, pkt.Channel, len(pkt.Payload), len(want))
		}

		// Server echoes back to client A.
		if err := srv.Send(cliA.LocalID(), ch, want); err != nil {
			t.Fatalf("srv.Send(%d): %v", i, err)
		}
		reply := waitPacket(t, cliA, 2*time.Second)
		if reply.From != 1 || reply.Channel != ch || !bytes.Equal(reply.Payload, want) {
			t.Fatalf("server->client frame %d mismatch: from=%d ch=%v len=%d (want len=%d)",
				i, reply.From, reply.Channel, len(reply.Payload), len(want))
		}
	}

	// Broadcast from server reaches both connected clients.
	broadcastMsg := []byte("broadcast-all")
	if err := srv.Send(netplay.PeerBroadcast, netplay.ChannelReliableOrdered, broadcastMsg); err != nil {
		t.Fatalf("srv broadcast: %v", err)
	}
	pA := waitPacket(t, cliA, 2*time.Second)
	pB := waitPacket(t, cliB, 2*time.Second)
	if !bytes.Equal(pA.Payload, broadcastMsg) || !bytes.Equal(pB.Payload, broadcastMsg) {
		t.Fatalf("broadcast mismatch: A=%q B=%q", pA.Payload, pB.Payload)
	}
}

func TestWebSocketServerReplicationRoundTrip(t *testing.T) {
	srvT, err := netplay.ListenWebSocket("127.0.0.1:0", 1, netplay.Auth{})
	if err != nil {
		t.Fatalf("ListenWebSocket: %v", err)
	}
	defer func() { _ = srvT.Close() }()

	cliT, err := netplay.DialWebSocket("ws://"+srvT.Addr().String(), nil)
	if err != nil {
		t.Fatalf("DialWebSocket: %v", err)
	}
	defer func() { _ = cliT.Close() }()
	waitPeers(t, srvT, 1, 2*time.Second)

	srv := netplay.NewServerWorld(srvT)
	cli := netplay.NewClientWorld(cliT, 1)

	id := srv.Spawn(netplay.EntityState{
		ComponentState: netplay.ComponentState{Kind: 7, X: 12.5, Y: -4.25, Props: []byte("ws")},
	})

	if err := srv.Step(1); err != nil {
		t.Fatalf("srv.Step(1): %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cli.Poll() > 0 {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}

	got, ok := cli.Entity(id)
	if !ok || got.Kind != 7 || got.X != 12.5 || got.Y != -4.25 || !bytes.Equal(got.Props, []byte("ws")) {
		t.Fatalf("replicated entity over WebSocket mismatch: ok=%v got=%+v", ok, got)
	}
}

func TestUDPTransportRoundTrip(t *testing.T) {
	u1, err := netplay.ListenUDP("127.0.0.1:0", 1)
	if err != nil {
		t.Fatalf("ListenUDP 1: %v", err)
	}
	defer func() { _ = u1.Close() }()

	u2, err := netplay.ListenUDP("127.0.0.1:0", 2)
	if err != nil {
		t.Fatalf("ListenUDP 2: %v", err)
	}
	defer func() { _ = u2.Close() }()

	if err := u1.AddPeer(2, u2.Addr().String()); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	if err := u2.AddPeer(1, u1.Addr().String()); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	msg := []byte("udp-ping")
	if err := u1.Send(2, netplay.ChannelUnreliable, msg); err != nil {
		t.Fatalf("u1.Send: %v", err)
	}
	pkt := waitPacket(t, u2, 2*time.Second)
	if pkt.From != 1 || pkt.Channel != netplay.ChannelUnreliable || !bytes.Equal(pkt.Payload, msg) || pkt.Seq != 1 {
		t.Fatalf("unexpected UDP packet: %+v", pkt)
	}

	// u2 replies to the address it registered for u1.
	reply := []byte("udp-pong")
	if err := u2.Send(1, netplay.ChannelUnreliable, reply); err != nil {
		t.Fatalf("u2.Send reply: %v", err)
	}
	pkt2 := waitPacket(t, u1, 2*time.Second)
	if pkt2.From != 2 || !bytes.Equal(pkt2.Payload, reply) {
		t.Fatalf("unexpected UDP reply: %+v", pkt2)
	}
}

func TestUDPRefusesTheReliableChannel(t *testing.T) {
	// UDP cannot retransmit or reorder, so it must refuse the reliable channel
	// rather than let lockstep and rollback lose inputs silently.
	u, err := netplay.ListenUDP("127.0.0.1:0", 1)
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer func() { _ = u.Close() }()
	if err := u.Send(2, netplay.ChannelReliableOrdered, []byte("input")); !errors.Is(err, netplay.ErrUnsupportedChannel) {
		t.Fatalf("reliable send over UDP = %v, want ErrUnsupportedChannel", err)
	}
}
