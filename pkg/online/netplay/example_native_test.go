//go:build !js

package netplay_test

import (
	"crypto/ecdh"
	"crypto/rand"
	"fmt"
	"runtime"

	"github.com/nsgo-dev/game/pkg/online/netplay"
)

// ListenUDPServer and DialUDP establish an encrypted UDP session with a Noise NK key exchange against the server's static X25519 key, then encrypt and authenticate every datagram with ChaCha20-Poly1305.
func ExampleListenUDPServer() {
	serverKey, _ := ecdh.X25519().GenerateKey(rand.Reader)
	srv, _ := netplay.ListenUDPServer("127.0.0.1:0", 1, netplay.Auth{StaticKey: serverKey})
	defer func() { _ = srv.Close() }()

	// Clients obtain the server's 32-byte X25519 public key out of band (e.g. matchmaking ticket or config).
	pubBytes := srv.PublicKey().Bytes()
	pubKey, _ := ecdh.X25519().NewPublicKey(pubBytes)

	cli, _ := netplay.DialUDP(srv.Addr().String(), pubKey, []byte("ticket"))
	defer func() { _ = cli.Close() }()

	_ = cli.Send(srv.LocalID(), netplay.ChannelUnreliable, []byte("move"))
	for {
		if pkt, ok := srv.Recv(); ok {
			fmt.Println("server got:", pkt.From, string(pkt.Payload))
			break
		}
		runtime.Gosched()
	}
	// Output:
	// server got: 2 move
}

// UDPTransport.AddPeerKey protects direct peer-to-peer UDP datagrams with ChaCha20-Poly1305 using a 32-byte pre-shared key, refusing tampered, replayed, reflected or plaintext datagrams.
func ExampleUDPTransport_AddPeerKey() {
	a, _ := netplay.ListenUDP("127.0.0.1:0", 1)
	defer func() { _ = a.Close() }()
	b, _ := netplay.ListenUDP("127.0.0.1:0", 2)
	defer func() { _ = b.Close() }()

	var key [32]byte
	_, _ = rand.Read(key[:])
	_ = a.AddPeerKey(2, b.Addr().String(), key)
	_ = b.AddPeerKey(1, a.Addr().String(), key)

	_ = a.Send(2, netplay.ChannelUnreliable, []byte("ping"))
	for {
		if pkt, ok := b.Recv(); ok {
			fmt.Println("peer 2 got:", pkt.From, string(pkt.Payload))
			break
		}
		runtime.Gosched()
	}
	// Output:
	// peer 2 got: 1 ping
}
