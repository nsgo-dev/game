package importsettings_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/tooling/importsettings"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*importsettings.File]{
		Decode: importsettings.Parse,
		Encode: importsettings.Encode,
		Files:  []string{"testdata/*.import.yaml"},
		Doc:    "tooling/importsettings",
	})
}

func TestAssetSourceFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*importsettings.AssetSource]{
		Decode: importsettings.ParseAssetSource,
		Encode: importsettings.EncodeAssetSource,
		Files:  []string{"testdata/*.source.yaml"},
		Doc:    "tooling/importsettings#source",
	})
}
