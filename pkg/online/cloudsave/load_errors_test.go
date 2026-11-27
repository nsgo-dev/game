package cloudsave_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/online/cloudsave"
)

func TestParseConfigLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nmaxSlotBytes: 1024\nmaxSlots: 4\nkeepVersions: 2\nextra: 1\n")
		_, err := cloudsave.ParseConfig(data)
		loaderrtest.Expect(t, err, "", 5)
	})

	t.Run("invalid maxSlots", func(t *testing.T) {
		data := []byte("version: 1\nmaxSlotBytes: 1024\nmaxSlots: 0\nkeepVersions: 2\n")
		_, err := cloudsave.ParseConfig(data)
		loaderrtest.ExpectField(t, err, "", data, "maxSlots")
	})
}
