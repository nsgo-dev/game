package hotreload_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/tooling/hotreload"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*hotreload.MaterialDef]{
		Decode: hotreload.ParseMaterial,
		Encode: hotreload.EncodeMaterial,
		Files:  []string{"testdata/crate.material.yaml"},
		Doc:    "tooling/hotreload",
	})
}
