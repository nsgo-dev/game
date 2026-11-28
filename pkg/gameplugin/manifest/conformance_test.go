package manifest_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/gameplugin/manifest"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[manifest.Manifest]{
		Decode:       manifest.Parse,
		Encode:       manifest.Encode,
		Files:        []string{"testdata/*.yaml"},
		VersionField: "manifestVersion",
		Doc:          "gameplugin/manifest",
	})
}
