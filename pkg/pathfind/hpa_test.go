package pathfind_test

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/pathfind"
)

func computePathCost(g pathfind.Grid, path []pathfind.Cell, diagonal bool) float64 {
	eg, hasEdge := g.(pathfind.EdgeCostGrid)
	cg, hasCost := g.(pathfind.CostGrid)
	total := 0.0
	for i := 1; i < len(path); i++ {
		a, b := path[i-1], path[i]
		dist := 1.0
		if a.Col != b.Col && a.Row != b.Row {
			dist = math.Sqrt2
		}
		mul := 1.0
		if hasEdge {
			ec := eg.EdgeCost(a.Col, a.Row, b.Col, b.Row)
			if ec >= 1 {
				mul = ec
			}
		} else if hasCost {
			tc := cg.MoveCost(b.Col, b.Row)
			if tc >= 1 {
				mul = tc
			}
		}
		total += dist * mul
	}
	return total
}

// buildLargeBenchmarkMap builds a deterministic 1024x1024 map with room walls,
// staggered doorways in every room, and obstacle pillars so long cross-map searches require real routing.
func buildLargeBenchmarkMap(size int) *pathfind.BoolGrid {
	g := pathfind.NewBoolGrid(size, size)
	// Scatter deterministic small pillars first so walls and doorways carved afterwards stay guaranteed open.
	rng := rand.New(rand.NewPCG(833, 1024))
	for range size * 2 {
		pc := 8 + rng.IntN(size-16)
		pr := 8 + rng.IntN(size-16)
		for dr := range 4 {
			for dc := range 4 {
				g.SetWalkable(pc+dc, pr+dr, false)
			}
		}
	}
	// Vertical room walls every 64 cols with a staggered doorway in every 128-row band.
	for c := 64; c < size; c += 64 {
		for r := range size {
			g.SetWalkable(c, r, false)
		}
		for band := 0; band < size; band += 128 {
			offset := 20 + ((c/64)*37+(band/128)*53)%80
			doorStart := band + offset
			doorEnd := min(doorStart+16, min(band+124, size))
			for r := doorStart; r < doorEnd; r++ {
				for dc := -2; dc <= 2; dc++ {
					g.SetWalkable(c+dc, r, true)
				}
			}
		}
	}
	// Horizontal room walls every 128 rows with a staggered doorway in every 64-col band.
	for r := 128; r < size; r += 128 {
		for c := range size {
			g.SetWalkable(c, r, false)
		}
		for band := 0; band < size; band += 64 {
			offset := 10 + ((r/128)*29+(band/64)*17)%36
			doorStart := band + offset
			doorEnd := min(doorStart+12, min(band+60, size))
			for c := doorStart; c < doorEnd; c++ {
				for dr := -2; dr <= 2; dr++ {
					g.SetWalkable(c, r+dr, true)
				}
			}
		}
	}
	// Guarantee start and goal pockets are open.
	for r := range 12 {
		for c := range 12 {
			g.SetWalkable(c, r, true)
			g.SetWalkable(size-1-c, size-1-r, true)
		}
	}
	return g
}

func TestHPABasic4WayAnd8Way(t *testing.T) {
	g := pathfind.NewBoolGrid(48, 36)
	for r := 4; r < 32; r++ {
		g.SetWalkable(20, r, false)
	}
	for r := 0; r < 28; r++ {
		g.SetWalkable(34, r, false)
	}

	for _, tc := range []struct {
		name               string
		diagonal           bool
		allowCornerCutting bool
	}{
		{"4way", false, false},
		{"8way_no_cut", true, false},
		{"8way_allow_cut", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := pathfind.NewHPA(g, pathfind.HPAOptions{
				ClusterSize:        8,
				Diagonal:           tc.diagonal,
				AllowCornerCutting: tc.allowCornerCutting,
			})
			astar := pathfind.NewAStar()
			astar.Diagonal = tc.diagonal
			astar.NoCornerCutting = !tc.allowCornerCutting

			start := pathfind.Cell{Col: 2, Row: 18}
			goal := pathfind.Cell{Col: 45, Row: 12}

			path, cost, ok := h.Find(start, goal)
			if !ok {
				t.Fatal("HPA failed to find path")
			}
			verifyPath(t, g, path, start, goal, tc.diagonal, !tc.allowCornerCutting)
			exact := computePathCost(g, path, tc.diagonal)
			if math.Abs(cost-exact) > 1e-6 {
				t.Fatalf("reported cost = %v, computed path cost = %v", cost, exact)
			}

			_, optCost, optOK := astar.Find(g, start, goal)
			if !optOK {
				t.Fatal("AStar failed")
			}
			if cost+1e-9 < optCost {
				t.Fatalf("HPA cost %v < optimal A* cost %v", cost, optCost)
			}
			if ratio := cost / optCost; ratio > 1.20 {
				t.Fatalf("HPA suboptimality ratio = %.3f (cost=%v, opt=%v), want <= 1.20", ratio, cost, optCost)
			}
		})
	}
}

func TestHPASameClusterWithDetourOutside(t *testing.T) {
	// Start and goal lie in the same 16x16 cluster (0,0), separated by a wall spanning rows 0..15 at col 8.
	// Only way around is through cluster (0,1) at row 16.
	g := pathfind.NewBoolGrid(32, 32)
	for r := range 16 {
		g.SetWalkable(8, r, false)
	}
	h := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 16, Diagonal: true})
	start := pathfind.Cell{Col: 2, Row: 4}
	goal := pathfind.Cell{Col: 14, Row: 4}

	path, cost, ok := h.Find(start, goal)
	if !ok {
		t.Fatal("expected HPA path detouring through neighboring cluster")
	}
	verifyPath(t, g, path, start, goal, true, true)
	if math.Abs(cost-computePathCost(g, path, true)) > 1e-6 {
		t.Fatalf("cost mismatch: %v vs %v", cost, computePathCost(g, path, true))
	}
	// Verify path actually dipped into row >= 16.
	crossed := false
	for _, c := range path {
		if c.Row >= 16 {
			crossed = true
			break
		}
	}
	if !crossed {
		t.Fatal("expected detour into cluster (0,1)")
	}
}

func TestHPADiagonalCornerSqueezeCompleteness(t *testing.T) {
	// When AllowCornerCutting is true, a diagonal-only gap across a cluster border
	// (col 7 -> col 8) must still be found.
	g := pathfind.NewBoolGrid(16, 16)
	for r := range 16 {
		g.SetWalkable(7, r, false)
		g.SetWalkable(8, r, false)
	}
	// Open a diagonal squeeze: (7, 5) and (8, 6) walkable while (8, 5) and (7, 6) stay blocked.
	g.SetWalkable(7, 5, true)
	g.SetWalkable(8, 6, true)

	noCut := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 8, Diagonal: true, AllowCornerCutting: false})
	if _, _, ok := noCut.Find(pathfind.Cell{Col: 1, Row: 5}, pathfind.Cell{Col: 14, Row: 6}); ok {
		t.Fatal("expected unreachable when AllowCornerCutting is false")
	}

	allowCut := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 8, Diagonal: true, AllowCornerCutting: true})
	path, cost, ok := allowCut.Find(pathfind.Cell{Col: 1, Row: 5}, pathfind.Cell{Col: 14, Row: 6})
	if !ok {
		t.Fatal("expected path through diagonal border squeeze when AllowCornerCutting is true")
	}
	verifyPath(t, g, path, pathfind.Cell{Col: 1, Row: 5}, pathfind.Cell{Col: 14, Row: 6}, true, false)
	if math.Abs(cost-computePathCost(g, path, true)) > 1e-6 {
		t.Fatalf("cost mismatch: %v", cost)
	}
}

func TestHPAWeightedCostMap(t *testing.T) {
	// 48x32 map with a vertical swamp (cost 10) across cols 14..18 except a cost-1 road along row 15.
	cm := pathfind.NewCostMap(48, 32)
	for r := range 32 {
		for c := 14; c <= 18; c++ {
			cm.SetCost(c, r, 10)
		}
	}
	for c := 14; c <= 18; c++ {
		cm.SetCost(c, 15, 1)
	}

	h := pathfind.NewHPA(cm, pathfind.HPAOptions{ClusterSize: 16, Diagonal: true})
	astar := pathfind.NewAStar()
	astar.Diagonal = true

	start := pathfind.Cell{Col: 2, Row: 10}
	goal := pathfind.Cell{Col: 40, Row: 10}
	path, cost, ok := h.Find(start, goal)
	if !ok {
		t.Fatal("expected weighted HPA path")
	}
	verifyPath(t, cm, path, start, goal, true, true)
	_, optCost, _ := astar.Find(cm, start, goal)
	if math.Abs(cost-computePathCost(cm, path, true)) > 1e-6 {
		t.Fatalf("cost mismatch: %v vs %v", cost, computePathCost(cm, path, true))
	}
	if ratio := cost / optCost; ratio > 1.25 {
		t.Fatalf("weighted HPA ratio = %.3f (cost=%v, opt=%v), want <= 1.25", ratio, cost, optCost)
	}
}

type oneWayCliffGrid struct {
	*pathfind.BoolGrid
	cliffCol int
}

func (g oneWayCliffGrid) EdgeCost(fc, fr, tc, tr int) float64 {
	// Stepping east across cliffCol is cheap (jumping down); stepping west across cliffCol is forbidden.
	if fc == g.cliffCol && tc == g.cliffCol+1 {
		return 1
	}
	if fc == g.cliffCol+1 && tc == g.cliffCol {
		return -1
	}
	return 1
}

func TestHPAAsymmetricEdgeCostGrid(t *testing.T) {
	// One-way ledge on the cluster border between col 7 and col 8 (ClusterSize=8).
	g := oneWayCliffGrid{BoolGrid: pathfind.NewBoolGrid(16, 16), cliffCol: 7}
	h := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 8, Diagonal: false})

	// Eastbound across the ledge succeeds.
	path, cost, ok := h.Find(pathfind.Cell{Col: 2, Row: 4}, pathfind.Cell{Col: 12, Row: 4})
	if !ok {
		t.Fatal("expected eastbound path down one-way cliff")
	}
	verifyPath(t, g, path, pathfind.Cell{Col: 2, Row: 4}, pathfind.Cell{Col: 12, Row: 4}, false, true)
	if math.Abs(cost-computePathCost(g, path, false)) > 1e-6 {
		t.Fatalf("cost mismatch: %v", cost)
	}

	// Westbound back up the one-way cliff must fail.
	if _, _, ok := h.Find(pathfind.Cell{Col: 12, Row: 4}, pathfind.Cell{Col: 2, Row: 4}); ok {
		t.Fatal("expected westbound path up one-way cliff to be blocked")
	}
}

func TestHPAIncrementalDirtyClusterUpdates(t *testing.T) {
	// 64x64 grid with ClusterSize 16 -> 4x4 = 16 clusters.
	g := pathfind.NewBoolGrid(64, 64)
	h := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 16, Diagonal: true})
	st0 := h.Stats()
	if st0.ClusterCols != 4 || st0.ClusterRows != 4 || st0.TotalClusterRebuilds != 16 {
		t.Fatalf("initial stats = %+v", st0)
	}

	// 1. Interior cell edit inside cluster (1, 1) (cols 16..31, rows 16..31):
	// ONLY cluster (1, 1) should be marked dirty and rebuilt!
	g.SetWalkable(22, 22, false)
	h.UpdateCell(22, 22)
	if dirty := h.Stats().DirtyClusters; dirty != 1 {
		t.Fatalf("interior edit dirty clusters = %d, want 1", dirty)
	}
	rebuilt := h.RebuildDirty()
	if rebuilt != 1 {
		t.Fatalf("interior edit rebuilt = %d, want 1", rebuilt)
	}
	if st := h.Stats(); st.TotalClusterRebuilds != 17 || st.LastRebuiltClusters != 1 || st.DirtyClusters != 0 {
		t.Fatalf("after interior rebuild stats = %+v", st)
	}

	// 2. Shared vertical border cell edit at col 31, row 20 (border between (1,1) and (2,1)):
	// ONLY those 2 clusters should be rebuilt!
	g.SetWalkable(31, 20, false)
	h.UpdateCell(31, 20)
	if dirty := h.Stats().DirtyClusters; dirty != 2 {
		t.Fatalf("border edit dirty clusters = %d, want 2", dirty)
	}
	if rebuilt := h.RebuildDirty(); rebuilt != 2 {
		t.Fatalf("border edit rebuilt = %d, want 2", rebuilt)
	}

	// 3. Build a partial wall blocking the middle of the map and verify paths route around it
	// automatically on Find (without manually calling RebuildDirty).
	start := pathfind.Cell{Col: 2, Row: 32}
	goal := pathfind.Cell{Col: 60, Row: 32}
	pBefore, cBefore, ok := h.Find(start, goal)
	if !ok {
		t.Fatal("expected initial path")
	}
	verifyPath(t, g, pBefore, start, goal, true, true)

	// Block col 31 rows 16..55 (touches clusters (1,1..3) and (2,1..3) = 6 clusters, NOT all 16!).
	for r := 16; r <= 55; r++ {
		g.SetWalkable(31, r, false)
	}
	h.UpdateRect(31, 16, 31, 55)
	pAfter, cAfter, ok := h.Find(start, goal)
	if !ok {
		t.Fatal("expected detour path around wall")
	}
	verifyPath(t, g, pAfter, start, goal, true, true)
	if cAfter <= cBefore {
		t.Fatalf("expected detour to cost more than open path: before=%v after=%v", cBefore, cAfter)
	}
	if st := h.Stats(); st.LastRebuiltClusters >= 16 {
		t.Fatalf("expected partial rebuild (<16), got %d", st.LastRebuiltClusters)
	}

	// 4. Seal the remaining rows of col 31 so the map is split in two -> Find must report unreachable.
	for r := range 64 {
		g.SetWalkable(31, r, false)
	}
	h.UpdateRect(31, 0, 31, 63)
	if _, _, ok := h.Find(start, goal); ok {
		t.Fatal("expected unreachable after sealing wall")
	}

	// 5. Punch a single non-corner border door at (31, 50) -> Find must route through that door,
	// rebuilding only the 2 clusters sharing that vertical border!
	g.SetWalkable(31, 50, true)
	h.UpdateCell(31, 50)
	pDoor, _, ok := h.Find(start, goal)
	if !ok {
		t.Fatal("expected path after punching door")
	}
	verifyPath(t, g, pDoor, start, goal, true, true)
	if !slices.Contains(pDoor, pathfind.Cell{Col: 31, Row: 50}) {
		t.Fatalf("expected path to pass through newly opened door (31,50)")
	}
	if st := h.Stats(); st.LastRebuiltClusters != 2 {
		t.Fatalf("single border door rebuild touched %d clusters, want 2", st.LastRebuiltClusters)
	}

	// 6. Corner border cell (31, 48) touches both the vertical border to (2,3) and horizontal border to (1,2) -> 3 clusters.
	g.SetWalkable(31, 48, true)
	h.UpdateCell(31, 48)
	if rebuilt := h.RebuildDirty(); rebuilt != 3 {
		t.Fatalf("corner border cell rebuild touched %d clusters, want 3", rebuilt)
	}
}

func TestHPAAbstractAndClusterQueries(t *testing.T) {
	g := pathfind.NewBoolGrid(64, 64)
	for r := 0; r < 52; r++ {
		g.SetWalkable(32, r, false)
	}
	h := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 16, Diagonal: true})
	start := pathfind.Cell{Col: 4, Row: 4}
	goal := pathfind.Cell{Col: 60, Row: 4}

	abs, absCost, ok := h.FindAbstract(start, goal)
	if !ok || len(abs) < 2 {
		t.Fatalf("FindAbstract failed: %v %v %v", abs, absCost, ok)
	}
	if abs[0] != start || abs[len(abs)-1] != goal {
		t.Fatalf("abstract endpoints = (%v, %v), want (%v, %v)", abs[0], abs[len(abs)-1], start, goal)
	}

	clusters, clCost, ok := h.FindClusters(start, goal)
	if !ok || len(clusters) < 2 {
		t.Fatalf("FindClusters failed: %v %v %v", clusters, clCost, ok)
	}
	if math.Abs(absCost-clCost) > 1e-9 {
		t.Fatalf("abstract cost %v != cluster cost %v", absCost, clCost)
	}
	if clusters[0] != (pathfind.ClusterID{Col: 0, Row: 0}) || clusters[len(clusters)-1] != (pathfind.ClusterID{Col: 3, Row: 0}) {
		t.Fatalf("cluster endpoints = %v .. %v", clusters[0], clusters[len(clusters)-1])
	}

	// Verify every cell of refined Find path lies inside one of the returned clusters.
	clusterSet := make(map[pathfind.ClusterID]bool, len(clusters))
	for _, cid := range clusters {
		clusterSet[cid] = true
	}
	path, _, ok := h.Find(start, goal)
	if !ok {
		t.Fatal("Find failed")
	}
	for _, cell := range path {
		cid, ok := h.ClusterAt(cell)
		if !ok || !clusterSet[cid] {
			t.Fatalf("cell %v in cluster %v not in FindClusters corridor %v", cell, cid, clusters)
		}
	}
}

func TestHPADeterminismAndZeroAllocFindInto(t *testing.T) {
	g := pathfind.NewBoolGrid(96, 96)
	rng := rand.New(rand.NewPCG(123, 456))
	for range 1200 {
		g.SetWalkable(rng.IntN(96), rng.IntN(96), false)
	}
	start := pathfind.Cell{Col: 0, Row: 0}
	goal := pathfind.Cell{Col: 95, Row: 95}
	g.SetWalkable(start.Col, start.Row, true)
	g.SetWalkable(goal.Col, goal.Row, true)

	h1 := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 16, Diagonal: true})
	h2 := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 16, Diagonal: true})

	ref, refCost, ok := h1.Find(start, goal)
	if !ok {
		t.Fatal("expected path")
	}
	for i := range 20 {
		got, gotCost, ok := h1.Find(start, goal)
		if !ok || gotCost != refCost || !slices.Equal(got, ref) {
			t.Fatalf("repeat %d differed", i)
		}
	}
	got2, cost2, ok2 := h2.Find(start, goal)
	if !ok2 || cost2 != refCost || !slices.Equal(got2, ref) {
		t.Fatal("independent HPA instance produced different path")
	}

	// Zero-allocation FindInto after warm-up.
	buf := make([]pathfind.Cell, 0, len(ref)+32)
	buf, _, ok = h1.FindInto(buf, start, goal)
	if !ok {
		t.Fatal("warm-up FindInto failed")
	}
	allocs := testing.AllocsPerRun(50, func() {
		var c float64
		buf, c, ok = h1.FindInto(buf[:0], start, goal)
		if !ok || c != refCost {
			t.Fatal("FindInto mismatch")
		}
	})
	if allocs != 0 {
		t.Fatalf("HPA.FindInto allocated %v times per run, want 0", allocs)
	}
}

func TestHPAEdgeCases(t *testing.T) {
	// Nil and empty grids.
	nilH := pathfind.NewHPA(nil, pathfind.HPAOptions{})
	if _, _, ok := nilH.Find(pathfind.Cell{}, pathfind.Cell{}); ok {
		t.Fatal("nil grid should fail")
	}
	emptyH := pathfind.NewHPA(pathfind.NewBoolGrid(0, 0), pathfind.HPAOptions{})
	if _, _, ok := emptyH.Find(pathfind.Cell{}, pathfind.Cell{}); ok {
		t.Fatal("empty grid should fail")
	}

	// Small grid smaller than cluster size (single cluster).
	g := pathfind.NewBoolGrid(6, 5)
	g.SetWalkable(2, 2, false)
	h := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 16, Diagonal: true})
	if p, cost, ok := h.Find(pathfind.Cell{Col: 1, Row: 1}, pathfind.Cell{Col: 1, Row: 1}); !ok || cost != 0 || !slices.Equal(p, []pathfind.Cell{{Col: 1, Row: 1}}) {
		t.Fatalf("start==goal gave (%v, %v, %v)", p, cost, ok)
	}
	if _, _, ok := h.Find(pathfind.Cell{Col: 2, Row: 2}, pathfind.Cell{Col: 4, Row: 4}); ok {
		t.Fatal("blocked start should fail")
	}
	if _, _, ok := h.Find(pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 2, Row: 2}); ok {
		t.Fatal("blocked goal should fail")
	}
	if _, _, ok := h.Find(pathfind.Cell{Col: -1, Row: 0}, pathfind.Cell{Col: 4, Row: 4}); ok {
		t.Fatal("out-of-bounds should fail")
	}
	// Single-cluster path must match exact A*.
	path, cost, ok := h.Find(pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 5, Row: 4})
	if !ok {
		t.Fatal("single-cluster path failed")
	}
	verifyPath(t, g, path, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 5, Row: 4}, true, true)
	a := pathfind.NewAStar()
	a.Diagonal = true
	_, optCost, _ := a.Find(g, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 5, Row: 4})
	if math.Abs(cost-optCost) > 1e-9 {
		t.Fatalf("single-cluster cost = %v, want optimal %v", cost, optCost)
	}
}

func TestHPA1024SpeedupAndOptimalityBound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 1024x1024 comparison in -short mode")
	}
	g := buildLargeBenchmarkMap(1024)
	start := pathfind.Cell{Col: 2, Row: 2}
	goal := pathfind.Cell{Col: 1021, Row: 1021}

	astar := pathfind.NewAStar()
	astar.Diagonal = true
	hpa := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 32, Diagonal: true})

	// Warm up both searchers.
	astarBuf := make([]pathfind.Cell, 0, 4096)
	hpaBuf := make([]pathfind.Cell, 0, 4096)
	_, optCost, optOK := astar.FindInto(astarBuf, g, start, goal)
	if !optOK {
		t.Fatal("AStar failed on 1024x1024 map")
	}
	hpaPath, hpaCost, hpaOK := hpa.FindInto(hpaBuf, start, goal)
	if !hpaOK {
		t.Fatal("HPA failed on 1024x1024 map")
	}
	verifyPath(t, g, hpaPath, start, goal, true, true)

	ratio := hpaCost / optCost
	if ratio > 1.15 {
		t.Fatalf("1024x1024 HPA* cost ratio = %.4f (hpa=%.2f, astar=%.2f), want <= 1.15", ratio, hpaCost, optCost)
	}

	// Measure average search latency over multiple runs.
	const astarRuns = 5
	t0 := time.Now()
	for range astarRuns {
		astarBuf, _, _ = astar.FindInto(astarBuf[:0], g, start, goal)
	}
	astarDur := time.Since(t0) / astarRuns

	const hpaRuns = 50
	t1 := time.Now()
	for range hpaRuns {
		hpaBuf, _, _ = hpa.FindInto(hpaBuf[:0], start, goal)
	}
	hpaDur := time.Since(t1) / hpaRuns

	speedup := float64(astarDur) / float64(hpaDur)
	t.Logf("1024x1024 search: A*=%v, HPA*=%v, speedup=%.1fx, cost_ratio=%.4f (opt=%.2f, hpa=%.2f)",
		astarDur, hpaDur, speedup, ratio, optCost, hpaCost)
	if speedup < 10.0 {
		t.Fatalf("HPA* speedup = %.2fx (A*=%v, HPA*=%v), want >= 10x", speedup, astarDur, hpaDur)
	}
}

func BenchmarkAStar1024(b *testing.B) {
	g := buildLargeBenchmarkMap(1024)
	a := pathfind.NewAStar()
	a.Diagonal = true
	start := pathfind.Cell{Col: 2, Row: 2}
	goal := pathfind.Cell{Col: 1021, Row: 1021}
	buf := make([]pathfind.Cell, 0, 4096)
	buf, _, _ = a.FindInto(buf, g, start, goal)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buf, _, _ = a.FindInto(buf[:0], g, start, goal)
	}
}

func BenchmarkHPA1024(b *testing.B) {
	g := buildLargeBenchmarkMap(1024)
	h := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 32, Diagonal: true})
	start := pathfind.Cell{Col: 2, Row: 2}
	goal := pathfind.Cell{Col: 1021, Row: 1021}
	buf := make([]pathfind.Cell, 0, 4096)
	buf, _, _ = h.FindInto(buf, start, goal)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buf, _, _ = h.FindInto(buf[:0], start, goal)
	}
}

func BenchmarkHPAAbstract1024(b *testing.B) {
	g := buildLargeBenchmarkMap(1024)
	h := pathfind.NewHPA(g, pathfind.HPAOptions{ClusterSize: 32, Diagonal: true})
	start := pathfind.Cell{Col: 2, Row: 2}
	goal := pathfind.Cell{Col: 1021, Row: 1021}
	buf := make([]pathfind.Cell, 0, 256)
	buf, _, _ = h.FindAbstractInto(buf, start, goal)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buf, _, _ = h.FindAbstractInto(buf[:0], start, goal)
	}
}
