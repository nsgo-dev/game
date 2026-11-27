package main

import (
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/rts"
)

// The orders both sides give: the player's through the controls and the computer player's through its choices, so the two play by the same rules. The computer sees through its own side's fog of war like the player, but knows the map, so it knows where the player's headquarters stands.

// train queues a unit of type u at the least busy finished building of t that produces it, and reports whether it could: t needs the crystals, room under rules.MaxUnits and a building with room in its queue.
func (w *world) train(t team, u *unitRules) bool {
	if w.over || u == nil || w.sides[t].crystals < u.Cost || w.supply(t) >= w.r.MaxUnits {
		return false
	}
	var at *entity
	for _, b := range w.buildings(t, w.r.building(u.TrainedAt)) {
		if b.built() && len(b.queue) < w.r.QueueLimit && (at == nil || len(b.queue) < len(at.queue)) {
			at = b
		}
	}
	if at == nil {
		return false
	}
	w.sides[t].crystals -= u.Cost
	if len(at.queue) == 0 {
		at.trainLeft = secondsToSteps(u.TrainSeconds)
	}
	at.queue = append(at.queue, u)
	return true
}

// buildBarracks starts a barracks on t's first free build pad and reports whether it could: t needs the crystals and a free pad.
func (w *world) buildBarracks(t team) bool {
	b := w.r.building(barracksName)
	s := &w.sides[t]
	if w.over || s.crystals < b.Cost {
		return false
	}
	for i, used := range s.padUsed {
		if used {
			continue
		}
		s.crystals -= b.Cost
		s.padUsed[i] = true
		w.addBuilding(t, b, s.pads[i], false)
		return true
	}
	return false
}

// freePad reports whether t has a build pad without a barracks.
func (w *world) freePad(t team) bool {
	for _, used := range w.sides[t].padUsed {
		if !used {
			return true
		}
	}
	return false
}

// command gives the units of ids that t owns the order a right click, a tap or the gamepad's command button gives at world point p: attack the enemy there; otherwise combat units attack-move there, fighting every enemy they see on the way, and workers mine the crystal cell there or walk there.
func (w *world) command(t team, ids []rts.UnitID, p math2d.Vec2) {
	units := w.owned(t, ids)
	if len(units) == 0 || w.over || !finiteVec(p) {
		return
	}
	if target := w.enemyAt(t, p); target != nil {
		var walkers []*entity
		for _, e := range units {
			if e.fights() {
				e.order, e.target, e.resume, e.repath = orderAttack, target.id, false, 0
			} else {
				walkers = append(walkers, e)
			}
		}
		w.moveGroup(walkers, p, orderMove)
		return
	}
	// Combat units fight their way to the point; workers walk there, or mine the crystal cell there.
	n := w.nodeAt(p)
	var fighters, walkers []*entity
	for _, e := range units {
		switch {
		case e.fights():
			fighters = append(fighters, e)
		case n >= 0:
			e.order, e.node, e.returning, e.mining, e.repath = orderGather, n, e.carry > 0, 0, 0
		default:
			walkers = append(walkers, e)
		}
	}
	w.moveGroup(fighters, p, orderAttackMove)
	w.moveGroup(walkers, p, orderMove)
}

// attackMove sends the units of ids that t owns to p in formation, attacking every enemy they see on the way.
func (w *world) attackMove(t team, ids []rts.UnitID, p math2d.Vec2) {
	w.moveGroup(w.owned(t, ids), p, orderAttackMove)
}

// moveGroup orders units to p together, in data/formations.yaml's move formation, with order.
func (w *world) moveGroup(units []*entity, p math2d.Vec2, order orderKind) {
	if len(units) == 0 {
		return
	}
	bodies := make([]rts.UnitID, len(units))
	for i, e := range units {
		bodies[i] = e.body
		e.order, e.target, e.resume, e.dest, e.repath = order, 0, false, p, repathSteps
		e.mining = 0
	}
	f, _ := w.data.formations.Get(moveFormation)
	w.crowd.Move(bodies, rts.MoveOrder{Target: p, Formation: f, KeepPace: true})
}

// owned returns the living units of ids that t owns, in the order of ids.
func (w *world) owned(t team, ids []rts.UnitID) []*entity {
	var out []*entity
	for _, id := range ids {
		if e := w.byBody[id]; e != nil && e.team == t && e.alive() {
			out = append(out, e)
		}
	}
	return out
}

// enemyAt returns the enemy of t that t sees at world point p: a unit whose body is within a few points of it, or a building whose footprint holds it.
func (w *world) enemyAt(t team, p math2d.Vec2) *entity {
	if id, ok := w.crowd.UnitAt(p, 4); ok {
		if e := w.byBody[id]; e != nil && e.team != t && w.visibleTo(t, e) {
			return e
		}
	}
	for _, e := range w.ents {
		if e.building != nil && e.team != t && w.buildingRect(e).Contains(p) && w.visibleTo(t, e) {
			return e
		}
	}
	return nil
}
