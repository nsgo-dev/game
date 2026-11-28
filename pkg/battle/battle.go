// Package battle resolves deterministic, presentation-independent turn-based battles between a party and a troop.
//
// # Turns
//
// New copies the party's and the troop's Fighters into a Runner. Each call to Resolve is one turn: the game supplies a Command per acting fighter, from either side, and the Runner orders them by Agility, highest first, breaking ties by side (the party before the troop) and then by the fighter's index, so the order never depends on the order of the command list. Commands whose actor is out of range or already defeated, including one defeated earlier in the same turn, are skipped.
//
// # Actions
//
// An Action's Scope picks its targets relative to the actor's side. OneEnemy and AllEnemies deal Power plus the actor's Attack minus the target's Defense, at least 1, to living opponents, each target rolling for a hit against Accuracy and then for a critical against Rules.CriticalChance; a critical doubles the damage. Self, OneAlly and AllAllies heal Power HP on living allies without a roll, never above MaxHP. An Action with a Cost spends that much MP before it resolves, and an actor without enough MP does nothing.
//
// # Events and outcome
//
// Resolve returns the Events of the turn in order, for the presentation layer to animate in its own time. When the last living troop member falls the battle is won, and when the last living party member falls it is lost; Resolve stops at that point and returns nil from then on.
//
// # Determinism and snapshots
//
// Every roll is drawn from the Runner's random.Stream, so a seed and the same commands replay the same events on every platform. Snapshot copies the fighters, the turn counter, the outcome and the stream's state; Restore puts them back, so a battle can be saved mid-fight or rolled back and replayed. A Snapshot marshals to JSON for save slots.
package battle

import (
	"errors"
	"sort"

	"github.com/nsgo-dev/game/pkg/random"
)

// Side names one of the two sides of a battle.
type Side int

const (
	// PartySide is the player's party.
	PartySide Side = iota
	// TroopSide is the enemy troop.
	TroopSide
)

// opponent returns the other side.
func (s Side) opponent() Side {
	if s == PartySide {
		return TroopSide
	}
	return PartySide
}

// Scope is which fighters an Action affects, relative to its actor's side.
type Scope string

const (
	// Self targets the actor.
	Self Scope = "self"
	// OneEnemy targets the living opponent at Command.Target.
	OneEnemy Scope = "one_enemy"
	// AllEnemies targets every living opponent.
	AllEnemies Scope = "all_enemies"
	// OneAlly targets the living ally at Command.Target, which may be the actor.
	OneAlly Scope = "one_ally"
	// AllAllies targets every living ally, the actor included.
	AllAllies Scope = "all_allies"
)

// hostile reports whether s targets the actor's opponents.
func (s Scope) hostile() bool { return s == OneEnemy || s == AllEnemies }

// Fighter is one battler's stats and current state.
type Fighter struct {
	// ID identifies the fighter to the game.
	ID string `json:"id"`
	// Name is the fighter's display name.
	Name string `json:"name,omitempty"`
	// HP is the fighter's current hit points; a fighter starts the battle alive when HP is above 0.
	HP int `json:"hp"`
	// MaxHP caps healing; 0 means healing is not capped.
	MaxHP int `json:"max_hp,omitempty"`
	// MP is the fighter's current magic points, spent by an Action's Cost.
	MP int `json:"mp,omitempty"`
	// Attack is added to the Power of the fighter's hostile actions.
	Attack int `json:"attack,omitempty"`
	// Defense is subtracted from the damage the fighter takes.
	Defense int `json:"defense,omitempty"`
	// Agility orders the fighter's command within a turn, highest first.
	Agility int `json:"agility,omitempty"`
	// Alive reports whether the fighter can still act and be targeted.
	Alive bool `json:"alive"`
}

// Action is a skill, an attack or an item use.
type Action struct {
	// ID identifies the action to the game.
	ID string
	// Scope picks the action's targets.
	Scope Scope
	// Power is the action's base damage for hostile scopes and its healing otherwise.
	Power int
	// Cost is the MP the actor spends to use the action.
	Cost int
	// Accuracy is the chance in (0, 1] that a hostile action hits each target; 0 means it always hits.
	Accuracy float64
}

// Ref names one fighter by side and index.
type Ref struct {
	// Side is the fighter's side.
	Side Side
	// Index is the fighter's index in its side's slice.
	Index int
}

// Command is one fighter's choice for a turn.
type Command struct {
	// Actor is the fighter using the action.
	Actor Ref
	// Action is the action used.
	Action Action
	// Target is the index of the target on the side the scope selects, for OneEnemy and OneAlly; the other scopes ignore it.
	Target int
}

// EventKind names what happened in an Event.
type EventKind string

const (
	// Missed is a hostile action missing its target.
	Missed EventKind = "missed"
	// Damaged is a target losing Amount HP; Critical marks a critical hit.
	Damaged EventKind = "damaged"
	// Healed is a target gaining Amount HP.
	Healed EventKind = "healed"
	// Defeated is a target falling to 0 HP.
	Defeated EventKind = "defeated"
	// NoMP is an actor lacking the MP its action costs; the action does nothing.
	NoMP EventKind = "no_mp"
	// Won is the last living troop member falling.
	Won EventKind = "won"
	// Lost is the last living party member falling.
	Lost EventKind = "lost"
)

// Event is one thing that happened while a turn resolved.
type Event struct {
	// Kind is what happened.
	Kind EventKind
	// Actor is the fighter whose command caused the event; it is the zero Ref for Won and Lost.
	Actor Ref
	// Target is the fighter affected; it is the zero Ref for NoMP, Won and Lost.
	Target Ref
	// Amount is the HP lost or gained, for Damaged and Healed.
	Amount int
	// Critical reports whether a Damaged event was a critical hit.
	Critical bool
}

// Rules are the battle-wide tuning values.
type Rules struct {
	// CriticalChance is the chance in [0, 1] that a hit is critical and deals double damage.
	CriticalChance float64
}

// Runner holds a battle's state and resolves its turns.
type Runner struct {
	// Party is the player's fighters.
	Party []Fighter
	// Troop is the enemy fighters.
	Troop []Fighter
	// Rules are the battle's tuning values.
	Rules Rules
	// RNG is the stream every roll is drawn from.
	RNG *random.Stream
	// Turn counts the turns resolved so far.
	Turn int
	// Won reports whether the troop has been defeated.
	Won bool
	// Lost reports whether the party has been defeated.
	Lost bool
}

// New returns a Runner over copies of party and troop, marking each fighter alive when its HP is above 0. A nil rng is replaced with random.New(0).
func New(party, troop []Fighter, rules Rules, rng *random.Stream) *Runner {
	p, t := clone(party), clone(troop)
	for i := range p {
		p[i].Alive = p[i].HP > 0
	}
	for i := range t {
		t[i].Alive = t[i].HP > 0
	}
	if rng == nil {
		rng = random.New(0)
	}
	return &Runner{Party: p, Troop: t, Rules: rules, RNG: rng}
}

func clone(in []Fighter) []Fighter { return append([]Fighter(nil), in...) }

// side returns the fighters of s.
func (r *Runner) side(s Side) []Fighter {
	if s == PartySide {
		return r.Party
	}
	return r.Troop
}

// fighter returns the fighter ref names, or nil when it is out of range.
func (r *Runner) fighter(ref Ref) *Fighter {
	fs := r.side(ref.Side)
	if (ref.Side != PartySide && ref.Side != TroopSide) || ref.Index < 0 || ref.Index >= len(fs) {
		return nil
	}
	return &fs[ref.Index]
}

// Order returns the commands whose actor exists, sorted into the order Resolve runs them: by the actor's Agility, highest first, then the party before the troop, then by the actor's index.
func (r *Runner) Order(commands []Command) []Command {
	out := make([]Command, 0, len(commands))
	for _, c := range commands {
		if r.fighter(c.Actor) != nil {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Actor, out[j].Actor
		if ag, bg := r.fighter(a).Agility, r.fighter(b).Agility; ag != bg {
			return ag > bg
		}
		if a.Side != b.Side {
			return a.Side < b.Side
		}
		return a.Index < b.Index
	})
	return out
}

// Resolve runs one turn of commands and returns its events. It returns nil once the battle is won or lost.
func (r *Runner) Resolve(commands []Command) []Event {
	if r.Won || r.Lost {
		return nil
	}
	r.Turn++
	var events []Event
	for _, c := range r.Order(commands) {
		actor := r.fighter(c.Actor)
		if !actor.Alive {
			continue
		}
		if c.Action.Cost > 0 {
			if actor.MP < c.Action.Cost {
				events = append(events, Event{Kind: NoMP, Actor: c.Actor})
				continue
			}
			actor.MP -= c.Action.Cost
		}
		for _, t := range r.targets(c) {
			if c.Action.Scope.hostile() {
				events = r.strike(events, c, *actor, t)
			} else {
				events = r.heal(events, c, t)
			}
			if r.Won || r.Lost {
				return events
			}
		}
	}
	return events
}

// strike resolves a hostile action of actor against target.
func (r *Runner) strike(events []Event, c Command, actor Fighter, target Ref) []Event {
	if a := c.Action.Accuracy; a > 0 && a < 1 && !r.RNG.Chance(a) {
		return append(events, Event{Kind: Missed, Actor: c.Actor, Target: target})
	}
	f := r.fighter(target)
	d := max(c.Action.Power+actor.Attack-f.Defense, 1)
	crit := r.Rules.CriticalChance > 0 && r.RNG.Chance(r.Rules.CriticalChance)
	if crit {
		d *= 2
	}
	d = min(d, f.HP)
	f.HP -= d
	events = append(events, Event{Kind: Damaged, Actor: c.Actor, Target: target, Amount: d, Critical: crit})
	if f.HP > 0 {
		return events
	}
	f.Alive = false
	events = append(events, Event{Kind: Defeated, Actor: c.Actor, Target: target})
	switch {
	case !anyAlive(r.Troop):
		r.Won = true
		events = append(events, Event{Kind: Won})
	case !anyAlive(r.Party):
		r.Lost = true
		events = append(events, Event{Kind: Lost})
	}
	return events
}

// heal resolves a friendly action on target.
func (r *Runner) heal(events []Event, c Command, target Ref) []Event {
	f := r.fighter(target)
	n := max(c.Action.Power, 0)
	if f.MaxHP > 0 {
		n = min(n, max(f.MaxHP-f.HP, 0))
	}
	f.HP += n
	return append(events, Event{Kind: Healed, Actor: c.Actor, Target: target, Amount: n})
}

// targets returns the living fighters c's scope selects.
func (r *Runner) targets(c Command) []Ref {
	switch c.Action.Scope {
	case Self:
		return []Ref{c.Actor}
	case OneEnemy, OneAlly:
		side := c.Actor.Side
		if c.Action.Scope == OneEnemy {
			side = side.opponent()
		}
		ref := Ref{Side: side, Index: c.Target}
		if f := r.fighter(ref); f != nil && f.Alive {
			return []Ref{ref}
		}
		return nil
	case AllEnemies, AllAllies:
		side := c.Actor.Side
		if c.Action.Scope == AllEnemies {
			side = side.opponent()
		}
		var out []Ref
		for i, f := range r.side(side) {
			if f.Alive {
				out = append(out, Ref{Side: side, Index: i})
			}
		}
		return out
	}
	return nil
}

func anyAlive(fs []Fighter) bool {
	for _, f := range fs {
		if f.Alive {
			return true
		}
	}
	return false
}

// Snapshot is a copy of a battle's mutable state, from Runner.Snapshot.
type Snapshot struct {
	// Party is the party's fighters.
	Party []Fighter `json:"party"`
	// Troop is the troop's fighters.
	Troop []Fighter `json:"troop"`
	// Turn is the number of turns resolved.
	Turn int `json:"turn"`
	// Won reports whether the battle was won.
	Won bool `json:"won,omitempty"`
	// Lost reports whether the battle was lost.
	Lost bool `json:"lost,omitempty"`
	// RNG is the state of the battle's stream.
	RNG random.State `json:"rng"`
}

// Snapshot returns a copy of r's mutable state.
func (r *Runner) Snapshot() Snapshot {
	return Snapshot{Party: clone(r.Party), Troop: clone(r.Troop), Turn: r.Turn, Won: r.Won, Lost: r.Lost, RNG: r.RNG.State()}
}

// Restore replaces r's mutable state with a copy of s. It returns an error for a nil Runner or a Runner without a stream.
func (r *Runner) Restore(s Snapshot) error {
	if r == nil || r.RNG == nil {
		return errors.New("battle: Restore on a nil Runner or a Runner without a stream")
	}
	r.Party = clone(s.Party)
	r.Troop = clone(s.Troop)
	r.Turn = s.Turn
	r.Won = s.Won
	r.Lost = s.Lost
	r.RNG.SetState(s.RNG)
	return nil
}
