package save

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
)

// ObjectState holds persisted changes for one object inside a scene.
// Nil pointer fields mean the object kept the transform from its scene file.
type ObjectState struct {
	// Destroyed records that the object was removed (picked up, defeated, broken)
	// and should be omitted when the scene is built or spawned again.
	Destroyed bool `json:"destroyed,omitempty"`
	// X, Y and PosZ override the object's local position when set.
	X    *float32 `json:"x,omitempty"`
	Y    *float32 `json:"y,omitempty"`
	PosZ *float32 `json:"posZ,omitempty"`
	// Rotation overrides the object's 2D clockwise rotation in radians when set.
	Rotation *float32 `json:"rotation,omitempty"`
	// Components holds persisted component field overrides keyed by component name, then field name.
	Components map[string]map[string]any `json:"components,omitempty"`
	// Switches holds per-object boolean flags (RPG Maker-style self switches, such as "A" or "opened").
	Switches map[string]bool `json:"switches,omitempty"`
	// Vars holds arbitrary JSON-encodable per-object variables.
	Vars map[string]any `json:"vars,omitempty"`
}

func (o ObjectState) empty() bool {
	return !o.Destroyed &&
		o.X == nil && o.Y == nil && o.PosZ == nil && o.Rotation == nil &&
		len(o.Components) == 0 && len(o.Switches) == 0 && len(o.Vars) == 0
}

// PersistenceSnapshot is the deterministic JSON form of a Persistence store.
type PersistenceSnapshot struct {
	// Scenes maps cleaned scene paths to object states keyed by object ID.
	Scenes map[string]map[string]ObjectState `json:"scenes,omitempty"`
}

// Persistence tracks per-scene object state across scene reloads, room transitions and save slots,
// keyed by scene path (such as "scenes/cave.scene.yaml") and object ID.
//
// It implements Part, StagedPart and Resetter so a game can register it directly on a save.Game.
// The zero value of Persistence is ready to use.
type Persistence struct {
	scenes map[string]map[string]ObjectState
}

// NewPersistence returns an empty Persistence store.
func NewPersistence() *Persistence {
	return &Persistence{}
}

func cleanScenePath(scene string) string {
	if scene == "" {
		return ""
	}
	cleaned := path.Clean(strings.ReplaceAll(scene, "\\", "/"))
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func normFloat32(v float32) float32 {
	if v == 0 {
		return 0 // avoid -0.0 diff noise
	}
	return v
}

func float32Ptr(v float32) *float32 {
	n := normFloat32(v)
	return &n
}

func (p *Persistence) ensureObject(scene, id string) (string, *ObjectState, bool) {
	if p == nil || id == "" {
		return "", nil, false
	}
	sc := cleanScenePath(scene)
	if sc == "" {
		return "", nil, false
	}
	if p.scenes == nil {
		p.scenes = make(map[string]map[string]ObjectState)
	}
	objs := p.scenes[sc]
	if objs == nil {
		objs = make(map[string]ObjectState)
		p.scenes[sc] = objs
	}
	st := objs[id]
	return sc, &st, true
}

func (p *Persistence) putObject(sc, id string, st ObjectState) {
	if st.empty() {
		p.deleteClean(sc, id)
		return
	}
	if p.scenes == nil {
		p.scenes = make(map[string]map[string]ObjectState)
	}
	if p.scenes[sc] == nil {
		p.scenes[sc] = make(map[string]ObjectState)
	}
	p.scenes[sc][id] = st
}

func (p *Persistence) deleteClean(sc, id string) {
	if p == nil || p.scenes == nil {
		return
	}
	objs, ok := p.scenes[sc]
	if !ok {
		return
	}
	delete(objs, id)
	if len(objs) == 0 {
		delete(p.scenes, sc)
	}
}

// MarkDestroyed records that object id in scene was destroyed (picked up, defeated, broken).
func (p *Persistence) MarkDestroyed(scene, id string) {
	sc, st, ok := p.ensureObject(scene, id)
	if !ok {
		return
	}
	st.Destroyed = true
	p.putObject(sc, id, *st)
}

// IsDestroyed reports whether object id in scene has been marked destroyed.
func (p *Persistence) IsDestroyed(scene, id string) bool {
	st, ok := p.Object(scene, id)
	return ok && st.Destroyed
}

// SetPosition records a persisted 2D position override for object id in scene.
func (p *Persistence) SetPosition(scene, id string, x, y float32) {
	sc, st, ok := p.ensureObject(scene, id)
	if !ok {
		return
	}
	st.X = float32Ptr(x)
	st.Y = float32Ptr(y)
	p.putObject(sc, id, *st)
}

// SetPosition3D records a persisted 3D position override for object id in scene.
func (p *Persistence) SetPosition3D(scene, id string, x, y, z float32) {
	sc, st, ok := p.ensureObject(scene, id)
	if !ok {
		return
	}
	st.X = float32Ptr(x)
	st.Y = float32Ptr(y)
	st.PosZ = float32Ptr(z)
	p.putObject(sc, id, *st)
}

// Position returns the persisted 2D position override for object id in scene and whether one is set.
func (p *Persistence) Position(scene, id string) (x, y float32, ok bool) {
	st, exists := p.Object(scene, id)
	if !exists || st.X == nil || st.Y == nil {
		return 0, 0, false
	}
	return *st.X, *st.Y, true
}

// SetRotation records a persisted 2D rotation override (in radians) for object id in scene.
func (p *Persistence) SetRotation(scene, id string, rad float32) {
	sc, st, ok := p.ensureObject(scene, id)
	if !ok {
		return
	}
	st.Rotation = float32Ptr(rad)
	p.putObject(sc, id, *st)
}

// SetComponentField records a persisted override for component.field on object id in scene.
func (p *Persistence) SetComponentField(scene, id, component, field string, value any) {
	if component == "" || field == "" {
		return
	}
	sc, st, ok := p.ensureObject(scene, id)
	if !ok {
		return
	}
	if st.Components == nil {
		st.Components = make(map[string]map[string]any)
	}
	if st.Components[component] == nil {
		st.Components[component] = make(map[string]any)
	}
	st.Components[component][field] = cloneAny(value)
	p.putObject(sc, id, *st)
}

// ComponentField returns the persisted override for component.field on object id in scene.
func (p *Persistence) ComponentField(scene, id, component, field string) (any, bool) {
	st, ok := p.Object(scene, id)
	if !ok || st.Components == nil {
		return nil, false
	}
	fields, ok := st.Components[component]
	if !ok {
		return nil, false
	}
	v, ok := fields[field]
	if !ok {
		return nil, false
	}
	return cloneAny(v), true
}

// SetSwitch sets an RPG Maker-style per-object boolean self switch on object id in scene.
// Setting a switch to false removes it so default false values do not bloat the save.
func (p *Persistence) SetSwitch(scene, id, key string, value bool) {
	if key == "" {
		return
	}
	sc, st, ok := p.ensureObject(scene, id)
	if !ok {
		return
	}
	if !value {
		delete(st.Switches, key)
		if len(st.Switches) == 0 {
			st.Switches = nil
		}
	} else {
		if st.Switches == nil {
			st.Switches = make(map[string]bool)
		}
		st.Switches[key] = true
	}
	p.putObject(sc, id, *st)
}

// Switch returns the boolean value of self switch key on object id in scene (false when unset).
func (p *Persistence) Switch(scene, id, key string) bool {
	if p == nil || p.scenes == nil {
		return false
	}
	objs, ok := p.scenes[cleanScenePath(scene)]
	if !ok {
		return false
	}
	st, ok := objs[id]
	if !ok {
		return false
	}
	return st.Switches[key]
}

// SetVar stores a per-object variable under key on object id in scene.
// Passing nil deletes the variable.
func (p *Persistence) SetVar(scene, id, key string, value any) {
	if key == "" {
		return
	}
	sc, st, ok := p.ensureObject(scene, id)
	if !ok {
		return
	}
	if value == nil {
		delete(st.Vars, key)
		if len(st.Vars) == 0 {
			st.Vars = nil
		}
	} else {
		if st.Vars == nil {
			st.Vars = make(map[string]any)
		}
		st.Vars[key] = cloneAny(value)
	}
	p.putObject(sc, id, *st)
}

// Var returns the per-object variable stored under key on object id in scene.
func (p *Persistence) Var(scene, id, key string) (any, bool) {
	st, ok := p.Object(scene, id)
	if !ok || st.Vars == nil {
		return nil, false
	}
	v, ok := st.Vars[key]
	if !ok {
		return nil, false
	}
	return cloneAny(v), true
}

// Object returns a deep copy of the persisted state for object id in scene and whether any exists.
func (p *Persistence) Object(scene, id string) (ObjectState, bool) {
	if p == nil || p.scenes == nil || id == "" {
		return ObjectState{}, false
	}
	objs, ok := p.scenes[cleanScenePath(scene)]
	if !ok {
		return ObjectState{}, false
	}
	st, ok := objs[id]
	if !ok {
		return ObjectState{}, false
	}
	return cloneObjectState(st), true
}

// SetObject replaces the persisted state for object id in scene with a deep copy of st.
// An empty ObjectState clears the entry.
func (p *Persistence) SetObject(scene, id string, st ObjectState) {
	if p == nil || id == "" {
		return
	}
	sc := cleanScenePath(scene)
	if sc == "" {
		return
	}
	cleaned := normalizeObjectState(st)
	p.putObject(sc, id, cleaned)
}

// Scene returns a deep copy of every persisted object state in scene, or nil when none exist.
func (p *Persistence) Scene(scene string) map[string]ObjectState {
	if p == nil || p.scenes == nil {
		return nil
	}
	objs, ok := p.scenes[cleanScenePath(scene)]
	if !ok || len(objs) == 0 {
		return nil
	}
	out := make(map[string]ObjectState, len(objs))
	for id, st := range objs {
		out[id] = cloneObjectState(st)
	}
	return out
}

// Scenes returns the sorted scene paths that currently hold persisted state.
func (p *Persistence) Scenes() []string {
	if p == nil || len(p.scenes) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(p.scenes))
}

// ClearObject removes all persisted state for object id in scene.
func (p *Persistence) ClearObject(scene, id string) {
	p.deleteClean(cleanScenePath(scene), id)
}

// ClearScene removes all persisted state for scene.
func (p *Persistence) ClearScene(scene string) {
	if p == nil || p.scenes == nil {
		return
	}
	delete(p.scenes, cleanScenePath(scene))
}

// Clear removes all persisted state across every scene.
func (p *Persistence) Clear() {
	if p == nil {
		return
	}
	p.scenes = nil
}

// Snapshot returns a deep, deterministic copy of p's state.
func (p *Persistence) Snapshot() PersistenceSnapshot {
	if p == nil || len(p.scenes) == 0 {
		return PersistenceSnapshot{}
	}
	scenes := make(map[string]map[string]ObjectState, len(p.scenes))
	for sc, objs := range p.scenes {
		if len(objs) == 0 {
			continue
		}
		cp := make(map[string]ObjectState, len(objs))
		for id, st := range objs {
			cl := normalizeObjectState(st)
			if !cl.empty() {
				cp[id] = cl
			}
		}
		if len(cp) > 0 {
			scenes[sc] = cp
		}
	}
	if len(scenes) == 0 {
		return PersistenceSnapshot{}
	}
	return PersistenceSnapshot{Scenes: scenes}
}

// Restore validates snap and atomically replaces p's state. On error p is unchanged.
func (p *Persistence) Restore(snap PersistenceSnapshot) error {
	if p == nil {
		return errors.New("save: nil persistence")
	}
	next, err := preparePersistenceMap(snap)
	if err != nil {
		return err
	}
	p.scenes = next
	return nil
}

// SaveState implements Part.
func (p *Persistence) SaveState() (any, error) {
	if p == nil {
		return nil, errors.New("save: nil persistence")
	}
	return p.Snapshot(), nil
}

// LoadState implements Part.
func (p *Persistence) LoadState(raw json.RawMessage) error {
	apply, err := p.PrepareLoad(raw)
	if err != nil {
		return err
	}
	apply()
	return nil
}

// PrepareLoad implements StagedPart. Integer JSON numbers inside Components and Vars are preserved
// as Go int values so scene component fields typed as FieldInt round-trip cleanly.
func (p *Persistence) PrepareLoad(raw json.RawMessage) (func(), error) {
	if p == nil {
		return nil, errors.New("save: nil persistence")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	var snap PersistenceSnapshot
	if err := dec.Decode(&snap); err != nil {
		return nil, fmt.Errorf("save: persistence: %w", err)
	}
	next, err := preparePersistenceMap(snap)
	if err != nil {
		return nil, err
	}
	return func() { p.scenes = next }, nil
}

// ResetState implements Resetter.
func (p *Persistence) ResetState() {
	p.Clear()
}

func preparePersistenceMap(snap PersistenceSnapshot) (map[string]map[string]ObjectState, error) {
	if len(snap.Scenes) == 0 {
		return nil, nil
	}
	out := make(map[string]map[string]ObjectState, len(snap.Scenes))
	for rawScene, objs := range snap.Scenes {
		sc := cleanScenePath(rawScene)
		if sc == "" {
			return nil, fmt.Errorf("save: persistence: invalid empty scene path %q", rawScene)
		}
		if len(objs) == 0 {
			continue
		}
		dstObjs := out[sc]
		if dstObjs == nil {
			dstObjs = make(map[string]ObjectState, len(objs))
		}
		for id, st := range objs {
			if id == "" {
				return nil, fmt.Errorf("save: persistence: scene %q has empty object ID", sc)
			}
			if err := validateObjectState(sc, id, st); err != nil {
				return nil, err
			}
			cl := normalizeObjectState(st)
			if !cl.empty() {
				dstObjs[id] = cl
			}
		}
		if len(dstObjs) > 0 {
			out[sc] = dstObjs
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func validateObjectState(sc, id string, st ObjectState) error {
	checkF32 := func(field string, p *float32) error {
		if p != nil && (math.IsNaN(float64(*p)) || math.IsInf(float64(*p), 0)) {
			return fmt.Errorf("save: persistence: scene %q object %q %s is not finite", sc, id, field)
		}
		return nil
	}
	if err := checkF32("x", st.X); err != nil {
		return err
	}
	if err := checkF32("y", st.Y); err != nil {
		return err
	}
	if err := checkF32("posZ", st.PosZ); err != nil {
		return err
	}
	if err := checkF32("rotation", st.Rotation); err != nil {
		return err
	}
	for comp, fields := range st.Components {
		if comp == "" {
			return fmt.Errorf("save: persistence: scene %q object %q has empty component name", sc, id)
		}
		for field := range fields {
			if field == "" {
				return fmt.Errorf("save: persistence: scene %q object %q component %q has empty field name", sc, id, comp)
			}
		}
	}
	for k := range st.Switches {
		if k == "" {
			return fmt.Errorf("save: persistence: scene %q object %q has empty switch name", sc, id)
		}
	}
	for k := range st.Vars {
		if k == "" {
			return fmt.Errorf("save: persistence: scene %q object %q has empty var name", sc, id)
		}
	}
	return nil
}

func normalizeObjectState(st ObjectState) ObjectState {
	out := ObjectState{
		Destroyed: st.Destroyed,
	}
	if st.X != nil {
		out.X = float32Ptr(*st.X)
	}
	if st.Y != nil {
		out.Y = float32Ptr(*st.Y)
	}
	if st.PosZ != nil {
		out.PosZ = float32Ptr(*st.PosZ)
	}
	if st.Rotation != nil {
		out.Rotation = float32Ptr(*st.Rotation)
	}
	if len(st.Components) > 0 {
		comps := make(map[string]map[string]any, len(st.Components))
		for comp, fields := range st.Components {
			if len(fields) == 0 {
				continue
			}
			cf := make(map[string]any, len(fields))
			for k, v := range fields {
				cf[k] = normalizeDynamicValue(v)
			}
			comps[comp] = cf
		}
		if len(comps) > 0 {
			out.Components = comps
		}
	}
	if len(st.Switches) > 0 {
		sw := make(map[string]bool, len(st.Switches))
		for k, v := range st.Switches {
			if v {
				sw[k] = true
			}
		}
		if len(sw) > 0 {
			out.Switches = sw
		}
	}
	if len(st.Vars) > 0 {
		vs := make(map[string]any, len(st.Vars))
		for k, v := range st.Vars {
			if v != nil {
				vs[k] = normalizeDynamicValue(v)
			}
		}
		if len(vs) > 0 {
			out.Vars = vs
		}
	}
	return out
}

func cloneObjectState(st ObjectState) ObjectState {
	return normalizeObjectState(st)
}

func cloneAny(v any) any {
	return normalizeDynamicValue(v)
}

func normalizeDynamicValue(v any) any {
	switch x := v.(type) {
	case json.Number:
		s := x.String()
		if !strings.ContainsAny(s, ".eE") {
			if i, err := strconv.Atoi(s); err == nil {
				return i
			}
		}
		if f, err := x.Float64(); err == nil {
			return f
		}
		return s
	case map[string]any:
		cp := make(map[string]any, len(x))
		for k, val := range x {
			cp[k] = normalizeDynamicValue(val)
		}
		return cp
	case []any:
		cp := make([]any, len(x))
		for i, val := range x {
			cp[i] = normalizeDynamicValue(val)
		}
		return cp
	case []string:
		return slices.Clone(x)
	case [][2]float64:
		return slices.Clone(x)
	default:
		return v
	}
}
