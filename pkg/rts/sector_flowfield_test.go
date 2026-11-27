package rts_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/game/pkg/rts"
)

func TestSectorFlowFieldAlongHPADescendsToGoal(t *testing.T) {
	g := pathfind.NewCostMap(128, 128)
	for r := 0; r < 110; r++ {
		g.SetWalkable(40, r, false)
	}
	for r := 18; r < 128; r++ {
		g.SetWalkable(88, r, false)
	}
	// Add a weighted mud strip to confirm CostGrid integration.
	for r := 110; r < 120; r++ {
		g.SetCost(40, r, 3)
	}

	hpa := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 16, Diagonal: true})
	start := pathfind.Cell{Col: 4, Row: 10}
	goal := pathfind.Cell{Col: 120, Row: 120}

	sf := rts.NewSectorFlowField(math2d.Vec2{X: 10, Y: 20}, 8)
	if !sf.BuildAlongHPA(hpa, start, goal, 0) {
		t.Fatal("BuildAlongHPA failed")
	}
	if sf.ActiveSectors() >= 64 {
		t.Fatalf("expected far fewer active sectors than all 64, got %d", sf.ActiveSectors())
	}
	if !sf.Reachable(start.Col, start.Row) {
		t.Fatal("start cell must be reachable inside HPA corridor")
	}

	// Cells far outside the corridor must be inactive and unreachable.
	if sf.SectorActive(120, 4) || sf.Reachable(120, 4) {
		t.Fatal("far corner outside corridor should not be active")
	}

	// Walking directions from start must strictly descend to goal.
	col, row := start.Col, start.Row
	for steps := 0; sf.Cost(col, row) > 0; steps++ {
		if steps > 1000 {
			t.Fatalf("failed to reach goal from (%d,%d), stuck at (%d,%d)", start.Col, start.Row, col, row)
		}
		d := sf.Direction(col, row)
		if d == (math2d.Vec2{}) {
			t.Fatalf("zero direction at non-goal (%d,%d)", col, row)
		}
		nc := col + int(math.Round(float64(d.X)))
		nr := row + int(math.Round(float64(d.Y)))
		if !(sf.Cost(nc, nr) < sf.Cost(col, row)) {
			t.Fatalf("direction at (%d,%d) did not descend: %v -> %v", col, row, sf.Cost(col, row), sf.Cost(nc, nr))
		}
		col, row = nc, nr
	}

	// Sample at start world center returns a unit direction; sample at goal returns zero vector with ok=true.
	if dir, ok := sf.Sample(sf.CellCenter(start)); !ok || !near(dir.Len(), 1) {
		t.Fatalf("Sample at start = (%v, %v), want unit vector", dir, ok)
	}
	if dir, ok := sf.Sample(sf.CellCenter(goal)); !ok || dir != (math2d.Vec2{}) {
		t.Fatalf("Sample at goal = (%v, %v), want zero vector", dir, ok)
	}
	if _, ok := sf.Sample(sf.CellCenter(pathfind.Cell{Col: 120, Row: 4})); ok {
		t.Fatal("Sample outside active sectors reported ok")
	}
}

func TestSectorFlowFieldPaddingAndZeroAllocRebuild(t *testing.T) {
	g := pathfind.NewBoolGrid(128, 128)
	hpa := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 16, Diagonal: true})
	start := pathfind.Cell{Col: 8, Row: 8}
	goal := pathfind.Cell{Col: 120, Row: 8}

	sf := rts.NewSectorFlowField(math2d.Vec2{}, 1)
	if !sf.BuildAlongHPA(hpa, start, goal, 0) {
		t.Fatal("padding=0 failed")
	}
	n0 := sf.ActiveSectors()
	if !sf.BuildAlongHPA(hpa, start, goal, 1) {
		t.Fatal("padding=1 failed")
	}
	n1 := sf.ActiveSectors()
	if n1 <= n0 {
		t.Fatalf("padding=1 active sectors (%d) should exceed padding=0 (%d)", n1, n0)
	}

	// Warm up both HPA and SectorFlowField, then verify zero allocations on rebuild.
	allocs := testing.AllocsPerRun(20, func() {
		if !sf.BuildAlongHPA(hpa, start, goal, 0) {
			t.Fatal("BuildAlongHPA failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("SectorFlowField.BuildAlongHPA allocated %v times per run, want 0", allocs)
	}
}

func TestSectorFlowFieldEdgeCases(t *testing.T) {
	sf := rts.NewSectorFlowField(math2d.Vec2{}, 1)
	if sf.BuildAlongHPA(nil, pathfind.Cell{}, pathfind.Cell{}, 0) {
		t.Fatal("nil HPA should fail")
	}
	g := pathfind.NewBoolGrid(32, 32)
	for r := range 32 {
		g.SetWalkable(16, r, false)
	}
	hpa := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 8, Diagonal: true})
	if sf.BuildAlongHPA(hpa, pathfind.Cell{Col: 1, Row: 1}, pathfind.Cell{Col: 30, Row: 30}, 0) {
		t.Fatal("unreachable goal should fail")
	}
	if sf.ActiveSectors() != 0 {
		t.Fatalf("expected 0 active sectors on failed build, got %d", sf.ActiveSectors())
	}
}

func openObstaclesGrid(cols, rows int) *pathfind.BoolGrid {
	g := pathfind.NewBoolGrid(cols, rows)
	// Room walls every 128 cols/rows with generous doorways so long corridors span a narrow subset of sectors.
	for c := 128; c < cols; c += 128 {
		for r := range rows {
			if (r % 128) < 24 {
				continue
			}
			g.SetWalkable(c, r, false)
		}
	}
	for r := 128; r < rows; r += 128 {
		for c := range cols {
			if (c % 128) < 24 {
				continue
			}
			g.SetWalkable(c, r, false)
		}
	}
	return g
}

func BenchmarkSectorFlowField1024(b *testing.B) {
	g := openObstaclesGrid(1024, 1024)
	hpa := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 32, Diagonal: true})
	sf := rts.NewSectorFlowField(math2d.Vec2{}, 1)
	start := pathfind.Cell{Col: 4, Row: 4}
	goal := pathfind.Cell{Col: 1018, Row: 1018}
	if !sf.BuildAlongHPA(hpa, start, goal, 0) {
		b.Fatal("BuildAlongHPA failed")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sf.BuildAlongHPA(hpa, start, goal, 0)
	}
}

func BenchmarkWholeFlowField1024(b *testing.B) {
	g := openObstaclesGrid(1024, 1024)
	f := rts.NewFlowField(math2d.Vec2{}, 1)
	goal := pathfind.Cell{Col: 1018, Row: 1018}
	if !f.Build(g, goal) {
		b.Fatal("Build failed")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		f.Build(g, goal)
	}
}
