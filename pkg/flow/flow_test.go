package flow_test

import (
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/flow"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func startLoop(t *testing.T, rate int, update func(dt time.Duration)) *nsgotest.App {
	t.Helper()
	l := &game.Loop{Rate: rate, Update: update}
	return nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})
}

func TestTimersFireOnExactLoopStepAndPause(t *testing.T) {
	var timers flow.Timers
	step := 0
	var afterStep int
	var everySteps []int

	oneShot := timers.After(100*time.Millisecond, func() { afterStep = step })
	repeating := timers.Every(60*time.Millisecond, func() { everySteps = append(everySteps, step) })

	if !oneShot.Active() || oneShot.Remaining() != 100*time.Millisecond {
		t.Fatalf("oneShot active=%v remaining=%v, want true and 100ms", oneShot.Active(), oneShot.Remaining())
	}

	// 50 Hz = exact 20 ms per fixed step.
	app := startLoop(t, 50, func(dt time.Duration) {
		step++
		timers.Update(dt)
	})

	// 4 steps = 80 ms: repeating fired at step 3 (60 ms), oneShot still has 20 ms left.
	app.Advance(80 * time.Millisecond)
	if step != 4 {
		t.Fatalf("step = %d, want 4", step)
	}
	if afterStep != 0 || !oneShot.Active() || oneShot.Remaining() != 20*time.Millisecond {
		t.Fatalf("at 80ms: afterStep=%d active=%v remaining=%v", afterStep, oneShot.Active(), oneShot.Remaining())
	}
	if !slices.Equal(everySteps, []int{3}) {
		t.Fatalf("everySteps at 80ms = %v, want [3]", everySteps)
	}

	// Pause the scheduler: advancing the loop runs updates, but timers do not advance.
	timers.SetPaused(true)
	if !timers.Paused {
		t.Fatal("timers.Paused = false after SetPaused(true)")
	}
	app.Advance(200 * time.Millisecond)
	if afterStep != 0 || !oneShot.Active() || oneShot.Remaining() != 20*time.Millisecond {
		t.Fatalf("while paused: afterStep=%d active=%v remaining=%v", afterStep, oneShot.Active(), oneShot.Remaining())
	}
	if !slices.Equal(everySteps, []int{3}) {
		t.Fatalf("everySteps while paused = %v, want [3]", everySteps)
	}

	// Unpause: oneShot fires on the very next step (step 15), repeating on step 16 (20ms left + 20ms).
	timers.SetPaused(false)
	app.Advance(60 * time.Millisecond)
	if afterStep != 15 || oneShot.Active() || oneShot.Remaining() != 0 {
		t.Fatalf("after resume: afterStep=%d active=%v remaining=%v", afterStep, oneShot.Active(), oneShot.Remaining())
	}
	if !slices.Equal(everySteps, []int{3, 16}) {
		t.Fatalf("everySteps after resume = %v, want [3 16]", everySteps)
	}

	// Background lifecycle pauses game.Loop itself, so timers pause automatically too.
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	app.Advance(time.Second)
	if !slices.Equal(everySteps, []int{3, 16}) {
		t.Fatalf("everySteps in background = %v, want [3 16]", everySteps)
	}
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	app.Advance(120 * time.Millisecond)
	if !slices.Equal(everySteps, []int{3, 16, 19, 22}) {
		t.Fatalf("everySteps after foreground = %v, want [3 16 19 22]", everySteps)
	}

	repeating.Cancel()
	if repeating.Active() || repeating.Remaining() != 0 {
		t.Fatalf("after Cancel: active=%v remaining=%v", repeating.Active(), repeating.Remaining())
	}
	app.Advance(200 * time.Millisecond)
	if !slices.Equal(everySteps, []int{3, 16, 19, 22}) {
		t.Fatalf("everySteps after cancel = %v, want [3 16 19 22]", everySteps)
	}
}

func TestTimersEveryPreservesRemainderWithoutDrift(t *testing.T) {
	var timers flow.Timers
	ticks := 0
	timers.Every(50*time.Millisecond, func() { ticks++ })

	// 60 Hz steps (16.666666ms) do not divide 50 ms evenly; remainder carry keeps 20 ticks/sec with no drift.
	app := startLoop(t, 60, func(dt time.Duration) {
		timers.Update(dt)
	})
	// 10s + 1 frame so 60*16666666ns*10 crosses the full 10.000s mark.
	app.Advance(10*time.Second + 17*time.Millisecond)
	if ticks != 200 {
		t.Fatalf("ticks over 10s = %d, want 200", ticks)
	}

	// A single large step fires multiple periods and stops immediately if cancelled mid-burst.
	var burst flow.Timers
	var tm *flow.Timer
	burstHits := 0
	tm = burst.Every(10*time.Millisecond, func() {
		burstHits++
		if burstHits == 2 {
			tm.Cancel()
		}
	})
	burst.Update(50 * time.Millisecond)
	if burstHits != 2 || tm.Active() {
		t.Fatalf("burstHits=%d active=%v, want 2 and false", burstHits, tm.Active())
	}
}

func TestTimersDeterministicOrderAndSafeMutationInCallback(t *testing.T) {
	var timers flow.Timers
	var order []string
	var second *flow.Timer

	timers.After(15*time.Millisecond, func() {
		order = append(order, "first")
		second.Cancel()
		// Newly scheduled timer starts on the next Update, not this one.
		timers.After(0, func() { order = append(order, "added") })
	})
	second = timers.After(5*time.Millisecond, func() {
		order = append(order, "second")
	})
	timers.After(10*time.Millisecond, func() {
		order = append(order, "third")
	})

	timers.Update(20 * time.Millisecond)
	if !slices.Equal(order, []string{"first", "third"}) {
		t.Fatalf("step 1 order = %v, want [first third]", order)
	}

	timers.Update(1 * time.Millisecond)
	if !slices.Equal(order, []string{"first", "third", "added"}) {
		t.Fatalf("step 2 order = %v, want [first third added]", order)
	}

	// Nil timer safety, Clear and non-positive Every interval.
	var nilTimer *flow.Timer
	nilTimer.Cancel()
	if nilTimer.Active() || nilTimer.Remaining() != 0 {
		t.Fatal("nil Timer should be inactive with 0 remaining")
	}

	zeroTicks := 0
	timers.Every(0, func() { zeroTicks++ })
	timers.Update(10 * time.Millisecond)
	timers.Update(10 * time.Millisecond)
	if zeroTicks != 2 {
		t.Fatalf("Every(0) ticks = %d, want 2", zeroTicks)
	}
	timers.Clear()
	timers.Update(10 * time.Millisecond)
	if zeroTicks != 2 {
		t.Fatalf("Every(0) fired after Clear: %d", zeroTicks)
	}
}

func TestMachinePatrolAndChaseOnLoopSteps(t *testing.T) {
	type aiState int
	const (
		patrol aiState = iota + 1
		chase
		search
		dead
	)

	var log []string
	step := 0
	dist := 100.0
	hp := 10

	m := flow.NewMachine(patrol)
	m.AddState(patrol, flow.State{
		Enter:  func() { log = append(log, "enter:patrol") },
		Update: func(dt time.Duration) { log = append(log, "update:patrol") },
		Exit:   func() { log = append(log, "exit:patrol") },
	})
	m.AddState(chase, flow.State{
		Enter:  func() { log = append(log, "enter:chase") },
		Update: func(dt time.Duration) { log = append(log, "update:chase") },
		Exit:   func() { log = append(log, "exit:chase") },
	})
	m.AddState(search, flow.State{
		Enter: func() { log = append(log, "enter:search") },
	})
	m.AddState(dead, flow.State{
		Enter: func() { log = append(log, "enter:dead") },
	})

	// Declaration order: dead wildcard takes priority over patrol/chase transitions.
	m.AddAnyTransition(dead, func() bool { return hp <= 0 })
	m.AddTransition(patrol, chase, func() bool { return dist < 50 })
	m.AddTransition(chase, search, func() bool { return dist > 80 })
	m.AddTransition(search, patrol, func() bool { return m.TimeInState() >= 60*time.Millisecond })

	m.Start()
	m.Start() // idempotent
	if !slices.Equal(log, []string{"enter:patrol"}) {
		t.Fatalf("after Start: log = %v, want [enter:patrol]", log)
	}

	app := startLoop(t, 50, func(dt time.Duration) {
		step++
		// Move player into sight on step 3, out of sight on step 5, kill on step 10.
		switch step {
		case 3:
			dist = 30
		case 5:
			dist = 90
		case 10:
			hp = 0
		}
		m.Update(dt)
	})

	// Steps 1-2: stay in patrol (40ms).
	app.Advance(40 * time.Millisecond)
	if m.Current() != patrol || m.TimeInState() != 40*time.Millisecond {
		t.Fatalf("step 2: state=%v time=%v, want patrol and 40ms", m.Current(), m.TimeInState())
	}

	// Step 3: dist=30 triggers patrol -> chase, runs Exit(patrol), Enter(chase), Update(chase).
	app.Advance(20 * time.Millisecond)
	if m.Current() != chase || m.Previous() != patrol || m.TimeInState() != 20*time.Millisecond {
		t.Fatalf("step 3: cur=%v prev=%v time=%v", m.Current(), m.Previous(), m.TimeInState())
	}
	wantLog := []string{
		"enter:patrol",
		"update:patrol",
		"update:patrol",
		"exit:patrol",
		"enter:chase",
		"update:chase",
	}
	if !slices.Equal(log, wantLog) {
		t.Fatalf("log at step 3 = %v, want %v", log, wantLog)
	}

	// Step 4: chase (40ms); Step 5: chase -> search (20ms); Steps 6-7: search (60ms); Step 8: search -> patrol (20ms).
	app.Advance(100 * time.Millisecond)
	if step != 8 || m.Current() != patrol || m.Previous() != search || m.TimeInState() != 20*time.Millisecond {
		t.Fatalf("step %d: cur=%v prev=%v time=%v, want step 8 patrol from search 20ms", step, m.Current(), m.Previous(), m.TimeInState())
	}

	// Step 9: patrol; Step 10: hp=0 -> dead; Step 11: stays dead without re-entering.
	app.Advance(60 * time.Millisecond)
	if m.Current() != dead || m.Previous() != patrol || m.TimeInState() != 40*time.Millisecond {
		t.Fatalf("step 11: cur=%v prev=%v time=%v", m.Current(), m.Previous(), m.TimeInState())
	}
	deadEnters := 0
	for _, entry := range log {
		if entry == "enter:dead" {
			deadEnters++
		}
	}
	if deadEnters != 1 {
		t.Fatalf("enter:dead ran %d times, want 1", deadEnters)
	}

	// Explicit SetState switches cleanly and is a no-op for the current state.
	m.SetState(dead)
	m.SetState(patrol)
	if m.Current() != patrol || m.Previous() != dead || m.TimeInState() != 0 {
		t.Fatalf("after SetState(patrol): cur=%v prev=%v time=%v", m.Current(), m.Previous(), m.TimeInState())
	}
}

func TestMachineAutoStartsOnUpdate(t *testing.T) {
	entered := 0
	updated := 0
	m := flow.NewMachine("idle")
	m.AddState("idle", flow.State{
		Enter:  func() { entered++ },
		Update: func(dt time.Duration) { updated++ },
	})
	m.Update(16 * time.Millisecond)
	m.Update(16 * time.Millisecond)
	if entered != 1 || updated != 2 || m.TimeInState() != 32*time.Millisecond {
		t.Fatalf("entered=%d updated=%d time=%v", entered, updated, m.TimeInState())
	}
}

func TestSequenceWaitSpawnWaitUntilClearedAndCancel(t *testing.T) {
	var runner flow.Runner
	step := 0
	spawnStep := 0
	doneStep := 0
	enemies := 0

	wave := runner.Start(flow.Seq(
		flow.Wait(2*time.Second),
		flow.Do(func() {
			spawnStep = step
			enemies = 3
		}),
		flow.WaitUntil(func() bool { return enemies == 0 }),
		flow.Do(func() {
			doneStep = step
		}),
	))

	app := startLoop(t, 50, func(dt time.Duration) {
		step++
		if step == 125 {
			enemies = 0
		}
		runner.Update(dt)
	})

	// Just before 2s (99 steps = 1.98s): not spawned yet.
	app.Advance(1980 * time.Millisecond)
	if spawnStep != 0 || wave.Done() {
		t.Fatalf("at 1.98s: spawnStep=%d done=%v", spawnStep, wave.Done())
	}

	// Step 100 (exact 2.00s): spawns enemies, waits until cleared.
	app.Advance(20 * time.Millisecond)
	if spawnStep != 100 || enemies != 3 || doneStep != 0 || wave.Done() {
		t.Fatalf("at 2.00s: spawnStep=%d enemies=%d doneStep=%d done=%v", spawnStep, enemies, doneStep, wave.Done())
	}

	// Advance to step 124: still waiting.
	app.Advance(480 * time.Millisecond)
	if doneStep != 0 || wave.Done() {
		t.Fatalf("at step 124: doneStep=%d done=%v", doneStep, wave.Done())
	}

	// Step 125: enemies cleared -> finishes on exact step 125.
	app.Advance(20 * time.Millisecond)
	if doneStep != 125 || !wave.Done() {
		t.Fatalf("at step 125: doneStep=%d done=%v, want 125 and true", doneStep, wave.Done())
	}

	// Cancellation mid-sequence prevents subsequent steps from running.
	ranAfterCancel := false
	cancelable := runner.Start(flow.Seq(
		flow.Wait(100*time.Millisecond),
		flow.Do(func() { ranAfterCancel = true }),
	))
	app.Advance(40 * time.Millisecond)
	if cancelable.Done() {
		t.Fatal("cancelable done too early")
	}
	cancelable.Cancel()
	if !cancelable.Done() {
		t.Fatal("cancelable not done after Cancel")
	}
	app.Advance(200 * time.Millisecond)
	if ranAfterCancel {
		t.Fatal("cancelled sequence ran subsequent Do step")
	}
}

func TestSequenceCombinators(t *testing.T) {
	t.Run("SeqCarriesLeftoverTime", func(t *testing.T) {
		var runner flow.Runner
		finished := false
		h := runner.Start(flow.Seq(
			flow.Wait(25*time.Millisecond),
			flow.Wait(25*time.Millisecond),
			flow.Do(func() { finished = true }),
		))
		// Two 30ms steps = 60ms >= 50ms total; without leftover carry each Wait would take a full step and still finish,
		// so test with 20ms steps: 3 steps = 60ms finishes both 25ms waits (50ms total).
		runner.Update(20 * time.Millisecond)
		runner.Update(20 * time.Millisecond)
		if finished || h.Done() {
			t.Fatal("finished before 50ms")
		}
		runner.Update(20 * time.Millisecond)
		if !finished || !h.Done() {
			t.Fatal("expected completion at 60ms with leftover carry")
		}
	})

	t.Run("While", func(t *testing.T) {
		var runner flow.Runner
		pos := 0
		arrived := false
		h := runner.Start(flow.Seq(
			flow.While(
				func() bool { return pos < 30 },
				func(dt time.Duration) { pos += int(dt / time.Millisecond) },
			),
			flow.Do(func() { arrived = true }),
		))
		runner.Update(10 * time.Millisecond)
		runner.Update(10 * time.Millisecond)
		if pos != 20 || arrived || h.Done() {
			t.Fatalf("after 20ms: pos=%d arrived=%v done=%v", pos, arrived, h.Done())
		}
		runner.Update(10 * time.Millisecond)
		if pos != 30 || !arrived || !h.Done() {
			t.Fatalf("after 30ms: pos=%d arrived=%v done=%v", pos, arrived, h.Done())
		}
	})

	t.Run("Parallel", func(t *testing.T) {
		var runner flow.Runner
		var events []string
		h := runner.Start(flow.Seq(
			flow.Parallel(
				flow.Seq(flow.Wait(20*time.Millisecond), flow.Do(func() { events = append(events, "fast") })),
				flow.Seq(flow.Wait(50*time.Millisecond), flow.Do(func() { events = append(events, "slow") })),
			),
			flow.Do(func() { events = append(events, "joined") }),
		))
		runner.Update(30 * time.Millisecond)
		if !slices.Equal(events, []string{"fast"}) || h.Done() {
			t.Fatalf("at 30ms: events=%v done=%v", events, h.Done())
		}
		runner.Update(30 * time.Millisecond)
		if !slices.Equal(events, []string{"fast", "slow", "joined"}) || !h.Done() {
			t.Fatalf("at 60ms: events=%v done=%v", events, h.Done())
		}
	})

	t.Run("RaceCancelsLoser", func(t *testing.T) {
		var runner flow.Runner
		winner, loser := false, false
		h := runner.Start(flow.Race(
			flow.Seq(flow.Wait(40*time.Millisecond), flow.Do(func() { loser = true })),
			flow.Seq(flow.Wait(20*time.Millisecond), flow.Do(func() { winner = true })),
		))
		runner.Update(20 * time.Millisecond)
		if !winner || loser || !h.Done() {
			t.Fatalf("after 20ms: winner=%v loser=%v done=%v", winner, loser, h.Done())
		}
		runner.Update(100 * time.Millisecond)
		if loser {
			t.Fatal("losing branch ran after Race completed")
		}
	})

	t.Run("RepeatAndLoop", func(t *testing.T) {
		var runner flow.Runner
		count := 0
		h := runner.Start(flow.Repeat(3, func() flow.Step {
			return flow.Seq(
				flow.Wait(20*time.Millisecond),
				flow.Do(func() { count++ }),
			)
		}))
		runner.Update(20 * time.Millisecond)
		runner.Update(20 * time.Millisecond)
		if count != 2 || h.Done() {
			t.Fatalf("after 40ms: count=%d done=%v", count, h.Done())
		}
		runner.Update(20 * time.Millisecond)
		if count != 3 || !h.Done() {
			t.Fatalf("after 60ms: count=%d done=%v", count, h.Done())
		}

		// Infinite zero-duration Loop runs once per step without hanging and stops on Cancel.
		loopTicks := 0
		lh := runner.Start(flow.Loop(func() flow.Step {
			return flow.Do(func() { loopTicks++ })
		}))
		runner.Update(16 * time.Millisecond)
		runner.Update(16 * time.Millisecond)
		if loopTicks != 2 || lh.Done() {
			t.Fatalf("loopTicks=%d done=%v, want 2 and false", loopTicks, lh.Done())
		}
		lh.Cancel()
		runner.Update(16 * time.Millisecond)
		if loopTicks != 2 || !lh.Done() {
			t.Fatalf("after cancel: loopTicks=%d done=%v", loopTicks, lh.Done())
		}
	})

	t.Run("SelfCancelStopsSeq", func(t *testing.T) {
		var runner flow.Runner
		var h *flow.Handle
		ranSecond := false
		h = runner.Start(flow.Seq(
			flow.Do(func() { h.Cancel() }),
			flow.Do(func() { ranSecond = true }),
		))
		runner.Update(16 * time.Millisecond)
		if !h.Done() || ranSecond {
			t.Fatalf("done=%v ranSecond=%v, want true and false", h.Done(), ranSecond)
		}
	})
}

func TestBusSubscriptionOrderAndInStepDispatch(t *testing.T) {
	type scored struct{ Points int }
	type hit struct{ Damage int }

	var busA, busB flow.Bus
	var log []string
	step := 0

	// Register handlers on busA in order.
	flow.Subscribe(&busA, func(e scored) { log = append(log, "sub1") })
	flow.Subscribe(&busA, func(e scored) { log = append(log, "sub2") })
	flow.Subscribe(&busA, func(e scored) { log = append(log, "sub3") })

	// Separate Bus instance has zero shared state with busA.
	busBHits := 0
	flow.Subscribe(&busB, func(e scored) { busBHits++ })

	// Different event type does not collide.
	damage := 0
	flow.Subscribe(&busA, func(e hit) { damage += e.Damage })

	app := startLoop(t, 50, func(dt time.Duration) {
		step++
		if step == 2 {
			flow.Publish(&busA, scored{Points: 10})
			log = append(log, "after-publish")
		}
		if step == 3 {
			flow.Enqueue(&busA, hit{Damage: 5})
			flow.Enqueue(&busA, hit{Damage: 7})
			if damage != 0 {
				t.Errorf("Enqueue delivered before Flush: damage=%d", damage)
			}
			busA.Flush()
		}
	})

	app.Advance(40 * time.Millisecond)
	want := []string{"sub1", "sub2", "sub3", "after-publish"}
	if !slices.Equal(log, want) {
		t.Fatalf("step 2 log = %v, want %v", log, want)
	}
	if busBHits != 0 {
		t.Fatalf("busB received %d events from busA", busBHits)
	}

	app.Advance(20 * time.Millisecond)
	if damage != 12 {
		t.Fatalf("after step 3 Flush: damage=%d, want 12", damage)
	}
}

func TestBusSafeUnsubscribeSubscribeOnceAndFlush(t *testing.T) {
	type ping struct{ N int }

	var bus flow.Bus
	var order []string
	var unsubSelf, unsubNext func()

	unsubSelf = flow.Subscribe(&bus, func(p ping) {
		order = append(order, "1")
		unsubSelf()
		unsubNext()
		// Subscribed mid-dispatch: should not run for the current event.
		flow.Subscribe(&bus, func(p ping) {
			order = append(order, "late")
		})
	})
	unsubNext = flow.Subscribe(&bus, func(p ping) {
		order = append(order, "2-removed")
	})
	flow.Subscribe(&bus, func(p ping) {
		order = append(order, "3")
	})

	flow.Publish(&bus, ping{N: 1})
	if !slices.Equal(order, []string{"1", "3"}) {
		t.Fatalf("first publish order = %v, want [1 3]", order)
	}

	order = nil
	flow.Publish(&bus, ping{N: 2})
	if !slices.Equal(order, []string{"3", "late"}) {
		t.Fatalf("second publish order = %v, want [3 late]", order)
	}

	// SubscribeOnce fires once even when re-entrantly publishing the same type.
	var onceBus flow.Bus
	onceCount := 0
	flow.SubscribeOnce(&onceBus, func(p ping) {
		onceCount++
		flow.Publish(&onceBus, ping{N: p.N + 1})
	})
	flow.Publish(&onceBus, ping{N: 1})
	if onceCount != 1 {
		t.Fatalf("SubscribeOnce fired %d times, want 1", onceCount)
	}

	// Flush drains cascading Enqueue calls in strict FIFO order.
	var qBus flow.Bus
	var seq []int
	flow.Subscribe(&qBus, func(p ping) {
		seq = append(seq, p.N)
		if p.N == 1 {
			flow.Enqueue(&qBus, ping{N: 3})
		}
	})
	flow.Enqueue(&qBus, ping{N: 1})
	flow.Enqueue(&qBus, ping{N: 2})
	qBus.Flush()
	if !slices.Equal(seq, []int{1, 2, 3}) {
		t.Fatalf("Flush FIFO order = %v, want [1 2 3]", seq)
	}

	// Clear mid-dispatch cancels remaining subscribers for the event in flight.
	var clearBus flow.Bus
	secondRan := false
	flow.Subscribe(&clearBus, func(p ping) { clearBus.Clear() })
	flow.Subscribe(&clearBus, func(p ping) { secondRan = true })
	flow.Publish(&clearBus, ping{N: 1})
	if secondRan {
		t.Fatal("second subscriber ran after Bus.Clear() in first subscriber")
	}
}
