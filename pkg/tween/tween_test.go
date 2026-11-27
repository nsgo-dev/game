package tween_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/game/pkg/tween"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func startLoop(t *testing.T, l *game.Loop) *nsgotest.App {
	t.Helper()
	return nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestTweenTypedConstructorsAndExactEndpoints(t *testing.T) {
	const ms = time.Millisecond

	// Float32 reaches exact target on the exact step.
	var f32 float32
	tw32 := tween.Float32(10, 50, 200*ms, core.Linear, func(v float32) { f32 = v })
	for i := 1; i <= 10; i++ {
		done := tw32.Update(20 * ms)
		want := float32(10 + 4*i)
		if f32 != want {
			t.Fatalf("step %d: float32 = %g, want %g", i, f32, want)
		}
		if done != (i == 10) {
			t.Fatalf("step %d: done = %v, want %v", i, done, i == 10)
		}
	}
	if !tw32.Done() {
		t.Fatal("float32 tween should be done")
	}

	// Float64 with zero core.Curve{} defaults to core.Linear.
	var f64 float64
	tw64 := tween.Float64(-5.5, 14.5, 100*ms, core.Curve{}, func(v float64) { f64 = v })
	tw64.Update(25 * ms)
	if !near(f64, -0.5, 1e-12) {
		t.Fatalf("float64 at 25%% = %g, want -0.5", f64)
	}
	if !tw64.Update(75*ms) || f64 != 14.5 {
		t.Fatalf("float64 end = %g (done=%v), want 14.5 (done=true)", f64, tw64.Done())
	}

	// Point interpolates both axes and hits exact endpoint.
	var pt core.Point
	fromPt := core.Point{X: 10, Y: -20}
	toPt := core.Point{X: 110, Y: 80}
	twPt := tween.Point(fromPt, toPt, 100*ms, core.Linear, func(p core.Point) { pt = p })
	twPt.Update(50 * ms)
	if pt != (core.Point{X: 60, Y: 30}) {
		t.Fatalf("point midpoint = %+v, want {60 30}", pt)
	}
	twPt.Update(50 * ms)
	if pt != toPt {
		t.Fatalf("point end = %+v, want %+v", pt, toPt)
	}

	// Color interpolates channels and avoids dark fringe from transparent.
	var col core.Color
	red := core.RGB(0xff0000)
	blue := core.RGB(0x0000ff)
	twCol := tween.Color(red, blue, 100*ms, core.Linear, func(c core.Color) { col = c })
	twCol.Seek(0)
	if col != red {
		t.Fatalf("color at 0 = %#08x, want %#08x", uint32(col), uint32(red))
	}
	twCol.Seek(50 * ms)
	if want := core.Color(0x800080ff); col != want {
		t.Fatalf("color midpoint = %#08x, want %#08x", uint32(col), uint32(want))
	}
	twCol.Seek(100 * ms)
	if col != blue {
		t.Fatalf("color end = %#08x, want %#08x", uint32(col), uint32(blue))
	}

	// Transparent to red fades alpha cleanly without fading through black.
	twFade := tween.Color(core.Transparent, red, 100*ms, core.Linear, func(c core.Color) { col = c })
	twFade.Seek(0)
	if col != core.Transparent {
		t.Fatalf("fade start = %#08x, want Transparent", uint32(col))
	}
	twFade.Seek(50 * ms)
	if want := core.RGBA(0xff0000, 0.5); col != want {
		t.Fatalf("fade midpoint = %#08x, want %#08x", uint32(col), uint32(want))
	}
	twFade.Seek(100 * ms)
	if col != red {
		t.Fatalf("fade end = %#08x, want %#08x", uint32(col), uint32(red))
	}

	// Overshoot curve on Color clamps channels cleanly into [0, 255].
	overshoot := core.Curve{X1: 0.34, Y1: 1.56, X2: 0.64, Y2: 1}
	twOver := tween.Color(core.RGB(0x000000), core.RGB(0xffffff), 100*ms, overshoot, func(c core.Color) { col = c })
	twOver.Seek(50 * ms)
	if col != core.RGB(0xffffff) {
		t.Fatalf("overshoot color midpoint = %#08x, want clamped white %#08x", uint32(col), uint32(core.RGB(0xffffff)))
	}
	twOver.Seek(100 * ms)
	if col != core.RGB(0xffffff) {
		t.Fatalf("overshoot color end = %#08x, want %#08x", uint32(col), uint32(core.RGB(0xffffff)))
	}

	// Generic New[T] with custom struct.
	type vec3 struct{ x, y, z float64 }
	var v3 vec3
	twV3 := tween.New(
		vec3{1, 2, 3},
		vec3{5, 10, 15},
		100*ms,
		core.Linear,
		func(a, b vec3, u float64) vec3 {
			return vec3{a.x + (b.x-a.x)*u, a.y + (b.y-a.y)*u, a.z + (b.z-a.z)*u}
		},
		func(v vec3) { v3 = v },
	)
	twV3.Seek(25 * ms)
	if v3 != (vec3{2, 4, 6}) {
		t.Fatalf("generic vec3 at 25%% = %+v, want {2 4 6}", v3)
	}
	twV3.Seek(100 * ms)
	if v3 != (vec3{5, 10, 15}) {
		t.Fatalf("generic vec3 end = %+v, want {5 10 15}", v3)
	}

	// Nil set and nil lerp do not panic.
	twNil := tween.New(1, 2, 100*ms, core.Linear, nil, nil)
	twNil.Update(50 * ms)
	twNil.Update(50 * ms)
	if !twNil.Done() {
		t.Fatal("nil-callback tween should finish")
	}
}

func TestTweenCurvesAndDeterminism(t *testing.T) {
	curves := []struct {
		name  string
		curve core.Curve
	}{
		{"linear", core.Linear},
		{"ease", core.Ease},
		{"easeIn", core.EaseIn},
		{"easeOut", core.EaseOut},
		{"easeInOut", core.EaseInOut},
		{"overshoot", core.Curve{X1: 0.34, Y1: 1.56, X2: 0.64, Y2: 1}},
	}

	const steps = 20
	const step = 10 * time.Millisecond
	const dur = steps * step

	for _, tc := range curves {
		t.Run(tc.name, func(t *testing.T) {
			run := func() []float64 {
				var got []float64
				tw := tween.Float64(100, 300, dur, tc.curve, func(v float64) {
					got = append(got, v)
				})
				for range steps {
					tw.Update(step)
				}
				if !tw.Done() {
					t.Fatal("expected done on exact final step")
				}
				return got
			}

			a := run()
			b := run()
			if !slices.Equal(a, b) {
				t.Fatalf("non-deterministic curve %s: %v != %v", tc.name, a, b)
			}
			if len(a) != steps {
				t.Fatalf("len = %d, want %d", len(a), steps)
			}
			for i := 1; i < steps; i++ {
				want := 100 + 200*tc.curve.At(float64(i)/float64(steps))
				if !near(a[i-1], want, 1e-12) {
					t.Fatalf("step %d: got %g, want %g", i, a[i-1], want)
				}
			}
			if a[steps-1] != 300 {
				t.Fatalf("final value = %g, want exact 300", a[steps-1])
			}
		})
	}
}

func TestTweenDelayRepeatYoyoAndOnComplete(t *testing.T) {
	const ms = time.Millisecond

	// Delay holds off writes until elapsed reaches delay, then runs full duration.
	var val float64 = -1
	writes := 0
	completed := 0
	tw := tween.Float64(0, 100, 100*ms, core.Linear, func(v float64) {
		val = v
		writes++
	}).Delay(60 * ms).OnComplete(func() {
		completed++
	})
	if tw.Duration() != 160*ms {
		t.Fatalf("Duration = %v, want 160ms", tw.Duration())
	}
	tw.Update(40 * ms)
	if writes != 0 || val != -1 {
		t.Fatalf("wrote during delay: writes=%d val=%g", writes, val)
	}
	tw.Update(20 * ms) // elapsed == 60ms (start of active window)
	if writes != 1 || val != 0 {
		t.Fatalf("at delay end: writes=%d val=%g, want 1 and 0", writes, val)
	}
	tw.Update(50 * ms) // elapsed == 110ms (50%)
	if val != 50 {
		t.Fatalf("at 110ms: val=%g, want 50", val)
	}
	if !tw.Update(50 * ms) {
		t.Fatal("expected done at 160ms")
	}
	if val != 100 || completed != 1 {
		t.Fatalf("at 160ms: val=%g completed=%d, want 100 and 1", val, completed)
	}
	// Extra updates after done are no-ops.
	tw.Update(50 * ms)
	if completed != 1 {
		t.Fatalf("OnComplete fired %d times, want 1", completed)
	}

	// Repeat(1, true) -> 2 passes: forward then backward, ending at exact from.
	yoyoVal := -1.0
	yoyoDone := 0
	yoyo := tween.Float64(10, 110, 100*ms, core.EaseIn, func(v float64) {
		yoyoVal = v
	}).Repeat(1, true).OnComplete(func() {
		yoyoDone++
	})
	if yoyo.Duration() != 200*ms {
		t.Fatalf("yoyo Duration = %v, want 200ms", yoyo.Duration())
	}
	yoyo.Update(100 * ms)
	if yoyoVal != 110 || yoyo.Done() {
		t.Fatalf("yoyo turnaround: val=%g done=%v, want 110 false", yoyoVal, yoyo.Done())
	}
	yoyo.Update(50 * ms)
	wantMid := 10 + 100*core.EaseIn.At(0.5)
	if !near(yoyoVal, wantMid, 1e-12) {
		t.Fatalf("yoyo return mid: val=%g, want %g", yoyoVal, wantMid)
	}
	if !yoyo.Update(50*ms) || yoyoVal != 10 || yoyoDone != 1 {
		t.Fatalf("yoyo end: val=%g done=%v onComplete=%d, want 10 true 1", yoyoVal, yoyo.Done(), yoyoDone)
	}

	// Repeat(2, true) -> 3 passes: forward, back, forward, ending at exact to.
	yoyo3 := tween.Float64(0, 50, 100*ms, core.Linear, func(v float64) { yoyoVal = v }).Repeat(2, true)
	if yoyo3.Duration() != 300*ms {
		t.Fatalf("yoyo3 Duration = %v, want 300ms", yoyo3.Duration())
	}
	if !yoyo3.Update(300*ms) || yoyoVal != 50 {
		t.Fatalf("yoyo3 end: val=%g done=%v, want 50 true", yoyoVal, yoyo3.Done())
	}

	// Repeat(1, false) -> 2 forward passes, ending at exact to.
	repVal := -1.0
	rep := tween.Float64(0, 100, 100*ms, core.Linear, func(v float64) { repVal = v }).Repeat(1, false)
	rep.Update(100 * ms)
	if repVal != 0 || rep.Done() {
		t.Fatalf("repeat pass 2 start: val=%g done=%v, want 0 false", repVal, rep.Done())
	}
	if !rep.Update(100*ms) || repVal != 100 {
		t.Fatalf("repeat end: val=%g done=%v, want 100 true", repVal, rep.Done())
	}

	// RepeatForever never finishes and alternates with yoyo.
	var infVal float64
	inf := tween.Float64(0, 10, 100*ms, core.Linear, func(v float64) { infVal = v }).Repeat(tween.RepeatForever, true)
	if inf.Duration() != 200*ms {
		t.Fatalf("infinite yoyo cycle Duration = %v, want 200ms", inf.Duration())
	}
	for range 10 {
		if inf.Update(50 * ms) {
			t.Fatal("RepeatForever tween should never finish")
		}
	}
	if infVal != 10 {
		t.Fatalf("infinite yoyo at 500ms: val=%g, want 10", infVal)
	}

	// Zero-duration tween completes on first positive Update or Seek(0), or after Delay.
	var zVal float64
	zDone := 0
	z := tween.Float64(1, 9, 0, core.Linear, func(v float64) { zVal = v }).Delay(40 * ms).OnComplete(func() { zDone++ })
	if z.Update(20 * ms) {
		t.Fatal("delayed zero-duration tween finished before delay")
	}
	if !z.Update(20*ms) || zVal != 9 || zDone != 1 {
		t.Fatalf("delayed zero-duration tween end: val=%g done=%v onComplete=%d", zVal, z.Done(), zDone)
	}

	// Seek does not trigger OnComplete; Reset allows replaying and firing OnComplete again.
	z.Reset()
	z.Seek(40 * ms)
	if !z.Done() || zDone != 1 {
		t.Fatalf("Seek should set Done without firing OnComplete: done=%v onComplete=%d", z.Done(), zDone)
	}
	z.Reset()
	if z.Done() {
		t.Fatal("Reset should clear Done")
	}
	z.Update(40 * ms)
	if zDone != 2 {
		t.Fatalf("OnComplete after Reset+Update = %d, want 2", zDone)
	}
}

func TestGroupStepsMultipleTweensOnGameLoop(t *testing.T) {
	const ms = time.Millisecond
	var g tween.Group

	var a, b, c float32
	g.Add(
		tween.Float32(0, 100, 200*ms, core.Linear, func(v float32) { a = v }).OnComplete(func() {
			// Re-entrantly add a follow-up tween from OnComplete.
			g.Add(tween.Float32(0, 50, 100*ms, core.Linear, func(v float32) { c = v }))
		}),
		tween.Float32(10, 20, 400*ms, core.EaseOut, func(v float32) { b = v }),
	)
	if g.Len() != 2 {
		t.Fatalf("initial Len = %d, want 2", g.Len())
	}

	l := &game.Loop{
		Rate: 50, // exact 20ms steps
		Update: func(dt time.Duration) {
			g.Update(dt)
		},
	}
	app := startLoop(t, l)

	// Advance 200ms (10 steps at 50 Hz): a finishes, spawns c; b is halfway.
	app.Advance(200 * ms)
	if a != 100 {
		t.Fatalf("after 200ms: a = %g, want 100", a)
	}
	if g.Len() != 2 {
		t.Fatalf("after 200ms: Len = %d, want 2 (b and newly added c)", g.Len())
	}

	// Advance another 100ms (5 steps): c finishes at exact 50; only b remains.
	app.Advance(100 * ms)
	if c != 50 {
		t.Fatalf("after 300ms: c = %g, want 50", c)
	}
	if g.Len() != 1 {
		t.Fatalf("after 300ms: Len = %d, want 1 (only b)", g.Len())
	}

	// Advance another 100ms (5 steps): b finishes at exact 20; group is empty.
	app.Advance(100 * ms)
	if b != 20 || g.Len() != 0 {
		t.Fatalf("after 400ms: b=%g Len=%d, want 20 and 0", b, g.Len())
	}

	// Remove and Clear.
	tw1 := tween.Float32(0, 1, 100*ms, core.Linear, nil)
	tw2 := tween.Float32(0, 1, 100*ms, core.Linear, nil)
	g.Add(tw1, tw2)
	g.Remove(tw1)
	if g.Len() != 1 {
		t.Fatalf("after Remove: Len = %d, want 1", g.Len())
	}
	g.Clear()
	if g.Len() != 0 {
		t.Fatalf("after Clear: Len = %d, want 0", g.Len())
	}
}

func TestTimelineSeekSetsEveryTrackAccurately(t *testing.T) {
	const ms = time.Millisecond

	var x, y, z, snap float64
	cueCount := 0

	tl := tween.NewTimeline()
	// Track 1: x goes 0 -> 100 over [100ms, 500ms]
	tl.Add(100*ms, tween.Float64(0, 100, 400*ms, core.Linear, func(v float64) { x = v }))
	// Track 2: x continues 100 -> 300 over [500ms, 900ms] (chained on same target!)
	tl.Add(500*ms, tween.Float64(100, 300, 400*ms, core.Linear, func(v float64) { x = v }))
	// Track 3: y goes 10 -> 50 over [300ms, 700ms] with EaseInOut
	tl.Add(300*ms, tween.Float64(10, 50, 400*ms, core.EaseInOut, func(v float64) { y = v }))
	// Track 4: z has Delay(100ms) and duration 200ms, starting at 600ms -> active [700ms, 900ms]
	tl.Add(600*ms, tween.Float64(5, 25, 200*ms, core.Linear, func(v float64) { z = v }).Delay(100*ms))
	// Track 5: zero-duration snap 0 -> 1 at 400ms
	tl.Add(400*ms, tween.Float64(0, 1, 0, core.Linear, func(v float64) { snap = v }))
	// Cue at 400ms must not fire during Seek.
	tl.Call(400*ms, func() { cueCount++ })

	if tl.Duration() != 900*ms {
		t.Fatalf("Duration = %v, want 900ms", tl.Duration())
	}

	checks := []struct {
		at       time.Duration
		x, y, z  float64
		wantSnap float64
	}{
		{0, 0, 10, 5, 0},
		{50 * ms, 0, 10, 5, 0},
		{100 * ms, 0, 10, 5, 0},
		{300 * ms, 50, 10, 5, 0},
		{400 * ms, 75, 10 + 40*core.EaseInOut.At(0.25), 5, 1},
		{500 * ms, 100, 10 + 40*core.EaseInOut.At(0.5), 5, 1},
		{650 * ms, 175, 10 + 40*core.EaseInOut.At(0.875), 5, 1},
		{700 * ms, 200, 50, 5, 1},
		{800 * ms, 250, 50, 15, 1},
		{900 * ms, 300, 50, 25, 1},
		{1200 * ms, 300, 50, 25, 1}, // clamped to Duration
		// Now seek backward in non-monotonic order!
		{200 * ms, 25, 10, 5, 0},
		{800 * ms, 250, 50, 15, 1},
		{50 * ms, 0, 10, 5, 0},
		{450 * ms, 87.5, 10 + 40*core.EaseInOut.At(0.375), 5, 1},
		{-50 * ms, 0, 10, 5, 0}, // clamped to 0
	}

	for _, tc := range checks {
		tl.Seek(tc.at)
		if !near(x, tc.x, 1e-9) || !near(y, tc.y, 1e-9) || !near(z, tc.z, 1e-9) || snap != tc.wantSnap {
			t.Fatalf("Seek(%v): got (x=%g, y=%g, z=%g, snap=%g), want (%g, %g, %g, %g)",
				tc.at, x, y, z, snap, tc.x, tc.y, tc.z, tc.wantSnap)
		}
	}
	if cueCount != 0 {
		t.Fatalf("Seek fired %d cues, want 0", cueCount)
	}
}

func TestTimelinePlaybackForwardReverseCuesAndLoopOnGameLoop(t *testing.T) {
	const ms = time.Millisecond

	var x float64
	var events []string

	tl := tween.NewTimeline()
	tl.Call(0, func() { events = append(events, "start") })
	tl.Append(tween.Float64(0, 100, 200*ms, core.Linear, func(v float64) { x = v }).OnComplete(func() {
		events = append(events, "t1-done")
	}))
	tl.Call(200*ms, func() { events = append(events, "mid") })
	tl.Append(tween.Float64(100, 200, 200*ms, core.Linear, func(v float64) { x = v }))
	tl.Call(400*ms, func() { events = append(events, "end") })

	if tl.Duration() != 400*ms {
		t.Fatalf("Duration = %v, want 400ms", tl.Duration())
	}

	l := &game.Loop{
		Rate: 50, // exact 20ms steps
		Update: func(dt time.Duration) {
			tl.Update(dt)
		},
	}
	app := startLoop(t, l)

	// Paused initially: advancing the loop does nothing.
	app.Advance(100 * ms)
	if tl.Time() != 0 || len(events) != 0 || tl.Playing() {
		t.Fatalf("paused timeline moved: time=%v events=%v playing=%v", tl.Time(), events, tl.Playing())
	}

	// Play forward to 200ms, pause, verify no drift while paused, then finish.
	tl.Play()
	app.Advance(200 * ms)
	if tl.Time() != 200*ms || x != 100 || !tl.Playing() || tl.Done() {
		t.Fatalf("at 200ms: time=%v x=%g playing=%v done=%v", tl.Time(), x, tl.Playing(), tl.Done())
	}
	if want := []string{"start", "t1-done", "mid"}; !slices.Equal(events, want) {
		t.Fatalf("at 200ms events = %v, want %v", events, want)
	}

	tl.Pause()
	app.Advance(100 * ms)
	if tl.Time() != 200*ms || x != 100 {
		t.Fatalf("moved while paused: time=%v x=%g", tl.Time(), x)
	}

	tl.Play()
	app.Advance(200 * ms)
	if tl.Time() != 400*ms || x != 200 || tl.Playing() || !tl.Done() {
		t.Fatalf("at end: time=%v x=%g playing=%v done=%v", tl.Time(), x, tl.Playing(), tl.Done())
	}
	if want := []string{"start", "t1-done", "mid", "end"}; !slices.Equal(events, want) {
		t.Fatalf("forward events = %v, want %v", events, want)
	}

	// Reverse from natural end: should NOT double-fire "end", fires "mid" then "start".
	events = nil
	tl.Reverse(true)
	tl.Play()
	app.Advance(400 * ms)
	if tl.Time() != 0 || x != 0 || tl.Playing() || !tl.Done() {
		t.Fatalf("reverse end: time=%v x=%g playing=%v done=%v", tl.Time(), x, tl.Playing(), tl.Done())
	}
	if want := []string{"mid", "start"}; !slices.Equal(events, want) {
		t.Fatalf("reverse from natural end events = %v, want %v", events, want)
	}

	// Restart while reversed jumps to Duration and fires "end", "mid", "start" in reverse order.
	events = nil
	tl.Restart()
	app.Advance(400 * ms)
	if tl.Time() != 0 || x != 0 || !tl.Done() {
		t.Fatalf("reverse Restart end: time=%v x=%g done=%v", tl.Time(), x, tl.Done())
	}
	if want := []string{"end", "mid", "start"}; !slices.Equal(events, want) {
		t.Fatalf("reverse Restart events = %v, want %v", events, want)
	}

	// Looping forward fires all cues cleanly on every cycle.
	events = nil
	tl.Reverse(false)
	tl.SetLoop(true)
	tl.Restart()
	app.Advance(800 * ms) // 2 exact cycles
	wantLoop := []string{"start", "t1-done", "mid", "end", "start", "t1-done", "mid", "end"}
	if !slices.Equal(events, wantLoop) {
		t.Fatalf("2-loop events = %v, want %v", events, wantLoop)
	}
	if tl.Time() != 0 || !tl.Playing() || tl.Done() {
		t.Fatalf("after 2 loops: time=%v playing=%v done=%v", tl.Time(), tl.Playing(), tl.Done())
	}

	// Cue calling Pause stops later cues in the same step.
	var stopped []string
	tlPause := tween.NewTimeline()
	tlPause.Call(50*ms, func() {
		stopped = append(stopped, "first")
		tlPause.Pause()
	})
	tlPause.Call(80*ms, func() { stopped = append(stopped, "second") })
	tlPause.Play()
	tlPause.Update(100 * ms)
	if !slices.Equal(stopped, []string{"first"}) || tlPause.Playing() {
		t.Fatalf("pause in cue: events=%v playing=%v, want [first] false", stopped, tlPause.Playing())
	}

	// Zero-duration looping timeline must not hang.
	zFired := 0
	zTL := tween.NewTimeline().Call(0, func() { zFired++ })
	zTL.SetLoop(true)
	zTL.Play()
	zTL.Update(100 * ms)
	if zFired != 1 {
		t.Fatalf("zero-duration cue fired %d times, want 1", zFired)
	}
}

func TestCameraMovesExactTargetsAndBoundsOnGameLoop(t *testing.T) {
	const ms = time.Millisecond

	bounds := core.Bounds{
		Origin: core.Point{X: 0, Y: 0},
		Size:   core.Size{Width: 1000, Height: 600},
	}

	t.Run("unbounded exact targets", func(t *testing.T) {
		cam := &tilemap.Camera{X: 10, Y: 20, Zoom: 0, Rotation: 0, Width: 200, Height: 100}

		var g tween.Group
		pan := tween.CameraPan(cam, 210, -80, 200*ms, core.EaseInOut)
		zoom := tween.CameraZoom(cam, 2.5, 200*ms, core.EaseOut)
		rot := tween.CameraRotate(cam, math.Pi/2, 200*ms, core.EaseIn)
		g.Add(pan, zoom, rot)

		l := &game.Loop{Rate: 50, Update: g.Update}
		app := startLoop(t, l)

		// Midpoint check: Zoom started from effective 1 (not 0).
		app.Advance(100 * ms)
		wantZoomMid := float32(1 + (2.5-1)*core.EaseOut.At(0.5))
		if !near(float64(cam.Zoom), float64(wantZoomMid), 1e-4) {
			t.Fatalf("midpoint Zoom = %g, want %g (effective start zoom 1)", cam.Zoom, wantZoomMid)
		}

		// Exact final step (200ms = 10 steps at 20ms).
		app.Advance(100 * ms)
		if cam.X != 210 || cam.Y != -80 || cam.Zoom != 2.5 || cam.Rotation != float32(math.Pi/2) {
			t.Fatalf("end camera = (%g, %g, zoom=%g, rot=%g), want (210, -80, 2.5, %g)",
				cam.X, cam.Y, cam.Zoom, cam.Rotation, float32(math.Pi/2))
		}
		if !pan.Done() || !zoom.Done() || !rot.Done() || g.Len() != 0 {
			t.Fatalf("expected all camera tweens done, len=%d", g.Len())
		}
	})

	t.Run("CameraMove with bounds clamping every step", func(t *testing.T) {
		cam := &tilemap.Camera{
			X: 200, Y: 200, Zoom: 2, Rotation: 0,
			Width: 200, Height: 100,
			Bounds: &bounds,
		}
		// Move to (980, 580) at zoom 1, rotation 0: valid clamped center is [100..900] x [50..550].
		mv := tween.CameraMove(cam, 980, 580, 1, 0, 200*ms, core.Linear)

		l := &game.Loop{
			Rate: 50,
			Update: func(dt time.Duration) {
				mv.Update(dt)
				vb := cam.VisibleBounds()
				if vb.Origin.X < bounds.Origin.X-1e-3 ||
					vb.Origin.Y < bounds.Origin.Y-1e-3 ||
					vb.Origin.X+vb.Size.Width > bounds.Origin.X+bounds.Size.Width+1e-3 ||
					vb.Origin.Y+vb.Size.Height > bounds.Origin.Y+bounds.Size.Height+1e-3 {
					t.Errorf("VisibleBounds %+v escaped Bounds %+v", vb, bounds)
				}
			},
		}
		app := startLoop(t, l)
		app.Advance(200 * ms)

		if !mv.Done() {
			t.Fatal("CameraMove should be done at 200ms")
		}
		if cam.X != 900 || cam.Y != 550 || cam.Zoom != 1 || cam.Rotation != 0 {
			t.Fatalf("clamped CameraMove end = (%g, %g, zoom=%g, rot=%g), want (900, 550, 1, 0)",
				cam.X, cam.Y, cam.Zoom, cam.Rotation)
		}
	})

	t.Run("CameraFollowTransition tracks moving target and lands exactly on it", func(t *testing.T) {
		cam := &tilemap.Camera{
			X: 100, Y: 100, Zoom: 1,
			Width: 200, Height: 100,
			Bounds: &bounds,
		}
		player := core.Point{X: 300, Y: 200}

		ft := tween.CameraFollowTransition(cam, func() core.Point { return player }, 200*ms, core.EaseInOut)

		l := &game.Loop{
			Rate: 50, // 10 steps of 20ms
			Update: func(dt time.Duration) {
				// Player moves every step during the transition.
				player.X += 15
				player.Y += 10
				ft.Update(dt)
			},
		}
		app := startLoop(t, l)
		app.Advance(200 * ms)

		if !ft.Done() {
			t.Fatal("CameraFollowTransition should be done at 200ms")
		}
		// After 10 steps: player is at (450, 300), well inside bounds [100..900] x [50..550].
		if player != (core.Point{X: 450, Y: 300}) {
			t.Fatalf("player = %+v, want {450 300}", player)
		}
		if cam.X != 450 || cam.Y != 300 {
			t.Fatalf("camera end = (%g, %g), want exact player position (450, 300)", cam.X, cam.Y)
		}

		// When target moves outside bounds, CameraFollowTransition still clamps cleanly.
		farTarget := core.Point{X: 2000, Y: -500}
		ftClamped := tween.CameraFollowTransition(cam, func() core.Point { return farTarget }, 100*ms, core.Linear)
		ftClamped.Update(100 * ms)
		if cam.X != 900 || cam.Y != 50 {
			t.Fatalf("clamped follow end = (%g, %g), want (900, 50)", cam.X, cam.Y)
		}
	})

	t.Run("chained camera timeline play seek and restart", func(t *testing.T) {
		cam := &tilemap.Camera{X: 0, Y: 0, Zoom: 1, Rotation: 0, Width: 200, Height: 100}

		tl := tween.NewTimeline()
		tl.Append(tween.CameraPan(cam, 200, 100, 200*ms, core.Linear))
		tl.Append(tween.CameraPan(cam, 400, 300, 200*ms, core.Linear))
		tl.Add(200*ms, tween.CameraZoom(cam, 2, 200*ms, core.Linear))

		// Cold Seek directly into the middle of the second pan resolves track 1 first!
		tl.Seek(300 * ms)
		if cam.X != 300 || cam.Y != 200 || cam.Zoom != 1.5 {
			t.Fatalf("cold Seek(300ms): cam=(%g, %g, zoom=%g), want (300, 200, 1.5)", cam.X, cam.Y, cam.Zoom)
		}

		// Seek back into first pan restores track 1's position and zoom 1.
		tl.Seek(100 * ms)
		if cam.X != 100 || cam.Y != 50 || cam.Zoom != 1 {
			t.Fatalf("Seek(100ms): cam=(%g, %g, zoom=%g), want (100, 50, 1)", cam.X, cam.Y, cam.Zoom)
		}

		// Restart replays the entire choreography on game.Loop.
		tl.Restart()
		l := &game.Loop{Rate: 50, Update: tl.Update}
		app := startLoop(t, l)
		app.Advance(400 * ms)
		if !tl.Done() || cam.X != 400 || cam.Y != 300 || cam.Zoom != 2 {
			t.Fatalf("after Restart+400ms: done=%v cam=(%g, %g, zoom=%g), want true (400, 300, 2)",
				tl.Done(), cam.X, cam.Y, cam.Zoom)
		}
	})

	t.Run("nil camera and nil target do not panic", func(t *testing.T) {
		for _, tw := range []*tween.Tween{
			tween.CameraPan(nil, 10, 20, 100*ms, core.Linear),
			tween.CameraZoom(nil, 2, 100*ms, core.Linear),
			tween.CameraRotate(nil, 1, 100*ms, core.Linear),
			tween.CameraMove(nil, 10, 20, 2, 1, 100*ms, core.Linear),
			tween.CameraFollowTransition(nil, nil, 100*ms, core.Linear),
		} {
			tw.Update(50 * ms)
			if !tw.Update(50 * ms) {
				t.Fatal("nil-camera tween should complete")
			}
		}

		cam := &tilemap.Camera{X: 12, Y: 34}
		twNilTarget := tween.CameraFollowTransition(cam, nil, 100*ms, core.Linear)
		twNilTarget.Update(100 * ms)
		if cam.X != 12 || cam.Y != 34 {
			t.Fatalf("nil-target follow moved camera to (%g, %g)", cam.X, cam.Y)
		}
	})
}
