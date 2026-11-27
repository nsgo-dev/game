package animclip_test

import (
	"errors"
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/animclip"
)

func TestParseStateMachineLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nname: Knight\nentry: idle\nstates:\n  - name: idle\n    clip: idle.anim.yaml\n    extra: true\n")
		_, err := animclip.ParseStateMachine(data)
		loaderrtest.Expect(t, err, "", 7)
	})

	t.Run("unknown entry state preserves StateMachineError", func(t *testing.T) {
		data := []byte("version: 1\nname: Knight\nentry: missing\nstates:\n  - name: idle\n    clip: idle.anim.yaml\n")
		_, err := animclip.ParseStateMachine(data)
		loaderrtest.ExpectField(t, err, "", data, "entry")
		var smErr *animclip.StateMachineError
		if !errors.As(err, &smErr) {
			t.Errorf("errors.As(err, &smErr) = false (err: %v)", err)
		}
	})
}
