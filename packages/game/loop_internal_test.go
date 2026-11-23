package game

import (
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestAFrameBeforeTheResumptionTakesNoTimeOff(t *testing.T) {
	var r *loopRun
	updates := 0
	l := &Loop{Update: func(time.Duration) { updates++ }}
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		r = &loopRun{loop: l, cx: cx}
		r.resume()
		return core.View{}
	})
	r.frame(r.last.Add(-5 * time.Millisecond))
	if r.acc < 0 {
		t.Fatalf("acc %v after a frame stamped before the resumption, want at least 0", r.acc)
	}
	r.frame(r.last.Add(l.step()))
	if updates != 1 {
		t.Fatalf("%d updates after a whole step, want 1", updates)
	}
}

// recordImmersive replaces setImmersive for the test with one that records each request, which NSGo's nsgotest does not (nsgotest.StatusBarConfig has no Immersive field, nsgo-dev/nsgo#2255).
func recordImmersive(t *testing.T) *[]bool {
	t.Helper()
	var asked []bool
	orig := setImmersive
	setImmersive = func(cx *core.Scope, on bool) {
		asked = append(asked, on)
		orig(cx, on)
	}
	t.Cleanup(func() { setImmersive = orig })
	return &asked
}

func expectImmersive(t *testing.T, asked *[]bool, want ...bool) {
	t.Helper()
	if !slices.Equal(*asked, want) {
		t.Fatalf("immersive requests %v, want %v", *asked, want)
	}
}

func TestImmersiveHidesTheBarsWhilePlayingAndShowsThemInMenus(t *testing.T) {
	asked := recordImmersive(t)
	l := &Loop{Immersive: true}
	var stop func()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		stop = l.Run(cx)
		return core.View{}
	})
	expectImmersive(t, asked, true)

	app.Advance(time.Second)
	expectImmersive(t, asked, true)

	// A pause menu brings the bars back on the next frame, and leaving it hides them again.
	app.Post(func() { l.SetPaused(true) })
	app.Settle()
	app.Advance(time.Second / 60)
	expectImmersive(t, asked, true, false)
	app.Advance(time.Second)
	expectImmersive(t, asked, true, false)
	app.Post(func() { l.SetPaused(false) })
	app.Settle()
	app.Advance(time.Second / 60)
	expectImmersive(t, asked, true, false, true)

	// The background leaves the bars as they are, and coming back asks for nothing new.
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	app.Advance(time.Second)
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	app.Advance(time.Second / 60)
	expectImmersive(t, asked, true, false, true)

	// Stopping shows the bars once, and later changes ask for nothing.
	app.Post(stop)
	app.Settle()
	expectImmersive(t, asked, true, false, true, false)
	app.Post(func() { l.SetPaused(true); l.SetPaused(false); stop() })
	app.Settle()
	app.Advance(time.Second)
	expectImmersive(t, asked, true, false, true, false)
}

func TestImmersiveFollowsAPauseFromUpdateInTheSameFrame(t *testing.T) {
	asked := recordImmersive(t)
	updates := 0
	var l *Loop
	l = &Loop{Immersive: true, Update: func(time.Duration) {
		updates++
		if updates == 3 {
			l.SetPaused(true)
		}
	}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})
	app.FastForward(5 * l.step())
	if updates != 3 {
		t.Fatalf("%d updates, want 3", updates)
	}
	expectImmersive(t, asked, true, false)
}

func TestImmersiveTogglesWhileRunning(t *testing.T) {
	asked := recordImmersive(t)
	l := &Loop{}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})
	app.Advance(time.Second)
	expectImmersive(t, asked)

	app.Post(func() { l.Immersive = true })
	app.Settle()
	app.Advance(time.Second / 60)
	expectImmersive(t, asked, true)

	app.Post(func() { l.Immersive = false })
	app.Settle()
	app.Advance(time.Second / 60)
	expectImmersive(t, asked, true, false)
}

func TestImmersiveWaitsForTheForegroundAndAPausedStart(t *testing.T) {
	asked := recordImmersive(t)
	l := &Loop{Immersive: true, Paused: true}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	}, nsgotest.Environment(core.Environment{Lifecycle: core.LifecycleBackground}))
	app.Post(func() { l.SetPaused(false) })
	app.Settle()
	app.Advance(time.Second)
	expectImmersive(t, asked)

	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	expectImmersive(t, asked, true)
}

func TestDisposingTheScopeShowsTheBars(t *testing.T) {
	asked := recordImmersive(t)
	l := &Loop{Immersive: true}
	var shown *core.Signal[bool]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shown = core.NewSignal(cx, true)
		return core.Show{When: shown.Get, Then: func(cx *core.Scope) core.Node {
			l.Run(cx)
			return core.View{}
		}}
	})
	expectImmersive(t, asked, true)
	app.Post(func() { shown.Set(false) })
	app.Settle()
	app.Advance(time.Second)
	expectImmersive(t, asked, true, false)
}

func TestStopFromUpdateShowsTheBars(t *testing.T) {
	asked := recordImmersive(t)
	var stop func()
	l := &Loop{Immersive: true, Update: func(time.Duration) { stop() }}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		stop = l.Run(cx)
		return core.View{}
	})
	app.FastForward(20 * time.Millisecond)
	app.Advance(time.Second)
	expectImmersive(t, asked, true, false)
}

func TestRunsInAWindowShareTheBars(t *testing.T) {
	asked := recordImmersive(t)
	a := &Loop{Immersive: true}
	b := &Loop{Immersive: true}
	var showA *core.Signal[bool]
	var stopB func()
	var root *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		root = cx
		showA = core.NewSignal(cx, true)
		return core.Show{When: showA.Get, Then: func(cx *core.Scope) core.Node {
			a.Run(cx)
			return core.View{}
		}}
	})
	expectImmersive(t, asked, true)

	// A second loop starting while the first plays asks for nothing new, as a scene entering under a transition would.
	app.Post(func() { stopB = b.Run(root) })
	app.Settle()
	app.Advance(time.Second / 60)
	expectImmersive(t, asked, true)

	// Disposing the first loop's scope, or pausing it as a pushed scene's Pause would, leaves the bars hidden while the second plays.
	app.Post(func() { a.SetPaused(true) })
	app.Settle()
	app.Advance(time.Second / 60)
	app.Post(func() { showA.Set(false) })
	app.Settle()
	app.Advance(time.Second)
	expectImmersive(t, asked, true)

	// The last run that wants them hidden brings them back.
	app.Post(stopB)
	app.Settle()
	expectImmersive(t, asked, true, false)
}
