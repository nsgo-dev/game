package wire_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\npackage: netmsg\nextra: 1\nmessages:\n  - name: Ping\n    fields:\n      - name: seq\n        type: u32\n")
		_, err := wire.Parse(data)
		loaderrtest.Expect(t, err, "", 3)
	})

	t.Run("unsupported version", func(t *testing.T) {
		data := []byte("version: 2\npackage: netmsg\nmessages:\n  - name: Ping\n    fields:\n      - name: seq\n        type: u32\n")
		_, err := wire.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})
}
