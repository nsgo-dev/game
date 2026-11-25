package navbake3d_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/navbake3d"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*navbake3d.File]{
		Decode: navbake3d.Parse,
		Encode: navbake3d.Encode,
		Files:  []string{"testdata/*.navmesh.yaml"},
		Doc:    "navbake3d",
	})
}
