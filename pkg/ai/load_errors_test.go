package ai_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/ai"
)

func TestParseYAMLLoadErrors(t *testing.T) {
	t.Run("GOAP unknown field", func(t *testing.T) {
		data := []byte("version: 1\nextra: 1\nfacts:\n  bools: [armed]\nactions: []\n")
		_, err := ai.ParseGOAP(data)
		loaderrtest.Expect(t, err, "", 2)
	})

	t.Run("GOAP unknown fact reference", func(t *testing.T) {
		data := []byte("version: 1\nfacts:\n  bools: [armed]\nactions:\n  - name: shoot\n    preconditions:\n      bools:\n        - fact: missing\n          value: true\n")
		_, err := ai.ParseGOAP(data)
		loaderrtest.ExpectField(t, err, "", data, "actions[0].preconditions.bools[0].fact")
	})

	t.Run("Utility unknown curve kind", func(t *testing.T) {
		data := []byte("version: 1\nactions:\n  - name: attack\n    considerations:\n      - input: dist\n        curve:\n          kind: spline\n")
		_, err := ai.ParseUtility(data)
		loaderrtest.ExpectField(t, err, "", data, "actions[0].considerations[0].curve.kind")
	})
}
