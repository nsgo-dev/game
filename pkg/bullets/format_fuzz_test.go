package bullets_test

import (
	"bytes"
	"testing"

	"github.com/nsgo-dev/game/pkg/bullets"
)

func FuzzParse(f *testing.F) {
	f.Add(readFixture(f))
	for _, s := range []string{
		"",
		"version: 1\n",
		"version: 1\npatterns: []\n",
		"version: 2\npatterns: []\n",
		"version: 1\npatterns:\n  - name: a\n    kind: ring\n    count: 3\n    speed: 5\n    shots: 1\n",
		"version: 1\npatterns:\n  - name: a\n    kind: aimed\n    count: 3\n    speed: 5\n    spread: 360\n    shots: 0\n    interval: 1ms\n",
		"version: 1\npatterns:\n  - name: a\n    kind: spiral\n    count: 1\n    speed: -0\n    spin: 1e-45\n    shots: 2\n    interval: 1h\n",
		"version: 1\npatterns:\n  - {name: a, kind: ring, count: 1, speed: .nan, shots: 1}\n",
		"[[[",
		"null",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		file, err := bullets.Parse(data)
		if err != nil {
			return
		}
		out, err := bullets.Encode(file)
		if err != nil {
			t.Fatalf("Encode of a parsed file failed: %v", err)
		}
		again, err := bullets.Parse(out)
		if err != nil {
			t.Fatalf("Parse of an encoded file failed: %v\n%s", err, out)
		}
		out2, err := bullets.Encode(again)
		if err != nil {
			t.Fatalf("second Encode failed: %v", err)
		}
		if !bytes.Equal(out, out2) {
			t.Fatalf("round trip changed the file:\n%s\nthen:\n%s", out, out2)
		}
	})
}
