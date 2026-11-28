package gameserver_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/online/gameserver"
	"github.com/nsgo-dev/game/pkg/online/netplay"
)

func FuzzGuard(f *testing.F) {
	f.Add(uint8(1), []byte("hello"))
	f.Add(uint8(7), []byte{})
	f.Add(uint8(3), make([]byte, 1024))
	f.Fuzz(func(t *testing.T, peer uint8, payload []byte) {
		g := gameserver.NewGuard(gameserver.Limits{
			PacketsPerSecond: 100,
			BytesPerSecond:   64 << 10,
			MaxMessageSize:   4096,
		}, gameserver.LimitDrop)
		_, _ = g.Check(netplay.PeerID(peer%8+1), netplay.Packet{Payload: payload})
	})
}
