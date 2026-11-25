package main

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/ai"
)

// order is one thing the player does between waves: build a tower of kind on cell, or upgrade the tower on cell when kind is empty.
type order struct {
	kind string
	cell ai.Cell
}

// winningPlan is what a player who wins the level does before each wave, wave by wave: bolts covering the first bends, a frost tower to hold the enemies under them, a mortar for the crowds and upgrades as the money comes in. The towers stand where two stretches of the road pass them.
var winningPlan = [][]order{
	{{"bolt", ai.Cell{Col: 5, Row: 4}}, {"bolt", ai.Cell{Col: 6, Row: 2}}},
	{{"bolt", ai.Cell{Col: 9, Row: 4}}},
	{{"frost", ai.Cell{Col: 5, Row: 5}}, {"", ai.Cell{Col: 5, Row: 4}}},
	{{"mortar", ai.Cell{Col: 8, Row: 5}}, {"", ai.Cell{Col: 6, Row: 2}}},
	{{"", ai.Cell{Col: 9, Row: 4}}, {"", ai.Cell{Col: 8, Row: 5}}, {"bolt", ai.Cell{Col: 12, Row: 6}}},
}

// thinPlan builds the first wave's bolts of winningPlan and then saves its money, which loses on a later wave.
var thinPlan = [][]order{winningPlan[0], {}, {}, {}, {}}

// cost returns the money o takes in w, and false when it cannot be done there.
func (o order) cost(w *World) (int, bool) {
	if o.kind == "" {
		t, ok := w.TowerAt(o.cell)
		if !ok {
			return 0, false
		}
		return t.UpgradeCost()
	}
	def, ok := w.Data.Towers.Tower(o.kind)
	if !ok || !w.Buildable(o.cell) {
		return 0, false
	}
	return def.Cost, true
}

// between reports whether w is between waves: waves are left, the last one has spawned and no enemy is on the road.
func between(w *World) bool {
	return w.Outcome == Playing && w.Wave < w.Waves() && !w.Spawning() && len(w.Enemies) == 0
}

// playWorld plays plan on w directly: between waves it carries out the next wave's orders, waiting for the money each needs, and then calls the wave. It returns after at most maxSteps steps.
func playWorld(t *testing.T, w *World, plan [][]order, maxSteps int) {
	t.Helper()
	var pending []order
	queued := -1
	for step := 0; step < maxSteps && w.Outcome == Playing; step++ {
		if between(w) && queued < w.Wave {
			queued = w.Wave
			pending = append(pending, plan[w.Wave]...)
		}
		for len(pending) > 0 {
			o := pending[0]
			cost, ok := o.cost(w)
			if !ok {
				t.Fatalf("order %+v cannot be carried out", o)
			}
			if w.Money < cost {
				break
			}
			if o.kind == "" {
				tower, _ := w.TowerAt(o.cell)
				if err := w.Upgrade(tower); err != nil {
					t.Fatal(err)
				}
			} else {
				def, _ := w.Data.Towers.Tower(o.kind)
				if _, err := w.Build(def, o.cell); err != nil {
					t.Fatal(err)
				}
			}
			pending = pending[1:]
		}
		if len(pending) == 0 && between(w) {
			w.CallWave()
		}
		w.Step()
	}
}
