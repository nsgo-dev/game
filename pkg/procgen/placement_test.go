package procgen_test

import (
	"os"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/procgen"
)

func loadForest(t *testing.T) *procgen.Placement {
	t.Helper()
	data, err := os.ReadFile("testdata/canonical/forest.placement.yaml")
	if err != nil {
		t.Fatal(err)
	}
	p, err := procgen.ParsePlacement(data)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type cell struct{ x, y float32 }

func stepCells(points []procgen.PlacementPoint, step string) []cell {
	var out []cell
	for _, pt := range points {
		if pt.Step == step {
			out = append(out, cell{pt.X, pt.Y})
		}
	}
	return out
}

// TestPlacementGridGolden pins the points of the fixture, so a change to the sampler or to random.Stream that moves a point fails here on every architecture.
func TestPlacementGridGolden(t *testing.T) {
	points := loadForest(t).Points(10, 8)
	if len(points) != 12 {
		t.Fatalf("got %d points, want 12: %+v", len(points), points)
	}
	wantTrees := []cell{{4, 0}, {6, 0}, {0, 2}, {0, 6}, {2, 6}, {6, 6}}
	if got := stepCells(points, "trees"); !slices.Equal(got, wantTrees) {
		t.Fatalf("trees = %v, want %v", got, wantTrees)
	}
	wantLamps := []cell{{0, 0}, {4, 0}, {8, 0}, {0, 4}, {4, 4}, {8, 4}}
	if got := stepCells(points, "lamps"); !slices.Equal(got, wantLamps) {
		t.Fatalf("lamps = %v, want %v", got, wantLamps)
	}
	if pt := points[2]; pt.ID != "trees-2" || pt.Step != "trees" || pt.Prefab != "tree" {
		t.Fatalf("points[2] = %+v, want trees-2 of step trees with prefab tree", pt)
	}
	if pt := points[6]; pt.ID != "lamps-0" || pt.Step != "lamps" || pt.Prefab != "lamp" {
		t.Fatalf("points[6] = %+v, want lamps-0 of step lamps with prefab lamp", pt)
	}
}

func TestPlacementStepsHaveIndependentStreams(t *testing.T) {
	p := loadForest(t)
	alone := &procgen.Placement{Version: 1, Seed: p.Seed, Steps: p.Steps[:1]}
	reordered := &procgen.Placement{Version: 1, Seed: p.Seed, Steps: []procgen.PlacementStep{p.Steps[1], p.Steps[0]}}
	want := stepCells(p.Points(10, 8), "trees")
	if got := stepCells(alone.Points(10, 8), "trees"); !slices.Equal(got, want) {
		t.Fatalf("removing the lamps step moved the trees: %v, want %v", got, want)
	}
	if got := stepCells(reordered.Points(10, 8), "trees"); !slices.Equal(got, want) {
		t.Fatalf("reordering the steps moved the trees: %v, want %v", got, want)
	}
	other := &procgen.Placement{Version: 1, Seed: p.Seed + 1, Steps: p.Steps[:1]}
	if got := stepCells(other.Points(10, 8), "trees"); slices.Equal(got, want) {
		t.Fatalf("seeds %d and %d chose the same trees %v", p.Seed, p.Seed+1, got)
	}
}

func TestPlacementPointsEdgeCases(t *testing.T) {
	p := &procgen.Placement{Version: 1, Steps: []procgen.PlacementStep{{ID: "rocks", Sampler: "grid", Count: 50, Prefab: "rock"}}}
	if got := p.Points(3, 2); len(got) != 6 {
		t.Fatalf("count above the lattice size gave %d points, want all 6", len(got))
	}
	if got := p.Points(0, 5); len(got) != 0 {
		t.Fatalf("an empty area gave %d points, want none", len(got))
	}
	if got := p.Points(-1, -1); len(got) != 0 {
		t.Fatalf("a negative area gave %d points, want none", len(got))
	}
}

func TestParsePlacementErrors(t *testing.T) {
	cases := []struct {
		name, data, field string
	}{
		{"unknown sampler", "version: 1\nsteps:\n  - id: a\n    sampler: poisson\n    prefab: p\n", "steps[0].sampler"},
		{"duplicate id", "version: 1\nsteps:\n  - id: a\n    sampler: grid\n    prefab: p\n  - id: a\n    sampler: grid\n    prefab: p\n", "steps[1].id"},
		{"missing id", "version: 1\nsteps:\n  - sampler: grid\n    prefab: p\n", "steps[0].id"},
		{"negative spacing", "version: 1\nsteps:\n  - id: a\n    sampler: grid\n    spacing: -1\n    prefab: p\n", "steps[0].spacing"},
		{"negative count", "version: 1\nsteps:\n  - id: a\n    sampler: grid\n    count: -2\n    prefab: p\n", "steps[0].count"},
		{"missing prefab", "version: 1\nsteps:\n  - id: a\n    sampler: grid\n", "steps[0].prefab"},
		{"no steps", "version: 1\nsteps: []\n", "steps"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := []byte(c.data)
			_, err := procgen.ParsePlacement(data)
			loaderrtest.ExpectField(t, err, "", data, c.field)
		})
	}
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nsteps:\n  - id: a\n    sampler: grid\n    prefab: p\n    exclude: [road]\n")
		_, err := procgen.ParsePlacement(data)
		loaderrtest.Expect(t, err, "", 6)
	})
	t.Run("trailing document", func(t *testing.T) {
		if _, err := procgen.ParsePlacement([]byte("version: 1\nsteps:\n  - {id: a, sampler: grid, prefab: p}\n---\nversion: 1\n")); err == nil {
			t.Fatal("ParsePlacement accepted a trailing YAML document")
		}
	})
}

func TestPlacementDeterministicAcrossRepeatedRuns(t *testing.T) {
	p := loadForest(t)
	first := p.Points(37, 29)
	for i := 0; i < 20; i++ {
		if got := p.Points(37, 29); !slices.Equal(got, first) {
			t.Fatalf("run %d changed placement", i)
		}
	}
}
