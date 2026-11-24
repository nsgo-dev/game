package modding_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/modding"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[modding.ModManifest]{
		Decode:       modding.ParseManifest,
		Encode:       modding.EncodeManifest,
		Files:        []string{"testdata/mods/*/mod.yaml"},
		Doc:          "modding",
		VersionField: "manifestVersion",
	})
}
