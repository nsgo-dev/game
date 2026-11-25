package animclip_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*animclip.StateMachineDoc]{
		Decode: animclip.ParseStateMachine,
		Encode: animclip.EncodeStateMachine,
		Files:  []string{"testdata/*.states.yaml"},
		Doc:    "animclip",
	})
}
