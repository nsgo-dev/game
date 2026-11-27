package quest

import "github.com/nsgo-dev/game/pkg/flow"

// Event is a game event an objective can count: Kind and Target must match the objective's, and Amount below 1 counts as 1. Collected, Reached, Defeated and Talked are typed shorthands for it.
type Event struct {
	Kind   Kind
	Target string
	Amount int
}

// Collected reports that the player picked up Amount of Item (below 1 counts as 1). It advances collect objectives whose target is Item.
type Collected struct {
	Item   string
	Amount int
}

// Event returns c as an Event.
func (c Collected) Event() Event { return Event{Kind: KindCollect, Target: c.Item, Amount: c.Amount} }

// Reached reports that the player arrived at Place. It advances reach objectives whose target is Place.
type Reached struct {
	Place string
}

// Event returns r as an Event.
func (r Reached) Event() Event { return Event{Kind: KindReach, Target: r.Place} }

// Defeated reports that the player defeated Amount of Enemy (below 1 counts as 1). It advances defeat objectives whose target is Enemy.
type Defeated struct {
	Enemy  string
	Amount int
}

// Event returns d as an Event.
func (d Defeated) Event() Event { return Event{Kind: KindDefeat, Target: d.Enemy, Amount: d.Amount} }

// Talked reports that the player talked to Character. It advances talk objectives whose target is Character.
type Talked struct {
	Character string
}

// Event returns t as an Event.
func (t Talked) Event() Event { return Event{Kind: KindTalk, Target: t.Character} }

// Attach subscribes l to Event, Collected, Reached, Defeated and Talked events published on bus, and publishes every Update on bus as well as delivering it to OnUpdate. Attaching again replaces the previous bus. The returned function detaches l and is idempotent.
func (l *Log) Attach(bus *flow.Bus) (detach func()) {
	if l.detach != nil {
		l.detach()
	}
	unsubs := []func(){
		flow.Subscribe(bus, func(e Event) { l.Handle(e) }),
		flow.Subscribe(bus, func(e Collected) { l.Handle(e.Event()) }),
		flow.Subscribe(bus, func(e Reached) { l.Handle(e.Event()) }),
		flow.Subscribe(bus, func(e Defeated) { l.Handle(e.Event()) }),
		flow.Subscribe(bus, func(e Talked) { l.Handle(e.Event()) }),
	}
	l.bus = bus
	done := false
	d := func() {
		if done {
			return
		}
		done = true
		for _, u := range unsubs {
			u()
		}
		l.bus = nil
		l.detach = nil
	}
	l.detach = d
	return d
}
