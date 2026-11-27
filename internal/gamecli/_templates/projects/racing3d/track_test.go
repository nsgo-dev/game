package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/vehicle3d"
)

// TestTrackFileIsCanonical checks data/track.yaml is in the form EncodeTrack writes, so a load and save changes nothing, and that data/car.yaml is in the form vehicle3d.EncodeConfig writes.
func TestTrackFileIsCanonical(t *testing.T) {
	track, cfg, err := LoadRace()
	if err != nil {
		t.Fatal(err)
	}
	out, err := EncodeTrack(track)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, trackYAML) {
		t.Fatalf("data/track.yaml is not canonical; EncodeTrack writes:\n%s", out)
	}
	carOut, err := vehicle3d.EncodeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(carOut, carYAML) {
		t.Fatalf("data/car.yaml is not canonical; vehicle3d.EncodeConfig writes:\n%s", carOut)
	}
}

// TestParseTrackRefuses checks the file errors a person editing the track can make are refused with the field named.
func TestParseTrackRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, from, to, want string
	}{
		{"unknown field", "laps: 2\n", "laps: 2\nturbo: true\n", "turbo"},
		{"version", "version: 1\n", "version: 2\n", "version"},
		{"no laps", "laps: 2\n", "laps: 0\n", "laps"},
		{"narrow road", "width: 12\n", "width: 2\n", "width"},
		{"repeated point", "  - {x: 0, z: -15}\n", "  - {x: 0, z: 30}\n", "repeats"},
		{"point off the ground", "  - {x: 64, z: -15}\n", "  - {x: 301, z: -15}\n", "within 300"},
		{"negative start", "start: 12\n", "start: -1\n", "non-negative"},
		{"one checkpoint", "checkpoints: 6\n", "checkpoints: 1\n", "checkpoints"},
		{"player's name", "name: Mika\n", "name: You\n", "name"},
		{"pace", "pace: 0.92\n", "pace: 1.5\n", "pace"},
		{"lane", "lane: -1.5\n", "lane: -5\n", "lane"},
		{"color", "color: [0.85, 0.3, 0.08]\n", "color: [2, 0.3, 0.08]\n", "color"},
		{"second document", "lane: 1.5\n", "lane: 1.5\n---\nversion: 1\n", "trailing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !bytes.Contains(trackYAML, []byte(tc.from)) {
				t.Fatalf("data/track.yaml has no %q to change", tc.from)
			}
			data := bytes.Replace(trackYAML, []byte(tc.from), []byte(tc.to), 1)
			_, err := ParseTrack(data)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseTrack = %v, want an error naming %q", err, tc.want)
			}
		})
	}
	if _, err := ParseTrack([]byte("  \n")); err == nil {
		t.Fatal("an empty file parsed")
	}
}

// TestCourseGeometry checks the course built from the track: samples a couple of meters apart around the whole loop, checkpoints spread along it, the grid behind the start line, and a racing line slower in the corners than on the straights.
func TestCourseGeometry(t *testing.T) {
	track, cfg, err := LoadRace()
	if err != nil {
		t.Fatal(err)
	}
	c := newCourse(track, cfg.MaxSpeed)
	if c.length < 200 || c.length > 600 {
		t.Fatalf("loop length %v m, want a short circuit", c.length)
	}
	if len(c.samples)%2 != 0 || c.spacing > sampleSpacing {
		t.Fatalf("%d samples %v m apart", len(c.samples), c.spacing)
	}
	for i, s := range c.samples {
		if d := s.pos.Dist(c.samples[c.wrap(i+1)].pos); d > 2*c.spacing {
			t.Fatalf("samples %d and %d are %v m apart", i, i+1, d)
		}
	}
	gap := c.length / float32(track.Checkpoints)
	for i := range track.Checkpoints {
		if d := c.checkpoint(i).Dist(c.checkpoint((i + 1) % track.Checkpoints)); d < gap/3 {
			t.Fatalf("checkpoints %d and %d are %v m apart", i, i+1, d)
		}
	}
	line := c.at(track.Start)
	for i := range len(track.Rivals) + 1 {
		pos, fwd := c.slot(i)
		if ahead := pos.Sub(line.pos).Dot(line.forward); ahead >= 0 {
			t.Fatalf("grid slot %d is %v m past the start line", i, ahead)
		}
		if fwd.Dot(line.forward) < 0.9 {
			t.Fatalf("grid slot %d faces %v, the start line %v", i, fwd, line.forward)
		}
	}
	slowest, fastest := cfg.MaxSpeed, float32(0)
	for _, s := range c.samples {
		slowest, fastest = min(slowest, s.speed), max(fastest, s.speed)
	}
	if slowest < 5 || fastest < 1.5*slowest {
		t.Fatalf("racing line speeds %v to %v m/s, want slower corners than straights", slowest, fastest)
	}
}

// FuzzParseTrack checks ParseTrack never panics and that whatever it accepts encodes and parses back the same.
func FuzzParseTrack(f *testing.F) {
	f.Add(trackYAML)
	f.Add([]byte("version: 1\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		tr, err := ParseTrack(data)
		if err != nil {
			return
		}
		out, err := EncodeTrack(tr)
		if err != nil {
			t.Fatalf("EncodeTrack of a parsed track: %v", err)
		}
		again, err := ParseTrack(out)
		if err != nil {
			t.Fatalf("ParseTrack of EncodeTrack's output: %v\n%s", err, out)
		}
		if out2, _ := EncodeTrack(again); !bytes.Equal(out, out2) {
			t.Fatalf("the round trip changed the track:\n%s\n%s", out, out2)
		}
	})
}
