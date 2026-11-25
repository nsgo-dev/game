package main

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/rts"
)

// opponentState is what the computer player reads about its own side before it chooses; data/opponent.yaml scores its actions from these numbers.
type opponentState struct {
	crystals      int
	workers       int
	army          int
	idleArmy      int
	barracks      int
	barracksReady int
	hqQueue       int
	barracksQueue int
	supplyLeft    int
	freePad       bool
	threat        bool
	affordWorker  bool
	affordSoldier bool
	affordRanger  bool
	affordBuild   bool
	rangerShare   float64
}

// opponentInputs are the inputs data/opponent.yaml's considerations read, by name.
var opponentInputs = map[string]func(opponentState) float64{
	"crystals":       func(s opponentState) float64 { return float64(s.crystals) },
	"workers":        func(s opponentState) float64 { return float64(s.workers) },
	"army":           func(s opponentState) float64 { return float64(s.army) },
	"idle_army":      func(s opponentState) float64 { return float64(s.idleArmy) },
	"barracks":       func(s opponentState) float64 { return float64(s.barracks) },
	"barracks_ready": func(s opponentState) float64 { return float64(s.barracksReady) },
	"hq_queue":       func(s opponentState) float64 { return float64(s.hqQueue) },
	"barracks_queue": func(s opponentState) float64 { return float64(s.barracksQueue) },
	"supply_left":    func(s opponentState) float64 { return float64(s.supplyLeft) },
	"free_pad":       func(s opponentState) float64 { return flag(s.freePad) },
	"threat":         func(s opponentState) float64 { return flag(s.threat) },
	"afford_worker":  func(s opponentState) float64 { return flag(s.affordWorker) },
	"afford_soldier": func(s opponentState) float64 { return flag(s.affordSoldier) },
	"afford_ranger":  func(s opponentState) float64 { return flag(s.affordRanger) },
	"afford_build":   func(s opponentState) float64 { return flag(s.affordBuild) },
	"ranger_share":   func(s opponentState) float64 { return s.rangerShare },
}

// The actions data/opponent.yaml must define, which the computer player knows how to carry out.
const (
	actTrainWorker   = "train_worker"
	actBuildBarracks = "build_barracks"
	actTrainSoldier  = "train_soldier"
	actTrainRanger   = "train_ranger"
	actAttack        = "attack"
	actDefend        = "defend"
)

// computer is the opponent's player: every rules.OpponentThinkSeconds it scores its actions with a utility reasoner and carries out the best one through the same orders the player gives.
type computer struct {
	t        team
	reasoner *ai.Reasoner[opponentState]
	every    int
	// last is the name of the action last carried out, for tests and the debug readout.
	last string
}

// newComputer builds the computer player for t from data/opponent.yaml, which must define every action it knows.
func newComputer(data *gameData, t team) (*computer, error) {
	r, err := ai.BuildReasoner(data.opponent, ai.UtilityBindings[opponentState]{Inputs: opponentInputs})
	if err != nil {
		return nil, err
	}
	for _, name := range []string{actTrainWorker, actBuildBarracks, actTrainSoldier, actTrainRanger, actAttack, actDefend} {
		if r.ActionIndex(name) < 0 {
			return nil, fmt.Errorf("opponent.yaml has no action %s", name)
		}
	}
	return &computer{t: t, reasoner: r, every: secondsToSteps(data.rules.OpponentThinkSeconds)}, nil
}

// step lets the computer think on its schedule.
func (c *computer) step(w *world) {
	if w.over || w.steps%uint64(c.every) != 0 {
		return
	}
	i, _ := c.reasoner.Choose(c.read(w), nil)
	if i < 0 {
		c.last = ""
		return
	}
	c.last = c.reasoner.ActionName(i)
	c.act(w, c.last)
}

// read gathers the computer's view of its side.
func (c *computer) read(w *world) opponentState {
	r := w.r
	s := opponentState{crystals: w.sides[c.t].crystals, supplyLeft: r.MaxUnits - w.supply(c.t), freePad: w.freePad(c.t), threat: w.threat(c.t) != nil}
	rangers := 0
	for _, e := range w.ents {
		if e.team != c.t || !e.alive() {
			continue
		}
		switch {
		case e.unit != nil && e.unit.Gathers:
			s.workers++
		case e.fights():
			s.army++
			if e.order == orderIdle || e.order == orderMove {
				s.idleArmy++
			}
			if e.unit.Name == rangerName {
				rangers++
			}
		case e.building != nil && e.building.Name == barracksName:
			s.barracks++
			if e.built() {
				s.barracksReady++
				s.barracksQueue += len(e.queue)
			}
		case e.building != nil:
			s.hqQueue += len(e.queue)
		}
	}
	if s.army > 0 {
		s.rangerShare = float64(rangers) / float64(s.army)
	}
	afford := func(name string) bool { u := r.unit(name); return u != nil && s.crystals >= u.Cost }
	s.affordWorker = s.crystals >= r.worker().Cost
	s.affordSoldier = afford(soldierName)
	s.affordRanger = afford(rangerName)
	s.affordBuild = s.crystals >= r.building(barracksName).Cost
	return s
}

// act carries out the action named name.
func (c *computer) act(w *world, name string) {
	switch name {
	case actTrainWorker:
		w.train(c.t, w.r.worker())
	case actBuildBarracks:
		w.buildBarracks(c.t)
	case actTrainSoldier:
		w.train(c.t, w.r.unit(soldierName))
	case actTrainRanger:
		w.train(c.t, w.r.unit(rangerName))
	case actAttack:
		w.attackMove(c.t, c.idleArmy(w), w.buildingCenter(w.sides[c.t.rival()].hq))
	case actDefend:
		if t := w.threat(c.t); t != nil {
			w.attackMove(c.t, c.idleArmy(w), w.pos(t))
		}
	}
}

// idleArmy returns the bodies of the computer's combat units that are not on an attack.
func (c *computer) idleArmy(w *world) []rts.UnitID {
	var out []rts.UnitID
	for _, e := range w.ents {
		if e.team == c.t && e.fights() && e.alive() && (e.order == orderIdle || e.order == orderMove) {
			out = append(out, e.body)
		}
	}
	return out
}

func flag(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
