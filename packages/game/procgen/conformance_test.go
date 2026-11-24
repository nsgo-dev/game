package procgen_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/procgen"
)

// TestFormatConformance checks the rules format on testdata/canonical, which holds fixtures in the bytes EncodeRules writes; testdata/ruins.yaml keeps its hand-written comment and flow style for the parser tests and fuzz seeds.
func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*procgen.Rules]{
		Decode: procgen.ParseRules,
		Encode: procgen.EncodeRules,
		Files:  []string{"testdata/canonical/*.yaml"},
	})
}
