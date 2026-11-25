package l10n_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/l10n"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*l10n.AssetManifest]{
		Decode: l10n.ParseAssetManifest,
		Encode: l10n.EncodeAssetManifest,
		Files:  []string{"testdata/assets.yaml"},
		Doc:    "l10n",
	})
	formattest.Run(t, formattest.Format[*l10n.StringTable]{
		Decode: l10n.ParseYAML,
		Encode: l10n.EncodeYAML,
		Files:  []string{"testdata/en.yaml"},
		Doc:    "l10n#strings",
	})
}
