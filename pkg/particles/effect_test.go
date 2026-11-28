package particles_test

import (
	"github.com/nsgo-dev/game/pkg/particles"
	"testing"
)

func TestEffectRoundTripAndParams(t *testing.T) {
	e, err := particles.ParseEffect([]byte("version: 1\nemitters:\n  - config: {}\nparams:\n  speed: {type: float, float: 1}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetParam("speed", 2.0); err != nil {
		t.Fatal(err)
	}
	b, err := particles.EncodeEffect(e)
	if err != nil || len(b) == 0 {
		t.Fatal(err)
	}
}
