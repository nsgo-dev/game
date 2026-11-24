package importsettings_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*importsettings.File]{
		Decode: importsettings.Parse,
		Encode: importsettings.Encode,
		Files:  []string{"testdata/*.import.yaml"},
		Doc:    "tooling/importsettings",
	})
}
