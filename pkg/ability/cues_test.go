package ability_test

import (
	"github.com/nsgo-dev/game/pkg/ability"
	"testing"
)

func TestCuesRoundTripAndStrictValidation(t *testing.T) {
	raw := []byte("version: 1\ncues:\n  hit.fire:\n    particles: fx/hit.yaml\n    sound: audio/hit.wav\n    once: true\n")
	c, err := ability.ParseCues(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := ability.EncodeCues(c)
	if err != nil || len(out) == 0 {
		t.Fatalf("out=%s err=%v", out, err)
	}
	if _, err := ability.ParseCues([]byte("version: 1\ncues:\n  hit.fire:\n    unknown: x\n")); err == nil {
		t.Fatal("unknown field accepted")
	}
}
