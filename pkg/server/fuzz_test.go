package server_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/net"
	"github.com/nsgo-dev/game/pkg/server"
)

func FuzzGuard(f *testing.F) {
	f.Add(uint8(1), []byte("hello"))
	f.Add(uint8(7), []byte{})
	f.Add(uint8(3), make([]byte, 1024))
	f.Fuzz(func(t *testing.T, peer uint8, payload []byte) {
		g := server.NewGuard(server.Limits{
			PacketsPerSecond: 100,
			BytesPerSecond:   64 << 10,
			MaxMessageSize:   4096,
		}, server.LimitDrop)
		_, _ = g.Check(net.PeerID(peer%8+1), net.Packet{Payload: payload})
	})
}
