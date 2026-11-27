package cutscene_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/cutscene"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nsource: intro.mp4\nloop: true\n")
		_, err := cutscene.Parse(data)
		loaderrtest.Expect(t, err, "", 3)
	})

	t.Run("unknown fit", func(t *testing.T) {
		data := []byte("version: 1\nsource: intro.mp4\nfit: zoom\n")
		_, err := cutscene.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "fit")
	})

	t.Run("unknown track kind", func(t *testing.T) {
		data := []byte("version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: lights\n")
		_, err := cutscene.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "tracks[0].kind")
	})
}
