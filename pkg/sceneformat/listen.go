package sceneformat

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// ListenOptions configures one Signals.Listen subscription.
type ListenOptions struct {
	// Once runs the listener only the first time the event fires after Listen, as Godot's CONNECT_ONE_SHOT does.
	Once bool
	// Deferred queues each run for the Flush after the event instead of running inside it, as Godot's CONNECT_DEFERRED does; one emission queues one run, and a once listener is used up by the emission that queues it.
	Deferred bool
}

// listener is one subscription Listen added. A wildcard subscription holds one listenerEntry per object that declares the event, all sharing the listener, so once and stop end every entry at once. spent marks a once listener whose first run has run or is queued; done is stop's, and a deferred run queued before stop is dropped at the Flush.
type listener struct {
	fn             func(Call)
	once, deferred bool
	spent, done    bool
}

// listenerEntry binds a listener to one object's event: the object's ID and document and the event's component and name.
type listenerEntry struct {
	l      *listener
	from   string
	source *ObjectDoc
	event  memberRef
}

// Listen calls fn when the object with ID from emits event, written as Emit takes it: a bare name such as "pressed", or "button.pressed" to say which component when several of the object's components declare it. An empty from listens to every object of the scene that declares the event, so game systems that no scene file should name, such as audio, analytics and achievements, hear every object without a connection per scene. The listener runs after the scene's connections for the same event, in the order Listen added it among the event's listeners. Its Call carries the Signals, the context given to Connect, the source object, the event's component and name, the payload and, when SpawnConnected made the Signals, the source's entity; Target, Component, Handler, Args and TargetEntity stay unset, a listener not being a connection.
//
// Listen checks from and event as Emit does and refuses an unknown object or event at the call, so a misspelt name fails at once; an empty from refuses an event no object of the scene declares. The returned stop ends the subscription: it is idempotent, and safe to call from inside a handler or listener. A listener added while an event runs takes effect from the next one, and a listener on an object never runs for it once its entity has been destroyed (SpawnConnected), as connections do.
func (s *Signals) Listen(from, event string, fn func(Call), opts ListenOptions) (stop func(), err error) {
	if s == nil {
		return nil, errors.New("sceneformat: listen on nil Signals")
	}
	if fn == nil {
		return nil, fmt.Errorf("sceneformat: listen %q: the listener func is nil", event)
	}
	l := &listener{fn: fn, once: opts.Once, deferred: opts.Deferred}
	if from != "" {
		o, m, err := s.resolveEvent("listen", from, event)
		if err != nil {
			return nil, err
		}
		s.addListener(&listenerEntry{l: l, from: from, source: o, event: m})
		return func() { l.done = true }, nil
	}
	if !validMemberRef(event) {
		return nil, fmt.Errorf("sceneformat: listen %q: event must be a name or component.name", event)
	}
	var entries []*listenerEntry
	destroyed := 0
	for _, id := range slices.Sorted(maps.Keys(s.names)) {
		m, ok := s.names[id][event]
		if !ok || m.component == "" {
			// Not declared, or a bare name two of the object's components declare, which Emit from it refuses too.
			continue
		}
		if s.world != nil && !s.world.Alive(s.entityOf[id]) {
			destroyed++
			continue
		}
		entries = append(entries, &listenerEntry{l: l, from: id, source: s.objects[id], event: m})
	}
	if len(entries) == 0 {
		if destroyed > 0 {
			return nil, fmt.Errorf("sceneformat: listen %q: the entity of every object that declares the event has been destroyed", event)
		}
		return nil, fmt.Errorf("sceneformat: listen %q: no object of the scene declares the event", event)
	}
	for _, e := range entries {
		s.addListener(e)
	}
	return func() { l.done = true }, nil
}

// addListener appends e to the entries of its event's key, keeping Listen's call order within each key.
func (s *Signals) addListener(e *listenerEntry) {
	if s.listeners == nil {
		s.listeners = make(map[string][]*listenerEntry)
	}
	k := e.event.key()
	s.listeners[k] = append(s.listeners[k], e)
}
