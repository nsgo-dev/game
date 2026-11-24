package rts_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/rts"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*rts.Formations]{
		Decode: rts.ParseFormations,
		Encode: (*rts.Formations).Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "rts",
	})
}
