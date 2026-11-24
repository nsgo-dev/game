package rhythm_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/rhythm"
)

func TestFormatConformance(t *testing.T) {
	t.Run("beatmap", func(t *testing.T) {
		formattest.Run(t, formattest.Format[*rhythm.BeatMap]{
			Decode: rhythm.ParseBeatMap,
			Encode: rhythm.EncodeBeatMap,
			Files:  []string{"testdata/*.beatmap.yaml"},
			Doc:    "rhythm",
		})
	})
	t.Run("calibrations", func(t *testing.T) {
		formattest.Run(t, formattest.Format[*rhythm.Calibrations]{
			Decode: rhythm.ParseCalibrations,
			Encode: rhythm.EncodeCalibrations,
			Files:  []string{"testdata/calibrations.yaml"},
			Doc:    "rhythm#calibrations",
		})
	})
}
