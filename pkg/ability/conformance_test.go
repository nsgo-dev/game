package ability_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/ability"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*ability.Set]{
		Decode: ability.Parse,
		Encode: ability.Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "ability",
	})
}
