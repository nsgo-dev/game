package main

import (
	"testing"
)

func newTestComputer(t *testing.T, w *world) *computer {
	t.Helper()
	c, err := newComputer(w.data, opponent)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// choose returns the action the computer picks for w now.
func choose(c *computer, w *world) string {
	i, _ := c.reasoner.Choose(c.read(w), nil)
	return c.reasoner.ActionName(i)
}

func TestComputerGrowsItsEconomyFirst(t *testing.T) {
	w := newTestWorld(t)
	c := newTestComputer(t, w)
	if got := choose(c, w); got != actTrainWorker {
		t.Fatalf("first choice %q, want %s", got, actTrainWorker)
	}
	// With the crystals for a barracks and enough workers, it builds one.
	for range 2 {
		w.spawn(opponent, w.r.worker(), w.sides[opponent].hq)
	}
	w.sides[opponent].crystals = w.r.building(barracksName).Cost
	c.reasoner.Reset()
	if got := choose(c, w); got != actBuildBarracks {
		t.Fatalf("with six workers and %d crystals the choice is %q, want %s", w.sides[opponent].crystals, got, actBuildBarracks)
	}
	c.act(w, actBuildBarracks)
	if len(w.buildings(opponent, w.r.building(barracksName))) != 1 {
		t.Fatal("build_barracks did not start a barracks")
	}
}

func TestComputerAttacksWithAnArmyAndDefendsItsBase(t *testing.T) {
	w := newTestWorld(t)
	c := newTestComputer(t, w)
	for range 4 {
		w.spawn(opponent, w.r.unit("soldier"), w.sides[opponent].hq)
	}
	w.sides[opponent].crystals = 0
	c.reasoner.Reset()
	if got := choose(c, w); got != actAttack {
		t.Fatalf("with six idle soldiers the choice is %q, want %s", got, actAttack)
	}
	c.act(w, actAttack)
	target := w.buildingCenter(w.sides[player].hq)
	for _, e := range unitsOf(w, opponent, "soldier") {
		if e.order != orderAttackMove || e.dest != target {
			t.Fatalf("a soldier was not sent at the player's headquarters: order %d dest %v", e.order, e.dest)
		}
	}
	if s := c.read(w); s.idleArmy != 0 || s.army != 6 {
		t.Fatalf("after attacking: %d idle of %d, want 0 of 6", s.idleArmy, s.army)
	}

	// An enemy next to its headquarters, with a defender at home, makes it defend.
	w2 := newTestWorld(t)
	c2 := newTestComputer(t, w2)
	intruder := unitsOf(w2, player, "soldier")[0]
	w2.moveGroup([]*entity{intruder}, w2.doorOf(w2.sides[opponent].hq), orderMove)
	for range 60 * 30 {
		w2.step(step)
		if w2.threat(opponent) != nil {
			break
		}
	}
	if w2.threat(opponent) == nil {
		t.Fatal("a player soldier at the opponent's door is no threat")
	}
	c2.reasoner.Reset()
	if got := choose(c2, w2); got != actDefend {
		t.Fatalf("under attack the choice is %q, want %s", got, actDefend)
	}
}

func TestComputerThinksOnItsSchedule(t *testing.T) {
	w := newTestWorld(t)
	c := newTestComputer(t, w)
	for range c.every - 1 {
		w.steps++
		c.step(w)
		if c.last != "" {
			t.Fatalf("the computer acted at step %d, before its first think", w.steps)
		}
	}
	w.steps++
	c.step(w)
	if c.last != actTrainWorker || len(w.sides[opponent].hq.queue) != 1 {
		t.Fatalf("at its first think the computer did %q with %d queued", c.last, len(w.sides[opponent].hq.queue))
	}
}
