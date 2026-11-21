package entities

// typeKey uniquely identifies a component type T without using reflect.
// Boxing a zero-sized typeTag[T]{} into any allocates zero bytes because the Go runtime uses a static type descriptor and zerobase.
type typeKey any

type typeTag[T any] struct{}

func typeKeyOf[T any]() typeKey {
	return typeTag[T]{}
}

type anyStore interface {
	key() typeKey
	removeIndex(idx uint32)
	clear()
	reset()
	len() int
	swapFrom(other anyStore)
	reparent(w *World)
}

// Store holds all instances of component type T in a World using a typed sparse set.
//
// Dense arrays (dense and values) keep components tightly packed for cache-friendly, zero-allocation iteration, while sparse maps entity slot indices to dense indices in O(1) time.
type Store[T any] struct {
	world  *World
	dense  []Entity
	values []T
	// sparse stores (denseIndex + 1) indexed by Entity.Index(); 0 marks absence.
	sparse []int32
}

// Component returns the typed component Store for T in w, creating it if it does not yet exist.
func Component[T any](w *World) *Store[T] {
	return ensureStore[T](w)
}

func existingStore[T any](w *World) *Store[T] {
	if w == nil || w.disposed || w.byType == nil {
		return nil
	}
	s, ok := w.byType[typeKeyOf[T]()]
	if !ok {
		return nil
	}
	return s.(*Store[T])
}

func ensureStore[T any](w *World) *Store[T] {
	k := typeKeyOf[T]()
	if w.byType != nil {
		if s, ok := w.byType[k]; ok {
			return s.(*Store[T])
		}
	} else {
		w.byType = make(map[typeKey]anyStore)
	}
	s := &Store[T]{world: w}
	w.byType[k] = s
	w.stores = append(w.stores, s)
	return s
}

func (s *Store[T]) key() typeKey { return typeKeyOf[T]() }

func (s *Store[T]) len() int { return len(s.dense) }

func (s *Store[T]) reparent(w *World) { s.world = w }

func (s *Store[T]) swapFrom(other anyStore) {
	o := other.(*Store[T])
	s.dense = o.dense
	s.values = o.values
	s.sparse = o.sparse
}

// Len returns the number of entities that currently carry component T.
func (s *Store[T]) Len() int { return len(s.dense) }

// Set attaches or replaces component v on e. It is a no-op if e is not alive in the store's World.
func (s *Store[T]) Set(e Entity, v T) {
	if !s.world.Alive(e) {
		return
	}
	idx := int(e.Index())
	if idx >= len(s.sparse) {
		need := idx + 1
		if need <= cap(s.sparse) {
			old := len(s.sparse)
			s.sparse = s.sparse[:need]
			clear(s.sparse[old:need])
		} else {
			nextCap := max(need, len(s.sparse)*2)
			grown := make([]int32, need, nextCap)
			copy(grown, s.sparse)
			s.sparse = grown
		}
	}
	if entry := s.sparse[idx]; entry != 0 {
		d := int(entry - 1)
		s.dense[d] = e
		s.values[d] = v
		return
	}
	d := len(s.dense)
	s.dense = append(s.dense, e)
	s.values = append(s.values, v)
	s.sparse[idx] = int32(d + 1)
}

// Get returns a pointer to e's component value and true, or (nil, false) if e is not alive or lacks T.
// The returned pointer remains valid until components are added or removed from this store.
func (s *Store[T]) Get(e Entity) (*T, bool) {
	d, ok := s.denseIndex(e)
	if !ok {
		return nil, false
	}
	return &s.values[d], true
}

// Has reports whether live entity e currently carries component T.
func (s *Store[T]) Has(e Entity) bool {
	_, ok := s.denseIndex(e)
	return ok
}

// Remove detaches component T from e if present. It is a no-op if e is stale or lacks T.
func (s *Store[T]) Remove(e Entity) {
	if !s.Has(e) {
		return
	}
	s.removeIndex(e.Index())
}

// Each calls fn for every entity carrying T in deterministic dense order until fn returns false.
// Each visits every entity carrying T in dense (insertion) order until fn returns false.
//
// A callback may destroy the entity it was handed, but not add or remove other
// entities/components of this store: a removal elsewhere swaps the last entry
// into the removed slot, which can skip or revisit an entity. Use EachSafe when
// the callback needs to touch other entities of the same store.
func (s *Store[T]) Each(fn func(Entity, *T) bool) {
	for i := 0; i < len(s.dense); {
		e := s.dense[i]
		if !fn(e, &s.values[i]) {
			return
		}
		if i < len(s.dense) && s.dense[i] == e {
			i++
		}
	}
}

func (s *Store[T]) denseIndex(e Entity) (int, bool) {
	if e == Zero || s == nil {
		return 0, false
	}
	idx := int(e.Index())
	if idx >= len(s.sparse) {
		return 0, false
	}
	entry := s.sparse[idx]
	if entry == 0 {
		return 0, false
	}
	d := int(entry - 1)
	if s.dense[d] != e {
		return 0, false
	}
	return d, true
}

func (s *Store[T]) removeIndex(idx uint32) {
	i := int(idx)
	if i >= len(s.sparse) {
		return
	}
	entry := s.sparse[i]
	if entry == 0 {
		return
	}
	d := int(entry - 1)
	last := len(s.dense) - 1
	if d != last {
		moved := s.dense[last]
		s.dense[d] = moved
		s.values[d] = s.values[last]
		s.sparse[moved.Index()] = entry
	}
	var zero T
	s.values[last] = zero
	s.dense = s.dense[:last]
	s.values = s.values[:last]
	s.sparse[i] = 0
}

func (s *Store[T]) clear() {
	for _, e := range s.dense {
		s.sparse[e.Index()] = 0
	}
	clear(s.values)
	s.dense = s.dense[:0]
	s.values = s.values[:0]
}

func (s *Store[T]) reset() {
	s.dense = nil
	s.values = nil
	s.sparse = nil
}

// Set attaches or replaces component v on entity e in w. It is a no-op if e is not alive in w.
func Set[T any](w *World, e Entity, v T) {
	if w == nil || !w.Alive(e) {
		return
	}
	ensureStore[T](w).Set(e, v)
}

// Get returns a pointer to entity e's component of type T in w, or (nil, false) if absent or if e is not alive.
func Get[T any](w *World, e Entity) (*T, bool) {
	s := existingStore[T](w)
	if s == nil {
		return nil, false
	}
	return s.Get(e)
}

// Has reports whether live entity e carries component T in w.
func Has[T any](w *World, e Entity) bool {
	s := existingStore[T](w)
	if s == nil {
		return false
	}
	return s.Has(e)
}

// Remove detaches component T from entity e in w if present.
func Remove[T any](w *World, e Entity) {
	s := existingStore[T](w)
	if s == nil {
		return
	}
	s.Remove(e)
}
