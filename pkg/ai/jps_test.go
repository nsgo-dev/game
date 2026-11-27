package ai_test

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/ai"
)

func TestJPSMatchesAStarExactOptimalRandomized(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 833))

	for _, noCut := range []bool{true, false} {
		astar := ai.NewAStar()
		astar.Diagonal = true
		astar.NoCornerCutting = noCut

		jps := ai.NewJPS()
		jps.NoCornerCutting = noCut

		densities := []float64{0.0, 0.10, 0.25, 0.35, 0.45}
		for _, density := range densities {
			for trial := range 30 {
				cols := 12 + rng.IntN(25)
				rows := 12 + rng.IntN(25)
				g := ai.NewBoolGrid(cols, rows)
				for r := range rows {
					for c := range cols {
						if rng.Float64() < density {
							g.SetWalkable(c, r, false)
						}
					}
				}
				start := ai.Cell{Col: rng.IntN(cols), Row: rng.IntN(rows)}
				goal := ai.Cell{Col: rng.IntN(cols), Row: rng.IntN(rows)}
				g.SetWalkable(start.Col, start.Row, true)
				g.SetWalkable(goal.Col, goal.Row, true)

				_, optCost, optOK := astar.Find(g, start, goal)
				path, jpsCost, jpsOK := jps.Find(g, start, goal)

				if jpsOK != optOK {
					t.Fatalf("noCut=%v density=%.2f trial=%d (%dx%d %v->%v): jpsOK=%v, optOK=%v",
						noCut, density, trial, cols, rows, start, goal, jpsOK, optOK)
				}
				if !optOK {
					continue
				}
				verifyPath(t, g, path, start, goal, true, noCut)
				computed := computePathCost(g, path, true)
				if math.Abs(jpsCost-computed) > 1e-6 {
					t.Fatalf("noCut=%v trial=%d: reported cost %v != path cost %v", noCut, trial, jpsCost, computed)
				}
				if math.Abs(jpsCost-optCost) > 1e-6 {
					t.Fatalf("noCut=%v density=%.2f trial=%d (%dx%d %v->%v): jpsCost=%.9f != astarCost=%.9f",
						noCut, density, trial, cols, rows, start, goal, jpsCost, optCost)
				}
			}
		}
	}
}

func TestJPSCustomGridFuncAndCornerCases(t *testing.T) {
	// Test JPS through non-BoolGrid (GridFunc) to exercise the interface path.
	blocked := map[ai.Cell]bool{
		{Col: 1, Row: 0}: true,
		{Col: 0, Row: 1}: true,
	}
	gf := ai.GridFunc{
		Cols: 5, Rows: 5,
		Walk: func(c, r int) bool { return !blocked[ai.Cell{Col: c, Row: r}] },
	}

	noCut := ai.NewJPS()
	if !noCut.NoCornerCutting {
		t.Fatal("NewJPS must default NoCornerCutting to true")
	}
	if _, _, ok := noCut.Find(gf, ai.Cell{Col: 0, Row: 0}, ai.Cell{Col: 1, Row: 1}); ok {
		t.Fatal("expected blocked diagonal squeeze when NoCornerCutting is true")
	}

	cut := ai.NewJPS()
	cut.NoCornerCutting = false
	path, cost, ok := cut.Find(gf, ai.Cell{Col: 0, Row: 0}, ai.Cell{Col: 1, Row: 1})
	if !ok || math.Abs(cost-math.Sqrt2) > 1e-9 || !slices.Equal(path, []ai.Cell{{Col: 0, Row: 0}, {Col: 1, Row: 1}}) {
		t.Fatalf("corner cut gave (%v, %v, %v)", path, cost, ok)
	}

	// Edge cases: nil, empty, out-of-bounds, blocked start/goal, start == goal.
	if _, _, ok := noCut.Find(nil, ai.Cell{}, ai.Cell{}); ok {
		t.Fatal("nil grid should fail")
	}
	if _, _, ok := noCut.Find(ai.NewBoolGrid(0, 0), ai.Cell{}, ai.Cell{}); ok {
		t.Fatal("empty grid should fail")
	}
	if _, _, ok := noCut.Find(gf, ai.Cell{Col: -1, Row: 0}, ai.Cell{Col: 4, Row: 4}); ok {
		t.Fatal("out-of-bounds start should fail")
	}
	if _, _, ok := noCut.Find(gf, ai.Cell{Col: 1, Row: 0}, ai.Cell{Col: 4, Row: 4}); ok {
		t.Fatal("blocked start should fail")
	}
	p0, c0, ok0 := noCut.Find(gf, ai.Cell{Col: 3, Row: 3}, ai.Cell{Col: 3, Row: 3})
	if !ok0 || c0 != 0 || !slices.Equal(p0, []ai.Cell{{Col: 3, Row: 3}}) {
		t.Fatalf("start==goal gave (%v, %v, %v)", p0, c0, ok0)
	}
}

func TestJPSJumpPointsDeterminismAndZeroAlloc(t *testing.T) {
	g := ai.NewBoolGrid(64, 64)
	for r := 8; r < 56; r++ {
		g.SetWalkable(32, r, false)
	}
	jps := ai.NewJPS()
	start := ai.Cell{Col: 4, Row: 32}
	goal := ai.Cell{Col: 60, Row: 32}

	ref, refCost, ok := jps.Find(g, start, goal)
	if !ok {
		t.Fatal("expected path")
	}
	verifyPath(t, g, ref, start, goal, true, true)

	jpts, jpCost, ok := jps.FindJumpPoints(g, start, goal)
	if !ok || jpCost != refCost || len(jpts) >= len(ref) {
		t.Fatalf("FindJumpPoints gave len=%d (full=%d) cost=%v (ref=%v)", len(jpts), len(ref), jpCost, refCost)
	}
	if jpts[0] != start || jpts[len(jpts)-1] != goal {
		t.Fatalf("jump point endpoints = (%v, %v)", jpts[0], jpts[len(jpts)-1])
	}

	// Determinism across repeated runs.
	for i := range 20 {
		got, gotCost, ok := jps.Find(g, start, goal)
		if !ok || gotCost != refCost || !slices.Equal(got, ref) {
			t.Fatalf("run %d differed", i)
		}
	}

	// Zero-allocation FindInto after warm-up.
	buf := make([]ai.Cell, 0, len(ref)+16)
	buf, _, ok = jps.FindInto(buf, g, start, goal)
	if !ok {
		t.Fatal("warm-up failed")
	}
	allocs := testing.AllocsPerRun(50, func() {
		var c float64
		buf, c, ok = jps.FindInto(buf[:0], g, start, goal)
		if !ok || c != refCost {
			t.Fatal("FindInto mismatch")
		}
	})
	if allocs != 0 {
		t.Fatalf("JPS.FindInto allocated %v times per run, want 0", allocs)
	}
}

func BenchmarkJPS1024(b *testing.B) {
	g := buildLargeBenchmarkMap(1024)
	j := ai.NewJPS()
	start := ai.Cell{Col: 2, Row: 2}
	goal := ai.Cell{Col: 1021, Row: 1021}
	buf := make([]ai.Cell, 0, 4096)
	buf, _, _ = j.FindInto(buf, g, start, goal)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buf, _, _ = j.FindInto(buf[:0], g, start, goal)
	}
}
