package voxel_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/voxel"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*voxel.WorldDoc]{
		Decode: voxel.Parse,
		Encode: voxel.Encode,
		Files:  []string{"testdata/hill.voxel.yaml"},
		Doc:    "voxel",
	})
}
