package ai_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func almostEqual32(a, b, eps float32) bool {
	return math.Abs(float64(a)-float64(b)) <= float64(eps)
}

func assertLayerBitIdentical(t *testing.T, got, want *ai.InfluenceLayer) {
	t.Helper()
	gc, gr := got.Size()
	wc, wr := want.Size()
	if gc != wc || gr != wr {
		t.Fatalf("size mismatch: got (%d,%d), want (%d,%d)", gc, gr, wc, wr)
	}
	gv, wv := got.Values(), want.Values()
	for i := range gv {
		if math.Float32bits(gv[i]) != math.Float32bits(wv[i]) {
			col, row := i%gc, i/gc
			t.Fatalf("cell (%d,%d) bit mismatch: got %.9g (0x%08x), want %.9g (0x%08x)",
				col, row, gv[i], math.Float32bits(gv[i]), wv[i], math.Float32bits(wv[i]))
		}
	}
}

func TestInfluenceStampFalloffsAndDeterminism(t *testing.T) {
	// 9x7 grid with a blocked cell inside a source's footprint.
	grid := ai.NewBoolGrid(9, 7)
	grid.SetWalkable(3, 3, false)

	custom := func(dist, radius float32) float32 {
		if radius <= 0 {
			return 1
		}
		x := 1 - dist/radius
		if x <= 0 {
			return 0
		}
		return x * x // quadratic falloff
	}

	build := func() *ai.InfluenceLayer {
		layer := ai.NewInfluenceLayerForGrid(grid, math2d.V2(16, 32), 8)
		// Linear source at (2, 3), strength 10, radius 2.
		layer.Stamp(grid, ai.InfluenceSource{
			Cell:     ai.Cell{Col: 2, Row: 3},
			Strength: 10,
			Radius:   2,
			Falloff:  ai.FalloffLinear,
		})
		// Exponential source at (6, 3), strength 8, radius 2, rate 0.5.
		layer.Stamp(grid, ai.InfluenceSource{
			Cell:        ai.Cell{Col: 6, Row: 3},
			Strength:    8,
			Radius:      2,
			Falloff:     ai.FalloffExponential,
			FalloffRate: 0.5,
		})
		// Custom quadratic source at (4, 5), strength -4, radius 2.
		layer.Stamp(grid, ai.InfluenceSource{
			Cell:          ai.Cell{Col: 4, Row: 5},
			Strength:      -4,
			Radius:        2,
			Falloff:       ai.FalloffCustom,
			CustomFalloff: custom,
		})
		// Radius 0 source stamps only its own cell.
		layer.Stamp(grid, ai.InfluenceSource{
			Cell:     ai.Cell{Col: 0, Row: 0},
			Strength: 3.5,
			Radius:   0,
		})
		return layer
	}

	l1 := build()
	l2 := build()
	assertLayerBitIdentical(t, l1, l2)

	// Linear source checks:
	if got := l1.At(2, 3); got != 10 {
		t.Errorf("linear center (2,3) = %v, want 10", got)
	}
	if got := l1.At(1, 3); !almostEqual32(got, 5, 1e-6) {
		t.Errorf("linear cardinal (1,3) = %v, want 5", got)
	}
	wantDiag := float32(10 * (1 - math.Sqrt2/2))
	if got := l1.At(1, 2); !almostEqual32(got, wantDiag, 1e-6) {
		t.Errorf("linear diagonal (1,2) = %v, want %v", got, wantDiag)
	}
	if got := l1.At(0, 3); got != 0 {
		t.Errorf("linear boundary (0,3) = %v, want 0", got)
	}
	// Blocked cell (3,3) must stay 0 even though it is 1 cell from (2,3).
	if got := l1.At(3, 3); got != 0 {
		t.Errorf("blocked cell (3,3) = %v, want 0", got)
	}

	// Exponential source checks:
	if got := l1.At(6, 3); got != 8 {
		t.Errorf("exp center (6,3) = %v, want 8", got)
	}
	wantExp1 := float32(8 * math.Exp(-0.5))
	if got := l1.At(7, 3); !almostEqual32(got, wantExp1, 1e-6) {
		t.Errorf("exp cardinal (7,3) = %v, want %v", got, wantExp1)
	}
	wantExp2 := float32(8 * math.Exp(-1.0))
	if got := l1.At(8, 3); !almostEqual32(got, wantExp2, 1e-6) {
		t.Errorf("exp dist-2 (8,3) = %v, want %v", got, wantExp2)
	}

	// Custom quadratic source checks:
	if got := l1.At(4, 5); got != -4 {
		t.Errorf("custom center (4,5) = %v, want -4", got)
	}
	if got := l1.At(4, 4); !almostEqual32(got, -1, 1e-6) { // (1 - 0.5)^2 * -4 = -1
		t.Errorf("custom cardinal (4,4) = %v, want -1", got)
	}

	// Radius 0 source checks:
	if got := l1.At(0, 0); got != 3.5 {
		t.Errorf("radius-0 center (0,0) = %v, want 3.5", got)
	}
	if got := l1.At(1, 0); got != 0 {
		t.Errorf("radius-0 neighbor (1,0) = %v, want 0", got)
	}

	// NaN / Inf values must never poison the grid.
	l1.Stamp(grid, ai.InfluenceSource{Cell: ai.Cell{Col: 0, Row: 6}, Strength: float32(math.NaN()), Radius: 2})
	l1.Stamp(grid, ai.InfluenceSource{Cell: ai.Cell{Col: 0, Row: 6}, Strength: 5, Radius: float32(math.NaN()), Falloff: ai.FalloffCustom, CustomFalloff: func(float32, float32) float32 { return float32(math.Inf(1)) }})
	if got := l1.At(0, 6); got != 0 {
		t.Errorf("sanitized cell (0,6) = %v, want 0", got)
	}
}

func TestInfluenceSpreadAroundBlockedCellsAndDeterminism(t *testing.T) {
	// 6x5 grid with a vertical wall at col 2, rows 0..3 (gap only at row 4):
	// . . # . . .
	// . S # . . .
	// . . # . . .
	// . . # . . .
	// . . . . . .
	grid := ai.NewBoolGrid(6, 5)
	for r := range 4 {
		grid.SetWalkable(2, r, false)
	}

	run := func() *ai.InfluenceLayer {
		l := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 1)
		l.Stamp(grid, ai.InfluenceSource{
			Cell:     ai.Cell{Col: 1, Row: 1},
			Strength: 10,
			Radius:   0,
		})
		l.Spread(grid, ai.SpreadOptions{
			Decay:      0.2,
			Momentum:   0,
			Iterations: 6,
			Diagonal:   false,
		})
		return l
	}

	a := run()
	b := run()
	assertLayerBitIdentical(t, a, b)

	// Wall cells must remain strictly 0.
	for r := range 4 {
		if v := a.At(2, r); v != 0 {
			t.Errorf("wall cell (2,%d) = %v, want 0", r, v)
		}
	}

	// Shortest 4-way path from (1,1) to (3,3) goes around the bottom gap (2,4):
	// (1,1)->(1,2)->(1,3)->(1,4)->(2,4)->(3,4)->(3,3) = 6 steps -> 10 * 0.8^6.
	wantAround := float32(10 * math.Pow(0.8, 6))
	if got := a.At(3, 3); !almostEqual32(got, wantAround, 1e-5) {
		t.Errorf("(3,3) around wall = %v, want %v (10*0.8^6)", got, wantAround)
	}
	// (3,1) is 8 steps away around the gap, so after 6 iterations it must still be 0
	// (confirming influence did not leak through the wall at (2,1)).
	if got := a.At(3, 1); got != 0 {
		t.Errorf("(3,1) behind wall after 6 iters = %v, want 0 (leaked through wall)", got)
	}
	// (1,4) is 3 steps down from (1,1): 10 * 0.8^3 = 5.12.
	if got := a.At(1, 4); !almostEqual32(got, 5.12, 1e-5) {
		t.Errorf("(1,4) = %v, want 5.12", got)
	}
}

func TestInfluenceSpreadDiagonalNoCornerCutting(t *testing.T) {
	// 3x3 grid with two walls forming a diagonal pinch around (1,1):
	// S # .
	// # . .
	// . . .
	grid := ai.NewBoolGrid(3, 3)
	grid.SetWalkable(1, 0, false)
	grid.SetWalkable(0, 1, false)

	l := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 1)
	l.Stamp(grid, ai.InfluenceSource{Cell: ai.Cell{Col: 0, Row: 0}, Strength: 10})
	l.Spread(grid, ai.SpreadOptions{Decay: 0.2, Momentum: 0, Iterations: 3, Diagonal: true})

	// (1,1) must NOT receive influence diagonally across the (1,0)/(0,1) wall corner.
	if got := l.At(1, 1); got != 0 {
		t.Fatalf("(1,1) cut diagonal corner = %v, want 0", got)
	}

	// Unblock (1,0) and confirm diagonal spread uses (1 - decay)^sqrt(2).
	grid.SetWalkable(1, 0, true)
	l.Clear()
	l.Stamp(grid, ai.InfluenceSource{Cell: ai.Cell{Col: 0, Row: 0}, Strength: 10})
	l.Spread(grid, ai.SpreadOptions{Decay: 0.2, Momentum: 0, Iterations: 1, Diagonal: true})
	// (0,1) is still blocked, so (1,1) still cannot be reached directly in 1 diagonal step.
	if got := l.At(1, 1); got != 0 {
		t.Fatalf("(1,1) with one cardinal neighbor blocked = %v, want 0", got)
	}

	grid.SetWalkable(0, 1, true)
	l.Clear()
	l.Stamp(grid, ai.InfluenceSource{Cell: ai.Cell{Col: 0, Row: 0}, Strength: 10})
	l.Spread(grid, ai.SpreadOptions{Decay: 0.2, Momentum: 0, Iterations: 1, Diagonal: true})
	wantDiag := float32(10 * math.Pow(0.8, math.Sqrt2))
	if got := l.At(1, 1); !almostEqual32(got, wantDiag, 1e-5) {
		t.Fatalf("(1,1) open diagonal = %v, want %v", got, wantDiag)
	}
}

func TestInfluenceSpreadMomentumWithTrackedSources(t *testing.T) {
	grid := ai.NewBoolGrid(6, 1)
	l := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 1)
	id := l.AddSource(ai.InfluenceSource{Cell: ai.Cell{Col: 0, Row: 0}, Strength: 10})
	l.Update(grid) // seed (0,0) = 10

	// Spread 1 step with momentum 0.5, decay 0.2:
	// (0,0): prev=10, anchor=10 -> next=10
	// (1,0): prev=0, target=10*0.8=8 -> next=0*0.5 + 8*0.5 = 4
	l.Spread(grid, ai.SpreadOptions{Decay: 0.2, Momentum: 0.5, Iterations: 1})
	if got := l.At(0, 0); !almostEqual32(got, 10, 1e-6) {
		t.Fatalf("after spread 1, (0,0) = %v, want 10", got)
	}
	if got := l.At(1, 0); !almostEqual32(got, 4, 1e-6) {
		t.Fatalf("after spread 1, (1,0) = %v, want 4", got)
	}

	// Move source to (5,0) and run Spread with momentum 0.5:
	// Old cell (0,0) now has base=0, neighbor (1,0)=4 -> target=4*0.8=3.2 -> next=10*0.5 + 3.2*0.5 = 6.6.
	// New cell (5,0) now has base=10, prev=0 -> next=0*0.5 + 10*0.5 = 5.0.
	l.MoveSource(id, ai.Cell{Col: 5, Row: 0})
	l.Spread(grid, ai.SpreadOptions{Decay: 0.2, Momentum: 0.5, Iterations: 1})
	if got := l.At(0, 0); !almostEqual32(got, 6.6, 1e-5) {
		t.Fatalf("after move+spread, old cell (0,0) = %v, want 6.6", got)
	}
	if got := l.At(5, 0); !almostEqual32(got, 5.0, 1e-5) {
		t.Fatalf("after move+spread, new cell (5,0) = %v, want 5.0", got)
	}

	// Remove source and step: with Decay=0.2 and Momentum=0.5, neighboring cells sustain each other
	// at rate at most Momentum + (1-Momentum)*(1-Decay) = 0.9 per step, so peak values decay
	// smoothly and exponentially toward 0.
	l.RemoveSource(id)
	prevPeak := float32(6.6)
	for step := range 60 {
		l.Spread(grid, ai.SpreadOptions{Decay: 0.2, Momentum: 0.5, Iterations: 1})
		_, peak, _ := l.Highest(grid)
		if peak > prevPeak*0.9001 {
			t.Fatalf("step %d: peak %v exceeded expected decay bound %v", step, peak, prevPeak*0.9)
		}
		prevPeak = peak
	}
	if prevPeak >= 0.02 {
		t.Fatalf("after removing source and 60 momentum steps, peak = %v, want < 0.02", prevPeak)
	}
}

func TestInfluenceIncrementalMatchesRebuildBitIdentical(t *testing.T) {
	grid := ai.NewBoolGrid(32, 24)
	for r := 4; r < 20; r++ {
		grid.SetWalkable(15, r, false)
	}

	inc := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 1)
	ref := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 1)

	// Add 12 overlapping sources with mixed falloffs and signs.
	idsInc := make([]ai.InfluenceSourceID, 12)
	idsRef := make([]ai.InfluenceSourceID, 12)
	for i := range 12 {
		src := ai.InfluenceSource{
			Cell:        ai.Cell{Col: (i*5 + 2) % 32, Row: (i*3 + 1) % 24},
			Strength:    float32(5 + i%7),
			Radius:      float32(2 + i%4),
			Falloff:     ai.FalloffKind(i % 2),
			FalloffRate: 0.4 + 0.1*float32(i%3),
		}
		if i%4 == 0 {
			src.Strength = -src.Strength
		}
		idsInc[i] = inc.AddSource(src)
		idsRef[i] = ref.AddSource(src)
	}
	inc.Update(grid)
	ref.Rebuild(grid)
	assertLayerBitIdentical(t, inc, ref)

	// Perform 150 deterministic incremental mutations: moves, strength/radius edits, removals, re-adds,
	// off-grid moves, and wall toggles via MarkDirtyCell.
	seed := uint32(0x12345678)
	nextInt := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int((seed >> 8) % uint32(n))
	}

	for step := range 150 {
		idx := step % len(idsInc)
		switch step % 5 {
		case 0:
			// Move one source.
			c := ai.Cell{Col: nextInt(32), Row: nextInt(24)}
			inc.MoveSource(idsInc[idx], c)
			ref.MoveSource(idsRef[idx], c)
		case 1:
			// Move one source multiple times before Update.
			c1 := ai.Cell{Col: nextInt(32), Row: nextInt(24)}
			c2 := ai.Cell{Col: nextInt(32), Row: nextInt(24)}
			inc.MoveSource(idsInc[idx], c1)
			inc.MoveSource(idsInc[idx], c2)
			ref.MoveSource(idsRef[idx], c2)
		case 2:
			// Change radius and strength.
			s, ok := inc.Source(idsInc[idx])
			if !ok {
				t.Fatalf("step %d: missing source %d", step, idx)
			}
			s.Radius = float32(1 + nextInt(6))
			s.Strength = float32(nextInt(20) - 8)
			inc.SetSource(idsInc[idx], s)
			ref.SetSource(idsRef[idx], s)
		case 3:
			// Remove and immediately re-add (exercising free-slot reuse and stale ID rejection).
			oldID := idsInc[idx]
			if !inc.RemoveSource(oldID) {
				t.Fatalf("step %d: RemoveSource failed", step)
			}
			if inc.MoveSource(oldID, ai.Cell{Col: 1, Row: 1}) {
				t.Fatalf("step %d: MoveSource accepted removed ID", step)
			}
			ref.RemoveSource(idsRef[idx])
			ns := ai.InfluenceSource{
				Cell:     ai.Cell{Col: nextInt(36) - 2, Row: nextInt(28) - 2}, // includes off-grid coords
				Strength: float32(1 + nextInt(10)),
				Radius:   float32(nextInt(5)),
				Falloff:  ai.FalloffLinear,
			}
			idsInc[idx] = inc.AddSource(ns)
			idsRef[idx] = ref.AddSource(ns)
		case 4:
			// Toggle a grid cell walkability and mark it dirty incrementally.
			tc := ai.Cell{Col: nextInt(32), Row: nextInt(24)}
			grid.SetWalkable(tc.Col, tc.Row, !grid.Walkable(tc.Col, tc.Row))
			inc.MarkDirtyCell(tc)
		}

		inc.Update(grid)
		ref.Rebuild(grid)
		assertLayerBitIdentical(t, inc, ref)
	}

	if inc.SourceCount() != len(idsInc) {
		t.Fatalf("SourceCount = %d, want %d", inc.SourceCount(), len(idsInc))
	}
	if _, ok := inc.Source(0); ok {
		t.Fatal("Source(0) returned ok=true, want false")
	}
}

func TestInfluenceLayerCombinationsAndTacticalMaps(t *testing.T) {
	// 10x6 tactical arena with friendly squad on the left and enemy squad on the right.
	// A vertical wall at col 4 with a gap at rows 2..3 splits the arena.
	grid := ai.NewBoolGrid(10, 6)
	for _, r := range []int{0, 1, 4, 5} {
		grid.SetWalkable(4, r, false)
	}

	friendly := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	enemy := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)

	friendly.AddSource(ai.InfluenceSource{Cell: ai.Cell{Col: 1, Row: 2}, Strength: 10, Radius: 5, Falloff: ai.FalloffLinear})
	enemy.AddSource(ai.InfluenceSource{Cell: ai.Cell{Col: 8, Row: 2}, Strength: 12, Radius: 6, Falloff: ai.FalloffLinear})
	friendly.Update(grid)
	enemy.Update(grid)

	// Control = friendly - enemy
	control := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	control.Subtract(friendly, enemy)
	if control.At(1, 2) <= 0 {
		t.Errorf("friendly base control (1,2) = %v, want > 0", control.At(1, 2))
	}
	if control.At(8, 2) >= 0 {
		t.Errorf("enemy base control (8,2) = %v, want < 0", control.At(8, 2))
	}

	// Tension = friendly + enemy
	tension := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	tension.Add(friendly, enemy)
	for r := range 6 {
		for c := range 10 {
			want := friendly.At(c, r) + enemy.At(c, r)
			if got := tension.At(c, r); !almostEqual32(got, want, 1e-6) {
				t.Fatalf("tension(%d,%d) = %v, want %v", c, r, got, want)
			}
		}
	}

	// Vulnerability = tension - |control| (peaks in the contested middle gap around col 4..5, row 2).
	absControl := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	absControl.CopyFrom(control)
	absControl.Abs()
	vuln := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	vuln.Subtract(tension, absControl)
	if vuln.At(5, 2) <= 0 {
		t.Errorf("contested midpoint vulnerability (5,2) = %v, want > 0", vuln.At(5, 2))
	}
	if vuln.At(1, 2) != 0 {
		t.Errorf("uncontested friendly base vulnerability (1,2) = %v, want 0", vuln.At(1, 2))
	}

	// Multiply: contested product (friendly * enemy) is non-zero only where both overlap.
	overlap := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	overlap.Multiply(friendly, enemy)
	if overlap.At(5, 2) <= 0 || overlap.At(1, 2) != 0 || overlap.At(8, 2) != 0 {
		t.Errorf("overlap unexpected: (5,2)=%v (1,2)=%v (8,2)=%v", overlap.At(5, 2), overlap.At(1, 2), overlap.At(8, 2))
	}

	// Weighted Combine with self-aliasing: threat = 0.5*enemy + 0.25*tension.
	threat := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	threat.CopyFrom(enemy)
	threat.Combine(
		ai.WeightedLayer{Layer: threat, Weight: 0.5},
		ai.WeightedLayer{Layer: tension, Weight: 0.25},
	)
	for r := range 6 {
		for c := range 10 {
			want := 0.5*enemy.At(c, r) + 0.25*tension.At(c, r)
			if got := threat.At(c, r); !almostEqual32(got, want, 1e-5) {
				t.Fatalf("combined threat(%d,%d) = %v, want %v", c, r, got, want)
			}
		}
	}

	// Normalize preserves 0 as neutral and maps signed control into [-1, 1].
	normControl := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	normControl.CopyFrom(control)
	normControl.Normalize()
	if got := normControl.At(8, 2); !almostEqual32(got, -1, 1e-6) {
		t.Errorf("normalized enemy peak = %v, want -1", got)
	}
	if got := normControl.At(4, 0); got != 0 {
		t.Errorf("normalized neutral cell = %v, want 0", got)
	}
	if got := normControl.At(1, 2); !(got > 0 && got <= 1) {
		t.Errorf("normalized friendly peak = %v, want in (0, 1]", got)
	}

	// Clamp and AddScaled check.
	clamped := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	clamped.CopyFrom(control)
	clamped.Clamp(-2, 2)
	if clamped.At(1, 2) != 2 || clamped.At(8, 2) != -2 {
		t.Errorf("Clamp(-2, 2) gave (1,2)=%v (8,2)=%v", clamped.At(1, 2), clamped.At(8, 2))
	}
	clamped.AddScaled(friendly, 0.5)
	if got := clamped.At(1, 2); !almostEqual32(got, 7, 1e-6) {
		t.Errorf("AddScaled (1,2) = %v, want 7", got)
	}
}

func TestInfluenceLeastThreatenedReachableCell(t *testing.T) {
	// 8x5 arena with a solid wall completely dividing col 4 (rows 0..4).
	// Enemy threat is at (2, 2) on the left side with radius 4.
	// Unit stands at (3, 2).
	// Cell (5, 2) is within Euclidean radius 3 of the unit and has threat 0,
	// BUT it is on the OTHER side of the solid wall — unreachable!
	// On the unit's side of the wall, within radius 3 of (3,2):
	//   - (0, 2) is at dist 3 from unit (3,2), dist 2 from enemy (2,2) -> threat = 10*(1 - 2/4) = 5.0
	//   - (1, 0) is at dist sqrt(8)≈2.83 from unit, dist sqrt(5)≈2.236 from enemy -> threat = 10*(1 - sqrt(5)/4) ≈ 4.41
	//   - (3, 0) is at dist 2 from unit, dist sqrt(5)≈2.236 from enemy -> threat ≈ 4.41
	//   - (3, 4) is at dist 2 from unit, dist sqrt(5)≈2.236 from enemy -> threat ≈ 4.41
	//   - (1, 4) is at dist sqrt(8) from unit, dist sqrt(5) from enemy -> threat ≈ 4.41
	// Wait: let's add a second threat at (3, 4) so (3, 0) is uniquely the least-threatened reachable cell!
	grid := ai.NewBoolGrid(8, 5)
	for r := range 5 {
		grid.SetWalkable(4, r, false)
	}

	threat := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	threat.AddSource(ai.InfluenceSource{Cell: ai.Cell{Col: 2, Row: 2}, Strength: 10, Radius: 4, Falloff: ai.FalloffLinear})
	threat.AddSource(ai.InfluenceSource{Cell: ai.Cell{Col: 2, Row: 4}, Strength: 6, Radius: 3, Falloff: ai.FalloffLinear})
	threat.Update(grid)

	unitCell := ai.Cell{Col: 3, Row: 2}

	// Without Reachable, the query picks (5, 2) across the wall because its threat is 0.
	unreachCell, unreachVal, ok := threat.LowestInRadius(unitCell, 3, ai.InfluenceQuery{
		Grid:      grid,
		Reachable: false,
	})
	if !ok {
		t.Fatal("LowestInRadius(Reachable=false) returned ok=false")
	}
	if unreachCell.Col <= 4 || unreachVal != 0 {
		t.Fatalf("LowestInRadius(Reachable=false) = (%v, %v), want cell across wall with threat 0", unreachCell, unreachVal)
	}

	// With Reachable=true, the query cannot cross the wall at col 4.
	// Among reachable cells within radius 3 of (3,2):
	// (3, 0) has dist from (2,2) = sqrt(5) -> threat 10*(1 - sqrt(5)/4) ≈ 4.4098, dist from (2,4) = sqrt(17) > 3 -> +0.
	// (1, 0) has the exact same threat ≈ 4.4098, but (3,0) is closer to unit (dist^2 = 4 vs 8), so deterministic tie-break picks (3, 0)!
	// Wait: check (0, 2): dist from (2,2) = 2 -> threat 5.0; (2, 0): dist from (2,2) = 2 -> threat 5.0.
	// What about (3, 0) vs (0, 1)? dist((0,1),(3,2)) = sqrt(10) > 3 (outside radius).
	safeCell, safeVal, ok := threat.LowestInRadius(unitCell, 3, ai.InfluenceQuery{
		Grid:      grid,
		Reachable: true,
	})
	if !ok {
		t.Fatal("LowestInRadius(Reachable=true) returned ok=false")
	}
	wantCell := ai.Cell{Col: 3, Row: 0}
	wantVal := threat.At(wantCell.Col, wantCell.Row)
	if safeCell != wantCell || !almostEqual32(safeVal, wantVal, 1e-6) {
		t.Fatalf("LowestInRadius(Reachable=true) = (%v, %v), want (%v, %v)", safeCell, safeVal, wantCell, wantVal)
	}

	// Confirm the cell is genuinely reachable by A*.
	if _, _, reachable := ai.NewAStar().Find(grid, unitCell, safeCell); !reachable {
		t.Fatalf("returned safe cell %v is not reachable from %v", safeCell, unitCell)
	}

	// HighestInRadius near unit must return the threat peak (2, 2).
	peakCell, peakVal, ok := threat.HighestInRadius(unitCell, 3, ai.InfluenceQuery{
		Grid:      grid,
		Reachable: true,
	})
	if !ok || peakCell != (ai.Cell{Col: 2, Row: 2}) || peakVal <= 10 {
		t.Fatalf("HighestInRadius = (%v, %v, %v), want ({2 2}, >10, true)", peakCell, peakVal, ok)
	}

	// ExcludeCenter omits the center cell even when it is the extremum.
	exCell, _, ok := threat.HighestInRadius(ai.Cell{Col: 2, Row: 2}, 1, ai.InfluenceQuery{
		Grid:          grid,
		Reachable:     true,
		ExcludeCenter: true,
	})
	if !ok || exCell == (ai.Cell{Col: 2, Row: 2}) {
		t.Fatalf("ExcludeCenter returned (%v, %v), want non-center cell", exCell, ok)
	}

	// Blocked center with Reachable=true returns false.
	if _, _, ok := threat.LowestInRadius(ai.Cell{Col: 4, Row: 2}, 3, ai.InfluenceQuery{Grid: grid, Reachable: true}); ok {
		t.Fatal("LowestInRadius from blocked cell returned ok=true, want false")
	}
}

func TestInfluenceSampleAndGradient(t *testing.T) {
	// Linear ramp f(col, row) = 2*col + 4*row with Origin=(10, 20), CellSize=2.
	// In world coordinates: df/dx = 2/2 = 1, df/dy = 4/2 = 2 everywhere!
	l := ai.NewInfluenceLayer(6, 5)
	l.Origin = math2d.V2(10, 20)
	l.CellSize = 2
	for r := range 5 {
		for c := range 6 {
			l.Set(c, r, float32(2*c+4*r))
		}
	}

	// Exact values at cell centers and exact agreement between Gradient and GradientAtCell.
	for r := range 5 {
		for c := range 6 {
			center := l.CellCenter(ai.Cell{Col: c, Row: r})
			cell, in := l.CellAt(center)
			if !in || cell != (ai.Cell{Col: c, Row: r}) {
				t.Fatalf("CellAt(CellCenter(%d,%d)) = (%v, %v)", c, r, cell, in)
			}
			v, ok := l.Sample(center)
			if !ok || !almostEqual32(v, float32(2*c+4*r), 1e-5) {
				t.Fatalf("Sample(center %d,%d) = (%v, %v), want %v", c, r, v, ok, 2*c+4*r)
			}
			gc := l.GradientAtCell(c, r)
			gp := l.Gradient(center)
			if !gc.ApproxEqual(math2d.V2(1, 2), 1e-5) {
				t.Fatalf("GradientAtCell(%d,%d) = %v, want (1, 2)", c, r, gc)
			}
			if !gp.ApproxEqual(gc, 1e-5) {
				t.Fatalf("Gradient(center %d,%d) = %v, want %v", c, r, gp, gc)
			}
		}
	}

	// Midpoint between (1,1), (2,1), (1,2), (2,2):
	// Centers are (13,23), (15,23), (13,25), (15,25); midpoint is (14, 24).
	// Average of 6, 8, 10, 12 = 9.
	mid := math2d.V2(14, 24)
	if v, ok := l.Sample(mid); !ok || !almostEqual32(v, 9, 1e-5) {
		t.Fatalf("Sample(midpoint) = (%v, %v), want (9, true)", v, ok)
	}
	if g := l.Gradient(mid); !g.ApproxEqual(math2d.V2(1, 2), 1e-5) {
		t.Fatalf("Gradient(midpoint) = %v, want (1, 2)", g)
	}

	// Symmetric peak has zero gradient at its center, and gradient points toward peak from neighbors.
	peak := ai.NewInfluenceLayer(5, 5)
	peak.Stamp(nil, ai.InfluenceSource{Cell: ai.Cell{Col: 2, Row: 2}, Strength: 10, Radius: 3, Falloff: ai.FalloffLinear})
	if g := peak.Gradient(peak.CellCenter(ai.Cell{Col: 2, Row: 2})); !g.ApproxEqual(math2d.Zero(), 1e-6) {
		t.Fatalf("Gradient at symmetric peak = %v, want (0, 0)", g)
	}
	// From left of peak (1, 2), uphill gradient must point +X.
	if g := peak.Gradient(peak.CellCenter(ai.Cell{Col: 1, Row: 2})); !(g.X > 0 && almostEqual32(g.Y, 0, 1e-6)) {
		t.Fatalf("Gradient left of peak = %v, want (+X, 0)", g)
	}

	// Out-of-bounds returns false / zero.
	if v, ok := l.Sample(math2d.V2(-1, 25)); ok || v != 0 {
		t.Fatalf("Sample(out of bounds) = (%v, %v), want (0, false)", v, ok)
	}
	if g := l.Gradient(math2d.V2(999, 999)); g != math2d.Zero() {
		t.Fatalf("Gradient(out of bounds) = %v, want (0, 0)", g)
	}
}

func TestInfluenceZeroAllocsAfterWarmup(t *testing.T) {
	grid := ai.NewBoolGrid(32, 32)
	grid.SetWalkable(16, 16, false)

	friendly := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	enemy := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	control := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	tension := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)

	var movingID ai.InfluenceSourceID
	for i := range 20 {
		id := friendly.AddSource(ai.InfluenceSource{
			Cell:     ai.Cell{Col: (i * 3) % 30, Row: (i * 5) % 30},
			Strength: 10,
			Radius:   4,
			Falloff:  ai.FalloffLinear,
		})
		if i == 0 {
			movingID = id
		}
	}
	enemy.AddSource(ai.InfluenceSource{Cell: ai.Cell{Col: 24, Row: 24}, Strength: 12, Radius: 5, Falloff: ai.FalloffExponential, FalloffRate: 0.5})

	// Warm up internal buffers.
	friendly.Update(grid)
	enemy.Update(grid)
	enemy.Spread(grid, ai.SpreadOptions{Decay: 0.2, Momentum: 0.5, Iterations: 2, Diagonal: true})
	control.Subtract(friendly, enemy)
	tension.Add(friendly, enemy)
	_, _, _ = enemy.LowestInRadius(ai.Cell{Col: 10, Row: 10}, 5, ai.InfluenceQuery{Grid: grid, Reachable: true})

	posA := ai.Cell{Col: 4, Row: 4}
	posB := ai.Cell{Col: 5, Row: 4}
	toggle := false

	allocsUpdate := testing.AllocsPerRun(50, func() {
		toggle = !toggle
		if toggle {
			friendly.MoveSource(movingID, posA)
		} else {
			friendly.MoveSource(movingID, posB)
		}
		friendly.Update(grid)
	})
	if allocsUpdate != 0 {
		t.Errorf("incremental MoveSource+Update allocated %v times/run, want 0", allocsUpdate)
	}

	allocsSpread := testing.AllocsPerRun(50, func() {
		enemy.Spread(grid, ai.SpreadOptions{Decay: 0.2, Momentum: 0.5, Iterations: 2, Diagonal: true})
	})
	if allocsSpread != 0 {
		t.Errorf("Spread allocated %v times/run, want 0", allocsSpread)
	}

	allocsCombine := testing.AllocsPerRun(50, func() {
		control.Subtract(friendly, enemy)
		tension.Add(friendly, enemy)
		tension.Combine(
			ai.WeightedLayer{Layer: friendly, Weight: 0.6},
			ai.WeightedLayer{Layer: enemy, Weight: 0.4},
		)
		tension.Normalize()
	})
	if allocsCombine != 0 {
		t.Errorf("layer combinations allocated %v times/run, want 0", allocsCombine)
	}

	allocsQuery := testing.AllocsPerRun(50, func() {
		_, _, _ = enemy.LowestInRadius(ai.Cell{Col: 10, Row: 10}, 5, ai.InfluenceQuery{Grid: grid, Reachable: true})
		_, _, _ = enemy.HighestInRadius(ai.Cell{Col: 10, Row: 10}, 5, ai.InfluenceQuery{Grid: grid, Reachable: false})
		_, _ = enemy.Sample(math2d.V2(168, 168))
		_ = enemy.Gradient(math2d.V2(168, 168))
	})
	if allocsQuery != 0 {
		t.Errorf("spatial queries allocated %v times/run, want 0", allocsQuery)
	}
}

func TestInfluenceHeatmapDraw(t *testing.T) {
	l := ai.NewInfluenceLayer(10, 8)
	l.Origin = math2d.V2(0, 0)
	l.CellSize = 16
	l.Set(1, 1, 10)
	l.Set(2, 1, -8)
	l.Set(9, 7, 10) // outside camera view below

	cam := &tilemap.Camera{
		X:      32,
		Y:      24,
		Zoom:   1,
		Width:  64,
		Height: 48,
	}

	var d core.Drawing
	l.DrawHeatmap(&d, cam, ai.HeatmapOptions{DrawGrid: true})

	if !ai.InfluenceHeatmapEnabled() {
		// Release build: DrawHeatmap must compile out to a strict no-op.
		if len(d.Ops) != 0 {
			t.Fatalf("release build DrawHeatmap emitted %d ops, want 0", len(d.Ops))
		}
		return
	}

	// Default (!release) build: verify positive & negative cells inside cam were drawn and (9,7) was culled.
	var fillRects, strokes int
	var fillColors []core.Color
	for _, op := range d.Ops {
		switch op.Kind {
		case core.DrawFillColor:
			fillColors = append(fillColors, op.Color)
		case core.DrawRect:
			fillRects++
		case core.DrawStroke:
			strokes++
		}
	}
	if fillRects != 2 {
		t.Fatalf("DrawHeatmap with camera culling drew %d cells, want 2 (culled offscreen cell (9,7))", fillRects)
	}
	if len(fillColors) != 2 || fillColors[0] == fillColors[1] {
		t.Fatalf("expected distinct positive and negative heatmap fill colors, got %x", fillColors)
	}
	if strokes != 1 {
		t.Fatalf("DrawHeatmap(DrawGrid=true) emitted %d stroke ops, want 1", strokes)
	}

	// Drawing without camera draws all 3 non-zero cells.
	var dFull core.Drawing
	l.DrawHeatmap(&dFull, nil, ai.HeatmapOptions{})
	fullRects := 0
	for _, op := range dFull.Ops {
		if op.Kind == core.DrawRect {
			fullRects++
		}
	}
	if fullRects != 3 {
		t.Fatalf("DrawHeatmap(cam=nil) drew %d cells, want 3", fullRects)
	}

	// All-zero layer emits 0 ops when DrawGrid is false.
	empty := ai.NewInfluenceLayer(4, 4)
	var dEmpty core.Drawing
	empty.DrawHeatmap(&dEmpty, cam, ai.HeatmapOptions{})
	if len(dEmpty.Ops) != 0 {
		t.Fatalf("empty layer DrawHeatmap emitted %d ops, want 0", len(dEmpty.Ops))
	}
}

func TestInfluenceGlobalExtremaAndPostSpreadUpdate(t *testing.T) {
	grid := ai.NewBoolGrid(6, 5)
	grid.SetWalkable(3, 2, false)

	inc := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 1)
	ref := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 1)

	idA := inc.AddSource(ai.InfluenceSource{Cell: ai.Cell{Col: 1, Row: 1}, Strength: 8, Radius: 2})
	idB := inc.AddSource(ai.InfluenceSource{Cell: ai.Cell{Col: 4, Row: 3}, Strength: -5, Radius: 2})
	ref.AddSource(ai.InfluenceSource{Cell: ai.Cell{Col: 1, Row: 1}, Strength: 8, Radius: 2})
	refB := ref.AddSource(ai.InfluenceSource{Cell: ai.Cell{Col: 4, Row: 3}, Strength: -5, Radius: 2})

	inc.Update(grid)
	inc.Spread(grid, ai.SpreadOptions{Decay: 0.25, Momentum: 0.3, Iterations: 2, Diagonal: true})

	// Calling Update after Spread (with or without a source move) must restore exact unspread Rebuild parity.
	inc.MoveSource(idB, ai.Cell{Col: 5, Row: 3})
	ref.MoveSource(refB, ai.Cell{Col: 5, Row: 3})
	inc.Update(grid)
	ref.Rebuild(grid)
	assertLayerBitIdentical(t, inc, ref)

	// Calling Update after Fill + MoveSource must also match Rebuild bit-for-bit.
	inc.Fill(99)
	inc.MoveSource(idA, ai.Cell{Col: 0, Row: 0})
	inc.Update(grid)
	ref.ClearSources()
	ref.AddSource(ai.InfluenceSource{Cell: ai.Cell{Col: 0, Row: 0}, Strength: 8, Radius: 2})
	ref.AddSource(ai.InfluenceSource{Cell: ai.Cell{Col: 5, Row: 3}, Strength: -5, Radius: 2})
	ref.Rebuild(grid)
	assertLayerBitIdentical(t, inc, ref)

	// Global Highest / Lowest respect walkability and return exact extrema.
	hiCell, hiVal, ok := inc.Highest(grid)
	if !ok || hiCell != (ai.Cell{Col: 0, Row: 0}) || hiVal != 8 {
		t.Fatalf("Highest = (%v, %v, %v), want ({0 0}, 8, true)", hiCell, hiVal, ok)
	}
	loCell, loVal, ok := inc.Lowest(grid)
	if !ok || loCell != (ai.Cell{Col: 5, Row: 3}) || loVal != -5 {
		t.Fatalf("Lowest = (%v, %v, %v), want ({5 3}, -5, true)", loCell, loVal, ok)
	}

	// Scale and ClearSources.
	inc.Scale(0.5)
	if got := inc.At(0, 0); got != 4 {
		t.Fatalf("after Scale(0.5), (0,0) = %v, want 4", got)
	}
	inc.ClearSources()
	if inc.SourceCount() != 0 {
		t.Fatalf("SourceCount after ClearSources = %d, want 0", inc.SourceCount())
	}
	inc.Update(grid)
	if got := inc.At(0, 0); got != 0 {
		t.Fatalf("after ClearSources+Update, (0,0) = %v, want 0", got)
	}
}

func BenchmarkInfluenceIncrementalMove(b *testing.B) {
	grid := ai.NewBoolGrid(64, 64)
	l := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	var id ai.InfluenceSourceID
	for i := range 40 {
		sid := l.AddSource(ai.InfluenceSource{
			Cell:     ai.Cell{Col: (i * 7) % 64, Row: (i * 11) % 64},
			Strength: 10,
			Radius:   5,
			Falloff:  ai.FalloffLinear,
		})
		if i == 0 {
			id = sid
		}
	}
	l.Update(grid)

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		l.MoveSource(id, ai.Cell{Col: 10 + (i % 4), Row: 10})
		l.Update(grid)
	}
}

func BenchmarkInfluenceRebuild(b *testing.B) {
	grid := ai.NewBoolGrid(64, 64)
	l := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	for i := range 40 {
		l.AddSource(ai.InfluenceSource{
			Cell:     ai.Cell{Col: (i * 7) % 64, Row: (i * 11) % 64},
			Strength: 10,
			Radius:   5,
			Falloff:  ai.FalloffLinear,
		})
	}
	l.Update(grid)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		l.Rebuild(grid)
	}
}

func BenchmarkInfluenceSpread(b *testing.B) {
	grid := ai.NewBoolGrid(64, 64)
	l := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	l.AddSource(ai.InfluenceSource{Cell: ai.Cell{Col: 10, Row: 10}, Strength: 10, Radius: 4})
	l.Update(grid)
	opts := ai.SpreadOptions{Decay: 0.2, Momentum: 0.5, Iterations: 1, Diagonal: true}
	l.Spread(grid, opts)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		l.Spread(grid, opts)
	}
}
