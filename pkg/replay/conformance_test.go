package replay_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/replay"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*replay.Recording]{
		Decode: replay.Decode,
		Encode: (*replay.Recording).Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "replay",
	})
}
