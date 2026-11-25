package world3d_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

func TestTerrainFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*world3d.TerrainDoc]{
		Decode: world3d.ParseTerrain,
		Encode: world3d.EncodeTerrain,
		Files:  []string{"testdata/alpine.terrain.yaml"},
		Doc:    "world3d#terrain",
	})
}
