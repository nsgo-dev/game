package playtest_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/playtest"
)

func TestFormatConformance(t *testing.T) {
	t.Run("config", func(t *testing.T) {
		formattest.Run(t, formattest.Format[*playtest.Config]{
			Decode: playtest.ParseConfig,
			Encode: playtest.EncodeConfig,
			Files:  []string{"testdata/*.playtest.yaml"},
			Doc:    "playtest",
		})
	})
	t.Run("report", func(t *testing.T) {
		formattest.Run(t, formattest.Format[*playtest.Report]{
			Decode: playtest.ParseReport,
			Encode: playtest.EncodeReport,
			Files:  []string{"testdata/*.report.yaml"},
			Doc:    "playtest#report",
		})
	})
}
