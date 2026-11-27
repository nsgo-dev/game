package liveops_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/online/liveops"
)

func TestParseRemindersLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nextra: true\n")
		_, err := liveops.ParseReminders(data)
		loaderrtest.Expect(t, err, "", 2)
	})

	t.Run("unsupported version", func(t *testing.T) {
		data := []byte("version: 2\n")
		_, err := liveops.ParseReminders(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})
}
