package cutscene_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/cutscene"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const (
	introURI = "asset:cutscenes/intro.mp4"
	musicURI = "asset:music/theme.m4a"
	video    = "id:cutscene"
)

var (
	foreground = core.Environment{Audio: true}
	background = core.Environment{Audio: true, Lifecycle: core.LifecycleBackground}
)

// rig is a game with a loop that counts its updates, a mixer playing music in "music", and a cutscene player over its view.
type rig struct {
	app     *nsgotest.App
	loop    *game.Loop
	mixer   *sound.Mixer
	music   *sound.Music
	player  *cutscene.Player
	scenes  *scene.Manager
	updates int
	results []cutscene.Result
}

func start(t *testing.T, opts cutscene.Options) *rig {
	t.Helper()
	r := &rig{}
	r.app = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		r.loop = &game.Loop{Update: func(time.Duration) { r.updates++ }}
		r.loop.Run(cx)
		r.mixer = sound.NewMixer(cx, "sound")
		r.music = r.mixer.Music("music")
		r.scenes = scene.NewManager(cx)
		opts.Loop, opts.Mixer = r.loop, r.mixer
		r.player = cutscene.NewPlayer(cx, opts)
		return core.View{Class: "flex-1", Children: []core.Node{
			core.View{Class: "flex-1", TestID: "game"},
			r.player.View(),
		}}
	}, nsgotest.Environment(foreground), func(a *nsgotest.App) {
		a.AudioDurations = map[string]time.Duration{musicURI: time.Minute}
	})
	r.music.Play(audio.Source{URI: musicURI}, 0)
	r.app.Settle()
	return r
}

// done records how a cutscene ended.
func (r *rig) done(res cutscene.Result) { r.results = append(r.results, res) }

func (r *rig) play(t *testing.T, c cutscene.Cutscene) {
	t.Helper()
	if err := r.player.Play(c, r.done); err != nil {
		t.Fatal(err)
	}
	r.app.Settle()
}

// expectRunning checks the game runs, its loop updating and its music playing, with no video over it, or that it is paused under one.
func (r *rig) expectRunning(t *testing.T, running bool) {
	t.Helper()
	r.app.Settle()
	before := r.updates
	r.app.Advance(100 * time.Millisecond)
	if updated := r.updates > before; updated != running {
		t.Fatalf("the loop updated %d times in 100 ms, want it running %v", r.updates-before, running)
	}
	if p := r.app.Players[0]; p.Playing != running {
		t.Fatalf("the music playing %v, want %v", p.Playing, running)
	}
	if shown := r.app.Exists(video); shown == running {
		t.Fatalf("the video shown %v with the game running %v", shown, running)
	}
	if r.player.Playing() == running || r.mixer.Suspended() == running || r.loop.Paused == running {
		t.Fatalf("playing %v, mixer suspended %v, loop paused %v; want all %v", r.player.Playing(), r.mixer.Suspended(), r.loop.Paused, !running)
	}
}

func (r *rig) expectResults(t *testing.T, want ...cutscene.Result) {
	t.Helper()
	r.app.Settle()
	if len(r.results) != len(want) {
		t.Fatalf("results %+v, want %+v", r.results, want)
	}
	for i := range want {
		got := r.results[i]
		if got.Reason != want[i].Reason || got.Position != want[i].Position || (got.Err == nil) != (want[i].Err == nil) {
			t.Fatalf("result %d is %+v, want %+v", i, got, want[i])
		}
	}
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

func TestACutscenePausesTheGameUntilItEnds(t *testing.T) {
	r := start(t, cutscene.Options{})
	r.expectRunning(t, true)

	r.play(t, cutscene.Cutscene{Source: introURI, Fit: cutscene.FitCover})
	r.expectRunning(t, false)
	v := r.app.Find(video)
	if v.Video.Source != introURI || v.Video.Paused || v.Video.Muted || v.Video.ResizeMode != core.Cover || v.Video.Repeat || v.Video.Controls {
		t.Fatalf("video %+v, want %s playing, covering the screen, once and without controls", v.Video, introURI)
	}

	r.app.Play(video, core.VideoInfo{Duration: 3 * time.Second})
	r.app.Progress(video, time.Second, 3*time.Second)
	r.expectRunning(t, false)
	r.expectResults(t)
	r.app.Progress(video, 3*time.Second, 3*time.Second)
	r.expectResults(t, cutscene.Result{Reason: cutscene.Finished, Position: 3 * time.Second})
	r.expectRunning(t, true)
}

func TestTheLoopStaysPausedIfItWasBefore(t *testing.T) {
	r := start(t, cutscene.Options{})
	r.loop.SetPaused(true)
	r.play(t, cutscene.Cutscene{Source: introURI})
	r.player.Stop()
	if !r.loop.Paused {
		t.Fatal("the loop was unpaused after the cutscene, though the game had paused it")
	}
	r.expectResults(t, cutscene.Result{Reason: cutscene.Stopped})
}

func TestAnUnskippableCutsceneIgnoresSkipInput(t *testing.T) {
	r := start(t, cutscene.Options{})
	r.play(t, cutscene.Cutscene{Source: introURI})
	if r.player.CanSkip() {
		t.Fatal("an unskippable cutscene can be skipped")
	}
	r.app.Press(video)
	r.app.Key("enter")
	r.app.Key("space")
	r.app.Key("escape")
	if r.player.Skip() {
		t.Fatal("Skip skipped an unskippable cutscene")
	}
	r.expectResults(t)
	r.expectRunning(t, false)
	if !r.player.Stop() {
		t.Fatal("Stop did not stop the cutscene")
	}
	r.expectResults(t, cutscene.Result{Reason: cutscene.Stopped})
	r.expectRunning(t, true)
	if r.player.Stop() || r.player.Skip() {
		t.Fatal("Stop or Skip did something with no cutscene playing")
	}
}

func TestSkipInputSkipsASkippableCutscene(t *testing.T) {
	for name, skip := range map[string]func(r *rig){
		"press":  func(r *rig) { r.app.Press(video) },
		"enter":  func(r *rig) { r.app.Key("enter") },
		"space":  func(r *rig) { r.app.Key("space") },
		"back":   func(r *rig) { r.app.Key("escape") },
		"method": func(r *rig) { r.player.Skip() },
	} {
		t.Run(name, func(t *testing.T) {
			r := start(t, cutscene.Options{})
			r.play(t, cutscene.Cutscene{Source: introURI, Skippable: true})
			if !r.player.CanSkip() {
				t.Fatal("a skippable cutscene cannot be skipped")
			}
			r.app.Play(video, core.VideoInfo{Duration: 3 * time.Second})
			r.app.Progress(video, 500*time.Millisecond, 3*time.Second)
			skip(r)
			r.expectResults(t, cutscene.Result{Reason: cutscene.Skipped, Position: 500 * time.Millisecond})
			r.expectRunning(t, true)
		})
	}
}

func TestSkipAfterHoldsSkipsBackUntilThePictureGetsThere(t *testing.T) {
	r := start(t, cutscene.Options{})
	r.play(t, cutscene.Cutscene{Source: introURI, Skippable: true, SkipAfter: cutscene.Duration(2 * time.Second)})
	r.app.Play(video, core.VideoInfo{Duration: 5 * time.Second})
	r.app.Progress(video, 1750*time.Millisecond, 5*time.Second)
	r.app.Press(video)
	r.app.Key("enter")
	if r.player.CanSkip() || r.player.Skip() {
		t.Fatal("the cutscene could be skipped before skipAfter")
	}
	r.expectResults(t)
	r.app.Progress(video, 2*time.Second, 5*time.Second)
	if !r.player.CanSkip() {
		t.Fatal("the cutscene cannot be skipped at skipAfter")
	}
	r.app.Key("enter")
	r.expectResults(t, cutscene.Result{Reason: cutscene.Skipped, Position: 2 * time.Second})
}

func TestSkipKeysCanBeChosen(t *testing.T) {
	r := start(t, cutscene.Options{SkipKeys: []string{"x"}})
	r.play(t, cutscene.Cutscene{Source: introURI, Skippable: true})
	r.app.Key("enter")
	r.app.Key("space")
	r.expectResults(t)
	r.app.Key("x")
	r.expectResults(t, cutscene.Result{Reason: cutscene.Skipped})

	r = start(t, cutscene.Options{SkipKeys: []string{}})
	r.play(t, cutscene.Cutscene{Source: introURI, Skippable: true})
	r.app.Key("enter")
	r.expectResults(t)
	r.app.Press(video)
	r.expectResults(t, cutscene.Result{Reason: cutscene.Skipped})
}

func TestAFailedVideoEndsTheCutscene(t *testing.T) {
	r := start(t, cutscene.Options{})
	r.play(t, cutscene.Cutscene{Source: "asset:missing.mp4"})
	r.app.Report(nsgotest.Event{Kind: nsgotest.EventError, ID: r.app.Find(video).ID, Text: "no such file"})
	r.expectResults(t, cutscene.Result{Reason: cutscene.Failed, Err: errors.New("no such file")})
	var me *core.MediaError
	if !errors.As(r.results[0].Err, &me) || me.Message != "no such file" {
		t.Fatalf("error %v, want the platform's *core.MediaError", r.results[0].Err)
	}
	r.expectRunning(t, true)
}

func TestTheVideoPlaysAtTheMixersVolumes(t *testing.T) {
	r := start(t, cutscene.Options{})
	r.mixer.SetMaster(0.5)
	r.mixer.SetVolume("cutscenes", 0.5)
	r.play(t, cutscene.Cutscene{Source: introURI, Group: "cutscenes"})
	if v := r.app.Find(video).Video; !near(v.Volume, 0.25) || v.Muted {
		t.Fatalf("volume %v, muted %v; want 0.25 and not muted", v.Volume, v.Muted)
	}
	// A settings menu over the cutscene changes it as it plays.
	r.mixer.SetVolume("cutscenes", 1)
	r.app.Settle()
	if v := r.app.Find(video).Video; !near(v.Volume, 0.5) || v.Muted {
		t.Fatalf("volume %v, muted %v; want 0.5 and not muted", v.Volume, v.Muted)
	}
	// core.Video plays a volume of 0 at full volume, so a silent group mutes it.
	r.mixer.SetVolume("cutscenes", 0)
	r.app.Settle()
	if v := r.app.Find(video).Video; !v.Muted {
		t.Fatalf("video %+v not muted at a group volume of 0", v)
	}
	r.player.Stop()

	// With no group, the master volume alone.
	r.mixer.SetVolume("cutscenes", 0.1)
	r.play(t, cutscene.Cutscene{Source: introURI})
	if v := r.app.Find(video).Video; !near(v.Volume, 0.5) || v.Muted {
		t.Fatalf("volume %v, muted %v; want 0.5 and not muted", v.Volume, v.Muted)
	}
}

func TestWithoutAMixerTheVideoPlaysAtFullVolume(t *testing.T) {
	r := &rig{}
	r.app = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		r.player = cutscene.NewPlayer(cx, cutscene.Options{TestID: "movie"})
		return core.View{Class: "flex-1", Children: []core.Node{r.player.View()}}
	})
	r.play(t, cutscene.Cutscene{Source: introURI, Skippable: true})
	if v := r.app.Find("id:movie").Video; v.Volume != 1 || v.Muted {
		t.Fatalf("volume %v, muted %v; want 1 and not muted", v.Volume, v.Muted)
	}
	r.app.Press("id:movie")
	r.expectResults(t, cutscene.Result{Reason: cutscene.Skipped})
	if r.app.Exists("id:movie") {
		t.Fatal("the video still shows after it was skipped")
	}
}

func TestTheVideoPausesInTheBackground(t *testing.T) {
	r := start(t, cutscene.Options{})
	r.play(t, cutscene.Cutscene{Source: introURI})
	r.app.SetEnvironment(background)
	r.app.PauseVideo(video, core.VideoPauseInterruption)
	r.app.Settle()
	if !r.app.Find(video).Video.Paused {
		t.Fatal("the video plays on in the background")
	}
	r.app.SetEnvironment(foreground)
	r.app.Settle()
	if r.app.Find(video).Video.Paused {
		t.Fatal("the video stays paused as the app returns")
	}
	// The game stays paused under it throughout.
	r.expectRunning(t, false)
}

func TestInputPlaysOnAVideoThePlatformPaused(t *testing.T) {
	r := start(t, cutscene.Options{})
	r.play(t, cutscene.Cutscene{Source: introURI, Skippable: true})
	r.app.PauseVideo(video, core.VideoPauseRouteLost)
	r.app.Settle()
	if !r.app.Find(video).Video.Paused {
		t.Fatal("the video is not held paused after the platform paused it")
	}
	r.app.Press(video)
	r.app.Settle()
	if r.app.Find(video).Video.Paused {
		t.Fatal("a press did not play the paused video on")
	}
	r.expectResults(t)
	r.app.Press(video)
	r.expectResults(t, cutscene.Result{Reason: cutscene.Skipped})

	// The platform playing it on itself releases the hold too.
	r.play(t, cutscene.Cutscene{Source: introURI})
	r.app.PauseVideo(video, core.VideoPauseControls)
	r.app.PlayVideo(video)
	r.app.Settle()
	if r.app.Find(video).Video.Paused {
		t.Fatal("the video stays paused after the platform played it")
	}
}

func TestPlayingAnotherCutsceneStopsTheFirst(t *testing.T) {
	r := start(t, cutscene.Options{})
	r.play(t, cutscene.Cutscene{Source: introURI})
	second := "asset:cutscenes/second.mp4"
	r.play(t, cutscene.Cutscene{Source: second})
	r.expectResults(t, cutscene.Result{Reason: cutscene.Stopped})
	if src := r.app.Find(video).Video.Source; src != second {
		t.Fatalf("video %s, want %s", src, second)
	}
	r.app.Play(video, core.VideoInfo{Duration: time.Second})
	r.app.Progress(video, time.Second, time.Second)
	r.expectResults(t, cutscene.Result{Reason: cutscene.Stopped}, cutscene.Result{Reason: cutscene.Finished, Position: time.Second})
	// The loop and the mixer are back as before the first.
	r.expectRunning(t, true)
}

func TestDoneMayChainCutscenes(t *testing.T) {
	r := start(t, cutscene.Options{})
	second := "asset:cutscenes/second.mp4"
	if err := r.player.Play(cutscene.Cutscene{Source: introURI}, func(res cutscene.Result) {
		r.done(res)
		if err := r.player.Play(cutscene.Cutscene{Source: second}, r.done); err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	r.app.Settle()
	r.app.Progress(video, time.Second, time.Second)
	r.expectResults(t, cutscene.Result{Reason: cutscene.Finished, Position: time.Second})
	if src := r.app.Find(video).Video.Source; src != second {
		t.Fatalf("video %s, want %s", src, second)
	}
	r.expectRunning(t, false)
	r.app.Progress(video, time.Second, time.Second)
	r.expectRunning(t, true)
}

func TestDoneMayChainFromAStoppedCutscene(t *testing.T) {
	r := start(t, cutscene.Options{})
	second, third := "asset:cutscenes/second.mp4", "asset:cutscenes/third.mp4"
	if err := r.player.Play(cutscene.Cutscene{Source: introURI}, func(res cutscene.Result) {
		r.done(res)
		if err := r.player.Play(cutscene.Cutscene{Source: second}, r.done); err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	r.app.Settle()
	// Playing over the first stops it, and its done plays the second in place of the third.
	if err := r.player.Play(cutscene.Cutscene{Source: third}, r.done); !errors.Is(err, cutscene.ErrReplaced) {
		t.Fatalf("Play over a chaining cutscene returned %v, want ErrReplaced", err)
	}
	r.app.Settle()
	r.expectResults(t, cutscene.Result{Reason: cutscene.Stopped})
	if src := r.app.Find(video).Video.Source; src != second {
		t.Fatalf("video %s, want %s", src, second)
	}
	r.expectRunning(t, false)
	r.app.Progress(video, time.Second, time.Second)
	r.expectResults(t, cutscene.Result{Reason: cutscene.Stopped}, cutscene.Result{Reason: cutscene.Finished, Position: time.Second})
	r.expectRunning(t, true)
}

func TestLosingFocusKeepsAVideoThePlatformPausedPaused(t *testing.T) {
	r := start(t, cutscene.Options{})
	r.play(t, cutscene.Cutscene{Source: introURI, Skippable: true})
	r.app.PauseVideo(video, core.VideoPauseRouteLost)
	r.app.Settle()
	r.app.SetEnvironment(core.Environment{Audio: true, Lifecycle: core.LifecycleInactive})
	r.app.Settle()
	if !r.app.Find(video).Video.Paused {
		t.Fatal("the video played on as the window lost focus, after the platform paused it")
	}
	r.app.SetEnvironment(foreground)
	r.app.Settle()
	if !r.app.Find(video).Video.Paused {
		t.Fatal("the video played on as the window got focus back, after the platform paused it")
	}
	r.app.Press(video)
	r.app.Settle()
	if r.app.Find(video).Video.Paused {
		t.Fatal("a press did not play the paused video on")
	}
	r.expectResults(t)
}

func TestPlayRefusesABadCutscene(t *testing.T) {
	r := start(t, cutscene.Options{})
	for _, c := range []cutscene.Cutscene{{}, {Source: introURI, Fit: "zoom"}, {Source: introURI, SkipAfter: -1}} {
		if err := r.player.Play(c, r.done); err == nil {
			t.Fatalf("played %+v", c)
		}
		if _, err := r.player.Scene(c, r.done); err == nil {
			t.Fatalf("made a scene of %+v", c)
		}
	}
	r.expectRunning(t, true)
}

func TestACutsceneSceneSwitchesOnWhenItEnds(t *testing.T) {
	r := start(t, cutscene.Options{})
	entered := 0
	level := &scene.Controller{Enter: func(*core.Scope) { entered++ }}
	intro, err := r.player.Scene(cutscene.Cutscene{Source: introURI, Skippable: true}, func(res cutscene.Result) {
		r.done(res)
		r.scenes.Switch(level, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	r.scenes.Switch(intro, nil)
	r.app.Settle()
	r.expectRunning(t, false)
	r.app.Key("space")
	r.expectResults(t, cutscene.Result{Reason: cutscene.Skipped})
	if entered != 1 || r.scenes.Current() != level {
		t.Fatalf("entered the level %d times, current scene %p; want once and %p", entered, r.scenes.Current(), level)
	}
	r.expectRunning(t, true)
}

func TestLeavingACutsceneSceneStopsItQuietly(t *testing.T) {
	r := start(t, cutscene.Options{})
	intro, err := r.player.Scene(cutscene.Cutscene{Source: introURI}, r.done)
	if err != nil {
		t.Fatal(err)
	}
	r.scenes.Switch(intro, nil)
	r.app.Settle()
	r.expectRunning(t, false)
	r.scenes.Switch(&scene.Controller{}, nil)
	r.expectResults(t)
	r.expectRunning(t, true)
}

func TestCrossfadingBetweenCutsceneScenesStopsTheFirstQuietly(t *testing.T) {
	r := start(t, cutscene.Options{})
	var first, second []cutscene.Result
	a, err := r.player.Scene(cutscene.Cutscene{Source: introURI}, func(res cutscene.Result) { first = append(first, res) })
	if err != nil {
		t.Fatal(err)
	}
	other := "asset:cutscenes/second.mp4"
	b, err := r.player.Scene(cutscene.Cutscene{Source: other}, func(res cutscene.Result) { second = append(second, res) })
	if err != nil {
		t.Fatal(err)
	}
	r.scenes.Switch(a, nil)
	r.app.Settle()
	r.scenes.Switch(b, scene.Crossfade(500*time.Millisecond))
	r.app.Settle()
	r.app.Advance(time.Second)
	r.app.Settle()
	if r.scenes.Transitioning() {
		t.Fatal("the crossfade did not finish")
	}
	if len(first) != 0 {
		t.Fatalf("the first scene's done got %+v, want nothing as its scene was left", first)
	}
	if src := r.app.Find(video).Video.Source; src != other {
		t.Fatalf("video %s, want %s", src, other)
	}
	r.expectRunning(t, false)
	r.app.Progress(video, time.Second, time.Second)
	r.app.Settle()
	if len(second) != 1 || second[0].Reason != cutscene.Finished {
		t.Fatalf("the second scene's done got %+v, want it finished", second)
	}
	r.expectRunning(t, true)
}

func TestReasonsHaveNames(t *testing.T) {
	for r, want := range map[cutscene.Reason]string{cutscene.Finished: "finished", cutscene.Skipped: "skipped", cutscene.Stopped: "stopped", cutscene.Failed: "failed", 9: "unknown"} {
		if r.String() != want {
			t.Errorf("%d is %q, want %q", r, r.String(), want)
		}
	}
}
