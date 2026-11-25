package main

import (
	"github.com/nsgo-dev/game/packages/game/ability"
)

// The bots the tests play with. They read the run, which a player cannot fully do, because their job is to reach a win and a loss on every seed the tests use, not to play fairly.
const (
	// botClear clears each floor of its monsters and items before taking the exit, quaffs when hurt and cleaves when surrounded: it plays to win.
	botClear = iota
	// botDive heads straight for each exit, fighting only what stands in its way and never quaffing: deeper floors are meant to kill it.
	botDive
)

// botAction returns what the bot of the given style does next on r.
func botAction(r *run, style int) action {
	if style == botClear {
		if r.health*100 < r.maxHealth()*45 && r.caster.CanActivate(abilityQuaff) == ability.ReasonOK {
			return action{kind: actAbility, ability: abilityQuaff}
		}
		if len(r.adjacent()) >= 2 && r.caster.CanActivate(abilityCleave) == ability.ReasonOK {
			return action{kind: actAbility, ability: abilityCleave}
		}
		if r.health*100 < r.maxHealth()*80 && !threatened(r) {
			return action{kind: actWait}
		}
	}
	for _, st := range steps {
		if e, ok := r.floor.foeAt(cell{X: r.pos.X + st.X, Y: r.pos.Y + st.Y}); ok && (style == botClear || !r.floor.guards(e)) {
			return action{kind: actMove, dir: st}
		}
	}
	return action{kind: actMove, dir: stepToward(r, botTarget(r, style))}
}

// threatened reports whether a monster is next to the player or one that is not a guardian hunts within ten steps, so resting would only let it hit.
func threatened(r *run) bool {
	for _, e := range r.floor.foes() {
		d := r.distAt(r.floor.at(e))
		if d == 1 || r.floor.foe(e).Hunting && !r.floor.guards(e) && d >= 0 && d <= 10 {
			return true
		}
	}
	return false
}

// botTarget returns the cell the bot heads for: for botClear the nearest item or monster, the guardian last, and then the exit; for botDive the exit.
func botTarget(r *run, style int) cell {
	if style == botClear {
		best, bestDist := cell{}, -1
		consider := func(c cell) {
			if d := r.distAt(c); d >= 0 && (bestDist < 0 || d < bestDist) {
				best, bestDist = c, d
			}
		}
		for _, e := range r.floor.loot() {
			consider(r.floor.at(e))
		}
		for _, e := range r.floor.foes() {
			if !r.floor.guards(e) {
				consider(r.floor.at(e))
			}
		}
		if bestDist >= 0 {
			return best
		}
	}
	return r.floor.exit
}

// stepToward returns the step from the player that walks down the distance map of target.
func stepToward(r *run, target cell) cell {
	dist := distances(r.floor.grid, target)
	at := func(c cell) int {
		if !r.floor.grid.In(c.X, c.Y) {
			return -1
		}
		return dist[c.Y*r.floor.grid.Width+c.X]
	}
	best, bestDist := cell{}, at(r.pos)
	for _, st := range steps {
		if d := at(cell{X: r.pos.X + st.X, Y: r.pos.Y + st.Y}); d >= 0 && d < bestDist {
			best, bestDist = st, d
		}
	}
	return best
}

// playRun plays r with the bot style until it ends or limit actions pass, and returns how it ended.
func playRun(r *run, style, limit int) outcome {
	for n := 0; n < limit && r.state == playing; n++ {
		a := botAction(r, style)
		if !r.act(a) {
			// A refused action would loop forever; wait instead.
			r.act(action{kind: actWait})
		}
	}
	return r.state
}
