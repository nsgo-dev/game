package bullets_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/bullets"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\npatterns:\n  - name: ring\n    kind: ring\n    count: 8\n    speed: 100\n    color: red\n")
		_, err := bullets.Parse(data)
		loaderrtest.Expect(t, err, "", 7)
	})

	t.Run("duplicate pattern name", func(t *testing.T) {
		data := []byte("version: 1\npatterns:\n  - name: ring\n    kind: ring\n    count: 8\n    speed: 100\n    shots: 1\n  - name: ring\n    kind: ring\n    count: 4\n    speed: 100\n    shots: 1\n")
		_, err := bullets.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "patterns[1].name")
	})

	t.Run("invalid speed", func(t *testing.T) {
		data := []byte("version: 1\npatterns:\n  - name: ring\n    kind: ring\n    count: 8\n    speed: -5\n")
		_, err := bullets.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "patterns[0].speed")
	})
}
