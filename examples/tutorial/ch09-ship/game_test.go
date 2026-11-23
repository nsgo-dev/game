package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// start runs the game with seed in a 240 by 320 window, where a view point is a playfield point.
func start(t *testing.T, seed uint64) (*nsgotest.App, *starCatcher) {
	t.Helper()
	var s *starCatcher
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newStarCatcher(cx, seed)
		return s.view()
	}, nsgotest.Size(240, 320))
	return app, s
}

// startPlaying runs the game and presses Start on its title screen.
func startPlaying(t *testing.T) (*nsgotest.App, *starCatcher) {
	t.Helper()
	app, s := start(t, 1)
	app.Find("text:Star Catcher")
	app.Press("role:button Start")
	app.Find("hud")
	return app, s
}

func TestTitleWaitsForStart(t *testing.T) {
	app, s := start(t, 1)
	app.Find("title")
	app.Advance(3 * time.Second)
	if len(s.world.items) != 0 {
		t.Fatalf("%d items fell on the title screen", len(s.world.items))
	}
	if app.Exists("hud") {
		t.Fatal("the HUD shows on the title screen")
	}

	app.Press("role:button Start")
	app.Advance(3 * time.Second)
	if app.Exists("title") {
		t.Fatal("the title screen still shows after Start")
	}
	if len(s.world.items) == 0 {
		t.Fatal("nothing fell in the first three seconds of a game")
	}
}

func TestEnterStartsAGame(t *testing.T) {
	app, _ := start(t, 1)
	app.Key("enter")
	app.Find("hud")
	app.Find("text:Lives: 3")
}

func TestPauseStopsTheWorld(t *testing.T) {
	app, s := startPlaying(t)
	s.world.spawnIn = time.Hour
	s.world.drop(kindStar, 0, 40)
	app.Advance(step)

	app.Press("role:button Pause")
	app.Find("text:Paused")
	y := s.world.items[0].y
	app.Advance(time.Second)
	if got := s.world.items[0].y; got != y {
		t.Fatalf("star fell from %v to %v while paused", y, got)
	}

	app.Press("role:button Resume")
	app.Advance(time.Second / 2)
	if got := s.world.items[0].y; got <= y {
		t.Fatalf("star at %v after resuming, want below %v", got, y)
	}
}

func TestEscapeTogglesPause(t *testing.T) {
	app, _ := startPlaying(t)
	app.Key("escape")
	app.Find("paused")
	app.Key("p")
	app.Find("hud")
	if app.Exists("paused") {
		t.Fatal("pause menu still shows after P")
	}
}

func TestGameOverScreen(t *testing.T) {
	app, s := startPlaying(t)
	s.world.spawnIn = time.Hour
	s.world.drop(kindStar, s.world.basketLane, basketY-itemSize)
	s.world.drop(kindStar, 0, groundY-itemSize/2-60)
	s.world.lives = 1
	app.Advance(time.Second)
	app.Find("text:Game over")
	app.Find("text:You caught 1 star")
	if app.Exists("hud") {
		t.Fatal("the HUD still shows after the game ended")
	}

	app.Press("role:button Play again")
	app.Find("hud")
	app.Find("text:Score: 0")
	app.Find("text:Lives: 3")
}

func TestQuitReturnsToTitle(t *testing.T) {
	app, s := startPlaying(t)
	s.world.drop(kindRock, 0, 100)
	app.Key("escape")
	app.Press("role:button Quit")
	app.Find("title")
	if len(s.world.items) != 0 {
		t.Fatal("quitting left items on the playfield")
	}
	app.Advance(time.Second)
	if len(s.world.items) != 0 {
		t.Fatal("items fall on the title screen after quitting")
	}
}

// withSounds gives the test platform audio, and lets it decode the game's effects, as a device decodes them.
func withSounds(t *testing.T) (nsgotest.Option, map[event]audio.Source) {
	t.Helper()
	sources, err := makeSounds()
	if err != nil {
		t.Fatal(err)
	}
	return func(a *nsgotest.App) {
		nsgotest.Environment(core.Environment{Audio: true})(a)
		a.AudioDurations = map[string]time.Duration{}
		for _, src := range sources {
			a.AudioDurations[string(src.Data)] = time.Second / 2
		}
	}, sources
}

// plays is how often the platform played src.
func plays(app *nsgotest.App, src audio.Source) int {
	for _, snd := range app.Sounds {
		if bytes.Equal(snd.Source.Data, src.Data) {
			return len(snd.Plays)
		}
	}
	return 0
}

func TestEventsPlayTheirSounds(t *testing.T) {
	sounds, sources := withSounds(t)
	var s *starCatcher
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newStarCatcher(cx, 1)
		return s.view()
	}, sounds, nsgotest.Size(240, 320))
	app.Advance(step)
	if len(s.effects) != len(sources) {
		t.Fatalf("%d effects loaded, want %d", len(s.effects), len(sources))
	}

	app.Press("role:button Start")
	s.world.spawnIn = time.Hour
	s.world.lives = 1
	s.world.drop(kindStar, s.world.basketLane, basketY-itemSize)
	s.world.drop(kindRock, s.world.basketLane, basketY-itemSize-60)
	app.Advance(2 * time.Second)

	for _, e := range []event{eventCaught, eventHit, eventOver} {
		if n := plays(app, sources[e]); n != 1 {
			t.Fatalf("event %d played its sound %d times, want once", e, n)
		}
	}
	if n := plays(app, sources[eventMissed]); n != 0 {
		t.Fatalf("the miss sound played %d times with no miss", n)
	}
}

func TestMuteIsRemembered(t *testing.T) {
	sounds, sources := withSounds(t)
	app, _ := start(t, 1)
	app.Find("text:Sound: on")
	app.Press("role:button Sound")
	app.Find("text:Sound: off")
	if v, _ := app.Storage().GetItem("starcatcher.sound.master"); v != "0" {
		t.Fatalf("stored master volume = %q, want 0", v)
	}

	// The next launch starts muted, and plays nothing.
	var s *starCatcher
	next := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newStarCatcher(cx, 1)
		return s.view()
	}, sounds, nsgotest.Size(240, 320), nsgotest.Storage(map[string]string{"starcatcher.sound.master": "0"}))
	next.Find("text:Sound: off")
	next.Press("role:button Start")
	s.world.spawnIn = time.Hour
	s.world.drop(kindStar, s.world.basketLane, basketY-itemSize)
	next.Advance(time.Second)
	next.Find("text:Score: 1")
	if n := plays(next, sources[eventCaught]); n != 0 {
		t.Fatalf("a muted catch played %d times", n)
	}
}

func TestSoundsAreShortAndInRange(t *testing.T) {
	_, sources := withSounds(t)
	for e, src := range sources {
		samples, rate, channels, err := sound.DecodeWAV(src.Data)
		if err != nil {
			t.Fatalf("event %d: %v", e, err)
		}
		if rate != sampleRate || channels != 1 {
			t.Fatalf("event %d: %d Hz, %d channels; want %d Hz mono", e, rate, channels, sampleRate)
		}
		if d := time.Duration(len(samples)) * time.Second / sampleRate; d <= 0 || d > time.Second {
			t.Fatalf("event %d lasts %v, want under a second", e, d)
		}
		if last := samples[len(samples)-1]; last > 0.01 || last < -0.01 {
			t.Fatalf("event %d ends at %v, want silence", e, last)
		}
	}
}

// endGame ends the game being played after the basket catches catches stars.
func endGame(app *nsgotest.App, s *starCatcher, catches int) {
	s.world.spawnIn = time.Hour
	for i := range catches {
		s.world.drop(kindStar, s.world.basketLane, basketY-itemSize-float32(30*i))
	}
	s.world.lives = 1
	s.world.drop(kindStar, (s.world.basketLane+2)%laneCount, basketY-itemSize-float32(30*catches))
	app.Advance(3 * time.Second)
	app.Find("game-over")
}

func TestBestScoreIsKept(t *testing.T) {
	app, s := startPlaying(t)
	if app.Exists("best") {
		t.Fatal("the title showed a best score before any game")
	}
	endGame(app, s, 2)
	app.Find("text:New best!")
	if got, _ := loadRecords(newRecordsStore(app.Storage())); got != (records{Best: 2, Games: 1}) {
		t.Fatalf("saved records = %+v, want best 2 of 1 game", got)
	}

	app.Press("role:button Play again")
	endGame(app, s, 1)
	app.Find("text:Best: 2")
	if app.Exists("new-best") {
		t.Fatal("a lower score was called a new best")
	}

	app.Press("role:button Title")
	app.Find("text:Best: 2")
}

func TestNewerRecordsAreNotSavedOver(t *testing.T) {
	const newer = `{"version":99,"data":{"best":5,"games":1}}`
	var s *starCatcher
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newStarCatcher(cx, 1)
		return s.view()
	}, nsgotest.Size(240, 320), nsgotest.Storage(map[string]string{recordsKey: newer}))
	app.Press("role:button Start")
	app.Find("hud")
	endGame(app, s, 2)
	if raw, _ := app.Storage().GetItem(recordsKey); raw != newer {
		t.Fatalf("slot = %s, want the newer version's records untouched", raw)
	}
}

func TestTitleShowsTheSavedBest(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return newStarCatcher(cx, 1).view() },
		nsgotest.Size(240, 320),
		nsgotest.Storage(map[string]string{recordsKey: `{"version":1,"data":{"best":12,"games":5}}`}))
	app.Find("title")
	app.Find("text:Best: 12")
}
