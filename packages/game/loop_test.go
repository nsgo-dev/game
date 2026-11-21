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
