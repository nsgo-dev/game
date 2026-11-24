package statedump_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/statedump"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*statedump.Dump]{
		Decode: statedump.Parse,
		Encode: statedump.Encode,
		Files:  []string{"testdata/*.yaml"},
	})
}
