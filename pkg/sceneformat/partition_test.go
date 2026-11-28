package sceneformat

import (
	"math"
	"slices"
	"testing"
)

func TestPartitionCellsByBounds(t *testing.T) {
	p := Partition{CellSize: 10, LoadRadius: 1, UnloadRadius: 2}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		o    CellObject
		want []CellCoord
	}{
		{"spans four cells", CellObject{X: 10, Y: 10, Width: 12, Height: 2}, []CellCoord{{0, 0}, {1, 0}, {0, 1}, {1, 1}}},
		{"edge on boundary stays in one cell", CellObject{X: 5, Y: 5, Width: 10, Height: 10}, []CellCoord{{0, 0}}},
		{"long bridge", CellObject{X: 0, Y: 5, Width: 40, Height: 1}, []CellCoord{{-2, 0}, {-1, 0}, {0, 0}, {1, 0}}},
		{"point on boundary", CellObject{X: 10, Y: 10}, []CellCoord{{1, 1}}},
		{"point at negative coordinate", CellObject{X: -0.5, Y: -10}, []CellCoord{{-1, -1}}},
		{"always loaded", CellObject{X: 5, Y: 5, Width: 1, Height: 1, AlwaysLoaded: true}, nil},
		{"negative size", CellObject{X: 5, Y: 5, Width: -1, Height: 1}, nil},
		{"NaN position", CellObject{X: math.NaN(), Y: 5}, nil},
		{"infinite size", CellObject{X: 5, Y: 5, Width: math.Inf(1), Height: 1}, nil},
	}
	for _, tc := range cases {
		if got := p.Cells(tc.o); !slices.Equal(got, tc.want) {
			t.Errorf("%s: Cells = %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := (Partition{}).Cells(CellObject{X: 1, Y: 1}); got != nil {
		t.Errorf("zero cell size: Cells = %v, want nil", got)
	}
}

func TestPartitionNeededAndRetained(t *testing.T) {
	p := Partition{CellSize: 10, LoadRadius: 1, UnloadRadius: 2}
	sources := []StreamingSource{{X: 25, Y: 5}, {X: 5, Y: 5}}
	want := []CellCoord{
		{-1, -1}, {0, -1}, {1, -1}, {2, -1}, {3, -1},
		{-1, 0}, {0, 0}, {1, 0}, {2, 0}, {3, 0},
		{-1, 1}, {0, 1}, {1, 1}, {2, 1}, {3, 1},
	}
	got := p.Needed(sources)
	if !slices.Equal(got, want) {
		t.Fatalf("Needed = %v, want %v", got, want)
	}
	slices.Reverse(sources)
	if again := p.Needed(sources); !slices.Equal(again, got) {
		t.Fatalf("Needed depends on source order: %v vs %v", again, got)
	}

	retained := p.Retained(sources)
	if len(retained) != 7*5 {
		t.Fatalf("Retained has %d cells, want 35: %v", len(retained), retained)
	}
	for _, c := range got {
		if !slices.Contains(retained, c) {
			t.Fatalf("needed cell %v is not retained", c)
		}
	}

	scaled := p.Needed([]StreamingSource{{X: 5, Y: 5, RadiusScale: 1.5}})
	if len(scaled) != 5*5 {
		t.Fatalf("RadiusScale 1.5 gives %d cells, want 25 (radius 2)", len(scaled))
	}
	if got := p.Needed([]StreamingSource{{X: math.NaN(), Y: 0}}); len(got) != 0 {
		t.Fatalf("NaN source loads %v", got)
	}
	if got := (Partition{CellSize: -1}).Needed(sources); got != nil {
		t.Fatalf("invalid partition: Needed = %v, want nil", got)
	}
}

func TestPartitionValidate(t *testing.T) {
	for _, p := range []Partition{
		{CellSize: 0},
		{CellSize: math.NaN()},
		{CellSize: math.Inf(1)},
		{CellSize: 1, LoadRadius: -1},
		{CellSize: 1, LoadRadius: 2, UnloadRadius: 1},
	} {
		if err := p.Validate(); err == nil {
			t.Errorf("Validate(%+v) = nil, want an error", p)
		}
	}
	if err := (Partition{CellSize: 1, LoadRadius: 2, UnloadRadius: 2}).Validate(); err != nil {
		t.Errorf("equal radii rejected: %v", err)
	}
}
