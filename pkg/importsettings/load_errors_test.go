package importsettings_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/importsettings"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("sidecar unknown field", func(t *testing.T) {
		data := []byte("version: 1\nimage:\n  filter: nearest\n  extra: true\n")
		_, err := importsettings.Parse(data)
		loaderrtest.Expect(t, err, "", 4)
	})

	t.Run("sidecar invalid filter", func(t *testing.T) {
		data := []byte("version: 1\nimage:\n  filter: cubic\n")
		_, err := importsettings.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "image.filter")
	})

	t.Run("asset source missing prompt", func(t *testing.T) {
		data := []byte("version: 1\ngenerator: pixel-gen\nprompt: \"\"\n")
		_, err := importsettings.ParseAssetSource(data)
		loaderrtest.ExpectField(t, err, "", data, "prompt")
	})

	t.Run("font charsFrom duplicate", func(t *testing.T) {
		data := []byte("version: 1\nfont:\n  charsFrom:\n    - l10n/en.yaml\n    - l10n/en.yaml\n")
		_, err := importsettings.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "font.charsFrom")
	})

	t.Run("font charsFile invalid", func(t *testing.T) {
		data := []byte("version: 1\nfont:\n  charsFile: ../escape.txt\n")
		_, err := importsettings.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "font.charsFile")
	})
}
