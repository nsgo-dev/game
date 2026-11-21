package entities

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/nsgo/core"
)

// Zero is the invalid entity handle. Live entities always have a non-zero generation, so Zero is never alive.
const Zero Entity = 0

// ErrWorldDisposed reports an operation attempted on a World that has been disposed.
var ErrWorldDisposed = errors.New("entities: world is disposed")

// Entity is a 64-bit generational handle identifying an entity in a World.
//
// The low 32 bits store the slot index and the high 32 bits store the slot's generation. When an entity is destroyed, its slot is recycled with an incremented generation, so stale handles never alias newly spawned entities.
type Entity uint64

func makeEntity(index, generation uint32) Entity {
	return Entity(generation)<<32 | Entity(index)
}

// Index returns the entity's 32-bit slot index.
func (e Entity) Index() uint32 { return uint32(e) }

// Generation returns the entity's 32-bit generation counter (0 for Zero).
func (e Entity) Generation() uint32 { return uint32(e >> 32) }

// IsZero reports whether e is the invalid Zero handle.
func (e Entity) IsZero() bool { return e == Zero }

// String formats e as "Entity(index:generation)" or "Entity(0)" for Zero.
func (e Entity) String() string {
	if e == Zero {
		return "Entity(0)"
	}
	return fmt.Sprintf("Entity(%d:%d)", e.Index(), e.Generation())
}

// World owns entities, their typed component stores, and registered serialization codecs.
//
// The zero value of World is ready to use; NewWorld returns a freshly allocated one.
type World struct {
	gens  []uint32
	alive []bool
	free  []uint32
	count int

	stores []anyStore
	byType map[typeKey]anyStore

	codecs     map[string]componentCodec
	codecOrder []string
	codecByKey map[typeKey]string

	hierarchy *Hierarchy2D

	disposed bool
}

// NewWorld returns an empty World ready for spawning entities and attaching components.
func NewWorld() *World {
	return &World{}
}

// Attach binds w's lifetime to cx so w.Dispose runs automatically when cx is disposed.
func (w *World) Attach(cx *core.Scope) {
	if cx == nil {
		return
	}
	cx.Cleanup(w.Dispose)
}

// Dispose releases all entities and component storage in w and marks w as disposed.
// Calling Dispose more than once is a no-op.
func (w *World) Dispose() {
	if w.disposed {
		return
	}
	w.Reset()
	w.disposed = true
}

// Disposed reports whether w has been disposed.
func (w *World) Disposed() bool {
	return w.disposed
}

// Spawn creates a new live entity, recycling a previously destroyed slot when available.
// It returns Zero if w has been disposed.
func (w *World) Spawn() Entity {
	if w.disposed {
		return Zero
	}
	if n := len(w.free); n > 0 {
		idx := w.free[n-1]
		w.free = w.free[:n-1]
		w.alive[idx] = true
		w.count++
		return makeEntity(idx, w.gens[idx])
	}
	idx := uint32(len(w.gens))
	w.gens = append(w.gens, 1)
	w.alive = append(w.alive, true)
	w.count++
	return makeEntity(idx, 1)
}

// Destroy removes e and all of its components from w and returns its slot to the free list with an incremented generation.
// Destroying Zero or an entity that is not alive in w is a no-op.
func (w *World) Destroy(e Entity) {
	if !w.Alive(e) {
		return
	}
	if w.hierarchy != nil {
		w.hierarchy.onWorldDestroy(e)
	}
	idx := e.Index()
	w.alive[idx] = false
	gen := w.gens[idx] + 1
	if gen == 0 {
		gen = 1
	}
	w.gens[idx] = gen
	w.free = append(w.free, idx)
	w.count--
	for _, s := range w.stores {
		s.removeIndex(idx)
	}
}

// Alive reports whether e refers to a currently live entity in w.
func (w *World) Alive(e Entity) bool {
	if e == Zero || w.disposed {
		return false
	}
	idx := e.Index()
	if int(idx) >= len(w.alive) {
		return false
	}
	return w.alive[idx] && w.gens[idx] == e.Generation()
}

// Count returns the number of live entities in w.
func (w *World) Count() int {
	return w.count
}

// Each calls fn for every live entity in w in ascending slot-index order until fn returns false.
func (w *World) Each(fn func(Entity) bool) {
	if w.count == 0 || w.disposed {
		return
	}
	for i := 0; i < len(w.alive); i++ {
		if !w.alive[i] {
			continue
		}
		if !fn(makeEntity(uint32(i), w.gens[i])) {
			return
		}
	}
}

// Clear removes all live entities and their components while preserving slot generations, allocated capacities and registered codecs for zero-allocation reuse.
// All entity handles issued before Clear become stale.
func (w *World) Clear() {
	if w.disposed {
		return
	}
	if w.hierarchy != nil {
		w.hierarchy.clear()
	}
	for _, s := range w.stores {
		s.clear()
	}
	w.free = w.free[:0]
	for i := len(w.alive) - 1; i >= 0; i-- {
		if w.alive[i] {
			w.alive[i] = false
			gen := w.gens[i] + 1
			if gen == 0 {
				gen = 1
			}
			w.gens[i] = gen
		}
		w.free = append(w.free, uint32(i))
	}
	w.count = 0
}

// Reset removes all entities and releases entity and component backing storage while keeping registered codecs intact.
func (w *World) Reset() {
	if w.hierarchy != nil {
		w.hierarchy.reset()
	}
	for _, s := range w.stores {
		s.reset()
	}
	w.gens = nil
	w.alive = nil
	w.free = nil
	w.count = 0
}
