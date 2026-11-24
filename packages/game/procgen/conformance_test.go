package procgen_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/procgen"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*procgen.Rules]{
		Decode: procgen.ParseRules,
		Encode: procgen.EncodeRules,
		Files:  []string{"testdata/*.yaml"},
	})
}
