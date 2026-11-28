// Package battle provides a deterministic, presentation-independent turn battle runner.
package battle

import (
	"errors"
	"fmt"
	"math/rand"
	"slices"
)

type Mode uint8

const (
	ModeTurn Mode = iota
	ModeGauge
)

type Scope uint8

const (
	ScopeOneEnemy Scope = iota
	ScopeAllEnemies
	ScopeOneAlly
	ScopeAllAllies
	ScopeSelf
)

type Actor struct {
	ID                                             string
	HP, MaxHP, MP, MaxMP, Attack, Defense, Agility int
	Alive                                          bool
}
type Skill struct {
	ID            string
	Scope         Scope
	CostMP, Power int
	Hit, Critical float64
}
type Command struct {
	Actor  int
	Skill  Skill
	Target int
}
type EventKind uint8

const (
	EventAct EventKind = iota
	EventMiss
	EventDamage
	EventHeal
	EventDefeated
	EventWon
	EventLost
)

type Event struct {
	Kind          EventKind
	Actor, Target int
	Amount        int
	Skill         string
}
type Rules struct {
	Mode   Mode
	Damage func(attacker, target Actor, skill Skill) int
}
type Snapshot struct {
	Party  []Actor
	Troop  []Actor
	Turn   int
	Next   int
	Events []Event
}
type Battle struct {
	Party, Troop []Actor
	Rules        Rules
	rng          *rand.Rand
	turn, next   int
	events       []Event
}

func New(party, troop []Actor, rules Rules, seed int64) (*Battle, error) {
	if len(party) == 0 || len(troop) == 0 {
		return nil, errors.New("battle: party and troop must not be empty")
	}
	b := &Battle{Party: cloneActors(party), Troop: cloneActors(troop), Rules: rules, rng: rand.New(rand.NewSource(seed))}
	for i := range b.Party {
		b.Party[i].Alive = true
	}
	for i := range b.Troop {
		b.Troop[i].Alive = true
	}
	return b, nil
}
func cloneActors(in []Actor) []Actor {
	out := slices.Clone(in)
	for i := range out {
		if out[i].MaxHP <= 0 {
			out[i].MaxHP = out[i].HP
		}
		if out[i].MaxMP < 0 {
			out[i].MaxMP = 0
		}
	}
	return out
}
func (b *Battle) Order() []int {
	type entry struct{ i, agi int }
	var all []entry
	for i, a := range b.Party {
		if a.Alive {
			all = append(all, entry{i, a.Agility})
		}
	}
	for i, a := range b.Troop {
		if a.Alive {
			all = append(all, entry{len(b.Party) + i, a.Agility})
		}
	}
	slices.SortStableFunc(all, func(a, c entry) int {
		if a.agi > c.agi {
			return -1
		}
		if a.agi < c.agi {
			return 1
		}
		return a.i - c.i
	})
	out := make([]int, len(all))
	for i, e := range all {
		out[i] = e.i
	}
	return out
}
func (b *Battle) Resolve(cmd Command) ([]Event, error) {
	if cmd.Actor < 0 || cmd.Actor >= len(b.Party) {
		return nil, fmt.Errorf("battle: actor %d out of range", cmd.Actor)
	}
	a := &b.Party[cmd.Actor]
	if !a.Alive {
		return nil, errors.New("battle: defeated actor cannot act")
	}
	if a.MP < cmd.Skill.CostMP {
		return nil, errors.New("battle: not enough MP")
	}
	targets, err := b.targets(cmd.Actor, cmd.Skill.Scope, cmd.Target)
	if err != nil {
		return nil, err
	}
	a.MP -= cmd.Skill.CostMP
	events := []Event{{Kind: EventAct, Actor: cmd.Actor, Skill: cmd.Skill.ID}}
	for _, ti := range targets {
		target := b.actor(ti)
		if target == nil || !target.Alive {
			continue
		}
		hit := cmd.Skill.Hit
		if hit <= 0 {
			hit = 1
		}
		if b.rng.Float64() > hit {
			events = append(events, Event{Kind: EventMiss, Actor: cmd.Actor, Target: ti, Skill: cmd.Skill.ID})
			continue
		}
		amount := cmd.Skill.Power
		if b.Rules.Damage != nil {
			amount = b.Rules.Damage(*a, *target, cmd.Skill)
		} else {
			amount = max(1, a.Attack+cmd.Skill.Power-target.Defense)
		}
		if cmd.Skill.Scope == ScopeOneAlly || cmd.Skill.Scope == ScopeAllAllies || cmd.Skill.Scope == ScopeSelf {
			target.HP = min(target.MaxHP, target.HP+amount)
			events = append(events, Event{Kind: EventHeal, Actor: cmd.Actor, Target: ti, Amount: amount, Skill: cmd.Skill.ID})
		} else {
			target.HP -= amount
			events = append(events, Event{Kind: EventDamage, Actor: cmd.Actor, Target: ti, Amount: amount, Skill: cmd.Skill.ID})
			if target.HP <= 0 {
				target.HP = 0
				target.Alive = false
				events = append(events, Event{Kind: EventDefeated, Target: ti})
			}
		}
	}
	b.events = append(b.events, events...)
	if b.won() {
		b.events = append(b.events, Event{Kind: EventWon})
	}
	if b.lost() {
		b.events = append(b.events, Event{Kind: EventLost})
	}
	return events, nil
}
func (b *Battle) actor(i int) *Actor {
	if i < len(b.Party) {
		return &b.Party[i]
	}
	i -= len(b.Party)
	if i < len(b.Troop) {
		return &b.Troop[i]
	}
	return nil
}
func (b *Battle) targets(actor int, scope Scope, target int) ([]int, error) {
	party := actor < len(b.Party)
	switch scope {
	case ScopeSelf:
		return []int{actor}, nil
	case ScopeOneEnemy:
		if party {
			target += len(b.Party)
		} else if target >= len(b.Party) {
			target -= len(b.Party)
		}
		if b.actor(target) == nil {
			return nil, errors.New("battle: target out of range")
		}
		return []int{target}, nil
	case ScopeAllEnemies:
		var out []int
		if party {
			for i, a := range b.Troop {
				if a.Alive {
					out = append(out, len(b.Party)+i)
				}
			}
		} else {
			for i, a := range b.Party {
				if a.Alive {
					out = append(out, i)
				}
			}
		}
		return out, nil
	case ScopeOneAlly:
		if target < 0 || target >= len(b.Party) {
			return nil, errors.New("battle: ally target out of range")
		}
		return []int{target}, nil
	case ScopeAllAllies:
		var out []int
		for i, a := range b.Party {
			if a.Alive {
				out = append(out, i)
			}
		}
		return out, nil
	}
	return nil, errors.New("battle: unknown scope")
}
func (b *Battle) won() bool {
	for _, a := range b.Troop {
		if a.Alive {
			return false
		}
	}
	return true
}
func (b *Battle) lost() bool {
	for _, a := range b.Party {
		if a.Alive {
			return false
		}
	}
	return true
}
func max(a, c int) int {
	if a > c {
		return a
	}
	return c
}
func min(a, c int) int {
	if a < c {
		return a
	}
	return c
}
func (b *Battle) Snapshot() Snapshot {
	return Snapshot{cloneActors(b.Party), cloneActors(b.Troop), b.turn, b.next, slices.Clone(b.events)}
}
func (b *Battle) Restore(s Snapshot) {
	b.Party = cloneActors(s.Party)
	b.Troop = cloneActors(s.Troop)
	b.turn = s.Turn
	b.next = s.Next
	b.events = slices.Clone(s.Events)
}
