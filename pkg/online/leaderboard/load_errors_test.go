package leaderboard_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/online/leaderboard"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nleaderboards:\n  - name: high-score\n    gameCenter: gc.score\n    steam: st.score\n")
		_, err := leaderboard.Parse(data)
		loaderrtest.Expect(t, err, "", 5)
	})

	t.Run("duplicate board name", func(t *testing.T) {
		data := []byte("version: 1\nleaderboards:\n  - name: high-score\n    gameCenter: gc.1\n  - name: high-score\n    gameCenter: gc.2\n")
		_, err := leaderboard.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "leaderboards[1].name")
	})

	t.Run("invalid miniGame order", func(t *testing.T) {
		data := []byte("version: 1\nleaderboards:\n  - name: high-score\n    miniGame:\n      key: best\n      order: highest\n")
		_, err := leaderboard.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "leaderboards[0].miniGame.order")
	})
}
