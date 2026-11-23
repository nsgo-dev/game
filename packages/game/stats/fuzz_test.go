package stats_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/nsgo-dev/game/packages/game/stats"
)

// FuzzParse checks that Parse never panics and that anything it accepts encodes to a fixed point: encoding, parsing and encoding again gives the same bytes.
func FuzzParse(f *testing.F) {
	if data, err := os.ReadFile("testdata/hero.yaml"); err == nil {
		f.Add(data)
	}
	f.Add([]byte("version: 1\nstats:\n  - name: a\n    base: 1\n"))
	f.Add([]byte("version: 1\nstats:\n  - name: a\n    base: 1e300\n    min: -0\neffects:\n  - name: e\n    steps: 2\n    modifiers:\n      - stat: a\n        op: mul\n        value: 0.1\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		file, err := stats.Parse(data)
		if err != nil {
			return
		}
		first, err := stats.Encode(file)
		if err != nil {
			t.Fatalf("Encode of a parsed file: %v", err)
		}
		again, err := stats.Parse(first)
		if err != nil {
			t.Fatalf("Parse of encoded file: %v\n%s", err, first)
		}
		second, err := stats.Encode(again)
		if err != nil {
			t.Fatalf("second Encode: %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("encoding is not a fixed point:\n%s\n---\n%s", first, second)
		}
		if _, err := file.NewSheet(); err != nil {
			t.Fatalf("NewSheet of a valid file: %v", err)
		}
	})
}
