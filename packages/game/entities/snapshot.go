package entities

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/save"
)

var (
	// ErrEmptyCodecName reports registering a component codec with an empty type name.
	ErrEmptyCodecName = errors.New("entities: empty codec name")
	// ErrDuplicateCodec reports registering a codec name or component type that is already registered on the World.
	ErrDuplicateCodec = errors.New("entities: duplicate codec registration")
	// ErrUnknownCodec reports a snapshot containing a component type that has no registered codec on the target World.
	ErrUnknownCodec = errors.New("entities: unknown component codec")
	// ErrInvalidSnapshot reports a malformed WorldSnapshot (such as zero generation, duplicate entity index, or out-of-range index).
	ErrInvalidSnapshot = errors.New("entities: invalid world snapshot")
)

// maxSnapshotIndex caps the slot table size restored from a snapshot to prevent unbounded allocation from corrupted data.
const maxSnapshotIndex = 1 << 20

type componentCodec struct {
	name         string
	key          typeKey
	has          func(w *World, e Entity) bool
	encode       func(w *World, e Entity) (json.RawMessage, bool, error)
	decode       func(w *World, e Entity, raw json.RawMessage) error
	decodeStrict func(w *World, e Entity, raw json.RawMessage, patch bool) error
	remove       func(w *World, e Entity) bool
}

// WorldSnapshot is a deterministic, JSON-serializable snapshot of a World's live entities and registered components.
type WorldSnapshot struct {
	Entities []EntitySnapshot `json:"entities"`
}

// EntitySnapshot captures one live entity's generational handle and its serialized components in sorted type-name order.
type EntitySnapshot struct {
	Index uint32 `json:"index"`
	// Parent is the slot index of this entity's hierarchy parent, when the
	// snapshot recorded one; it is nil for a root entity.
	Parent     *uint32             `json:"parent,omitempty"`
	Generation uint32              `json:"generation"`
	Components []ComponentSnapshot `json:"components,omitempty"`
}

// ComponentSnapshot holds a single JSON-encoded component tagged by its registered codec name.
type ComponentSnapshot struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// RegisterCodec registers a JSON codec for component type T under typeName on w.
//
// Only component types with a registered codec are included in WorldSnapshot output. Registering the same typeName or the same Go type T twice returns ErrDuplicateCodec.
func RegisterCodec[T any](w *World, typeName string) error {
	if w == nil {
		return errors.New("entities: nil world")
	}
	if typeName == "" {
		return ErrEmptyCodecName
	}
	k := typeKeyOf[T]()
	if _, exists := w.codecs[typeName]; exists {
		return fmt.Errorf("entities: codec %q: %w", typeName, ErrDuplicateCodec)
	}
	if prev, exists := w.codecByKey[k]; exists {
		return fmt.Errorf("entities: component already registered as %q: %w", prev, ErrDuplicateCodec)
	}
	if w.codecs == nil {
		w.codecs = make(map[string]componentCodec)
		w.codecByKey = make(map[typeKey]string)
	}
	w.codecs[typeName] = componentCodec{
		name: typeName,
		key:  k,
		has: func(w *World, e Entity) bool {
			return Has[T](w, e)
		},
		encode: func(w *World, e Entity) (json.RawMessage, bool, error) {
			v, ok := Get[T](w, e)
			if !ok {
				return nil, false, nil
			}
			data, err := json.Marshal(*v)
			if err != nil {
				return nil, false, err
			}
			return json.RawMessage(data), true, nil
		},
		decode: func(w *World, e Entity, raw json.RawMessage) error {
			var v T
			if err := json.Unmarshal(raw, &v); err != nil {
				return err
			}
			Set[T](w, e, v)
			return nil
		},
		decodeStrict: func(w *World, e Entity, raw json.RawMessage, patch bool) error {
			trimmed := bytes.TrimSpace(raw)
			if len(trimmed) == 0 {
				return errors.New("empty JSON value")
			}
			var v T
			if bytes.Equal(trimmed, []byte("null")) {
				zeroJSON, err := json.Marshal(v)
				if err != nil || !bytes.Equal(bytes.TrimSpace(zeroJSON), []byte("null")) {
					return errors.New("cannot decode null into non-nullable component")
				}
			} else if patch {
				if cur, ok := Get[T](w, e); ok {
					// Deep-copy via JSON so a partial decode error never mutates *cur's maps or slices.
					curJSON, err := json.Marshal(*cur)
					if err != nil {
						return err
					}
					if err := json.Unmarshal(curJSON, &v); err != nil {
						return err
					}
				}
			}
			if err := decodeStrictJSON(trimmed, &v); err != nil {
				return err
			}
			Set[T](w, e, v)
			return nil
		},
		remove: func(w *World, e Entity) bool {
			if !Has[T](w, e) {
				return false
			}
			Remove[T](w, e)
			return true
		},
	}
	w.codecByKey[k] = typeName
	w.codecOrder = append(w.codecOrder, typeName)
	slices.Sort(w.codecOrder)
	return nil
}

// CodecNames returns the component codec names registered on w in ascending order.
func (w *World) CodecNames() []string {
	if w == nil || len(w.codecOrder) == 0 {
		return nil
	}
	return slices.Clone(w.codecOrder)
}

// CodecName returns the codec name registered for component type T on w and whether one is registered.
func CodecName[T any](w *World) (string, bool) {
	if w == nil || w.codecByKey == nil {
		return "", false
	}
	name, ok := w.codecByKey[typeKeyOf[T]()]
	return name, ok
}

// HasCodec reports whether live entity e currently carries the codec-registered component named typeName.
func (w *World) HasCodec(e Entity, typeName string) bool {
	if w == nil || w.disposed || !w.Alive(e) {
		return false
	}
	codec, ok := w.codecs[typeName]
	if !ok {
		return false
	}
	return codec.has(w, e)
}

// EncodeComponent encodes the codec-registered component named typeName on live entity e as JSON.
// It returns (nil, false, nil) when e is not alive, typeName is not registered, or e does not carry the component.
func (w *World) EncodeComponent(e Entity, typeName string) (json.RawMessage, bool, error) {
	if w == nil {
		return nil, false, errors.New("entities: nil world")
	}
	if w.disposed {
		return nil, false, ErrWorldDisposed
	}
	if !w.Alive(e) {
		return nil, false, nil
	}
	codec, ok := w.codecs[typeName]
	if !ok {
		return nil, false, nil
	}
	raw, present, err := codec.encode(w, e)
	if err != nil {
		return nil, false, fmt.Errorf("entities: encode %s on %s: %w", typeName, e, err)
	}
	return raw, present, nil
}

func decodeStrictJSON(trimmed []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("unexpected trailing data")
	}
	return nil
}

// DecodeComponent strictly decodes raw JSON into the codec-registered component named typeName and attaches or replaces it on live entity e.
// Unknown JSON fields, trailing data and invalid nulls for non-nullable component types are rejected without mutating e.
func (w *World) DecodeComponent(e Entity, typeName string, raw json.RawMessage) error {
	return w.decodeComponentStrict(e, typeName, raw, false)
}

// PatchComponent strictly decodes raw JSON into a copy of live entity e's existing codec-registered component (or its zero value when absent) and writes the result back atomically.
// Unknown JSON fields and trailing data are rejected without mutating e.
func (w *World) PatchComponent(e Entity, typeName string, raw json.RawMessage) error {
	return w.decodeComponentStrict(e, typeName, raw, true)
}

func (w *World) decodeComponentStrict(e Entity, typeName string, raw json.RawMessage, patch bool) error {
	if w == nil {
		return errors.New("entities: nil world")
	}
	if w.disposed {
		return ErrWorldDisposed
	}
	if !w.Alive(e) {
		return ErrDeadEntity
	}
	codec, ok := w.codecs[typeName]
	if !ok {
		return fmt.Errorf("entities: component %q: %w", typeName, ErrUnknownCodec)
	}
	if err := codec.decodeStrict(w, e, raw, patch); err != nil {
		return fmt.Errorf("entities: decode %s on %s: %w", typeName, e, err)
	}
	return nil
}

// RemoveComponent detaches the codec-registered component named typeName from live entity e and reports whether it was present.
func (w *World) RemoveComponent(e Entity, typeName string) (bool, error) {
	if w == nil {
		return false, errors.New("entities: nil world")
	}
	if w.disposed {
		return false, ErrWorldDisposed
	}
	if !w.Alive(e) {
		return false, ErrDeadEntity
	}
	codec, ok := w.codecs[typeName]
	if !ok {
		return false, fmt.Errorf("entities: component %q: %w", typeName, ErrUnknownCodec)
	}
	return codec.remove(w, e), nil
}

// EntityComponents encodes all codec-registered components carried by live entity e in ascending codec-name order.
func (w *World) EntityComponents(e Entity) ([]ComponentSnapshot, error) {
	if w == nil {
		return nil, errors.New("entities: nil world")
	}
	if w.disposed {
		return nil, ErrWorldDisposed
	}
	if !w.Alive(e) {
		return nil, nil
	}
	var out []ComponentSnapshot
	for _, name := range w.codecOrder {
		codec := w.codecs[name]
		raw, ok, err := codec.encode(w, e)
		if err != nil {
			return nil, fmt.Errorf("entities: encode %s on %s: %w", name, e, err)
		}
		if ok {
			out = append(out, ComponentSnapshot{
				Type: name,
				Data: raw,
			})
		}
	}
	return out, nil
}

// Snapshot encodes all live entities and their codec-registered components into a deterministic WorldSnapshot.
// Entities are emitted in ascending slot-index order and each entity's components in ascending codec-name order.
func (w *World) Snapshot() (WorldSnapshot, error) {
	if w == nil {
		return WorldSnapshot{}, errors.New("entities: nil world")
	}
	if w.disposed {
		return WorldSnapshot{}, ErrWorldDisposed
	}
	snap := WorldSnapshot{
		Entities: make([]EntitySnapshot, 0, w.count),
	}
	for i := 0; i < len(w.alive); i++ {
		if !w.alive[i] {
			continue
		}
		e := makeEntity(uint32(i), w.gens[i])
		es := EntitySnapshot{
			Index:      uint32(i),
			Generation: w.gens[i],
		}
		if w.hierarchy != nil && int(i) < len(w.hierarchy.nodes) {
			n := &w.hierarchy.nodes[i]
			if n.entity == e && n.parent != Zero {
				pi := n.parent.Index()
				es.Parent = &pi
			}
		}
		for _, name := range w.codecOrder {
			codec := w.codecs[name]
			raw, ok, err := codec.encode(w, e)
			if err != nil {
				return WorldSnapshot{}, fmt.Errorf("entities: encode %s on %s: %w", name, e, err)
			}
			if ok {
				es.Components = append(es.Components, ComponentSnapshot{
					Type: name,
					Data: raw,
				})
			}
		}
		snap.Entities = append(snap.Entities, es)
	}
	return snap, nil
}

// Restore atomically replaces w's entities and components with snap using w's registered codecs.
//
// Entity indices and generations are preserved verbatim so cross-entity references stored inside components remain valid. If decoding fails (for example due to an unknown codec or invalid JSON), w is left completely unchanged.
//
// An error about the snapshot's contents is a *loaderr.Error that names the field at fault, such as "entities[2].parent", and how to fix it, and matches ErrInvalidSnapshot or ErrUnknownCodec with errors.Is.
func (w *World) Restore(snap WorldSnapshot) error {
	if w == nil {
		return errors.New("entities: nil world")
	}
	if w.disposed {
		return ErrWorldDisposed
	}
	r, err := w.prepareRestore(snap)
	if err != nil {
		return err
	}
	return w.commitRestore(r)
}

// restored is a snapshot decoded into a world of its own, ready for commitRestore to move into the world it was prepared for.
type restored struct {
	tmp   *World
	links [][2]Entity
}

// fixSnapshot is the fix for a snapshot this build cannot restore.
const fixSnapshot = "save the world again with SaveWorld from this build; a snapshot edited by hand must keep the indices, generations and parents SaveWorld wrote"

// prepareRestore decodes snap without touching w.
func (w *World) prepareRestore(snap WorldSnapshot) (*restored, error) {
	tmp := &World{}
	var links [][2]Entity
	if len(snap.Entities) > 0 {
		var maxIdx uint32
		gens := make(map[uint32]uint32, len(snap.Entities))
		// position maps an entity index to where it is in snap.Entities, for errors.
		position := make(map[uint32]int, len(snap.Entities))
		for i, es := range snap.Entities {
			at := loaderr.Path("entities", i)
			if es.Generation == 0 {
				return nil, loaderr.Errorf(loaderr.Join(at, "generation"), fixSnapshot, "index %d has zero generation: %w", es.Index, ErrInvalidSnapshot)
			}
			if es.Index >= maxSnapshotIndex {
				return nil, loaderr.Errorf(loaderr.Join(at, "index"), fixSnapshot, "index %d exceeds limit: %w", es.Index, ErrInvalidSnapshot)
			}
			if _, dup := gens[es.Index]; dup {
				return nil, loaderr.Errorf(loaderr.Join(at, "index"), fixSnapshot, "duplicate entity index %d: %w", es.Index, ErrInvalidSnapshot)
			}
			position[es.Index] = i
			gens[es.Index] = es.Generation
			if es.Index > maxIdx {
				maxIdx = es.Index
			}
		}
		// Validate the recorded parent links before touching w: the parent must
		// exist in the snapshot and the links must form a forest, born in the
		// same order Restore writes them.
		for i, es := range snap.Entities {
			if es.Parent == nil {
				continue
			}
			if *es.Parent == es.Index {
				return nil, loaderr.Errorf(loaderr.Path("entities", i, "parent"), fixSnapshot, "entity %d is its own parent: %w", es.Index, ErrInvalidSnapshot)
			}
			parentGen, ok := gens[*es.Parent]
			if !ok {
				return nil, loaderr.Errorf(loaderr.Path("entities", i, "parent"), fixSnapshot, "entity %d has unknown parent %d: %w", es.Index, *es.Parent, ErrInvalidSnapshot)
			}
			links = append(links, [2]Entity{
				makeEntity(es.Index, es.Generation),
				makeEntity(*es.Parent, parentGen),
			})
		}
		parentOf := make(map[uint32]uint32, len(snap.Entities))
		for _, es := range snap.Entities {
			if es.Parent != nil {
				parentOf[es.Index] = *es.Parent
			}
		}
		// Walked in snapshot order, so the same cycle is reported every time.
		for _, es := range snap.Entities {
			start := es.Index
			if _, ok := parentOf[start]; !ok {
				continue
			}
			steps := 0
			for at := start; ; {
				parent, ok := parentOf[at]
				if !ok {
					break
				}
				steps++
				if steps > len(parentOf) {
					return nil, loaderr.Errorf(loaderr.Path("entities", position[start], "parent"), fixSnapshot, "hierarchy cycle through entity %d: %w", start, ErrInvalidSnapshot)
				}
				at = parent
			}
		}
		size := int(maxIdx) + 1
		tmp.gens = make([]uint32, size)
		tmp.alive = make([]bool, size)
		for i := range tmp.gens {
			if i < len(w.gens) && w.gens[i] > 0 {
				// A dead slot must not reuse a generation this world already
				// issued, or a stale handle would alias after the load.
				tmp.gens[i] = w.gens[i] + 1
			} else {
				tmp.gens[i] = 1
			}
		}
		for _, es := range snap.Entities {
			tmp.gens[es.Index] = es.Generation
			tmp.alive[es.Index] = true
			tmp.count++
		}
		for i := size - 1; i >= 0; i-- {
			if !tmp.alive[i] {
				tmp.free = append(tmp.free, uint32(i))
			}
		}

		for i, es := range snap.Entities {
			e := makeEntity(es.Index, es.Generation)
			for j, cs := range es.Components {
				at := loaderr.Path("entities", i, "components", j)
				codec, ok := w.codecs[cs.Type]
				if !ok {
					return nil, loaderr.Errorf(loaderr.Join(at, "type"), "register the component's codec with RegisterCodec before loading, or remove the component", "component %q on %s: %w", cs.Type, e, ErrUnknownCodec)
				}
				if err := codec.decode(tmp, e, cs.Data); err != nil {
					return nil, loaderr.Errorf(loaderr.Join(at, "data"), "write the data as the component type's JSON, as SaveWorld does", "decode %q on %s: %w", cs.Type, e, err)
				}
			}
		}
	}
	return &restored{tmp: tmp, links: links}, nil
}

// commitRestore moves r, which prepareRestore made for w, into w.
func (w *World) commitRestore(r *restored) error {
	tmp, links := r.tmp, r.links
	// Commit decoded state into w, preserving existing *Store[T] pointers where present.
	w.gens = tmp.gens
	w.alive = tmp.alive
	w.free = tmp.free
	w.count = tmp.count

	for _, existing := range w.stores {
		if next, ok := tmp.byType[existing.key()]; ok {
			existing.swapFrom(next)
		} else {
			existing.clear()
		}
	}
	for _, next := range tmp.stores {
		if _, ok := w.byType[next.key()]; !ok {
			if w.byType == nil {
				w.byType = make(map[typeKey]anyStore)
			}
			next.reparent(w)
			w.byType[next.key()] = next
			w.stores = append(w.stores, next)
		}
	}
	h := w.hierarchy
	if h != nil {
		h.onWorldRestore()
	}
	if len(links) > 0 {
		if h == nil {
			h = NewHierarchy2D(w)
		}
		// keepWorld=false: the local transforms already hold the saved locals.
		for _, link := range links {
			if err := h.SetParent(link[0], link[1], false); err != nil {
				return fmt.Errorf("entities: restore hierarchy: %w", err)
			}
		}
		h.Sync()
	}
	return nil
}

// SaveWorld snapshots w and writes it to slot in store through game/save.
func SaveWorld(store *save.Store, slot string, w *World) error {
	if store == nil {
		return errors.New("entities: nil save store")
	}
	snap, err := w.Snapshot()
	if err != nil {
		return err
	}
	return store.Save(slot, snap)
}

// LoadWorld reads slot from store through game/save and atomically restores it into w.
func LoadWorld(store *save.Store, slot string, w *World) error {
	if store == nil {
		return errors.New("entities: nil save store")
	}
	if w == nil {
		return errors.New("entities: nil world")
	}
	if w.disposed {
		return ErrWorldDisposed
	}
	var snap WorldSnapshot
	var r *restored
	// The snapshot is checked while the save is decoded, so its errors give the line in the slot, and a primary copy that cannot be restored falls back to the backup.
	err := store.LoadChecked(slot, &snap, func() error {
		var err error
		r, err = w.prepareRestore(snap)
		return err
	})
	if err != nil {
		return err
	}
	return w.commitRestore(r)
}
