package flow

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
)

// VarsSnapshotVersion is the [VarsSnapshot] schema version written and read by [Vars.Snapshot] and [Vars.Restore].
const VarsSnapshotVersion = 1

// VarChange describes one global variable mutation delivered to [Vars.OnChange] listeners.
type VarChange struct {
	Name string
	Old  any
	New  any
}

// VarInfo describes one declared variable's metadata alongside its current runtime value,
// used by inspectors, debuggers, and MCP tools.
type VarInfo struct {
	Name    string
	Kind    VarKind
	Value   any
	Default any
	Min     *float64
	Max     *float64
	Doc     string
}

// VarsSnapshot is a plain JSON/YAML-serializable snapshot of a [Vars] store's global variables
// and per-object self variables for save slots and deterministic state restore.
type VarsSnapshot struct {
	// Version is VarsSnapshotVersion (1).
	Version int `json:"version" yaml:"version"`
	// Vars maps declared global variable names to their saved values.
	Vars map[string]any `json:"vars,omitempty" yaml:"vars,omitempty"`
	// Self maps scene object IDs to their saved per-object self variables.
	Self map[string]map[string]any `json:"self,omitempty" yaml:"self,omitempty"`
}

type varListener struct {
	id uint64
	fn func(VarChange)
}

// Vars holds project-wide declared game variables and switches alongside scoped per-object
// self variables, with typed accessors, range enforcement, deterministic change notifications,
// and save/restore snapshots.
//
// Nothing in Vars reads the wall clock, spawns goroutines, or holds package-level state.
type Vars struct {
	order      []string
	decls      map[string]VarDecl
	values     map[string]any
	self       map[string]map[string]any
	listeners  map[string][]varListener // key "" receives changes for every global variable
	nextListen uint64
}

// NewVars creates a runtime variable store initialized to the defaults declared in doc.
// Passing nil creates an empty store with no global declarations (self variables still work).
func NewVars(doc *VarsDoc) (*Vars, error) {
	v := &Vars{
		decls:  make(map[string]VarDecl),
		values: make(map[string]any),
	}
	if doc == nil {
		return v, nil
	}
	if err := ValidateVars(doc); err != nil {
		return nil, err
	}
	norm := cloneVarsDoc(doc)
	normalizeVarsDoc(norm)
	v.order = make([]string, 0, len(norm.Vars))
	for _, d := range norm.Vars {
		v.order = append(v.order, d.Name)
		v.decls[d.Name] = d
		v.values[d.Name] = d.DefaultValue()
	}
	return v, nil
}

// Has reports whether name (optional leading '$' ignored) is declared in v.
func (v *Vars) Has(name string) bool {
	if v == nil {
		return false
	}
	_, ok := v.decls[normalizeVarName(name)]
	return ok
}

// Decl returns a copy of the declaration for name (optional leading '$' ignored).
func (v *Vars) Decl(name string) (VarDecl, bool) {
	if v == nil {
		return VarDecl{}, false
	}
	d, ok := v.decls[normalizeVarName(name)]
	if !ok {
		return VarDecl{}, false
	}
	return cloneVarDecl(d), true
}

// Decls returns copies of all declarations in declaration order.
func (v *Vars) Decls() []VarDecl {
	if v == nil || len(v.order) == 0 {
		return nil
	}
	out := make([]VarDecl, len(v.order))
	for i, name := range v.order {
		out[i] = cloneVarDecl(v.decls[name])
	}
	return out
}

// Names returns all declared variable names in declaration order.
func (v *Vars) Names() []string {
	if v == nil || len(v.order) == 0 {
		return nil
	}
	return slices.Clone(v.order)
}

// Info returns the current value and declaration metadata for name (optional leading '$' ignored).
func (v *Vars) Info(name string) (VarInfo, bool) {
	if v == nil {
		return VarInfo{}, false
	}
	key := normalizeVarName(name)
	d, ok := v.decls[key]
	if !ok {
		return VarInfo{}, false
	}
	return varInfoFrom(d, v.values[key]), true
}

// List returns Info for every declared variable in declaration order.
func (v *Vars) List() []VarInfo {
	if v == nil || len(v.order) == 0 {
		return nil
	}
	out := make([]VarInfo, len(v.order))
	for i, name := range v.order {
		out[i] = varInfoFrom(v.decls[name], v.values[name])
	}
	return out
}

func varInfoFrom(d VarDecl, val any) VarInfo {
	info := VarInfo{
		Name:    d.Name,
		Kind:    d.Kind,
		Value:   val,
		Default: d.DefaultValue(),
		Doc:     d.Doc,
	}
	if d.Min != nil {
		m := *d.Min
		info.Min = &m
	}
	if d.Max != nil {
		m := *d.Max
		info.Max = &m
	}
	return info
}

// Get returns the current typed value (bool, int, float64, or string) of name and whether it is declared.
// A leading '$' is optional and normalized away.
func (v *Vars) Get(name string) (any, bool) {
	if v == nil {
		return nil, false
	}
	key := normalizeVarName(name)
	if _, ok := v.decls[key]; !ok {
		return nil, false
	}
	return v.values[key], true
}

// Lookup returns the current value of name for expression evaluators and condition hooks.
// It is equivalent to Get and accepts an optional leading '$'.
func (v *Vars) Lookup(name string) (any, bool) {
	return v.Get(name)
}

// Map returns a shallow copy of all declared variables and their current values.
func (v *Vars) Map() map[string]any {
	if v == nil || len(v.values) == 0 {
		return make(map[string]any)
	}
	out := make(map[string]any, len(v.values))
	maps.Copy(out, v.values)
	return out
}

// Bool returns the current value of a VarBool variable.
func (v *Vars) Bool(name string) (bool, error) {
	d, val, err := v.getDeclared(name)
	if err != nil {
		return false, err
	}
	if d.Kind != VarBool {
		return false, fmt.Errorf("%w: variable %q is %s, not bool", ErrVarTypeMismatch, d.Name, d.Kind)
	}
	b, _ := val.(bool)
	return b, nil
}

// Int returns the current value of a VarInt variable.
func (v *Vars) Int(name string) (int, error) {
	d, val, err := v.getDeclared(name)
	if err != nil {
		return 0, err
	}
	if d.Kind != VarInt {
		return 0, fmt.Errorf("%w: variable %q is %s, not int", ErrVarTypeMismatch, d.Name, d.Kind)
	}
	iv, _ := val.(int)
	return iv, nil
}

// Float returns the current value of a VarFloat variable.
func (v *Vars) Float(name string) (float64, error) {
	d, val, err := v.getDeclared(name)
	if err != nil {
		return 0, err
	}
	if d.Kind != VarFloat {
		return 0, fmt.Errorf("%w: variable %q is %s, not float", ErrVarTypeMismatch, d.Name, d.Kind)
	}
	fv, _ := val.(float64)
	return fv, nil
}

// String returns the current value of a VarString variable.
func (v *Vars) String(name string) (string, error) {
	d, val, err := v.getDeclared(name)
	if err != nil {
		return "", err
	}
	if d.Kind != VarString {
		return "", fmt.Errorf("%w: variable %q is %s, not string", ErrVarTypeMismatch, d.Name, d.Kind)
	}
	s, _ := val.(string)
	return s, nil
}

func (v *Vars) getDeclared(name string) (VarDecl, any, error) {
	key := normalizeVarName(name)
	if v == nil {
		return VarDecl{}, nil, fmt.Errorf("%w: %q", ErrUnknownVar, key)
	}
	d, ok := v.decls[key]
	if !ok {
		return VarDecl{}, nil, fmt.Errorf("%w: %q", ErrUnknownVar, key)
	}
	return d, v.values[key], nil
}

// Set updates the declared variable name to val, enforcing its declared VarKind and optional [Min, Max] bounds.
// If the value changes, registered OnChange listeners are called synchronously in registration order.
func (v *Vars) Set(name string, val any) error {
	_, _, _, err := v.setValue(name, val, false)
	return err
}

// SetClamped updates the declared variable name to val like Set, but clamps out-of-range numeric values
// into [Min, Max] instead of returning ErrVarOutOfRange.
func (v *Vars) SetClamped(name string, val any) (oldVal, newVal any, clamped bool, err error) {
	return v.setValue(name, val, true)
}

// SetBool updates a VarBool variable.
func (v *Vars) SetBool(name string, val bool) error {
	return v.Set(name, val)
}

// SetInt updates a VarInt variable, enforcing any declared [Min, Max] range.
func (v *Vars) SetInt(name string, val int) error {
	return v.Set(name, val)
}

// SetFloat updates a VarFloat variable, refusing NaN/Inf and enforcing any declared [Min, Max] range.
func (v *Vars) SetFloat(name string, val float64) error {
	return v.Set(name, val)
}

// SetString updates a VarString variable.
func (v *Vars) SetString(name string, val string) error {
	return v.Set(name, val)
}

func (v *Vars) setValue(name string, val any, clamp bool) (oldVal, newVal any, clamped bool, err error) {
	d, cur, err := v.getDeclared(name)
	if err != nil {
		return nil, nil, false, err
	}
	coerced, didClamp, err := coerceVarValue(d, val, clamp, false)
	if err != nil {
		return nil, nil, false, err
	}
	if cur == coerced {
		return cur, coerced, didClamp, nil
	}
	v.values[d.Name] = coerced
	v.notify(VarChange{Name: d.Name, Old: cur, New: coerced})
	return cur, coerced, didClamp, nil
}

// SetFromExpr updates a declared variable from an expression language's value, such as a Yarn dialogue script's, where every number is a float64: an integral float64 is accepted for a VarInt variable. A leading '$' on name is optional. An undeclared name returns an error wrapping [ErrUndeclaredVar]; kinds and declared ranges are enforced as by [Vars.Set].
func (v *Vars) SetFromExpr(name string, val any) error {
	d, cur, err := v.getDeclared(name)
	if err != nil {
		return fmt.Errorf("%w: $%s", ErrUndeclaredVar, normalizeVarName(name))
	}
	coerced, _, err := coerceVarValue(d, val, false, true)
	if err != nil {
		return err
	}
	if cur == coerced {
		return nil
	}
	v.values[d.Name] = coerced
	v.notify(VarChange{Name: d.Name, Old: cur, New: coerced})
	return nil
}

func coerceVarValue(d VarDecl, val any, clamp, allowFloatToInt bool) (coerced any, clamped bool, err error) {
	switch d.Kind {
	case VarBool:
		b, ok := val.(bool)
		if !ok {
			return nil, false, fmt.Errorf("%w: variable %q expects bool, got %T", ErrVarTypeMismatch, d.Name, val)
		}
		return b, false, nil

	case VarString:
		s, ok := val.(string)
		if !ok {
			return nil, false, fmt.Errorf("%w: variable %q expects string, got %T", ErrVarTypeMismatch, d.Name, val)
		}
		return s, false, nil

	case VarInt:
		iv, ok := toExactInt(val)
		if !ok && allowFloatToInt {
			if f, isF := val.(float64); isF {
				if math.IsNaN(f) || math.IsInf(f, 0) || math.Trunc(f) != f {
					return nil, false, fmt.Errorf("%w: variable %q expects int, got %v", ErrVarTypeMismatch, d.Name, f)
				}
				if f < float64(math.MinInt) || f > float64(math.MaxInt) {
					return nil, false, fmt.Errorf("%w: variable %q integer %g overflows int", ErrVarOutOfRange, d.Name, f)
				}
				iv, ok = int(f), true
			}
		}
		if !ok {
			return nil, false, fmt.Errorf("%w: variable %q expects int, got %T", ErrVarTypeMismatch, d.Name, val)
		}
		if d.Min != nil && float64(iv) < *d.Min {
			if !clamp {
				return nil, false, fmt.Errorf("%w: variable %q value %d is less than min %g", ErrVarOutOfRange, d.Name, iv, *d.Min)
			}
			iv = int(*d.Min)
			clamped = true
		}
		if d.Max != nil && float64(iv) > *d.Max {
			if !clamp {
				return nil, false, fmt.Errorf("%w: variable %q value %d is greater than max %g", ErrVarOutOfRange, d.Name, iv, *d.Max)
			}
			iv = int(*d.Max)
			clamped = true
		}
		return iv, clamped, nil

	case VarFloat:
		var fv float64
		switch x := val.(type) {
		case float64:
			fv = x
		case float32:
			fv = float64(x)
		default:
			if allowFloatToInt {
				if f, ok := toFiniteFloat(val); ok {
					fv = f
					break
				}
			}
			return nil, false, fmt.Errorf("%w: variable %q expects float, got %T", ErrVarTypeMismatch, d.Name, val)
		}
		if math.IsNaN(fv) || math.IsInf(fv, 0) {
			return nil, false, fmt.Errorf("%w: variable %q requires a finite float, got %v", ErrVarOutOfRange, d.Name, fv)
		}
		if fv == 0 {
			fv = 0
		}
		if d.Min != nil && fv < *d.Min {
			if !clamp {
				return nil, false, fmt.Errorf("%w: variable %q value %g is less than min %g", ErrVarOutOfRange, d.Name, fv, *d.Min)
			}
			fv = *d.Min
			clamped = true
		}
		if d.Max != nil && fv > *d.Max {
			if !clamp {
				return nil, false, fmt.Errorf("%w: variable %q value %g is greater than max %g", ErrVarOutOfRange, d.Name, fv, *d.Max)
			}
			fv = *d.Max
			clamped = true
		}
		if fv == 0 {
			fv = 0
		}
		return fv, clamped, nil

	default:
		return nil, false, fmt.Errorf("%w: variable %q has unknown kind %q", ErrInvalidVars, d.Name, d.Kind)
	}
}

// OnChange subscribes fn to changes of the named global variable (optional leading '$' ignored),
// or to every global variable change when name is "".
// Listeners are invoked synchronously in registration order. Unsubscribing or subscribing inside
// a callback never skips remaining listeners for the change in flight.
// The returned function unsubscribes fn and is safe to call more than once.
func (v *Vars) OnChange(name string, fn func(VarChange)) func() {
	if v == nil || fn == nil {
		return func() {}
	}
	key := ""
	if name != "" {
		key = normalizeVarName(name)
	}
	v.nextListen++
	id := v.nextListen
	if v.listeners == nil {
		v.listeners = make(map[string][]varListener)
	}
	v.listeners[key] = append(v.listeners[key], varListener{id: id, fn: fn})
	removed := false
	return func() {
		if removed {
			return
		}
		removed = true
		list := v.listeners[key]
		for i, item := range list {
			if item.id == id {
				v.listeners[key] = append(list[:i], list[i+1:]...)
				if len(v.listeners[key]) == 0 {
					delete(v.listeners, key)
				}
				return
			}
		}
	}
}

func (v *Vars) notify(ch VarChange) {
	if len(v.listeners) == 0 {
		return
	}
	named := v.listeners[ch.Name]
	anyList := v.listeners[""]
	if len(named) == 0 && len(anyList) == 0 {
		return
	}
	// Merge named and wildcard listeners in global registration order (by monotonically increasing id)
	// using a snapshot so callbacks may safely subscribe or unsubscribe.
	snap := make([]varListener, 0, len(named)+len(anyList))
	i, j := 0, 0
	for i < len(named) && j < len(anyList) {
		if named[i].id < anyList[j].id {
			snap = append(snap, named[i])
			i++
		} else {
			snap = append(snap, anyList[j])
			j++
		}
	}
	snap = append(snap, named[i:]...)
	snap = append(snap, anyList[j:]...)
	for _, l := range snap {
		l.fn(ch)
	}
}

// Reset restores every declared variable to its declared default and clears all per-object self variables.
// Global variables whose value actually changed notify their OnChange listeners in declaration order.
func (v *Vars) Reset() {
	if v == nil {
		return
	}
	v.self = nil
	for _, name := range v.order {
		def := v.decls[name].DefaultValue()
		cur := v.values[name]
		if cur != def {
			v.values[name] = def
			v.notify(VarChange{Name: name, Old: cur, New: def})
		}
	}
}

// --- Scoped per-object self variables ---

// SelfVars is a view of the per-object self variables belonging to one scene object ID inside a [Vars] store.
// Self variables model per-instance state such as "this chest was opened" without cluttering global declarations.
type SelfVars struct {
	store    *Vars
	objectID string
}

// Self returns a scoped self-variable view for objectID inside v.
// Calling Self allocates no map entry until a key is set.
func (v *Vars) Self(objectID string) *SelfVars {
	return &SelfVars{store: v, objectID: objectID}
}

// SelfObjects returns the sorted list of object IDs that currently have at least one self variable set.
func (v *Vars) SelfObjects() []string {
	if v == nil || len(v.self) == 0 {
		return nil
	}
	out := make([]string, 0, len(v.self))
	for id, m := range v.self {
		if len(m) > 0 {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// ClearSelf removes all self variables recorded for objectID.
func (v *Vars) ClearSelf(objectID string) {
	if v == nil || v.self == nil {
		return
	}
	delete(v.self, objectID)
}

// ObjectID returns the scene object ID this view is scoped to.
func (s *SelfVars) ObjectID() string {
	if s == nil {
		return ""
	}
	return s.objectID
}

// Has reports whether key is set on this object.
func (s *SelfVars) Has(key string) bool {
	_, ok := s.Get(key)
	return ok
}

// Get returns the value stored for key on this object and whether it is set.
func (s *SelfVars) Get(key string) (any, bool) {
	if s == nil || s.store == nil || s.store.self == nil {
		return nil, false
	}
	m := s.store.self[s.objectID]
	if m == nil {
		return nil, false
	}
	val, ok := m[key]
	return val, ok
}

// Bool returns the boolean value of key on this object, or false when unset or non-bool.
func (s *SelfVars) Bool(key string) bool {
	val, ok := s.Get(key)
	if !ok {
		return false
	}
	b, _ := val.(bool)
	return b
}

// Int returns the integer value of key on this object, or 0 when unset or non-integer.
func (s *SelfVars) Int(key string) int {
	val, ok := s.Get(key)
	if !ok {
		return 0
	}
	if iv, ok := toExactInt(val); ok {
		return iv
	}
	if f, ok := val.(float64); ok && !math.IsNaN(f) && !math.IsInf(f, 0) && math.Trunc(f) == f && f >= float64(math.MinInt) && f <= float64(math.MaxInt) {
		return int(f)
	}
	return 0
}

// Float returns the float64 value of key on this object, or 0 when unset or non-numeric.
func (s *SelfVars) Float(key string) float64 {
	val, ok := s.Get(key)
	if !ok {
		return 0
	}
	if fv, ok := toFiniteFloat(val); ok {
		return fv
	}
	return 0
}

// String returns the string value of key on this object, or "" when unset or non-string.
func (s *SelfVars) String(key string) string {
	val, ok := s.Get(key)
	if !ok {
		return ""
	}
	str, _ := val.(string)
	return str
}

// Set stores a scalar value (bool, int, finite float64, or string) under key on this object.
func (s *SelfVars) Set(key string, val any) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("%w: nil vars store", ErrInvalidVars)
	}
	if strings.TrimSpace(s.objectID) == "" {
		return fmt.Errorf("%w: empty self object id", ErrInvalidVars)
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("%w: empty self variable key", ErrInvalidVars)
	}
	norm, err := normalizeSelfValue(val)
	if err != nil {
		return err
	}
	if s.store.self == nil {
		s.store.self = make(map[string]map[string]any)
	}
	m := s.store.self[s.objectID]
	if m == nil {
		m = make(map[string]any)
		s.store.self[s.objectID] = m
	}
	m[key] = norm
	return nil
}

// SetBool sets a boolean self switch on this object.
func (s *SelfVars) SetBool(key string, val bool) { _ = s.Set(key, val) }

// SetInt sets an integer self variable on this object.
func (s *SelfVars) SetInt(key string, val int) { _ = s.Set(key, val) }

// SetFloat sets a finite float64 self variable on this object.
func (s *SelfVars) SetFloat(key string, val float64) error { return s.Set(key, val) }

// SetString sets a string self variable on this object.
func (s *SelfVars) SetString(key string, val string) { _ = s.Set(key, val) }

// Delete removes key from this object's self variables.
func (s *SelfVars) Delete(key string) {
	if s == nil || s.store == nil || s.store.self == nil {
		return
	}
	m := s.store.self[s.objectID]
	if m == nil {
		return
	}
	delete(m, key)
	if len(m) == 0 {
		delete(s.store.self, s.objectID)
	}
}

// Clear removes all self variables on this object.
func (s *SelfVars) Clear() {
	if s == nil || s.store == nil {
		return
	}
	s.store.ClearSelf(s.objectID)
}

// Keys returns the sorted list of self variable keys set on this object.
func (s *SelfVars) Keys() []string {
	if s == nil || s.store == nil || s.store.self == nil {
		return nil
	}
	m := s.store.self[s.objectID]
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Map returns a shallow copy of this object's self variables.
func (s *SelfVars) Map() map[string]any {
	if s == nil || s.store == nil || s.store.self == nil {
		return nil
	}
	m := s.store.self[s.objectID]
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	maps.Copy(out, m)
	return out
}

func normalizeSelfValue(val any) (any, error) {
	switch x := val.(type) {
	case bool:
		return x, nil
	case string:
		return x, nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, fmt.Errorf("%w: self variable float must be finite, got %v", ErrVarOutOfRange, x)
		}
		if x == 0 {
			return 0.0, nil
		}
		return x, nil
	case float32:
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("%w: self variable float must be finite, got %v", ErrVarOutOfRange, x)
		}
		if f == 0 {
			return 0.0, nil
		}
		return f, nil
	default:
		if iv, ok := toExactInt(val); ok {
			return iv, nil
		}
		return nil, fmt.Errorf("%w: self variable expects bool, int, float, or string, got %T", ErrVarTypeMismatch, val)
	}
}

// --- Snapshot & Restore ---

// Snapshot returns a deterministic deep copy of v's global variables and per-object self variables.
// It shares no memory with v and encodes cleanly to JSON or YAML for save slots.
func (v *Vars) Snapshot() VarsSnapshot {
	snap := VarsSnapshot{Version: VarsSnapshotVersion}
	if v == nil {
		return snap
	}
	if len(v.values) > 0 {
		snap.Vars = make(map[string]any, len(v.values))
		maps.Copy(snap.Vars, v.values)
	}
	if len(v.self) > 0 {
		for id, m := range v.self {
			if len(m) == 0 {
				continue
			}
			if snap.Self == nil {
				snap.Self = make(map[string]map[string]any, len(v.self))
			}
			cp := make(map[string]any, len(m))
			maps.Copy(cp, m)
			snap.Self[id] = cp
		}
	}
	return snap
}

// Restore validates snap against v's declarations and replaces v's values and self variables atomically
// without invoking OnChange listeners.
//
// The zero VarsSnapshot (Version 0 with empty Vars and Self) resets every variable to its declared
// default and clears all self variables, matching a save slot written before variables were saved.
// Variables declared in v but absent from snap keep their declared defaults so new variables added
// in a game update load older saves cleanly. Unknown variable names, type mismatches, out-of-range
// values, or unsupported versions return an error wrapping [ErrInvalidSnapshot] without modifying v.
func (v *Vars) Restore(snap VarsSnapshot) error {
	if v == nil {
		return fmt.Errorf("%w: nil vars store", ErrInvalidSnapshot)
	}
	if snap.Version == 0 && len(snap.Vars) == 0 && len(snap.Self) == 0 {
		v.values = make(map[string]any, len(v.order))
		for _, name := range v.order {
			v.values[name] = v.decls[name].DefaultValue()
		}
		v.self = nil
		return nil
	}
	if snap.Version != VarsSnapshotVersion {
		return fmt.Errorf("%w: unsupported vars snapshot version %d (want %d)", ErrInvalidSnapshot, snap.Version, VarsSnapshotVersion)
	}

	nextVals := make(map[string]any, len(v.order))
	for _, name := range v.order {
		nextVals[name] = v.decls[name].DefaultValue()
	}
	for rawName, rawVal := range snap.Vars {
		name := normalizeVarName(rawName)
		d, ok := v.decls[name]
		if !ok {
			return fmt.Errorf("%w: %w: %q", ErrInvalidSnapshot, ErrUnknownVar, rawName)
		}
		// Allow whole-number float64 -> int coercion because encoding/json decodes numbers in map[string]any as float64.
		coerced, _, err := coerceVarValue(d, rawVal, false, true)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidSnapshot, err)
		}
		nextVals[name] = coerced
	}

	var nextSelf map[string]map[string]any
	if len(snap.Self) > 0 {
		nextSelf = make(map[string]map[string]any, len(snap.Self))
		for objID, m := range snap.Self {
			if strings.TrimSpace(objID) == "" {
				return fmt.Errorf("%w: empty self object id", ErrInvalidSnapshot)
			}
			if len(m) == 0 {
				continue
			}
			cp := make(map[string]any, len(m))
			for k, rawVal := range m {
				if strings.TrimSpace(k) == "" {
					return fmt.Errorf("%w: empty self variable key on %q", ErrInvalidSnapshot, objID)
				}
				norm, err := normalizeSelfValue(rawVal)
				if err != nil {
					return fmt.Errorf("%w: self %q.%q: %w", ErrInvalidSnapshot, objID, k, err)
				}
				cp[k] = norm
			}
			nextSelf[objID] = cp
		}
		if len(nextSelf) == 0 {
			nextSelf = nil
		}
	}

	v.values = nextVals
	v.self = nextSelf
	return nil
}

// normalizeVarName strips the optional leading '$' that Yarn scripts put on variable names.
func normalizeVarName(name string) string {
	return strings.TrimPrefix(name, "$")
}
