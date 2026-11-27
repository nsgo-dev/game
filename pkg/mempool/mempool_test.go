package mempool_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/mempool"
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
	p := mempool.NewPool(4, func(b *testBullet) {
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
	zeroPool := mempool.NewPool[testBullet](2, nil)
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
	p := mempool.NewPool(128, func(b *testBullet) {
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
	a := mempool.NewArena[testBullet](16, nil)

	var zeroH mempool.Handle
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
	seen := make(map[mempool.Handle]float32)
	a.ForEach(func(h mempool.Handle, v *testBullet) {
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
	handles := make([]mempool.Handle, 0, 128)
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
		a.ForEach(func(_ mempool.Handle, b *testBullet) {
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
	sp := mempool.NewSlicePool[int](4)
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

// --- Benchmarks ---

func BenchmarkPoolAcquireRelease100k(b *testing.B) {
	p := mempool.NewPool(64, func(t *testBullet) {
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
	a := mempool.NewArena[testBullet](1024, nil)
	handles := make([]mempool.Handle, 0, 1024)
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
