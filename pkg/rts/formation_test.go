package rts_test

import (
	"bytes"
	"math"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/rts"
)

func TestLayoutShapes(t *testing.T) {
	for _, tc := range []struct {
		f    rts.Formation
		n    int
		rows []int
	}{
		{rts.Formation{Shape: rts.ShapeLine, Spacing: 2}, 5, []int{5}},
		{rts.Formation{Shape: rts.ShapeLine, Spacing: 2, Width: 4}, 10, []int{4, 4, 2}},
		{rts.Formation{Shape: rts.ShapeColumn, Spacing: 2}, 3, []int{1, 1, 1}},
		{rts.Formation{Shape: rts.ShapeColumn, Spacing: 2, Width: 2}, 5, []int{2, 2, 1}},
		{rts.Formation{Shape: rts.ShapeBox, Spacing: 1}, 10, []int{4, 4, 2}},
		{rts.Formation{Shape: rts.ShapeWedge, Spacing: 1}, 8, []int{1, 2, 3, 2}},
	} {
		slots := tc.f.Layout(nil, tc.n)
		if len(slots) != tc.n {
			t.Fatalf("%v: %d slots, want %d", tc.f, len(slots), tc.n)
		}
		var rows []int
		for i, s := range slots {
			if s.Row >= len(rows) {
				rows = append(rows, 0)
			}
			rows[s.Row]++
			if i > 0 && slots[i-1].Row == s.Row && slots[i-1].Offset.X >= s.Offset.X {
				t.Fatalf("%v: row %d not left to right", tc.f, s.Row)
			}
			if i > 0 && slots[i-1].Row < s.Row && slots[i-1].Offset.Y <= s.Offset.Y {
				t.Fatalf("%v: row %d not behind row %d", tc.f, s.Row, slots[i-1].Row)
			}
		}
		if !slices.Equal(rows, tc.rows) {
			t.Fatalf("%v: rows %v, want %v", tc.f, rows, tc.rows)
		}
		// Every pair of slots is at least Spacing apart, and the layout is centered.
		var sum math2d.Vec2
		for i := range slots {
			sum = sum.Add(slots[i].Offset)
			for j := i + 1; j < len(slots); j++ {
				if d := slots[i].Offset.Dist(slots[j].Offset); d < tc.f.Spacing-1e-4 {
					t.Fatalf("%v: slots %d and %d %v apart", tc.f, i, j, d)
				}
			}
		}
		if tc.f.Shape == rts.ShapeLine && tc.f.Width == 0 && !near(sum.X, 0) {
			t.Fatalf("%v: line not centered: %v", tc.f, sum)
		}
	}
	if got := (rts.Formation{Shape: "zigzag", Spacing: -1}).Layout(nil, 4); len(got) != 4 || !near(got[1].Offset.X-got[0].Offset.X, 1) {
		t.Fatalf("invalid formation layout = %v, want a box with spacing 1", got)
	}
	if got := (rts.Formation{Shape: rts.ShapeLine, Spacing: 1}).Layout(nil, 0); len(got) != 0 {
		t.Fatal("zero units laid out slots")
	}
}

func TestPlaceRotatesToFacing(t *testing.T) {
	slots := rts.Formation{Shape: rts.ShapeLine, Spacing: 2}.Layout(nil, 3)
	// Facing right (+X), the line runs down the screen; "right of facing" is +Y.
	got := rts.Place(nil, slots, math2d.Vec2{X: 10, Y: 10}, math2d.Vec2{X: 5})
	want := []math2d.Vec2{{X: 10, Y: 8}, {X: 10, Y: 10}, {X: 10, Y: 12}}
	for i := range want {
		if !got[i].ApproxEqual(want[i], 1e-5) {
			t.Fatalf("Place = %v, want %v", got, want)
		}
	}
	// A zero facing faces up the screen.
	got = rts.Place(got, slots, math2d.Vec2{}, math2d.Vec2{})
	if !got[0].ApproxEqual(math2d.Vec2{X: -2}, 1e-5) {
		t.Fatalf("Place with zero facing = %v", got)
	}
}

func TestAssignKeepsPathsUncrossed(t *testing.T) {
	// Units in a column to the left move into a line facing right: each unit's lateral order must match its slot's.
	f := rts.Formation{Shape: rts.ShapeLine, Spacing: 1}
	slots := f.Layout(nil, 5)
	facing := math2d.Vec2{X: 1}
	positions := []math2d.Vec2{{X: 0, Y: 4}, {X: 0, Y: 0}, {X: 0, Y: 2}, {X: 0, Y: 1}, {X: 0, Y: 3}}
	assigned := rts.Assign(nil, positions, slots, facing)
	targets := rts.Place(nil, slots, math2d.Vec2{X: 20, Y: 2}, facing)
	for i := range positions {
		for j := range positions {
			if positions[i].Y < positions[j].Y && !(targets[assigned[i]].Y < targets[assigned[j]].Y) {
				t.Fatalf("units %d and %d cross: %v", i, j, assigned)
			}
		}
	}
	seen := map[int]bool{}
	for _, s := range assigned {
		if seen[s] {
			t.Fatalf("slot %d assigned twice: %v", s, assigned)
		}
		seen[s] = true
	}
	// Front units take the front row.
	box := rts.Formation{Shape: rts.ShapeBox, Spacing: 1, Width: 2}.Layout(nil, 4)
	positions = []math2d.Vec2{{X: 0, Y: 0}, {X: 5, Y: 0}, {X: 0, Y: 1}, {X: 5, Y: 1}}
	assigned = rts.Assign(assigned, positions, box, facing)
	if box[assigned[1]].Row != 0 || box[assigned[3]].Row != 0 || box[assigned[0]].Row != 1 {
		t.Fatalf("front units not in the front row: %v", assigned)
	}
	if got := rts.Assign(nil, nil, nil, facing); len(got) != 0 {
		t.Fatal("Assign of nothing returned slots")
	}
}

const formationsYAML = `version: 1
formations:
  - name: line
    shape: line
    spacing: 1.5
  - name: block
    shape: box
    spacing: 1.2
    width: 8
  - name: spear
    shape: wedge
    spacing: 2
`

func TestFormationsRoundTrip(t *testing.T) {
	fs, err := rts.ParseFormations([]byte(formationsYAML))
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := fs.Get("block"); !ok || f.Width != 8 || f.Shape != rts.ShapeBox || f.Spacing != 1.2 {
		t.Fatalf("Get(block) = %+v, %v", f, ok)
	}
	if _, ok := fs.Get("missing"); ok {
		t.Fatal("Get found a missing formation")
	}
	out, err := fs.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != formationsYAML {
		t.Fatalf("round trip changed the file:\n%s", out)
	}
	data, err := os.ReadFile("testdata/formations.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fs, err = rts.ParseFormations(data)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := fs.Encode(); !bytes.Equal(out, data) {
		t.Fatalf("testdata/formations.yaml does not round-trip:\n%s", out)
	}
}

func TestFormationsRefuseBadFiles(t *testing.T) {
	for name, src := range map[string]string{
		"empty":       "",
		"version":     "version: 2\nformations: []\n",
		"unknown":     "version: 1\nformations: []\ncolor: red\n",
		"field":       "version: 1\nformations:\n  - name: a\n    shape: line\n    spacing: 1\n    angle: 3\n",
		"no name":     "version: 1\nformations:\n  - shape: line\n    spacing: 1\n",
		"duplicate":   "version: 1\nformations:\n  - name: a\n    shape: line\n    spacing: 1\n  - name: a\n    shape: box\n    spacing: 1\n",
		"shape":       "version: 1\nformations:\n  - name: a\n    shape: circle\n    spacing: 1\n",
		"spacing":     "version: 1\nformations:\n  - name: a\n    shape: line\n    spacing: 0\n",
		"inf spacing": "version: 1\nformations:\n  - name: a\n    shape: line\n    spacing: .inf\n",
		"width":       "version: 1\nformations:\n  - name: a\n    shape: line\n    spacing: 1\n    width: -2\n",
		"trailing":    "version: 1\nformations: []\n---\nversion: 1\n",
	} {
		if _, err := rts.ParseFormations([]byte(src)); err == nil {
			t.Errorf("%s: parsed without error", name)
		} else if !strings.Contains(err.Error(), "rts: ") && !strings.Contains(err.Error(), "unknown field") {
			t.Errorf("%s: error %q lacks expected diagnostic", name, err)
		}
	}
	if _, err := (*rts.Formations)(nil).Encode(); err == nil {
		t.Fatal("Encode(nil) succeeded")
	}
}

func FuzzParseFormations(f *testing.F) {
	f.Add([]byte(formationsYAML))
	f.Add([]byte("version: 1\nformations: []\n"))
	f.Add([]byte("version: 1\nformations:\n  - name: x\n    shape: column\n    spacing: 3\n    width: 2\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		fs, err := rts.ParseFormations(data)
		if err != nil {
			return
		}
		out, err := fs.Encode()
		if err != nil {
			t.Fatalf("a parsed file failed to encode: %v", err)
		}
		again, err := rts.ParseFormations(out)
		if err != nil {
			t.Fatalf("encoded file failed to parse: %v\n%s", err, out)
		}
		out2, err := again.Encode()
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("encoding is not stable:\n%s\n---\n%s", out, out2)
		}
		for _, f := range fs.Formations {
			if n := len(f.Layout(nil, 7)); n != 7 {
				t.Fatalf("%+v laid out %d slots", f, n)
			}
		}
	})
}

func TestLayoutHugeWidth(t *testing.T) {
	for _, shape := range []rts.Shape{rts.ShapeLine, rts.ShapeColumn, rts.ShapeBox} {
		f := rts.Formation{Shape: shape, Spacing: 1, Width: math.MaxInt}
		if got := f.Layout(nil, 2); len(got) != 2 || got[0].Row != 0 || got[1].Row != 0 {
			t.Fatalf("%s with the largest width laid out %+v", shape, got)
		}
	}
}
