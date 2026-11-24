package ability_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/ability"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*ability.Set]{
		Decode: ability.Parse,
		Encode: ability.Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "ability",
	})
}
