package stats_test

import (
	"errors"
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/stats"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nstats:\n  - name: hp\n    base: 100\n    color: red\n")
		_, err := stats.Parse(data)
		loaderrtest.Expect(t, err, "", 5)
	})

	t.Run("duplicate stat preserves sentinel", func(t *testing.T) {
		data := []byte("version: 1\nstats:\n  - name: hp\n    base: 100\n  - name: hp\n    base: 50\n")
		_, err := stats.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "stats[1].name")
		if !errors.Is(err, stats.ErrDuplicateStat) {
			t.Errorf("errors.Is(err, ErrDuplicateStat) = false (err: %v)", err)
		}
	})

	t.Run("unknown stat in modifier preserves sentinel", func(t *testing.T) {
		data := []byte("version: 1\nstats:\n  - name: hp\n    base: 100\neffects:\n  - name: buff\n    modifiers:\n      - stat: mana\n        op: add\n        value: 10\n")
		_, err := stats.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "effects[0].modifiers[0].stat")
		if !errors.Is(err, stats.ErrUnknownStat) {
			t.Errorf("errors.Is(err, ErrUnknownStat) = false (err: %v)", err)
		}
	})
}
