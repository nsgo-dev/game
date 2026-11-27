package main

import (
	"fmt"
	"hash/fnv"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/fog"
	"github.com/nsgo-dev/game/pkg/rts"
)

func newTestWorld(t *testing.T) *world {
	t.Helper()
	d, err := loadGameData()
	if err != nil {
		t.Fatal(err)
	}
	return newWorld(d)
}

// run steps w for d of game time.
func run(w *world, d time.Duration) {
	for range int(d / step) {
		w.step(step)
	}
}

// unitsOf returns t's units of type name, in the world's order.
func unitsOf(w *world, t team, name string) []*entity {
	var out []*entity
	for _, e := range w.ents {
		if e.team == t && e.unit != nil && e.unit.Name == name {
			out = append(out, e)
		}
	}
	return out
}

func bodies(es []*entity) []rts.UnitID {
	out := make([]rts.UnitID, len(es))
	for i, e := range es {
		out[i] = e.body
	}
	return out
}

func TestWorldStartsEachSideWithItsBaseAndUnits(t *testing.T) {
	w := newTestWorld(t)
	for _, side := range []team{player, opponent} {
		if hq := w.sides[side].hq; hq == nil || !hq.built() || hq.hp != hq.maxHP {
			t.Fatalf("team %d has no finished headquarters", side)
		}
		if got := w.sides[side].crystals; got != w.r.StartCrystals {
			t.Errorf("team %d starts with %d crystals, want %d", side, got, w.r.StartCrystals)
		}
		for _, s := range w.r.Start {
			if got := len(unitsOf(w, side, s.Unit)); got != s.Count {
				t.Errorf("team %d starts with %d %ss, want %d", side, got, s.Unit, s.Count)
			}
		}
		if len(w.sides[side].pads) != 2 {
			t.Errorf("team %d has %d build pads, want 2", side, len(w.sides[side].pads))
		}
	}
	for _, e := range unitsOf(w, player, "worker") {
		if e.order != orderGather {
			t.Fatalf("a starting worker is not gathering: order %d", e.order)
		}
	}
}

func TestWorkersMineCrystalsAndCarryThemHome(t *testing.T) {
	w := newTestWorld(t)
	start := w.sides[player].crystals
	total := 0
	for _, n := range w.nodes {
		total += n.left
	}
	run(w, 20*time.Second)
	got := w.sides[player].crystals - start
	if got < 4*w.r.CarryAmount || got%w.r.CarryAmount != 0 {
		t.Fatalf("four workers brought home %d crystals in 20 s, want several loads of %d", got, w.r.CarryAmount)
	}
	left := 0
	for _, n := range w.nodes {
		left += n.left
	}
	if mined := total - left; mined < got {
		t.Fatalf("%d crystals reached the headquarters but only %d left the map", got, mined)
	}
}

func TestMinedOutCrystalOpensItsCell(t *testing.T) {
	w := newTestWorld(t)
	worker := unitsOf(w, player, "worker")[0]
	n := &w.nodes[worker.node]
	n.left = w.r.CarryAmount
	if w.grid.Walkable(n.cell.Col, n.cell.Row) {
		t.Fatal("a crystal cell is walkable")
	}
	run(w, 10*time.Second)
	if n.left != 0 || !w.grid.Walkable(n.cell.Col, n.cell.Row) {
		t.Fatalf("the mined-out cell: %d left, walkable %v", n.left, w.grid.Walkable(n.cell.Col, n.cell.Row))
	}
	if worker.order != orderGather || worker.node < 0 || w.nodes[worker.node].left == 0 {
		t.Fatalf("the worker did not move on to another crystal cell: order %d node %d", worker.order, worker.node)
	}
}

func TestTrainingCostsCrystalsAndTakesTime(t *testing.T) {
	w := newTestWorld(t)
	worker := w.r.worker()
	if !w.train(player, worker) {
		t.Fatal("could not train a worker with the starting crystals")
	}
	if got, want := w.sides[player].crystals, w.r.StartCrystals-worker.Cost; got != want {
		t.Fatalf("crystals after queueing a worker = %d, want %d", got, want)
	}
	if w.train(player, w.r.unit("soldier")) {
		t.Fatal("trained a soldier without a barracks")
	}
	w.sides[player].crystals = 0
	if w.train(player, worker) {
		t.Fatal("trained a worker without the crystals")
	}
	before := len(unitsOf(w, player, "worker"))
	run(w, time.Duration(worker.TrainSeconds*float64(time.Second))-step)
	if len(unitsOf(w, player, "worker")) != before {
		t.Fatal("the worker came out before its training time")
	}
	run(w, 2*step)
	if len(unitsOf(w, player, "worker")) != before+1 {
		t.Fatal("the worker did not come out after its training time")
	}
}

func TestQueueAndUnitLimits(t *testing.T) {
	w := newTestWorld(t)
	w.sides[player].crystals = 10000
	for i := range w.r.QueueLimit {
		if !w.train(player, w.r.worker()) {
			t.Fatalf("queueing worker %d failed", i+1)
		}
	}
	if w.train(player, w.r.worker()) {
		t.Fatal("queued more than queueLimit units at one building")
	}
	// Each test loads its own copy of the rules, so lowering the limit here changes no other test.
	w.sides[player].hq.queue = nil
	w.r.MaxUnits = w.supply(player)
	if w.train(player, w.r.worker()) {
		t.Fatal("trained past maxUnits")
	}
}

func TestBarracksIsBuiltOnAPadAndTrainsSoldiers(t *testing.T) {
	w := newTestWorld(t)
	b := w.r.building(barracksName)
	if w.buildBarracks(player) {
		t.Fatal("built a barracks without the crystals")
	}
	w.sides[player].crystals = 2*b.Cost + w.r.unit("soldier").Cost
	if !w.buildBarracks(player) || !w.buildBarracks(player) {
		t.Fatal("could not build two barracks with the crystals for them")
	}
	if w.freePad(player) || w.buildBarracks(player) {
		t.Fatal("built a third barracks with only two pads")
	}
	if w.train(player, w.r.unit("soldier")) {
		t.Fatal("trained a soldier at a barracks under construction")
	}
	run(w, time.Duration(b.BuildSeconds*float64(time.Second))+step)
	for _, e := range w.buildings(player, b) {
		if !e.built() || e.hp != e.maxHP {
			t.Fatalf("a barracks is not finished after its build time: %d steps left, %d/%d hp", e.buildLeft, e.hp, e.maxHP)
		}
	}
	if !w.train(player, w.r.unit("soldier")) {
		t.Fatal("could not train a soldier at a finished barracks")
	}
	run(w, 10*time.Second)
	if got := len(unitsOf(w, player, "soldier")); got != 3 {
		t.Fatalf("%d soldiers after training one, want 3", got)
	}
}

func TestFogHidesTheOpponentUntilScouted(t *testing.T) {
	w := newTestWorld(t)
	enemyHQ := w.sides[opponent].hq
	if w.visibleTo(player, enemyHQ) {
		t.Fatal("the player sees the opponent's headquarters at the start")
	}
	for _, e := range unitsOf(w, opponent, "worker") {
		if w.visibleTo(player, e) {
			t.Fatal("the player sees an opponent's worker at the start")
		}
	}
	if !w.fog.Visible(fog.Team(player), w.sides[player].hq.cell.Col, w.sides[player].hq.cell.Row) {
		t.Fatal("the player does not see its own headquarters")
	}
	// Send the soldiers across the map; once they have looked at the opponent's base, its headquarters stays shown.
	w.command(player, bodies(unitsOf(w, player, "soldier")), w.buildingCenter(enemyHQ))
	for range 60 * 60 {
		w.step(step)
		if w.visibleTo(player, enemyHQ) {
			return
		}
	}
	t.Fatal("soldiers sent at the opponent's base never saw its headquarters")
}

func TestSoldiersAttackAndDestroyingTheHeadquartersEndsTheGame(t *testing.T) {
	w := newTestWorld(t)
	soldiers := unitsOf(w, player, "soldier")
	// Clear the opponent's defenders so the test is about the headquarters.
	for _, e := range unitsOf(w, opponent, "soldier") {
		e.hp = 0
	}
	w.removeDead()
	w.sides[opponent].hq.hp = 60
	w.command(player, bodies(soldiers), w.buildingCenter(w.sides[opponent].hq))
	for range 120 * 60 {
		if w.over {
			break
		}
		w.step(step)
	}
	if !w.over || w.winner != player {
		t.Fatalf("over=%v winner=%d after the opponent's headquarters should have fallen", w.over, w.winner)
	}
	if w.byID[w.sides[opponent].hq.id] != nil {
		t.Fatal("the destroyed headquarters is still in the world")
	}
	if w.train(player, w.r.worker()) {
		t.Fatal("trained a unit after the game ended")
	}
}

func TestCombatRemovesTheDeadEverywhere(t *testing.T) {
	w := newTestWorld(t)
	var removed []int
	w.onRemove = func(e *entity) { removed = append(removed, e.id) }
	// Clear the opponent's soldiers out of the way, so the test is about a kill.
	for _, e := range unitsOf(w, opponent, "soldier") {
		e.hp = 0
	}
	w.removeDead()
	victim := unitsOf(w, opponent, "worker")[0]
	victim.hp = 1
	soldier := unitsOf(w, player, "soldier")[0]
	w.command(player, []rts.UnitID{soldier.body}, w.pos(victim))
	if soldier.order != orderAttackMove {
		t.Fatalf("a right click on a hidden enemy did not attack-move: order %d", soldier.order)
	}
	for range 90 * 60 {
		if w.byID[victim.id] == nil {
			break
		}
		w.step(step)
	}
	if w.byID[victim.id] != nil || w.byBody[victim.body] != nil {
		t.Fatal("the opponent's worker was not killed and removed")
	}
	if _, ok := w.crowd.Unit(victim.body); ok {
		t.Fatal("the dead worker is still in the crowd")
	}
	if _, ok := w.fog.Source(victim.fogID); ok {
		t.Fatal("the dead worker still gives its side vision")
	}
	found := false
	for _, id := range removed {
		found = found || id == victim.id
	}
	if !found {
		t.Fatal("onRemove did not hear the dead worker")
	}
}

func TestCommandOrdersWorkersToGatherAndFightersToAttack(t *testing.T) {
	w := newTestWorld(t)
	workers := unitsOf(w, player, "worker")
	far := -1
	for i, n := range w.nodes {
		if n.cell.Row < w.rows/2 {
			far = i
			break
		}
	}
	w.command(player, bodies(workers[:1]), w.cellCenter(w.nodes[far].cell))
	if workers[0].order != orderGather || workers[0].node != far {
		t.Fatalf("a command on a crystal cell did not set the worker gathering it: order %d node %d", workers[0].order, workers[0].node)
	}
	w.command(player, bodies(workers[1:2]), w.size().Scale(0.5))
	if workers[1].order != orderMove {
		t.Fatalf("a command on open ground did not move the worker: order %d", workers[1].order)
	}
	// An enemy the player sees is attacked directly.
	soldier := unitsOf(w, player, "soldier")[0]
	enemy := unitsOf(w, opponent, "soldier")[0]
	col, row, _ := w.fog.CellAt(w.pos(enemy).Point())
	w.fog.AddSource(fog.Source{Team: fog.Team(player), Col: col, Row: row, Radius: 2})
	w.fog.Update()
	w.command(player, []rts.UnitID{soldier.body}, w.pos(enemy))
	if soldier.order != orderAttack || soldier.target != enemy.id {
		t.Fatalf("a command on a visible enemy did not attack it: order %d target %d", soldier.order, soldier.target)
	}
	// The opponent's units ignore the player's commands.
	w.command(player, []rts.UnitID{enemy.body}, w.size().Scale(0.5))
	if enemy.order == orderMove || enemy.order == orderAttackMove {
		t.Fatal("the player ordered an opponent's unit")
	}
}

// TestSkirmishIsDeterministic plays the same skirmish twice and checks that it ends in the same state, as replays and lockstep multiplayer need.
func TestSkirmishIsDeterministic(t *testing.T) {
	play := func() string {
		g := newSkirmish(mustLoadGameData(), nil, gameViewport())
		for i := range 90 * stepRate {
			if i == 30*stepRate {
				g.selectArmy()
				g.w.command(player, g.sel.IDs(), g.w.buildingCenter(g.w.sides[opponent].hq))
			}
			g.update(step)
		}
		h := fnv.New64a()
		for _, e := range g.w.ents {
			fmt.Fprintf(h, "%d %d %d %v %d|", e.id, e.team, e.hp, g.w.pos(e), e.order)
		}
		return fmt.Sprintf("%x %d %d", h.Sum64(), g.w.sides[player].crystals, g.w.sides[opponent].crystals)
	}
	if a, b := play(), play(); a != b {
		t.Fatalf("two runs of the same skirmish differ: %s and %s", a, b)
	}
}

func TestFogShowsAScoutedBuildingAsLastSeen(t *testing.T) {
	w := newTestWorld(t)
	w.sides[opponent].crystals = w.r.building(barracksName).Cost
	if !w.buildBarracks(opponent) {
		t.Fatal("the opponent could not build a barracks")
	}
	barracks := w.buildings(opponent, w.r.building(barracksName))[0]
	w.updateFog()
	// Explored ground the player does not see now shows nothing placed there since.
	w.fog.ExploreRect(fog.Team(player), 0, 0, w.cols, w.rows)
	if w.visibleTo(player, barracks) || w.enemyAt(player, w.buildingCenter(barracks)) != nil {
		t.Fatal("the player knows of a barracks it never saw")
	}
	// A scout at the opponent's door sees the barracks and remembers it as it was.
	scout := w.spawn(player, w.r.unit(soldierName), w.sides[opponent].hq)
	w.updateFog()
	if !w.inSight(player, barracks) {
		t.Fatal("a scout at the opponent's door does not see its barracks")
	}
	seenHP, seenLeft := barracks.hp, barracks.buildLeft
	scout.hp = 0
	w.removeDead()
	w.updateFog()
	barracks.hp, barracks.buildLeft = 1, 0
	if !w.visibleTo(player, barracks) {
		t.Fatal("the player forgot a barracks it scouted")
	}
	if hp, left := w.shownTo(player, barracks); hp != seenHP || left != seenLeft {
		t.Fatalf("out of sight the barracks shows hp %d and %d steps left, want what was seen: %d and %d", hp, left, seenHP, seenLeft)
	}
	if hp, _ := w.shownTo(opponent, barracks); hp != 1 {
		t.Fatalf("its owner sees hp %d, want the live 1", hp)
	}
}

func TestOpponentSeesOnlyThreatsInItsSight(t *testing.T) {
	w := newTestWorld(t)
	intruder := w.spawn(player, w.r.unit(soldierName), w.sides[opponent].hq)
	w.updateFog()
	if w.threat(opponent) != intruder {
		t.Fatal("the opponent does not see an intruder at its door")
	}
	// Blinded, the opponent no longer knows the intruder is there.
	for _, e := range w.ents {
		if e.team == opponent {
			w.fog.RemoveSource(e.fogID)
		}
	}
	w.updateFog()
	if w.threat(opponent) != nil {
		t.Fatal("the opponent finds an intruder it cannot see")
	}
}

func TestBothHeadquartersFallingIsADraw(t *testing.T) {
	w := newTestWorld(t)
	w.sides[player].hq.hp = 0
	w.sides[opponent].hq.hp = 0
	w.step(step)
	if !w.over || !w.drawn {
		t.Fatalf("over=%v drawn=%v after both headquarters fell in one step", w.over, w.drawn)
	}
	g := &skirmish{data: w.data, w: w}
	if s := g.hud(); s.won || !s.drawn {
		t.Fatalf("the HUD reads won=%v drawn=%v, want a draw", s.won, s.drawn)
	}
}

func TestADeadEntityDoesNotAct(t *testing.T) {
	w := newTestWorld(t)
	w.sides[player].crystals = 1000
	if !w.buildBarracks(player) {
		t.Fatal("could not build a barracks")
	}
	b := w.buildings(player, w.r.building(barracksName))[0]
	b.buildLeft = 0
	if !w.train(player, w.r.unit(soldierName)) {
		t.Fatal("could not train a soldier")
	}
	b.trainLeft, b.hp = 1, 0
	before := len(unitsOf(w, player, soldierName))
	w.step(step)
	if got := len(unitsOf(w, player, soldierName)); got != before {
		t.Fatalf("a barracks destroyed this step still trained: %d soldiers, want %d", got, before)
	}
}
