// Package battle provides deterministic, presentation-independent turn battles.
package battle

import (
	"fmt"
	"github.com/nsgo-dev/game/pkg/random"
	"sort"
)

type Scope string

const (
	Self       Scope = "self"
	OneEnemy   Scope = "one_enemy"
	AllEnemies Scope = "all_enemies"
	OneAlly    Scope = "one_ally"
	AllAllies  Scope = "all_allies"
)

type Fighter struct {
	ID                                  string
	Name                                string
	HP, MaxHP, Attack, Defense, Agility int
	MP                                  int
	Alive                               bool
}
type Action struct {
	ID       string
	Scope    Scope
	Power    int
	Cost     int
	Accuracy float64
}
type Command struct {
	Actor  int
	Action Action
	Target int
}
type Event struct {
	Kind          string
	Actor, Target int
	Amount        int
}
type Rules struct {
	TurnMode       string
	CriticalChance float64
}
type Runner struct {
	Party     []Fighter
	Enemies   []Fighter
	Rules     Rules
	RNG       *random.Stream
	Turn      int
	Lost, Won bool
}

func New(party, enemies []Fighter, rules Rules, rng *random.Stream) *Runner {
	p := clone(party)
	e := clone(enemies)
	for i := range p {
		p[i].Alive = true
	}
	for i := range e {
		e[i].Alive = true
	}
	if rng == nil {
		rng = random.New(0)
	}
	return &Runner{Party: p, Enemies: e, Rules: rules, RNG: rng}
}
func clone(in []Fighter) []Fighter { return append([]Fighter(nil), in...) }
func (r *Runner) Order(commands []Command) []Command {
	out := append([]Command(nil), commands...)
	sort.SliceStable(out, func(i, j int) bool { return r.Party[out[i].Actor].Agility > r.Party[out[j].Actor].Agility })
	return out
}
func (r *Runner) Resolve(commands []Command) []Event {
	if r.Won || r.Lost {
		return nil
	}
	r.Turn++
	var events []Event
	for _, c := range r.Order(commands) {
		if c.Actor < 0 || c.Actor >= len(r.Party) || !r.Party[c.Actor].Alive {
			continue
		}
		targets := r.targets(c)
		for _, t := range targets {
			accuracy := c.Action.Accuracy
			if accuracy <= 0 {
				accuracy = 1
			}
			if !r.RNG.Chance(accuracy) {
				events = append(events, Event{Kind: "missed", Actor: c.Actor, Target: t})
				continue
			}
			d := c.Action.Power + r.Party[c.Actor].Attack - r.defense(t)
			if d < 1 {
				d = 1
			}
			r.damage(t, d)
			events = append(events, Event{Kind: "damaged", Actor: c.Actor, Target: t, Amount: d})
			if !r.aliveEnemies() {
				r.Won = true
				events = append(events, Event{Kind: "won"})
				break
			}
		}
	}
	if !r.aliveParty() {
		r.Lost = true
		events = append(events, Event{Kind: "lost"})
	}
	return events
}
func (r *Runner) targets(c Command) []int {
	if c.Action.Scope == AllEnemies {
		out := []int{}
		for i, e := range r.Enemies {
			if e.Alive {
				out = append(out, i)
			}
		}
		return out
	}
	if c.Target < 0 || c.Target >= len(r.Enemies) || !r.Enemies[c.Target].Alive {
		return nil
	}
	return []int{c.Target}
}
func (r *Runner) defense(i int) int { return r.Enemies[i].Defense }
func (r *Runner) damage(i, n int) {
	r.Enemies[i].HP -= n
	if r.Enemies[i].HP <= 0 {
		r.Enemies[i].HP = 0
		r.Enemies[i].Alive = false
	}
}
func (r *Runner) aliveEnemies() bool {
	for _, e := range r.Enemies {
		if e.Alive {
			return true
		}
	}
	return false
}
func (r *Runner) aliveParty() bool {
	for _, p := range r.Party {
		if p.Alive {
			return true
		}
	}
	return false
}
func (r *Runner) Snapshot() Snapshot {
	return Snapshot{Party: clone(r.Party), Enemies: clone(r.Enemies), Turn: r.Turn, Won: r.Won, Lost: r.Lost, RNG: r.RNG.State()}
}

type Snapshot struct {
	Party, Enemies []Fighter
	Turn           int
	Won, Lost      bool
	RNG            random.State
}

func (r *Runner) Restore(s Snapshot) error {
	if r == nil || r.RNG == nil {
		return fmt.Errorf("battle: nil runner")
	}
	r.Party = clone(s.Party)
	r.Enemies = clone(s.Enemies)
	r.Turn = s.Turn
	r.Won = s.Won
	r.Lost = s.Lost
	r.RNG.SetState(s.RNG)
	return nil
}
