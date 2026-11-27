package sceneformat_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

func TestParseConflictLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nextra: true\nconflicts:\n  - kind: field\n    scope: scene\n")
		_, err := sceneformat.ParseConflict(data)
		loaderrtest.Expect(t, err, "", 2)
	})

	t.Run("invalid kind", func(t *testing.T) {
		data := []byte("version: 1\nconflicts:\n  - kind: bogus\n    scope: scene\n")
		_, err := sceneformat.ParseConflict(data)
		loaderrtest.ExpectField(t, err, "", data, "conflicts[0].kind")
	})
}
