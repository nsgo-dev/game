package modding_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/modding"
)

func TestFormatConformance(t *testing.T) {
	t.Run("manifest", func(t *testing.T) {
		formattest.Run(t, formattest.Format[modding.ModManifest]{
			Decode:       modding.ParseManifest,
			Encode:       modding.EncodeManifest,
			Files:        []string{"testdata/mods/*/mod.yaml"},
			Doc:          "modding",
			VersionField: "manifestVersion",
			MaxVersion:   modding.ManifestVersion,
		})
	})
	t.Run("workshop", func(t *testing.T) {
		formattest.Run(t, formattest.Format[modding.WorkshopMetadata]{
			Decode: modding.ParseWorkshopMetadata,
			Encode: modding.EncodeWorkshopMetadata,
			Files:  []string{"testdata/workshop.yaml"},
			Doc:    "modding#workshop",
		})
	})
}
