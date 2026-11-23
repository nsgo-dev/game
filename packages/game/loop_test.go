package game_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// counted is a loop that counts its updates and draws.
type counted struct {
	game.Loop
	updates, draws int
}

func count(l game.Loop) *counted {
	c := &counted{Loop: l}
	c.Update = func(time.Duration) { c.updates++ }
	c.Draw = func(float64) { c.draws++ }
	return c
}

// start runs l in an app's root view.
func start(t *testing.T, l *game.Loop) *nsgotest.App {
	t.Helper()
	return nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})
}

func (c *counted) expect(t *testing.T, updates, draws int) {
	t.Helper()
	if c.updates != updates || c.draws != draws {
		t.Fatalf("%d updates and %d draws, want %d and %d", c.updates, c.draws, updates, draws)
	}
}

func TestASecondRunsRateUpdatesAtTheFixedStep(t *testing.T) {
	for _, rate := range []int{60, 120, 0} {
		t.Run(fmt.Sprint(rate), func(t *testing.T) {
			want := rate
			if want == 0 {
				want = 60
			}
			var updates, draws int
			l := &game.Loop{
				Rate: rate,
				Update: func(dt time.Duration) {
					updates++
					if dt != time.Second/time.Duration(want) {
						t.Errorf("update %d: dt %v, want %v", updates, dt, time.Second/time.Duration(want))
					}
				},
				Draw: func(alpha float64) {
					draws++
					if alpha < 0 || alpha >= 1 {
						t.Errorf("draw %d: alpha %v, want it in [0, 1)", draws, alpha)
					}
				},
			}
			app := start(t, l)
			if updates != 0 || draws != 0 {
				t.Fatalf("%d updates and %d draws before a frame", updates, draws)
			}
			// nsgotest runs a display frame every 16.667 ms, 60 a second.
			app.Advance(time.Second)
			if updates != want || draws != 60 {
				t.Fatalf("after 1 s: %d updates and %d draws, want %d and 60", updates, draws, want)
			}
			app.Advance(9 * time.Second)
			if updates != 10*want || draws != 600 {
				t.Fatalf("after 10 s: %d updates and %d draws, want %d and 600", updates, draws, 10*want)
			}
		})
	}
}

func TestDrawGetsHowFarTheFrameFallsIntoTheNextStep(t *testing.T) {
	var alphas []float64
	l := &game.Loop{Rate: 40, Draw: func(alpha float64) { alphas = append(alphas, alpha) }}
	app := start(t, l)
	app.Advance(6 * 16667 * time.Microsecond)
	// 25 ms steps on 16.667 ms frames: two thirds of a step left, then a third, then none.
	want := []float64{2. / 3, 1. / 3, 0, 2. / 3, 1. / 3, 0}
	if len(alphas) != len(want) {
		t.Fatalf("alphas %v, want %v", alphas, want)
	}
	for i := range want {
		if math.Abs(alphas[i]-want[i]) > 1e-3 {
			t.Fatalf("alphas %v, want %v", alphas, want)
		}
	}
}

func TestAFrameAddsTheTimeSinceTheLastFrameUnsmoothed(t *testing.T) {
	var updates int
	var alphas []float64
	l := &game.Loop{
		Rate:   100,
		Update: func(time.Duration) { updates++ },
		Draw:   func(alpha float64) { alphas = append(alphas, alpha) },
	}
	app := start(t, l)
	// Uneven frames, as a display whose rate changes delivers them: each adds exactly its own length to the 10 ms steps.
	for _, frame := range []struct {
		length  time.Duration
		updates int
		alpha   float64
	}{
		{10 * time.Millisecond, 1, 0},
		{4 * time.Millisecond, 1, 0.4},
		{13 * time.Millisecond, 2, 0.7},
		{16 * time.Millisecond, 4, 0.3},
		{7 * time.Millisecond, 5, 0},
	} {
		app.Advance(frame.length)
		got := alphas[len(alphas)-1]
		if updates != frame.updates || math.Abs(got-frame.alpha) > 1e-9 {
			t.Fatalf("after a %v frame: %d updates and alpha %v, want %d and %v", frame.length, updates, got, frame.updates, frame.alpha)
		}
	}
}

func TestAFrameAddsTheGapBetweenTheDisplaysStamps(t *testing.T) {
	var updates int
	var alphas []float64
	l := &game.Loop{
		Rate:   100,
		Update: func(time.Duration) { updates++ },
		Draw:   func(alpha float64) { alphas = append(alphas, alpha) },
	}
	var cx *core.Scope
	app := nsgotest.Start(t, func(s *core.Scope) core.Node {
		cx = s
		l.Run(s)
		return core.View{}
	})
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	app.Advance(time.Second)
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	resumed := cx.Now()
	// Frames stamped by the display rather than by the graph clock, which stays at the resumption throughout.
	for _, frame := range []struct {
		name    string
		at      time.Duration
		updates int
		alpha   float64
	}{
		{"stamped before the resumption", -5 * time.Millisecond, 0, 0},
		{"stamped a frame ahead of now", 25 * time.Millisecond, 2, 0.5},
		{"7 ms of display time later", 32 * time.Millisecond, 3, 0.2},
		{"13 ms of display time later", 45 * time.Millisecond, 4, 0.5},
	} {
		at := resumed.Add(frame.at)
		app.Post(func() { cx.RunFrameAt(at) })
		app.Settle()
		if !cx.Now().Equal(resumed) {
			t.Fatalf("the graph clock moved to %v", cx.Now().Sub(resumed))
		}
		got := alphas[len(alphas)-1]
		if updates != frame.updates || math.Abs(got-frame.alpha) > 1e-9 {
			t.Fatalf("frame %s: %d updates and alpha %v, want %d and %v", frame.name, updates, got, frame.updates, frame.alpha)
		}
	}
}

func TestALongFrameRunsAtMostMaxStepsAndDropsTheRest(t *testing.T) {
	for _, tc := range []struct {
		name     string
		loop     game.Loop
		rate, at int
	}{
		{"default", game.Loop{}, 60, 6},
		{"default at 120", game.Loop{Rate: 120}, 120, 12},
		{"set", game.Loop{MaxSteps: 3}, 60, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := count(tc.loop)
			app := start(t, &c.Loop)
			app.Advance(time.Second)
			c.expect(t, tc.rate, 60)
			// One frame 500 ms after the last, as after a hitch.
			app.FastForward(500 * time.Millisecond)
			c.expect(t, tc.rate+tc.at, 61)
			// The steps past the cap are gone: the next second runs a second's updates.
			app.Advance(time.Second)
			c.expect(t, 2*tc.rate+tc.at, 121)
		})
	}
}

func TestTheBackgroundPausesTheLoopWithoutACatchUp(t *testing.T) {
	c := count(game.Loop{})
	app := start(t, &c.Loop)
	app.Advance(time.Second)
	c.expect(t, 60, 60)

	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	app.Advance(time.Second)
	c.expect(t, 60, 60)

	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	app.Advance(time.Second)
	c.expect(t, 120, 120)

	// A window that shows without the focus keeps playing, on time.
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleInactive})
	app.Advance(time.Second)
	c.expect(t, 180, 180)
}

func TestStopEndsTheLoop(t *testing.T) {
	c := count(game.Loop{})
	var stop func()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		stop = c.Run(cx)
		return core.View{}
	})
	app.Advance(time.Second)
	c.expect(t, 60, 60)
	stop()
	app.Advance(time.Second)
	c.expect(t, 60, 60)
	stop()
	// The background and back again do not restart a stopped loop.
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	app.Advance(time.Second)
	c.expect(t, 60, 60)
}

func TestStopFromUpdateEndsTheFrame(t *testing.T) {
	var stop func()
	updates, draws := 0, 0
	l := &game.Loop{
		Update: func(time.Duration) {
			updates++
			if updates == 2 {
				stop()
			}
		},
		Draw: func(float64) { draws++ },
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		stop = l.Run(cx)
		return core.View{}
	})
	// A frame that holds three steps.
	app.FastForward(50 * time.Millisecond)
	app.Advance(time.Second)
	if updates != 2 || draws != 0 {
		t.Fatalf("%d updates and %d draws, want 2 and 0", updates, draws)
	}
}

func TestDisposingTheScopeEndsTheLoop(t *testing.T) {
	c := count(game.Loop{})
	var shown *core.Signal[bool]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shown = core.NewSignal(cx, true)
		return core.Show{When: shown.Get, Then: func(cx *core.Scope) core.Node {
			c.Run(cx)
			return core.View{}
		}}
	})
	app.Advance(time.Second)
	c.expect(t, 60, 60)
	app.Post(func() { shown.Set(false) })
	app.Settle()
	app.Advance(time.Second)
	c.expect(t, 60, 60)
}

func TestLoopKeepsDisplayAwakeInForegroundAndSleepsWhenPaused(t *testing.T) {
	var stop func()
	l := &game.Loop{}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		stop = l.Run(cx)
		return core.View{}
	})
	if len(app.Awake) != 1 || !app.Awake[0].Display {
		t.Fatalf("active awake holds = %+v, want 1 display hold", app.Awake)
	}
	firstID := app.Awake[0].ID

	// Moving to background releases the hold so the display may sleep.
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	if len(app.Awake) != 0 {
		t.Fatalf("background awake holds = %+v, want none", app.Awake)
	}
	if len(app.AwakeReleases) != 1 || app.AwakeReleases[0] != firstID {
		t.Fatalf("background releases = %v, want [%d]", app.AwakeReleases, firstID)
	}

	// Returning to foreground acquires a fresh display hold.
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	if len(app.Awake) != 1 || !app.Awake[0].Display {
		t.Fatalf("resumed awake holds = %+v, want 1 display hold", app.Awake)
	}

	// Stopping the loop releases the hold and ignores later lifecycle changes.
	app.Post(stop)
	app.Settle()
	if len(app.Awake) != 0 {
		t.Fatalf("stopped awake holds = %+v, want none", app.Awake)
	}
	app.Post(stop)
	app.Settle()
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	if len(app.Awake) != 0 {
		t.Fatalf("post-stop awake holds = %+v, want none", app.Awake)
	}
}

func TestLoopStartingInBackgroundDoesNotKeepAwakeUntilForeground(t *testing.T) {
	l := &game.Loop{}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	}, nsgotest.Environment(core.Environment{Lifecycle: core.LifecycleBackground}))
	if len(app.Awake) != 0 || len(app.AwakeHolds) != 0 {
		t.Fatalf("background start holds = %+v / %+v, want none", app.Awake, app.AwakeHolds)
	}
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	if len(app.Awake) != 1 || !app.Awake[0].Display {
		t.Fatalf("foreground holds = %+v, want 1 display hold", app.Awake)
	}
}

func TestDisposingScopeOrStoppingInUpdateReleasesKeepAwake(t *testing.T) {
	t.Run("dispose", func(t *testing.T) {
		var shown *core.Signal[bool]
		l := &game.Loop{}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			shown = core.NewSignal(cx, true)
			return core.Show{When: shown.Get, Then: func(cx *core.Scope) core.Node {
				l.Run(cx)
				return core.View{}
			}}
		})
		if len(app.Awake) != 1 {
			t.Fatalf("active holds = %+v, want 1", app.Awake)
		}
		app.Post(func() { shown.Set(false) })
		app.Settle()
		if len(app.Awake) != 0 {
			t.Fatalf("holds after scope dispose = %+v, want none", app.Awake)
		}
	})
	t.Run("stop_in_update", func(t *testing.T) {
		var stop func()
		l := &game.Loop{Update: func(time.Duration) { stop() }}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			stop = l.Run(cx)
			return core.View{}
		})
		if len(app.Awake) != 1 {
			t.Fatalf("active holds = %+v, want 1", app.Awake)
		}
		app.FastForward(20 * time.Millisecond)
		if len(app.Awake) != 0 {
			t.Fatalf("holds after stop in Update = %+v, want none", app.Awake)
		}
	})
}

func TestPausedSkipsUpdatesWithoutACatchUp(t *testing.T) {
	c := count(game.Loop{Paused: true})
	app := start(t, &c.Loop)

	app.Advance(time.Second)
	c.expect(t, 0, 60)

	app.Post(func() { c.SetPaused(false) })
	app.Settle()
	app.Advance(time.Second)
	c.expect(t, 60, 120)

	app.Post(func() { c.SetPaused(true) })
	app.Settle()
	app.FastForward(500 * time.Millisecond)
	app.Advance(time.Second)
	c.expect(t, 60, 181)

	app.Post(func() { c.SetPaused(false) })
	app.Settle()
	app.Advance(time.Second)
	c.expect(t, 120, 241)
}

func TestPauseFromUpdateEndsTheFramesUpdates(t *testing.T) {
	updates, draws := 0, 0
	var l *game.Loop
	l = &game.Loop{
		Update: func(time.Duration) {
			updates++
			if updates == 2 {
				l.SetPaused(true)
			}
		},
		Draw: func(alpha float64) {
			draws++
			if alpha < 0 || alpha >= 1 {
				t.Errorf("draw %d: alpha %v, want it in [0, 1)", draws, alpha)
			}
		},
	}
	app := start(t, l)
	// A frame that holds three steps stops updating after the second pauses the loop.
	app.FastForward(50 * time.Millisecond)
	if updates != 2 || draws != 1 {
		t.Fatalf("%d updates and %d draws, want 2 and 1", updates, draws)
	}
	app.Advance(time.Second)
	if updates != 2 || draws != 61 {
		t.Fatalf("after 1 s paused: %d updates and %d draws, want 2 and 61", updates, draws)
	}
}

func TestTimeScaleScalesSimulationWithTheFixedStep(t *testing.T) {
	for _, tc := range []struct {
		name  string
		scale float64
		want  int
	}{
		{"half", 0.5, 30},
		{"double", 2.0, 120},
		{"zero", 0, 60},
		{"negative", -1, 60},
		{"nan", math.NaN(), 60},
		{"inf", math.Inf(1), 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var updates, draws int
			wantStep := time.Second / 60
			l := &game.Loop{
				Rate:      60,
				TimeScale: tc.scale,
				Update: func(dt time.Duration) {
					updates++
					if dt != wantStep {
						t.Errorf("update %d: dt %v, want %v", updates, dt, wantStep)
					}
				},
				Draw: func(alpha float64) {
					draws++
					if alpha < 0 || alpha >= 1 {
						t.Errorf("draw %d: alpha %v, want it in [0, 1)", draws, alpha)
					}
				},
			}
			app := start(t, l)
			app.Advance(time.Second)
			if updates != tc.want || draws != 60 {
				t.Fatalf("after 1 s: %d updates and %d draws, want %d and 60", updates, draws, tc.want)
			}
			app.Advance(9 * time.Second)
			if updates != 10*tc.want || draws != 600 {
				t.Fatalf("after 10 s: %d updates and %d draws, want %d and 600", updates, draws, 10*tc.want)
			}
		})
	}
}

func TestSetTimeScaleChangesSpeedWhileRunning(t *testing.T) {
	c := count(game.Loop{Rate: 60})
	app := start(t, &c.Loop)

	app.Advance(time.Second)
	c.expect(t, 60, 60)

	app.Post(func() { c.SetTimeScale(0.5) })
	app.Settle()
	app.Advance(time.Second)
	c.expect(t, 90, 120)

	app.Post(func() { c.SetTimeScale(2.0) })
	app.Settle()
	app.Advance(time.Second)
	c.expect(t, 210, 180)
}

func TestStepRunsFixedUpdatesWhilePaused(t *testing.T) {
	wantStep := time.Second / 60
	var updates, draws int
	var lastAlpha float64
	l := &game.Loop{
		Rate:     60,
		Paused:   true,
		OnDemand: true,
		Update: func(dt time.Duration) {
			updates++
			if dt != wantStep {
				t.Errorf("update %d: dt %v, want %v", updates, dt, wantStep)
			}
		},
		Draw: func(alpha float64) {
			draws++
			lastAlpha = alpha
		},
	}
	app := start(t, l)

	app.Advance(time.Second)
	if updates != 0 || draws != 0 {
		t.Fatalf("while idle: %d updates and %d draws, want 0 and 0", updates, draws)
	}

	app.Post(func() { l.Step(1) })
	app.Settle()
	app.Advance(17 * time.Millisecond)
	if updates != 1 || draws != 1 || lastAlpha != 0 {
		t.Fatalf("after Step(1): %d updates, %d draws, alpha %v; want 1, 1, 0", updates, draws, lastAlpha)
	}

	app.Post(func() { l.StepOnce() })
	app.Settle()
	app.Advance(17 * time.Millisecond)
	if updates != 2 || draws != 2 {
		t.Fatalf("after StepOnce(): %d updates and %d draws, want 2 and 2", updates, draws)
	}

	app.Post(func() { l.Step(3) })
	app.Settle()
	app.Advance(17 * time.Millisecond)
	if updates != 5 || draws != 3 {
		t.Fatalf("after Step(3): %d updates and %d draws, want 5 and 3", updates, draws)
	}

	app.Post(func() {
		l.Step(0)
		l.Step(-2)
	})
	app.Settle()
	app.Advance(time.Second)
	if updates != 5 || draws != 3 {
		t.Fatalf("after non-positive Step and 1 s idle: %d updates and %d draws, want 5 and 3", updates, draws)
	}
}

func TestStopFromUpdateEndsAStep(t *testing.T) {
	var stop func()
	updates, draws := 0, 0
	l := &game.Loop{
		Paused: true,
		Update: func(time.Duration) {
			updates++
			if updates == 2 {
				stop()
			}
		},
		Draw: func(float64) { draws++ },
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		stop = l.Run(cx)
		return core.View{}
	})
	app.Post(func() { l.Step(3) })
	app.Settle()
	app.Advance(17 * time.Millisecond)
	if updates != 2 || draws != 0 {
		t.Fatalf("%d updates and %d draws, want 2 and 0", updates, draws)
	}
}

func TestDrawRateCapsDrawsWhileUpdatesKeepToRate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		rate      int
		drawRate  int
		wantUpd   int
		wantDraws int
	}{
		{"30 on 60 Hz", 60, 30, 60, 30},
		{"20 at 120 Hz updates", 120, 20, 120, 20},
		{"above display rate", 60, 120, 60, 60},
		{"uncapped zero", 60, 0, 60, 60},
		{"uncapped negative", 60, -10, 60, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := count(game.Loop{Rate: tc.rate, DrawRate: tc.drawRate})
			app := start(t, &c.Loop)
			app.Advance(time.Second)
			c.expect(t, tc.wantUpd, tc.wantDraws)
			app.Advance(9 * time.Second)
			c.expect(t, 10*tc.wantUpd, 10*tc.wantDraws)
		})
	}
}

func TestOnDemandDrawsOnlyOnUpdatesOrMarkDirty(t *testing.T) {
	c := count(game.Loop{Paused: true, OnDemand: true})
	app := start(t, &c.Loop)

	app.Advance(time.Second)
	c.expect(t, 0, 0)

	app.Post(func() { c.MarkDirty() })
	app.Settle()
	app.Advance(time.Second)
	c.expect(t, 0, 1)

	app.Post(func() { c.MarkDirty() })
	app.Settle()
	app.Advance(17 * time.Millisecond)
	c.expect(t, 0, 2)

	app.Advance(time.Second)
	c.expect(t, 0, 2)
}

func TestOnDemandSkipsFramesWithoutUpdates(t *testing.T) {
	// 30 updates/s on 60 Hz display frames: half the frames run no update and skip Draw.
	c := count(game.Loop{Rate: 30, OnDemand: true})
	app := start(t, &c.Loop)
	app.Advance(time.Second)
	c.expect(t, 30, 30)
}

func TestOnDemandPreservesDirtyAcrossDrawRateCap(t *testing.T) {
	c := count(game.Loop{Paused: true, OnDemand: true, DrawRate: 30})
	app := start(t, &c.Loop)

	app.Advance(time.Second)
	c.expect(t, 0, 0)

	// First MarkDirty after idle draws on the next frame immediately.
	app.Post(func() { c.MarkDirty() })
	app.Settle()
	app.Advance(17 * time.Millisecond)
	c.expect(t, 0, 1)

	// A second MarkDirty right away waits for the 30 fps draw interval without dropping the dirty flag.
	app.Post(func() { c.MarkDirty() })
	app.Settle()
	app.Advance(17 * time.Millisecond)
	c.expect(t, 0, 1)
	app.Advance(17 * time.Millisecond)
	c.expect(t, 0, 2)
}

func TestMarkDirtyFromDrawRequestsTheNextFrame(t *testing.T) {
	draws := 0
	var l *game.Loop
	l = &game.Loop{
		Paused:   true,
		OnDemand: true,
		Draw: func(float64) {
			draws++
			if draws == 1 {
				l.MarkDirty()
			}
		},
	}
	l.MarkDirty()
	app := start(t, l)
	app.Advance(time.Second)
	if draws != 2 {
		t.Fatalf("%d draws, want 2", draws)
	}
}

func TestFrameTimeReportsEachFramesLengthWithoutTheTimeAway(t *testing.T) {
	var frames []time.Duration
	c := count(game.Loop{})
	c.FrameTime = func(dt time.Duration) { frames = append(frames, dt) }
	app := start(t, &c.Loop)
	app.Advance(time.Second)
	if len(frames) != 60 {
		t.Fatalf("%d frame times after 1 s, want 60", len(frames))
	}
	var total time.Duration
	for i, dt := range frames {
		// nsgotest runs a display frame every 16.667 ms.
		if dt < 16*time.Millisecond || dt > 17*time.Millisecond {
			t.Fatalf("frame %d: %v, want about 16.667 ms", i, dt)
		}
		total += dt
	}
	if total < 999*time.Millisecond || total > time.Second+time.Millisecond {
		t.Fatalf("frame times add up to %v, want about 1 s", total)
	}

	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	app.Advance(5 * time.Second)
	if len(frames) != 60 {
		t.Fatalf("%d frame times in the background, want none past 60", len(frames))
	}
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	app.Advance(time.Second)
	for i, dt := range frames[60:] {
		if dt > 17*time.Millisecond {
			t.Fatalf("frame %d after the background: %v, want the time away left out", i, dt)
		}
	}
	c.expect(t, 120, 120)
}

func TestStopFromFrameTimeEndsTheFrame(t *testing.T) {
	var stop func()
	c := count(game.Loop{})
	frames := 0
	c.FrameTime = func(time.Duration) {
		frames++
		if frames == 3 {
			stop()
		}
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		stop = c.Run(cx)
		return core.View{}
	})
	app.Advance(time.Second)
	if frames != 3 {
		t.Fatalf("%d frame times, want 3", frames)
	}
	c.expect(t, 2, 2)
}
