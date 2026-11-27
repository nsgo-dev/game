package selfhost_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost"
)

func TestParseConfigLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nextra: true\nboards:\n  - name: high-score\n    order: descending\n    reset: allTime\n")
		_, err := selfhost.ParseConfig(data)
		loaderrtest.Expect(t, err, "", 2)
	})

	t.Run("invalid order", func(t *testing.T) {
		data := []byte("version: 1\nboards:\n  - name: high-score\n    order: sideways\n    reset: allTime\n")
		_, err := selfhost.ParseConfig(data)
		loaderrtest.ExpectField(t, err, "", data, "boards[0].order")
	})
}
