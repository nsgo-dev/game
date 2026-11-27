package ability_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/ability"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nabilities:\n  - id: dash\n    colldown: 1s\n")
		_, err := ability.Parse(data)
		loaderrtest.Expect(t, err, "", 4)
	})

	t.Run("duplicate id", func(t *testing.T) {
		data := []byte("version: 1\nabilities:\n  - id: dash\n  - id: dash\n")
		_, err := ability.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "abilities[1].id")
	})

	t.Run("negative cooldown", func(t *testing.T) {
		data := []byte("version: 1\nabilities:\n  - id: dash\n    cooldown: -1s\n")
		_, err := ability.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "abilities[0].cooldown")
	})
}
