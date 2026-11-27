package playtest_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/playtest"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("config unknown field", func(t *testing.T) {
		data := []byte("version: 1\nextra: true\nscope:\n  scene: level_01\nruns:\n  count: 1\nbot:\n  profile: shooter\n")
		_, err := playtest.ParseConfig(data)
		loaderrtest.Expect(t, err, "", 2)
	})

	t.Run("config unsupported version", func(t *testing.T) {
		data := []byte("version: 2\nscope:\n  scene: level_01\nruns:\n  count: 1\nbot:\n  profile: shooter\n")
		_, err := playtest.ParseConfig(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})

	t.Run("report unsupported version", func(t *testing.T) {
		data := []byte("version: 2\nscope:\n  scene: level_01\nbot:\n  profile: shooter\nrate: 60\nseedCount: 0\npassed: true\nruns: []\n")
		_, err := playtest.ParseReport(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})
}
