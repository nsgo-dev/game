package perf

import "iter"

const defaultPoolChunk = 16

// Pool is a deterministic, single-threaded object pool for game-loop entities
// such as bullets, enemies, particles, and transient structs.
//
// Unlike sync.Pool, Pool never discards pooled objects on garbage collection:
// once warmed up, Acquire and Release run in O(1) time with 0 allocations.
// Pointers returned by Acquire remain stable across pool growth because memory
// is allocated in fixed chunks.
type Pool[T any] struct {
	chunks [][]T
	free   []*T
	reset  func(*T)
	active int
	total  int
}

// NewPool creates a Pool pre-populated with initialCap reusable objects.
//
// When reset is non-nil, it is called on each object when released or when
// Reset is called, allowing callers to clear fields while keeping internal
// slice capacities. When reset is nil, objects are zeroed on release and Reset.
func NewPool[T any](initialCap int, reset func(*T)) *Pool[T] {
	p := &Pool[T]{reset: reset}
	if initialCap > 0 {
		p.grow(initialCap)
	}
	return p
}

// Acquire returns a pointer to a pooled object, allocating a new chunk only
// when no free objects remain.
func (p *Pool[T]) Acquire() *T {
	if len(p.free) == 0 {
		growBy := p.total
		if growBy < defaultPoolChunk {
			growBy = defaultPoolChunk
		}
		p.grow(growBy)
	}
	last := len(p.free) - 1
	v := p.free[last]
	p.free = p.free[:last]
	p.active++
	return v
}

// Release resets v and returns it to the free list for future Acquire calls.
// Passing nil is a no-op.
func (p *Pool[T]) Release(v *T) {
	if v == nil {
		return
	}
	if p.reset != nil {
		p.reset(v)
	} else {
		var zero T
		*v = zero
	}
	if p.active > 0 {
		p.active--
	}
	p.free = append(p.free, v)
	if p.active+len(p.free) > p.total {
		p.total = p.active + len(p.free)
	}
}

// Active returns the number of objects currently checked out via Acquire.
func (p *Pool[T]) Active() int { return p.active }

// Free returns the number of objects immediately available without allocation.
func (p *Pool[T]) Free() int { return len(p.free) }

// Cap returns the total number of slots managed by the pool.
func (p *Pool[T]) Cap() int { return p.total }

// Prewarm ensures at least n objects are available in the free list so that
// subsequent Acquire calls do not allocate.
func (p *Pool[T]) Prewarm(n int) {
	if need := n - len(p.free); need > 0 {
		p.grow(need)
	}
}

// Reset reclaims all objects in the pool's chunks back to the free list
// without freeing backing memory. Every object is reset via the pool's reset
// hook (or zeroed if nil).
func (p *Pool[T]) Reset() {
	p.free = p.free[:0]
	total := 0
	for c := len(p.chunks) - 1; c >= 0; c-- {
		chunk := p.chunks[c]
		total += len(chunk)
		if p.reset != nil {
			for i := len(chunk) - 1; i >= 0; i-- {
				p.reset(&chunk[i])
				p.free = append(p.free, &chunk[i])
			}
		} else {
			clear(chunk)
			for i := len(chunk) - 1; i >= 0; i-- {
				p.free = append(p.free, &chunk[i])
			}
		}
	}
	p.active = 0
	p.total = total
}

func (p *Pool[T]) grow(n int) {
	chunk := make([]T, n)
	p.chunks = append(p.chunks, chunk)
	if cap(p.free) < len(p.free)+n {
		next := make([]*T, len(p.free), max(len(p.free)+n, cap(p.free)*2))
		copy(next, p.free)
		p.free = next
	}
	// Push in reverse so Acquire pops elements in forward memory order.
	for i := n - 1; i >= 0; i-- {
		p.free = append(p.free, &chunk[i])
	}
	p.total += n
}

// Handle is a stable generational reference to an element inside an [Arena].
// Its zero value is always invalid.
type Handle struct {
	index uint32
	gen   uint32
}

// IsZero reports whether h is the invalid zero handle.
func (h Handle) IsZero() bool { return h.gen == 0 }

type arenaSlot struct {
	dense uint32
	gen   uint32
	alive bool
}

// Arena is a dense, contiguous object pool with stable generational [Handle]s
// and O(1) swap-remove. Active values live contiguously in memory for
// cache-friendly iteration via [Arena.ForEach], [Arena.All], and [Arena.Slice].
type Arena[T any] struct {
	items         []T
	denseToSparse []uint32
	sparse        []arenaSlot
	freeSlots     []uint32
	reset         func(*T)
}

// NewArena creates an Arena with initial backing capacity for initialCap items.
// When reset is non-nil, it is invoked on removed or reset elements instead of
// zeroing them.
func NewArena[T any](initialCap int, reset func(*T)) *Arena[T] {
	if initialCap < 0 {
		initialCap = 0
	}
	return &Arena[T]{
		items:         make([]T, 0, initialCap),
		denseToSparse: make([]uint32, 0, initialCap),
		sparse:        make([]arenaSlot, 0, initialCap),
		freeSlots:     make([]uint32, 0, initialCap),
		reset:         reset,
	}
}

// Add inserts v into the arena and returns its stable handle.
func (a *Arena[T]) Add(v T) Handle {
	h, ptr := a.Alloc()
	*ptr = v
	return h
}

// Alloc reserves a slot in the arena and returns its handle and a pointer to
// the newly reserved element for in-place initialization. The pointer is valid
// only until the next Add, Alloc, Remove, or Reset.
func (a *Arena[T]) Alloc() (Handle, *T) {
	var slotIdx uint32
	if n := len(a.freeSlots); n > 0 {
		slotIdx = a.freeSlots[n-1]
		a.freeSlots = a.freeSlots[:n-1]
	} else {
		slotIdx = uint32(len(a.sparse))
		a.sparse = append(a.sparse, arenaSlot{gen: 1})
	}
	denseIdx := uint32(len(a.items))
	var zero T
	a.items = append(a.items, zero)
	a.denseToSparse = append(a.denseToSparse, slotIdx)

	slot := &a.sparse[slotIdx]
	if slot.gen == 0 {
		slot.gen = 1
	}
	slot.dense = denseIdx
	slot.alive = true
	return Handle{index: slotIdx, gen: slot.gen}, &a.items[denseIdx]
}

// Get resolves h to a pointer to its live element. It returns (nil, false) if
// h is zero or has been removed. The returned pointer is valid until the next
// mutation of the arena.
func (a *Arena[T]) Get(h Handle) (*T, bool) {
	if h.gen == 0 || int(h.index) >= len(a.sparse) {
		return nil, false
	}
	slot := a.sparse[h.index]
	if !slot.alive || slot.gen != h.gen {
		return nil, false
	}
	return &a.items[slot.dense], true
}

// Has reports whether h currently refers to a live element in the arena.
func (a *Arena[T]) Has(h Handle) bool {
	if h.gen == 0 || int(h.index) >= len(a.sparse) {
		return false
	}
	slot := a.sparse[h.index]
	return slot.alive && slot.gen == h.gen
}

// Remove deletes the element referenced by h in O(1) time via swap-remove and
// invalidates h. It returns false if h is already invalid.
func (a *Arena[T]) Remove(h Handle) bool {
	if h.gen == 0 || int(h.index) >= len(a.sparse) {
		return false
	}
	slot := &a.sparse[h.index]
	if !slot.alive || slot.gen != h.gen {
		return false
	}

	denseIdx := int(slot.dense)
	lastIdx := len(a.items) - 1
	if denseIdx != lastIdx {
		a.items[denseIdx] = a.items[lastIdx]
		movedSlot := a.denseToSparse[lastIdx]
		a.denseToSparse[denseIdx] = movedSlot
		a.sparse[movedSlot].dense = uint32(denseIdx)
	}

	if a.reset != nil {
		a.reset(&a.items[lastIdx])
	} else {
		var zero T
		a.items[lastIdx] = zero
	}
	a.items = a.items[:lastIdx]
	a.denseToSparse = a.denseToSparse[:lastIdx]

	slot.alive = false
	slot.gen++
	if slot.gen == 0 {
		slot.gen = 1
	}
	a.freeSlots = append(a.freeSlots, h.index)
	return true
}

// Len returns the number of live elements in the arena.
func (a *Arena[T]) Len() int { return len(a.items) }

// Cap returns the current dense slice capacity of the arena.
func (a *Arena[T]) Cap() int { return cap(a.items) }

// Slice returns the contiguous slice of all live elements in the arena for
// direct cache-friendly iteration. Callers must not append to or re-slice the
// returned slice header in place of arena mutations.
func (a *Arena[T]) Slice() []T { return a.items }

// ForEach invokes fn for every live element in dense memory order.
// Callers must not add or remove elements while iterating.
func (a *Arena[T]) ForEach(fn func(h Handle, v *T)) {
	for i := range a.items {
		slotIdx := a.denseToSparse[i]
		fn(Handle{index: slotIdx, gen: a.sparse[slotIdx].gen}, &a.items[i])
	}
}

// All returns an iterator over every live (handle, pointer) pair in dense
// memory order, supporting early termination with break.
func (a *Arena[T]) All() iter.Seq2[Handle, *T] {
	return func(yield func(Handle, *T) bool) {
		for i := range a.items {
			slotIdx := a.denseToSparse[i]
			if !yield(Handle{index: slotIdx, gen: a.sparse[slotIdx].gen}, &a.items[i]) {
				return
			}
		}
	}
}

// Reset removes all live elements, invalidating all outstanding handles, while
// retaining all backing capacities for zero-allocation reuse.
func (a *Arena[T]) Reset() {
	if a.reset != nil {
		for i := range a.items {
			a.reset(&a.items[i])
		}
	} else {
		clear(a.items)
	}
	for _, slotIdx := range a.denseToSparse {
		slot := &a.sparse[slotIdx]
		slot.alive = false
		slot.gen++
		if slot.gen == 0 {
			slot.gen = 1
		}
		a.freeSlots = append(a.freeSlots, slotIdx)
	}
	a.items = a.items[:0]
	a.denseToSparse = a.denseToSparse[:0]
}

// SlicePool is a deterministic pool of reusable []T scratch slices (for query
// buffers, draw lists, particle emitters, etc.) that never drops slices on GC.
type SlicePool[T any] struct {
	free      [][]T
	maxPooled int
}

// NewSlicePool creates a SlicePool that retains up to maxPooled slices.
// When maxPooled <= 0, the pool retains all released slices without a cap.
func NewSlicePool[T any](maxPooled int) *SlicePool[T] {
	p := &SlicePool[T]{maxPooled: maxPooled}
	if maxPooled > 0 {
		p.free = make([][]T, 0, maxPooled)
	}
	return p
}

// Acquire returns a zero-length slice with capacity at least minCap, reusing
// the tightest-fitting pooled slice when available.
func (p *SlicePool[T]) Acquire(minCap int) []T {
	if minCap < 0 {
		minCap = 0
	}
	bestIdx := -1
	bestCap := 0
	for i, s := range p.free {
		c := cap(s)
		if c >= minCap && (bestIdx < 0 || c < bestCap) {
			bestIdx = i
			bestCap = c
			if c == minCap {
				break
			}
		}
	}
	if bestIdx >= 0 {
		s := p.free[bestIdx]
		last := len(p.free) - 1
		p.free[bestIdx] = p.free[last]
		p.free[last] = nil
		p.free = p.free[:last]
		return s[:0]
	}
	return make([]T, 0, minCap)
}

// Release clears s's backing elements (preventing retained pointers) and
// returns s[:0] to the pool for reuse.
func (p *SlicePool[T]) Release(s []T) {
	if cap(s) == 0 {
		return
	}
	if p.maxPooled > 0 && len(p.free) >= p.maxPooled {
		return
	}
	clear(s[:cap(s)])
	p.free = append(p.free, s[:0])
}

// Free returns the number of slices currently stored in the pool.
func (p *SlicePool[T]) Free() int { return len(p.free) }

// Reset drops all pooled slice references from the pool.
func (p *SlicePool[T]) Reset() {
	clear(p.free)
	p.free = p.free[:0]
}
