package sceneformat

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// FieldKind is the value type of one registered component field.
type FieldKind uint8

const (
	// FieldBool holds true or false and decodes to a Go bool.
	FieldBool FieldKind = iota + 1
	// FieldInt holds a whole number and decodes to a Go int. A YAML float such as 3.0 is refused.
	FieldInt
	// FieldFloat holds a finite number and decodes to a Go float64. A YAML integer is accepted and widened.
	FieldFloat
	// FieldString holds text and decodes to a Go string.
	FieldString
	// FieldStringList holds a list of strings (such as named collision masks) and decodes to a Go []string.
	FieldStringList
	// FieldPointList holds a list of 2D [x, y] points (such as an occluder polygon's vertices) and decodes to a Go [][2]float64.
	FieldPointList
	// FieldPoint holds a 2D point {x, y} in object-local space and decodes to a Go Point.
	FieldPoint
	// FieldRect holds a 2D rectangle {x, y, width, height} in object-local space and decodes to a Go Rect.
	FieldRect
	// FieldCircle holds a 2D circle {x, y, radius} in object-local space and decodes to a Go Circle.
	FieldCircle
	// FieldPolygon holds a list of 2D points [{x, y}, ...] in object-local space and decodes to a Go []Point.
	FieldPolygon
)

// String returns the kind's name as written in error messages: "bool", "int", "float", "string", "[]string", "[][2]float64", "point", "rect", "circle" or "polygon".
func (k FieldKind) String() string {
	switch k {
	case FieldBool:
		return "bool"
	case FieldInt:
		return "int"
	case FieldFloat:
		return "float"
	case FieldString:
		return "string"
	case FieldStringList:
		return "[]string"
	case FieldPointList:
		return "[][2]float64"
	case FieldPoint:
		return "point"
	case FieldRect:
		return "rect"
	case FieldCircle:
		return "circle"
	case FieldPolygon:
		return "polygon"
	default:
		return "FieldKind(" + strconv.Itoa(int(k)) + ")"
	}
}

// IsNumeric reports whether the kind is FieldInt or FieldFloat, the kinds a Field's Min and Max apply to.
func (k FieldKind) IsNumeric() bool { return k == FieldInt || k == FieldFloat }

// IsShape reports whether the kind is one of the 2D geometry shape kinds: FieldPoint, FieldRect, FieldCircle or FieldPolygon.
func (k FieldKind) IsShape() bool { return k >= FieldPoint && k <= FieldPolygon }

// Field describes one field of a component type: its YAML key, value kind, default, optional inclusive range and documentation.
type Field struct {
	// Name is the field's YAML key inside the component mapping. It must be non-empty and unique within its component type.
	Name string
	// Kind is the field's value type.
	Kind FieldKind
	// Default is the value an unset field takes. Nil means the kind's zero value (false, 0, 0.0 or ""). Register normalizes it to bool, int, float64 or string and refuses a default of the wrong kind or outside the range.
	Default any
	// Min and Max are the optional inclusive bounds of a FieldInt or FieldFloat value; nil leaves that side open. They are refused on other kinds, and on a FieldInt they must be whole numbers.
	Min, Max *float64
	// Step is the increment tools such as the editor's inspector use for a FieldInt or FieldFloat value, and the precision a typed value is rounded to from Min; 0 means 1 for a FieldInt and 0.1 for a FieldFloat. It is refused when negative or not finite, on other kinds, and on a FieldInt when it is not a whole number.
	Step float64
	// Enum optionally restricts a FieldString value to one of the listed strings. Refused on other kinds.
	Enum []string
	// Doc is a one-line description shown by tools such as the editor's inspector.
	Doc string
}

// ComponentType is a registered component: the name it is keyed by under an object's components and its fields in declaration order.
type ComponentType struct {
	// Name is the key under an object's components mapping, such as "body" or "collider". It must be non-empty and unique within a registry.
	Name string
	// Doc is a one-line description shown by tools such as the editor's inspector.
	Doc string
	// Fields are the component's fields in the order tools list them.
	Fields []Field
	// Events are the events an object with this component emits, such as a button's "pressed", in the order tools list them. A scene's connections (SceneDoc.Connections) name them, and Signals.Emit fires them.
	Events []Event
	// Handlers are what an object with this component can do when an event connected to it fires, such as a door's "open", in the order tools list them.
	Handlers []Handler
}

// Field returns the field named name and whether the type has it.
func (t *ComponentType) Field(name string) (Field, bool) {
	if t == nil {
		return Field{}, false
	}
	for _, f := range t.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// Event returns the event named name and whether the type declares it.
func (t *ComponentType) Event(name string) (Event, bool) {
	if t == nil {
		return Event{}, false
	}
	for _, e := range t.Events {
		if e.Name == name {
			return e, true
		}
	}
	return Event{}, false
}

// Handler returns the handler named name and whether the type declares it.
func (t *ComponentType) Handler(name string) (Handler, bool) {
	if t == nil {
		return Handler{}, false
	}
	for _, h := range t.Handlers {
		if h.Name == name {
			return h, true
		}
	}
	return Handler{}, false
}

// FieldNames returns the type's field names in declaration order.
func (t *ComponentType) FieldNames() []string {
	if t == nil {
		return nil
	}
	names := make([]string, len(t.Fields))
	for i, f := range t.Fields {
		names[i] = f.Name
	}
	return names
}

// ComponentRegistry holds the component types scene files may use. Types are declared explicitly in Go, so loading needs no reflection over game structs. Register every type before the registry is shared; lookups are safe from many goroutines once registration is done, but Register is not safe to call concurrently with them.
type ComponentRegistry struct {
	types map[string]*ComponentType
}

// NewComponentRegistry returns an empty registry.
func NewComponentRegistry() *ComponentRegistry {
	return &ComponentRegistry{types: make(map[string]*ComponentType)}
}

// Float64 returns a pointer to v for a Field's optional Min and Max.
func Float64(v float64) *float64 { return &v }

// Register adds t to the registry. It refuses an empty or duplicate type name, an empty or duplicate field name, an unknown kind, a range or step on a non-numeric field, a range whose Min is above its Max, a fractional range or step on an int field, a negative or non-finite step, and a default of the wrong kind or outside the range. It refuses an event or handler whose name is empty, holds a dot or repeats another event's or handler's of the type, and a handler's params on the same terms as fields; an event and a handler may share a name. The registry keeps its own copy of t.
func (r *ComponentRegistry) Register(t ComponentType) error {
	if r == nil {
		return errors.New("sceneformat: register on nil component registry")
	}
	if t.Name == "" {
		return errors.New("sceneformat: component type has an empty name")
	}
	if _, dup := r.types[t.Name]; dup {
		return fmt.Errorf("sceneformat: component type %q is already registered", t.Name)
	}
	owner := fmt.Sprintf("component type %q", t.Name)
	fields, err := normalizeFields(owner, "field", t.Fields)
	if err != nil {
		return err
	}
	ct := &ComponentType{Name: t.Name, Doc: t.Doc, Fields: fields}
	if ct.Events, err = normalizeEvents(owner, t.Events); err != nil {
		return err
	}
	if ct.Handlers, err = normalizeHandlers(owner, t.Handlers); err != nil {
		return err
	}
	if r.types == nil {
		r.types = make(map[string]*ComponentType)
	}
	r.types[t.Name] = ct
	return nil
}

// normalizeFields checks fields, which belong to owner (such as `component type "body"`) and are called noun in errors ("field" or "param"), and returns copies with their defaults normalized and their ranges copied.
func normalizeFields(owner, noun string, fields []Field) ([]Field, error) {
	out := make([]Field, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for i, f := range fields {
		if f.Name == "" {
			return nil, fmt.Errorf("sceneformat: %s %s %d has an empty name", owner, noun, i)
		}
		if _, dup := seen[f.Name]; dup {
			return nil, fmt.Errorf("sceneformat: %s has duplicate %s %q", owner, noun, f.Name)
		}
		seen[f.Name] = struct{}{}
		where := fmt.Sprintf("%s %s %q", owner, noun, f.Name)
		if f.Kind < FieldBool || f.Kind > FieldPolygon {
			return nil, fmt.Errorf("sceneformat: %s has unknown kind %v", where, f.Kind)
		}
		if len(f.Enum) > 0 && f.Kind != FieldString {
			return nil, fmt.Errorf("sceneformat: %s is %v and cannot have an enum", where, f.Kind)
		}
		if f.Min != nil || f.Max != nil {
			if !f.Kind.IsNumeric() {
				return nil, fmt.Errorf("sceneformat: %s is %v and cannot have a range", where, f.Kind)
			}
			if (f.Min != nil && !isFinite64(*f.Min)) || (f.Max != nil && !isFinite64(*f.Max)) {
				return nil, fmt.Errorf("sceneformat: %s range must be finite", where)
			}
			if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
				return nil, fmt.Errorf("sceneformat: %s has min %v above max %v", where, *f.Min, *f.Max)
			}
			if f.Kind == FieldInt && ((f.Min != nil && *f.Min != math.Trunc(*f.Min)) || (f.Max != nil && *f.Max != math.Trunc(*f.Max))) {
				return nil, fmt.Errorf("sceneformat: %s is int and its range must be whole numbers", where)
			}
		}
		if f.Step != 0 {
			if !f.Kind.IsNumeric() {
				return nil, fmt.Errorf("sceneformat: %s is %v and cannot have a step", where, f.Kind)
			}
			if !isFinite64(f.Step) || f.Step < 0 {
				return nil, fmt.Errorf("sceneformat: %s step must be positive and finite", where)
			}
			if f.Kind == FieldInt && f.Step != math.Trunc(f.Step) {
				return nil, fmt.Errorf("sceneformat: %s is int and its step must be a whole number", where)
			}
		}
		nf := f
		if f.Min != nil {
			nf.Min = Float64(*f.Min)
		}
		if f.Max != nil {
			nf.Max = Float64(*f.Max)
		}
		if len(f.Enum) > 0 {
			nf.Enum = slices.Clone(f.Enum)
		}
		def := f.Default
		if def == nil {
			def = zeroValue(f.Kind)
		}
		norm, msg := nf.check(def)
		if msg != "" {
			return nil, fmt.Errorf("sceneformat: %s default: %s", where, msg)
		}
		nf.Default = norm
		out[i] = nf
	}
	return out, nil
}

// MustRegister is Register for package-level setup; it panics when Register returns an error.
func (r *ComponentRegistry) MustRegister(t ComponentType) {
	if err := r.Register(t); err != nil {
		panic(err)
	}
}

// Lookup returns a copy of the component type named name and whether it is registered.
func (r *ComponentRegistry) Lookup(name string) (ComponentType, bool) {
	t := r.lookup(name)
	if t == nil {
		return ComponentType{}, false
	}
	return cloneComponentType(t), true
}

// Names returns the registered type names in ascending order.
func (r *ComponentRegistry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.types))
	for n := range r.types {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// Types returns copies of the registered types ordered by name.
func (r *ComponentRegistry) Types() []ComponentType {
	names := r.Names()
	out := make([]ComponentType, len(names))
	for i, n := range names {
		out[i] = cloneComponentType(r.types[n])
	}
	return out
}

// CheckField checks value against the registered field component.field and returns it normalized to the field's Go type (bool, int, float64 or string). The error names the component and field; it is a *ComponentError without a position.
func (r *ComponentRegistry) CheckField(component, field string, value any) (any, error) {
	t := r.lookup(component)
	if t == nil {
		return nil, &ComponentError{Component: component, Msg: r.unknownTypeMsg(component)}
	}
	f, ok := t.Field(field)
	if !ok {
		return nil, &ComponentError{Component: component, Field: field, Msg: unknownFieldMsg(t, field)}
	}
	norm, msg := f.check(value)
	if msg != "" {
		return nil, &ComponentError{Component: component, Field: field, Msg: msg}
	}
	return norm, nil
}

// CheckComponent checks that value is a mapping whose keys are all registered fields of component with values of the right kind and range. Fields are checked in ascending name order so the first error is deterministic.
func (r *ComponentRegistry) CheckComponent(component string, value any) error {
	t := r.lookup(component)
	if t == nil {
		return &ComponentError{Component: component, Msg: r.unknownTypeMsg(component)}
	}
	m, ok := value.(map[string]any)
	if !ok {
		return &ComponentError{Component: component, Msg: fmt.Sprintf("must be a mapping of fields, got %s", describeValue(value))}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if _, err := r.CheckField(component, k, m[k]); err != nil {
			return err
		}
	}
	return nil
}

// Resolve checks data against component and returns a new map holding every registered field: the value from data where it is set and the registered default otherwise. Use it at runtime to read a component without repeating its defaults; the scene document itself keeps only the fields its author wrote.
func (r *ComponentRegistry) Resolve(component string, data map[string]any) (map[string]any, error) {
	if data == nil {
		data = map[string]any{}
	}
	if err := r.CheckComponent(component, data); err != nil {
		return nil, err
	}
	t := r.lookup(component)
	out := make(map[string]any, len(t.Fields))
	for _, f := range t.Fields {
		if v, ok := data[f.Name]; ok {
			norm, _ := f.check(v)
			out[f.Name] = norm
			continue
		}
		out[f.Name] = cloneFieldDefault(f.Default)
	}
	return out, nil
}

// ValidateComponents checks the components of every object in doc, children included, against reg: unregistered component types, unknown fields, wrong kinds and out-of-range values are refused. Objects are checked in document order and components and fields in ascending name order. The returned error is a *ComponentError naming the object; it carries no line because doc holds none, so prefer ParseWith with ParseOptions.Components when loading files.
func ValidateComponents(doc *SceneDoc, reg *ComponentRegistry) error {
	if doc == nil || reg == nil {
		return nil
	}
	var err error
	for li := range doc.Layers {
		WalkObjects(doc.Layers[li].Objects, func(o, _ *ObjectDoc) bool {
			err = validateObjectComponents(o, reg)
			return err == nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func validateObjectComponents(o *ObjectDoc, reg *ComponentRegistry) error {
	names := make([]string, 0, len(o.Components))
	for n := range o.Components {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		if err := reg.CheckComponent(n, o.Components[n]); err != nil {
			var ce *ComponentError
			if errors.As(err, &ce) {
				ce.Object = o.ID
			}
			return err
		}
	}
	return nil
}

// ComponentError reports a component value that does not match its registered type. File, Line and Column locate it in a scene file when it came from ParseWith; they are empty or zero otherwise.
type ComponentError struct {
	// File is ParseOptions.File, or empty.
	File string
	// Line and Column are the 1-based position of the offending key or value, or 0 when unknown.
	Line, Column int
	// Object is the ID of the object holding the component, or empty when the check was not about a scene object.
	Object string
	// Component is the component type name as written.
	Component string
	// Field is the field name as written, or empty when the error is about the whole component.
	Field string
	// Msg says what is wrong.
	Msg string
}

// Error formats the error as "file:line:col: sceneformat: object "id" component "name" field "f": msg", leaving out the parts that are unknown.
func (e *ComponentError) Error() string {
	var b strings.Builder
	if e.File != "" {
		b.WriteString(e.File)
		if e.Line > 0 {
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(e.Line))
			if e.Column > 0 {
				b.WriteByte(':')
				b.WriteString(strconv.Itoa(e.Column))
			}
		}
		b.WriteString(": ")
	} else if e.Line > 0 {
		fmt.Fprintf(&b, "line %d: ", e.Line)
	}
	b.WriteString("sceneformat:")
	if e.Object != "" {
		fmt.Fprintf(&b, " object %q", e.Object)
	}
	fmt.Fprintf(&b, " component %q", e.Component)
	if e.Field != "" {
		fmt.Fprintf(&b, " field %q", e.Field)
	}
	b.WriteString(": ")
	b.WriteString(e.Msg)
	return b.String()
}

func (r *ComponentRegistry) lookup(name string) *ComponentType {
	if r == nil {
		return nil
	}
	return r.types[name]
}

func (r *ComponentRegistry) unknownTypeMsg(name string) string {
	names := r.Names()
	if len(names) == 0 {
		return "unknown component type (none are registered)"
	}
	return fmt.Sprintf("unknown component type (registered: %s)", strings.Join(names, ", "))
}

func unknownFieldMsg(t *ComponentType, field string) string {
	names := t.FieldNames()
	if len(names) == 0 {
		return "unknown field (the type has no fields)"
	}
	return fmt.Sprintf("unknown field (registered: %s)", strings.Join(names, ", "))
}

func cloneComponentType(t *ComponentType) ComponentType {
	out := ComponentType{Name: t.Name, Doc: t.Doc, Fields: cloneFields(t.Fields), Events: slices.Clone(t.Events)}
	if t.Handlers != nil {
		out.Handlers = make([]Handler, len(t.Handlers))
		for i, h := range t.Handlers {
			out.Handlers[i] = h
			out.Handlers[i].Params = cloneFields(h.Params)
		}
	}
	return out
}

// cloneFields copies fields and their ranges.
func cloneFields(fields []Field) []Field {
	if fields == nil {
		return nil
	}
	out := make([]Field, len(fields))
	for i, f := range fields {
		out[i] = f
		if f.Min != nil {
			out[i].Min = Float64(*f.Min)
		}
		if f.Max != nil {
			out[i].Max = Float64(*f.Max)
		}
		if len(f.Enum) > 0 {
			out[i].Enum = slices.Clone(f.Enum)
		}
		out[i].Default = cloneFieldDefault(f.Default)
	}
	return out
}

func zeroValue(k FieldKind) any {
	switch k {
	case FieldBool:
		return false
	case FieldInt:
		return 0
	case FieldFloat:
		return 0.0
	case FieldStringList:
		return []string(nil)
	case FieldPointList:
		return [][2]float64(nil)
	case FieldPoint:
		return Point{}
	case FieldRect:
		return Rect{}
	case FieldCircle:
		return Circle{}
	case FieldPolygon:
		return []Point(nil)
	default:
		return ""
	}
}

// check normalizes v to the field's Go type and returns a non-empty message when it is the wrong kind or out of range.
func (f Field) check(v any) (any, string) {
	switch f.Kind {
	case FieldBool:
		b, ok := v.(bool)
		if !ok {
			return nil, "want bool, got " + describeValue(v)
		}
		return b, ""
	case FieldString:
		s, ok := v.(string)
		if !ok {
			return nil, "want string, got " + describeValue(v)
		}
		if len(f.Enum) > 0 && !slices.Contains(f.Enum, s) {
			return nil, fmt.Sprintf("must be one of %s, got %q", strings.Join(f.Enum, ", "), s)
		}
		return s, ""
	case FieldStringList:
		if v == nil {
			return []string(nil), ""
		}
		switch xs := v.(type) {
		case []string:
			return slices.Clone(xs), ""
		case []any:
			out := make([]string, len(xs))
			for i, elem := range xs {
				s, ok := elem.(string)
				if !ok {
					return nil, fmt.Sprintf("element %d: want string, got %s", i, describeValue(elem))
				}
				out[i] = s
			}
			return out, ""
		default:
			return nil, "want list of strings, got " + describeValue(v)
		}
	case FieldPointList:
		pts, msg := checkPointList(v)
		if msg != "" {
			return nil, msg
		}
		return pts, ""
	case FieldInt:
		n, ok := toInt(v)
		if !ok {
			return nil, "want int, got " + describeValue(v)
		}
		if msg := f.checkRange(float64(n)); msg != "" {
			return nil, msg
		}
		return n, ""
	case FieldFloat:
		x, ok := toFloat(v)
		if !ok {
			return nil, "want float, got " + describeValue(v)
		}
		if !isFinite64(x) {
			return nil, fmt.Sprintf("value %v must be finite", x)
		}
		if msg := f.checkRange(x); msg != "" {
			return nil, msg
		}
		return x, ""
	case FieldPoint:
		p, msg := checkPoint(v)
		if msg != "" {
			return nil, msg
		}
		return p, ""
	case FieldRect:
		r, msg := checkRect(v)
		if msg != "" {
			return nil, msg
		}
		return r, ""
	case FieldCircle:
		c, msg := checkCircle(v)
		if msg != "" {
			return nil, msg
		}
		return c, ""
	case FieldPolygon:
		pts, msg := checkPolygon(v)
		if msg != "" {
			return nil, msg
		}
		return pts, ""
	default:
		return nil, fmt.Sprintf("unknown kind %v", f.Kind)
	}
}

func (f Field) checkRange(x float64) string {
	if f.Min != nil && x < *f.Min {
		return fmt.Sprintf("value %v is below the minimum %v", x, *f.Min)
	}
	if f.Max != nil && x > *f.Max {
		return fmt.Sprintf("value %v is above the maximum %v", x, *f.Max)
	}
	return ""
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		if n < math.MinInt || n > math.MaxInt {
			return 0, false
		}
		return int(n), true
	case uint:
		if uint64(n) > math.MaxInt {
			return 0, false
		}
		return int(n), true
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		if uint64(n) > math.MaxInt {
			return 0, false
		}
		return int(n), true
	case uint64:
		if n > math.MaxInt {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	}
	if i, ok := toInt(v); ok {
		return float64(i), true
	}
	switch n := v.(type) {
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}

func describeValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool " + strconv.FormatBool(x)
	case string:
		return strconv.Quote(x)
	case float32, float64:
		return fmt.Sprintf("float %v", x)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("int %v", x)
	case map[string]any:
		return "a mapping"
	case []any:
		return "a sequence"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func isFinite64(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// checkComponentNodes walks the YAML node tree beside the decoded doc so a component error can point at the key or value that caused it.
func checkComponentNodes(root *yaml.Node, doc *SceneDoc, reg *ComponentRegistry) error {
	n := resolveAlias(root)
	if n != nil && n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = resolveAlias(n.Content[0])
	}
	layers := mappingValue(n, "layers")
	if layers == nil || layers.Kind != yaml.SequenceNode {
		return nil
	}
	for li, ln := range layers.Content {
		if li >= len(doc.Layers) {
			break
		}
		if err := checkObjectNodes(mappingValue(resolveAlias(ln), "objects"), doc.Layers[li].Objects, reg); err != nil {
			return err
		}
	}
	return nil
}

// checkObjectNodes checks the components of objs and, recursively, their children against the matching sequence node.
func checkObjectNodes(seq *yaml.Node, objs []ObjectDoc, reg *ComponentRegistry) error {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	for oi, on := range seq.Content {
		if oi >= len(objs) {
			break
		}
		obj := &objs[oi]
		on = resolveAlias(on)
		if comps := mappingValue(on, "components"); comps != nil && comps.Kind == yaml.MappingNode {
			if err := checkComponentsNode(comps, obj, reg); err != nil {
				return err
			}
		}
		if err := checkObjectNodes(mappingValue(on, "children"), obj.Children, reg); err != nil {
			return err
		}
	}
	return nil
}

func checkComponentsNode(comps *yaml.Node, obj *ObjectDoc, reg *ComponentRegistry) error {
	at := func(n *yaml.Node, component, field, msg string) error {
		return &ComponentError{Line: n.Line, Column: n.Column, Object: obj.ID, Component: component, Field: field, Msg: msg}
	}
	for i := 0; i+1 < len(comps.Content); i += 2 {
		k, v := comps.Content[i], comps.Content[i+1]
		if k.Tag == "!!merge" {
			continue
		}
		name := k.Value
		t := reg.lookup(name)
		if t == nil {
			return at(k, name, "", reg.unknownTypeMsg(name))
		}
		fields, ok := obj.Components[name].(map[string]any)
		vn := resolveAlias(v)
		if !ok || vn.Kind != yaml.MappingNode {
			return at(v, name, "", fmt.Sprintf("must be a mapping of fields, got %s", describeValue(obj.Components[name])))
		}
		for j := 0; j+1 < len(vn.Content); j += 2 {
			fk, fv := vn.Content[j], vn.Content[j+1]
			if fk.Tag == "!!merge" {
				continue
			}
			f, ok := t.Field(fk.Value)
			if !ok {
				return at(fk, name, fk.Value, unknownFieldMsg(t, fk.Value))
			}
			if _, msg := f.check(fields[fk.Value]); msg != "" {
				return at(fv, name, fk.Value, msg)
			}
		}
	}
	return nil
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	for depth := 0; n != nil && n.Kind == yaml.AliasNode && depth < 64; depth++ {
		n = n.Alias
	}
	return n
}

func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key && n.Content[i].Tag != "!!merge" {
			return resolveAlias(n.Content[i+1])
		}
	}
	return nil
}
