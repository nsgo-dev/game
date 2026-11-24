package loot_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/loot"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*loot.Document]{
		Decode: loot.Parse,
		Encode: loot.Encode,
		Files:  []string{"testdata/chest.yaml"},
	})
}
