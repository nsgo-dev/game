package cutscene_test

import (
	"math"
	"os"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/cutscene"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func floor1Scene(t *testing.T) *sceneformat.BuiltScene {
	t.Helper()
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		VirtualWidth:  640,
		VirtualHeight: 360,
		Camera:        &sceneformat.CameraDoc{X: 0, Y: 0},
		Layers: []sceneformat.LayerDoc{
			{
				Name: "world",
				Objects: []sceneformat.ObjectDoc{
					{ID: "door", X: 30, Y: 222},
					{ID: "knight", X: 60, Y: 255},
					{ID: "slime_01", X: 420, Y: -40},
				},
			},
		},
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	return built
}

func floor1Catalog(t *testing.T) *l10n.Catalog {
	t.Helper()
	cat := l10n.NewCatalog("en")
	cat.Set("en", "intro.knight.01", "Another floor. Another locked door.")
	cat.Set("en", "intro.knight.02", "The lever has to be down here somewhere.")
	cat.Set("zh", "intro.knight.01", "又一层。又一扇锁着的门。")
	cat.Set("zh", "intro.knight.02", "拉杆一定就在这下面。")
	return cat
}

func loadFloor1Fixture(t *testing.T) cutscene.Cutscene {
	t.Helper()
	data, err := os.ReadFile("testdata/floor1_intro.cutscene.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c, err := cutscene.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func nearF32(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-2 }

// Acceptance: A fixture cutscene moves an actor and the camera to the positions its keys give at chosen times in a test.
func TestFloor1FixtureMovesActorsAndCameraDeterministically(t *testing.T) {
	c := loadFloor1Fixture(t)
	sc := floor1Scene(t)
	cat := floor1Catalog(t)

	var (
		states    []string
		dialogues []cutscene.DialogueLine
		sounds    []string
		musics    []string
		stopped   []string
		results   []cutscene.Result
	)

	tl, err := cutscene.NewTimelinePlayer(c, cutscene.TimelineTarget{
		Scene:   sc,
		Catalog: cat,
		Locale:  "en",
		OnActorState: func(actorID, state string) {
			states = append(states, actorID+":"+state)
		},
		OnDialogue: func(line cutscene.DialogueLine) {
			dialogues = append(dialogues, line)
		},
		OnSound: func(clip cutscene.Clip, group string, vol float32) {
			sounds = append(sounds, clip.ID+"@"+group+":"+clip.Source)
		},
		OnMusic: func(clip cutscene.Clip, group string, vol float32) {
			musics = append(musics, clip.ID+"@"+group+":"+clip.Source)
		},
		OnMusicStop: func(clip cutscene.Clip, _ time.Duration) {
			stopped = append(stopped, clip.ID)
		},
		OnDone: func(r cutscene.Result) {
			results = append(results, r)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	tl.Play()

	stepTo := func(target time.Duration) {
		t.Helper()
		const step = 100 * time.Millisecond
		for tl.Time() < target {
			dt := min(step, target-tl.Time())
			tl.Update(dt)
		}
	}

	// t = 0.5s: camera holds on door (30, 222); knight still at (60, 255); door_slam fired at 200ms.
	stepTo(500 * time.Millisecond)
	if !nearF32(sc.Camera.X, 30) || !nearF32(sc.Camera.Y, 222) {
		t.Fatalf("t=0.5s camera = (%v, %v), want (30, 222)", sc.Camera.X, sc.Camera.Y)
	}
	if !nearF32(sc.Object("knight").Doc.X, 60) {
		t.Fatalf("t=0.5s knight.X = %v, want 60", sc.Object("knight").Doc.X)
	}
	if len(sounds) != 1 || sounds[0] != "sx1@sfx:audio/door_slam.wav" {
		t.Fatalf("t=0.5s sounds = %v, want sx1", sounds)
	}

	// t = 2.2s: knight is halfway through walk (1.0s..3.4s, easeOut from 60 to 220).
	stepTo(2200 * time.Millisecond)
	wantKnightX := float32(60 + (220-60)*cutscene.EaseOut.At(0.5))
	if !nearF32(sc.Object("knight").Doc.X, wantKnightX) {
		t.Fatalf("t=2.2s knight.X = %v, want %v", sc.Object("knight").Doc.X, wantKnightX)
	}
	if got := tl.ActorState("knight"); got != "run" {
		t.Fatalf("t=2.2s knight state = %q, want run", got)
	}
	// Camera is 0.6s into 2.4s pan (u=0.25, easeInOut) from door (30,222) to knight(2.2s).
	panU := cutscene.EaseInOut.At(0.25)
	wantCamX := float32(30 + (float64(wantKnightX)-30)*panU)
	wantCamY := float32(222 + (255-222)*panU)
	if !nearF32(sc.Camera.X, wantCamX) || !nearF32(sc.Camera.Y, wantCamY) {
		t.Fatalf("t=2.2s camera = (%v, %v), want (%v, %v)", sc.Camera.X, sc.Camera.Y, wantCamX, wantCamY)
	}
	if len(musics) != 1 || musics[0] != "mu1@music:audio/cellar_theme.ogg" {
		t.Fatalf("t=2.2s musics = %v, want mu1", musics)
	}

	// t = 4.0s: knight finished walk at x=220 and is idle; camera finished pan and starts follow on knight (220, 255); dialogue dl1 active.
	stepTo(4000 * time.Millisecond)
	if !nearF32(sc.Object("knight").Doc.X, 220) || tl.ActorState("knight") != "idle" {
		t.Fatalf("t=4.0s knight.X = %v state=%q, want 220 idle", sc.Object("knight").Doc.X, tl.ActorState("knight"))
	}
	if !nearF32(sc.Camera.X, 220) || !nearF32(sc.Camera.Y, 255) {
		t.Fatalf("t=4.0s camera = (%v, %v), want (220, 255)", sc.Camera.X, sc.Camera.Y)
	}
	if dl := tl.ActiveDialogue(); !dl.Active || dl.ID != "intro.knight.01" || dl.Text != "Another floor. Another locked door." {
		t.Fatalf("t=4.0s dialogue = %+v", dl)
	}

	// t = 6.4s: slime is halfway through drop (6.0s..6.8s, easeIn from -40 to 276); slime_drop fired at 6.0s.
	stepTo(6400 * time.Millisecond)
	wantSlimeY := float32(-40 + (276-(-40))*cutscene.EaseIn.At(0.5))
	if !nearF32(sc.Object("slime_01").Doc.Y, wantSlimeY) {
		t.Fatalf("t=6.4s slime_01.Y = %v, want %v", sc.Object("slime_01").Doc.Y, wantSlimeY)
	}
	if len(sounds) != 2 || sounds[1] != "sx2@sfx:audio/slime_drop.wav" {
		t.Fatalf("t=6.4s sounds = %v, want sx1+sx2", sounds)
	}

	// t = 8.3s: slime has been patrolling for 1.5s at speed 40 from 420 to 520 => x = 480; dialogue dl2 active.
	stepTo(8300 * time.Millisecond)
	if !nearF32(sc.Object("slime_01").Doc.X, 480) || !nearF32(sc.Object("slime_01").Doc.Y, 276) {
		t.Fatalf("t=8.3s slime_01 = (%v, %v), want (480, 276)", sc.Object("slime_01").Doc.X, sc.Object("slime_01").Doc.Y)
	}
	if dl := tl.ActiveDialogue(); !dl.Active || dl.ID != "intro.knight.02" || dl.Text != "The lever has to be down here somewhere." {
		t.Fatalf("t=8.3s dialogue = %+v", dl)
	}

	// t = 10.3s: slime traveled 3.5s * 40 = 140 over 100-unit span => leg 1 (returning), x = 520 - 40 = 480.
	stepTo(10300 * time.Millisecond)
	if !nearF32(sc.Object("slime_01").Doc.X, 480) {
		t.Fatalf("t=10.3s slime_01.X = %v, want 480 (returning leg)", sc.Object("slime_01").Doc.X)
	}

	// Finish at t = 12.0s: knight reaches 600, camera follows to (600, 255), music stops, OnDone gets Finished.
	stepTo(12 * time.Second)
	if !tl.Done() || tl.Playing() {
		t.Fatalf("at 12s done=%v playing=%v, want true/false", tl.Done(), tl.Playing())
	}
	if !nearF32(sc.Object("knight").Doc.X, 600) {
		t.Fatalf("t=12s knight.X = %v, want 600", sc.Object("knight").Doc.X)
	}
	if !nearF32(sc.Camera.X, 600) || !nearF32(sc.Camera.Y, 255) {
		t.Fatalf("t=12s camera = (%v, %v), want (600, 255)", sc.Camera.X, sc.Camera.Y)
	}
	if len(stopped) != 1 || stopped[0] != "mu1" {
		t.Fatalf("stopped music = %v, want [mu1]", stopped)
	}
	if len(results) != 1 || results[0].Reason != cutscene.Finished || results[0].Position != 12*time.Second {
		t.Fatalf("results = %+v, want Finished at 12s", results)
	}

	// Seek reproduces the exact same poses without firing audio or done callbacks.
	soundsBefore := len(sounds)
	tl.Seek(2200 * time.Millisecond)
	if !nearF32(sc.Object("knight").Doc.X, wantKnightX) || !nearF32(sc.Camera.X, wantCamX) {
		t.Fatalf("Seek(2.2s) knight.X=%v cam.X=%v, want %v %v", sc.Object("knight").Doc.X, sc.Camera.X, wantKnightX, wantCamX)
	}
	if len(sounds) != soundsBefore || len(results) != 1 {
		t.Fatalf("Seek fired audio or done callbacks")
	}

	// Reset restores initial positions cleanly.
	tl.Reset()
	if !nearF32(sc.Object("knight").Doc.X, 60) || !nearF32(sc.Object("slime_01").Doc.Y, -40) || !nearF32(sc.Camera.X, 0) {
		t.Fatalf("Reset did not restore initial poses: knight.X=%v slime.Y=%v cam.X=%v", sc.Object("knight").Doc.X, sc.Object("slime_01").Doc.Y, sc.Camera.X)
	}
}

func TestTimelinePlayerValidatesMissingSceneObject(t *testing.T) {
	c := loadFloor1Fixture(t)
	emptyDoc := &sceneformat.SceneDoc{Version: sceneformat.CurrentVersion}
	if _, err := cutscene.NewTimelinePlayer(c, cutscene.TimelineTarget{Doc: emptyDoc}); err == nil {
		t.Fatal("expected error for missing object in scene")
	}
}

func TestPlayerPlayTimelineOnFixedLoopAndSkip(t *testing.T) {
	c := loadFloor1Fixture(t)
	sc := floor1Scene(t)
	cat := floor1Catalog(t)

	var (
		player  *cutscene.Player
		loop    *game.Loop
		mixer   *sound.Mixer
		results []cutscene.Result
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mixer = sound.NewMixer(cx, "sound")
		player = cutscene.NewPlayer(cx, cutscene.Options{Mixer: mixer})
		loop = &game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				player.Step(dt)
			},
		}
		loop.Run(cx)
		return core.View{Class: "flex-1", Children: []core.Node{
			core.View{Class: "flex-1", TestID: "game"},
			player.View(),
		}}
	}, nsgotest.Environment(foreground), func(a *nsgotest.App) {
		a.AudioDurations = map[string]time.Duration{"audio/cellar_theme.ogg": time.Minute}
	})

	if _, err := player.PlayTimeline(c, cutscene.TimelineTarget{
		Scene:   sc,
		Catalog: cat,
		Locale:  "zh",
	}, func(r cutscene.Result) {
		results = append(results, r)
	}); err != nil {
		t.Fatal(err)
	}
	app.Settle()

	if !player.Playing() {
		t.Fatal("player.Playing() is false")
	}
	if loop.Paused {
		t.Fatal("timeline cutscene must not pause the game loop")
	}
	if player.CanSkip() {
		t.Fatal("cutscene should not be skippable before skipAfter (1s)")
	}
	// Try skipping before skipAfter: ignored.
	app.Key("space")
	app.Settle()
	if !player.Playing() {
		t.Fatal("cutscene skipped before skipAfter")
	}

	// Advance 4s on fixed loop steps -> knight moved to 220, dialogue active in zh, music playing.
	app.Advance(4 * time.Second)
	app.Settle()
	if !player.CanSkip() {
		t.Fatal("cutscene should be skippable after skipAfter")
	}
	if !nearF32(sc.Object("knight").Doc.X, 220) {
		t.Fatalf("knight.X = %v at 4s, want 220", sc.Object("knight").Doc.X)
	}
	if dl := player.Dialogue(); !dl.Active || dl.ID != "intro.knight.01" || dl.Text != "又一层。又一扇锁着的门。" {
		t.Fatalf("player.Dialogue() = %+v", dl)
	}

	// Skip with space key, just like video cutscenes.
	app.Key("space")
	app.Settle()
	if player.Playing() {
		t.Fatal("cutscene still playing after skip key")
	}
	if len(results) != 1 || results[0].Reason != cutscene.Skipped || results[0].Position < 3500*time.Millisecond {
		t.Fatalf("results = %+v, want Skipped near 4s", results)
	}
	if player.Dialogue().Active {
		t.Fatal("dialogue remained active after skip")
	}
}

func TestPlayerTimelineScene(t *testing.T) {
	c := loadFloor1Fixture(t)
	sc := floor1Scene(t)

	var (
		player  *cutscene.Player
		scenes  *scene.Manager
		results []cutscene.Result
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		player = cutscene.NewPlayer(cx, cutscene.Options{})
		scenes = scene.NewManager(cx)
		loop := &game.Loop{Rate: 60, Update: player.Step}
		loop.Run(cx)
		return core.View{Class: "flex-1", Children: []core.Node{player.View()}}
	}, nsgotest.Environment(foreground))

	ctrl, err := player.TimelineScene(c, cutscene.TimelineTarget{Scene: sc}, func(r cutscene.Result) {
		results = append(results, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	scenes.Switch(ctrl, nil)
	app.Settle()
	if !player.Playing() {
		t.Fatal("TimelineScene did not start playing on enter")
	}
	// Leaving early stops quietly without calling done.
	scenes.Switch(&scene.Controller{}, nil)
	app.Settle()
	if player.Playing() {
		t.Fatal("TimelineScene kept playing after exit")
	}
	if len(results) != 0 {
		t.Fatalf("done called on early exit: %+v", results)
	}
}

func TestTimelineEdgeCasesSeekPauseStopResetAndPatrol(t *testing.T) {
	toX := float32(1e-30)
	fromX := float32(0)
	extremeSpeed := float32(math.MaxFloat32)

	c := cutscene.Cutscene{
		Version: cutscene.CurrentVersion,
		Kind:    cutscene.KindTimeline,
		Length:  cutscene.Duration(5 * time.Second),
		Tracks: []cutscene.Track{
			{
				Kind:   cutscene.TrackActor,
				Target: "alpha",
				Clips: []cutscene.Clip{
					{
						Start:    0,
						Duration: cutscene.Duration(4 * time.Second),
						Action:   cutscene.ActionState,
						State:    "idle",
					},
				},
			},
			{
				Kind:   cutscene.TrackActor,
				Target: "beta",
				Clips: []cutscene.Clip{
					{
						Start:    0,
						Duration: cutscene.Duration(4 * time.Second),
						Action:   cutscene.ActionPatrol,
						FromX:    &fromX,
						ToX:      &toX,
						Speed:    &extremeSpeed,
						State:    "patrol",
					},
				},
			},
			{
				Kind:   cutscene.TrackActor,
				Target: "gamma",
				Clips: []cutscene.Clip{
					{
						Start:    0,
						Duration: cutscene.Duration(4 * time.Second),
						Action:   cutscene.ActionState,
						State:    "walk",
					},
				},
			},
			{
				Kind: cutscene.TrackSound,
				Clips: []cutscene.Clip{
					{
						ID:       "s0",
						Start:    0,
						Duration: 0,
						Source:   "audio/zero.wav",
					},
					{
						ID:       "s3",
						Start:    cutscene.Duration(3 * time.Second),
						Duration: 0,
						Source:   "audio/three.wav",
					},
				},
			},
			{
				Kind: cutscene.TrackMusic,
				Clips: []cutscene.Clip{
					{
						ID:       "m0",
						Start:    0,
						Duration: cutscene.Duration(5 * time.Second),
						Source:   "audio/bgm.ogg",
					},
				},
			},
		},
	}

	makeDoc := func() *sceneformat.SceneDoc {
		return &sceneformat.SceneDoc{
			Version: sceneformat.CurrentVersion,
			Layers: []sceneformat.LayerDoc{
				{
					Name: "world",
					Objects: []sceneformat.ObjectDoc{
						{ID: "alpha", X: 1, Y: 2},
						{ID: "beta", X: 3, Y: 4},
						{ID: "gamma", X: 5, Y: 6},
					},
				},
			},
		}
	}

	// 1. Seek past start:0s then Play+Update must NOT fire start:0s audio clips, while still firing later clips.
	var sounds, musics, stopped []string
	var results []cutscene.Result
	tl, err := cutscene.NewTimelinePlayer(c, cutscene.TimelineTarget{
		Doc:         makeDoc(),
		OnSound:     func(cl cutscene.Clip, _ string, _ float32) { sounds = append(sounds, cl.ID) },
		OnMusic:     func(cl cutscene.Clip, _ string, _ float32) { musics = append(musics, cl.ID) },
		OnMusicStop: func(cl cutscene.Clip, _ time.Duration) { stopped = append(stopped, cl.ID) },
		OnDone:      func(r cutscene.Result) { results = append(results, r) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if tl.Stop() {
		t.Fatal("Stop before Play returned true")
	}
	tl.Seek(2 * time.Second)
	tl.Play()
	tl.Update(500 * time.Millisecond)
	if len(sounds) != 0 || len(musics) != 0 {
		t.Fatalf("Seek(2s)+Play+Update(0.5s) fired start:0s audio: sounds=%v musics=%v", sounds, musics)
	}
	// Extreme patrol speed must remain finite.
	bx, by, ok := tl.ActorPosition("beta")
	if !ok || math.IsNaN(float64(bx)) || math.IsInf(float64(bx), 0) || math.IsNaN(float64(by)) || math.IsInf(float64(by), 0) {
		t.Fatalf("extreme patrol produced non-finite position (%v, %v, %v)", bx, by, ok)
	}
	tl.Update(600 * time.Millisecond)
	if len(sounds) != 1 || sounds[0] != "s3" {
		t.Fatalf("crossing 3s sounds = %v, want [s3]", sounds)
	}

	// 2. Play -> Update (music starts) -> Pause -> Stop must clean up active music and emit Stopped.
	tl.Reset()
	sounds, musics, stopped, results = nil, nil, nil, nil
	tl.Play()
	tl.Update(500 * time.Millisecond)
	if len(musics) != 1 || musics[0] != "m0" {
		t.Fatalf("expected m0 music to start, got %v", musics)
	}
	tl.Pause()
	if !tl.Stop() {
		t.Fatal("Stop while paused mid-cutscene returned false, want true")
	}
	if len(stopped) != 1 || stopped[0] != "m0" {
		t.Fatalf("Stop while paused did not stop music: stopped=%v", stopped)
	}
	if len(results) != 1 || results[0].Reason != cutscene.Stopped {
		t.Fatalf("Stop while paused results = %+v, want Stopped", results)
	}
	if !tl.Done() || tl.Stop() {
		t.Fatal("expected Done()==true and second Stop()==false")
	}

	// 3. Reset clears actor states in deterministic track order across repeated runs.
	for run := range 20 {
		var order []string
		tlDet, err := cutscene.NewTimelinePlayer(c, cutscene.TimelineTarget{
			Doc: makeDoc(),
			OnActorState: func(actorID, state string) {
				if state == "" {
					order = append(order, actorID)
				}
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		tlDet.Play()
		tlDet.Update(100 * time.Millisecond)
		order = nil
		tlDet.Reset()
		want := []string{"alpha", "beta", "gamma"}
		if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
			t.Fatalf("run %d: Reset OnActorState clear order = %v, want %v", run, order, want)
		}
	}
}

func TestTimelineCamera3DTarget(t *testing.T) {
	camera := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 2}, 1, 1, .1, 100)
	p, err := cutscene.NewTimelinePlayer(cutscene.Cutscene{Version: 2, Kind: cutscene.KindTimeline, Length: cutscene.Duration(time.Second)}, cutscene.TimelineTarget{Scene3D: camera})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := p.Camera3D()
	if !ok || got.Pos.X != 2 {
		t.Fatalf("camera = %+v, %v", got, ok)
	}
	got.Pos.X = 8
	p.SetCamera3D(got)
	if camera.Pos.X != 8 {
		t.Fatalf("target camera = %+v", camera.Pos)
	}
}
