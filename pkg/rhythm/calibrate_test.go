package rhythm_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/rhythm"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// tapAll taps cal at each cue from first on plus offset[i % len(offset)], until it is done or n taps.
func tapAll(cal *rhythm.Calibrator, first, n int, offset ...time.Duration) {
	for k := first; k < first+n && !cal.Done(); k++ {
		cal.Tap(cal.Cue(k).Add(offset[(k-first)%len(offset)]))
	}
}

func TestCalibratorMeasuresTheMedianOffset(t *testing.T) {
	cal := &rhythm.Calibrator{}
	if _, counted := cal.Tap(epoch); counted || cal.Started() {
		t.Fatal("a tap before Start counted")
	}
	cal.Start(epoch)
	// The warmup taps are wild and ignored; the rest are 90ms late give or take 10ms, with one outlier the median ignores.
	for k, off := range []time.Duration{-300, 250, 10, 360} {
		if _, counted := cal.Tap(cal.Cue(k).Add(off * time.Millisecond)); counted {
			t.Fatalf("warmup tap %d counted", k)
		}
	}
	offs := []time.Duration{90, 80, 100, 85, 95, 90, 300, 90, 88, 92, 90, 90, 80, 100, 90, 90}
	for i, off := range offs {
		got, counted := cal.Tap(cal.Cue(4 + i).Add(off * time.Millisecond))
		if !counted || got != off*time.Millisecond {
			t.Fatalf("tap %d: %v counted %v", i, got, counted)
		}
	}
	r, done := cal.Result()
	if !done || !cal.Done() || r.Taps != rhythm.DefaultTaps || cal.Count() != rhythm.DefaultTaps {
		t.Fatalf("done %v with %d taps", done, r.Taps)
	}
	if r.Offset != 90*time.Millisecond || r.Spread != 2*time.Millisecond || !r.Steady() {
		t.Fatalf("result %+v", r)
	}
	if _, counted := cal.Tap(cal.Cue(30)); counted {
		t.Fatal("a tap after done counted")
	}
}

func TestCalibratorReportsAnUnsteadyPlayer(t *testing.T) {
	cal := &rhythm.Calibrator{Taps: 6, Warmup: -1}
	cal.Start(epoch)
	if _, ok := cal.Result(); ok {
		t.Fatal("a result before any tap")
	}
	tapAll(cal, 0, 6, 0, 150*time.Millisecond, -100*time.Millisecond)
	r, done := cal.Result()
	if !done || r.Steady() || r.Spread < 100*time.Millisecond {
		t.Fatalf("result %+v done %v", r, done)
	}
}

func TestCalibratorMatchesTapsAroundTheSeed(t *testing.T) {
	// 450ms of Bluetooth latency is past half an interval: without a seed a tap is matched with the next cue.
	cal := &rhythm.Calibrator{Interval: 600 * time.Millisecond, Taps: 4, Warmup: -1}
	cal.Start(epoch)
	tapAll(cal, 0, 4, 450*time.Millisecond)
	if r, _ := cal.Result(); r.Offset != -150*time.Millisecond {
		t.Fatalf("unseeded offset %v, want the next cue's -150ms", r.Offset)
	}
	cal.Seed = 420 * time.Millisecond
	cal.Start(epoch)
	tapAll(cal, 0, 4, 450*time.Millisecond)
	if r, _ := cal.Result(); r.Offset != 450*time.Millisecond {
		t.Fatalf("seeded offset %v, want 450ms", r.Offset)
	}
	// A tap before cue 0, less the seed, is not matched.
	cal.Start(epoch)
	if _, counted := cal.Tap(epoch.Add(-time.Second)); counted || cal.Count() != 0 {
		t.Fatal("a tap before the first cue counted")
	}
}

func TestCalibratorMeasuresFromPresentedCues(t *testing.T) {
	cal := &rhythm.Calibrator{Taps: 3, Warmup: -1}
	cal.Start(epoch)
	if k, level := cal.Pulse(epoch.Add(-time.Millisecond)); k != -1 || level != 0 {
		t.Fatalf("pulse before cue 0: %d %v", k, level)
	}
	if k, level := cal.Pulse(cal.Cue(2)); k != 2 || level != 1 {
		t.Fatalf("pulse at cue 2: %d %v", k, level)
	}
	if _, level := cal.Pulse(cal.Cue(2).Add(rhythm.DefaultInterval / 8)); level != 0.5 {
		t.Fatalf("pulse an eighth after: %v", level)
	}
	if _, level := cal.Pulse(cal.Cue(2).Add(rhythm.DefaultInterval / 2)); level != 0 {
		t.Fatalf("pulse half after: %v", level)
	}
	// Each cue's frame came 16ms late; only the first presentation counts.
	for k := range 3 {
		cal.Present(k, cal.Cue(k).Add(16*time.Millisecond))
		cal.Present(k, cal.Cue(k).Add(33*time.Millisecond))
	}
	cal.Present(-1, epoch)
	tapAll(cal, 0, 3, 66*time.Millisecond)
	if r, _ := cal.Result(); r.Offset != 50*time.Millisecond {
		t.Fatalf("offset %v, want 50ms from the presented frames", r.Offset)
	}
	if k, _ := cal.Pulse(cal.Cue(4)); k != -1 {
		t.Fatal("a done calibration still pulses")
	}
}

// startCalibration starts an app with a click, and a calibration of kind made from it.
func startCalibration(t *testing.T, kind rhythm.CalibrationKind, env core.Environment) (*nsgotest.App, *rhythm.Calibration) {
	app, cal, _ := startCalibrationIn(t, kind, env)
	return app, cal
}

// startCalibrationIn is startCalibration that also returns the app's scope.
func startCalibrationIn(t *testing.T, kind rhythm.CalibrationKind, env core.Environment) (*nsgotest.App, *rhythm.Calibration, *core.Scope) {
	t.Helper()
	const click = "asset:sounds/click.wav"
	var cal *rhythm.Calibration
	var scope *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		scope = cx
		audio.LoadSound(cx, audio.Source{URI: click}, func(s *audio.Sound, err error) {
			if err != nil {
				t.Errorf("load the click: %v", err)
				return
			}
			cal = rhythm.NewCalibration(cx, kind, s)
		})
		return core.View{}
	}, nsgotest.Environment(env), func(a *nsgotest.App) {
		a.AudioDurations = map[string]time.Duration{click: 30 * time.Millisecond}
		a.AudioOutputLatency = 25 * time.Millisecond
	})
	if cal == nil {
		t.Fatal("the click did not load")
	}
	return app, cal, scope
}

func TestAudioCalibrationSchedulesClicksAndSeedsWithTheOutputLatency(t *testing.T) {
	app, cal := startCalibration(t, rhythm.CalibrateAudio, core.Environment{Audio: true, AudioSchedule: true})
	start := app.Clock.Now()
	cal.Taps, cal.Warmup = 8, 2
	cal.Start()
	app.Settle()
	if lat, ok := cal.OutputLatency(); !ok || lat != 25*time.Millisecond || cal.Seed != 25*time.Millisecond {
		t.Fatalf("latency %v %v, seed %v", lat, ok, cal.Seed)
	}
	if !cal.Cue(0).Equal(start.Add(rhythm.DefaultLead)) {
		t.Fatalf("cue 0 at %v, want a lead after %v", cal.Cue(0), start)
	}
	plays := app.Sounds[0].Plays
	// Scheduled two intervals ahead: the lead of a second holds cue 0 only.
	if len(plays) != 1 || !plays[0].StartAt.Equal(cal.Cue(0)) {
		t.Fatalf("plays %+v", plays)
	}
	for k := 0; !cal.Done(); k++ {
		app.FastForward(cal.Cue(k).Add(140 * time.Millisecond).Sub(app.Clock.Now()))
		cal.Update()
		cal.Tap()
	}
	r, done := cal.Apply(rhythm.Offsets{Video: 7 * time.Millisecond})
	if !done || r.Audio != 140*time.Millisecond || r.Video != 7*time.Millisecond {
		t.Fatalf("applied %+v %v", r, done)
	}
	plays = app.Sounds[0].Plays
	for k, p := range plays {
		if !p.StartAt.Equal(cal.Cue(k)) {
			t.Fatalf("click %d at %v, want cue %v", k, p.StartAt, cal.Cue(k))
		}
	}
	if len(plays) < 10 {
		t.Fatalf("%d clicks for 10 taps", len(plays))
	}
	// The tap that finishes stops the clicks booked ahead, and no more are booked.
	app.Settle()
	for i, p := range app.Sounds[0].Playbacks {
		if !p.Ended {
			t.Fatalf("click %d still plays after the last tap", i)
		}
	}
	booked := len(app.Sounds[0].Plays)
	app.FastForward(3 * rhythm.DefaultInterval)
	cal.Update()
	app.Settle()
	if len(app.Sounds[0].Plays) != booked {
		t.Fatal("a done calibration booked another click")
	}
	// Starting over stops the clicks still waiting to play and schedules afresh.
	before := len(app.Sounds[0].Playbacks)
	cal.Start()
	app.Settle()
	for i, p := range app.Sounds[0].Playbacks[:before] {
		if !p.Ended {
			t.Fatalf("click %d of the last run still plays", i)
		}
	}
	if cal.Count() != 0 || !app.Sounds[0].Plays[len(app.Sounds[0].Plays)-1].StartAt.Equal(cal.Cue(0)) {
		t.Fatal("a retry did not start over")
	}
}

func TestAudioCalibrationStopsWhenItsScreenGoes(t *testing.T) {
	app, cal := startCalibration(t, rhythm.CalibrateAudio, core.Environment{Audio: true, AudioSchedule: true})
	cal.Warmup = -1
	cal.Start()
	app.FastForward(cal.Cue(1).Sub(app.Clock.Now()))
	cal.Update()
	cal.Tap()
	app.Settle()
	plays := len(app.Sounds[0].Plays)
	if plays < 2 || cal.Count() != 1 {
		t.Fatalf("%d clicks, %d taps before stopping", plays, cal.Count())
	}
	cal.Stop()
	app.Settle()
	for i, p := range app.Sounds[0].Playbacks {
		if !p.Ended {
			t.Fatalf("click %d still plays after Stop", i)
		}
	}
	app.FastForward(3 * rhythm.DefaultInterval)
	cal.Update()
	app.Settle()
	if _, counted := cal.Tap(); counted || len(app.Sounds[0].Plays) != plays || cal.Started() {
		t.Fatal("a stopped calibration clicked or took a tap")
	}
	if r, _ := cal.Result(); r.Taps != 1 {
		t.Fatalf("stopping lost the taps: %+v", r)
	}
	cal.Start()
	app.Settle()
	if !cal.Started() || cal.Count() != 0 || len(app.Sounds[0].Plays) == plays {
		t.Fatal("Start did not run it again")
	}
}

func TestAudioCalibrationPlaysAtTheCueWithoutScheduling(t *testing.T) {
	app, cal := startCalibration(t, rhythm.CalibrateAudio, core.Environment{Audio: true})
	cal.Taps, cal.Warmup = 3, -1
	cal.Start()
	app.Settle()
	if len(app.Sounds[0].Plays) != 0 {
		t.Fatal("a click played before its cue")
	}
	// Frames come 10ms after each cue: the click plays then, and the tap is measured from it.
	for k := range 3 {
		app.FastForward(cal.Cue(k).Add(10 * time.Millisecond).Sub(app.Clock.Now()))
		cal.Update()
		cal.Update()
		app.FastForward(100 * time.Millisecond)
		cal.Tap()
	}
	plays := app.Sounds[0].Plays
	if len(plays) != 3 || !plays[0].StartAt.IsZero() {
		t.Fatalf("plays %+v", plays)
	}
	if r, done := cal.Result(); !done || r.Offset != 100*time.Millisecond {
		t.Fatalf("result %+v %v", r, done)
	}
}

func TestVideoCalibrationPlaysNothingAndSetsTheVideoOffset(t *testing.T) {
	app, cal := startCalibration(t, rhythm.CalibrateVideo, core.Environment{Audio: true, AudioSchedule: true})
	cal.Taps, cal.Warmup = 4, -1
	cal.Start()
	for k := range 4 {
		app.FastForward(cal.Cue(k).Add(45 * time.Millisecond).Sub(app.Clock.Now()))
		cal.Update()
		cal.Tap()
	}
	if len(app.Sounds[0].Plays) != 0 {
		t.Fatal("a video calibration clicked")
	}
	if _, ok := cal.OutputLatency(); ok {
		t.Fatal("a video calibration read the output latency")
	}
	o, ok := cal.Apply(rhythm.Offsets{Audio: time.Millisecond})
	if !ok || o.Video != 45*time.Millisecond || o.Audio != time.Millisecond {
		t.Fatalf("applied %+v %v", o, ok)
	}
}

func TestCalibrationKeepsNoUnsteadyResult(t *testing.T) {
	app, cal := startCalibration(t, rhythm.CalibrateVideo, core.Environment{Audio: true})
	cal.Taps, cal.Warmup = 4, -1
	cal.Start()
	for k, off := range []time.Duration{0, 200, -150, 300} {
		app.FastForward(cal.Cue(k).Add(off * time.Millisecond).Sub(app.Clock.Now()))
		cal.Tap()
	}
	if o, ok := cal.Apply(rhythm.Offsets{Video: 3}); ok || o.Video != 3 {
		t.Fatalf("applied an unsteady result: %+v", o)
	}
}
