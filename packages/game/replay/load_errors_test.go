package replay_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/replay"
)

func TestDecodeErrorsNameFileLineAndFix(t *testing.T) {
	const head = "version: 2\nseed: 1\nrate: 60\nactions: [a]\naxes: [x]\ntotal_steps: 5\n"
	cases := []struct {
		name, yaml, field string
	}{
		{"version", "version: 9\nseed: 1\nrate: 60\ntotal_steps: 1\n", "version"},
		{"rate", "version: 2\nseed: 1\nrate: 0\ntotal_steps: 1\n", "rate"},
		{"dup action", "version: 2\nseed: 1\nrate: 60\nactions:\n  - a\n  - a\ntotal_steps: 1\n", "actions[1]"},
		{"step missing", head + "steps:\n  - {step: 0, press: [a]}\n  - {release: [a]}\n", "steps[1]"},
		{"step order", head + "steps:\n  - {step: 2, press: [a]}\n  - {step: 2, release: [a]}\n", "steps[1].step"},
		{"unknown release", head + "steps:\n  - {step: 0, press: [a]}\n  - {step: 1, release: [a, b]}\n", "steps[1].release[1]"},
		{"press twice", head + "steps:\n  - {step: 1, press: [a, a]}\n", "steps[0].press[1]"},
		{"unknown axis", head + "steps:\n  - {step: 0, press: [a]}\n  - {step: 1, axes: {y: 1}}\n", "steps[1].axes.y"},
		{"axis nan", head + "steps:\n  - {step: 0, press: [a]}\n  - {step: 1, axes: {x: .nan}}\n", "steps[1].axes.x"},
		{"seed missing", "version: 2\nrate: 60\ntotal_steps: 1\n", ""},
	}
	dir := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.yaml)
			_, err := replay.Decode(data)
			loaderrtest.ExpectField(t, loaderr.WithFile(err, "run.replay.yaml"), "run.replay.yaml", data, tc.field)

			path := filepath.Join(dir, "run.replay.yaml")
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err = replay.LoadFile(path)
			loaderrtest.ExpectField(t, err, path, data, tc.field)
		})
	}
	lines := []struct {
		name, yaml string
		line       int
	}{
		{"type", head + "steps:\n  - {step: 0, press: [a]}\n  - {step: two}\n", 9},
		{"unknown key", head + "steps:\n  - {step: 0, hold: [a]}\n", 8},
		{"dup axis", head + "steps:\n  - {step: 0, press: [a]}\n  - {step: 1, axes: {x: 1, x: 0}}\n", 9},
		{"axes not a mapping", head + "steps:\n  - {step: 0, press: [a]}\n  - {step: 1, axes: [x]}\n", 9},
		{"trailing document", head + "---\nversion: 2\n", 8},
	}
	for _, tc := range lines {
		t.Run(tc.name, func(t *testing.T) {
			_, err := replay.Decode([]byte(tc.yaml))
			loaderrtest.Expect(t, loaderr.WithFile(err, "run.replay.yaml"), "run.replay.yaml", tc.line)
		})
	}
	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(dir, "none.replay.yaml")
		_, err := replay.LoadFile(path)
		loaderrtest.Expect(t, err, path, 0)
	})
}
