package rooms_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/rooms"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*rooms.Map]{
		Decode: rooms.Parse,
		Encode: rooms.Encode,
		Files:  []string{"testdata/castle.rooms.yaml"},
	})
}
