package library_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/tooling/library"
)

func TestFormatConformance(t *testing.T) {
	t.Run("manifest", func(t *testing.T) {
		formattest.Run(t, formattest.Format[*library.Manifest]{
			Decode: library.ParseManifest,
			Encode: library.EncodeManifest,
			Files:  []string{"testdata/pack.yaml"},
		})
	})
	t.Run("index", func(t *testing.T) {
		formattest.Run(t, formattest.Format[*library.Index]{
			Decode: library.ParseIndex,
			Encode: library.EncodeIndex,
			Files:  []string{"testdata/index.yaml"},
		})
	})
}
