package ai

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/nsgo/core"
)

// BlackboardKind identifies the stored value type of a [Blackboard] entry.
type BlackboardKind uint8

const (
	// BlackboardNone represents an unset or absent blackboard key.
	BlackboardNone BlackboardKind = iota
	// BlackboardBool stores a bool.
	BlackboardBool
	// BlackboardInt stores an int.
	BlackboardInt
	// BlackboardFloat stores a float64.
	BlackboardFloat
	// BlackboardString stores a string.
	BlackboardString
	// BlackboardVec2 stores a 2D float32 vector (math2d.Vec2 / core.Point).
	BlackboardVec2
	// BlackboardVec3 stores a 3D float32 vector (ai.Vec3).
	BlackboardVec3
	// BlackboardEntity stores a uint64 entity identifier.
	BlackboardEntity
)

// String returns the canonical YAML name for k.
func (k BlackboardKind) String() string {
	switch k {
	case BlackboardBool:
		return "bool"
	case BlackboardInt:
		return "int"
	case BlackboardFloat:
		return "float"
	case BlackboardString:
		return "string"
	case BlackboardVec2:
		return "vec2"
	case BlackboardVec3:
		return "vec3"
	case BlackboardEntity:
		return "entity"
	default:
		return "none"
	}
}

// BlackboardKindName is the string representation of a [BlackboardKind] in YAML documents.
type BlackboardKindName string

const (
	// BBTypeBool is the YAML type name for bool blackboard keys.
	BBTypeBool BlackboardKindName = "bool"
	// BBTypeInt is the YAML type name for int blackboard keys.
	BBTypeInt BlackboardKindName = "int"
	// BBTypeFloat is the YAML type name for float64 blackboard keys.
	BBTypeFloat BlackboardKindName = "float"
	// BBTypeString is the YAML type name for string blackboard keys.
	BBTypeString BlackboardKindName = "string"
	// BBTypeVec2 is the YAML type name for 2D vector blackboard keys.
	BBTypeVec2 BlackboardKindName = "vec2"
	// BBTypeVec3 is the YAML type name for 3D vector blackboard keys.
	BBTypeVec3 BlackboardKindName = "vec3"
	// BBTypeEntity is the YAML type name for uint64 entity ID blackboard keys.
	BBTypeEntity BlackboardKindName = "entity"
)

// Kind converts a YAML [BlackboardKindName] into a [BlackboardKind].
func (n BlackboardKindName) Kind() (BlackboardKind, bool) {
	switch n {
	case BBTypeBool:
		return BlackboardBool, true
	case BBTypeInt:
		return BlackboardInt, true
	case BBTypeFloat:
		return BlackboardFloat, true
	case BBTypeString:
		return BlackboardString, true
	case BBTypeVec2:
		return BlackboardVec2, true
	case BBTypeVec3:
		return BlackboardVec3, true
	case BBTypeEntity:
		return BlackboardEntity, true
	default:
		return BlackboardNone, false
	}
}

// BlackboardValue is a typed tagged union stored in a [Blackboard] without interface boxing.
type BlackboardValue struct {
	Kind   BlackboardKind
	Bool   bool
	Int    int
	Float  float64
	String string
	Vec2   math2d.Vec2
	Vec3   Vec3
	Entity uint64
}

// BBBool returns a [BlackboardValue] holding v.
func BBBool(v bool) BlackboardValue { return BlackboardValue{Kind: BlackboardBool, Bool: v} }

// BBInt returns a [BlackboardValue] holding v.
func BBInt(v int) BlackboardValue { return BlackboardValue{Kind: BlackboardInt, Int: v} }

// BBFloat returns a [BlackboardValue] holding v.
func BBFloat(v float64) BlackboardValue { return BlackboardValue{Kind: BlackboardFloat, Float: v} }

// BBString returns a [BlackboardValue] holding v.
func BBString(v string) BlackboardValue { return BlackboardValue{Kind: BlackboardString, String: v} }

// BBVec2 returns a [BlackboardValue] holding v.
func BBVec2(v math2d.Vec2) BlackboardValue { return BlackboardValue{Kind: BlackboardVec2, Vec2: v} }

// BBPoint returns a [BlackboardValue] holding p as a 2D vector.
func BBPoint(p core.Point) BlackboardValue {
	return BlackboardValue{Kind: BlackboardVec2, Vec2: math2d.FromPoint(p)}
}

// BBVec3 returns a [BlackboardValue] holding v.
func BBVec3(v Vec3) BlackboardValue { return BlackboardValue{Kind: BlackboardVec3, Vec3: v} }

// BBEntity returns a [BlackboardValue] holding entity identifier id.
func BBEntity(id uint64) BlackboardValue { return BlackboardValue{Kind: BlackboardEntity, Entity: id} }

// IsZero reports whether v represents an unset value (BlackboardNone).
func (v BlackboardValue) IsZero() bool { return v.Kind == BlackboardNone }

// Equal reports whether v and o have the same kind and identical stored value.
func (v BlackboardValue) Equal(o BlackboardValue) bool {
	if v.Kind != o.Kind {
		return false
	}
	switch v.Kind {
	case BlackboardNone:
		return true
	case BlackboardBool:
		return v.Bool == o.Bool
	case BlackboardInt:
		return v.Int == o.Int
	case BlackboardFloat:
		if math.IsNaN(v.Float) && math.IsNaN(o.Float) {
			return true
		}
		return v.Float == o.Float
	case BlackboardString:
		return v.String == o.String
	case BlackboardVec2:
		return float32Equal(v.Vec2.X, o.Vec2.X) && float32Equal(v.Vec2.Y, o.Vec2.Y)
	case BlackboardVec3:
		return float32Equal(v.Vec3.X, o.Vec3.X) && float32Equal(v.Vec3.Y, o.Vec3.Y) && float32Equal(v.Vec3.Z, o.Vec3.Z)
	case BlackboardEntity:
		return v.Entity == o.Entity
	default:
		return false
	}
}

func float32Equal(a, b float32) bool {
	if math.IsNaN(float64(a)) && math.IsNaN(float64(b)) {
		return true
	}
	return a == b
}

// BlackboardChange describes a single key mutation observed by a [BlackboardListener].
type BlackboardChange struct {
	// Key is the modified blackboard key.
	Key string
	// Old is the effective value before the mutation (BlackboardNone if previously unset).
	Old BlackboardValue
	// New is the effective value after the mutation (BlackboardNone if removed).
	New BlackboardValue
	// Board is the Blackboard where the mutation occurred (which may be a shared parent).
	Board *Blackboard
}

// BlackboardListener is invoked synchronously when a watched blackboard key changes.
type BlackboardListener func(change BlackboardChange)

type bbWatcher struct {
	id  uint64
	key string // empty watches all keys
	fn  BlackboardListener
}

// Blackboard is a typed per-agent key-value store with optional parent scoping
// (for a squad or team), monotonic revision tracking, and synchronous change
// notifications.
//
// Reads check local entries first and fall back to Parent when unset locally.
// Writes via Set* target the local blackboard; SetShared* writes to Parent when
// present (or locally if Parent is nil). Blackboard is designed for deterministic
// single-threaded stepping on game.Loop and is not safe for concurrent use.
type Blackboard struct {
	parent      *Blackboard
	entries     map[string]BlackboardValue
	rev         uint64
	keyRev      map[string]uint64
	nextWatchID uint64
	watchers    []bbWatcher
}

// NewBlackboard creates an empty standalone [Blackboard].
func NewBlackboard() *Blackboard {
	return &Blackboard{}
}

// NewChildBlackboard creates a [Blackboard] scoped to an agent that falls back to
// parent for reads not shadowed locally.
func NewChildBlackboard(parent *Blackboard) *Blackboard {
	return &Blackboard{parent: parent}
}

// Parent returns b's shared parent blackboard, or nil when b has no parent.
func (b *Blackboard) Parent() *Blackboard {
	if b == nil {
		return nil
	}
	return b.parent
}

// Revision returns a monotonically increasing mutation counter covering b and any
// ancestor blackboards.
func (b *Blackboard) Revision() uint64 {
	var total uint64
	for cur := b; cur != nil; cur = cur.parent {
		total += cur.rev
	}
	return total
}

// KeyRevision returns a monotonically increasing mutation counter for key across
// b and any ancestor blackboards. It allocates zero bytes.
func (b *Blackboard) KeyRevision(key string) uint64 {
	var total uint64
	for cur := b; cur != nil; cur = cur.parent {
		if cur.keyRev != nil {
			total += cur.keyRev[key]
		}
	}
	return total
}

// Watch registers fn to be called synchronously whenever key changes on b or on
// an ancestor blackboard (unless shadowed locally). Pass an empty key "" to watch
// all keys. The returned cancel function unregisters the watcher cleanly.
func (b *Blackboard) Watch(key string, fn BlackboardListener) (cancel func()) {
	if b == nil || fn == nil {
		return func() {}
	}
	b.nextWatchID++
	id := b.nextWatchID
	b.watchers = append(b.watchers, bbWatcher{id: id, key: key, fn: fn})

	var cancelParent func()
	if b.parent != nil {
		cancelParent = b.parent.Watch(key, func(ch BlackboardChange) {
			// Ignore parent changes shadowed by a local entry on b.
			if _, shadowed := b.entries[ch.Key]; shadowed {
				return
			}
			fn(ch)
		})
	}

	canceled := false
	return func() {
		if canceled {
			return
		}
		canceled = true
		if cancelParent != nil {
			cancelParent()
		}
		for i, w := range b.watchers {
			if w.id == id {
				b.watchers = slices.Delete(b.watchers, i, i+1)
				break
			}
		}
	}
}

func (b *Blackboard) bumpAndNotify(key string, oldVal, newVal BlackboardValue) {
	b.rev++
	if b.keyRev == nil {
		b.keyRev = make(map[string]uint64)
	}
	b.keyRev[key]++
	if len(b.watchers) == 0 {
		return
	}
	ch := BlackboardChange{
		Key:   key,
		Old:   oldVal,
		New:   newVal,
		Board: b,
	}
	// Snapshot length/slice copy if watchers mutate during callback.
	snap := slices.Clone(b.watchers)
	for _, w := range snap {
		if w.key == "" || w.key == key {
			w.fn(ch)
		}
	}
}

// Get returns the effective value for key, falling back to Parent when unset locally.
func (b *Blackboard) Get(key string) (BlackboardValue, bool) {
	for cur := b; cur != nil; cur = cur.parent {
		if cur.entries != nil {
			if v, ok := cur.entries[key]; ok {
				return v, true
			}
		}
	}
	return BlackboardValue{}, false
}

// GetLocal returns the value stored directly on b without consulting Parent.
func (b *Blackboard) GetLocal(key string) (BlackboardValue, bool) {
	if b == nil || b.entries == nil {
		return BlackboardValue{}, false
	}
	v, ok := b.entries[key]
	return v, ok
}

// Has reports whether key is set on b or any ancestor blackboard.
func (b *Blackboard) Has(key string) bool {
	_, ok := b.Get(key)
	return ok
}

// HasLocal reports whether key is set directly on b.
func (b *Blackboard) HasLocal(key string) bool {
	_, ok := b.GetLocal(key)
	return ok
}

// Kind returns the [BlackboardKind] of key, or BlackboardNone when unset.
func (b *Blackboard) Kind(key string) BlackboardKind {
	v, ok := b.Get(key)
	if !ok {
		return BlackboardNone
	}
	return v.Kind
}

// Set stores val under key on b's local scope. Setting a zero (BlackboardNone)
// value deletes the local key.
func (b *Blackboard) Set(key string, val BlackboardValue) {
	if b == nil {
		return
	}
	if val.Kind == BlackboardNone {
		b.Delete(key)
		return
	}
	if cur, ok := b.GetLocal(key); ok && cur.Equal(val) {
		return
	}
	oldEff, _ := b.Get(key)
	if b.entries == nil {
		b.entries = make(map[string]BlackboardValue)
	}
	b.entries[key] = val
	if !oldEff.Equal(val) {
		b.bumpAndNotify(key, oldEff, val)
	}
}

// Delete removes key from b's local scope, exposing any value stored in Parent.
func (b *Blackboard) Delete(key string) {
	if b == nil || b.entries == nil {
		return
	}
	oldLocal, ok := b.entries[key]
	if !ok {
		return
	}
	delete(b.entries, key)
	newEff, _ := b.Get(key)
	if !oldLocal.Equal(newEff) {
		b.bumpAndNotify(key, oldLocal, newEff)
	}
}

// Clear removes all local entries from b in deterministic sorted key order,
// notifying watchers for each key whose effective value changes.
func (b *Blackboard) Clear() {
	if b == nil || len(b.entries) == 0 {
		return
	}
	keys := b.LocalKeys(nil)
	for _, k := range keys {
		b.Delete(k)
	}
}

// SetShared stores val under key on b.Parent() when non-nil, or locally on b
// when b has no parent.
func (b *Blackboard) SetShared(key string, val BlackboardValue) {
	if b == nil {
		return
	}
	if b.parent != nil {
		b.parent.Set(key, val)
		return
	}
	b.Set(key, val)
}

// DeleteShared deletes key from b.Parent() when non-nil, or locally from b when
// b has no parent.
func (b *Blackboard) DeleteShared(key string) {
	if b == nil {
		return
	}
	if b.parent != nil {
		b.parent.Delete(key)
		return
	}
	b.Delete(key)
}

// Typed local setters.

// SetBool stores a bool under key on b.
func (b *Blackboard) SetBool(key string, v bool) { b.Set(key, BBBool(v)) }

// SetInt stores an int under key on b.
func (b *Blackboard) SetInt(key string, v int) { b.Set(key, BBInt(v)) }

// SetFloat stores a float64 under key on b.
func (b *Blackboard) SetFloat(key string, v float64) { b.Set(key, BBFloat(v)) }

// SetString stores a string under key on b.
func (b *Blackboard) SetString(key string, v string) { b.Set(key, BBString(v)) }

// SetVec2 stores a 2D vector under key on b.
func (b *Blackboard) SetVec2(key string, v math2d.Vec2) { b.Set(key, BBVec2(v)) }

// SetPoint stores a core.Point as a 2D vector under key on b.
func (b *Blackboard) SetPoint(key string, p core.Point) { b.Set(key, BBPoint(p)) }

// SetVec3 stores a 3D vector under key on b.
func (b *Blackboard) SetVec3(key string, v Vec3) { b.Set(key, BBVec3(v)) }

// SetEntity stores an entity ID under key on b.
func (b *Blackboard) SetEntity(key string, id uint64) { b.Set(key, BBEntity(id)) }

// Typed shared setters (targeting Parent when present, else b).

// SetSharedBool stores a bool on the shared parent blackboard (or b if standalone).
func (b *Blackboard) SetSharedBool(key string, v bool) { b.SetShared(key, BBBool(v)) }

// SetSharedInt stores an int on the shared parent blackboard (or b if standalone).
func (b *Blackboard) SetSharedInt(key string, v int) { b.SetShared(key, BBInt(v)) }

// SetSharedFloat stores a float64 on the shared parent blackboard (or b if standalone).
func (b *Blackboard) SetSharedFloat(key string, v float64) { b.SetShared(key, BBFloat(v)) }

// SetSharedString stores a string on the shared parent blackboard (or b if standalone).
func (b *Blackboard) SetSharedString(key string, v string) { b.SetShared(key, BBString(v)) }

// SetSharedVec2 stores a 2D vector on the shared parent blackboard (or b if standalone).
func (b *Blackboard) SetSharedVec2(key string, v math2d.Vec2) { b.SetShared(key, BBVec2(v)) }

// SetSharedPoint stores a core.Point as a 2D vector on the shared parent blackboard (or b if standalone).
func (b *Blackboard) SetSharedPoint(key string, p core.Point) { b.SetShared(key, BBPoint(p)) }

// SetSharedVec3 stores a 3D vector on the shared parent blackboard (or b if standalone).
func (b *Blackboard) SetSharedVec3(key string, v Vec3) { b.SetShared(key, BBVec3(v)) }

// SetSharedEntity stores an entity ID on the shared parent blackboard (or b if standalone).
func (b *Blackboard) SetSharedEntity(key string, id uint64) { b.SetShared(key, BBEntity(id)) }

// Typed getters (falling back to Parent).

// Bool returns the bool stored under key, or (false, false) if unset or of another type.
func (b *Blackboard) Bool(key string) (bool, bool) {
	v, ok := b.Get(key)
	if !ok || v.Kind != BlackboardBool {
		return false, false
	}
	return v.Bool, true
}

// Int returns the int stored under key, or (0, false) if unset or of another type.
func (b *Blackboard) Int(key string) (int, bool) {
	v, ok := b.Get(key)
	if !ok || v.Kind != BlackboardInt {
		return 0, false
	}
	return v.Int, true
}

// Float returns the float64 stored under key, or (0, false) if unset or of another type.
func (b *Blackboard) Float(key string) (float64, bool) {
	v, ok := b.Get(key)
	if !ok || v.Kind != BlackboardFloat {
		return 0, false
	}
	return v.Float, true
}

// String returns the string stored under key, or ("", false) if unset or of another type.
func (b *Blackboard) String(key string) (string, bool) {
	v, ok := b.Get(key)
	if !ok || v.Kind != BlackboardString {
		return "", false
	}
	return v.String, true
}

// Vec2 returns the 2D vector stored under key, or (zero, false) if unset or of another type.
func (b *Blackboard) Vec2(key string) (math2d.Vec2, bool) {
	v, ok := b.Get(key)
	if !ok || v.Kind != BlackboardVec2 {
		return math2d.Vec2{}, false
	}
	return v.Vec2, true
}

// Point returns the 2D vector stored under key as a core.Point, or (zero, false) if unset or of another type.
func (b *Blackboard) Point(key string) (core.Point, bool) {
	v, ok := b.Get(key)
	if !ok || v.Kind != BlackboardVec2 {
		return core.Point{}, false
	}
	return v.Vec2.Point(), true
}

// Vec3 returns the 3D vector stored under key, or (zero, false) if unset or of another type.
func (b *Blackboard) Vec3(key string) (Vec3, bool) {
	v, ok := b.Get(key)
	if !ok || v.Kind != BlackboardVec3 {
		return Vec3{}, false
	}
	return v.Vec3, true
}

// Entity returns the uint64 entity ID stored under key, or (0, false) if unset or of another type.
func (b *Blackboard) Entity(key string) (uint64, bool) {
	v, ok := b.Get(key)
	if !ok || v.Kind != BlackboardEntity {
		return 0, false
	}
	return v.Entity, true
}

// LocalKeys appends all keys stored directly on b to dst[:0] in deterministic ascending order.
func (b *Blackboard) LocalKeys(dst []string) []string {
	dst = dst[:0]
	if b == nil || len(b.entries) == 0 {
		return dst
	}
	dst = slices.Grow(dst, len(b.entries))
	for k := range b.entries {
		dst = append(dst, k)
	}
	slices.Sort(dst)
	return dst
}

// Keys appends all visible keys on b and its ancestors to dst[:0] in deterministic ascending order without duplicates.
func (b *Blackboard) Keys(dst []string) []string {
	dst = dst[:0]
	if b == nil {
		return dst
	}
	if b.parent == nil {
		return b.LocalKeys(dst)
	}
	seen := make(map[string]struct{})
	for cur := b; cur != nil; cur = cur.parent {
		for k := range cur.entries {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				dst = append(dst, k)
			}
		}
	}
	slices.Sort(dst)
	return dst
}
