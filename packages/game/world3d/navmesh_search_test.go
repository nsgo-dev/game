package world3d_test

import (
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

func buildGridNavMesh3D(cols, rows int, blocked map[[2]int]bool) *world3d.NavMesh {
	var polys [][]math3d.Vec3
	const cellSize = 4.0
	for r := range rows {
		for c := range cols {
			if blocked[[2]int{c, r}] {
				continue
			}
			x0 := float32(c) * cellSize
			z0 := float32(r) * cellSize
			polys = append(polys, quadXZ(x0, z0, x0+cellSize, z0+cellSize, 0))
		}
	}
	return world3d.BuildNavMesh(polys, math3d.DegToRad(45))
}

func assertNav3DResultEqual(t *testing.T, label string, got, want world3d.NavPathResult) {
	t.Helper()
	if got.Found != want.Found || got.Partial != want.Partial || got.Exhausted != want.Exhausted || got.Expansions != want.Expansions {
		t.Fatalf("%s metadata mismatch:\ngot  = {Found:%v Partial:%v Exhausted:%v Expansions:%d}\nwant = {Found:%v Partial:%v Exhausted:%v Expansions:%d}",
			label, got.Found, got.Partial, got.Exhausted, got.Expansions,
			want.Found, want.Partial, want.Exhausted, want.Expansions)
	}
	if !slices.Equal(got.Path, want.Path) {
		t.Fatalf("%s path mismatch:\ngot  = %v\nwant = %v", label, got.Path, want.Path)
	}
}

func TestNavMesh3DPartialAndBudgeted(t *testing.T) {
	// Multi-level scene with a ramp, bridge deck, and disconnected floating island.
	ground := quadXZ(0, -2, 12, 2, 0)
	eastLanding := quadXZ(12, -2, 16, 2, 0)
	northApproach := quadXZ(12, 2, 16, 6, 0)
	ramp := []math3d.Vec3{
		{X: 6, Y: 3, Z: 2},
		{X: 6, Y: 3, Z: 6},
		{X: 12, Y: 0, Z: 6},
		{X: 12, Y: 0, Z: 2},
	}
	upperLanding := quadXZ(2, 2, 6, 6, 3)
	bridgeDeck := quadXZ(2, -2, 10, 2, 3)
	island := quadXZ(20, -2, 24, 2, 3)

	nm := world3d.BuildNavMesh([][]math3d.Vec3{
		ground,
		eastLanding,
		northApproach,
		ramp,
		upperLanding,
		bridgeDeck,
		island,
	}, math3d.DegToRad(45))
	nm.VerticalTolerance = 1.2

	start := math3d.Vec3{X: 6, Y: 0, Z: 0}
	islandGoal := math3d.Vec3{X: 22, Y: 3, Z: 0}

	// Without AllowPartial, disconnected island fails.
	noPart := nm.FindPathWithOptions(start, islandGoal, ai.PathOptions{AllowPartial: false})
	if noPart.Found || noPart.Partial || noPart.Exhausted {
		t.Fatalf("expected clean failure without AllowPartial, got %+v", noPart)
	}

	// With AllowPartial, path ends at (16, 0, 0) — the closest reachable 3D point on eastLanding.
	part := nm.FindPathWithOptions(start, islandGoal, ai.PathOptions{AllowPartial: true})
	if !part.Found || !part.Partial || part.Exhausted {
		t.Fatalf("expected partial path to disconnected island, got %+v", part)
	}
	wantEnd := math3d.Vec3{X: 16, Y: 0, Z: 0}
	if !part.Path[0].ApproxEqual(start, eps) || !part.Path[len(part.Path)-1].ApproxEqual(wantEnd, eps) {
		t.Fatalf("partial endpoints = (%v, %v), want (%v, %v)", part.Path[0], part.Path[len(part.Path)-1], start, wantEnd)
	}
	for _, wp := range part.Path {
		polyIdx, ok := nm.FindPolygon(wp)
		if !ok {
			t.Fatalf("partial waypoint %v is off the 3D navmesh", wp)
		}
		surfY := nm.Polys[polyIdx].HeightAt(wp.X, wp.Z)
		if !math3d.ApproxEqual(wp.Y, surfY, 1e-3) {
			t.Fatalf("partial waypoint %v does not match surface height %v", wp, surfY)
		}
	}

	// Off-mesh goal outside the bridge/ground returns partial path to nearest reachable edge point.
	offMeshGoal := math3d.Vec3{X: 30, Y: 0, Z: 1}
	offPart := nm.FindPathWithOptions(start, offMeshGoal, ai.PathOptions{AllowPartial: true})
	if !offPart.Found || !offPart.Partial || offPart.Exhausted {
		t.Fatalf("expected partial path for off-mesh goal, got %+v", offPart)
	}
	if !offPart.Path[len(offPart.Path)-1].ApproxEqual(math3d.Vec3{X: 16, Y: 0, Z: 1}, eps) {
		t.Fatalf("off-mesh partial end = %v, want (16, 0, 1)", offPart.Path[len(offPart.Path)-1])
	}

	// Budget exhaustion on a large 3D grid navmesh.
	blocked := map[[2]int]bool{}
	for r := range 8 {
		blocked[[2]int{4, r}] = true
	}
	large := buildGridNavMesh3D(10, 10, blocked)
	gStart := math3d.Vec3{X: 2, Y: 0, Z: 2}
	gGoal := math3d.Vec3{X: 38, Y: 0, Z: 2}

	failBudget := large.FindPathWithOptions(gStart, gGoal, ai.PathOptions{MaxExpansions: 5, AllowPartial: false})
	if failBudget.Found || failBudget.Partial || !failBudget.Exhausted || failBudget.Expansions != 5 {
		t.Fatalf("expected exhausted failure at 5 expansions, got %+v", failBudget)
	}

	partBudget := large.FindPathWithOptions(gStart, gGoal, ai.PathOptions{MaxExpansions: 5, AllowPartial: true})
	if !partBudget.Found || !partBudget.Partial || !partBudget.Exhausted || partBudget.Expansions != 5 {
		t.Fatalf("expected exhausted partial at 5 expansions, got %+v", partBudget)
	}
	for _, wp := range partBudget.Path {
		if _, ok := large.FindPolygon(wp); !ok {
			t.Fatalf("budget-partial waypoint %v lies off the navmesh", wp)
		}
	}
}

func TestNavMesh3DIncrementalMatchesSingleShot(t *testing.T) {
	blocked := map[[2]int]bool{}
	for r := range 8 {
		blocked[[2]int{3, r}] = true
	}
	for r := 2; r < 10; r++ {
		blocked[[2]int{6, r}] = true
	}
	nm := buildGridNavMesh3D(10, 10, blocked)

	walledBlocked := map[[2]int]bool{}
	for k, v := range blocked {
		walledBlocked[k] = v
	}
	walledBlocked[[2]int{6, 0}] = true
	walledBlocked[[2]int{6, 1}] = true
	walled := buildGridNavMesh3D(10, 10, walledBlocked)

	start := math3d.Vec3{X: 2, Y: 0, Z: 2}
	goal := math3d.Vec3{X: 38, Y: 0, Z: 38}

	scenarios := []struct {
		name string
		mesh *world3d.NavMesh
		opts ai.PathOptions
	}{
		{"complete", nm, ai.PathOptions{AllowPartial: true}},
		{"walled_partial", walled, ai.PathOptions{AllowPartial: true}},
		{"exhausted_partial", nm, ai.PathOptions{MaxExpansions: 15, AllowPartial: true}},
		{"exhausted_fail", nm, ai.PathOptions{MaxExpansions: 15, AllowPartial: false}},
	}

	for _, sc := range scenarios {
		want := sc.mesh.FindPathWithOptions(start, goal, sc.opts)
		for _, stepSize := range []int{1, 5, 17} {
			search := sc.mesh.StartSearch(start, goal, sc.opts)
			if _, spent := search.Step(0); spent != 0 {
				t.Fatalf("%s step(0) spent %d, want 0", sc.name, spent)
			}
			totalSpent := 0
			for !search.Status().Done() {
				_, spent := search.Step(stepSize)
				if spent < 0 || spent > stepSize {
					t.Fatalf("%s step(%d) spent %d > budget", sc.name, stepSize, spent)
				}
				totalSpent += spent
			}
			if _, spent := search.Step(stepSize); spent != 0 {
				t.Fatalf("%s post-done step spent %d, want 0", sc.name, spent)
			}
			if totalSpent != search.Expansions() {
				t.Fatalf("%s totalSpent=%d != Expansions()=%d", sc.name, totalSpent, search.Expansions())
			}
			assertNav3DResultEqual(t, sc.name, search.Result(), want)
		}
	}
	// Multi-level ramp/bridge incremental equivalence (1 expansion per step).
	rampNM := world3d.BuildNavMesh([][]math3d.Vec3{
		quadXZ(0, -2, 12, 2, 0),
		quadXZ(12, -2, 16, 2, 0),
		quadXZ(12, 2, 16, 6, 0),
		{{X: 6, Y: 3, Z: 2}, {X: 6, Y: 3, Z: 6}, {X: 12, Y: 0, Z: 6}, {X: 12, Y: 0, Z: 2}},
		quadXZ(2, 2, 6, 6, 3),
		quadXZ(2, -2, 10, 2, 3),
	}, math3d.DegToRad(45))
	rampNM.VerticalTolerance = 1.2
	wantClimb := rampNM.FindPathWithOptions(math3d.Vec3{X: 6, Y: 0, Z: 0}, math3d.Vec3{X: 6, Y: 3, Z: 0}, ai.PathOptions{})
	climbSearch := rampNM.StartSearch(math3d.Vec3{X: 6, Y: 0, Z: 0}, math3d.Vec3{X: 6, Y: 3, Z: 0}, ai.PathOptions{})
	for !climbSearch.Status().Done() {
		climbSearch.Step(1)
	}
	assertNav3DResultEqual(t, "multi-level climb step=1", climbSearch.Result(), wantClimb)
}

func TestNavMesh3DWithPathQueueAndDeterminism(t *testing.T) {
	blocked := map[[2]int]bool{}
	for r := range 8 {
		blocked[[2]int{3, r}] = true
	}
	for r := 2; r < 10; r++ {
		blocked[[2]int{6, r}] = true
	}
	nm := buildGridNavMesh3D(10, 10, blocked)

	runOnce := func() ([]int, []world3d.NavPathResult) {
		q := ai.NewPathQueue(ai.PathQueueConfig{
			Policy:    ai.PathQueueRoundRobin,
			SliceSize: 4,
		})
		searches := make([]*world3d.NavMeshSearch, 12)
		for i := range 12 {
			start := math3d.Vec3{X: float32(2 + (i%3)*4), Y: 0, Z: float32(2 + (i%9)*4)}
			goal := math3d.Vec3{X: float32(30 + (i%3)*4), Y: 0, Z: float32(2 + ((i*3)%9)*4)}
			searches[i] = nm.StartSearch(start, goal, ai.PathOptions{AllowPartial: true})
			q.Submit(searches[i], i%2, nil)
		}
		var perStep []int
		const budget = 13
		for q.Pending() > 0 {
			used := q.Step(budget)
			if used < 0 || used > budget {
				t.Fatalf("PathQueue.Step used %d > budget %d", used, budget)
			}
			perStep = append(perStep, used)
		}
		out := make([]world3d.NavPathResult, len(searches))
		for i, s := range searches {
			out[i] = s.Result()
		}
		return perStep, out
	}

	refSteps, refRes := runOnce()
	for run := range 4 {
		gotSteps, gotRes := runOnce()
		if !slices.Equal(gotSteps, refSteps) {
			t.Fatalf("run %d step expansions differed: %v vs %v", run, gotSteps, refSteps)
		}
		for i := range refRes {
			assertNav3DResultEqual(t, "3d queue determinism", gotRes[i], refRes[i])
		}
	}
}

func BenchmarkNavMesh3DPathQueueBudgetedStep(b *testing.B) {
	blocked := map[[2]int]bool{}
	for r := range 10 {
		blocked[[2]int{4, r}] = true
	}
	for r := 2; r < 12; r++ {
		blocked[[2]int{8, r}] = true
	}
	nm := buildGridNavMesh3D(12, 12, blocked)

	const perStepBudget = 32
	q := ai.NewPathQueue(ai.PathQueueConfig{
		DefaultBudget: perStepBudget,
		Policy:        ai.PathQueueRoundRobin,
		SliceSize:     8,
	})
	refill := func() {
		for i := range 24 {
			start := math3d.Vec3{X: float32(2 + (i%3)*4), Y: 0, Z: float32(2 + (i%10)*4)}
			goal := math3d.Vec3{X: float32(38 + (i%3)*4), Y: 0, Z: float32(2 + ((i*3)%10)*4)}
			q.Submit(nm.StartSearch(start, goal, ai.PathOptions{AllowPartial: true}), i%2, nil)
		}
	}
	refill()

	b.ResetTimer()
	for range b.N {
		if q.Pending() < 12 {
			refill()
		}
		used := q.Step(perStepBudget)
		if used > perStepBudget {
			b.Fatalf("PathQueue.Step used %d expansions, exceeding per-step budget %d", used, perStepBudget)
		}
	}
}
