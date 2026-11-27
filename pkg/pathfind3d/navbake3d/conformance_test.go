package navbake3d_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/pathfind3d/navbake3d"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*navbake3d.File]{
		Decode: navbake3d.Parse,
		Encode: navbake3d.Encode,
		Files:  []string{"testdata/*.navmesh.yaml"},
		Doc:    "pathfind3d/navbake3d",
	})
}
