package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/placeholder"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// stepTime is one fixed step of the game loop.
const stepTime = time.Second / stepRate

// laneKeys are the keys game.yaml binds to each lane.
var laneKeys = [Lanes]string{"d", "f", "j", "k"}

// laneButtons are the controller buttons game.yaml binds to each lane.
var laneButtons = [Lanes]gamepad.Button{gamepad.ButtonDPadLeft, gamepad.ButtonDPadDown, gamepad.ButtonSouth, gamepad.ButtonEast}

// startGame starts the game in a phone-sized window and returns it with its state.
func startGame(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *rhythmGame) {
	t.Helper()
	var g *rhythmGame
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		g = newRhythmGame(cx)
		return g.view()
	}, append([]nsgotest.Option{nsgotest.Size(480, 720)}, opts...)...)
	app.Advance(stepTime)
	return app, g
}

// playsAudio is an environment that plays audio, decoding the placeholder click and music as a platform would.
func playsAudio(t *testing.T) []nsgotest.Option {
	t.Helper()
	click, err := placeholder.Sound("click")
	if err != nil {
		t.Fatal(err)
	}
	return []nsgotest.Option{
		nsgotest.Environment(core.Environment{Audio: true, AudioSchedule: true}),
		func(a *nsgotest.App) {
			a.AudioDurations = map[string]time.Duration{
				string(click.Data):                 60 * time.Millisecond,
				string(Songs.Tracks[0].Music.Data): 6400 * time.Millisecond,
			}
		},
	}
}

// advanceTo runs the game until the engine's clock reads at, if it has not yet.
func advanceTo(app *nsgotest.App, at time.Time) {
	if d := at.Sub(app.Clock.Now()); d > 0 {
		app.Advance(d)
	}
}

// noteAt is when a player on this device hits note i of the chart being played dead on: its track time, played by the clock, plus the device's audio offset.
func noteAt(g *rhythmGame, i int) time.Time {
	return g.clock.TrackTime(g.run.NoteTime(i) + g.clock.Offsets().Audio)
}

// finishChart runs the game past the end of the chart being played.
func finishChart(app *nsgotest.App, g *rhythmGame) {
	advanceTo(app, g.clock.TrackTime(g.run.End()+g.clock.Offsets().Audio))
	app.Advance(4 * stepTime)
}

// key presses a key and runs the steps that see it go down and come up, so the next key is a press of its own.
func key(app *nsgotest.App, name string) {
	app.Key(name)
	app.Advance(4 * stepTime)
}

func expectText(t *testing.T, app *nsgotest.App, id, want string) {
	t.Helper()
	if got := app.Find(id).Text; got != want {
		t.Fatalf("%s = %q, want %q", id, got, want)
	}
}

// keepStorage returns what the app's storage holds, to start another app with.
func keepStorage(app *nsgotest.App) map[string]string {
	kept := map[string]string{}
	st := app.Storage()
	for _, k := range st.Keys() {
		kept[k], _ = st.GetItem(k)
	}
	return kept
}

// TestKeyboardClearsTheEasyChartPerfectlyAndKeepsTheRecord plays the easy chart to a full-score clear with the lane keys and checks the record outlives the app.
func TestKeyboardClearsTheEasyChartPerfectlyAndKeepsTheRecord(t *testing.T) {
	app, g := startGame(t)
	expectText(t, app, "chart-night-drive-easy-detail", "Not cleared")
	app.Key("enter")
	app.Advance(stepTime)
	if g.screen.Get() != screenPlay {
		t.Fatal("Enter did not start the highlighted chart")
	}
	expectText(t, app, "judgement", "Get ready")
	for i, n := range g.run.Chart().Notes {
		advanceTo(app, noteAt(g, i))
		app.Key(laneKeys[n.Lane])
		app.Advance(stepTime)
		expectText(t, app, "judgement", "Perfect")
	}
	expectText(t, app, "combo", "28 combo")
	finishChart(app, g)
	expectText(t, app, "result-title", "Cleared")
	expectText(t, app, "result-score", "1,000,000")
	expectText(t, app, "result-rank", "Rank S")
	expectText(t, app, "result-counts", "Perfect 28 · Great 0 · Good 0 · Miss 0")
	if !app.Exists("result-best") {
		t.Fatal("the first clear is not a new best")
	}
	// Escape goes back to the menu, which shows the record.
	app.Key("escape")
	app.Advance(stepTime)
	expectText(t, app, "chart-night-drive-easy-detail", "Best 1,000,000 S")

	again, _ := startGame(t, nsgotest.Storage(keepStorage(app)))
	expectText(t, again, "chart-night-drive-easy-detail", "Best 1,000,000 S")
}

// TestGamepadFailsTheHardChart picks the hard chart with the D-pad and South, hits its first notes with the controller and then stops, so the misses run the energy out.
func TestGamepadFailsTheHardChart(t *testing.T) {
	app, g := startGame(t)
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	app.Advance(stepTime)
	press := func(b gamepad.Button) {
		app.SetGamepadButton(1, b, 1)
		app.Advance(stepTime)
		app.SetGamepadButton(1, b, 0)
		app.Advance(stepTime)
	}
	press(gamepad.ButtonDPadDown)
	press(gamepad.ButtonSouth)
	if g.screen.Get() != screenPlay || g.run.Chart().ID != "hard" {
		t.Fatal("the controller did not start the hard chart")
	}
	const hits = 3
	for i, n := range g.run.Chart().Notes[:hits] {
		// The controller is read once a step, so the button goes down a step before the note.
		advanceTo(app, noteAt(g, i).Add(-stepTime))
		app.SetGamepadButton(1, laneButtons[n.Lane], 1)
		app.Advance(stepTime)
		app.SetGamepadButton(1, laneButtons[n.Lane], 0)
		app.Advance(stepTime)
		if g.run.Counts[Perfect]+g.run.Counts[Great] != i+1 {
			t.Fatalf("hit %d: counts %v", i, g.run.Counts)
		}
	}
	finishChart(app, g)
	expectText(t, app, "result-title", "Failed")
	expectText(t, app, "result-rank", "Rank -")
	if app.Exists("result-best") || len(g.records.Best) != 0 {
		t.Fatal("a failed chart was recorded")
	}
	// South retries the chart from its start.
	press(gamepad.ButtonSouth)
	if g.screen.Get() != screenPlay || g.run.Counts != [len(grades)]int{} {
		t.Fatal("South did not retry")
	}
}

// TestTouchAndMouseClearTheEasyChart taps the lanes of the easy chart, the first half with a finger and the rest with the mouse, some of them early.
func TestTouchAndMouseClearTheEasyChart(t *testing.T) {
	app, g := startGame(t)
	app.Press("chart-night-drive-easy")
	app.Advance(stepTime)
	if g.screen.Get() != screenPlay {
		t.Fatal("tapping the chart did not start it")
	}
	l := layoutFor(g.field)
	notes := g.run.Chart().Notes
	for i, n := range notes {
		at := noteAt(g, i)
		early := i%4 == 3
		if early {
			at = at.Add(-60 * time.Millisecond)
		}
		advanceTo(app, at)
		b := l.laneBounds(n.Lane)
		p := core.Point{X: b.Origin.X + b.Size.Width/2, Y: core.Points(l.hitY)}
		if i < len(notes)/2 {
			f := app.Touch("field", p)
			app.Advance(stepTime)
			f.Lift()
		} else {
			app.PressAt("field", p)
			app.Advance(stepTime)
		}
		want := "Perfect"
		if early {
			want = "Great · early"
		}
		expectText(t, app, "judgement", want)
		app.Advance(stepTime)
	}
	finishChart(app, g)
	expectText(t, app, "result-title", "Cleared")
	expectText(t, app, "result-counts", "Perfect 21 · Great 7 · Good 0 · Miss 0")
	expectText(t, app, "result-rank", "Rank A")
	// The result's Menu button goes back.
	app.Press("menu")
	app.Advance(stepTime)
	expectText(t, app, "chart-night-drive-easy-detail", "Best 925,000 A")
}

// TestPauseStopsTheChart pauses before the first note, waits, resumes and hits it, then quits from the pause menu.
func TestPauseStopsTheChart(t *testing.T) {
	app, g := startGame(t)
	app.Key("enter")
	app.Advance(stepTime)
	first := g.run.Chart().Notes[0]
	advanceTo(app, noteAt(g, 0).Add(-200*time.Millisecond))
	app.Key("escape")
	app.Advance(stepTime)
	if !app.Exists("pause-title") {
		t.Fatal("Escape did not pause")
	}
	app.Advance(5 * time.Second)
	if g.run.Counts[Miss] != 0 || g.run.Judged(0) {
		t.Fatal("a note was missed while paused")
	}
	app.Key("enter")
	app.Advance(stepTime)
	if app.Exists("pause-title") {
		t.Fatal("Enter on Resume did not resume")
	}
	advanceTo(app, noteAt(g, 0))
	app.Key(laneKeys[first.Lane])
	app.Advance(stepTime)
	expectText(t, app, "judgement", "Perfect")

	app.Press("pause")
	app.Advance(stepTime)
	key(app, "down")
	key(app, "down")
	app.Key("enter")
	app.Advance(stepTime)
	if g.screen.Get() != screenMenu || g.music != nil || g.cursor.Get() != 0 {
		t.Fatal("Quit did not end the chart and highlight it in the menu")
	}
}

// TestLosingTheFocusPauses has the window lose the focus mid-chart, which pauses it.
func TestLosingTheFocusPauses(t *testing.T) {
	app, g := startGame(t)
	app.Key("enter")
	app.Advance(stepTime)
	app.Advance(time.Second)
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	app.Advance(stepTime)
	if !g.paused.Get() {
		t.Fatal("going to the background did not pause")
	}
}

// TestAudioCalibrationSetsTheOffsetChartsAreJudgedBy calibrates the audio offset tapping 80ms after each click, then plays the easy chart 80ms behind the music, which the offset judges perfect; the music plays and loops all the while, keeping the clock.
func TestAudioCalibrationSetsTheOffsetChartsAreJudgedBy(t *testing.T) {
	const late = 80 * time.Millisecond
	app, g := startGame(t, playsAudio(t)...)
	if !app.Enabled("calibrate-audio") {
		t.Fatal("audio calibration is disabled where audio plays")
	}
	key(app, "down")
	key(app, "down")
	app.Key("enter")
	app.Advance(stepTime)
	if g.screen.Get() != screenCalibrate {
		t.Fatal("Enter did not start the audio calibration")
	}
	cal := g.calScreen.Calibration()
	for k := 0; !cal.Done(); k++ {
		advanceTo(app, cal.Cue(k).Add(late))
		app.Key("j")
		app.Advance(stepTime)
		if k > 40 {
			t.Fatal("the calibration did not finish")
		}
	}
	if r, done := cal.Result(); !done || r.Offset != late || !r.Steady() {
		t.Fatalf("result %+v, done %v", r, done)
	}
	app.Key("enter")
	app.Advance(stepTime)
	if g.screen.Get() != screenMenu {
		t.Fatal("Enter did not save the result")
	}
	expectText(t, app, "offsets", "Audio offset +80 ms · Video offset +0 ms")

	key(app, "up")
	key(app, "up")
	app.Key("enter")
	app.Advance(stepTime)
	for i, n := range g.run.Chart().Notes {
		advanceTo(app, noteAt(g, i))
		app.Key(laneKeys[n.Lane])
		app.Advance(stepTime)
	}
	if !g.musicStarted || g.silent || len(app.Players) == 0 || !app.Players[len(app.Players)-1].Playing {
		t.Fatal("the music is not playing")
	}
	finishChart(app, g)
	expectText(t, app, "result-counts", "Perfect 28 · Great 0 · Good 0 · Miss 0")

	// The offset outlives the app.
	again, _ := startGame(t, append(playsAudio(t), nsgotest.Storage(keepStorage(app)))...)
	expectText(t, again, "offsets", "Audio offset +80 ms · Video offset +0 ms")
}

// TestVideoCalibrationMeasuresTheFlashes taps 30ms after each flash of the video calibration, taps the screen to save it, and checks the audio calibration is disabled where no audio plays.
func TestVideoCalibrationMeasuresTheFlashes(t *testing.T) {
	const late = 30 * time.Millisecond
	app, g := startGame(t)
	if app.Enabled("calibrate-audio") {
		t.Fatal("audio calibration is enabled where no audio plays")
	}
	app.Press("calibrate-video")
	app.Advance(stepTime)
	cal := g.calScreen.Calibration()
	for k := 0; !cal.Done(); k++ {
		advanceTo(app, cal.Cue(k))
		advanceTo(app, cal.Cue(k).Add(late))
		app.Touch("calibration", core.Point{X: 240, Y: 300}).Lift()
		app.Advance(stepTime)
		if k > 40 {
			t.Fatal("the calibration did not finish")
		}
	}
	if r, _ := cal.Result(); r.Offset != late {
		t.Fatalf("result %+v", r)
	}
	app.Key("enter")
	app.Advance(stepTime)
	expectText(t, app, "offsets", "Audio offset +0 ms · Video offset +30 ms")

	// Back leaves a calibration without saving.
	app.Press("calibrate-video")
	app.Advance(stepTime)
	app.Press("calibration-back")
	app.Advance(stepTime)
	if g.screen.Get() != screenMenu || g.calScreen != nil {
		t.Fatal("Back did not leave the calibration")
	}
}
