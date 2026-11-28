package authtoken_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/online/accounts/authtoken"
)

func TestParseKeySetLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nissuer: https://accounts.example.com\naudience: game\nextra: 1\nkeys: []\n")
		_, err := authtoken.ParseKeySet(data)
		loaderrtest.Expect(t, err, "", 4)
	})

	t.Run("empty keys", func(t *testing.T) {
		data := []byte("version: 1\nissuer: https://accounts.example.com\naudience: game\nkeys: []\n")
		_, err := authtoken.ParseKeySet(data)
		loaderrtest.ExpectField(t, err, "", data, "keys")
	})
}
