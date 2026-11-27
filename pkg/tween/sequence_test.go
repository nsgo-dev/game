package tween_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/tween"
	"github.com/nsgo-dev/nsgo/core"
)

func TestNamedEasesMatchReferenceValues(t *testing.T) {
	cases := []struct {
		name string
		e    tween.Ease
		mid  float64
	}{
		{"SineIn", tween.SineIn, 0.292893231},
		{"SineOut", tween.SineOut, 0.707106769},
		{"SineInOut", tween.SineInOut, 0.5},
		{"SineOutIn", tween.SineOutIn, 0.5},
		{"QuadIn", tween.QuadIn, 0.25},
		{"QuadOut", tween.QuadOut, 0.75},
		{"QuadInOut", tween.QuadInOut, 0.5},
		{"QuadOutIn", tween.QuadOutIn, 0.5},
		{"CubicIn", tween.CubicIn, 0.125},
		{"CubicOut", tween.CubicOut, 0.875},
		{"CubicInOut", tween.CubicInOut, 0.5},
		{"CubicOutIn", tween.CubicOutIn, 0.5},
		{"QuartIn", tween.QuartIn, 0.0625},
		{"QuartOut", tween.QuartOut, 0.9375},
		{"QuartInOut", tween.QuartInOut, 0.5},
		{"QuartOutIn", tween.QuartOutIn, 0.5},
		{"QuintIn", tween.QuintIn, 0.03125},
		{"QuintOut", tween.QuintOut, 0.96875},
		{"QuintInOut", tween.QuintInOut, 0.5},
		{"QuintOutIn", tween.QuintOutIn, 0.5},
		{"ExpoIn", tween.ExpoIn, 0.03125},
		{"ExpoOut", tween.ExpoOut, 0.96875},
		{"ExpoInOut", tween.ExpoInOut, 0.5},
		{"ExpoOutIn", tween.ExpoOutIn, 0.5},
		{"CircIn", tween.CircIn, 0.133974597},
		{"CircOut", tween.CircOut, 0.866025388},
		{"CircInOut", tween.CircInOut, 0.5},
		{"CircOutIn", tween.CircOutIn, 0.5},
		{"BackIn", tween.BackIn, -0.0876974985},
		{"BackOut", tween.BackOut, 1.08769751},
		{"BackInOut", tween.BackInOut, 0.5},
		{"BackOutIn", tween.BackOutIn, 0.5},
		{"ElasticIn", tween.ElasticIn, -0.015625},
		{"ElasticOut", tween.ElasticOut, 1.015625},
		{"ElasticInOut", tween.ElasticInOut, 0.5},
		{"ElasticOutIn", tween.ElasticOutIn, 0.5},
		{"BounceIn", tween.BounceIn, 0.234375},
		{"BounceOut", tween.BounceOut, 0.765625},
		{"BounceInOut", tween.BounceInOut, 0.5},
		{"BounceOutIn", tween.BounceOutIn, 0.5},
		{"SpringIn", tween.SpringIn, -0.111565113},
		{"SpringOut", tween.SpringOut, 1.11156511},
		{"SpringInOut", tween.SpringInOut, 0.5},
		{"SpringOutIn", tween.SpringOutIn, 0.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.e(0); got != 0 {
				t.Fatalf("%s(0) = %g, want exact 0", tc.name, got)
			}
			if got := tc.e(1); got != 1 {
				t.Fatalf("%s(1) = %g, want exact 1", tc.name, got)
			}
			if got := tc.e(0.5); math.Abs(float64(got)-tc.mid) > 1e-6 {
				t.Fatalf("%s(0.5) = %g, want %g", tc.name, got, tc.mid)
			}
		})
	}
}

func TestToReadsStartValueWhenStepStarts(t *testing.T) {
	const ms = time.Millisecond

	// Built with v at 0; the value moves to 42 before the tween first runs.
	v := float32(0)
	tw := tween.To(&v, 100, 100*ms, nil)
	v = 42
	if tw.Update(50 * ms) {
		t.Fatal("halfway should not be done")
	}
	if want := float32(71); v != want {
		t.Fatalf("at 50%%: v = %g, want %g (start 42 read at step start)", v, want)
	}
	if !tw.Update(50*ms) || v != 100 {
		t.Fatalf("end: v = %g done = %v, want 100 true", v, tw.Done())
	}

	// A sequence step reads whatever the previous step left behind.
	seq := tween.Sequence().
		Then(tween.To(&v, 10, 100*ms, nil)).
		Then(tween.To(&v, 50, 100*ms, nil)).
		Build()
	if seq.Duration() != 200*ms {
		t.Fatalf("Duration = %v, want 200ms", seq.Duration())
	}
	seq.Play()
	seq.Update(100 * ms)
	if v != 10 {
		t.Fatalf("after first step: v = %g, want 10", v)
	}
	seq.Update(50 * ms)
	// Second step must start from 10 (the value when it started), so 50% -> 30.
	if want := float32(30); v != want {
		t.Fatalf("second step at 50%%: v = %g, want %g", v, want)
	}
	seq.Update(50 * ms)
	if v != 50 || !seq.Done() {
		t.Fatalf("end: v = %g done = %v, want 50 true", v, seq.Done())
	}

	// FromCurrent reads through the getter at step start.
	cur := 7.5
	fc := tween.FromCurrent(func() float64 { return cur }, func(n float64) { cur = n }, 20, 100*ms, tween.QuadOut)
	cur = 15
	fc.Update(50 * ms)
	want := 15 + (20-15)*0.75
	if !near(cur, want, 1e-6) {
		t.Fatalf("FromCurrent at 50%%: cur = %g, want %g", cur, want)
	}
	if !fc.Update(50*ms) || cur != 20 {
		t.Fatalf("FromCurrent end: cur = %g, want 20", cur)
	}
}

func TestByComposesRelatively(t *testing.T) {
	const ms = time.Millisecond

	v := float32(3)
	// By reads the start lazily: v moves to 10 before the tween runs.
	by := tween.By(&v, float32(5), 100*ms, nil, nil, nil)
	v = 10
	if !by.Update(100*ms) || v != 15 {
		t.Fatalf("By end: v = %g, want 15 (start 10 + delta 5)", v)
	}

	// Relative steps compose with the results of earlier steps in a sequence.
	var w float32
	seq := tween.Sequence().
		Then(tween.To(&v, 20, 100*ms, nil)).
		Then(tween.By(&v, float32(-8), 100*ms, nil, nil, nil)).
		With(tween.By(&w, float32(30), 50*ms, nil, nil, nil)).
		Build()
	seq.Play()
	seq.Update(100 * ms)
	if v != 20 {
		t.Fatalf("after first step: v = %g, want 20", v)
	}
	seq.Update(50 * ms)
	// The parallel With step has finished; the relative step is at its midpoint.
	if w != 30 {
		t.Fatalf("parallel By end: w = %g, want 30", w)
	}
	if want := float32(16); v != want {
		t.Fatalf("relative step at 50%%: v = %g, want %g (start 20 - 8)", v, want)
	}
	seq.Update(50 * ms)
	if v != 12 || !seq.Done() {
		t.Fatalf("relative end: v = %g done = %v, want 12 true", v, seq.Done())
	}

	// Point values add and interpolate with the defaults.
	pt := core.Point{X: 1, Y: 2}
	byPt := tween.By(&pt, core.Point{X: 10, Y: 20}, 100*ms, nil, nil, nil)
	byPt.Update(50 * ms)
	if pt != (core.Point{X: 6, Y: 12}) {
		t.Fatalf("By point at 50%% = %+v, want {6 12}", pt)
	}
	if !byPt.Update(50*ms) || pt != (core.Point{X: 11, Y: 22}) {
		t.Fatalf("By point end = %+v, want {11 22}", pt)
	}
}

func TestSequenceThenWithWaitCallAndLoops(t *testing.T) {
	const ms = time.Millisecond

	var scale, y float32
	calls := 0
	seq := tween.Sequence().
		Then(tween.To(&scale, 1.4, 80*ms, tween.BackOut)).
		With(tween.By(&y, -40, 300*ms, tween.QuadOut, nil, nil)).
		Wait(100 * ms).
		Then(tween.FromCurrent(func() float32 { return scale }, func(n float32) { scale = n }, 0.5, 200*ms, tween.ElasticOut)).
		Call(func() { calls++ }).
		Build()

	// Then(80ms) -> With parallel ends at 300ms -> Wait(100ms) -> Then(200ms) -> cursor 600ms.
	if seq.Duration() != 600*ms {
		t.Fatalf("Duration = %v, want 600ms", seq.Duration())
	}

	seq.Play()
	seq.Update(40 * ms)
	// Both parallel steps have started: the scale step is at 50% and the y step moves too.
	if y == 0 {
		t.Fatalf("parallel step not started: y = %g", y)
	}
	seq.Update(260 * ms)
	// At 300ms: scale step finished at its overshooting BackOut end (exact target), y at -40.
	if scale != 1.4 || y != -40 {
		t.Fatalf("at 300ms: scale = %g y = %g, want 1.4 -40", scale, y)
	}
	if calls != 0 {
		t.Fatalf("calls = %d before the cue, want 0", calls)
	}
	seq.Update(300 * ms)
	// At 600ms the FromCurrent step has ended at 0.5 and the cue fired once.
	if scale != 0.5 || calls != 1 || !seq.Done() {
		t.Fatalf("at 600ms: scale = %g calls = %d done = %v, want 0.5 1 true", scale, calls, seq.Done())
	}

	// Loops(3) plays exactly three runs, firing the cue on every run.
	calls = 0
	loop3 := tween.Sequence().
		Then(tween.To(&scale, 1, 100*ms, nil)).
		Call(func() { calls++ }).
		Loops(3).
		Build()
	loop3.Play()
	for i := 1; i <= 3; i++ {
		loop3.Update(100 * ms)
		if calls != i {
			t.Fatalf("run %d: calls = %d, want %d", i, calls, i)
		}
		if done := i == 3; loop3.Done() != done {
			t.Fatalf("run %d: done = %v, want %v", i, loop3.Done(), done)
		}
	}
	loop3.Update(100 * ms)
	if calls != 3 {
		t.Fatalf("after done: calls = %d, want 3", calls)
	}

	// The same sequence plays identically twice (determinism on fixed steps).
	run := func() []float32 {
		var got []float32
		v := float32(0)
		s := tween.Sequence().
			Then(tween.To(&v, 100, 100*ms, tween.ElasticOut)).
			With(tween.By(&v, float32(0), 100*ms, tween.BounceOut, nil, nil)).
			Loops(1).
			Build()
		s.Play()
		for range 10 {
			s.Update(10 * ms)
			got = append(got, v)
		}
		return got
	}
	a, b := run(), run()
	if !slices.Equal(a, b) {
		t.Fatalf("non-deterministic sequence: %v != %v", a, b)
	}
}

func TestTimelineSetLoopsCountsExactly(t *testing.T) {
	const ms = time.Millisecond

	finished := 0
	tl := tween.NewTimeline().
		Append(tween.Float64(0, 100, 100*ms, core.Linear, nil).OnComplete(func() { finished++ }))
	tl.SetLoops(3)
	tl.Play()

	// Each run's end wraps back to time 0 until the counted runs are exhausted.
	for i := 1; i <= 3; i++ {
		tl.Update(100 * ms)
		if finished != i {
			t.Fatalf("run %d: finished = %d, want %d", i, finished, i)
		}
		if done := i == 3; tl.Done() != done {
			t.Fatalf("run %d: done = %v, want %v", i, tl.Done(), done)
		}
	}
	if tl.Playing() {
		t.Fatal("counted timeline should stop after its last run")
	}
	// Extra updates do nothing after the counted runs end.
	tl.Update(100 * ms)
	if finished != 3 {
		t.Fatalf("after done: finished = %d, want 3", finished)
	}

	// SetLoop(true) stays infinite and SetLoops(0) is its alias.
	inf := tween.NewTimeline().Append(tween.Float64(0, 1, 100*ms, core.Linear, nil))
	inf.SetLoops(0)
	inf.Play()
	for range 5 {
		inf.Update(100 * ms)
	}
	if inf.Done() || !inf.Playing() {
		t.Fatalf("infinite loops: done = %v playing = %v, want false true", inf.Done(), inf.Playing())
	}
}

func TestTimelineAndGroupSpeedScale(t *testing.T) {
	const ms = time.Millisecond

	var x float64
	tl := tween.NewTimeline().Append(tween.Float64(0, 100, 100*ms, core.Linear, func(v float64) { x = v }))
	tl.SetSpeed(0.5)
	tl.Play()
	tl.Update(100 * ms) // advances only 50ms of timeline time
	if x != 50 || tl.Done() {
		t.Fatalf("speed 0.5 after 100ms: x = %g done = %v, want 50 false", x, tl.Done())
	}
	tl.Update(100 * ms)
	if x != 100 || !tl.Done() {
		t.Fatalf("speed 0.5 after 200ms: x = %g done = %v, want 100 true", x, tl.Done())
	}

	var y float64
	fast := tween.NewTimeline().Append(tween.Float64(0, 100, 100*ms, core.Linear, func(v float64) { y = v }))
	fast.SetSpeed(2)
	fast.Play()
	fast.Update(100 * ms)
	if y != 100 || !fast.Done() {
		t.Fatalf("speed 2 after 100ms: y = %g done = %v, want 100 true", y, fast.Done())
	}

	// A stopped timeline advances nothing.
	var z float64
	stopped := tween.NewTimeline().Append(tween.Float64(0, 100, 100*ms, core.Linear, func(v float64) { z = v }))
	stopped.SetSpeed(0)
	stopped.Play()
	stopped.Update(100 * ms)
	if z != 0 || stopped.Time() != 0 {
		t.Fatalf("speed 0: z = %g time = %v, want 0 0", z, stopped.Time())
	}

	// The group scales and pauses every tween and timeline together.
	var gx float64
	var g tween.Group
	gtl := tween.NewTimeline().Append(tween.Float64(0, 100, 100*ms, core.Linear, func(v float64) { gx = v }))
	gtl.Play()
	g.AddTimeline(gtl)
	g.Add(tween.Float64(0, 100, 100*ms, core.Linear, func(float64) {}))

	g.SetSpeed(0.25)
	g.Update(40 * ms) // 10ms of scaled time
	if gx != 10 {
		t.Fatalf("group speed 0.25 after 40ms: gx = %g, want 10", gx)
	}
	g.SetPaused(true)
	g.Update(40 * ms)
	if gx != 10 {
		t.Fatalf("group paused: gx = %g, want 10", gx)
	}
	if !g.Paused() {
		t.Fatal("group should report paused")
	}
	g.SetPaused(false)
	g.Update(360 * ms) // 90ms of scaled time -> the timeline finishes
	if gx != 100 || !gtl.Done() {
		t.Fatalf("group end: gx = %g done = %v, want 100 true", gx, gtl.Done())
	}
}

func TestGroupBindClearsWhenScopeEnds(t *testing.T) {
	const ms = time.Millisecond

	var g tween.Group
	g.Add(tween.Float64(0, 1, 100*ms, core.Linear, nil))
	tl := tween.NewTimeline().Append(tween.Float64(0, 1, 100*ms, core.Linear, nil))
	tl.Play()
	g.AddTimeline(tl)

	root := core.NewRoot(core.RootOptions{})
	g.Bind(root)
	root.Dispose()

	if g.Len() != 0 {
		t.Fatalf("after scope end: Len = %d, want 0", g.Len())
	}
	// The timeline was cleared too: the group steps nothing after the scope ends.
	var x float64
	g.Add(tween.Float64(0, 100, 100*ms, core.Linear, func(v float64) { x = v }))
	g.Update(100 * ms)
	if x != 100 || g.Len() != 0 {
		t.Fatalf("group still usable after Bind cleanup: x = %g len = %d", x, g.Len())
	}
}

func TestSequenceSeekThroughSteps(t *testing.T) {
	const ms = time.Millisecond

	var scale, y float32
	seq := tween.Sequence().
		Then(tween.To(&scale, 1, 100*ms, nil)).
		With(tween.By(&y, -10, 200*ms, nil, nil, nil)).
		Wait(100 * ms).
		Then(tween.To(&scale, 2, 100*ms, nil)).
		Build()

	if seq.Duration() != 400*ms {
		t.Fatalf("Duration = %v, want 400ms", seq.Duration())
	}

	checks := []struct {
		at       time.Duration
		scale, y float32
	}{
		{0, 0, 0},
		{50 * ms, 0.5, -2.5},
		{100 * ms, 1, -5},
		{200 * ms, 1, -10},
		{250 * ms, 1, -10}, // inside the wait
		{350 * ms, 1.5, -10},
		{400 * ms, 2, -10},
		{150 * ms, 1, -7.5}, // backward seek
	}
	for _, tc := range checks {
		seq.Seek(tc.at)
		if !near(float64(scale), float64(tc.scale), 1e-6) || !near(float64(y), float64(tc.y), 1e-6) {
			t.Fatalf("Seek(%v): scale = %g y = %g, want %g %g", tc.at, scale, y, tc.scale, tc.y)
		}
	}
}
