package tilemap_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/tilemap"
)

func TestChunkFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*tilemap.ChunkDoc]{
		Decode: tilemap.DecodeChunkYAML,
		Encode: tilemap.EncodeChunkYAML,
		Files:  []string{"testdata/sample.chunk.yaml"},
		Doc:    "tilemap#chunk",
	})
}
