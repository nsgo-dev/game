package idle_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/idle"
)

func TestParseLocaleLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nname: en\ndecimal: \".\"\ngroup: \",\"\nextra: true\n")
		_, err := idle.ParseLocale(data)
		loaderrtest.Expect(t, err, "", 5)
	})

	t.Run("unsupported version", func(t *testing.T) {
		data := []byte("version: 2\nname: en\ndecimal: \".\"\ngroup: \",\"\n")
		_, err := idle.ParseLocale(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})
}
