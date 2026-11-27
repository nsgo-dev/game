package idle_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/idle"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*idle.Locale]{
		Decode: idle.ParseLocale,
		Encode: idle.EncodeLocale,
		Files:  []string{"testdata/de.yaml"},
		Doc:    "idle",
	})
}
