package accounts_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/online/accounts"
)

func TestParseConfigLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nissuer: https://accounts.example.com\naudience: game\ntokenTTL: 15m\nextra: 1\npassword:\n  minLength: 10\n  argon2id:\n    time: 2\n    memoryKiB: 19456\n    threads: 1\n")
		_, err := accounts.ParseConfig(data)
		loaderrtest.Expect(t, err, "", 5)
	})

	t.Run("invalid minLength", func(t *testing.T) {
		data := []byte("version: 1\nissuer: https://accounts.example.com\naudience: game\ntokenTTL: 15m\npassword:\n  minLength: 4\n  argon2id:\n    time: 2\n    memoryKiB: 19456\n    threads: 1\n")
		_, err := accounts.ParseConfig(data)
		loaderrtest.ExpectField(t, err, "", data, "password.minLength")
	})
}
