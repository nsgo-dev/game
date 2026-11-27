package main

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/idle"
)

// hearth is the game's state and rules, with no view and no clock of its own: the game loop and the offline replay both advance it with step, so it plays the same online, away and in tests.
type hearth struct {
	data *hearthData

	// embers is what the player can spend.
	embers idle.Number
	// earned is every ember this run has made, spent or not, which prestige pays ash for.
	earned idle.Number
	// ash is the prestige currency, kept across runs; each adds Prestige.Bonus to production.
	ash idle.Number
	// owned counts each generator bought this run, by its index in data.Generators.
	owned []int64
	// bought marks each upgrade bought this run, by its index in data.Upgrades.
	bought []bool
	// fuel is in the gauge, from 0 to Fuel.Capacity.
	fuel float64
	// cold says the fire went out: the run is lost until rekindle starts a new one.
	cold bool
	// won says the goal has been bought; play goes on after it.
	won bool
	// runs counts the runs started by prestige or rekindling, after the first.
	runs int
	// played is the time this game has run, online and away, since it began.
	played time.Duration
}

// newHearth returns a new game on data, with a full gauge.
func newHearth(data *hearthData) *hearth {
	h := &hearth{data: data}
	h.newRun()
	return h
}

// newRun starts a run: no embers, generators or upgrades, a full gauge and a lit fire. Ash, the win and the play time stay.
func (h *hearth) newRun() {
	h.embers, h.earned = idle.Number{}, idle.Number{}
	h.owned = make([]int64, len(h.data.Generators))
	h.bought = make([]bool, len(h.data.Upgrades))
	h.fuel = h.data.Fuel.Capacity
	h.cold = false
}

// multiplier returns what generator i's output is multiplied by: its upgrades, the upgrades for every generator and the ash bonus.
func (h *hearth) multiplier(i int) idle.Number {
	m := idle.New(1).Add(h.ash.Scale(h.data.Prestige.Bonus))
	id := h.data.Generators[i].ID
	for j, u := range h.data.Upgrades {
		if h.bought[j] && (u.Effect == effectAll || u.Effect == effectGenerator && u.Target == id) {
			m = m.Scale(u.Value)
		}
	}
	return m
}

// generatorRate returns the embers per second generator i makes, all of it owned.
func (h *hearth) generatorRate(i int) idle.Number {
	if h.owned[i] == 0 {
		return idle.Number{}
	}
	return h.data.Generators[i].Rate.Mul(idle.New(float64(h.owned[i]))).Mul(h.multiplier(i))
}

// rate returns the embers per second every generator makes together, 0 while the fire is out.
func (h *hearth) rate() idle.Number {
	var r idle.Number
	if h.cold {
		return r
	}
	for i := range h.data.Generators {
		r = r.Add(h.generatorRate(i))
	}
	return r
}

// stokeEmbers returns the embers one stoke gathers.
func (h *hearth) stokeEmbers() idle.Number {
	e := h.data.Fuel.Embers
	for j, u := range h.data.Upgrades {
		if h.bought[j] && u.Effect == effectStoke {
			e = e.Scale(u.Value)
		}
	}
	return e
}

// fuelPerSecond returns the fuel the upgrades add per second.
func (h *hearth) fuelPerSecond() float64 {
	var f float64
	for j, u := range h.data.Upgrades {
		if h.bought[j] && u.Effect == effectFuel {
			f += u.Value
		}
	}
	return f
}

// netFuel returns how fast the gauge changes per second: negative while the fire burns faster than it is fed.
func (h *hearth) netFuel() float64 { return h.fuelPerSecond() - h.data.Fuel.Burn }

// step advances the game by dt, earning efficiency of the production, as 1 online and Offline.Efficiency away. It reports whether the fire went out during the step.
func (h *hearth) step(dt time.Duration, efficiency float64) (wentCold bool) {
	if h.cold || dt <= 0 {
		return false
	}
	h.played += dt
	secs := dt.Seconds()
	h.earn(h.rate().Scale(secs * efficiency))
	h.fuel = math.Min(h.fuel+h.netFuel()*secs, h.data.Fuel.Capacity)
	if h.fuel <= 0 {
		h.fuel, h.cold = 0, true
		return true
	}
	return false
}

// earn adds n embers to spend and to the run's total.
func (h *hearth) earn(n idle.Number) {
	h.embers = h.embers.Add(n)
	h.earned = h.earned.Add(n)
}

// stoke feeds the fire and gathers embers; it does nothing once the fire is out.
func (h *hearth) stoke() bool {
	if h.cold {
		return false
	}
	h.fuel = math.Min(h.fuel+h.data.Fuel.Stoke, h.data.Fuel.Capacity)
	h.earn(h.stokeEmbers())
	return true
}

// generatorCost returns the price of the next generator i: Cost × Growth^owned.
func (h *hearth) generatorCost(i int) idle.Number {
	g := h.data.Generators[i]
	return g.Cost.Mul(idle.New(g.Growth).Pow(h.owned[i]))
}

// spend takes cost from the embers if the fire is lit and there are enough, and reports whether it did.
func (h *hearth) spend(cost idle.Number) bool {
	if h.cold || h.embers.Cmp(cost) < 0 {
		return false
	}
	h.embers = h.embers.Add(cost.Neg())
	return true
}

// buyGenerator buys one generator i if the player can afford it.
func (h *hearth) buyGenerator(i int) bool {
	if !h.spend(h.generatorCost(i)) {
		return false
	}
	h.owned[i]++
	return true
}

// buyUpgrade buys upgrade j if it is not bought yet and the player can afford it.
func (h *hearth) buyUpgrade(j int) bool {
	if h.bought[j] || !h.spend(h.data.Upgrades[j].Cost) {
		return false
	}
	h.bought[j] = true
	return true
}

// buyGoal buys the goal, winning the game, if it is not won yet and the player can afford it.
func (h *hearth) buyGoal() bool {
	if h.won || !h.spend(h.data.Goal.Cost) {
		return false
	}
	h.won = true
	return true
}

// ashGain returns the ash scattering the ashes now would give: floor(√(earned / Threshold)). It uses only correctly rounded float64 operations, so it is the same on every platform.
func (h *hearth) ashGain() idle.Number {
	q := h.earned.Div(h.data.Prestige.Threshold)
	if q.Cmp(idle.New(1)) < 0 {
		return idle.Number{}
	}
	if f := q.Float64(); !math.IsInf(f, 0) {
		return idle.New(math.Floor(math.Sqrt(f)))
	}
	// Beyond float64, halve the power of ten and take the root of what is left.
	e := q.Exponent()
	m := q.Div(idle.NewExp(1, e-e%2)).Float64()
	return idle.NewExp(math.Sqrt(m), e/2)
}

// prestige scatters the ashes: it adds ashGain to ash and starts a new run, if the run has earned any ash.
func (h *hearth) prestige() bool {
	gain := h.ashGain()
	if h.cold || gain.IsZero() {
		return false
	}
	h.ash = h.ash.Add(gain)
	h.runs++
	h.newRun()
	return true
}

// rekindle starts a new run after the fire went out, keeping the ash but none of the lost run.
func (h *hearth) rekindle() bool {
	if !h.cold {
		return false
	}
	h.runs++
	h.newRun()
	return true
}

// awayReport is what happened while the player was away.
type awayReport struct {
	// Away is the absence as idle.Offline credited it.
	Away idle.Away
	// Earned is the embers made while away.
	Earned idle.Number
	// WentCold says the fire went out while away.
	WentCold bool
}

// creditAway replays the time between last and now in the fixed steps hearth.yaml sets, at its offline efficiency, so the fire can run dry while the player is away just as it would online. A zero last, as for a new game, credits nothing.
func (h *hearth) creditAway(last, now time.Time) awayReport {
	o := h.data.Offline
	away := o.offline().Absence(last, now)
	r := awayReport{Away: away}
	if last.IsZero() || away.Credited <= 0 || h.cold {
		return r
	}
	before := h.earned
	n, rest := away.Steps(o.step())
	for range n {
		if h.step(o.step(), o.Efficiency) {
			r.WentCold = true
			break
		}
	}
	if !r.WentCold && h.step(rest, o.Efficiency) {
		r.WentCold = true
	}
	r.Earned = h.earned.Add(before.Neg())
	return r
}
