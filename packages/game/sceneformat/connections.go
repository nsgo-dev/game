package sceneformat

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/loaderr"
)

// ConnectionDoc declares one event-to-handler connection between objects in a version 2 scene document, as Godot's signal connections do in .tscn files.
type ConnectionDoc struct {
	// From is the ID of the object that emits Event (such as "button" or "room_a/button").
	From string `yaml:"from" schema:"required,minLength=1"`
	// Event is the name of the event emitted by From (such as "pressed").
	Event string `yaml:"event" schema:"required,minLength=1"`
	// To is the ID of the object whose handler runs when From emits Event (such as "door" or "room_a/door").
	To string `yaml:"to" schema:"required,minLength=1"`
	// Handler is the name of the handler invoked on To (such as "open").
	Handler string `yaml:"handler" schema:"required,minLength=1"`
	// Args holds optional static arguments declared in YAML and passed to Handler alongside any runtime payload.
	Args map[string]any `yaml:"args,omitempty"`
	// Once, when true, disconnects this connection after the first time it fires.
	Once bool `yaml:"once,omitempty"`
}

// Clone returns a deep copy of c, copying Args so mutating the copy never mutates c.
func (c ConnectionDoc) Clone() ConnectionDoc {
	c.Args = cloneAnyMap(c.Args)
	return c
}

// EventType declares a named event that scene objects may emit.
type EventType struct {
	// Name is the event name written in ConnectionDoc.Event, such as "pressed". It must be non-empty and unique within an EventRegistry.
	Name string
	// Component, when non-empty, requires the source object (ConnectionDoc.From) to carry a component of this name when the scene is validated against the registry.
	Component string
	// Doc is a one-line description shown by tools and error messages.
	Doc string
}

// EventCall carries the context of one delivered event connection to a registered handler.
type EventCall struct {
	// Bus is the EventBus delivering this event; a handler may emit or enqueue follow-up events on it.
	Bus *EventBus
	// Source is the ID of the object that emitted the event.
	Source string
	// Event is the name of the emitted event.
	Event string
	// Target is the ID of the object receiving the event.
	Target string
	// Handler is the name of the invoked handler.
	Handler string
	// Args holds the connection's resolved static arguments (with HandlerType.Args defaults filled in and values normalized to bool, int, float64 or string), or nil when the handler declares no args. Each delivery receives a fresh copy.
	Args map[string]any
	// Payload is the runtime value passed to Emit, EmitEntity, Enqueue or EnqueueEntity, or nil.
	Payload any
	// World is the entities.World when the bus was bound to spawned entities, or nil otherwise.
	World *entities.World
	// SourceEntity and TargetEntity are the spawned entities of Source and Target when World is non-nil.
	SourceEntity entities.Entity
	TargetEntity entities.Entity
	// Scene is the BuiltScene when the bus was bound to a BuiltScene, or nil otherwise.
	Scene *BuiltScene
	// SourceObject and TargetObject are the BuiltObjects of Source and Target when Scene is non-nil.
	SourceObject *BuiltObject
	TargetObject *BuiltObject
}

// HandlerType declares a named handler that scene connections may invoke on target objects.
type HandlerType struct {
	// Name is the handler name written in ConnectionDoc.Handler, such as "open". It must be non-empty and unique within an EventRegistry.
	Name string
	// Component, when non-empty, requires the target object (ConnectionDoc.To) to carry a component of this name when the scene is validated against the registry.
	Component string
	// Doc is a one-line description shown by tools and error messages.
	Doc string
	// Args declares the optional static arguments a connection may bind in YAML, checked with the same Field rules as ComponentType.Fields.
	Args []Field
	// Func is invoked when a connected event fires. It may be nil in a validation-only registry; binding a scene that connects to a handler with a nil Func returns an error.
	Func func(EventCall)
}

// Arg returns the declared argument named name and whether the handler has it.
func (h *HandlerType) Arg(name string) (Field, bool) {
	if h == nil {
		return Field{}, false
	}
	for _, f := range h.Args {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// ArgNames returns the handler's declared argument names in declaration order.
func (h *HandlerType) ArgNames() []string {
	if h == nil {
		return nil
	}
	names := make([]string, len(h.Args))
	for i, f := range h.Args {
		names[i] = f.Name
	}
	return names
}

// EventRegistry holds the event and handler types that scene files may connect in their connections section. Types are declared explicitly in Go without reflection. Register every event and handler before sharing the registry; lookups and binding are safe concurrently once registration is done, while RegisterEvent and RegisterHandler are not.
type EventRegistry struct {
	events   map[string]*EventType
	handlers map[string]*HandlerType
}

// NewEventRegistry returns an empty event registry.
func NewEventRegistry() *EventRegistry {
	return &EventRegistry{
		events:   make(map[string]*EventType),
		handlers: make(map[string]*HandlerType),
	}
}

// RegisterEvent adds e to the registry. It refuses a nil registry and an empty or duplicate event name.
func (r *EventRegistry) RegisterEvent(e EventType) error {
	if r == nil {
		return errors.New("sceneformat: register event on nil event registry")
	}
	if e.Name == "" {
		return errors.New("sceneformat: event type has an empty name")
	}
	if _, dup := r.events[e.Name]; dup {
		return fmt.Errorf("sceneformat: event %q is already registered", e.Name)
	}
	if r.events == nil {
		r.events = make(map[string]*EventType)
	}
	cp := e
	r.events[e.Name] = &cp
	return nil
}

// MustRegisterEvent is RegisterEvent for package-level setup; it panics when RegisterEvent returns an error.
func (r *EventRegistry) MustRegisterEvent(e EventType) {
	if err := r.RegisterEvent(e); err != nil {
		panic(err)
	}
}

// RegisterHandler adds h to the registry. It refuses a nil registry, an empty or duplicate handler name, and invalid Args field definitions (using the same rules as ComponentRegistry.Register).
func (r *EventRegistry) RegisterHandler(h HandlerType) error {
	if r == nil {
		return errors.New("sceneformat: register handler on nil event registry")
	}
	if h.Name == "" {
		return errors.New("sceneformat: handler type has an empty name")
	}
	if _, dup := r.handlers[h.Name]; dup {
		return fmt.Errorf("sceneformat: handler %q is already registered", h.Name)
	}
	args, err := validateFields(fmt.Sprintf("handler %q", h.Name), "arg", h.Args)
	if err != nil {
		return err
	}
	if r.handlers == nil {
		r.handlers = make(map[string]*HandlerType)
	}
	r.handlers[h.Name] = &HandlerType{
		Name:      h.Name,
		Component: h.Component,
		Doc:       h.Doc,
		Args:      args,
		Func:      h.Func,
	}
	return nil
}

// MustRegisterHandler is RegisterHandler for package-level setup; it panics when RegisterHandler returns an error.
func (r *EventRegistry) MustRegisterHandler(h HandlerType) {
	if err := r.RegisterHandler(h); err != nil {
		panic(err)
	}
}

// LookupEvent returns a copy of the registered event named name and whether it exists.
func (r *EventRegistry) LookupEvent(name string) (EventType, bool) {
	if r == nil {
		return EventType{}, false
	}
	e, ok := r.events[name]
	if !ok {
		return EventType{}, false
	}
	return *e, true
}

// LookupHandler returns a copy of the registered handler named name and whether it exists.
func (r *EventRegistry) LookupHandler(name string) (HandlerType, bool) {
	if r == nil {
		return HandlerType{}, false
	}
	h, ok := r.handlers[name]
	if !ok {
		return HandlerType{}, false
	}
	return cloneHandlerType(h), true
}

// EventNames returns the registered event names in ascending order.
func (r *EventRegistry) EventNames() []string {
	if r == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(r.events))
}

// HandlerNames returns the registered handler names in ascending order.
func (r *EventRegistry) HandlerNames() []string {
	if r == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(r.handlers))
}

// Events returns copies of the registered events ordered by name.
func (r *EventRegistry) Events() []EventType {
	names := r.EventNames()
	out := make([]EventType, len(names))
	for i, n := range names {
		out[i] = *r.events[n]
	}
	return out
}

// Handlers returns copies of the registered handlers ordered by name.
func (r *EventRegistry) Handlers() []HandlerType {
	names := r.HandlerNames()
	out := make([]HandlerType, len(names))
	for i, n := range names {
		out[i] = cloneHandlerType(r.handlers[n])
	}
	return out
}

func cloneHandlerType(h *HandlerType) HandlerType {
	return HandlerType{
		Name:      h.Name,
		Component: h.Component,
		Doc:       h.Doc,
		Args:      cloneFields(h.Args),
		Func:      h.Func,
	}
}

// ValidateConnections checks every connection in doc against reg: each event and handler must be registered, each arg must match a declared field of the handler (kind and range), and when an event or handler requires a component (EventType.Component, HandlerType.Component), the referenced object must carry it whenever that object is present in doc. Errors are *loaderr.Error values wrapping a *FieldError naming the offending field.
func ValidateConnections(doc *SceneDoc, reg *EventRegistry) error {
	if doc == nil || reg == nil || len(doc.Connections) == 0 {
		return nil
	}
	byID := make(map[string]*ObjectDoc)
	for li := range doc.Layers {
		indexObjects(doc.Layers[li].Objects, byID)
	}
	for i := range doc.Connections {
		if err := validateConnectionWithRegistry(i, &doc.Connections[i], byID, reg); err != nil {
			return err
		}
	}
	return nil
}

func validateConnectionWithRegistry(i int, c *ConnectionDoc, byID map[string]*ObjectDoc, reg *EventRegistry) error {
	idx := strconv.Itoa(i)
	ev, ok := reg.events[c.Event]
	if !ok {
		names := reg.EventNames()
		var expected, fix, msg string
		if len(names) == 0 {
			msg = fmt.Sprintf("sceneformat: connection %d: unknown event %q (no events are registered)", i, c.Event)
			fix = fmt.Sprintf("register event %q on the EventRegistry before loading the scene", c.Event)
		} else {
			list := strings.Join(names, ", ")
			msg = fmt.Sprintf("sceneformat: connection %d: unknown event %q (registered: %s)", i, c.Event, list)
			expected = "one of: " + list
			fix = "use a registered event name or register it on the EventRegistry"
		}
		fe := fieldErr([]string{"connections", idx, "event"}, "%s", msg)
		return &loaderr.Error{
			Field:    loaderr.Path("connections", i, "event"),
			Problem:  msg,
			Expected: expected,
			Fix:      fix,
			Err:      fe,
		}
	}

	h, ok := reg.handlers[c.Handler]
	if !ok {
		names := reg.HandlerNames()
		var expected, fix, msg string
		if len(names) == 0 {
			msg = fmt.Sprintf("sceneformat: connection %d: unknown handler %q (no handlers are registered)", i, c.Handler)
			fix = fmt.Sprintf("register handler %q on the EventRegistry before loading the scene", c.Handler)
		} else {
			list := strings.Join(names, ", ")
			msg = fmt.Sprintf("sceneformat: connection %d: unknown handler %q (registered: %s)", i, c.Handler, list)
			expected = "one of: " + list
			fix = "use a registered handler name or register it on the EventRegistry"
		}
		fe := fieldErr([]string{"connections", idx, "handler"}, "%s", msg)
		return &loaderr.Error{
			Field:    loaderr.Path("connections", i, "handler"),
			Problem:  msg,
			Expected: expected,
			Fix:      fix,
			Err:      fe,
		}
	}

	if ev.Component != "" {
		if fromObj, present := byID[c.From]; present {
			if _, has := fromObj.Components[ev.Component]; !has {
				msg := fmt.Sprintf("sceneformat: connection %d: source object %q lacks component %q required by event %q", i, c.From, ev.Component, c.Event)
				fe := fieldErr([]string{"connections", idx, "from"}, "%s", msg)
				return &loaderr.Error{
					Field:   loaderr.Path("connections", i, "from"),
					Problem: msg,
					Fix:     fmt.Sprintf("add component %q to object %q or connect from an object that has it", ev.Component, c.From),
					Err:     fe,
				}
			}
		}
	}
	if h.Component != "" {
		if toObj, present := byID[c.To]; present {
			if _, has := toObj.Components[h.Component]; !has {
				msg := fmt.Sprintf("sceneformat: connection %d: target object %q lacks component %q required by handler %q", i, c.To, h.Component, c.Handler)
				fe := fieldErr([]string{"connections", idx, "to"}, "%s", msg)
				return &loaderr.Error{
					Field:   loaderr.Path("connections", i, "to"),
					Problem: msg,
					Fix:     fmt.Sprintf("add component %q to object %q or connect to an object that has it", h.Component, c.To),
					Err:     fe,
				}
			}
		}
	}

	if len(c.Args) > 0 {
		for _, k := range slices.Sorted(maps.Keys(c.Args)) {
			f, ok := h.Arg(k)
			if !ok {
				var msg, fix string
				argNames := h.ArgNames()
				if len(argNames) == 0 {
					msg = fmt.Sprintf("sceneformat: connection %d: handler %q takes no args, got %q", i, c.Handler, k)
					fix = fmt.Sprintf("remove args.%s or declare it on handler %q", k, c.Handler)
				} else {
					list := strings.Join(argNames, ", ")
					msg = fmt.Sprintf("sceneformat: connection %d: handler %q has unknown arg %q (declared: %s)", i, c.Handler, k, list)
					fix = fmt.Sprintf("use one of handler %q's declared args (%s) or remove %q", c.Handler, list, k)
				}
				fe := fieldErr([]string{"connections", idx, "args", k}, "%s", msg)
				return &loaderr.Error{
					Field:   loaderr.Path("connections", i, "args", k),
					Problem: msg,
					Fix:     fix,
					Err:     fe,
				}
			}
			if _, bad := f.check(c.Args[k]); bad != "" {
				msg := fmt.Sprintf("sceneformat: connection %d: handler %q arg %q: %s", i, c.Handler, k, bad)
				fe := fieldErr([]string{"connections", idx, "args", k}, "%s", msg)
				return &loaderr.Error{
					Field:   loaderr.Path("connections", i, "args", k),
					Problem: msg,
					Fix:     fmt.Sprintf("set args.%s to a valid %s value", k, f.Kind),
					Err:     fe,
				}
			}
		}
	}
	return nil
}

// resolveHandlerArgs builds the normalized static argument map for a validated connection against h, filling in defaults for unset declared args. Returns nil when h declares no args.
func resolveHandlerArgs(h *HandlerType, raw map[string]any) map[string]any {
	if len(h.Args) == 0 {
		return nil
	}
	out := make(map[string]any, len(h.Args))
	for _, f := range h.Args {
		if v, ok := raw[f.Name]; ok {
			norm, _ := f.check(v)
			out[f.Name] = norm
			continue
		}
		out[f.Name] = f.Default
	}
	return out
}

// validateConnectionsDoc checks the structural validity of connections in a scene document: non-empty from, event, to and handler, and when checkRefs is true, that both from and to name objects in objectIDs.
func validateConnectionsDoc(conns []ConnectionDoc, objectIDs map[string]struct{}, checkRefs bool) error {
	for i := range conns {
		c := &conns[i]
		cp := []string{"connections", strconv.Itoa(i)}
		if c.From == "" {
			return fieldErr(at(cp, "from"), "sceneformat: connection %d has an empty from", i)
		}
		if c.Event == "" {
			return fieldErr(at(cp, "event"), "sceneformat: connection %d has an empty event", i)
		}
		if c.To == "" {
			return fieldErr(at(cp, "to"), "sceneformat: connection %d has an empty to", i)
		}
		if c.Handler == "" {
			return fieldErr(at(cp, "handler"), "sceneformat: connection %d has an empty handler", i)
		}
		if checkRefs {
			if _, ok := objectIDs[c.From]; !ok {
				return fieldErr(at(cp, "from"), "sceneformat: connection %d references unknown source object %q", i, c.From)
			}
			if _, ok := objectIDs[c.To]; !ok {
				return fieldErr(at(cp, "to"), "sceneformat: connection %d references unknown target object %q", i, c.To)
			}
		}
	}
	return nil
}

type eventKey struct {
	from  string
	event string
}

type boundConnection struct {
	from    string
	event   string
	to      string
	handler string
	args    map[string]any
	once    bool
	active  bool
	fn      func(EventCall)
}

type queuedEvent struct {
	from    string
	event   string
	payload any
}

// EventBus dispatches events along the connections declared in a scene document.
//
// Connections run in document order; a Once connection disconnects before its handler runs, so re-entrant emissions never fire it twice. Calling Emit or DisconnectAll inside a handler is safe.
type EventBus struct {
	bySource map[eventKey][]*boundConnection
	all      []*boundConnection
	depth    int
	dirty    bool
	queue    []queuedEvent

	world    *entities.World
	entities map[string]entities.Entity
	entityID map[entities.Entity]string
	scene    *BuiltScene
}

// Bind validates doc (which must already be resolved if it references other scenes), checks every connection against r, and returns an EventBus with all declared connections wired. It returns an error if r is nil, doc is invalid or unresolved, any connection fails validation, or any connected handler has a nil Func.
func (r *EventRegistry) Bind(doc *SceneDoc) (*EventBus, error) {
	return r.bind(doc, nil, nil, nil)
}

// BindEntities validates doc and binds its connections to entities previously spawned into w (such as by SpawnEntities). Handlers receive World, SourceEntity and TargetEntity in EventCall, and EmitEntity / EnqueueEntity dispatch events by entity handle.
func (r *EventRegistry) BindEntities(doc *SceneDoc, w *entities.World, spawned map[string]entities.Entity) (*EventBus, error) {
	if w == nil {
		return nil, errors.New("sceneformat: nil entity world")
	}
	if w.Disposed() {
		return nil, fmt.Errorf("sceneformat: bind entities: %w", entities.ErrWorldDisposed)
	}
	return r.bind(doc, w, spawned, nil)
}

func (r *EventRegistry) bind(doc *SceneDoc, w *entities.World, spawned map[string]entities.Entity, scene *BuiltScene) (*EventBus, error) {
	if r == nil {
		return nil, errors.New("sceneformat: nil event registry")
	}
	if err := Validate(doc); err != nil {
		return nil, err
	}
	if doc.RefersToScenes() {
		return nil, errUnresolved
	}
	if err := ValidateConnections(doc, r); err != nil {
		return nil, err
	}
	bus := &EventBus{
		bySource: make(map[eventKey][]*boundConnection),
		all:      make([]*boundConnection, 0, len(doc.Connections)),
		world:    w,
		scene:    scene,
	}
	if w != nil {
		bus.entities = make(map[string]entities.Entity, len(spawned))
		bus.entityID = make(map[entities.Entity]string, len(spawned))
		for id, e := range spawned {
			bus.entities[id] = e
			bus.entityID[e] = id
		}
	}
	for i := range doc.Connections {
		c := &doc.Connections[i]
		h := r.handlers[c.Handler]
		if h.Func == nil {
			msg := fmt.Sprintf("sceneformat: connection %d: handler %q has no callback", i, c.Handler)
			fe := fieldErr([]string{"connections", strconv.Itoa(i), "handler"}, "%s", msg)
			return nil, &loaderr.Error{
				Field:   loaderr.Path("connections", i, "handler"),
				Problem: msg,
				Fix:     fmt.Sprintf("set Func when registering handler %q on the EventRegistry", c.Handler),
				Err:     fe,
			}
		}
		if w != nil {
			if _, ok := bus.entities[c.From]; !ok {
				return nil, fmt.Errorf("sceneformat: connection %d: source object %q is not in spawned entities", i, c.From)
			}
			if _, ok := bus.entities[c.To]; !ok {
				return nil, fmt.Errorf("sceneformat: connection %d: target object %q is not in spawned entities", i, c.To)
			}
		}
		bc := &boundConnection{
			from:    c.From,
			event:   c.Event,
			to:      c.To,
			handler: c.Handler,
			args:    resolveHandlerArgs(h, c.Args),
			once:    c.Once,
			active:  true,
			fn:      h.Func,
		}
		key := eventKey{from: c.From, event: c.Event}
		bus.bySource[key] = append(bus.bySource[key], bc)
		bus.all = append(bus.all, bc)
	}
	return bus, nil
}

// Emit synchronously delivers event from the object named fromID to every active connected handler in document order, returning how many handlers ran.
func (b *EventBus) Emit(fromID, event string, payload any) int {
	if b == nil {
		return 0
	}
	key := eventKey{from: fromID, event: event}
	list := b.bySource[key]
	if len(list) == 0 {
		return 0
	}
	b.depth++
	defer func() {
		b.depth--
		if b.depth == 0 && b.dirty {
			b.compact()
		}
	}()

	var srcEntity entities.Entity
	if b.world != nil {
		if b.world.Disposed() {
			return 0
		}
		srcEntity = b.entities[fromID]
	}
	var srcObj *BuiltObject
	if b.scene != nil {
		srcObj = b.scene.Objects[fromID]
	}

	called := 0
	n := len(list)
	for i := 0; i < n; i++ {
		bc := list[i]
		if !bc.active {
			continue
		}
		var dstEntity entities.Entity
		if b.world != nil {
			if b.world.Disposed() {
				break
			}
			dstEntity = b.entities[bc.to]
			if !b.world.Alive(dstEntity) {
				continue
			}
		}
		var dstObj *BuiltObject
		if b.scene != nil {
			dstObj = b.scene.Objects[bc.to]
		}
		fn := bc.fn
		if bc.once {
			bc.active = false
			bc.fn = nil
			b.dirty = true
		}
		fn(EventCall{
			Bus:          b,
			Source:       bc.from,
			Event:        bc.event,
			Target:       bc.to,
			Handler:      bc.handler,
			Args:         cloneAnyMap(bc.args),
			Payload:      payload,
			World:        b.world,
			SourceEntity: srcEntity,
			TargetEntity: dstEntity,
			Scene:        b.scene,
			SourceObject: srcObj,
			TargetObject: dstObj,
		})
		called++
	}
	return called
}

// EmitEntity synchronously delivers event from the spawned entity e to every active connected handler in document order, returning how many handlers ran. It returns 0 when b is nil, not bound to an entities.World, or e is dead or unknown to this scene.
func (b *EventBus) EmitEntity(e entities.Entity, event string, payload any) int {
	if b == nil || b.world == nil || b.world.Disposed() || !b.world.Alive(e) {
		return 0
	}
	id, ok := b.entityID[e]
	if !ok {
		return 0
	}
	return b.Emit(id, event, payload)
}

// Enqueue buffers an event from fromID to be delivered in FIFO order on the next Flush.
func (b *EventBus) Enqueue(fromID, event string, payload any) {
	if b == nil {
		return
	}
	b.queue = append(b.queue, queuedEvent{from: fromID, event: event, payload: payload})
}

// EnqueueEntity buffers an event from entity e to be delivered in FIFO order on the next Flush. Ignored when b is nil, not bound to an entities.World, or e is dead or unknown to this scene.
func (b *EventBus) EnqueueEntity(e entities.Entity, event string, payload any) {
	if b == nil || b.world == nil || b.world.Disposed() || !b.world.Alive(e) {
		return
	}
	id, ok := b.entityID[e]
	if !ok {
		return
	}
	b.Enqueue(id, event, payload)
}

// Flush delivers all events buffered by Enqueue and EnqueueEntity in FIFO order, including any events enqueued by handlers during Flush, and returns the total number of handler invocations.
func (b *EventBus) Flush() int {
	if b == nil {
		return 0
	}
	total := 0
	for len(b.queue) > 0 {
		q := b.queue
		b.queue = nil
		for _, ev := range q {
			total += b.Emit(ev.from, ev.event, ev.payload)
		}
	}
	return total
}

// Len returns the number of currently active connections on b.
func (b *EventBus) Len() int {
	if b == nil {
		return 0
	}
	n := 0
	for _, bc := range b.all {
		if bc.active {
			n++
		}
	}
	return n
}

// DisconnectAll deactivates every connection on b and discards any queued events.
func (b *EventBus) DisconnectAll() {
	if b == nil {
		return
	}
	for _, bc := range b.all {
		bc.active = false
		bc.fn = nil
	}
	b.queue = nil
	b.dirty = true
	if b.depth == 0 {
		b.compact()
	}
}

func (b *EventBus) compact() {
	for k, list := range b.bySource {
		w := 0
		for _, bc := range list {
			if bc.active {
				list[w] = bc
				w++
			}
		}
		clear(list[w:])
		if w == 0 {
			delete(b.bySource, k)
		} else {
			b.bySource[k] = list[:w]
		}
	}
	w := 0
	for _, bc := range b.all {
		if bc.active {
			b.all[w] = bc
			w++
		}
	}
	clear(b.all[w:])
	b.all = b.all[:w]
	b.dirty = false
}
