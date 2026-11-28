package procgen_test

import (
	"github.com/nsgo-dev/game/pkg/procgen"
	"testing"
)

func TestPlacementRoundTripAndDeterministicGrid(t *testing.T) {
	p, err := procgen.ParsePlacement([]byte("version: 1\nseed: 7\nsteps:\n  - id: trees\n    sampler: grid\n    count: 8\n    prefab: tree\n"))
	if err != nil {
		t.Fatal(err)
	}
	a, b := p.Grid(10, 10), p.Grid(10, 10)
	if len(a) != 8 || len(b) != 8 {
		t.Fatal("grid count")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("non deterministic")
		}
	}
	if _, err := procgen.EncodePlacement(p); err != nil {
		t.Fatal(err)
	}
}
