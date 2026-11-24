package assetbuild

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestCacheFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*cache]{
		Decode: parseCache,
		Encode: encodeCache,
		Files:  []string{"testdata/cache.yaml"},
	})
}
