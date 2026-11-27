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

// MakeEntity constructs a generational Entity handle from its 32-bit slot index and generation.
// It returns Zero when generation is 0.
func MakeEntity(index, generation uint32) Entity {
	if generation == 0 {
		return Zero
	}
	return makeEntity(index, generation)
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
	// floorGens is lazily allocated only when Restore rolls a live slot back to
	// a generation below the next unused generation this World already reached.
	// A non-zero floorGens[i] requires slot i to advance to at least floorGens[i]
	// the next time it dies, preventing handles from before the rollback from
	// aliasing future spawns.
	floorGens []uint32
	count     int

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

func nextGen(gen uint32) uint32 {
	gen++
	if gen == 0 {
		return 1
	}
	return gen
}

// advanceGen returns the next unused generation for a live slot idx that is
// being destroyed, honoring any rollback floor recorded on idx.
func (w *World) advanceGen(idx uint32) uint32 {
	gen := nextGen(w.gens[idx])
	if int(idx) < len(w.floorGens) {
		if floor := w.floorGens[idx]; floor > gen {
			gen = floor
		}
		w.floorGens[idx] = 0
	}
	return gen
}

// nextSlotGen returns the next unused generation for slot idx if that slot were dead.
func (w *World) nextSlotGen(idx int) uint32 {
	if idx >= len(w.gens) {
		return 1
	}
	gen := w.gens[idx]
	if idx < len(w.alive) && w.alive[idx] {
		gen = nextGen(gen)
	} else if gen == 0 {
		gen = 1
	}
	if idx < len(w.floorGens) && w.floorGens[idx] > gen {
		gen = w.floorGens[idx]
	}
	return gen
}

// Dispose releases all entities and component storage in w and marks w as disposed.
// Calling Dispose more than once is a no-op.
func (w *World) Dispose() {
	if w.disposed {
		return
	}
	w.Reset()
	w.gens = nil
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
	idx := uint32(len(w.alive))
	gen := uint32(1)
	if int(idx) < len(w.gens) {
		gen = w.gens[idx]
		if gen == 0 {
			gen = 1
			w.gens[idx] = 1
		}
	} else {
		w.gens = append(w.gens, 1)
	}
	w.alive = append(w.alive, true)
	w.count++
	return makeEntity(idx, gen)
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
	w.gens[idx] = w.advanceGen(idx)
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

// Resolve reconstructs the entity handle for (index, generation) and reports whether it is currently live in w.
func (w *World) Resolve(index, generation uint32) (Entity, bool) {
	if w == nil {
		return Zero, false
	}
	e := MakeEntity(index, generation)
	if !w.Alive(e) {
		return Zero, false
	}
	return e, true
}

// Count returns the number of live entities in w.
func (w *World) Count() int {
	return w.count
}

// Each calls fn for every live entity in w in ascending slot-index order until fn returns false.
// Entities spawned during iteration are not visited.
func (w *World) Each(fn func(Entity) bool) {
	if w.count == 0 || w.disposed {
		return
	}
	// Entities spawned during iteration are not visited: the bound is the
	// live set as it stood when Each began.
	limit := len(w.alive)
	for i := 0; i < limit; i++ {
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
			w.gens[i] = w.advanceGen(uint32(i))
		}
		w.free = append(w.free, uint32(i))
	}
	w.floorGens = nil
	w.count = 0
}

// Reset removes all entities and releases component and active entity backing storage while keeping bumped slot generations (so stale handles never alias newly spawned entities) and registered codecs intact.
func (w *World) Reset() {
	if w.disposed {
		return
	}
	if w.hierarchy != nil {
		w.hierarchy.reset()
	}
	for _, s := range w.stores {
		s.reset()
	}
	if len(w.gens) > 0 {
		for i := 0; i < len(w.alive); i++ {
			if w.alive[i] {
				w.gens[i] = w.advanceGen(uint32(i))
			}
		}
		if cap(w.gens) > len(w.gens) {
			trimmed := make([]uint32, len(w.gens))
			copy(trimmed, w.gens)
			w.gens = trimmed
		}
	}
	w.alive = nil
	w.free = nil
	w.floorGens = nil
	w.count = 0
}
