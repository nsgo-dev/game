package mempool_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/mempool"
)

type bullet struct{ X, Y, VX float32 }

// A Pool reuses objects such as bullets, so firing does not allocate once the pool is warm.
func ExamplePool() {
	pool := mempool.NewPool(8, func(b *bullet) { *b = bullet{} })
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
	arena := mempool.NewArena[bullet](4, nil)
	h1 := arena.Add(bullet{X: 1})
	h2 := arena.Add(bullet{X: 2})
	arena.Remove(h1)
	_, ok := arena.Get(h1)
	b, _ := arena.Get(h2)
	fmt.Println(arena.Len(), ok, b.X)
	// Output:
	// 1 false 2
}

// A SlicePool lends out scratch slices for per-frame work and takes them back for reuse.
func ExampleSlicePool() {
	pool := mempool.NewSlicePool[int](4)
	s := pool.Acquire(16)
	s = append(s, 1, 2, 3)
	fmt.Println(len(s), cap(s) >= 16)
	pool.Release(s)
	fmt.Println(pool.Free(), len(pool.Acquire(8)))
	// Output:
	// 3 true
	// 1 0
}
