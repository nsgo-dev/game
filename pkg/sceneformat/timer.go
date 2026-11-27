package sceneformat

import (
	"errors"
	"time"
)

// TimerComponentName is the built-in timer component type name in scene and prefab YAML files.
const TimerComponentName = "timer"

// TimerComponent returns the ComponentType schema for the built-in "timer" component, the text counterpart of Godot's Timer node: an object with a timer component emits timeout on the game clock after seconds of it, once or repeating, and a scene's connections wire that to any handler with no Go code. Signals.Update advances the timers of a connected scene, and the start and stop handlers restart and halt them.
func TimerComponent() ComponentType {
	return ComponentType{
		Name: TimerComponentName,
		Doc:  "Emits timeout on the game clock after a number of seconds, once or repeating.",
		Fields: []Field{
			{Name: "seconds", Kind: FieldFloat, Default: 1.0, Min: Float64(0), Step: 0.1, Doc: "Seconds of game time between timeout events (>= 0); 0 fires on every update step."},
			{Name: "oneShot", Kind: FieldBool, Default: false, Doc: "When true, the timer stops after the first timeout."},
			{Name: "autostart", Kind: FieldBool, Default: false, Doc: "When true, the timer starts running as soon as the scene is connected."},
		},
		Events: []Event{
			{Name: "timeout", Doc: "Emitted when the timer reaches zero."},
		},
		Handlers: []Handler{
			{Name: "start", Doc: "Starts the timer over from its full seconds.", Func: func(c Call) { c.Signals.startTimer(c.Target.ID) }},
			{Name: "stop", Doc: "Stops the timer without resetting it.", Func: func(c Call) { c.Signals.stopTimer(c.Target.ID) }},
		},
	}
}

// RegisterTimerComponent registers the built-in "timer" component type on reg.
func RegisterTimerComponent(reg *ComponentRegistry) error {
	return reg.Register(TimerComponent())
}

// TimerComponentRegistry returns a new ComponentRegistry containing the built-in "timer" component.
func TimerComponentRegistry() *ComponentRegistry {
	reg := NewComponentRegistry()
	if err := RegisterTimerComponent(reg); err != nil {
		panic(err)
	}
	return reg
}

var builtinTimerReg = TimerComponentRegistry()

// sceneTimer is the run-time state of one object's built-in timer component, advanced by Signals.Update.
type sceneTimer struct {
	id        string
	seconds   float64
	oneShot   bool
	running   bool
	remaining float64
}

// addTimers reads every object's built-in timer component, in document order, and starts the ones with autostart. An object whose timer component does not match the built-in schema, such as a game's own type of that name, gets no timer.
func (s *Signals) addTimers(doc *SceneDoc) {
	for li := range doc.Layers {
		WalkObjects(doc.Layers[li].Objects, func(o, _ *ObjectDoc) bool {
			raw, ok := o.Components[TimerComponentName]
			if !ok {
				return true
			}
			m, ok := raw.(map[string]any)
			if !ok {
				return true
			}
			resolved, err := builtinTimerReg.Resolve(TimerComponentName, m)
			if err != nil {
				return true
			}
			seconds := resolved["seconds"].(float64)
			t := &sceneTimer{id: o.ID, seconds: seconds, oneShot: resolved["oneShot"].(bool), running: resolved["autostart"].(bool), remaining: seconds}
			s.timers = append(s.timers, t)
			if s.timerOf == nil {
				s.timerOf = make(map[string]*sceneTimer)
			}
			s.timerOf[o.ID] = t
			return true
		})
	}
}

// startTimer starts the object's timer over from its full seconds, as the timer component's start handler does.
func (s *Signals) startTimer(id string) {
	if s == nil {
		return
	}
	if t := s.timerOf[id]; t != nil {
		t.remaining = t.seconds
		t.running = true
	}
}

// stopTimer stops the object's timer without resetting it, as the timer component's stop handler does.
func (s *Signals) stopTimer(id string) {
	if s == nil {
		return
	}
	if t := s.timerOf[id]; t != nil {
		t.running = false
	}
}

// Update advances the scene's built-in timer components (RegisterTimerComponent) by dt of game time and enqueues the timeout event of each that reaches zero, in document order, for the next Flush to dispatch: a one-shot timer fires once and stops, a repeating one carries the overshoot into the next period so it does not drift, and one with seconds 0 fires once per Update. The timer of an object whose entity has been destroyed (SpawnConnected) stops. Call it once per update step, before Flush, such as in game.Loop's Update; the queued timeouts run at that Flush, so timer events stay in step with the events the step raised.
//
// Update returns an error, and advances nothing, when a handler or listener calls it: timers then advance only from the game's update, never inside an event's handlers.
func (s *Signals) Update(dt time.Duration) error {
	if s == nil {
		return errors.New("sceneformat: update on nil Signals")
	}
	if s.flushing || s.depth > 0 {
		return errors.New("sceneformat: update from a handler; Update runs from the game's update, and timers a handler would advance wait for it")
	}
	if dt <= 0 {
		return nil
	}
	step := dt.Seconds()
	for _, t := range s.timers {
		if !t.running {
			continue
		}
		if s.world != nil && !s.world.Alive(s.entityOf[t.id]) {
			t.running = false
			continue
		}
		t.remaining -= step
		if t.remaining > 0 {
			continue
		}
		if t.oneShot || t.seconds <= 0 {
			t.running = !t.oneShot
			t.remaining = 0
			// The timer component declares timeout, and a destroyed entity was stopped above.
			_ = s.Enqueue(t.id, "timeout", nil)
			continue
		}
		for t.remaining <= 0 {
			t.remaining += t.seconds
			_ = s.Enqueue(t.id, "timeout", nil)
		}
	}
	return nil
}
