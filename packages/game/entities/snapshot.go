package entities

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

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
const maxSnapshotIndex = 1 << 24

type componentCodec struct {
	name   string
	key    typeKey
	encode func(w *World, e Entity) (json.RawMessage, bool, error)
	decode func(w *World, e Entity, raw json.RawMessage) error
}

// WorldSnapshot is a deterministic, JSON-serializable snapshot of a World's live entities and registered components.
type WorldSnapshot struct {
	Entities []EntitySnapshot `json:"entities"`
}

// EntitySnapshot captures one live entity's generational handle and its serialized components in sorted type-name order.
type EntitySnapshot struct {
	Index      uint32              `json:"index"`
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
	}
	w.codecByKey[k] = typeName
	w.codecOrder = append(w.codecOrder, typeName)
	slices.Sort(w.codecOrder)
	return nil
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
func (w *World) Restore(snap WorldSnapshot) error {
	if w == nil {
		return errors.New("entities: nil world")
	}
	if w.disposed {
		return ErrWorldDisposed
	}

	tmp := &World{}
	if len(snap.Entities) > 0 {
		var maxIdx uint32
		seen := make(map[uint32]struct{}, len(snap.Entities))
		for _, es := range snap.Entities {
			if es.Generation == 0 {
				return fmt.Errorf("entities: index %d has zero generation: %w", es.Index, ErrInvalidSnapshot)
			}
			if es.Index > maxSnapshotIndex {
				return fmt.Errorf("entities: index %d exceeds limit: %w", es.Index, ErrInvalidSnapshot)
			}
			if _, dup := seen[es.Index]; dup {
				return fmt.Errorf("entities: duplicate entity index %d: %w", es.Index, ErrInvalidSnapshot)
			}
			seen[es.Index] = struct{}{}
			if es.Index > maxIdx {
				maxIdx = es.Index
			}
		}

		size := int(maxIdx) + 1
		tmp.gens = make([]uint32, size)
		tmp.alive = make([]bool, size)
		for i := range tmp.gens {
			tmp.gens[i] = 1
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

		for _, es := range snap.Entities {
			e := makeEntity(es.Index, es.Generation)
			for _, cs := range es.Components {
				codec, ok := w.codecs[cs.Type]
				if !ok {
					return fmt.Errorf("entities: component %q on %s: %w", cs.Type, e, ErrUnknownCodec)
				}
				if err := codec.decode(tmp, e, cs.Data); err != nil {
					return fmt.Errorf("entities: decode %q on %s: %w", cs.Type, e, err)
				}
			}
		}
	}

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
	if err := store.Load(slot, &snap); err != nil {
		return err
	}
	return w.Restore(snap)
}
