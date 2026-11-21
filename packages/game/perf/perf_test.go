package perf_test

import (
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/perf"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// --- #223 Object Pools ---

type testBullet struct {
	X, Y   float32
	VX, VY float32
	Life   int
	Trail  []float32
}

func TestPoolLifecycleAndResetHook(t *testing.T) {
	resetCalls := 0
	p := perf.NewPool(4, func(b *testBullet) {
		resetCalls++
		b.X, b.Y, b.VX, b.VY, b.Life = 0, 0, 0, 0, 0
		b.Trail = b.Trail[:0]
	})

	if p.Cap() != 4 || p.Free() != 4 || p.Active() != 0 {
		t.Fatalf("initial state: cap=%d free=%d active=%d, want 4/4/0", p.Cap(), p.Free(), p.Active())
	}

	b1 := p.Acquire()
	b1.X = 10
	b1.Trail = append(b1.Trail, 1, 2, 3)
	origTrailCap := cap(b1.Trail)

	b2 := p.Acquire()
	b2.Life = 99
	if p.Active() != 2 || p.Free() != 2 {
		t.Fatalf("after 2 acquires: active=%d free=%d, want 2/2", p.Active(), p.Free())
	}

	// Nil release is a safe no-op.
	p.Release(nil)
	if p.Active() != 2 || p.Free() != 2 {
		t.Fatalf("after Release(nil): active=%d free=%d, want 2/2", p.Active(), p.Free())
	}

	// Release invokes reset hook and preserves slice capacity.
	p.Release(b1)
	if resetCalls != 1 || p.Active() != 1 || p.Free() != 3 {
		t.Fatalf("after release: resets=%d active=%d free=%d", resetCalls, p.Active(), p.Free())
	}
	reused := p.Acquire()
	if reused != b1 {
		t.Fatalf("expected LIFO reuse of b1, got different pointer")
	}
	if reused.X != 0 || len(reused.Trail) != 0 || cap(reused.Trail) != origTrailCap {
		t.Fatalf("reset hook did not reset fields while preserving Trail cap: %+v cap=%d", reused, cap(reused.Trail))
	}

	// Growth preserves pointer stability for active items.
	ptrs := []*testBullet{reused, b2}
	for range 20 {
		ptrs = append(ptrs, p.Acquire())
	}
	if p.Active() != 22 || p.Cap() < 22 {
		t.Fatalf("after growth: active=%d cap=%d", p.Active(), p.Cap())
	}
	if b2.Life != 99 {
		t.Fatalf("pointer stability broken after pool growth: b2.Life=%d", b2.Life)
	}

	// Prewarm ensures at least N free slots.
	p.Prewarm(50)
	if p.Free() < 50 {
		t.Fatalf("after Prewarm(50): free=%d, want >= 50", p.Free())
	}

	// Reset reclaims everything without dropping capacity.
	prevCap := p.Cap()
	p.Reset()
	if p.Active() != 0 || p.Free() != prevCap || p.Cap() != prevCap {
		t.Fatalf("after Reset: active=%d free=%d cap=%d, want 0/%d/%d", p.Active(), p.Free(), p.Cap(), prevCap, prevCap)
	}

	// Nil reset hook zeroes elements on Release and Reset.
	zeroPool := perf.NewPool[testBullet](2, nil)
	z := zeroPool.Acquire()
	z.X, z.Life = 42, 7
	zeroPool.Release(z)
	z2 := zeroPool.Acquire()
	if z2.X != 0 || z2.Life != 0 {
		t.Fatalf("nil reset did not zero element: %+v", z2)
	}
	z2.X = 55
	zeroPool.Reset()
	z3 := zeroPool.Acquire()
	if z3.X != 0 {
		t.Fatalf("Reset with nil hook did not zero element: %+v", z3)
	}
}

func TestPoolZeroAllocSteadyState(t *testing.T) {
	p := perf.NewPool(128, func(b *testBullet) {
		b.X, b.Y, b.VX, b.VY, b.Life = 0, 0, 0, 0, 0
	})

	// 100k acquire/release cycles in steady state must do 0 allocations.
	allocs := testing.AllocsPerRun(5, func() {
		for range 100_000 {
			b := p.Acquire()
			b.X = 1
			b.Life = 2
			p.Release(b)
		}
	})
	if allocs != 0 {
		t.Fatalf("Pool 100k acquire/release allocated %.1f times/run, want 0", allocs)
	}

	// Wave acquire + Reset in steady state must also do 0 allocations.
	p.Prewarm(1024)
	waveAllocs := testing.AllocsPerRun(20, func() {
		for i := range 1024 {
			b := p.Acquire()
			b.Life = i
		}
		p.Reset()
	})
	if waveAllocs != 0 {
		t.Fatalf("Pool wave+Reset allocated %.1f times/run, want 0", waveAllocs)
	}
}

func TestArenaHandlesSwapRemoveAndZeroAlloc(t *testing.T) {
	a := perf.NewArena[testBullet](16, nil)

	var zeroH perf.Handle
	if !zeroH.IsZero() || a.Has(zeroH) || a.Remove(zeroH) {
		t.Fatal("zero handle must be invalid")
	}
	if v, ok := a.Get(zeroH); ok || v != nil {
		t.Fatal("Get(zero) must return (nil, false)")
	}

	h1 := a.Add(testBullet{X: 1, Life: 10})
	h2 := a.Add(testBullet{X: 2, Life: 20})
	h3, p3 := a.Alloc()
	p3.X, p3.Life = 3, 30

	if a.Len() != 3 {
		t.Fatalf("Len=%d, want 3", a.Len())
	}

	// Remove middle element (h2); h3 swaps into its dense slot while keeping its handle valid.
	if !a.Remove(h2) {
		t.Fatal("Remove(h2) returned false")
	}
	if a.Remove(h2) {
		t.Fatal("second Remove(h2) must return false")
	}
	if a.Has(h2) {
		t.Fatal("removed handle must not be valid")
	}
	if v, ok := a.Get(h3); !ok || v.X != 3 || v.Life != 30 {
		t.Fatalf("h3 after swap-remove = (%+v, %v), want X=3 Life=30", v, ok)
	}

	// Reusing h2's slot bumps generation so stale h2 stays invalid.
	h4 := a.Add(testBullet{X: 4, Life: 40})
	if h4 == h2 {
		t.Fatal("reused handle must have a new generation")
	}
	if a.Has(h2) {
		t.Fatal("stale handle h2 became valid after slot reuse")
	}
	if v, ok := a.Get(h4); !ok || v.X != 4 {
		t.Fatalf("Get(h4) = (%+v, %v), want X=4", v, ok)
	}

	// ForEach and Slice visit all live elements.
	seen := make(map[perf.Handle]float32)
	a.ForEach(func(h perf.Handle, v *testBullet) {
		seen[h] = v.X
	})
	if len(seen) != 3 || seen[h1] != 1 || seen[h3] != 3 || seen[h4] != 4 {
		t.Fatalf("ForEach saw %v", seen)
	}
	if len(a.Slice()) != 3 {
		t.Fatalf("Slice len=%d, want 3", len(a.Slice()))
	}

	// All() supports early break.
	count := 0
	for range a.All() {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Fatalf("All() break stopped at %d, want 2", count)
	}

	// Reset invalidates all live handles and keeps capacity.
	a.Reset()
	if a.Len() != 0 || a.Has(h1) || a.Has(h3) || a.Has(h4) {
		t.Fatal("Reset did not invalidate all handles")
	}

	// Steady-state Add/Get/ForEach/Remove/Reset = 0 allocs.
	handles := make([]perf.Handle, 0, 128)
	for i := range 128 {
		handles = append(handles, a.Add(testBullet{Life: i}))
	}
	a.Reset()

	allocs := testing.AllocsPerRun(50, func() {
		handles = handles[:0]
		for i := range 128 {
			handles = append(handles, a.Add(testBullet{Life: i}))
		}
		sum := 0
		a.ForEach(func(_ perf.Handle, b *testBullet) {
			sum += b.Life
		})
		for _, h := range handles {
			a.Remove(h)
		}
		_ = sum
	})
	if allocs != 0 {
		t.Fatalf("Arena steady-state loop allocated %.1f times/run, want 0", allocs)
	}
}

func TestSlicePoolReuseAndZeroAlloc(t *testing.T) {
	sp := perf.NewSlicePool[int](4)
	s1 := sp.Acquire(16)
	s2 := sp.Acquire(64)
	s1 = append(s1, 1, 2, 3)
	s2 = append(s2, 10, 20)
	sp.Release(s1)
	sp.Release(s2)

	if sp.Free() != 2 {
		t.Fatalf("Free=%d, want 2", sp.Free())
	}
	// Best-fit should pick cap=16 for minCap=10 rather than cap=64.
	got := sp.Acquire(10)
	if len(got) != 0 || cap(got) != 16 {
		t.Fatalf("best-fit got len=%d cap=%d, want 0/16", len(got), cap(got))
	}
	sp.Release(got)

	allocs := testing.AllocsPerRun(100, func() {
		buf := sp.Acquire(16)
		buf = append(buf, 1, 2, 3, 4)
		sp.Release(buf)
	})
	if allocs != 0 {
		t.Fatalf("SlicePool steady state allocated %.1f times/run, want 0", allocs)
	}

	sp.Reset()
	if sp.Free() != 0 {
		t.Fatalf("after Reset Free=%d, want 0", sp.Free())
	}
}

// --- #224 Spatial Hash & Quadtree ---

type rectItem struct {
	id     int
	bounds core.Bounds
}

func bruteQuery(items []rectItem, area core.Bounds) []int {
	var out []int
	for _, it := range items {
		if perf.Intersects(it.bounds, area) {
			out = append(out, it.id)
		}
	}
	slices.Sort(out)
	return out
}

func bruteQueryPoint(items []rectItem, p core.Point) []int {
	var out []int
	for _, it := range items {
		if it.bounds.Contains(p) {
			out = append(out, it.id)
		}
	}
	slices.Sort(out)
	return out
}

func TestSpatialHashAndQuadtreeEquivalence1000(t *testing.T) {
	rng := rand.New(rand.NewPCG(224, 99991))
	world := core.Bounds{
		Origin: core.Point{X: -500, Y: -500},
		Size:   core.Size{Width: 1000, Height: 1000},
	}

	items := make([]rectItem, 0, 1200)
	// Edge, corner, negative, spanning, and outside-world rectangles.
	special := []core.Bounds{
		{Origin: core.Point{X: -500, Y: -500}, Size: core.Size{Width: 32, Height: 32}},   // world top-left corner
		{Origin: core.Point{X: 468, Y: 468}, Size: core.Size{Width: 32, Height: 32}},     // world bottom-right corner
		{Origin: core.Point{X: -16, Y: -16}, Size: core.Size{Width: 32, Height: 32}},     // straddling origin (0,0)
		{Origin: core.Point{X: -600, Y: -600}, Size: core.Size{Width: 150, Height: 150}}, // partially outside world
		{Origin: core.Point{X: 700, Y: 700}, Size: core.Size{Width: 80, Height: 80}},     // fully outside world
		{Origin: core.Point{X: -400, Y: -20}, Size: core.Size{Width: 800, Height: 40}},   // spans many cells horizontally
		{Origin: core.Point{X: 64, Y: 64}, Size: core.Size{Width: 64, Height: 64}},       // exact cell-aligned
		{Origin: core.Point{X: 128, Y: 64}, Size: core.Size{Width: 64, Height: 64}},      // touching right edge of previous
		{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 0, Height: 10}},          // degenerate empty width
		{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 10, Height: 0}},          // degenerate empty height
	}
	for _, b := range special {
		items = append(items, rectItem{id: len(items), bounds: b})
	}
	for len(items) < 1200 {
		x := float32(rng.Float64()*1100 - 550)
		y := float32(rng.Float64()*1100 - 550)
		w := float32(1 + rng.Float64()*80)
		h := float32(1 + rng.Float64()*80)
		items = append(items, rectItem{
			id: len(items),
			bounds: core.Bounds{
				Origin: core.Point{X: core.Points(x), Y: core.Points(y)},
				Size:   core.Size{Width: core.Points(w), Height: core.Points(h)},
			},
		})
	}

	sh := perf.NewSpatialHash[int](64)
	qt := perf.NewQuadtree[int](world, 8, 8)
	for _, it := range items {
		sh.Insert(it.id, it.bounds)
		qt.Insert(it.id, it.bounds)
	}
	if sh.Len() != len(items) || qt.Len() != len(items) {
		t.Fatalf("Len mismatch: sh=%d qt=%d want=%d", sh.Len(), qt.Len(), len(items))
	}

	queries := []core.Bounds{
		{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 0, Height: 0}},               // empty
		{Origin: core.Point{X: -650, Y: -650}, Size: core.Size{Width: 200, Height: 200}},     // partially outside world
		{Origin: core.Point{X: 650, Y: 650}, Size: core.Size{Width: 200, Height: 200}},       // wholly outside world
		{Origin: core.Point{X: 64, Y: 64}, Size: core.Size{Width: 64, Height: 64}},           // exact cell boundary
		{Origin: core.Point{X: -1000, Y: -1000}, Size: core.Size{Width: 2000, Height: 2000}}, // covers everything
	}
	for range 100 {
		x := float32(rng.Float64()*1200 - 600)
		y := float32(rng.Float64()*1200 - 600)
		w := float32(5 + rng.Float64()*250)
		h := float32(5 + rng.Float64()*250)
		queries = append(queries, core.Bounds{
			Origin: core.Point{X: core.Points(x), Y: core.Points(y)},
			Size:   core.Size{Width: core.Points(w), Height: core.Points(h)},
		})
	}

	var shOut, shOut2, qtOut []int
	for qi, area := range queries {
		want := bruteQuery(items, area)

		shOut = sh.Query(area, shOut[:0])
		shOut2 = sh.Query(area, shOut2[:0])
		if !slices.Equal(shOut, shOut2) {
			t.Fatalf("query %d: SpatialHash order not deterministic: %v vs %v", qi, shOut, shOut2)
		}
		qtOut = qt.Query(area, qtOut[:0])

		slices.Sort(shOut)
		slices.Sort(qtOut)
		if !slices.Equal(shOut, want) {
			t.Fatalf("query %d (%+v): SpatialHash got %d items, want %d\ngot:  %v\nwant: %v", qi, area, len(shOut), len(want), shOut, want)
		}
		if !slices.Equal(qtOut, want) {
			t.Fatalf("query %d (%+v): Quadtree got %d items, want %d\ngot:  %v\nwant: %v", qi, area, len(qtOut), len(want), qtOut, want)
		}
	}

	// QueryPoint equivalence across random and exact-boundary points.
	points := []core.Point{
		{X: -500, Y: -500},
		{X: 0, Y: 0},
		{X: 64, Y: 64},
		{X: 128, Y: 64},
		{X: 720, Y: 720},
		{X: 500, Y: 500},
	}
	for range 100 {
		points = append(points, core.Point{
			X: core.Points(rng.Float64()*1200 - 600),
			Y: core.Points(rng.Float64()*1200 - 600),
		})
	}
	for pi, pt := range points {
		want := bruteQueryPoint(items, pt)
		shOut = sh.QueryPoint(pt, shOut[:0])
		qtOut = qt.QueryPoint(pt, qtOut[:0])
		slices.Sort(shOut)
		slices.Sort(qtOut)
		if !slices.Equal(shOut, want) {
			t.Fatalf("point %d (%+v): SpatialHash got %v, want %v", pi, pt, shOut, want)
		}
		if !slices.Equal(qtOut, want) {
			t.Fatalf("point %d (%+v): Quadtree got %v, want %v", pi, pt, qtOut, want)
		}
	}
}

func TestSpatialHashUpdateRemoveAndQueryPairs(t *testing.T) {
	rng := rand.New(rand.NewPCG(77, 88))
	sh := perf.NewSpatialHash[int](32)

	active := make(map[int]core.Bounds)
	for i := range 250 {
		b := core.Bounds{
			Origin: core.Point{X: core.Points(rng.Float64()*400 - 200), Y: core.Points(rng.Float64()*400 - 200)},
			Size:   core.Size{Width: core.Points(10 + rng.Float64()*90), Height: core.Points(10 + rng.Float64()*90)},
		}
		sh.Insert(i, b)
		active[i] = b
	}

	// Remove every 5th item.
	for i := 0; i < 250; i += 5 {
		sh.Remove(i)
		delete(active, i)
	}
	sh.Remove(99999) // absent remove is a no-op

	// Update half of remaining items (some in-cell, some far away).
	for id, b := range active {
		if id%3 == 0 {
			b.Origin.X += 1 // likely same cell fast path
		} else if id%3 == 1 {
			b.Origin.X = core.Points(rng.Float64()*400 - 200)
			b.Origin.Y = core.Points(rng.Float64()*400 - 200)
		}
		sh.Update(id, b)
		active[id] = b
	}

	if sh.Len() != len(active) {
		t.Fatalf("Len=%d, want %d", sh.Len(), len(active))
	}

	type pair struct{ a, b int }
	norm := func(a, b int) pair {
		if a > b {
			return pair{b, a}
		}
		return pair{a, b}
	}

	// Brute-force reference pairs.
	ids := make([]int, 0, len(active))
	for id := range active {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	wantPairs := make(map[pair]struct{})
	for i := range ids {
		for j := i + 1; j < len(ids); j++ {
			if perf.Intersects(active[ids[i]], active[ids[j]]) {
				wantPairs[pair{ids[i], ids[j]}] = struct{}{}
			}
		}
	}

	gotPairs := make(map[pair]struct{})
	sh.QueryPairs(func(a, b int) {
		if a == b {
			t.Fatalf("self-pair emitted: %d", a)
		}
		p := norm(a, b)
		if _, dup := gotPairs[p]; dup {
			t.Fatalf("duplicate pair emitted: %+v", p)
		}
		gotPairs[p] = struct{}{}
	})

	if !reflect.DeepEqual(gotPairs, wantPairs) {
		t.Fatalf("QueryPairs mismatch: got %d pairs, want %d", len(gotPairs), len(wantPairs))
	}

	// QueryPairs must be 0 allocs/op.
	pairCount := 0
	allocs := testing.AllocsPerRun(20, func() {
		pairCount = 0
		sh.QueryPairs(func(_, _ int) { pairCount++ })
	})
	if allocs != 0 {
		t.Fatalf("QueryPairs allocated %.1f times/run, want 0", allocs)
	}
	if pairCount != len(wantPairs) {
		t.Fatalf("QueryPairs counted %d, want %d", pairCount, len(wantPairs))
	}
}

func TestQuadtreeRemoveUpdateAndDeepCluster(t *testing.T) {
	world := core.Bounds{
		Origin: core.Point{X: 0, Y: 0},
		Size:   core.Size{Width: 512, Height: 512},
	}
	qt := perf.NewQuadtree[int](world, 4, 6)

	// 30 identical rects at the same spot must hit maxDepth cleanly without infinite recursion.
	same := core.Bounds{Origin: core.Point{X: 10, Y: 10}, Size: core.Size{Width: 2, Height: 2}}
	for i := range 30 {
		qt.Insert(i, same)
	}
	if qt.Len() != 30 {
		t.Fatalf("Len=%d, want 30", qt.Len())
	}

	// Remove half, update 5 to another quadrant, verify queries.
	for i := 0; i < 15; i++ {
		qt.Remove(i)
	}
	qt.Remove(999) // no-op
	far := core.Bounds{Origin: core.Point{X: 400, Y: 400}, Size: core.Size{Width: 16, Height: 16}}
	for i := 15; i < 20; i++ {
		qt.Update(i, far)
	}
	// Duplicate insert updates cleanly without duplicating count.
	qt.Insert(19, far)
	if qt.Len() != 15 {
		t.Fatalf("Len after remove+update=%d, want 15", qt.Len())
	}

	nearHits := qt.Query(core.Bounds{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 50, Height: 50}}, nil)
	farHits := qt.Query(core.Bounds{Origin: core.Point{X: 390, Y: 390}, Size: core.Size{Width: 40, Height: 40}}, nil)
	slices.Sort(nearHits)
	slices.Sort(farHits)
	if !slices.Equal(nearHits, []int{20, 21, 22, 23, 24, 25, 26, 27, 28, 29}) {
		t.Fatalf("nearHits=%v", nearHits)
	}
	if !slices.Equal(farHits, []int{15, 16, 17, 18, 19}) {
		t.Fatalf("farHits=%v", farHits)
	}

	qt.Clear()
	if qt.Len() != 0 {
		t.Fatalf("Len after Clear=%d, want 0", qt.Len())
	}
	if hits := qt.Query(world, nil); len(hits) != 0 {
		t.Fatalf("Query after Clear returned %v", hits)
	}
}

func TestSpatialAndQuadtreeZeroAllocPerFrame(t *testing.T) {
	rng := rand.New(rand.NewPCG(1234, 5678))
	world := core.Bounds{
		Origin: core.Point{X: 0, Y: 0},
		Size:   core.Size{Width: 2048, Height: 2048},
	}
	items := make([]rectItem, 1000)
	for i := range items {
		items[i] = rectItem{
			id: i,
			bounds: core.Bounds{
				Origin: core.Point{X: core.Points(rng.Float64() * 2000), Y: core.Points(rng.Float64() * 2000)},
				Size:   core.Size{Width: core.Points(8 + rng.Float64()*32), Height: core.Points(8 + rng.Float64()*32)},
			},
		}
	}
	queryArea := core.Bounds{
		Origin: core.Point{X: 600, Y: 600},
		Size:   core.Size{Width: 300, Height: 300},
	}

	sh := perf.NewSpatialHash[int](64)
	qt := perf.NewQuadtree[int](world, 8, 8)
	out := make([]int, 0, 1024)

	// Warm up backing buffers and node/bucket pools.
	for range 2 {
		sh.Clear()
		qt.Clear()
		for _, it := range items {
			sh.Insert(it.id, it.bounds)
			qt.Insert(it.id, it.bounds)
		}
		out = sh.Query(queryArea, out[:0])
		out = qt.Query(queryArea, out[:0])
	}

	shAllocs := testing.AllocsPerRun(20, func() {
		sh.Clear()
		for _, it := range items {
			sh.Insert(it.id, it.bounds)
		}
		out = sh.Query(queryArea, out[:0])
	})
	if shAllocs != 0 {
		t.Fatalf("SpatialHash Clear+Insert(1000)+Query allocated %.1f times/run, want 0", shAllocs)
	}

	qtAllocs := testing.AllocsPerRun(20, func() {
		qt.Clear()
		for _, it := range items {
			qt.Insert(it.id, it.bounds)
		}
		out = qt.Query(queryArea, out[:0])
	})
	if qtAllocs != 0 {
		t.Fatalf("Quadtree Clear+Insert(1000)+Query allocated %.1f times/run, want 0", qtAllocs)
	}
}

// --- #225 Culling Off-Screen Objects ---

func TestIntersectsEdgeCases(t *testing.T) {
	a := core.Bounds{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 10, Height: 10}}
	cases := []struct {
		name string
		b    core.Bounds
		want bool
	}{
		{"overlap", core.Bounds{Origin: core.Point{X: 5, Y: 5}, Size: core.Size{Width: 10, Height: 10}}, true},
		{"contained", core.Bounds{Origin: core.Point{X: 2, Y: 2}, Size: core.Size{Width: 3, Height: 3}}, true},
		{"touchRight", core.Bounds{Origin: core.Point{X: 10, Y: 0}, Size: core.Size{Width: 10, Height: 10}}, false},
		{"touchBottom", core.Bounds{Origin: core.Point{X: 0, Y: 10}, Size: core.Size{Width: 10, Height: 10}}, false},
		{"touchLeft", core.Bounds{Origin: core.Point{X: -10, Y: 0}, Size: core.Size{Width: 10, Height: 10}}, false},
		{"disjoint", core.Bounds{Origin: core.Point{X: 20, Y: 20}, Size: core.Size{Width: 5, Height: 5}}, false},
		{"zeroWidth", core.Bounds{Origin: core.Point{X: 5, Y: 5}, Size: core.Size{Width: 0, Height: 5}}, false},
		{"negHeight", core.Bounds{Origin: core.Point{X: 5, Y: 5}, Size: core.Size{Width: 5, Height: -5}}, false},
		{"nan", core.Bounds{Origin: core.Point{X: core.Points(math.NaN()), Y: 0}, Size: core.Size{Width: 5, Height: 5}}, false},
	}
	for _, tc := range cases {
		if got := perf.Intersects(a, tc.b); got != tc.want {
			t.Errorf("%s: Intersects=%v, want %v", tc.name, got, tc.want)
		}
		if got := perf.Intersects(tc.b, a); got != tc.want {
			t.Errorf("%s (swapped): Intersects=%v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSpriteBoundsExactTransform(t *testing.T) {
	hero := sprite.Frame{Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16}
	coin := sprite.Frame{Name: "coin", X: 16, Y: 0, W: 8, H: 10, OffsetX: 4, OffsetY: 3, Width: 16, Height: 16}

	// Reference corner transform directly from sprite.Sprite.draw's affine matrix.
	refBounds := func(s *sprite.Sprite) core.Bounds {
		f := s.Frame
		sx, sy := s.ScaleX, s.ScaleY
		var tx, ty float32
		if s.FlipX {
			sx, tx = -sx, f.Width
		}
		if s.FlipY {
			sy, ty = -sy, f.Height
		}
		u0 := s.ScaleX * (tx - s.OriginX)
		v0 := s.ScaleY * (ty - s.OriginY)
		sn, cs := math.Sincos(float64(s.Rotation))
		sin, cos := float32(sn), float32(cs)
		a, b := cos*sx, sin*sx
		c, dd := -sin*sy, cos*sy
		e := s.X + cos*u0 - sin*v0
		ff := s.Y + sin*u0 + cos*v0

		corners := [4][2]float32{
			{f.OffsetX, f.OffsetY},
			{f.OffsetX + f.W, f.OffsetY},
			{f.OffsetX, f.OffsetY + f.H},
			{f.OffsetX + f.W, f.OffsetY + f.H},
		}
		minX, minY := float32(math.Inf(1)), float32(math.Inf(1))
		maxX, maxY := float32(math.Inf(-1)), float32(math.Inf(-1))
		for _, pt := range corners {
			wx := a*pt[0] + c*pt[1] + e
			wy := b*pt[0] + dd*pt[1] + ff
			minX = min(minX, wx)
			minY = min(minY, wy)
			maxX = max(maxX, wx)
			maxY = max(maxY, wy)
		}
		return core.Bounds{
			Origin: core.Point{X: core.Points(minX), Y: core.Points(minY)},
			Size:   core.Size{Width: core.Points(maxX - minX), Height: core.Points(maxY - minY)},
		}
	}

	cases := []struct {
		name string
		edit func(*sprite.Sprite)
		f    sprite.Frame
	}{
		{"plain", func(s *sprite.Sprite) { s.X, s.Y = 10, 20 }, hero},
		{"trimmed", func(s *sprite.Sprite) { s.X, s.Y = 7, 9 }, coin},
		{"scaledOrigin", func(s *sprite.Sprite) { s.X, s.Y, s.OriginX, s.OriginY, s.ScaleX, s.ScaleY = 50, 50, 8, 8, 2, 3 }, hero},
		{"negScale", func(s *sprite.Sprite) { s.X, s.Y, s.OriginX, s.OriginY, s.ScaleX, s.ScaleY = 50, 50, 8, 8, -2, -1.5 }, coin},
		{"flipX", func(s *sprite.Sprite) { s.X, s.Y, s.FlipX = 200, 0, true }, coin},
		{"flipXYAndNegScale", func(s *sprite.Sprite) {
			s.X, s.Y, s.OriginX, s.OriginY = 120, 80, 6, 4
			s.ScaleX, s.ScaleY = -1.5, 2
			s.FlipX, s.FlipY = true, true
		}, coin},
		{"rot90", func(s *sprite.Sprite) { s.X, s.Y, s.OriginX, s.OriginY, s.Rotation = 100, 100, 8, 8, math.Pi/2 }, hero},
		{"rot45", func(s *sprite.Sprite) { s.X, s.Y, s.OriginX, s.OriginY, s.Rotation = 100, 100, 8, 8, math.Pi/4 }, hero},
		{"rotArbitraryTrimmedFlipped", func(s *sprite.Sprite) {
			s.X, s.Y, s.OriginX, s.OriginY = 30, -40, 10, 5
			s.ScaleX, s.ScaleY = 1.75, -0.5
			s.FlipX, s.FlipY = true, true
			s.Rotation = -1.234
		}, coin},
	}

	near := func(a, b core.Points) bool { return math.Abs(float64(a-b)) < 1e-4 }
	for _, tc := range cases {
		s := sprite.NewSprite(tc.f)
		tc.edit(&s)
		got := perf.SpriteBounds(&s)
		want := refBounds(&s)
		if !near(got.Origin.X, want.Origin.X) || !near(got.Origin.Y, want.Origin.Y) ||
			!near(got.Size.Width, want.Size.Width) || !near(got.Size.Height, want.Size.Height) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, want)
		}
	}

	// Degenerate / nil inputs return zero bounds.
	if b := perf.SpriteBounds(nil); b != (core.Bounds{}) {
		t.Fatalf("SpriteBounds(nil) = %+v, want zero", b)
	}
	zero := sprite.Sprite{}
	if b := perf.SpriteBounds(&zero); b != (core.Bounds{}) {
		t.Fatalf("SpriteBounds(zero) = %+v, want zero", b)
	}
}

func TestCullSpritesEdgeCasesAndZeroAlloc(t *testing.T) {
	view := core.Bounds{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 100, Height: 100}}
	frame := sprite.Frame{Name: "box", W: 20, H: 20, Width: 20, Height: 20}

	makeSprite := func(edit func(*sprite.Sprite)) *sprite.Sprite {
		s := sprite.NewSprite(frame)
		edit(&s)
		return &s
	}

	// Rotated 45° centered at (-12, 50): half-extent is 10*sqrt(2) ≈ 14.14 > 12 -> visible!
	rotEdgeIn := makeSprite(func(s *sprite.Sprite) {
		s.X, s.Y, s.OriginX, s.OriginY, s.Rotation = -12, 50, 10, 10, math.Pi/4
	})
	// Unrotated centered at (-12, 50): half-extent is 10 < 12 -> culled!
	rotEdgeOut := makeSprite(func(s *sprite.Sprite) {
		s.X, s.Y, s.OriginX, s.OriginY = -12, 50, 10, 10
	})
	// Scaled 3x at (125, 50) centered: half-extent 30 -> spans [95, 155] -> visible!
	scaledIn := makeSprite(func(s *sprite.Sprite) {
		s.X, s.Y, s.OriginX, s.OriginY, s.ScaleX, s.ScaleY = 125, 50, 10, 10, 3, 1
	})
	// Flipped trimmed frame just poking into right edge.
	trimmed := sprite.Frame{Name: "t", W: 6, H: 10, OffsetX: 2, OffsetY: 0, Width: 20, Height: 10}
	flippedIn := makeSprite(func(s *sprite.Sprite) {
		s.Frame = trimmed
		// FlipX maps [2, 8] inside Width=20 to [12, 18]. At X=85, world X is [97, 103] -> intersects [0,100).
		s.X, s.Y, s.FlipX = 85, 10, true
	})
	flippedOut := makeSprite(func(s *sprite.Sprite) {
		s.Frame = trimmed
		// FlipX maps [2, 8] to [12, 18]. At X=88, world X is [100, 106] -> touching edge, outside!
		s.X, s.Y, s.FlipX = 88, 10, true
	})
	invisibleAlpha := makeSprite(func(s *sprite.Sprite) {
		s.X, s.Y, s.Alpha = 50, 50, 0
	})

	src := []*sprite.Sprite{rotEdgeIn, rotEdgeOut, scaledIn, flippedIn, flippedOut, invisibleAlpha, nil}
	got := perf.CullSprites(view, src, nil)
	want := []*sprite.Sprite{rotEdgeIn, scaledIn, flippedIn}
	if !slices.Equal(got, want) {
		t.Fatalf("CullSprites got %d items, want %d", len(got), len(want))
	}

	// Generic Cull[T] equivalence & zero alloc.
	type entity struct {
		id int
		b  core.Bounds
	}
	ents := []entity{
		{1, core.Bounds{Origin: core.Point{X: 10, Y: 10}, Size: core.Size{Width: 10, Height: 10}}},
		{2, core.Bounds{Origin: core.Point{X: 200, Y: 10}, Size: core.Size{Width: 10, Height: 10}}},
		{3, core.Bounds{Origin: core.Point{X: -5, Y: 50}, Size: core.Size{Width: 10, Height: 10}}},
	}
	entBuf := make([]entity, 0, len(ents))
	entAllocs := testing.AllocsPerRun(50, func() {
		entBuf = perf.Cull(view, ents, func(e entity) core.Bounds { return e.b }, entBuf[:0])
	})
	if len(entBuf) != 2 || entBuf[0].id != 1 || entBuf[1].id != 3 {
		t.Fatalf("Cull got %+v", entBuf)
	}
	if entAllocs != 0 {
		t.Fatalf("Cull allocated %.1f times/run, want 0", entAllocs)
	}

	// CullSprites on 1,000 sprites with reusable dst[:0] must run at 0 allocs/op.
	many := make([]*sprite.Sprite, 1000)
	for i := range many {
		s := sprite.NewSprite(frame)
		if i < 20 {
			s.X, s.Y = float32(i*4), 20
		} else {
			s.X, s.Y = 500+float32(i), 500
		}
		many[i] = &s
	}
	dst := make([]*sprite.Sprite, 0, 64)
	allocs := testing.AllocsPerRun(50, func() {
		dst = perf.CullSprites(view, many, dst[:0])
	})
	if len(dst) != 20 {
		t.Fatalf("CullSprites(1000) returned %d visible, want 20", len(dst))
	}
	if allocs != 0 {
		t.Fatalf("CullSprites(1000) allocated %.1f times/run, want 0", allocs)
	}
}

func TestCullBatchDrawsOnlyVisible20Of1000(t *testing.T) {
	sheet := &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:perf_sheet.png"},
		Frames: map[string]sprite.Frame{
			"hero": {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			"coin": {Name: "coin", X: 16, Y: 0, W: 8, H: 10, OffsetX: 4, OffsetY: 3, Width: 16, Height: 16},
		},
	}
	view := core.Bounds{
		Origin: core.Point{X: 0, Y: 0},
		Size:   core.Size{Width: 800, Height: 600},
	}

	all := make([]*sprite.Sprite, 0, 1000)
	expectedVisible := make([]*sprite.Sprite, 0, 20)

	// Place 1,000 sprites: exactly 20 visible inside [0,800)x[0,600), 980 off-screen.
	for i := range 1000 {
		var s sprite.Sprite
		if i%2 == 0 {
			s = sprite.NewSprite(sheet.Frames["hero"])
		} else {
			s = sprite.NewSprite(sheet.Frames["coin"])
		}
		s.Z = (i * 7) % 13

		if i%50 == 0 { // 20 visible sprites spread across the slice (indices 0, 50, ..., 950)
			k := i / 50
			s.X = float32(20 + (k%5)*160)
			s.Y = float32(20 + (k/5)*140)
			switch k % 4 {
			case 1:
				s.OriginX, s.OriginY, s.Rotation = 8, 8, math.Pi/4
			case 2:
				s.FlipX = true
				s.ScaleX, s.ScaleY = 1.5, 1.5
			case 3:
				s.Alpha = 0.75
			}
			sp := &s
			all = append(all, sp)
			expectedVisible = append(expectedVisible, sp)
		} else {
			// Off-screen in all four directions.
			switch i % 4 {
			case 0:
				s.X, s.Y = -200-float32(i), 100
			case 1:
				s.X, s.Y = 900+float32(i), 200
			case 2:
				s.X, s.Y = 300, -100-float32(i)
			case 3:
				s.X, s.Y = 400, 700+float32(i)
			}
			all = append(all, &s)
		}
	}
	if len(all) != 1000 || len(expectedVisible) != 20 {
		t.Fatalf("setup error: all=%d visible=%d", len(all), len(expectedVisible))
	}

	origSnapshot := slices.Clone(all)
	fullBatch := &sprite.Batch{Atlas: sheet, Sprites: all}
	refBatch := &sprite.Batch{Atlas: sheet, Sprites: expectedVisible}
	var culler perf.BatchCuller

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "culled", Style: core.Style{Width: core.Pt(200), Height: core.Pt(200)}, Draw: func(d *core.Drawing, _ core.Size) {
				perf.CullBatch(fullBatch, view, d)
			}},
			core.Canvas{TestID: "culler", Style: core.Style{Width: core.Pt(200), Height: core.Pt(200)}, Draw: func(d *core.Drawing, _ core.Size) {
				culler.Draw(fullBatch, view, d)
			}},
			core.Canvas{TestID: "ref", Style: core.Style{Width: core.Pt(200), Height: core.Pt(200)}, Draw: func(d *core.Drawing, _ core.Size) {
				refBatch.Draw(d)
			}},
		}}
	})

	gotCulled := app.Find("culled").Ops
	gotCuller := app.Find("culler").Ops
	wantOps := app.Find("ref").Ops

	// Count DrawImage ops — must be exactly 20!
	drawImages := 0
	for _, op := range gotCulled {
		if op.Kind == core.DrawImage {
			drawImages++
		}
	}
	if drawImages != 20 {
		t.Fatalf("CullBatch emitted %d DrawImage ops, want 20", drawImages)
	}
	if !reflect.DeepEqual(gotCulled, wantOps) {
		t.Fatalf("CullBatch ops (%d) did not match 20-sprite reference batch ops (%d)", len(gotCulled), len(wantOps))
	}
	if !reflect.DeepEqual(gotCuller, wantOps) {
		t.Fatalf("BatchCuller.Draw ops (%d) did not match 20-sprite reference batch ops (%d)", len(gotCuller), len(wantOps))
	}
	if !slices.Equal(fullBatch.Sprites, origSnapshot) {
		t.Fatal("CullBatch mutated batch.Sprites")
	}
}

// --- Benchmarks ---

func BenchmarkPoolAcquireRelease100k(b *testing.B) {
	p := perf.NewPool(64, func(t *testBullet) {
		t.X, t.Y, t.VX, t.VY, t.Life = 0, 0, 0, 0, 0
	})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for range 100_000 {
			obj := p.Acquire()
			obj.X = 1
			p.Release(obj)
		}
	}
}

func BenchmarkArenaAddRemove(b *testing.B) {
	a := perf.NewArena[testBullet](1024, nil)
	handles := make([]perf.Handle, 0, 1024)
	for i := range 1024 {
		handles = append(handles, a.Add(testBullet{Life: i}))
	}
	a.Reset()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		handles = handles[:0]
		for i := range 1024 {
			handles = append(handles, a.Add(testBullet{Life: i}))
		}
		for _, h := range handles {
			a.Remove(h)
		}
	}
}

func BenchmarkSpatialHashFrame1000(b *testing.B) {
	rng := rand.New(rand.NewPCG(1, 2))
	items := make([]rectItem, 1000)
	for i := range items {
		items[i] = rectItem{
			id: i,
			bounds: core.Bounds{
				Origin: core.Point{X: core.Points(rng.Float64() * 2000), Y: core.Points(rng.Float64() * 2000)},
				Size:   core.Size{Width: core.Points(16 + rng.Float64()*24), Height: core.Points(16 + rng.Float64()*24)},
			},
		}
	}
	area := core.Bounds{Origin: core.Point{X: 800, Y: 800}, Size: core.Size{Width: 200, Height: 200}}
	sh := perf.NewSpatialHash[int](64)
	out := make([]int, 0, 256)

	// Warm up.
	for _, it := range items {
		sh.Insert(it.id, it.bounds)
	}
	out = sh.Query(area, out[:0])

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		sh.Clear()
		for _, it := range items {
			sh.Insert(it.id, it.bounds)
		}
		out = sh.Query(area, out[:0])
	}
	_ = out
}

func BenchmarkSpatialHashQuery1000(b *testing.B) {
	rng := rand.New(rand.NewPCG(1, 2))
	sh := perf.NewSpatialHash[int](64)
	for i := range 1000 {
		sh.Insert(i, core.Bounds{
			Origin: core.Point{X: core.Points(rng.Float64() * 2000), Y: core.Points(rng.Float64() * 2000)},
			Size:   core.Size{Width: core.Points(16 + rng.Float64()*24), Height: core.Points(16 + rng.Float64()*24)},
		})
	}
	area := core.Bounds{Origin: core.Point{X: 800, Y: 800}, Size: core.Size{Width: 200, Height: 200}}
	out := make([]int, 0, 256)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		out = sh.Query(area, out[:0])
	}
	_ = out
}

func BenchmarkQuadtreeFrame1000(b *testing.B) {
	rng := rand.New(rand.NewPCG(1, 2))
	world := core.Bounds{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 2048, Height: 2048}}
	items := make([]rectItem, 1000)
	for i := range items {
		items[i] = rectItem{
			id: i,
			bounds: core.Bounds{
				Origin: core.Point{X: core.Points(rng.Float64() * 2000), Y: core.Points(rng.Float64() * 2000)},
				Size:   core.Size{Width: core.Points(16 + rng.Float64()*24), Height: core.Points(16 + rng.Float64()*24)},
			},
		}
	}
	area := core.Bounds{Origin: core.Point{X: 800, Y: 800}, Size: core.Size{Width: 200, Height: 200}}
	qt := perf.NewQuadtree[int](world, 8, 8)
	out := make([]int, 0, 256)

	// Warm up.
	for _, it := range items {
		qt.Insert(it.id, it.bounds)
	}
	out = qt.Query(area, out[:0])

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		qt.Clear()
		for _, it := range items {
			qt.Insert(it.id, it.bounds)
		}
		out = qt.Query(area, out[:0])
	}
	_ = out
}

func BenchmarkQuadtreeQuery1000(b *testing.B) {
	rng := rand.New(rand.NewPCG(1, 2))
	world := core.Bounds{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 2048, Height: 2048}}
	qt := perf.NewQuadtree[int](world, 8, 8)
	for i := range 1000 {
		qt.Insert(i, core.Bounds{
			Origin: core.Point{X: core.Points(rng.Float64() * 2000), Y: core.Points(rng.Float64() * 2000)},
			Size:   core.Size{Width: core.Points(16 + rng.Float64()*24), Height: core.Points(16 + rng.Float64()*24)},
		})
	}
	area := core.Bounds{Origin: core.Point{X: 800, Y: 800}, Size: core.Size{Width: 200, Height: 200}}
	out := make([]int, 0, 256)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		out = qt.Query(area, out[:0])
	}
	_ = out
}

func BenchmarkBruteForceQuery1000(b *testing.B) {
	rng := rand.New(rand.NewPCG(1, 2))
	items := make([]rectItem, 1000)
	for i := range items {
		items[i] = rectItem{
			id: i,
			bounds: core.Bounds{
				Origin: core.Point{X: core.Points(rng.Float64() * 2000), Y: core.Points(rng.Float64() * 2000)},
				Size:   core.Size{Width: core.Points(16 + rng.Float64()*24), Height: core.Points(16 + rng.Float64()*24)},
			},
		}
	}
	area := core.Bounds{Origin: core.Point{X: 800, Y: 800}, Size: core.Size{Width: 200, Height: 200}}
	out := make([]int, 0, 256)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		out = out[:0]
		for _, it := range items {
			if perf.Intersects(it.bounds, area) {
				out = append(out, it.id)
			}
		}
	}
	_ = out
}

func BenchmarkCullSprites1000(b *testing.B) {
	frame := sprite.Frame{Name: "hero", W: 16, H: 16, Width: 16, Height: 16}
	view := core.Bounds{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 800, Height: 600}}
	sprites := make([]*sprite.Sprite, 1000)
	for i := range sprites {
		s := sprite.NewSprite(frame)
		if i < 20 {
			s.X, s.Y = float32(i*35), 200
		} else {
			s.X, s.Y = 1200+float32(i), 900
		}
		sprites[i] = &s
	}
	dst := make([]*sprite.Sprite, 0, 64)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		dst = perf.CullSprites(view, sprites, dst[:0])
	}
	_ = dst
}
