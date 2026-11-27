package world3d_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/world3d"
)

func TestTerrainFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*world3d.TerrainDoc]{
		Decode: world3d.ParseTerrain,
		Encode: world3d.EncodeTerrain,
		Files:  []string{"testdata/*.terrain.yaml"},
		Doc:    "world3d#terrain",
	})
}

func TestGridMapFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*world3d.GridMapDoc]{
		Decode: world3d.ParseGridMap,
		Encode: world3d.EncodeGridMap,
		Files:  []string{"testdata/*.gridmap.yaml"},
		Doc:    "world3d#gridmap",
	})
}

func TestFoliageFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*world3d.FoliageDoc]{
		Decode: world3d.ParseFoliage,
		Encode: world3d.EncodeFoliage,
		Files:  []string{"testdata/*.foliage.yaml"},
		Doc:    "world3d#foliage",
	})
}
