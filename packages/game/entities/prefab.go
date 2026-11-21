package entities

// Prefab is a named entity template that attaches a preset list of components to each entity it spawns.
//
// Value components added with With are copied per entity, while reference-containing components (slices, maps, pointers) added with WithFunc invoke their constructor per spawn so entities never share mutable state.
type Prefab struct {
	name     string
	builders []prefabBuilder
}

type prefabBuilder struct {
	key   typeKey
	apply func(w *World, e Entity)
}

// NewPrefab returns an empty Prefab with the given name.
func NewPrefab(name string) *Prefab {
	return &Prefab{name: name}
}

// Name returns the prefab's name.
func (p *Prefab) Name() string {
	if p == nil {
		return ""
	}
	return p.name
}

// Clone returns a new Prefab named name with a shallow copy of p's component builders, allowing derived prefabs to override or add components without mutating p.
func (p *Prefab) Clone(name string) *Prefab {
	c := &Prefab{name: name}
	if p != nil && len(p.builders) > 0 {
		c.builders = make([]prefabBuilder, len(p.builders))
		copy(c.builders, p.builders)
	}
	return c
}

func (p *Prefab) setBuilder(key typeKey, apply func(w *World, e Entity)) {
	for i := range p.builders {
		if p.builders[i].key == key {
			p.builders[i].apply = apply
			return
		}
	}
	p.builders = append(p.builders, prefabBuilder{key: key, apply: apply})
}

// With attaches a value component template v of type T to p and returns p.
// If T is already present on p, its builder is replaced.
// For components containing slices, maps or pointers, prefer WithFunc so each spawned entity receives an independent copy.
func With[T any](p *Prefab, v T) *Prefab {
	p.setBuilder(typeKeyOf[T](), func(w *World, e Entity) {
		Set[T](w, e, v)
	})
	return p
}

// WithFunc attaches a constructor-backed component template of type T to p and returns p.
// make is invoked once per spawned entity so slices, maps and pointers are never shared across entities. If make is nil, the zero value of T is used.
func WithFunc[T any](p *Prefab, make func() T) *Prefab {
	p.setBuilder(typeKeyOf[T](), func(w *World, e Entity) {
		var v T
		if make != nil {
			v = make()
		}
		Set[T](w, e, v)
	})
	return p
}

// Spawn creates a new entity in w and applies all of p's component builders to it.
func (p *Prefab) Spawn(w *World) Entity {
	if w == nil {
		return Zero
	}
	e := w.Spawn()
	if e == Zero || p == nil {
		return e
	}
	for _, b := range p.builders {
		b.apply(w, e)
	}
	return e
}

// SpawnN spawns n independent entities from p in w and returns their handles.
// It returns nil when n <= 0 or w is nil.
func (p *Prefab) SpawnN(w *World, n int) []Entity {
	if w == nil || n <= 0 {
		return nil
	}
	out := make([]Entity, 0, n)
	for i := 0; i < n; i++ {
		e := p.Spawn(w)
		if e == Zero {
			break
		}
		out = append(out, e)
	}
	return out
}
