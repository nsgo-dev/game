package main

import (
	"fmt"
	"testing"

	"github.com/nsgo-dev/game/packages/game/fighting"
	"github.com/nsgo-dev/game/packages/game/framedata"
)

// testMatch starts a match between the fighters with ids a and b and runs it to the start of the fight.
func testMatch(t *testing.T, a, b string) *match {
	t.Helper()
	r, err := loadRoster(dataFS)
	if err != nil {
		t.Fatal(err)
	}
	pick := func(id string) *fighter {
		for _, f := range r.fighters {
			if f.def.ID == id {
				return f
			}
		}
		t.Fatalf("no fighter %q", id)
		return nil
	}
	m := newMatch(r, pick(a), pick(b))
	for m.phase == phaseIntro {
		m.step([2]control{neutral, neutral})
	}
	return m
}

// place stands the fighters gap points apart in the middle of the stage, facing each other.
func place(m *match, gap float32) {
	mid := (m.rules.StageLeft + m.rules.StageRight) / 2
	m.players[0].f.X, m.players[0].f.FacingLeft = mid-gap/2, false
	m.players[1].f.X, m.players[1].f.FacingLeft = mid+gap/2, true
}

// rel returns the control for p holding the direction d relative to the side it faces and the named buttons.
func rel(p *player, d fighting.Direction, buttons ...string) control {
	return control{dir: p.facing().Relative(d), held: buttonMask(p, buttons)}
}

// run steps the match n times with the same controls and returns every event.
func run(m *match, n int, in0, in1 control) []event {
	var all []event
	for range n {
		all = append(all, m.step([2]control{in0, in1})...)
	}
	return all
}

// play steps the match through a script of controls for the first player, each held for two steps, while the second holds in1.
func play(m *match, in1 control, steps ...control) []event {
	var all []event
	for _, in := range steps {
		all = append(all, run(m, 2, in, in1)...)
	}
	return all
}

func count(events []event, kind eventKind) int {
	n := 0
	for _, e := range events {
		if e.kind == kind {
			n++
		}
	}
	return n
}

func TestQuarterCircleThrowsAProjectileThatHits(t *testing.T) {
	m := testMatch(t, "kestrel", "basalt")
	place(m, 220)
	p, o := m.players[0], m.players[1]
	events := play(m, neutral, rel(p, fighting.Down), rel(p, fighting.DownForward), rel(p, fighting.Forward, "light"))
	if p.moveName() != "gale" {
		t.Fatalf("2, 3, 6 and light started %q, want gale", p.moveName())
	}
	events = append(events, run(m, 120, neutral, neutral)...)
	if count(events, evHit) != 1 || o.health != o.fighter.def.Health-70 {
		t.Fatalf("the gale hit %d times and left %d health, want 1 hit of 70 from %d", count(events, evHit), o.health, o.fighter.def.Health)
	}
	if p.shot != nil || len(m.shots) != 0 {
		t.Fatal("the projectile is still in flight after it hit")
	}
}

func TestOneProjectileAtATime(t *testing.T) {
	m := testMatch(t, "kestrel", "basalt")
	place(m, 360)
	p := m.players[0]
	qcf := []control{rel(p, fighting.Down), rel(p, fighting.DownForward), rel(p, fighting.Forward, "light")}
	play(m, neutral, qcf...)
	run(m, 50, neutral, neutral)
	if p.shot == nil {
		t.Fatal("no projectile in flight")
	}
	play(m, neutral, qcf...)
	if p.moveName() == "gale" {
		t.Fatal("a second gale started while the first was still in flight")
	}
}

func TestRisingAttackIsInvulnerableAsItStarts(t *testing.T) {
	m := testMatch(t, "kestrel", "basalt")
	place(m, 60)
	p := m.players[0]
	play(m, neutral, rel(p, fighting.Forward), rel(p, fighting.Down), rel(p, fighting.DownForward, "heavy"))
	if p.moveName() != "talon" {
		t.Fatalf("6, 2, 3 and heavy started %q, want talon", p.moveName())
	}
	for p.f.Frame() < 6 {
		for _, b := range p.f.Boxes(nil) {
			if b.Kind == framedata.KindHurt {
				t.Fatalf("talon has a hurtbox on frame %d", p.f.Frame())
			}
		}
		run(m, 1, neutral, neutral)
	}
	run(m, 20, neutral, neutral)
	if !p.airborne && p.f.Y == m.rules.Floor {
		t.Fatal("talon did not lift the fighter off the floor")
	}
}

func TestChargeMoveNeedsTheCharge(t *testing.T) {
	m := testMatch(t, "basalt", "kestrel")
	place(m, 200)
	p := m.players[0]
	run(m, 10, rel(p, fighting.Back), neutral)
	play(m, neutral, rel(p, fighting.Forward, "heavy"))
	if p.moveName() == "ram" {
		t.Fatal("ram started after a charge of 10 steps")
	}
	run(m, 60, neutral, neutral)
	place(m, 200)
	run(m, 45, rel(p, fighting.Back), neutral)
	play(m, neutral, rel(p, fighting.Forward, "heavy"))
	if p.moveName() != "ram" {
		t.Fatalf("back for 45 steps, then forward and heavy started %q, want ram", p.moveName())
	}
	x := p.f.X
	run(m, 15, neutral, neutral)
	if p.f.X-x < 40 {
		t.Fatalf("ram moved %.1f points forward, want at least 40", p.f.X-x)
	}
}

func TestGuardStopsMidsAndMatchesHighAndLow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		guard   fighting.Direction
		attack  []fighting.Direction
		button  string
		blocked bool
	}{
		{"standing guard stops a mid", fighting.Back, []fighting.Direction{fighting.Neutral}, "heavy", true},
		{"crouching guard stops a mid", fighting.DownBack, []fighting.Direction{fighting.Neutral}, "heavy", true},
		{"standing guard loses to a low", fighting.Back, []fighting.Direction{fighting.Down}, "light", false},
		{"crouching guard stops a low", fighting.DownBack, []fighting.Direction{fighting.Down}, "light", true},
		{"no guard is hit", fighting.Neutral, []fighting.Direction{fighting.Neutral}, "heavy", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testMatch(t, "kestrel", "basalt")
			place(m, 50)
			a, d := m.players[0], m.players[1]
			guard := rel(d, tc.guard)
			var events []event
			for _, dir := range tc.attack {
				events = append(events, run(m, 2, rel(a, dir, tc.button), guard)...)
			}
			events = append(events, run(m, 30, rel(a, tc.attack[len(tc.attack)-1]), guard)...)
			hits, blocks := count(events, evHit), count(events, evBlock)
			if tc.blocked && (blocks != 1 || hits != 0 || d.health != d.fighter.def.Health) {
				t.Fatalf("%d hits, %d blocks, health %d: want it blocked without damage", hits, blocks, d.health)
			}
			if !tc.blocked && (hits != 1 || blocks != 0 || d.health == d.fighter.def.Health) {
				t.Fatalf("%d hits, %d blocks, health %d: want it to hit", hits, blocks, d.health)
			}
		})
	}
}

func TestOverheadBeatsACrouchingGuard(t *testing.T) {
	m := testMatch(t, "kestrel", "basalt")
	place(m, 110)
	a, d := m.players[0], m.players[1]
	guard := rel(d, fighting.DownBack)
	events := run(m, 3, rel(a, fighting.UpForward), guard)
	for range 40 {
		in := neutral
		if a.airborne && a.vy > 0 && !a.airAttacked {
			in = rel(a, fighting.Neutral, "heavy")
		}
		events = append(events, m.step([2]control{in, guard})...)
	}
	if count(events, evHit) != 1 || d.health == d.fighter.def.Health {
		t.Fatalf("a jumping heavy against a crouching guard: %d hits, %d blocks", count(events, evHit), count(events, evBlock))
	}
}

func TestBlockedSpecialDealsChipDamage(t *testing.T) {
	m := testMatch(t, "kestrel", "basalt")
	place(m, 220)
	a, d := m.players[0], m.players[1]
	guard := rel(d, fighting.Back)
	events := play(m, guard, rel(a, fighting.Down), rel(a, fighting.DownForward), rel(a, fighting.Forward, "light"))
	events = append(events, run(m, 120, neutral, guard)...)
	if count(events, evBlock) != 1 || d.health != d.fighter.def.Health-70/m.rules.ChipDivisor {
		t.Fatalf("%d blocks left %d of %d health, want 1 block and %d chip damage", count(events, evBlock), d.health, d.fighter.def.Health, 70/m.rules.ChipDivisor)
	}
}

func TestCancelComboScalesDamageAndNamesIt(t *testing.T) {
	m := testMatch(t, "kestrel", "basalt")
	place(m, 45)
	a, d := m.players[0], m.players[1]
	events := run(m, 2, rel(a, fighting.Neutral, "light"), neutral)
	// Press heavy while light connects, inside its cancel window.
	events = append(events, run(m, 8, neutral, neutral)...)
	events = append(events, run(m, 2, rel(a, fighting.Neutral, "heavy"), neutral)...)
	events = append(events, run(m, 40, neutral, neutral)...)
	if hits := count(events, evHit); hits != 2 || a.combo != 0 && d.f.Stun() == 0 {
		t.Fatalf("light into heavy: %d hits, want 2", hits)
	}
	want := d.fighter.def.Health - 40 - 90*(100-m.rules.ComboScaling)/100
	if d.health != want {
		t.Fatalf("health after the combo = %d, want %d (the heavy scaled to %d%%)", d.health, want, 100-m.rules.ComboScaling)
	}
	var combo string
	for _, e := range events {
		if e.kind == evCombo {
			combo = e.name
		}
	}
	if combo != "chain" {
		t.Fatalf("combo event %q, want chain", combo)
	}
}

func TestTimeUpGoesToTheHealthierFighter(t *testing.T) {
	m := testMatch(t, "kestrel", "basalt")
	place(m, 300)
	m.players[0].health = 450
	m.players[1].health = 600
	events := run(m, m.clock, neutral, neutral)
	if count(events, evTimeUp) != 1 || m.winner != 1 || m.players[1].wins != 1 {
		t.Fatalf("time up: %d events, winner %d, wins %d", count(events, evTimeUp), m.winner, m.players[1].wins)
	}
	if m.phase != phaseRoundOver {
		t.Fatalf("phase %v after time up, want the round over", m.phase)
	}
}

func TestTimeUpWithEqualSharesIsADraw(t *testing.T) {
	m := testMatch(t, "kestrel", "basalt")
	place(m, 300)
	a, b := m.players[0], m.players[1]
	// Half of each fighter's health: more raw health for the sturdier one, the same share for both.
	a.health, b.health = a.fighter.def.Health/2, b.fighter.def.Health/2
	events := run(m, m.clock, neutral, neutral)
	if count(events, evTimeUp) != 1 || m.winner != -1 || a.wins != 1 || b.wins != 1 {
		t.Fatalf("time up: %d events, winner %d, wins %d and %d", count(events, evTimeUp), m.winner, a.wins, b.wins)
	}
}

func TestDoubleKnockoutCountsForBoth(t *testing.T) {
	m := testMatch(t, "kestrel", "basalt")
	place(m, 300)
	a, b := m.players[0], m.players[1]
	// One round each before this one, so the draw brings both to roundsToWin.
	a.wins, b.wins = m.rules.RoundsToWin-1, m.rules.RoundsToWin-1
	a.health, b.health = 0, 0
	events := run(m, 1, neutral, neutral)
	if count(events, evKO) != 1 || m.winner != -1 || a.wins != m.rules.RoundsToWin || b.wins != m.rules.RoundsToWin {
		t.Fatalf("double knockout: %d knockouts, winner %d, wins %d and %d", count(events, evKO), m.winner, a.wins, b.wins)
	}
	events = run(m, m.rules.OutroSteps, neutral, neutral)
	var over []event
	for _, e := range events {
		if e.kind == evMatchOver {
			over = append(over, e)
		}
	}
	if len(over) != 1 || over[0].player != -1 || m.phase != phaseMatchOver || m.winner != -1 {
		t.Fatalf("match over events %v, phase %v, winner %d, want one drawn match", over, m.phase, m.winner)
	}
	if got := (&fightingGame{mode: modeVersus}).resultText(m.winner); got != "Draw" {
		t.Fatalf("result %q, want Draw", got)
	}
}

func TestDrawAfterAWinEndsTheMatchForTheRoundWinner(t *testing.T) {
	m := testMatch(t, "kestrel", "basalt")
	place(m, 300)
	a, b := m.players[0], m.players[1]
	a.wins = m.rules.RoundsToWin - 1
	a.health, b.health = 0, 0
	run(m, 1, neutral, neutral)
	events := run(m, m.rules.OutroSteps, neutral, neutral)
	if count(events, evMatchOver) != 1 || m.winner != 0 {
		t.Fatalf("%d match over events, winner %d, want the match won by the first player", count(events, evMatchOver), m.winner)
	}
	if got := (&fightingGame{mode: modeCPU}).resultText(m.winner); got != "You win" {
		t.Fatalf("result %q, want You win", got)
	}
}

func TestKnockoutsWinTheMatch(t *testing.T) {
	m := testMatch(t, "kestrel", "basalt")
	for round := 1; round <= m.rules.RoundsToWin; round++ {
		if m.round != round || m.phase != phaseFight {
			t.Fatalf("round %d phase %v, want round %d being fought", m.round, m.phase, round)
		}
		place(m, 45)
		m.players[1].health = 1
		events := run(m, 2, rel(m.players[0], fighting.Neutral, "light"), neutral)
		events = append(events, run(m, 20, neutral, neutral)...)
		if count(events, evKO) != 1 || m.players[1].moveName() != "ko" {
			t.Fatalf("round %d: %d knockouts, the loser plays %q", round, count(events, evKO), m.players[1].moveName())
		}
		events = run(m, m.rules.OutroSteps, neutral, neutral)
		if round < m.rules.RoundsToWin {
			for m.phase == phaseIntro {
				m.step([2]control{neutral, neutral})
			}
		} else if count(events, evMatchOver) != 1 {
			t.Fatal("the last knockout did not end the match")
		}
	}
	if m.phase != phaseMatchOver || m.winner != 0 {
		t.Fatalf("phase %v winner %d, want the match over and won by the first player", m.phase, m.winner)
	}
}

func TestFightersFaceEachOtherAndStayApart(t *testing.T) {
	m := testMatch(t, "kestrel", "basalt")
	place(m, 100)
	a, b := m.players[0], m.players[1]
	run(m, 120, rel(a, fighting.Forward), rel(b, fighting.Forward))
	if gap := b.f.X - a.f.X; gap < m.rules.PushWidth-0.01 {
		t.Fatalf("walking into each other left them %.1f apart, want at least %v", gap, m.rules.PushWidth)
	}
	// A jump over the other fighter turns both around once they land.
	place(m, 40)
	run(m, 2, rel(a, fighting.UpForward), neutral)
	run(m, 80, neutral, neutral)
	if !(a.f.X > b.f.X && a.f.FacingLeft && !b.f.FacingLeft) {
		t.Fatalf("after a jump over: a at %.1f facing left %v, b at %.1f facing left %v", a.f.X, a.f.FacingLeft, b.f.X, b.f.FacingLeft)
	}
}

func TestMatchIsDeterministic(t *testing.T) {
	r, err := loadRoster(dataFS)
	if err != nil {
		t.Fatal(err)
	}
	state := func() string {
		m := testMatch(t, "kestrel", "basalt")
		c0, c1 := newCPU(&r.file.CPU), newCPU(&r.file.CPU)
		for range 1800 {
			m.step([2]control{c0.control(m, m.players[0]), c1.control(m, m.players[1])})
		}
		return fmt.Sprintf("%v %v %v %v %v", m.players[0].health, m.players[1].health, m.players[0].f.X, m.players[1].f.X, m.round)
	}
	if a, b := state(), state(); a != b {
		t.Fatalf("two CPU-against-CPU matches ended differently: %s and %s", a, b)
	}
}
