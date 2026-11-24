package idle_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/idle"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*idle.Locale]{
		Decode: idle.ParseLocale,
		Encode: idle.EncodeLocale,
		Files:  []string{"testdata/de.yaml"},
	})
}
