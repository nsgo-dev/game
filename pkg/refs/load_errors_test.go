package refs_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/refs"
)

func TestParseIndexLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nextra: 1\n")
		_, err := refs.ParseIndex(data)
		loaderrtest.Expect(t, err, "", 2)
	})

	t.Run("duplicate uid", func(t *testing.T) {
		data := []byte("version: 1\nfiles:\n  - path: a.yaml\n    uid: 0000000000000\n  - path: b.yaml\n    uid: 0000000000000\n")
		_, err := refs.ParseIndex(data)
		loaderrtest.ExpectField(t, err, "", data, "files[1].uid")
	})
}
