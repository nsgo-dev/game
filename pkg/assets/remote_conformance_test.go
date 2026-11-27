package assets

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
)

func TestRemoteManifestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*RemoteManifest]{
		Decode: ParseRemoteManifest,
		Encode: EncodeRemoteManifest,
		Files:  []string{"testdata/remote.yaml"},
		Doc:    "assets#remote",
	})
}

func TestRemoteCacheIndexFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*remoteCacheIndex]{
		Decode: parseRemoteCacheIndex,
		Encode: encodeRemoteCacheIndex,
		Files:  []string{"testdata/remote-cache-index.yaml"},
		Doc:    "assets#remote-cache",
	})
}
