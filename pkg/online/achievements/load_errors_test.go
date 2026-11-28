package achievements_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/online/achievements"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nachievements:\n  - id: first_win\n    extra: 1\n")
		_, err := achievements.Parse(data)
		loaderrtest.Expect(t, err, "", 4)
	})

	t.Run("duplicate achievement id", func(t *testing.T) {
		data := []byte("version: 1\nachievements:\n  - id: first_win\n  - id: first_win\n")
		_, err := achievements.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "achievements[1].id")
	})

	t.Run("progress unknown service", func(t *testing.T) {
		data := []byte("version: 1\nachievements:\n  - id: first_win\n    steps: 1\n    reported:\n      xbox: 1\n")
		_, err := achievements.ParseProgress(data)
		loaderrtest.ExpectField(t, err, "", data, "achievements[0].reported.xbox")
	})
}
