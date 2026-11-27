package framedata_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/framedata"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nmoves:\n  - name: jab\n    animation: jab\n    length: 14\n    extra: true\n")
		_, err := framedata.Parse(data)
		loaderrtest.Expect(t, err, "", 6)
	})

	t.Run("duplicate move name", func(t *testing.T) {
		data := []byte("version: 1\nmoves:\n  - name: jab\n    animation: jab\n    length: 14\n  - name: jab\n    animation: jab2\n    length: 10\n")
		_, err := framedata.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "moves[1].name")
	})

	t.Run("non-positive length", func(t *testing.T) {
		data := []byte("version: 1\nmoves:\n  - name: jab\n    animation: jab\n    length: 0\n")
		_, err := framedata.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "moves[0].length")
	})
}
