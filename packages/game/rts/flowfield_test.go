package rts_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/rts"
)

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

func TestFlowFieldCostsMatchAStar(t *testing.T) {
	g := ai.NewCostMap(12, 9)
	for r := 1; r < 8; r++ {
		g.SetWalkable(5, r, false)
	}
	g.SetCost(8, 4, 3)
	g.SetCost(9, 4, 3)
	goal := ai.Cell{Col: 10, Row: 4}
	f := rts.NewFlowField(math2d.Vec2{}, 1)
	if !f.Build(g, goal) {
		t.Fatal("Build reported no valid goal")
	}
	astar := ai.NewAStar()
	astar.Diagonal = true
	cols, rows := g.GridSize()
	for r := range rows {
		for c := range cols {
			_, cost, ok := astar.Find(g, ai.Cell{Col: c, Row: r}, goal)
			got := f.Cost(c, r)
			if !ok {
				if got != rts.Unreachable {
					t.Fatalf("cell (%d,%d) = %v, want unreachable", c, r, got)
				}
				continue
			}
			if !near(got, float32(cost)) {
				t.Fatalf("cell (%d,%d) cost = %v, A* = %v", c, r, got, cost)
			}
		}
	}
}

func TestFlowFieldDirectionsDescend(t *testing.T) {
	g := ai.NewBoolGrid(16, 16)
	for c := 2; c < 14; c++ {
		g.SetWalkable(c, 8, false)
	}
	f := rts.NewFlowField(math2d.Vec2{}, 1)
	f.Build(g, ai.Cell{Col: 8, Row: 2})
	// Following directions from any reachable cell reaches the goal, each step lowering the cost.
	for r := range 16 {
		for c := range 16 {
			if !f.Reachable(c, r) {
				continue
			}
			col, row := c, r
			for steps := 0; f.Cost(col, row) > 0; steps++ {
				if steps > 64 {
					t.Fatalf("no goal reached from (%d,%d)", c, r)
				}
				d := f.Direction(col, row)
				nc := col + int(math.Round(float64(d.X)))
				nr := row + int(math.Round(float64(d.Y)))
				if !(f.Cost(nc, nr) < f.Cost(col, row)) {
					t.Fatalf("direction at (%d,%d) does not descend: %v -> %v", col, row, f.Cost(col, row), f.Cost(nc, nr))
				}
				col, row = nc, nr
			}
		}
	}
}

func TestFlowFieldNoCornerCutting(t *testing.T) {
	g := ai.NewBoolGrid(3, 3)
	g.SetWalkable(1, 0, false)
	f := rts.NewFlowField(math2d.Vec2{}, 1)
	f.Build(g, ai.Cell{Col: 2, Row: 0})
	// (1,1) may not step diagonally past the blocked (1,0) to reach (2,0); it goes right then up.
	if d := f.Direction(1, 1); d != (math2d.Vec2{X: 1}) {
		t.Fatalf("direction at (1,1) = %v, want right", d)
	}
	if got := f.Cost(0, 0); !near(got, 1+1+math.Sqrt2) && !near(got, 4) {
		t.Fatalf("cost at (0,0) = %v", got)
	}
}

func TestFlowFieldMultipleGoalsAndInvalidGoals(t *testing.T) {
	g := ai.NewBoolGrid(10, 1)
	g.SetWalkable(4, 0, false)
	f := rts.NewFlowField(math2d.Vec2{}, 1)
	if f.Build(g, ai.Cell{Col: 4, Row: 0}, ai.Cell{Col: -1, Row: 0}) {
		t.Fatal("Build with only blocked and out-of-bounds goals reported success")
	}
	if f.Reachable(0, 0) {
		t.Fatal("cell reachable with no goals")
	}
	f.Build(g, ai.Cell{Col: 0, Row: 0}, ai.Cell{Col: 9, Row: 0}, ai.Cell{Col: 9, Row: 0})
	if got := f.Goals(); len(got) != 2 {
		t.Fatalf("Goals = %v, want 2 distinct", got)
	}
	if f.Cost(3, 0) != 3 || f.Cost(5, 0) != 4 {
		t.Fatalf("costs = %v, %v; want 3 and 4 from the nearer goal", f.Cost(3, 0), f.Cost(5, 0))
	}
	if d := f.Direction(5, 0); d.X != 1 {
		t.Fatalf("direction at 5 = %v, want toward the right goal", d)
	}
}

func TestFlowFieldSampleWorldSpace(t *testing.T) {
	g := ai.NewBoolGrid(8, 8)
	f := rts.NewFlowField(math2d.Vec2{X: 100, Y: 50}, 16)
	f.Build(g, ai.Cell{Col: 7, Row: 3})
	if c, ok := f.CellAt(math2d.Vec2{X: 100 + 16*2 + 1, Y: 50 + 16*3 + 15}); !ok || c != (ai.Cell{Col: 2, Row: 3}) {
		t.Fatalf("CellAt = %v, %v", c, ok)
	}
	if _, ok := f.CellAt(math2d.Vec2{X: 99, Y: 60}); ok {
		t.Fatal("CellAt outside reported inside")
	}
	if p := f.CellCenter(ai.Cell{Col: 1, Row: 2}); p != (math2d.Vec2{X: 124, Y: 90}) {
		t.Fatalf("CellCenter = %v", p)
	}
	dir, ok := f.Sample(f.CellCenter(ai.Cell{Col: 1, Row: 3}))
	if !ok || !near(dir.X, 1) || !near(dir.Y, 0) {
		t.Fatalf("Sample on the goal row = %v, %v; want right", dir, ok)
	}
	dir, ok = f.Sample(math2d.Vec2{X: 100 + 16*0.5, Y: 50 + 16*7.2})
	if !ok || !(dir.X > 0 && dir.Y < 0) || !near(dir.Len(), 1) {
		t.Fatalf("Sample below-left = %v, %v; want a unit vector up and right", dir, ok)
	}
	if dir, ok := f.Sample(f.CellCenter(ai.Cell{Col: 7, Row: 3})); !ok || dir != (math2d.Vec2{}) {
		t.Fatalf("Sample on goal = %v, %v", dir, ok)
	}
	if _, ok := f.Sample(math2d.Vec2{X: -5, Y: 0}); ok {
		t.Fatal("Sample outside the grid reported ok")
	}
}

func TestFlowFieldSampleIgnoresWalls(t *testing.T) {
	g := ai.NewBoolGrid(6, 6)
	for r := range 5 {
		g.SetWalkable(3, r, false)
	}
	f := rts.NewFlowField(math2d.Vec2{}, 1)
	f.Build(g, ai.Cell{Col: 5, Row: 0})
	// Beside the wall, the blend must still lead down around it, never into it.
	dir, ok := f.Sample(math2d.Vec2{X: 2.95, Y: 1.5})
	if !ok || dir.Y <= 0 {
		t.Fatalf("Sample beside the wall = %v, %v; want downward", dir, ok)
	}
}

func TestFlowFieldRebuildDoesNotAllocate(t *testing.T) {
	g := ai.NewCostMap(64, 64)
	f := rts.NewFlowField(math2d.Vec2{}, 1)
	f.Build(g, ai.Cell{Col: 32, Row: 32})
	allocs := testing.AllocsPerRun(5, func() { f.Build(g, ai.Cell{Col: 10, Row: 50}) })
	if allocs != 0 {
		t.Fatalf("rebuild allocated %v times", allocs)
	}
}

func TestFieldCacheSharesAndEvicts(t *testing.T) {
	g := ai.NewBoolGrid(10, 10)
	fc := rts.NewFieldCache(g, math2d.Vec2{}, 1, 2)
	a, ok := fc.Field(ai.Cell{Col: 1, Row: 1}, ai.Cell{Col: 2, Row: 2})
	if !ok {
		t.Fatal("Field failed")
	}
	b, _ := fc.Field(ai.Cell{Col: 2, Row: 2}, ai.Cell{Col: 1, Row: 1}, ai.Cell{Col: 1, Row: 1})
	if a != b || fc.Builds() != 1 {
		t.Fatalf("same goal set built twice (builds %d)", fc.Builds())
	}
	fc.Field(ai.Cell{Col: 5, Row: 5})
	fc.Field(ai.Cell{Col: 1, Row: 1}, ai.Cell{Col: 2, Row: 2}) // touch a
	fc.Field(ai.Cell{Col: 9, Row: 9})                          // evicts (5,5)
	if fc.Len() != 2 {
		t.Fatalf("Len = %d, want 2", fc.Len())
	}
	before := fc.Builds()
	if c, _ := fc.Field(ai.Cell{Col: 1, Row: 1}, ai.Cell{Col: 2, Row: 2}); c != a || fc.Builds() != before {
		t.Fatal("recently used field was evicted")
	}
	fc.Field(ai.Cell{Col: 5, Row: 5})
	if fc.Builds() != before+1 {
		t.Fatal("least recently used field was not evicted")
	}
	if _, ok := fc.Field(ai.Cell{Col: 20, Row: 20}); ok {
		t.Fatal("Field with an out-of-bounds goal succeeded")
	}
}

func TestFieldCacheRebuildFollowsGridChanges(t *testing.T) {
	g := ai.NewBoolGrid(5, 1)
	fc := rts.NewFieldCache(g, math2d.Vec2{}, 1, 0)
	f, _ := fc.Field(ai.Cell{Col: 4, Row: 0})
	if !f.Reachable(0, 0) {
		t.Fatal("open corridor unreachable")
	}
	g.SetWalkable(2, 0, false)
	fc.Rebuild()
	if f.Reachable(0, 0) {
		t.Fatal("Rebuild kept a path through the new wall")
	}
	fc.Clear()
	if fc.Len() != 0 {
		t.Fatal("Clear kept fields")
	}
}

func BenchmarkFlowFieldBuild256(b *testing.B) {
	g := mazeGrid(256, 256)
	f := rts.NewFlowField(math2d.Vec2{}, 1)
	b.ReportAllocs()
	for b.Loop() {
		f.Build(g, ai.Cell{Col: 250, Row: 128})
	}
}

// mazeGrid returns an open grid crossed by vertical walls every 16 columns, each with two gaps.
func mazeGrid(cols, rows int) *ai.CostMap {
	g := ai.NewCostMap(cols, rows)
	for c := 16; c < cols-8; c += 16 {
		for r := range rows {
			gap := (c/16)%3*rows/4 + rows/8
			if r >= gap && r < gap+6 || r >= rows-gap-6 && r < rows-gap {
				continue
			}
			g.SetWalkable(c, r, false)
		}
	}
	return g
}

// halfCost is a game-defined cost grid whose roads cost less than an open cell.
type halfCost struct{ *ai.BoolGrid }

func (halfCost) MoveCost(col, row int) float64 { return 0.5 }

func TestFlowFieldSubUnitCostsMatchAStar(t *testing.T) {
	g := halfCost{ai.NewBoolGrid(6, 1)}
	f := rts.NewFlowField(math2d.Vec2{}, 1)
	f.Build(g, ai.Cell{Col: 5, Row: 0})
	_, cost, ok := ai.NewAStar().Find(g, ai.Cell{Col: 0, Row: 0}, ai.Cell{Col: 5, Row: 0})
	if !ok || cost != 2.5 {
		t.Fatalf("A* cost = %v, %v, want 2.5", cost, ok)
	}
	if got := f.Cost(0, 0); !near(got, float32(cost)) {
		t.Fatalf("field cost = %v, A* = %v", got, cost)
	}
}
