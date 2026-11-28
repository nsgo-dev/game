package storecapture_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/tooling/storecapture"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*storecapture.Plan]{
		Decode: storecapture.Decode,
		Encode: (*storecapture.Plan).Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "tooling/storecapture",
	})
}
