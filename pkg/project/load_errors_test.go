package project_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/project"
)

func TestParseLoadErrors(t *testing.T) {
	t.Run("project unknown field", func(t *testing.T) {
		data := []byte("version: 1\nname: Demo\nextra: true\n")
		_, err := project.Parse(data)
		loaderrtest.Expect(t, err, "", 3)
	})

	t.Run("project unsupported version", func(t *testing.T) {
		data := []byte("version: 2\nname: Demo\n")
		_, err := project.Parse(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})

	t.Run("brief unsupported version", func(t *testing.T) {
		data := []byte("version: 2\n")
		_, err := project.ParseBrief(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})
}
