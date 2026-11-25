package animclip

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// ClipResolver loads an animation clip by the path or key written in a StateDoc.Clip field.
// *resource.Library implements ClipResolver.
type ClipResolver interface {
	Animation(path string) (*resource.Animation, error)
}

// ClipMap is an in-memory ClipResolver keyed by clip path or name, handy for tests and generated clips.
type ClipMap map[string]*resource.Animation

// Animation returns the clip registered under path or an error when absent.
func (m ClipMap) Animation(path string) (*resource.Animation, error) {
	c, ok := m[path]
	if !ok || c == nil {
		return nil, fmt.Errorf("animclip: clip %q not found", path)
	}
	return c, nil
}

// FSClips resolves animation clip YAML files from an fs.FS relative to its root.
type FSClips struct {
	FS fs.FS
}

// Animation reads and parses the animation resource at path from FS.
func (r FSClips) Animation(path string) (*resource.Animation, error) {
	if r.FS == nil {
		return nil, errors.New("animclip: nil file system for clips")
	}
	data, err := fs.ReadFile(r.FS, path)
	if err != nil {
		return nil, fmt.Errorf("animclip: read clip %q: %w", path, err)
	}
	return resource.ParseAs[*resource.Animation](data)
}

// StatesResolver loads a *.states.yaml StateMachineDoc by its project-relative path.
type StatesResolver interface {
	StateMachine(path string) (*StateMachineDoc, error)
}

// StatesMap is an in-memory StatesResolver keyed by path.
type StatesMap map[string]*StateMachineDoc

// StateMachine returns the document registered under path or an error when absent.
func (m StatesMap) StateMachine(path string) (*StateMachineDoc, error) {
	doc, ok := m[path]
	if !ok || doc == nil {
		return nil, fmt.Errorf("animclip: state machine %q not found", path)
	}
	return doc, nil
}

// FSStates resolves *.states.yaml files from an fs.FS relative to its root.
type FSStates struct {
	FS fs.FS
}

// StateMachine reads and parses the *.states.yaml file at path from FS.
func (r FSStates) StateMachine(path string) (*StateMachineDoc, error) {
	if r.FS == nil {
		return nil, errors.New("animclip: nil file system for state machines")
	}
	data, err := fs.ReadFile(r.FS, path)
	if err != nil {
		return nil, fmt.Errorf("animclip: read state machine %q: %w", path, err)
	}
	return ParseStateMachine(data)
}

type compiledState struct {
	doc    StateDoc
	player *Player
	speed  float64
}

type compiledTransition struct {
	doc  TransitionDoc
	cond *Condition
}

// StateMachine plays a *.states.yaml sprite animation graph on deterministic game.Loop fixed steps.
// The game (or editor preview) sets named float, bool and trigger parameters and calls Update(dt).
type StateMachine struct {
	// OnTransition, when set, is called synchronously whenever the machine switches states.
	OnTransition func(from, to string)
	// OnEvent, when set, is called synchronously whenever the active state's clip crosses a named cue event.
	OnEvent func(state, event string)

	doc         *StateMachineDoc
	states      map[string]*compiledState
	transitions []compiledTransition

	paramTypes map[string]ParamType
	floats     map[string]float64
	bools      map[string]bool
	triggers   map[string]bool

	current   string
	previous  string
	playing   bool
	speedMult float64
}

// NewStateMachine validates doc, resolves every state's clip through clips, builds a Player for each state bound via bind, and starts at doc.Entry (playing and posed at time 0).
func NewStateMachine(doc *StateMachineDoc, clips ClipResolver, reg *sceneformat.ComponentRegistry, bind Binder) (*StateMachine, error) {
	if err := ValidateStateMachine(doc); err != nil {
		return nil, err
	}
	if clips == nil {
		return nil, errors.New("animclip: nil clip resolver")
	}
	if bind == nil {
		return nil, errors.New("animclip: nil binder")
	}
	cloned := doc.Clone()
	paramTypes := cloned.paramTypeMap()

	sm := &StateMachine{
		doc:        cloned,
		states:     make(map[string]*compiledState, len(cloned.States)),
		paramTypes: paramTypes,
		floats:     make(map[string]float64),
		bools:      make(map[string]bool),
		triggers:   make(map[string]bool),
		playing:    true,
		speedMult:  1,
	}

	for _, st := range cloned.States {
		clip, err := clips.Animation(st.Clip)
		if err != nil {
			return nil, fmt.Errorf("animclip: state %q clip %q: %w", st.Name, st.Clip, err)
		}
		player, err := NewPlayer(clip, reg, bind)
		if err != nil {
			return nil, fmt.Errorf("animclip: state %q clip %q: %w", st.Name, st.Clip, err)
		}
		if st.Loop != nil {
			player.SetLoop(*st.Loop)
		}
		stateName := st.Name
		player.OnEvent = func(ev string) {
			if sm.OnEvent != nil {
				sm.OnEvent(stateName, ev)
			}
		}
		spd := float64(st.Speed)
		if spd <= 0 {
			spd = 1
		}
		sm.states[st.Name] = &compiledState{doc: st, player: player, speed: spd}
	}

	sm.transitions = make([]compiledTransition, len(cloned.Transitions))
	for i, tr := range cloned.Transitions {
		var cond *Condition
		if tr.When != "" {
			var err error
			cond, err = CompileCondition(tr.When, paramTypes)
			if err != nil {
				return nil, fmt.Errorf("animclip: transition %d (%s -> %s): %w", i, tr.From, tr.To, err)
			}
		}
		sm.transitions[i] = compiledTransition{doc: tr, cond: cond}
	}

	sm.Reset()
	return sm, nil
}

// NewAnimator builds a StateMachine for objectID in built from its built-in "animator" component, resolving the states file via states and each state's clip via clips.
func NewAnimator(built *sceneformat.BuiltScene, objectID string, states StatesResolver, clips ClipResolver, reg *sceneformat.ComponentRegistry) (*StateMachine, error) {
	if built == nil {
		return nil, errors.New("animclip: nil built scene")
	}
	if states == nil {
		return nil, errors.New("animclip: nil states resolver")
	}
	bo := built.Object(objectID)
	if bo == nil || bo.Doc == nil {
		return nil, fmt.Errorf("animclip: scene has no object %q", objectID)
	}
	cfg, ok, err := sceneformat.ResolveAnimatorComponent(bo.Doc)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("animclip: object %q has no animator component", objectID)
	}
	if cfg.States == "" {
		return nil, fmt.Errorf("animclip: object %q animator component has empty states path", objectID)
	}
	doc, err := states.StateMachine(cfg.States)
	if err != nil {
		return nil, fmt.Errorf("animclip: object %q animator states %q: %w", objectID, cfg.States, err)
	}
	sm, err := NewStateMachine(doc, clips, reg, BindScene(built, objectID))
	if err != nil {
		return nil, err
	}
	if cfg.Speed >= 0 {
		sm.SetSpeed(cfg.Speed)
	}
	if cfg.StartClip != "" {
		if !sm.SetState(cfg.StartClip) {
			// Also allow StartClip to match a state's clip path.
			matched := false
			for _, st := range doc.States {
				if st.Clip == cfg.StartClip {
					sm.SetState(st.Name)
					matched = true
					break
				}
			}
			if !matched {
				return nil, fmt.Errorf("animclip: object %q animator startClip %q matches no state or clip in %q", objectID, cfg.StartClip, cfg.States)
			}
		}
	}
	if !cfg.AutoPlay {
		sm.Pause()
	}
	return sm, nil
}

// Doc returns a copy of the StateMachineDoc this player runs.
func (sm *StateMachine) Doc() *StateMachineDoc { return sm.doc.Clone() }

// State returns the name of the currently active state.
func (sm *StateMachine) State() string { return sm.current }

// PreviousState returns the name of the state active before the last transition, or "" before any transition has fired.
func (sm *StateMachine) PreviousState() string { return sm.previous }

// ActivePlayer returns the clip Player of the currently active state, or nil.
func (sm *StateMachine) ActivePlayer() *Player {
	if cs := sm.states[sm.current]; cs != nil {
		return cs.player
	}
	return nil
}

// StateTime returns the active clip's current playback time.
func (sm *StateMachine) StateTime() time.Duration {
	if p := sm.ActivePlayer(); p != nil {
		return p.Time()
	}
	return 0
}

// StateLength returns the active clip's total length.
func (sm *StateMachine) StateLength() time.Duration {
	if p := sm.ActivePlayer(); p != nil {
		return p.Length()
	}
	return 0
}

// StateDone reports whether the active non-looping clip has played to its end.
func (sm *StateMachine) StateDone() bool {
	if p := sm.ActivePlayer(); p != nil {
		return p.Done()
	}
	return false
}

// Playing reports whether playback advances on Update.
func (sm *StateMachine) Playing() bool { return sm.playing }

// Play resumes fixed-step playback.
func (sm *StateMachine) Play() {
	sm.playing = true
	if p := sm.ActivePlayer(); p != nil {
		p.Play()
	}
}

// Pause halts playback at the current state and time.
func (sm *StateMachine) Pause() {
	sm.playing = false
	if p := sm.ActivePlayer(); p != nil {
		p.Pause()
	}
}

// SetSpeed sets the global playback speed multiplier (>= 0).
func (sm *StateMachine) SetSpeed(speed float64) {
	if math.IsNaN(speed) || math.IsInf(speed, 0) || speed < 0 {
		return
	}
	sm.speedMult = speed
}

// Speed returns the global playback speed multiplier.
func (sm *StateMachine) Speed() float64 { return sm.speedMult }

// Reset restores all parameters to their defaults, clears triggers, re-enters doc.Entry and poses frame 0.
func (sm *StateMachine) Reset() {
	clear(sm.floats)
	clear(sm.bools)
	clear(sm.triggers)
	for _, p := range sm.doc.Parameters {
		switch p.Type {
		case ParamFloat:
			if v, ok := toFloat64(p.Default); ok {
				sm.floats[p.Name] = v
			} else {
				sm.floats[p.Name] = 0
			}
		case ParamBool:
			if v, ok := p.Default.(bool); ok {
				sm.bools[p.Name] = v
			} else {
				sm.bools[p.Name] = false
			}
		case ParamTrigger:
			sm.triggers[p.Name] = false
		}
	}
	sm.previous = ""
	sm.current = sm.doc.Entry
	if cs := sm.states[sm.current]; cs != nil {
		cs.player.Restart()
		if !sm.playing {
			cs.player.Pause()
		}
	}
}

// SetState switches immediately to state name, restarting its clip from time 0 and posing frame 0.
// Returns false when name is not a state in the machine.
func (sm *StateMachine) SetState(name string) bool {
	cs, ok := sm.states[name]
	if !ok {
		return false
	}
	if name != sm.current {
		sm.previous = sm.current
	}
	sm.current = name
	cs.player.Restart()
	if !sm.playing {
		cs.player.Pause()
	}
	return true
}

// SetFloat sets a declared float parameter to v. Non-finite values and unknown/non-float names are ignored.
func (sm *StateMachine) SetFloat(name string, v float64) {
	if sm.paramTypes[name] != ParamFloat || math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	sm.floats[name] = v
}

// Float returns the current value of float parameter name, or 0 when unset.
func (sm *StateMachine) Float(name string) float64 { return sm.floats[name] }

// SetBool sets a declared bool parameter to v. Unknown/non-bool names are ignored.
func (sm *StateMachine) SetBool(name string, v bool) {
	if sm.paramTypes[name] != ParamBool {
		return
	}
	sm.bools[name] = v
}

// Bool returns the current value of bool or trigger parameter name.
func (sm *StateMachine) Bool(name string) bool {
	if sm.paramTypes[name] == ParamTrigger {
		return sm.triggers[name]
	}
	return sm.bools[name]
}

// Trigger pulses a declared trigger parameter for the next Update step.
func (sm *StateMachine) Trigger(name string) {
	if sm.paramTypes[name] != ParamTrigger {
		return
	}
	sm.triggers[name] = true
}

// ResetTrigger clears a latched trigger parameter before Update consumes it.
func (sm *StateMachine) ResetTrigger(name string) {
	if sm.paramTypes[name] != ParamTrigger {
		return
	}
	sm.triggers[name] = false
}

// Params returns a snapshot map of all declared parameters and their current values.
func (sm *StateMachine) Params() map[string]any {
	out := make(map[string]any, len(sm.paramTypes))
	for name, pt := range sm.paramTypes {
		switch pt {
		case ParamFloat:
			out[name] = sm.floats[name]
		case ParamBool:
			out[name] = sm.bools[name]
		case ParamTrigger:
			out[name] = sm.triggers[name]
		}
	}
	return out
}

// Update advances the state machine by one fixed step dt: evaluates transitions in declaration order, advances the active clip player (firing clip events), checks clip-end transitions if the clip finished on this step, and resets all trigger pulses at the end of the step.
func (sm *StateMachine) Update(dt time.Duration) {
	if !sm.playing || dt <= 0 {
		return
	}
	defer sm.clearTriggers()

	cs := sm.states[sm.current]
	if cs == nil {
		return
	}

	transitioned := sm.evalTransitions(false)
	cs = sm.states[sm.current]
	if cs == nil {
		return
	}

	scaled := time.Duration(math.Round(float64(dt) * cs.speed * sm.speedMult))
	if scaled > 0 {
		cs.player.Update(scaled)
	}

	// If a non-looping clip reached its end during this step and no transition already fired, take a matching onEnd transition immediately so there is no one-step stall.
	if !transitioned && cs.player.Done() {
		sm.evalTransitions(true)
	}
}

func (sm *StateMachine) evalTransitions(onlyOnEnd bool) bool {
	cs := sm.states[sm.current]
	done := cs != nil && cs.player.Done()
	for i := range sm.transitions {
		tr := &sm.transitions[i]
		if tr.doc.From != AnyState && tr.doc.From != sm.current {
			continue
		}
		if tr.doc.To == sm.current {
			continue
		}
		if onlyOnEnd && !tr.doc.OnEnd {
			continue
		}
		if tr.doc.OnEnd && !done {
			continue
		}
		if tr.cond != nil && !tr.cond.Eval(sm.getFloat, sm.getBool) {
			continue
		}
		from := sm.current
		sm.previous = from
		sm.current = tr.doc.To
		if next := sm.states[sm.current]; next != nil {
			next.player.Restart()
		}
		if sm.OnTransition != nil {
			sm.OnTransition(from, sm.current)
		}
		return true
	}
	return false
}

func (sm *StateMachine) getFloat(name string) float64 { return sm.floats[name] }

func (sm *StateMachine) getBool(name string) bool {
	if sm.paramTypes[name] == ParamTrigger {
		return sm.triggers[name]
	}
	return sm.bools[name]
}

func (sm *StateMachine) clearTriggers() {
	for k := range maps.Keys(sm.triggers) {
		sm.triggers[k] = false
	}
}
