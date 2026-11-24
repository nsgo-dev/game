package mcp

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/nsgo/automation"
	"github.com/nsgo-dev/nsgo/core"
)

// Tool names registered by Bridge with github.com/nsgo-dev/nsgo/automation.
const (
	// ToolScenesList lists active scenes on the stack and any registered named scenes.
	ToolScenesList = "game_scenes_list"
	// ToolLayersList lists layers in a scene in draw order.
	ToolLayersList = "game_layers_list"
	// ToolEntitiesList lists live entities with stable generational references.
	ToolEntitiesList = "game_entities_list"
	// ToolEntityInspect reads a single live entity's metadata, hierarchy and component values.
	ToolEntityInspect = "game_entity_inspect"
	// ToolComponentGet reads one component's value on a live entity.
	ToolComponentGet = "game_component_get"
)

var (
	// ErrNilBridge reports an operation attempted on a nil Bridge.
	ErrNilBridge = errors.New("mcp: nil bridge")
	// ErrAlreadyAttached reports calling Bridge.Attach on a Bridge that is already attached to a live scope.
	ErrAlreadyAttached = errors.New("mcp: bridge is already attached")
	// ErrEmptyComponentName reports registering a component inspector with an empty name.
	ErrEmptyComponentName = errors.New("mcp: empty component name")
	// ErrDuplicateComponent reports registering a component name that is already registered on the Bridge.
	ErrDuplicateComponent = errors.New("mcp: duplicate component registration")
)

// Config names the parts of a running game that MCP tools inspect and drive.
// Every field is optional; tools whose subsystem is unconfigured still register and return either an empty list or an explanatory error when called.
type Config struct {
	// Scenes is the game's scene stack manager. When set, game_scenes_list reports the active stack and transition state, and game_layers_list defaults to the current scene.
	Scenes *scene.Manager

	// Scene is a standalone active scene for games that do not use a scene.Manager.
	Scene *scene.Scene

	// SceneFunc dynamically resolves the active scene on each tool call (for example, when restarting a level allocates a new *scene.Scene). When non-nil and returning a non-nil scene, it overrides Scene.
	SceneFunc func() *scene.Scene

	// SceneName maps a scene.Controller on Scenes's stack to a human-readable name such as "title", "gameplay" or "pause". When nil or returning "", stack entries receive stable fallback names ("scene:0", "scene:1", ...).
	SceneName func(*scene.Controller) string

	// NamedScenes registers additional named scenes (active or inactive) so game_scenes_list lists them and game_layers_list can inspect them by name.
	NamedScenes map[string]*scene.Scene

	// World is the game's ECS entity world for entity and component inspection tools.
	World *entities.World

	// WorldFunc dynamically resolves the active entity world on each tool call (for example, when switching levels replaces the *entities.World). When non-nil and returning a non-nil world, it overrides World.
	WorldFunc func() *entities.World

	// EntityName returns a human-readable name for a live entity (such as "player" or "slime_1"). When nil or returning "", inspection falls back to sceneformat.ObjectRef.ID if attached.
	EntityName func(entities.Entity) string

	// EntityTags returns labels/tags attached to a live entity (such as ["enemy", "boss"]).
	EntityTags func(entities.Entity) []string

	// EntityLayer returns the scene layer name an entity belongs to. When nil or returning "", inspection falls back to sceneformat.ObjectRef.Layer if attached.
	EntityLayer func(entities.Entity) string
	// Loop is the game's fixed-step loop for pause, resume, synchronous stepping and time-scale tools.
	Loop *game.Loop

	// LoopFunc dynamically resolves the active game loop on each tool call. When non-nil and returning a non-nil loop, it overrides Loop.
	LoopFunc func() *game.Loop

	// Random is a deterministic random stream re-seeded by game_loop_seed.
	Random *random.Stream

	// RandomStreams is a named deterministic random stream manager re-seeded by game_loop_seed.
	RandomStreams *random.Streams

	// SetSeed is a custom callback invoked by game_loop_seed to apply a deterministic random seed.
	SetSeed func(uint64)

	// Actions is the game's input action map driven by game_input_action, game_input_axis, game_input_actions_list and game_input_pointer.
	Actions *input.Actions

	// ActionsFunc dynamically resolves the active input.Actions on each tool call. When non-nil and returning a non-nil *input.Actions, it overrides Actions.
	ActionsFunc func() *input.Actions

	// Viewport maps virtual game coordinates to view coordinates for game_input_pointer.
	Viewport *screen.Viewport

	// ViewportFunc dynamically resolves the active screen.Viewport on each tool call. When non-nil and returning a viewport with positive dimensions, it overrides Viewport.
	ViewportFunc func() screen.Viewport

	// ViewSizeFunc returns the current canvas/view size in points for Viewport.Layout and Viewport.GameToView.
	// When nil or returning a non-positive size, pointer mapping falls back to the attached scope's Env().Window(), and then to the viewport's virtual size.
	ViewSizeFunc func() core.Size

	// OnPointer is called whenever game_input_pointer injects a pointer press, release or move, after viewport mapping and scene hit testing.
	OnPointer func(PointerEvent)

	// Console is the game's debug console for tunable inspection/mutation and command execution tools.
	Console *debug.Console

	// ConsoleFunc dynamically resolves the active debug console on each tool call. When non-nil and returning a non-nil console, it overrides Console.
	ConsoleFunc func() *debug.Console
}

type compTypeKey any

type compTypeTag[T any] struct{}

func compTypeKeyOf[T any]() compTypeKey {
	return compTypeTag[T]{}
}

type componentEntry struct {
	name    string
	typeKey compTypeKey
	has     func(w *entities.World, e entities.Entity) bool
	read    func(w *entities.World, e entities.Entity) (any, bool, error)
}

// Bridge exposes a running game's subsystems to nsgo mcp via automation.Register.
//
// Create a Bridge with New or Attach, optionally register additional component inspectors with RegisterComponent or RegisterComponentFunc, and attach it to a core.Scope on the engine goroutine. Disposing the scope or calling Detach unregisters every tool automatically.
type Bridge struct {
	cfg Config
	sub *core.Scope

	components    map[string]componentEntry
	compOrder     []string
	compByType    map[compTypeKey]string
	pointerButton core.MouseButton
}

// New constructs a Bridge for cfg without registering its tools yet.
// Call Bridge.Attach on the engine goroutine to register its tools on a core.Scope.
func New(cfg Config) *Bridge {
	if len(cfg.NamedScenes) > 0 {
		copied := make(map[string]*scene.Scene, len(cfg.NamedScenes))
		for k, v := range cfg.NamedScenes {
			copied[k] = v
		}
		cfg.NamedScenes = copied
	}
	return &Bridge{cfg: cfg}
}

// Attach constructs a Bridge for cfg and registers its tools on cx until cx is disposed.
// Call it on the engine goroutine.
func Attach(cx *core.Scope, cfg Config) (*Bridge, error) {
	b := New(cfg)
	if err := b.Attach(cx); err != nil {
		return nil, err
	}
	return b, nil
}

// Attach registers all of b's tools on a child scope of cx via automation.Register.
// If any registration fails, all tools registered by this call are cleaned up before returning the error.
// Call Attach on the engine goroutine.
func (b *Bridge) Attach(cx *core.Scope) error {
	if b == nil {
		return ErrNilBridge
	}
	if cx == nil || cx.Disposed() {
		return errors.New("mcp: attach needs a live scope")
	}
	if b.Attached() {
		return ErrAlreadyAttached
	}
	var sub *core.Scope
	cx.Run(func() {
		sub = cx.Child()
	})
	if sub == nil {
		return errors.New("mcp: failed to create child scope")
	}
	for _, tool := range b.Tools() {
		if err := automation.Register(sub, tool); err != nil {
			sub.Dispose()
			return err
		}
	}
	b.sub = sub
	sub.Run(func() {
		sub.Cleanup(func() {
			if b.sub == sub {
				b.sub = nil
			}
		})
	})
	return nil
}

// Detach unregisters all tools registered by b on its attached scope.
// Calling Detach when b is nil or not attached is a no-op.
func (b *Bridge) Detach() {
	if b == nil || b.sub == nil {
		return
	}
	sub := b.sub
	b.sub = nil
	b.pointerButton = core.MouseButtonNone
	if !sub.Disposed() {
		sub.Dispose()
	}
}

// Attached reports whether b is currently attached to a live scope.
func (b *Bridge) Attached() bool {
	return b != nil && b.sub != nil && !b.sub.Disposed()
}

// SetWorld replaces the static World configured on b.
// If Config.WorldFunc is set and returns a non-nil world, WorldFunc continues to take precedence.
func (b *Bridge) SetWorld(w *entities.World) {
	if b == nil {
		return
	}
	b.cfg.World = w
}

// SetScene replaces the static standalone Scene configured on b.
// If Config.Scenes has an active scene or Config.SceneFunc returns a non-nil scene, those take precedence.
func (b *Bridge) SetScene(s *scene.Scene) {
	if b == nil {
		return
	}
	b.cfg.Scene = s
}

// SetActions replaces the static Actions configured on b.
// If Config.ActionsFunc is set and returns a non-nil *input.Actions, ActionsFunc continues to take precedence.
func (b *Bridge) SetActions(a *input.Actions) {
	if b == nil {
		return
	}
	b.cfg.Actions = a
}

// SetViewport replaces the static Viewport configured on b.
// If Config.ViewportFunc is set and returns a viewport with positive dimensions, ViewportFunc continues to take precedence.
func (b *Bridge) SetViewport(vp *screen.Viewport) {
	if b == nil {
		return
	}
	b.cfg.Viewport = vp
}

// SetConsole replaces the static debug Console configured on b. If Config.ConsoleFunc is set and returns a non-nil console, ConsoleFunc continues to take precedence.
func (b *Bridge) SetConsole(c *debug.Console) {
	if b == nil {
		return
	}
	b.cfg.Console = c
}

// Tools returns the full set of automation.Tool definitions owned by b, assembled from each tool group.
func (b *Bridge) Tools() []automation.Tool {
	if b == nil {
		return nil
	}
	var out []automation.Tool
	out = append(out, b.inspectTools()...)
	out = append(out, b.loopTools()...)
	out = append(out, b.inputTools()...)
	out = append(out, b.consoleTools()...)
	return out
}

// RegisterComponent registers a typed component inspector for component type T under name on b.
//
// Use this to expose runtime components that are not registered with entities.RegisterCodec on the World, or to override how a component is named or inspected.
func RegisterComponent[T any](b *Bridge, name string) error {
	if b == nil {
		return ErrNilBridge
	}
	if name == "" {
		return ErrEmptyComponentName
	}
	if _, dup := b.components[name]; dup {
		return fmt.Errorf("mcp: component %q: %w", name, ErrDuplicateComponent)
	}
	k := compTypeKeyOf[T]()
	if b.components == nil {
		b.components = make(map[string]componentEntry)
		b.compByType = make(map[compTypeKey]string)
	}
	b.components[name] = componentEntry{
		name:    name,
		typeKey: k,
		has: func(w *entities.World, e entities.Entity) bool {
			return entities.Has[T](w, e)
		},
		read: func(w *entities.World, e entities.Entity) (any, bool, error) {
			v, ok := entities.Get[T](w, e)
			if !ok {
				return nil, false, nil
			}
			return *v, true, nil
		},
	}
	b.compOrder = append(b.compOrder, name)
	slices.Sort(b.compOrder)
	b.compByType[k] = name
	return nil
}

// RegisterComponentFunc registers a custom component reader under name on b.
// read returns (value, true, nil) when entity e carries the component, (nil, false, nil) when absent, or an error.
func RegisterComponentFunc(b *Bridge, name string, read func(w *entities.World, e entities.Entity) (any, bool, error)) error {
	if b == nil {
		return ErrNilBridge
	}
	if name == "" {
		return ErrEmptyComponentName
	}
	if read == nil {
		return errors.New("mcp: nil component reader")
	}
	if _, dup := b.components[name]; dup {
		return fmt.Errorf("mcp: component %q: %w", name, ErrDuplicateComponent)
	}
	if b.components == nil {
		b.components = make(map[string]componentEntry)
		b.compByType = make(map[compTypeKey]string)
	}
	b.components[name] = componentEntry{
		name: name,
		read: read,
	}
	b.compOrder = append(b.compOrder, name)
	slices.Sort(b.compOrder)
	return nil
}

func (b *Bridge) activeWorld() *entities.World {
	if b == nil {
		return nil
	}
	if b.cfg.WorldFunc != nil {
		if w := b.cfg.WorldFunc(); w != nil {
			return w
		}
	}
	return b.cfg.World
}

func (b *Bridge) standaloneScene() *scene.Scene {
	if b == nil {
		return nil
	}
	if b.cfg.SceneFunc != nil {
		if s := b.cfg.SceneFunc(); s != nil {
			return s
		}
	}
	return b.cfg.Scene
}

// decodeArgs strictly decodes a JSON object argument payload into T, refusing unknown fields and trailing data.
func decodeArgs[T any](raw json.RawMessage) (T, error) {
	var zero T
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return zero, nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&zero); err != nil {
		return zero, fmt.Errorf("mcp: invalid arguments: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return zero, errors.New("mcp: invalid arguments: unexpected trailing data")
	}
	return zero, nil
}

const maxEncodeDepth = 32

var (
	jsonMarshalerType = reflect.TypeFor[json.Marshaler]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
)

// encodeValue converts a Go component value to a JSON-safe json.RawMessage.
// It uses encoding/json directly on the fast path, and falls back to a safe reflection sanitizer when encoding/json fails (for example on NaN/Inf floats, callback fields, channels, or cyclic pointers).
func encodeValue(v any) (json.RawMessage, error) {
	if raw, ok := v.(json.RawMessage); ok {
		if len(raw) == 0 {
			return json.RawMessage("null"), nil
		}
		if json.Valid(raw) {
			return bytes.Clone(raw), nil
		}
		return nil, errors.New("mcp: component returned invalid JSON")
	}
	if b, err := json.Marshal(v); err == nil {
		return json.RawMessage(b), nil
	}
	seen := make(map[visitKey]bool)
	sanitized := sanitizeValue(reflect.ValueOf(v), 0, seen)
	b, err := json.Marshal(sanitized)
	if err != nil {
		return nil, fmt.Errorf("mcp: encode component value: %w", err)
	}
	return json.RawMessage(b), nil
}

type visitKey struct {
	ptr uintptr
	typ reflect.Type
}

func sanitizeValue(rv reflect.Value, depth int, seen map[visitKey]bool) any {
	if !rv.IsValid() {
		return nil
	}
	if depth >= maxEncodeDepth {
		return "<max-depth>"
	}
	// Honor custom marshalers when they succeed cleanly.
	if rv.CanInterface() {
		t := rv.Type()
		if t.Implements(jsonMarshalerType) || t.Implements(textMarshalerType) {
			if b, err := json.Marshal(rv.Interface()); err == nil {
				return json.RawMessage(b)
			}
		}
	}

	switch rv.Kind() {
	case reflect.Bool:
		return rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint()
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		switch {
		case math.IsNaN(f):
			return "NaN"
		case math.IsInf(f, 1):
			return "+Inf"
		case math.IsInf(f, -1):
			return "-Inf"
		default:
			return f
		}
	case reflect.Complex64, reflect.Complex128:
		c := rv.Complex()
		return fmt.Sprintf("(%v+%vi)", real(c), imag(c))
	case reflect.String:
		return rv.String()
	case reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return sanitizeValue(rv.Elem(), depth+1, seen)
	case reflect.Pointer:
		if rv.IsNil() {
			return nil
		}
		key := visitKey{ptr: rv.Pointer(), typ: rv.Type()}
		if seen[key] {
			return "<cycle>"
		}
		seen[key] = true
		defer delete(seen, key)
		return sanitizeValue(rv.Elem(), depth+1, seen)
	case reflect.Slice:
		if rv.IsNil() {
			return nil
		}
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			if b, err := json.Marshal(rv.Bytes()); err == nil {
				return json.RawMessage(b)
			}
		}
		key := visitKey{ptr: rv.Pointer(), typ: rv.Type()}
		if rv.Len() > 0 {
			if seen[key] {
				return "<cycle>"
			}
			seen[key] = true
			defer delete(seen, key)
		}
		out := make([]any, rv.Len())
		for i := range rv.Len() {
			out[i] = sanitizeValue(rv.Index(i), depth+1, seen)
		}
		return out
	case reflect.Array:
		out := make([]any, rv.Len())
		for i := range rv.Len() {
			out[i] = sanitizeValue(rv.Index(i), depth+1, seen)
		}
		return out
	case reflect.Map:
		if rv.IsNil() {
			return nil
		}
		key := visitKey{ptr: rv.Pointer(), typ: rv.Type()}
		if seen[key] {
			return "<cycle>"
		}
		seen[key] = true
		defer delete(seen, key)
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			k := formatMapKey(iter.Key())
			out[k] = sanitizeValue(iter.Value(), depth+1, seen)
		}
		return out
	case reflect.Struct:
		out := make(map[string]any)
		collectStructFields(out, rv, depth, seen)
		return out
	case reflect.Func:
		if rv.IsNil() {
			return nil
		}
		return "<func>"
	case reflect.Chan:
		if rv.IsNil() {
			return nil
		}
		return "<chan>"
	case reflect.UnsafePointer:
		return fmt.Sprintf("0x%x", rv.Pointer())
	default:
		return fmt.Sprintf("<%s>", rv.Kind())
	}
}

func collectStructFields(dst map[string]any, rv reflect.Value, depth int, seen map[visitKey]bool) {
	rt := rv.Type()
	for i := range rt.NumField() {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		name, omitempty, skip := parseJSONTag(sf)
		if skip {
			continue
		}
		fv := rv.Field(i)
		// Flatten anonymous embedded structs when no explicit JSON tag name is given.
		if sf.Anonymous && sf.Tag.Get("json") == "" {
			embedded := fv
			if embedded.Kind() == reflect.Pointer {
				if embedded.IsNil() {
					continue
				}
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				collectStructFields(dst, embedded, depth+1, seen)
				continue
			}
		}
		if omitempty && isJSONEmpty(fv) {
			continue
		}
		dst[name] = sanitizeValue(fv, depth+1, seen)
	}
}

func parseJSONTag(sf reflect.StructField) (name string, omitempty, skip bool) {
	tag := sf.Tag.Get("json")
	if tag == "-" {
		return "", false, true
	}
	if tag == "" {
		return sf.Name, false, false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = sf.Name
	}
	for _, opt := range parts[1:] {
		if opt == "omitempty" || opt == "omitzero" {
			omitempty = true
		}
	}
	return name, omitempty, false
}

func isJSONEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer, reflect.Func, reflect.Chan:
		return v.IsNil()
	default:
		return v.IsZero()
	}
}

func formatMapKey(k reflect.Value) string {
	switch k.Kind() {
	case reflect.String:
		return k.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(k.Uint(), 10)
	default:
		if k.CanInterface() {
			if tm, ok := k.Interface().(encoding.TextMarshaler); ok {
				if b, err := tm.MarshalText(); err == nil {
					return string(b)
				}
			}
			return fmt.Sprint(k.Interface())
		}
		return k.String()
	}
}
