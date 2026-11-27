package perf_test

import (
	"fmt"
	"slices"

	"github.com/nsgo-dev/game/pkg/perf"
	"github.com/nsgo-dev/nsgo/core"
)

func box(x, y, w, h core.Points) core.Bounds {
	return core.Bounds{Origin: core.Point{X: x, Y: y}, Size: core.Size{Width: w, Height: h}}
}

type bullet struct{ X, Y, VX float32 }

// A Pool reuses objects such as bullets, so firing does not allocate once the pool is warm.
func ExamplePool() {
	pool := perf.NewPool(8, func(b *bullet) { *b = bullet{} })
	pool.Prewarm(4)
	a := pool.Acquire()
	a.VX = 300
	b := pool.Acquire()
	fmt.Println(pool.Active(), pool.Free())
	pool.Release(a)
	pool.Release(b)
	c := pool.Acquire()
	fmt.Println(pool.Active(), c.VX)
	// Output:
	// 2 6
	// 1 0
}

// An Arena stores values densely and hands out Handles that stay valid until their value is removed, and stop matching after.
func ExampleArena() {
	arena := perf.NewArena[bullet](4, nil)
	h1 := arena.Add(bullet{X: 1})
	h2 := arena.Add(bullet{X: 2})
	arena.Remove(h1)
	_, ok := arena.Get(h1)
	b, _ := arena.Get(h2)
	fmt.Println(arena.Len(), ok, b.X)
	// Output:
	// 1 false 2
}

// A SpatialHash finds the objects near an area without testing every one, here for collision pairs between enemies.
func ExampleSpatialHash() {
	grid := perf.NewSpatialHash[string](32)
	grid.Insert("slime", box(10, 10, 8, 8))
	grid.Insert("bat", box(14, 12, 8, 8))
	grid.Insert("golem", box(200, 200, 16, 16))

	near := grid.Query(box(0, 0, 40, 40), nil)
	slices.Sort(near)
	fmt.Println(near)
	var pairs []string
	grid.QueryPairs(func(a, b string) {
		if a > b {
			a, b = b, a
		}
		pairs = append(pairs, a+"+"+b)
	})
	fmt.Println(pairs)
	// Output:
	// [bat slime]
	// [bat+slime]
}

// A Quadtree indexes objects across a large world and answers point and area queries.
func ExampleQuadtree() {
	qt := perf.NewQuadtree[int](box(0, 0, 1024, 1024), 4, 8)
	for i := range 10 {
		qt.Insert(i, box(core.Points(i)*100, 0, 10, 10))
	}
	hits := qt.Query(box(150, 0, 300, 50), nil)
	slices.Sort(hits)
	fmt.Println(hits, qt.QueryPoint(core.Point{X: 905, Y: 5}, nil))
	// Output:
	// [2 3 4] [9]
}

// Cull keeps only the items whose bounds touch the view, so offscreen objects are not drawn.
func ExampleCull() {
	trees := []core.Bounds{box(0, 0, 10, 10), box(500, 0, 10, 10), box(95, 95, 10, 10)}
	view := box(0, 0, 100, 100)
	visible := perf.Cull(view, trees, func(b core.Bounds) core.Bounds { return b }, nil)
	fmt.Println(len(visible), perf.Intersects(view, trees[1]))
	// Output:
	// 2 false
}

// A SlicePool lends out scratch slices for per-frame work and takes them back for reuse.
func ExampleSlicePool() {
	pool := perf.NewSlicePool[int](4)
	s := pool.Acquire(16)
	s = append(s, 1, 2, 3)
	fmt.Println(len(s), cap(s) >= 16)
	pool.Release(s)
	fmt.Println(pool.Free(), len(pool.Acquire(8)))
	// Output:
	// 3 true
	// 1 0
}
