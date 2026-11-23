package bullets_test

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/bullets"
)

func readFixture(t testing.TB) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/patterns.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseFixture(t *testing.T) {
	f, err := bullets.Parse(readFixture(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(f.Patterns) != 3 {
		t.Fatalf("patterns = %d, want 3", len(f.Patterns))
	}
	volley, ok := f.Pattern("volley")
	if !ok {
		t.Fatal(`Pattern("volley") not found`)
	}
	want := bullets.Pattern{
		Name: "volley", Kind: bullets.Aimed, Frame: "dart", Count: 7, Speed: 220, Spread: 45,
		Shots: 3, Delay: time.Second, Interval: 150 * time.Millisecond, Lifetime: 3 * time.Second,
		Radius: 2.5, FaceHeading: true, Z: 2,
	}
	if *volley != want {
		t.Fatalf("volley = %+v, want %+v", *volley, want)
	}
	if _, ok := f.Pattern("missing"); ok {
		t.Fatal(`Pattern("missing") found`)
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	data := readFixture(t)
	f, err := bullets.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := bullets.Encode(f)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(out, data) {
		t.Fatalf("Encode changed the file:\n%s\nwant:\n%s", out, data)
	}
	again, err := bullets.Encode(f)
	if err != nil {
		t.Fatalf("Encode again: %v", err)
	}
	if !bytes.Equal(again, out) {
		t.Fatal("Encode is not deterministic")
	}
}

func TestParseRejects(t *testing.T) {
	const ring = "version: 1\npatterns:\n  - name: a\n    kind: ring\n    count: 4\n    speed: 10\n    shots: 1\n"
	if _, err := bullets.Parse([]byte(ring)); err != nil {
		t.Fatalf("base document rejected: %v", err)
	}
	cases := map[string]string{
		"empty":            "",
		"version":          strings.Replace(ring, "version: 1", "version: 2", 1),
		"no version":       strings.Replace(ring, "version: 1\n", "", 1),
		"unknown field":    ring + "    color: red\n",
		"unknown top":      ring + "extra: true\n",
		"trailing doc":     ring + "---\nversion: 1\n",
		"no name":          strings.Replace(ring, "name: a", "name: \"\"", 1),
		"duplicate":        ring + "  - name: a\n    kind: ring\n    count: 1\n    speed: 1\n    shots: 1\n",
		"kind":             strings.Replace(ring, "kind: ring", "kind: wave", 1),
		"count 0":          strings.Replace(ring, "count: 4", "count: 0", 1),
		"count max":        strings.Replace(ring, "count: 4", "count: 4097", 1),
		"negative speed":   strings.Replace(ring, "speed: 10", "speed: -1", 1),
		"nan speed":        strings.Replace(ring, "speed: 10", "speed: .nan", 1),
		"inf spin":         ring + "    spin: .inf\n",
		"negative radius":  ring + "    radius: -2\n",
		"ring spread":      ring + "    spread: 30\n",
		"spiral no spin":   strings.Replace(ring, "kind: ring", "kind: spiral", 1),
		"aimed no spread":  strings.Replace(ring, "kind: ring", "kind: aimed", 1),
		"aimed spread big": strings.Replace(ring, "kind: ring", "kind: aimed", 1) + "    spread: 361\n",
		"shots negative":   strings.Replace(ring, "shots: 1", "shots: -1", 1),
		"no interval":      strings.Replace(ring, "shots: 1", "shots: 3", 1),
		"endless":          strings.Replace(ring, "shots: 1", "shots: 0", 1),
		"negative delay":   ring + "    delay: -1s\n",
		"bad duration":     ring + "    lifetime: soon\n",
		"not a mapping":    "[1, 2]",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if f, err := bullets.Parse([]byte(doc)); err == nil {
				t.Fatalf("Parse accepted %q: %+v", doc, f)
			}
		})
	}
}

func TestEncodeRejectsInvalid(t *testing.T) {
	if _, err := bullets.Encode(nil); err == nil {
		t.Fatal("Encode(nil) succeeded")
	}
	f := &bullets.File{Version: bullets.FormatVersion, Patterns: []bullets.Pattern{{Name: "a", Kind: bullets.Ring}}}
	if _, err := bullets.Encode(f); err == nil {
		t.Fatal("Encode accepted a pattern with count 0")
	}
}
