package sceneformat

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/entities"
	"go.yaml.in/yaml/v3"
)

// Event declares an event that objects with a component emit (ComponentType.Events), such as a button's "pressed".
type Event struct {
	// Name is the event's name as connections write it. It must be non-empty, hold no dot and be unique among its component type's events.
	Name string
	// Doc is a one-line description shown by tools such as the editor.
	Doc string
}

// HandlerFunc runs a handler for one emitted event.
type HandlerFunc func(Call)

// Handler declares something objects with a component can do when an event connected to it fires (ComponentType.Handlers), such as a door's "open".
type Handler struct {
	// Name is the handler's name as connections write it. It must be non-empty, hold no dot and be unique among its component type's handlers.
	Name string
	// Doc is a one-line description shown by tools such as the editor.
	Doc string
	// Params are the arguments a connection may pass the handler (ConnectionDoc.Args), checked as component fields are: by kind and range, with a default for one a connection leaves out.
	Params []Field
	// Func runs the handler. A registry used only to check files, such as an editor's, may leave it nil; Connect refuses a connection to a handler without one.
	Func HandlerFunc
}

// Param returns the param named name and whether the handler has it.
func (h *Handler) Param(name string) (Field, bool) {
	if h == nil {
		return Field{}, false
	}
	for _, p := range h.Params {
		if p.Name == name {
			return p, true
		}
	}
	return Field{}, false
}

// Call is what a HandlerFunc receives: the connection that fired, the objects at both ends and the values passed along.
type Call struct {
	// Signals is the dispatcher that called the handler, so a handler can emit or enqueue further events.
	Signals *Signals
	// Context is the value passed to Connect, ConnectEntities or SpawnEntitiesWithSignals, such as the game's own state or a *BuiltScene.
	Context any
	// World is the entities.World bound to Signals when created with ConnectEntities or SpawnEntitiesWithSignals, or nil otherwise.
	World *entities.World
	// Source is the object that emitted the event, in the document passed to Connect.
	Source *ObjectDoc
	// SourceEntity is the entity in World bound to Source, or entities.Zero when Signals is not bound to a world.
	SourceEntity entities.Entity
	// SourceComponent is the component of Source whose type declares the event.
	SourceComponent string
	// Event is the event's name as its component type declares it, without the component.
	Event string
	// Target is the object whose handler runs, in the document passed to Connect.
	Target *ObjectDoc
	// TargetEntity is the entity in World bound to Target, or entities.Zero when Signals is not bound to a world.
	TargetEntity entities.Entity
	// Component is the component of Target whose type declares the handler.
	Component string
	// Handler is the handler's name as its component type declares it, without the component.
	Handler string
	// Args holds every param of the handler: the connection's value normalized to the param's Go type (bool, int, float64 or string), or the param's default. The map is the handler's own to change.
	Args map[string]any
	// Payload is the value passed to Signals.Emit, Signals.Enqueue, Signals.EmitEntity or Signals.EnqueueEntity.
	Payload any
}

// ConnectionError reports a connection (SceneDoc.Connections) that names an event, handler or argument its objects' registered component types do not declare. File, Line and Column locate it in a scene file when it came from ParseWith; they are empty or zero otherwise.
type ConnectionError struct {
	// File is ParseOptions.File, or empty.
	File string
	// Line and Column are the 1-based position of the offending value, or 0 when unknown.
	Line, Column int
	// Index is the connection's index in SceneDoc.Connections.
	Index int
	// From, Event, To and Handler are the connection's fields as written.
	From, Event, To, Handler string
	// Field is the connection's key at fault: "from", "event", "to", "handler" or "args".
	Field string
	// Arg is the argument at fault when Field is "args", or empty.
	Arg string
	// Msg says what is wrong.
	Msg string
}

// Error formats the error as `file:line:col: sceneformat: connection 0 ("button" pressed -> "door" open) argument "speed": msg`, leaving out the parts that are unknown.
func (e *ConnectionError) Error() string {
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
	fmt.Fprintf(&b, "sceneformat: connection %d (%q %s -> %q %s)", e.Index, e.From, e.Event, e.To, e.Handler)
	if e.Arg != "" {
		fmt.Fprintf(&b, " argument %q", e.Arg)
	}
	b.WriteString(": ")
	b.WriteString(e.Msg)
	return b.String()
}

// ValidateConnections checks every connection of doc against reg: each event must be declared by exactly one registered component type of its From object (or by the component it names, as in "button.pressed"), each handler likewise on its To object, and each argument must match a param of the handler in kind and range. doc must not instance or inherit other scene files; pass it through Resolve first. The error is a *ConnectionError without a position; ParseWith with ParseOptions.Components reports the same errors with the file and line.
func ValidateConnections(doc *SceneDoc, reg *ComponentRegistry) error {
	if doc == nil || reg == nil {
		return nil
	}
	if doc.RefersToScenes() {
		return errUnresolved
	}
	_, err := bindConnections(doc, reg)
	return err
}

// MaxEmitDepth is how deeply Signals.Emit nests: a handler that emits an event is one level, and a handler that event runs and emits another is two. Emit refuses to go deeper, which stops connections that trigger each other from looping forever.
const MaxEmitDepth = 64

// MaxFlushWaves is how many consecutive waves of events Signals.Flush drains in one call: the events queued before Flush starts are wave 1, any events handlers enqueue during wave 1 are wave 2, and so on. Flush stops, discards the remaining queue and returns an error if more events remain after MaxFlushWaves waves, which stops connections that re-enqueue each other from hanging the game.
const MaxFlushWaves = MaxEmitDepth

// queuedEvent is one event waiting for Signals.Flush.
type queuedEvent struct {
	from    string
	event   memberRef
	payload any
}

// Signals dispatches the events a scene's objects emit to the handlers its connections name. Connect, ConnectEntities and SpawnEntitiesWithSignals make one. It is not safe for concurrent use: emit, enqueue and flush from the goroutine that runs the game's update.
type Signals struct {
	ctx any
	// names maps every object ID to the events its registered components declare, keyed both "component.event" and by the bare event name; a bare name two components declare maps to an empty key.
	names map[string]map[string]memberRef
	// bindings maps an object ID and its event's "component.event" key to its connections in document order.
	bindings map[bindKey][]*binding
	depth    int
	queue    []queuedEvent
	flushing bool
	// world, entities and byEntity are set when Signals is bound to an entities.World via ConnectEntities or SpawnEntitiesWithSignals.
	world    *entities.World
	entities map[string]entities.Entity
	byEntity map[entities.Entity]string
}

// memberRef names an event or handler of one component.
type memberRef struct {
	component, name string
}

func (m memberRef) key() string { return m.component + "." + m.name }

type bindKey struct {
	from, event string
}

// binding is one connection made ready to call.
type binding struct {
	index                      int
	source, target             *ObjectDoc
	sourceEntity, targetEntity entities.Entity
	event, handler             memberRef
	fn                         HandlerFunc
	args                       map[string]any
	once, done                 bool
}

// Connect checks doc's components and connections against reg and returns the Signals that run them; ctx is passed to every handler as Call.Context. doc must be valid, must not instance or inherit other scene files (pass it through Resolve first, or use BuiltScene.Doc), and every connected handler must have a Func. Calls point into doc, so handlers see and may change its objects. The Signals is a snapshot of doc's objects as Connect finds them: after adding or removing objects in doc, which may also move the others in memory, call Connect again, since the old Signals would hand handlers stale objects and refuse events of the new ones.
func Connect(doc *SceneDoc, reg *ComponentRegistry, ctx any) (*Signals, error) {
	if reg == nil {
		return nil, errors.New("sceneformat: connect with nil component registry")
	}
	if err := Validate(doc); err != nil {
		return nil, err
	}
	if doc.RefersToScenes() {
		return nil, errUnresolved
	}
	if err := ValidateComponents(doc, reg); err != nil {
		return nil, err
	}
	bs, err := bindConnections(doc, reg)
	if err != nil {
		return nil, err
	}
	s := &Signals{ctx: ctx, names: make(map[string]map[string]memberRef), bindings: make(map[bindKey][]*binding, len(bs))}
	for _, b := range bs {
		if b.fn == nil {
			c := &doc.Connections[b.index]
			return nil, &ConnectionError{Index: b.index, From: c.From, Event: c.Event, To: c.To, Handler: c.Handler, Field: "handler", Msg: fmt.Sprintf("handler %q of component type %q has no Func", b.handler.name, b.handler.component)}
		}
		k := bindKey{b.source.ID, b.event.key()}
		s.bindings[k] = append(s.bindings[k], b)
	}
	for li := range doc.Layers {
		WalkObjects(doc.Layers[li].Objects, func(o, _ *ObjectDoc) bool {
			s.names[o.ID] = eventNames(o, reg)
			return true
		})
	}
	return s, nil
}

// ConnectEntities checks doc's components and connections against reg and binds them to the live entities spawned for doc in w (as SpawnEntities returns). ctx is passed to every handler as Call.Context, w as Call.World, and each handler receives SourceEntity and TargetEntity alongside Source and Target. Every object in doc must map to a distinct live entity in w. When an entity is later despawned from w, Emit, Enqueue, EmitEntity, EnqueueEntity and Flush cleanly skip connections whose source or target entity is no longer alive.
func ConnectEntities(doc *SceneDoc, reg *ComponentRegistry, w *entities.World, spawned map[string]entities.Entity, ctx any) (*Signals, error) {
	if w == nil {
		return nil, errors.New("sceneformat: nil entity world")
	}
	if w.Disposed() {
		return nil, fmt.Errorf("sceneformat: connect entities: %w", entities.ErrWorldDisposed)
	}
	s, err := Connect(doc, reg, ctx)
	if err != nil {
		return nil, err
	}
	if err := s.bindWorld(w, spawned); err != nil {
		return nil, err
	}
	return s, nil
}

// bindWorld binds every scene object of s to its live entity in w.
func (s *Signals) bindWorld(w *entities.World, spawned map[string]entities.Entity) error {
	ents := make(map[string]entities.Entity, len(s.names))
	byEnt := make(map[entities.Entity]string, len(s.names))
	for _, id := range slices.Sorted(maps.Keys(s.names)) {
		e, ok := spawned[id]
		if !ok || e.IsZero() {
			return fmt.Errorf("sceneformat: connect entities: missing entity for object %q", id)
		}
		if !w.Alive(e) {
			return fmt.Errorf("sceneformat: connect entities: entity %s for object %q is not alive", e, id)
		}
		if prev, dup := byEnt[e]; dup {
			return fmt.Errorf("sceneformat: connect entities: objects %q and %q share entity %s", prev, id, e)
		}
		ents[id] = e
		byEnt[e] = id
	}
	s.world = w
	s.entities = ents
	s.byEntity = byEnt
	for _, bs := range s.bindings {
		for _, b := range bs {
			b.sourceEntity = ents[b.source.ID]
			b.targetEntity = ents[b.target.ID]
		}
	}
	return nil
}

// eventNames returns the events o's registered components declare, keyed as Signals.names keeps them.
func eventNames(o *ObjectDoc, reg *ComponentRegistry) map[string]memberRef {
	var out map[string]memberRef
	for _, comp := range slices.Sorted(maps.Keys(o.Components)) {
		t := reg.lookup(comp)
		if t == nil {
			continue
		}
		for _, ev := range t.Events {
			if out == nil {
				out = make(map[string]memberRef)
			}
			m := memberRef{comp, ev.Name}
			out[m.key()] = m
			if prev, dup := out[ev.Name]; dup && prev != m {
				out[ev.Name] = memberRef{}
			} else {
				out[ev.Name] = m
			}
		}
	}
	return out
}

// resolveEvent validates from and event for op ("emit" or "enqueue") and returns the resolved event memberRef.
func (s *Signals) resolveEvent(op, from, event string) (memberRef, error) {
	if s == nil {
		return memberRef{}, fmt.Errorf("sceneformat: %s on nil Signals", op)
	}
	names, ok := s.names[from]
	if !ok {
		return memberRef{}, fmt.Errorf("sceneformat: %s %q: the scene has no object %q", op, event, from)
	}
	m, ok := names[event]
	if !ok {
		return memberRef{}, fmt.Errorf("sceneformat: %s %q: no registered component of object %q declares the event", op, event, from)
	}
	if m.component == "" {
		return memberRef{}, fmt.Errorf("sceneformat: %s %q: several components of object %q declare the event; name it as component.%s", op, event, from, event)
	}
	return m, nil
}

// dispatch calls every active binding for (from, ev) in document order, skipping despawned entities when bound to a world.
func (s *Signals) dispatch(from string, ev memberRef, payload any) {
	if s.world != nil && !s.world.Alive(s.entities[from]) {
		return
	}
	for _, b := range s.bindings[bindKey{from, ev.key()}] {
		if b.done {
			continue
		}
		if s.world != nil && !s.world.Alive(b.targetEntity) {
			continue
		}
		if b.once {
			b.done = true
		}
		b.fn(Call{
			Signals:         s,
			Context:         s.ctx,
			World:           s.world,
			Source:          b.source,
			SourceEntity:    b.sourceEntity,
			SourceComponent: b.event.component,
			Event:           b.event.name,
			Target:          b.target,
			TargetEntity:    b.targetEntity,
			Component:       b.handler.component,
			Handler:         b.handler.name,
			Args:            maps.Clone(b.args),
			Payload:         payload,
		})
	}
}

// Emit fires the event named event of the object with ID from, passing payload, and calls the handler of every connection from that event, in the order the scene lists them. event is a bare name, such as "pressed", or "button.pressed" to say which component when several of the object's components declare it. A once connection runs the first time only. An event no connection names does nothing. When Signals is bound to an entity world, despawned source or target entities are skipped cleanly.
//
// Emit returns an error, and calls nothing, when the scene has no object from, when none of its registered components declares the event or two declare a bare name, and when handlers that emit events have nested more than MaxEmitDepth levels.
func (s *Signals) Emit(from, event string, payload any) error {
	m, err := s.resolveEvent("emit", from, event)
	if err != nil {
		return err
	}
	if s.depth >= MaxEmitDepth {
		return fmt.Errorf("sceneformat: emit %q of object %q: handlers emitting events nest deeper than %d levels", event, from, MaxEmitDepth)
	}
	s.depth++
	defer func() { s.depth-- }()
	s.dispatch(from, m, payload)
	return nil
}

// Enqueue validates from and event and appends the event to s's deferred queue, to be dispatched in FIFO order on the next Flush. Use Enqueue and Flush to drain events once per game.Loop step without handlers re-entering mid-step: a handler that calls Enqueue appends to the queue and returns before the enqueued event runs. When Signals is bound to an entity world and from's entity has been despawned, Enqueue skips queueing cleanly and returns nil.
//
// Enqueue returns an error, and queues nothing, when s is nil, the scene has no object from, or no registered component of from unambiguously declares event.
func (s *Signals) Enqueue(from, event string, payload any) error {
	m, err := s.resolveEvent("enqueue", from, event)
	if err != nil {
		return err
	}
	if s.world != nil && !s.world.Alive(s.entities[from]) {
		return nil
	}
	s.queue = append(s.queue, queuedEvent{from: from, event: m, payload: payload})
	return nil
}

// Queued returns the number of events currently waiting for Flush.
func (s *Signals) Queued() int {
	if s == nil {
		return 0
	}
	return len(s.queue)
}

// Flush dispatches every queued event in strict FIFO enqueue order and drains any follow-up events enqueued by handlers wave by wave, without recursion: each handler returns before any event it enqueued runs. Events whose source or target entity has been despawned before dispatch are skipped cleanly.
//
// Flush returns an error when s is nil, when called re-entrantly from a handler running inside Flush, or when handlers keep enqueuing events past MaxFlushWaves waves (in which case the remaining queue is discarded).
func (s *Signals) Flush() error {
	if s == nil {
		return errors.New("sceneformat: flush on nil Signals")
	}
	if s.flushing {
		return errors.New("sceneformat: Flush cannot be called while another Flush is running")
	}
	if len(s.queue) == 0 {
		return nil
	}
	s.flushing = true
	defer func() { s.flushing = false }()
	for wave := 0; len(s.queue) > 0; wave++ {
		if wave >= MaxFlushWaves {
			s.queue = nil
			return fmt.Errorf("sceneformat: flush: handlers enqueuing events exceeded %d waves", MaxFlushWaves)
		}
		batch := s.queue
		s.queue = nil
		for _, q := range batch {
			s.dispatch(q.from, q.event, q.payload)
		}
	}
	return nil
}

// Entity returns the entity bound to the scene object with ID id, or entities.Zero and false when s is nil, not bound to an entity world, or has no object id.
func (s *Signals) Entity(id string) (entities.Entity, bool) {
	if s == nil || s.entities == nil {
		return entities.Zero, false
	}
	e, ok := s.entities[id]
	return e, ok
}

// EmitEntity fires the event named event of the scene object bound to entity from, passing payload. It returns an error when s is nil, not bound to an entity world, from is not bound in this scene, event is not unambiguously declared by a registered component of from's object, or nested Emit calls exceed MaxEmitDepth. When from was bound in this scene and has since been despawned from the world, EmitEntity skips dispatch cleanly and returns nil.
func (s *Signals) EmitEntity(from entities.Entity, event string, payload any) error {
	if s == nil {
		return errors.New("sceneformat: emit on nil Signals")
	}
	if s.world == nil {
		return errors.New("sceneformat: Signals is not bound to an entity world")
	}
	id, ok := s.byEntity[from]
	if !ok {
		return fmt.Errorf("sceneformat: emit %q: entity %s is not bound in this scene", event, from)
	}
	return s.Emit(id, event, payload)
}

// EnqueueEntity validates and queues event of the scene object bound to entity from, to be dispatched in FIFO order on the next Flush. It returns an error when s is nil, not bound to an entity world, from is not bound in this scene, or event is not unambiguously declared by a registered component of from's object. When from was bound in this scene and has since been despawned from the world, EnqueueEntity skips queueing cleanly and returns nil.
func (s *Signals) EnqueueEntity(from entities.Entity, event string, payload any) error {
	if s == nil {
		return errors.New("sceneformat: enqueue on nil Signals")
	}
	if s.world == nil {
		return errors.New("sceneformat: Signals is not bound to an entity world")
	}
	id, ok := s.byEntity[from]
	if !ok {
		return fmt.Errorf("sceneformat: enqueue %q: entity %s is not bound in this scene", event, from)
	}
	return s.Enqueue(id, event, payload)
}

// bindConnections checks the connections of doc, a valid scene that refers to no other scene file, against reg and returns them ready to call, in document order.
func bindConnections(doc *SceneDoc, reg *ComponentRegistry) ([]*binding, error) {
	if len(doc.Connections) == 0 {
		return nil, nil
	}
	objects := make(map[string]*ObjectDoc)
	for li := range doc.Layers {
		indexObjects(doc.Layers[li].Objects, objects)
	}
	out := make([]*binding, 0, len(doc.Connections))
	seen := make(map[[4]string]int, len(doc.Connections))
	for i := range doc.Connections {
		c := &doc.Connections[i]
		fail := func(field, arg, msg string) error {
			return &ConnectionError{Index: i, From: c.From, Event: c.Event, To: c.To, Handler: c.Handler, Field: field, Arg: arg, Msg: msg}
		}
		src, ok := objects[c.From]
		if !ok {
			return nil, fail("from", "", fmt.Sprintf("the scene has no object %q", c.From))
		}
		dst, ok := objects[c.To]
		if !ok {
			return nil, fail("to", "", fmt.Sprintf("the scene has no object %q", c.To))
		}
		ev, msg := findMember(src, reg, c.Event, false)
		if msg != "" {
			return nil, fail("event", "", msg)
		}
		hm, msg := findMember(dst, reg, c.Handler, true)
		if msg != "" {
			return nil, fail("handler", "", msg)
		}
		key := [4]string{c.From, ev.key(), c.To, hm.key()}
		if j, dup := seen[key]; dup {
			return nil, fail("handler", "", fmt.Sprintf("connects the same event and handler as connection %d", j))
		}
		seen[key] = i
		h, _ := reg.lookup(hm.component).Handler(hm.name)
		args := make(map[string]any, len(h.Params))
		for _, name := range slices.Sorted(maps.Keys(c.Args)) {
			p, ok := h.Param(name)
			if !ok {
				return nil, fail("args", name, unknownParamMsg(&h))
			}
			norm, msg := p.check(c.Args[name])
			if msg != "" {
				return nil, fail("args", name, msg)
			}
			args[name] = norm
		}
		for _, p := range h.Params {
			if _, set := args[p.Name]; !set {
				args[p.Name] = p.Default
			}
		}
		out = append(out, &binding{index: i, source: src, target: dst, event: ev, handler: hm, fn: h.Func, args: args, once: c.Once})
	}
	return out, nil
}

// findMember finds the event (or, when handler is true, the handler) that name, a bare name or "component.name", names on o, and returns a message saying what is wrong when no registered component of o, or more than one, declares it.
func findMember(o *ObjectDoc, reg *ComponentRegistry, name string, handler bool) (memberRef, string) {
	noun := "event"
	if handler {
		noun = "handler"
	}
	declares := func(t *ComponentType, n string) bool {
		if handler {
			_, ok := t.Handler(n)
			return ok
		}
		_, ok := t.Event(n)
		return ok
	}
	if comp, n, qualified := strings.Cut(name, "."); qualified {
		if _, has := o.Components[comp]; !has {
			return memberRef{}, fmt.Sprintf("object %q has no component %q", o.ID, comp)
		}
		t := reg.lookup(comp)
		if t == nil {
			return memberRef{}, fmt.Sprintf("component %q of object %q is not registered", comp, o.ID)
		}
		if !declares(t, n) {
			return memberRef{}, fmt.Sprintf("component type %q declares no %s %q", comp, noun, n)
		}
		return memberRef{comp, n}, ""
	}
	var found []memberRef
	var all []string
	for _, comp := range slices.Sorted(maps.Keys(o.Components)) {
		t := reg.lookup(comp)
		if t == nil {
			continue
		}
		if declares(t, name) {
			found = append(found, memberRef{comp, name})
		}
		if handler {
			for _, h := range t.Handlers {
				all = append(all, comp+"."+h.Name)
			}
		} else {
			for _, e := range t.Events {
				all = append(all, comp+"."+e.Name)
			}
		}
	}
	switch len(found) {
	case 1:
		return found[0], ""
	case 0:
		if len(all) == 0 {
			return memberRef{}, fmt.Sprintf("no registered component of object %q declares %s %q (it has no %ss)", o.ID, noun, name, noun)
		}
		return memberRef{}, fmt.Sprintf("no registered component of object %q declares %s %q (it has: %s)", o.ID, noun, name, strings.Join(all, ", "))
	default:
		keys := make([]string, len(found))
		for i, m := range found {
			keys[i] = m.key()
		}
		return memberRef{}, fmt.Sprintf("several components of object %q declare %s %q; write one of %s", o.ID, noun, name, strings.Join(keys, ", "))
	}
}

func unknownParamMsg(h *Handler) string {
	if len(h.Params) == 0 {
		return fmt.Sprintf("unknown argument (handler %q has no params)", h.Name)
	}
	names := make([]string, len(h.Params))
	for i, p := range h.Params {
		names[i] = p.Name
	}
	return fmt.Sprintf("unknown argument (params: %s)", strings.Join(names, ", "))
}

// normalizeEvents checks the events of the component type owner names and returns a copy of them.
func normalizeEvents(owner string, events []Event) ([]Event, error) {
	seen := make(map[string]struct{}, len(events))
	for i, e := range events {
		if err := checkMemberName(owner, "event", i, e.Name, seen); err != nil {
			return nil, err
		}
	}
	return slices.Clone(events), nil
}

// normalizeHandlers checks the handlers of the component type owner names and returns copies of them with their params normalized.
func normalizeHandlers(owner string, handlers []Handler) ([]Handler, error) {
	if handlers == nil {
		return nil, nil
	}
	out := make([]Handler, len(handlers))
	seen := make(map[string]struct{}, len(handlers))
	for i, h := range handlers {
		if err := checkMemberName(owner, "handler", i, h.Name, seen); err != nil {
			return nil, err
		}
		params, err := normalizeFields(fmt.Sprintf("%s handler %q", owner, h.Name), "param", h.Params)
		if err != nil {
			return nil, err
		}
		out[i] = h
		if h.Params == nil {
			params = nil
		}
		out[i].Params = params
	}
	return out, nil
}

// checkMemberName checks the name of the i-th event or handler (noun) of owner and records it in seen.
func checkMemberName(owner, noun string, i int, name string, seen map[string]struct{}) error {
	if name == "" {
		return fmt.Errorf("sceneformat: %s %s %d has an empty name", owner, noun, i)
	}
	if strings.Contains(name, ".") {
		return fmt.Errorf("sceneformat: %s %s %q must not contain a dot", owner, noun, name)
	}
	if _, dup := seen[name]; dup {
		return fmt.Errorf("sceneformat: %s has duplicate %s %q", owner, noun, name)
	}
	seen[name] = struct{}{}
	return nil
}

// validMemberRef reports whether name is an event or handler name as a connection writes it: a bare name, or a component and a name joined by one dot.
func validMemberRef(name string) bool {
	comp, n, qualified := strings.Cut(name, ".")
	if !qualified {
		return name != ""
	}
	return comp != "" && n != "" && !strings.Contains(n, ".")
}

// validateConnections checks the connections of doc without a registry: every field set, event and handler written as a name or component.name, arguments that are bools, finite numbers or strings, no connection repeated and, in a scene that refers to no other scene file, From and To naming objects among ids.
func validateConnections(doc *SceneDoc, ids map[string]struct{}) error {
	if len(doc.Connections) == 0 {
		return nil
	}
	selfContained := !doc.RefersToScenes()
	seen := make(map[[4]string]int, len(doc.Connections))
	for i := range doc.Connections {
		c := &doc.Connections[i]
		cp := []string{"connections", strconv.Itoa(i)}
		for _, f := range []struct{ field, v string }{{"from", c.From}, {"event", c.Event}, {"to", c.To}, {"handler", c.Handler}} {
			if f.v == "" {
				return fieldErr(at(cp, f.field), "sceneformat: connection %d has an empty %s", i, f.field)
			}
		}
		if !validMemberRef(c.Event) {
			return fieldErr(at(cp, "event"), "sceneformat: connection %d event %q must be a name or component.name", i, c.Event)
		}
		if !validMemberRef(c.Handler) {
			return fieldErr(at(cp, "handler"), "sceneformat: connection %d handler %q must be a name or component.name", i, c.Handler)
		}
		if selfContained {
			if _, ok := ids[c.From]; !ok {
				return fieldErr(at(cp, "from"), "sceneformat: connection %d from %q names no object of the scene", i, c.From)
			}
			if _, ok := ids[c.To]; !ok {
				return fieldErr(at(cp, "to"), "sceneformat: connection %d to %q names no object of the scene", i, c.To)
			}
		}
		for _, name := range slices.Sorted(maps.Keys(c.Args)) {
			if name == "" {
				return fieldErr(at(cp, "args"), "sceneformat: connection %d has an argument with an empty name", i)
			}
			if !scalarArg(c.Args[name]) {
				return fieldErr(at(cp, "args", name), "sceneformat: connection %d argument %q must be a bool, a finite number or a string, got %s", i, name, describeValue(c.Args[name]))
			}
		}
		key := [4]string{c.From, c.Event, c.To, c.Handler}
		if j, dup := seen[key]; dup {
			return fieldErr(cp, "sceneformat: connection %d repeats connection %d", i, j)
		}
		seen[key] = i
	}
	return nil
}

// scalarArg reports whether v is a value a connection argument may hold.
func scalarArg(v any) bool {
	switch x := v.(type) {
	case bool, string:
		return true
	case float64:
		return !math.IsNaN(x) && !math.IsInf(x, 0)
	case float32:
		return isFinite(x)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	default:
		return false
	}
}

// locateConnectionError sets the line and column of err, when it is a *ConnectionError, from the scene's YAML node tree.
func locateConnectionError(root *yaml.Node, err error) {
	var ce *ConnectionError
	if !errors.As(err, &ce) {
		return
	}
	n := resolveAlias(root)
	if n != nil && n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = resolveAlias(n.Content[0])
	}
	seq := mappingValue(n, "connections")
	if seq == nil || seq.Kind != yaml.SequenceNode || ce.Index >= len(seq.Content) {
		return
	}
	c := resolveAlias(seq.Content[ce.Index])
	pos := c
	if v := mappingValue(c, ce.Field); v != nil {
		pos = v
		if ce.Arg != "" {
			if a := mappingValue(v, ce.Arg); a != nil {
				pos = a
			}
		}
	}
	ce.Line, ce.Column = pos.Line, pos.Column
}

// cloneConnections deep copies conns, their arguments included.
func cloneConnections(conns []ConnectionDoc) []ConnectionDoc {
	if conns == nil {
		return nil
	}
	out := make([]ConnectionDoc, len(conns))
	for i, c := range conns {
		out[i] = c
		out[i].Args = cloneAnyMap(c.Args)
	}
	return out
}
