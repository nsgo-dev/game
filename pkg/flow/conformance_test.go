package flow_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/flow"
)

func TestVarsFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*flow.VarsDoc]{
		Decode: flow.ParseVars,
		Encode: flow.EncodeVars,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "flow#vars",
	})
}
