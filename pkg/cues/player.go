package cues

// EventKind identifies a cue lifecycle event.
type EventKind uint8

const (
	EventExecuted EventKind = iota + 1
	EventAdded
	EventRemoved
)

// Event is a deterministic cue notification. Instance distinguishes repeated
// executions of the same tag on one step and Target identifies its owner.
type Event struct {
	Tag, Target, Instance string
	Kind                  EventKind
	Step                  uint64
}

// Active identifies a while-active cue present after restoring gameplay state.
type Active struct{ Tag, Target string }

// Hooks connects cue data to client presentation. Nil hooks are skipped.
type Hooks struct {
	Particles func(Particles, Event)
	Sound     func(Sound, Event)
	Shake     func(Shake, Event)
	Flash     func(Flash, Event)
	Animation func(string, Event)
	Stop      func(Cue, Event)
}

type activeKey struct{ tag, target string }
type onceKey struct {
	tag, target, instance string
	step                  uint64
}

// Player dispatches cues on a client. It has no graphics or audio dependency,
// so a headless server can keep cue data loaded without constructing one.
type Player struct {
	set    *Set
	hooks  Hooks
	byTag  map[string]Cue
	active map[activeKey]struct{}
	fired  map[onceKey]struct{}
}

// NewPlayer returns a player for set. Hooks may be zero-valued.
func NewPlayer(set *Set, hooks Hooks) (*Player, error) {
	if err := Validate(set); err != nil {
		return nil, err
	}
	p := &Player{set: set, hooks: hooks, byTag: make(map[string]Cue, len(set.Cues)), active: make(map[activeKey]struct{}), fired: make(map[onceKey]struct{})}
	for _, c := range set.Cues {
		p.byTag[c.Tag] = c
	}
	return p, nil
}

// Dispatch delivers one event. Executed and Added events start handlers once.
// Re-dispatching the same once event, as rollback re-simulation does, is ignored.
func (p *Player) Dispatch(e Event) {
	c, ok := p.byTag[e.Tag]
	if !ok {
		return
	}
	key := activeKey{e.Tag, e.Target}
	if e.Kind == EventRemoved {
		if _, ok := p.active[key]; ok {
			delete(p.active, key)
			if p.hooks.Stop != nil {
				p.hooks.Stop(c, e)
			}
		}
		return
	}
	if c.Once {
		k := onceKey{e.Tag, e.Target, e.Instance, e.Step}
		if _, ok := p.fired[k]; ok {
			return
		}
		p.fired[k] = struct{}{}
	}
	if c.WhileActive {
		if _, ok := p.active[key]; ok {
			return
		}
		p.active[key] = struct{}{}
	}
	p.start(c, e)
}

func (p *Player) start(c Cue, e Event) {
	if c.Particles != nil && p.hooks.Particles != nil {
		p.hooks.Particles(*c.Particles, e)
	}
	if c.Sound != nil && p.hooks.Sound != nil {
		p.hooks.Sound(*c.Sound, e)
	}
	if c.Shake != nil && p.hooks.Shake != nil {
		p.hooks.Shake(*c.Shake, e)
	}
	if c.Flash != nil && p.hooks.Flash != nil {
		p.hooks.Flash(*c.Flash, e)
	}
	if c.Animation != "" && p.hooks.Animation != nil {
		p.hooks.Animation(c.Animation, e)
	}
}

// Reconcile replaces the while-active set after rollback or state restore.
// Missing cues stop first, then newly present cues start in input order.
func (p *Player) Reconcile(states []Active, step uint64) {
	want := make(map[activeKey]struct{}, len(states))
	for _, s := range states {
		want[activeKey{s.Tag, s.Target}] = struct{}{}
	}
	for k := range p.active {
		if _, ok := want[k]; !ok {
			p.Dispatch(Event{Tag: k.tag, Target: k.target, Kind: EventRemoved, Step: step})
		}
	}
	for _, s := range states {
		k := activeKey{s.Tag, s.Target}
		if _, ok := p.active[k]; !ok {
			p.Dispatch(Event{Tag: s.Tag, Target: s.Target, Kind: EventAdded, Step: step})
		}
	}
}

// Reset clears deduplication and active state, stopping currently active cues.
func (p *Player) Reset(step uint64) {
	for k := range p.active {
		p.Dispatch(Event{Tag: k.tag, Target: k.target, Kind: EventRemoved, Step: step})
	}
	clear(p.active)
	clear(p.fired)
}
