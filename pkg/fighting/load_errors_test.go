package fighting_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/fighting"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\ninput:\n  up: up\n  down: down\n  left: left\n  right: right\nmoves:\n  - name: lp\n    press:\n      - lp\n    speed: 3\n")
		_, err := fighting.Parse(data)
		loaderrtest.Expect(t, err, "", 11)
	})

	t.Run("duplicate move name", func(t *testing.T) {
		data := []byte("version: 1\ninput:\n  up: up\n  down: down\n  left: left\n  right: right\nmoves:\n  - name: lp\n    press:\n      - lp\n  - name: lp\n    press:\n      - hp\n")
		_, err := fighting.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "moves[1].name")
	})

	t.Run("LoadFile missing file names path", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "moves.yaml")
		_, err := fighting.LoadFile(missing)
		loaderrtest.Expect(t, err, missing, 0)
	})
}
