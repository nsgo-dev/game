package ai_test

import (
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// trackedAction is a test leaf that records ticks and resets and runs for ticksNeeded ticks before returning finalStatus.
type trackedAction struct {
	name        string
	ticksNeeded int
	finalStatus ai.Status
	ticks       int
	resets      int
	log         *[]string
}

func (a *trackedAction) Tick(time.Duration) ai.Status {
	a.ticks++
	*a.log = append(*a.log, a.name)
	if a.ticks < a.ticksNeeded {
		return ai.StatusRunning
	}
	a.ticks = 0
	return a.finalStatus
}

func (a *trackedAction) Reset() {
	a.ticks = 0
	a.resets++
	*a.log = append(*a.log, "reset:"+a.name)
}

func TestBehaviorTreeReferenceScenarioAcrossLoopSteps(t *testing.T) {
	// Reference enemy AI driven at 20 Hz (50ms per fixed step):
	//   Selector(
	//     Guard(enemyNear, Cooldown(100ms, Action("attack"))),
	//     Sequence(Action("move"), Wait(100ms), Action("look")),
	//   )
	var log []string
	enemyNear := false

	move := &trackedAction{name: "move", ticksNeeded: 1, finalStatus: ai.StatusSuccess, log: &log}
	look := &trackedAction{name: "look", ticksNeeded: 1, finalStatus: ai.StatusSuccess, log: &log}
	wait := ai.Wait(100 * time.Millisecond)

	attack := ai.Action(func(time.Duration) ai.Status {
		log = append(log, "attack")
		return ai.StatusSuccess
	})

	tree := ai.Selector(
		ai.Guard(func() bool { return enemyNear }, ai.Cooldown(100*time.Millisecond, attack)),
		ai.Sequence(move, wait, look),
	)

	step := 0
	loop := &game.Loop{
		Rate: 20, // 50ms fixed steps
		Update: func(dt time.Duration) {
			step++
			// Enemy appears on steps 2..4, then leaves on step 5.
			enemyNear = step >= 2 && step <= 4
			tree.Tick(dt)
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		loop.Run(cx)
		return core.View{}
	})
	// Advance 350ms -> 7 steps at 20 Hz (50ms each).
	app.Advance(350 * time.Millisecond)
	if step != 7 {
		t.Fatalf("ran %d steps, want 7", step)
	}

	// Trace step-by-step:
	// Step 1 (50ms, enemyNear=false): Sequence runs "move" (Success) then Wait(50ms/100ms -> Running).
	// Step 2 (50ms, enemyNear=true):  Guard->Cooldown->attack succeeds (starts 100ms cooldown).
	//                                 Selector preempts running Sequence -> Reset resets move & look & Wait!
	// Step 3 (50ms, enemyNear=true):  Cooldown remaining 100ms->50ms -> Failure.
	//                                 Sequence starts fresh: "move" (Success) then Wait(50ms/100ms -> Running).
	// Step 4 (50ms, enemyNear=true):  Cooldown remaining 50ms->0 -> attack succeeds (starts 100ms cooldown).
	//                                 Selector preempts running Sequence -> Reset!
	// Step 5 (50ms, enemyNear=false): Sequence starts fresh: "move" (Success) then Wait(50ms/100ms -> Running).
	// Step 6 (50ms, enemyNear=false): Sequence resumes Wait(100ms/100ms -> Success) then "look" (Success)!
	// Step 7 (50ms, enemyNear=false): Sequence starts fresh: "move" (Success) then Wait(50ms/100ms -> Running).
	want := []string{
		"move",
		"attack", "reset:move", "reset:look",
		"move",
		"attack", "reset:move", "reset:look",
		"move",
		"look",
		"move",
	}
	if !slices.Equal(log, want) {
		t.Fatalf("leaf sequence =\n  %v\nwant:\n  %v", log, want)
	}
}

func TestBehaviorTreeSequenceMemoryAndReset(t *testing.T) {
	var log []string
	a := &trackedAction{name: "a", ticksNeeded: 1, finalStatus: ai.StatusSuccess, log: &log}
	b := &trackedAction{name: "b", ticksNeeded: 3, finalStatus: ai.StatusSuccess, log: &log}
	c := &trackedAction{name: "c", ticksNeeded: 1, finalStatus: ai.StatusSuccess, log: &log}

	seq := ai.Sequence(a, b, c)
	dt := 16 * time.Millisecond

	// Tick 1: a succeeds, b starts running (1/3).
	if st := seq.Tick(dt); st != ai.StatusRunning {
		t.Fatalf("tick 1 = %v, want Running", st)
	}
	// Tick 2: resumes directly at b (a is NOT re-ticked), b running (2/3).
	if st := seq.Tick(dt); st != ai.StatusRunning {
		t.Fatalf("tick 2 = %v, want Running", st)
	}
	// Tick 3: b finishes (3/3), c succeeds -> Sequence succeeds.
	if st := seq.Tick(dt); st != ai.StatusSuccess {
		t.Fatalf("tick 3 = %v, want Success", st)
	}

	want := []string{"a", "b", "b", "b", "c"}
	if !slices.Equal(log, want) {
		t.Fatalf("log = %v, want %v", log, want)
	}

	// Failure resets sequence cursor back to 0.
	failAtB := ai.Sequence(
		ai.Action(func(time.Duration) ai.Status { log = append(log, "s1"); return ai.StatusSuccess }),
		ai.Action(func(time.Duration) ai.Status { log = append(log, "s2"); return ai.StatusFailure }),
		ai.Action(func(time.Duration) ai.Status { log = append(log, "s3"); return ai.StatusSuccess }),
	)
	log = nil
	if st := failAtB.Tick(dt); st != ai.StatusFailure {
		t.Fatalf("failing sequence = %v, want Failure", st)
	}
	if st := failAtB.Tick(dt); st != ai.StatusFailure {
		t.Fatalf("re-tick failing sequence = %v, want Failure", st)
	}
	if !slices.Equal(log, []string{"s1", "s2", "s1", "s2"}) {
		t.Fatalf("fail log = %v, want [s1 s2 s1 s2]", log)
	}
}

func TestBehaviorTreeParallelPoliciesAndInterruption(t *testing.T) {
	dt := 10 * time.Millisecond

	// RequireAll: completed child is not re-ticked while sibling runs; failure interrupts running sibling.
	var log []string
	fast := &trackedAction{name: "fast", ticksNeeded: 1, finalStatus: ai.StatusSuccess, log: &log}
	slow := &trackedAction{name: "slow", ticksNeeded: 3, finalStatus: ai.StatusSuccess, log: &log}
	pAll := ai.Parallel(ai.RequireAll, fast, slow)

	if st := pAll.Tick(dt); st != ai.StatusRunning {
		t.Fatalf("tick 1 = %v, want Running", st)
	}
	if st := pAll.Tick(dt); st != ai.StatusRunning {
		t.Fatalf("tick 2 = %v, want Running", st)
	}
	if st := pAll.Tick(dt); st != ai.StatusSuccess {
		t.Fatalf("tick 3 = %v, want Success", st)
	}
	if !slices.Equal(log, []string{"fast", "slow", "slow", "slow"}) {
		t.Fatalf("RequireAll log = %v, want [fast slow slow slow]", log)
	}

	// RequireAll early failure resets still-running child.
	log = nil
	failAfter2 := &trackedAction{name: "fail", ticksNeeded: 2, finalStatus: ai.StatusFailure, log: &log}
	longRun := &trackedAction{name: "long", ticksNeeded: 5, finalStatus: ai.StatusSuccess, log: &log}
	pFail := ai.Parallel(ai.RequireAll, failAfter2, longRun)
	if st := pFail.Tick(dt); st != ai.StatusRunning {
		t.Fatalf("pFail tick 1 = %v, want Running", st)
	}
	if st := pFail.Tick(dt); st != ai.StatusFailure {
		t.Fatalf("pFail tick 2 = %v, want Failure", st)
	}
	if longRun.resets != 1 {
		t.Fatalf("longRun resets = %d, want 1", longRun.resets)
	}

	// RequireOne: succeeds as soon as one child succeeds, resetting still-running siblings.
	log = nil
	quickWin := &trackedAction{name: "win", ticksNeeded: 2, finalStatus: ai.StatusSuccess, log: &log}
	other := &trackedAction{name: "other", ticksNeeded: 5, finalStatus: ai.StatusSuccess, log: &log}
	pOne := ai.Parallel(ai.RequireOne, quickWin, other)
	if st := pOne.Tick(dt); st != ai.StatusRunning {
		t.Fatalf("pOne tick 1 = %v, want Running", st)
	}
	if st := pOne.Tick(dt); st != ai.StatusSuccess {
		t.Fatalf("pOne tick 2 = %v, want Success", st)
	}
	if other.resets != 1 {
		t.Fatalf("other resets = %d, want 1", other.resets)
	}
}

func TestBehaviorTreeDecoratorsAndLeaves(t *testing.T) {
	dt := 25 * time.Millisecond

	// Condition, Inverter, Succeeder, Failer.
	ok := true
	cond := ai.Condition(func() bool { return ok })
	if cond.Tick(dt) != ai.StatusSuccess {
		t.Fatal("true condition should succeed")
	}
	if ai.Inverter(cond).Tick(dt) != ai.StatusFailure {
		t.Fatal("Inverter(Success) should fail")
	}
	if ai.Failer(cond).Tick(dt) != ai.StatusFailure {
		t.Fatal("Failer(Success) should fail")
	}
	ok = false
	if ai.Inverter(cond).Tick(dt) != ai.StatusSuccess {
		t.Fatal("Inverter(Failure) should succeed")
	}
	if ai.Succeeder(cond).Tick(dt) != ai.StatusSuccess {
		t.Fatal("Succeeder(Failure) should succeed")
	}

	// Repeater (finite count).
	runs := 0
	rep := ai.Repeater(3, ai.Action(func(time.Duration) ai.Status {
		runs++
		return ai.StatusSuccess
	}))
	if rep.Tick(dt) != ai.StatusRunning || rep.Tick(dt) != ai.StatusRunning || rep.Tick(dt) != ai.StatusSuccess {
		t.Fatal("Repeater(3) should return Running, Running, Success")
	}
	if runs != 3 {
		t.Fatalf("Repeater ran %d times, want 3", runs)
	}

	// UntilFail.
	left := 2
	uf := ai.UntilFail(ai.Action(func(time.Duration) ai.Status {
		if left > 0 {
			left--
			return ai.StatusSuccess
		}
		return ai.StatusFailure
	}))
	if uf.Tick(dt) != ai.StatusRunning || uf.Tick(dt) != ai.StatusRunning || uf.Tick(dt) != ai.StatusSuccess {
		t.Fatal("UntilFail should return Running until child fails, then Success")
	}

	// Guard resets running child when condition turns false.
	var log []string
	armed := true
	work := &trackedAction{name: "work", ticksNeeded: 3, finalStatus: ai.StatusSuccess, log: &log}
	guarded := ai.Guard(func() bool { return armed }, work)
	if guarded.Tick(dt) != ai.StatusRunning {
		t.Fatal("guarded tick 1 should be Running")
	}
	armed = false
	if guarded.Tick(dt) != ai.StatusFailure {
		t.Fatal("guarded tick 2 should Fail")
	}
	if work.resets != 1 {
		t.Fatalf("guard did not reset interrupted child: resets = %d", work.resets)
	}

	// Cooldown: failures do not trigger cooldown; Reset clears cooldown.
	succeed := false
	calls := 0
	cd := ai.Cooldown(50*time.Millisecond, ai.Action(func(time.Duration) ai.Status {
		calls++
		if succeed {
			return ai.StatusSuccess
		}
		return ai.StatusFailure
	}))
	if cd.Tick(dt) != ai.StatusFailure {
		t.Fatal("failed child should return Failure")
	}
	succeed = true
	if cd.Tick(dt) != ai.StatusSuccess {
		t.Fatal("child should run immediately after previous failure")
	}
	if cd.Tick(dt) != ai.StatusFailure {
		t.Fatal("child should be blocked at 25ms/50ms cooldown")
	}
	if cd.Tick(dt) != ai.StatusSuccess {
		t.Fatal("child should run again once 50ms cooldown elapses")
	}
	// Reset clears active cooldown immediately.
	cd.Reset()
	if cd.Tick(dt) != ai.StatusSuccess {
		t.Fatal("child should run immediately after Cooldown.Reset()")
	}
	if calls != 4 {
		t.Fatalf("cooldown child calls = %d, want 4", calls)
	}

	// Wait accumulates dt and resets cleanly.
	w := ai.Wait(60 * time.Millisecond)
	if w.Tick(25*time.Millisecond) != ai.StatusRunning {
		t.Fatal("wait at 25ms should be Running")
	}
	if w.Tick(25*time.Millisecond) != ai.StatusRunning {
		t.Fatal("wait at 50ms should be Running")
	}
	w.Reset()
	if w.Tick(25*time.Millisecond) != ai.StatusRunning {
		t.Fatal("wait after Reset at 25ms should be Running")
	}
	if w.Tick(35*time.Millisecond) != ai.StatusSuccess {
		t.Fatal("wait at 60ms should Succeed")
	}
}
