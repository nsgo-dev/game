package cues_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/cues"
)

func sample() []byte {
	return []byte("version: 1\ncues:\n  - tag: cue.hit.fire\n    once: true\n    particles:\n      file: fx/hit.yaml\n      attach: chest\n    sound:\n      file: sfx/hit.ogg\n    shake:\n      amplitude: 0.5\n      duration: 100ms\n  - tag: cue.status.burning\n    whileActive: true\n    animation: burning\n")
}

func TestParseEncodeRoundTrip(t *testing.T) {
	set, err := cues.Parse(sample())
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Cues) != 2 || set.Cues[0].Shake == nil || time.Duration(set.Cues[0].Shake.Duration) != 100*time.Millisecond {
		t.Fatalf("set = %+v", set)
	}
	out, err := cues.Encode(set)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, sample()) {
		t.Fatalf("canonical output changed:\n%s", out)
	}
	again, err := cues.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set, again) {
		t.Fatalf("round trip changed set: %#v %#v", set, again)
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	cases := []struct{ name, doc, want string }{
		{"version", "version: 2\ncues: []\n", "unsupported version"},
		{"unknown", "version: 1\ncues:\n- tag: cue.x\n  nope: true\n", "unknown field"},
		{"bad tag", "version: 1\ncues:\n- tag: cue x\n  once: true\n", "dotted tag"},
		{"no mode", "version: 1\ncues:\n- tag: cue.x\n", "once or whileActive"},
		{"duplicate", "version: 1\ncues:\n- {tag: cue.x, once: true}\n- {tag: cue.x, once: true}\n", "duplicate"},
		{"bad duration", "version: 1\ncues:\n- tag: cue.x\n  shake: {amplitude: 1, duration: nope}\n  once: true\n", "duration"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cues.Parse([]byte(tc.doc))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
