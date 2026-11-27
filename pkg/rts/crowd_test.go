package rts_test

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/rts"
)

const step = time.Second / 60

// thousandUnitTargets each put the box formation across one of the 160x128 maze's walls, so some units must plan their own A* detours around it to reach their slots.
var thousandUnitTargets = []math2d.Vec2{{X: 140, Y: 64}, {X: 100, Y: 20}, {X: 60, Y: 100}}

func allArrived(c *rts.Crowd) bool {
	for _, u := range c.Units() {
		if u.State != rts.Arrived {
			return false
		}
	}
	return true
}

// blocked reports whether a body of radius r at p overlaps a blocked cell or leaves the grid, allowing eps of penetration.
func blocked(g ai.Grid, p math2d.Vec2, r, eps float32) bool {
	cols, rows := g.GridSize()
	if p.X < r-eps || p.Y < r-eps || p.X > float32(cols)-r+eps || p.Y > float32(rows)-r+eps {
		return true
	}
	for row := int(p.Y - r - 1); row <= int(p.Y+r+1); row++ {
		for col := int(p.X - r - 1); col <= int(p.X+r+1); col++ {
			if col < 0 || row < 0 || col >= cols || row >= rows || g.Walkable(col, row) {
				continue
			}
			box := math2d.Rect{X: float32(col), Y: float32(row), W: 1, H: 1}
			if box.ClampPoint(p).Dist(p) < r-eps {
				return true
			}
		}
	}
	return false
}

// spawnBlock adds n units in a block of rows of width units, 1 unit apart, starting at origin.
func spawnBlock(c *rts.Crowd, n, width int, origin math2d.Vec2) []rts.UnitID {
	ids := make([]rts.UnitID, 0, n)
	for i := range n {
		p := origin.Add(math2d.Vec2{X: float32(i%width) * 1.0, Y: float32(i/width) * 1.0})
		ids = append(ids, c.Add(rts.UnitSpec{Pos: p, Radius: 0.35, MaxSpeed: 6, Kind: i % 3}))
	}
	return ids
}

func TestCrowdThousandUnitsReachTarget(t *testing.T) {
	for _, target := range thousandUnitTargets {
		t.Run(fmt.Sprintf("%v,%v", target.X, target.Y), func(t *testing.T) {
			moveThousand(t, target)
		})
	}
}

func moveThousand(t *testing.T, target math2d.Vec2) {
	g := mazeGrid(160, 128)
	c := rts.NewCrowd(g, rts.CrowdOptions{Gap: 0.05})
	ids := spawnBlock(c, 1000, 40, math2d.Vec2{X: 2.5, Y: 40.5})

	n := c.Move(ids, rts.MoveOrder{Target: target, Formation: rts.Formation{Shape: rts.ShapeBox, Spacing: 0.9}})
	if n != 1000 {
		t.Fatalf("Move ordered %d units, want 1000", n)
	}
	const maxSteps = 60 * 90
	steps := 0
	for steps < maxSteps {
		c.Step(step)
		steps++
		if allArrived(c) {
			break
		}
	}
	if steps == maxSteps && !allArrived(c) {
		moving := 0
		for _, u := range c.Units() {
			if u.State != rts.Arrived {
				moving++
			}
		}
		t.Fatalf("%d of 1000 units still moving after %d steps", moving, steps)
	}
	t.Logf("1000 units arrived in %d steps (%.1f s of game time)", steps, float64(steps)/60)
	if b := c.Fields().Builds(); b > 2 {
		t.Fatalf("the group built %d flow fields, want them shared", b)
	}
	for _, u := range c.Units() {
		if blocked(g, u.Pos, u.Radius, 0.02) {
			t.Fatalf("unit %d ended inside a wall at %v", u.ID, u.Pos)
		}
		// Arrived units hold their slots, though neighbors settling in may nudge them a little.
		if u.Pos.Dist(u.Target) > 2*u.Radius {
			t.Fatalf("unit %d arrived %v from its slot", u.ID, u.Pos.Dist(u.Target))
		}
	}
}

func TestCrowdIsDeterministic(t *testing.T) {
	run := func() []rts.Unit {
		g := mazeGrid(64, 64)
		c := rts.NewCrowd(g, rts.CrowdOptions{})
		ids := spawnBlock(c, 120, 12, math2d.Vec2{X: 2.5, Y: 20.5})
		c.Move(ids, rts.MoveOrder{Target: math2d.Vec2{X: 56, Y: 30}, Formation: rts.Formation{Shape: rts.ShapeWedge, Spacing: 1}, KeepPace: true})
		for range 300 {
			c.Step(step)
		}
		return append([]rts.Unit(nil), c.Units()...)
	}
	a, b := run(), run()
	for i := range a {
		if a[i].Pos != b[i].Pos || a[i].Vel != b[i].Vel || a[i].State != b[i].State {
			t.Fatalf("unit %d differs between runs: %+v vs %+v", i, a[i].Pos, b[i].Pos)
		}
	}
}

func TestCrowdSeparatesStackedUnits(t *testing.T) {
	c := rts.NewCrowd(ai.NewBoolGrid(20, 20), rts.CrowdOptions{})
	var ids []rts.UnitID
	for range 10 {
		ids = append(ids, c.Add(rts.UnitSpec{Pos: math2d.Vec2{X: 10, Y: 10}, Radius: 0.4}))
	}
	for range 240 {
		c.Step(step)
	}
	us := c.Units()
	for i := range us {
		if us[i].State != rts.Idle {
			t.Fatalf("idle unit changed state to %v", us[i].State)
		}
		for j := i + 1; j < len(us); j++ {
			if d := us[i].Pos.Dist(us[j].Pos); d < 0.6 {
				t.Fatalf("units %d and %d still %v apart", i, j, d)
			}
		}
	}
	_ = ids
}

func TestCrowdSlidesAlongWalls(t *testing.T) {
	g := ai.NewBoolGrid(10, 10)
	for r := range 10 {
		g.SetWalkable(5, r, false)
	}
	c := rts.NewCrowd(g, rts.CrowdOptions{})
	id := c.Add(rts.UnitSpec{Pos: math2d.Vec2{X: 2, Y: 5}, Radius: 0.4})
	// The far side is unreachable, so the order goes to the nearest place on the near side.
	if n := c.Move([]rts.UnitID{id}, rts.MoveOrder{Target: math2d.Vec2{X: 8, Y: 5}}); n != 1 {
		t.Fatalf("Move = %d", n)
	}
	for range 600 {
		c.Step(step)
		u, _ := c.Unit(id)
		if blocked(g, u.Pos, u.Radius, 0.02) {
			t.Fatalf("unit entered the wall at %v", u.Pos)
		}
	}
	u, _ := c.Unit(id)
	if u.State != rts.Arrived || u.Pos.X > 5 {
		t.Fatalf("unit %v at %v, want arrived on the near side", u.State, u.Pos)
	}
}

func TestMoveMovesSlotsOffWalls(t *testing.T) {
	g := ai.NewBoolGrid(30, 30)
	for c := 10; c < 20; c++ {
		for r := 10; r < 20; r++ {
			g.SetWalkable(c, r, false)
		}
	}
	c := rts.NewCrowd(g, rts.CrowdOptions{})
	ids := spawnBlock(c, 25, 5, math2d.Vec2{X: 2.5, Y: 2.5})
	if n := c.Move(ids, rts.MoveOrder{Target: math2d.Vec2{X: 15, Y: 15}}); n != 25 {
		t.Fatalf("Move into a solid block ordered %d units", n)
	}
	cells := map[ai.Cell]int{}
	for _, u := range c.Units() {
		cell := ai.Cell{Col: int(u.Target.X), Row: int(u.Target.Y)}
		if !g.Walkable(cell.Col, cell.Row) || blocked(g, u.Target, u.Radius, 1e-3) {
			t.Fatalf("slot %v is in a wall", u.Target)
		}
		cells[cell]++
	}
	for cell, k := range cells {
		if k > 1 {
			t.Fatalf("%d slots share cell %v", k, cell)
		}
	}
	if n := c.Move(ids, rts.MoveOrder{Target: math2d.Vec2{X: -3, Y: 5}}); n != 0 {
		t.Fatalf("Move outside the grid ordered %d units", n)
	}
	if n := c.Move([]rts.UnitID{999}, rts.MoveOrder{Target: math2d.Vec2{X: 5, Y: 5}}); n != 0 {
		t.Fatalf("Move of unknown units ordered %d", n)
	}
}

func TestMoveFollowsOwnFieldAcrossSplitFormation(t *testing.T) {
	// A wall splits the formation's area; units whose slots lie beyond it must go around rather than stop at the nearest slot cell.
	g := ai.NewBoolGrid(40, 30)
	for r := range 25 {
		g.SetWalkable(20, r, false)
	}
	c := rts.NewCrowd(g, rts.CrowdOptions{})
	ids := spawnBlock(c, 12, 4, math2d.Vec2{X: 3.5, Y: 3.5})
	c.Move(ids, rts.MoveOrder{Target: math2d.Vec2{X: 20.5, Y: 6}, Formation: rts.Formation{Shape: rts.ShapeLine, Spacing: 1.5}, Facing: math2d.Vec2{Y: -1}})
	for range 60 * 60 {
		c.Step(step)
	}
	for _, u := range c.Units() {
		if u.State != rts.Arrived {
			t.Fatalf("unit %d %v at %v, target %v", u.ID, u.State, u.Pos, u.Target)
		}
	}
}

func TestCrowdAddRemoveStopAndQueries(t *testing.T) {
	c := rts.NewCrowd(ai.NewBoolGrid(20, 20), rts.CrowdOptions{CellSize: 2})
	a := c.Add(rts.UnitSpec{Pos: math2d.Vec2{X: 5, Y: 5}, Kind: 1})
	b := c.Add(rts.UnitSpec{Pos: math2d.Vec2{X: 9, Y: 5}, Kind: 2})
	d := c.Add(rts.UnitSpec{Pos: math2d.Vec2{X: 30, Y: 30}, Kind: 1})
	if u, _ := c.Unit(a); u.Radius != 0.5 || u.MaxSpeed != 8 || u.MaxAccel != 32 {
		t.Fatalf("defaults = %+v", u)
	}
	if got := c.UnitsIn(math2d.Rect{X: 10, Y: 0, W: -7, H: 10}, nil); len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("UnitsIn = %v", got)
	}
	if id, ok := c.UnitAt(math2d.Vec2{X: 8.6, Y: 5}, 0); !ok || id != b {
		t.Fatalf("UnitAt = %v, %v", id, ok)
	}
	if _, ok := c.UnitAt(math2d.Vec2{X: 7, Y: 5}, 0.5); ok {
		t.Fatal("UnitAt picked a unit out of reach")
	}
	if k, ok := c.KindOf(d); !ok || k != 1 {
		t.Fatalf("KindOf = %v, %v", k, ok)
	}
	if p, ok := c.Centroid([]rts.UnitID{a, b, 77}); !ok || p != (math2d.Vec2{X: 7, Y: 5}) {
		t.Fatalf("Centroid = %v, %v", p, ok)
	}
	c.Move([]rts.UnitID{a, b}, rts.MoveOrder{Target: math2d.Vec2{X: 20, Y: 20}})
	c.Step(step)
	c.Stop(a)
	if u, _ := c.Unit(a); u.State != rts.Idle || u.Target != u.Pos {
		t.Fatalf("stopped unit = %+v", u)
	}
	if !c.Remove(a) || c.Remove(a) || c.Len() != 2 {
		t.Fatal("Remove misreported")
	}
	if u, ok := c.Unit(d); !ok || u.ID != d {
		t.Fatal("removing a unit lost another")
	}
	if c.Units()[0].ID != d {
		t.Fatal("the last unit did not take the removed unit's place")
	}
	steps := c.Steps()
	c.Step(0)
	if c.Steps() != steps {
		t.Fatal("Step(0) advanced the crowd")
	}
}

func TestCrowdStepDoesNotAllocate(t *testing.T) {
	g := mazeGrid(64, 64)
	c := rts.NewCrowd(g, rts.CrowdOptions{})
	ids := spawnBlock(c, 200, 20, math2d.Vec2{X: 2.5, Y: 20.5})
	c.Move(ids, rts.MoveOrder{Target: math2d.Vec2{X: 56, Y: 30}})
	c.Step(step)
	if allocs := testing.AllocsPerRun(20, func() { c.Step(step) }); allocs != 0 {
		t.Fatalf("Step allocated %v times", allocs)
	}
}

func BenchmarkCrowdThousandUnits(b *testing.B) {
	for _, target := range thousandUnitTargets {
		b.Run(fmt.Sprintf("%v,%v", target.X, target.Y), func(b *testing.B) {
			benchmarkMoveThousand(b, target)
		})
	}
}

func benchmarkMoveThousand(b *testing.B, target math2d.Vec2) {
	g := mazeGrid(160, 128)
	const maxSteps = 60 * 90
	times := make([]time.Duration, 0, maxSteps)
	var lastSteps int
	var totalStep time.Duration
	var p99Sum time.Duration

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		c := rts.NewCrowd(g, rts.CrowdOptions{Gap: 0.05})
		ids := spawnBlock(c, 1000, 40, math2d.Vec2{X: 2.5, Y: 40.5})
		if n := c.Move(ids, rts.MoveOrder{Target: target, Formation: rts.Formation{Shape: rts.ShapeBox, Spacing: 0.9}}); n != 1000 {
			b.Fatalf("Move ordered %d units, want 1000", n)
		}
		times = times[:0]
		b.StartTimer()
		for len(times) < maxSteps {
			t0 := time.Now()
			c.Step(step)
			times = append(times, time.Since(t0))
			if allArrived(c) {
				break
			}
		}
		b.StopTimer()
		steps := len(times)
		if steps == maxSteps && !allArrived(c) {
			b.Fatalf("units still moving after %d steps", steps)
		}
		var iterTotal time.Duration
		for _, d := range times {
			iterTotal += d
		}
		slices.Sort(times)
		p99Sum += times[(steps*99+99)/100-1]
		totalStep += iterTotal
		lastSteps = steps
	}
	if b.N > 0 && lastSteps > 0 {
		b.ReportMetric(float64(lastSteps), "steps/op")
		b.ReportMetric(float64(totalStep)/float64(b.N*lastSteps)/float64(time.Millisecond), "avg-ms/step")
		b.ReportMetric(float64(p99Sum)/float64(b.N)/float64(time.Millisecond), "p99-ms/step")
	}
}

func BenchmarkCrowdStep1000(b *testing.B) {
	g := mazeGrid(160, 128)
	c := rts.NewCrowd(g, rts.CrowdOptions{Gap: 0.05})
	ids := spawnBlock(c, 1000, 40, math2d.Vec2{X: 2.5, Y: 40.5})
	c.Move(ids, rts.MoveOrder{Target: math2d.Vec2{X: 140, Y: 64}, Formation: rts.Formation{Shape: rts.ShapeBox, Spacing: 0.9}})
	// Start mid-journey, where units crowd through the gaps.
	for range 300 {
		c.Step(step)
	}
	b.ReportAllocs()
	for b.Loop() {
		c.Step(step)
	}
}

func BenchmarkMove1000(b *testing.B) {
	g := mazeGrid(160, 128)
	c := rts.NewCrowd(g, rts.CrowdOptions{})
	ids := spawnBlock(c, 1000, 40, math2d.Vec2{X: 2.5, Y: 40.5})
	targets := []math2d.Vec2{{X: 140, Y: 64}, {X: 120, Y: 20}}
	i := 0
	for b.Loop() {
		c.Fields().Clear()
		c.Move(ids, rts.MoveOrder{Target: targets[i%2]})
		i++
	}
}

func TestCrowdRebuildReroutesUnitsOnEvictedFields(t *testing.T) {
	// A wall down the middle with two gaps, one near the top and one near the bottom.
	g := ai.NewBoolGrid(40, 20)
	for r := range 20 {
		if r != 2 && r != 3 && r != 16 && r != 17 {
			g.SetWalkable(20, r, false)
		}
	}
	c := rts.NewCrowd(g, rts.CrowdOptions{FieldCacheSize: 2})
	group := spawnBlock(c, 4, 2, math2d.Vec2{X: 5.5, Y: 2.5})
	c.Move(group, rts.MoveOrder{Target: math2d.Vec2{X: 35, Y: 3}, Formation: rts.Formation{Shape: rts.ShapeBox, Spacing: 1}})
	for range 30 {
		c.Step(step)
	}
	// Later orders push the group's fields out of the cache.
	other := c.Add(rts.UnitSpec{Pos: math2d.Vec2{X: 5.5, Y: 10.5}, Radius: 0.35})
	for i := range 10 {
		c.Move([]rts.UnitID{other}, rts.MoveOrder{Target: math2d.Vec2{X: 2.5 + float32(i), Y: 10.5}})
	}
	g.SetWalkable(20, 2, false)
	g.SetWalkable(20, 3, false)
	c.Rebuild()
	for range 60 * 30 {
		c.Step(step)
	}
	for _, id := range group {
		u, _ := c.Unit(id)
		if u.State != rts.Arrived {
			t.Fatalf("unit %d %v at %v after the top gap closed, want it arrived through the bottom gap", u.ID, u.State, u.Pos)
		}
	}
}

func TestCrowdRebuildStopsUnitsWhoseGoalsAreBlocked(t *testing.T) {
	g := ai.NewBoolGrid(10, 10)
	c := rts.NewCrowd(g, rts.CrowdOptions{})
	id := c.Add(rts.UnitSpec{Pos: math2d.Vec2{X: 1.5, Y: 1.5}, Radius: 0.35})
	c.Move([]rts.UnitID{id}, rts.MoveOrder{Target: math2d.Vec2{X: 8.5, Y: 8.5}})
	c.Step(step)
	for col := 6; col < 10; col++ {
		for row := 6; row < 10; row++ {
			g.SetWalkable(col, row, false)
		}
	}
	c.Rebuild()
	if u, _ := c.Unit(id); u.State != rts.Idle {
		t.Fatalf("unit ordered into a cell now blocked is %v, want idle", u.State)
	}
	if _, ok := c.Fields().Field(ai.Cell{Col: 8, Row: 8}); ok {
		t.Fatal("the cache still serves a field whose only goal is blocked")
	}
}

// strictGrid indexes its rows directly, as a game's own tilemap might, so any out-of-bounds query panics.
type strictGrid [][]bool

func (g strictGrid) GridSize() (cols, rows int) { return len(g[0]), len(g) }
func (g strictGrid) Walkable(col, row int) bool { return g[row][col] }

func TestCrowdNeverQueriesCellsOutsideTheGrid(t *testing.T) {
	g := make(strictGrid, 10)
	for r := range g {
		g[r] = make([]bool, 10)
		for c := range g[r] {
			g[r][c] = r > 0 && c > 0 && r < 9 && c < 9
		}
	}
	c := rts.NewCrowd(g, rts.CrowdOptions{})
	// Bodies wider than half a cell, starting inside the blocked border, including its corners.
	var ids []rts.UnitID
	for _, p := range []math2d.Vec2{{X: 0.5, Y: 0.5}, {X: 9.5, Y: 9.5}, {X: 0.5, Y: 5}, {X: 5, Y: 9.5}} {
		ids = append(ids, c.Add(rts.UnitSpec{Pos: p, Radius: 0.8}))
	}
	for range 60 {
		c.Step(step)
	}
	c.Move(ids, rts.MoveOrder{Target: math2d.Vec2{X: 5, Y: 5}})
	for range 120 {
		c.Step(step)
	}
}

func TestCrowdSanitizesNonFiniteOptions(t *testing.T) {
	nan, inf := float32(math.NaN()), float32(math.Inf(1))
	for _, opts := range []rts.CrowdOptions{
		{Origin: math2d.Vec2{X: nan, Y: inf}, CellSize: nan, Separation: nan, Gap: nan, ArriveRadius: nan, DirectRange: nan, Responsiveness: nan},
		{CellSize: inf, Separation: inf, Gap: inf, ArriveRadius: inf, DirectRange: inf, Responsiveness: inf},
	} {
		c := rts.NewCrowd(ai.NewBoolGrid(20, 20), opts)
		ids := spawnBlock(c, 9, 3, math2d.Vec2{X: 2.5, Y: 2.5})
		c.Move(ids, rts.MoveOrder{Target: math2d.Vec2{X: 15, Y: 15}})
		for range 60 * 10 {
			c.Step(step)
		}
		for _, u := range c.Units() {
			if math.IsNaN(float64(u.Pos.X)) || math.IsNaN(float64(u.Pos.Y)) || u.State != rts.Arrived {
				t.Fatalf("options %+v: unit %d %v at %v", opts, u.ID, u.State, u.Pos)
			}
		}
	}
}

func TestCrowdStepsHugeUnits(t *testing.T) {
	c := rts.NewCrowd(ai.NewBoolGrid(20, 20), rts.CrowdOptions{})
	c.Add(rts.UnitSpec{Pos: math2d.Vec2{X: 5, Y: 5}, Radius: 1e6})
	c.Add(rts.UnitSpec{Pos: math2d.Vec2{X: 6, Y: 5}, Radius: 0.4})
	// A body wider than the map costs no more than the map to keep inside it.
	for range 10 {
		c.Step(step)
	}
}

func TestCrowdORCAHeadOnAndMazeNavigation(t *testing.T) {
	t.Run("head-on opposing units pass without overlapping", func(t *testing.T) {
		g := ai.NewBoolGrid(30, 20)
		c := rts.NewCrowd(g, rts.CrowdOptions{
			Avoidance: rts.AvoidanceORCA,
			ORCA:      ai.ORCAConfig{TimeHorizon: 2.0},
		})
		if c.Options().Avoidance != rts.AvoidanceORCA || c.Options().Avoidance.String() != "orca" {
			t.Fatalf("unexpected avoidance mode: %v", c.Options().Avoidance)
		}
		a := c.Add(rts.UnitSpec{Pos: math2d.V2(4, 10), Radius: 0.4, MaxSpeed: 5, MaxAccel: 40})
		b := c.Add(rts.UnitSpec{Pos: math2d.V2(26, 10), Radius: 0.4, MaxSpeed: 5, MaxAccel: 40})
		c.Move([]rts.UnitID{a}, rts.MoveOrder{Target: math2d.V2(26, 10)})
		c.Move([]rts.UnitID{b}, rts.MoveOrder{Target: math2d.V2(4, 10)})

		for s := range 60 * 10 {
			c.Step(step)
			ua, _ := c.Unit(a)
			ub, _ := c.Unit(b)
			if d := ua.Pos.Dist(ub.Pos); d < ua.Radius+ub.Radius-0.02 {
				t.Fatalf("step %d: ORCA crowd units overlapped: dist=%.4f", s, d)
			}
			if ua.State == rts.Arrived && ub.State == rts.Arrived {
				break
			}
		}
		ua, _ := c.Unit(a)
		ub, _ := c.Unit(b)
		if ua.State != rts.Arrived || ub.State != rts.Arrived {
			t.Fatalf("units did not arrive: a=%v at %v, b=%v at %v", ua.State, ua.Pos, ub.State, ub.Pos)
		}
	})

	t.Run("maze crossing deterministic and zero-alloc", func(t *testing.T) {
		run := func(checkAllocs bool) []rts.Unit {
			g := mazeGrid(64, 64)
			c := rts.NewCrowd(g, rts.CrowdOptions{
				Gap:       0.05,
				Avoidance: rts.AvoidanceORCA,
				ORCA:      ai.ORCAConfig{TimeHorizon: 1.5, NeighborDist: 4, MaxNeighbors: 12},
			})
			ids := spawnBlock(c, 48, 8, math2d.V2(2.5, 20.5))
			c.Move(ids, rts.MoveOrder{Target: math2d.V2(56, 30), Formation: rts.Formation{Shape: rts.ShapeBox, Spacing: 1.0}})
			c.Step(step)
			if checkAllocs {
				if allocs := testing.AllocsPerRun(20, func() { c.Step(step) }); allocs != 0 {
					t.Fatalf("ORCA Crowd.Step allocated %v times", allocs)
				}
			}
			for range 60 * 40 {
				c.Step(step)
				if allArrived(c) {
					break
				}
			}
			for _, u := range c.Units() {
				if blocked(g, u.Pos, u.Radius, 0.02) {
					t.Fatalf("unit %d ended inside wall at %v", u.ID, u.Pos)
				}
				if u.State != rts.Arrived {
					t.Fatalf("unit %d %v at %v, want arrived", u.ID, u.State, u.Pos)
				}
			}
			return append([]rts.Unit(nil), c.Units()...)
		}
		a := run(true)
		b := run(false)
		for i := range a {
			if a[i].Pos != b[i].Pos || a[i].Vel != b[i].Vel || a[i].State != b[i].State {
				t.Fatalf("ORCA crowd unit %d diverged between runs: %+v vs %+v", i, a[i].Pos, b[i].Pos)
			}
		}
	})
}

func BenchmarkCrowdStep1000ORCA(b *testing.B) {
	g := mazeGrid(160, 128)
	c := rts.NewCrowd(g, rts.CrowdOptions{
		Gap:       0.05,
		Avoidance: rts.AvoidanceORCA,
		ORCA:      ai.ORCAConfig{TimeHorizon: 1.5, NeighborDist: 4, MaxNeighbors: 12},
	})
	ids := spawnBlock(c, 1000, 40, math2d.Vec2{X: 2.5, Y: 40.5})
	c.Move(ids, rts.MoveOrder{Target: math2d.Vec2{X: 140, Y: 64}, Formation: rts.Formation{Shape: rts.ShapeBox, Spacing: 0.9}})
	for range 300 {
		c.Step(step)
	}
	b.ReportAllocs()
	for b.Loop() {
		c.Step(step)
	}
}
