package sound_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const (
	uiClickURI = "asset:sounds/ui_click.wav"
	longSFXURI = "asset:sounds/long_sfx.wav"
	uiMusicURI = "asset:music/menu.m4a"
)

type pauseHarness struct {
	app     *nsgotest.App
	mixer   *sound.Mixer
	sfx     *sound.Effect
	longSFX *sound.Effect
	uiFX    *sound.Effect
	music   *sound.Music
	uiMusic *sound.Music
	loop    *game.Loop
	updates int
	draws   int
}

func startPauseHarness(t *testing.T, loop *game.Loop, opts ...nsgotest.Option) *pauseHarness {
	t.Helper()
	h := &pauseHarness{loop: loop}
	opts = append([]nsgotest.Option{
		nsgotest.Environment(playsAudio),
		func(a *nsgotest.App) {
			a.AudioDurations = map[string]time.Duration{
				clickURI:   100 * time.Millisecond,
				longSFXURI: 500 * time.Millisecond,
				uiClickURI: 100 * time.Millisecond,
				trackA:     time.Minute,
				trackB:     time.Minute,
				uiMusicURI: time.Minute,
				introURI:   time.Second,
				loopURI:    10 * time.Second,
			}
		},
	}, opts...)
	h.app = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		h.mixer = sound.NewMixer(cx, "sound")
		audio.LoadSound(cx, audio.Source{URI: clickURI}, func(s *audio.Sound, err error) {
			if err != nil {
				t.Errorf("load sfx: %v", err)
				return
			}
			h.sfx = h.mixer.Effect(s, "effects", 2)
		})
		audio.LoadSound(cx, audio.Source{URI: longSFXURI}, func(s *audio.Sound, err error) {
			if err != nil {
				t.Errorf("load long sfx: %v", err)
				return
			}
			h.longSFX = h.mixer.Effect(s, "effects", 4)
		})
		audio.LoadSound(cx, audio.Source{URI: uiClickURI}, func(s *audio.Sound, err error) {
			if err != nil {
				t.Errorf("load ui click: %v", err)
				return
			}
			h.uiFX = h.mixer.Effect(s, "ui", 4)
		})
		h.music = h.mixer.Music("music")
		h.uiMusic = h.mixer.Music("ui")
		if h.loop != nil {
			h.loop.OnPause = h.mixer.SetPaused
			h.loop.Update = func(time.Duration) { h.updates++ }
			h.loop.Draw = func(float64) { h.draws++ }
			h.loop.Run(cx)
		}
		return core.View{}
	}, opts...)
	if h.sfx == nil || h.longSFX == nil || h.uiFX == nil {
		t.Fatal("failed to load test sounds")
	}
	return h
}

func (h *pauseHarness) soundByURI(t *testing.T, uri string) *nsgotest.Sound {
	t.Helper()
	h.app.Settle()
	for _, s := range h.app.Sounds {
		if s.Source.URI == uri {
			return s
		}
	}
	t.Fatalf("sound %q not found", uri)
	return nil
}

func (h *pauseHarness) playerByURI(t *testing.T, uri string) *nsgotest.AudioPlayer {
	t.Helper()
	h.app.Settle()
	for _, p := range h.app.Players {
		if p.Source.URI == uri {
			return p
		}
	}
	t.Fatalf("player %q not found", uri)
	return nil
}

func expectPB(t *testing.T, app *nsgotest.App, p *nsgotest.AudioPlayback, volume float32) {
	t.Helper()
	app.Settle()
	if p.Ended || !near(p.Volume, volume) {
		t.Fatalf("playback: ended %v, volume %v; want playing at %v", p.Ended, p.Volume, volume)
	}
}

func TestSetPausedSilencesGameplayEffectsAndPausesMusicAndResumes(t *testing.T) {
	h := startPauseHarness(t, nil)
	h.mixer.SetVolume("music", 0.8)
	h.mixer.SetVolume("effects", 1)

	// Start looping gameplay effect, a 500ms non-looping effect, and gameplay music.
	if !h.sfx.Play(audio.Voice{Volume: 0.5, Loop: true}) {
		t.Fatal("looping sfx did not start")
	}
	if !h.sfx.Play(audio.Voice{Volume: 0.8, Loop: true}) {
		t.Fatal("second looping sfx did not start")
	}
	if !h.longSFX.Play(audio.Voice{Volume: 0.6}) {
		t.Fatal("long sfx did not start")
	}
	h.music.Play(audio.Source{URI: trackA}, 0)

	sfxSound := h.soundByURI(t, clickURI)
	longSound := h.soundByURI(t, longSFXURI)
	musicPlayer := h.playerByURI(t, trackA)

	expectPB(t, h.app, sfxSound.Playbacks[0], 0.5)
	expectPB(t, h.app, sfxSound.Playbacks[1], 0.8)
	expectPB(t, h.app, longSound.Playbacks[0], 0.6)
	expectPlayer(t, musicPlayer, true, 0.8)

	// Let audio run 100ms so music moves past 0 and longSFX still has 400ms left.
	h.app.Advance(100 * time.Millisecond)
	pausedAt := musicPlayer.Position
	if pausedAt <= 0 {
		t.Fatalf("music did not advance before pause: pos=%v", pausedAt)
	}

	// Open pause menu: silence gameplay effects without stopping them, pause music at its position.
	h.mixer.SetPaused(true)
	if !h.mixer.Paused() {
		t.Fatal("mixer.Paused() = false, want true")
	}
	// Repeating SetPaused(true) is a no-op.
	h.mixer.SetPaused(true)

	expectPB(t, h.app, sfxSound.Playbacks[0], 0)
	expectPB(t, h.app, sfxSound.Playbacks[1], 0)
	expectPB(t, h.app, longSound.Playbacks[0], 0)
	if sfxSound.Stops != 0 || longSound.Stops != 0 {
		t.Fatalf("unexpected stops on pause: sfx=%d long=%d", sfxSound.Stops, longSound.Stops)
	}
	expectPlayer(t, musicPlayer, false, 0)

	// New gameplay effects drop without evicting paused playbacks.
	if h.sfx.Play(audio.Voice{}) || len(sfxSound.Playbacks) != 2 {
		t.Fatalf("gameplay sfx played while paused: playbacks=%d", len(sfxSound.Playbacks))
	}

	// While paused, music position stays frozen and volume edits stay silent until resume.
	h.app.Advance(100 * time.Millisecond)
	if musicPlayer.Position != pausedAt {
		t.Fatalf("music advanced while paused: %v -> %v", pausedAt, musicPlayer.Position)
	}
	h.mixer.SetVolume("effects", 0.5)
	h.mixer.SetVolume("music", 0.4)
	expectPB(t, h.app, sfxSound.Playbacks[0], 0)
	expectPlayer(t, musicPlayer, false, 0)

	// Close pause menu: live effects restore at updated volumes, music resumes where it left off.
	h.mixer.SetPaused(false)
	if h.mixer.Paused() {
		t.Fatal("mixer.Paused() = true after resume, want false")
	}
	expectPB(t, h.app, sfxSound.Playbacks[0], 0.25)
	expectPB(t, h.app, sfxSound.Playbacks[1], 0.4)
	expectPB(t, h.app, longSound.Playbacks[0], 0.3)
	expectPlayer(t, musicPlayer, true, 0.4)
	if musicPlayer.Position != pausedAt {
		t.Fatalf("music did not resume from paused position: got %v, want %v", musicPlayer.Position, pausedAt)
	}

	h.app.Advance(500 * time.Millisecond)
	if musicPlayer.Position <= pausedAt {
		t.Fatalf("music did not advance after resume: pos=%v, pausedAt=%v", musicPlayer.Position, pausedAt)
	}
	if !h.sfx.Play(audio.Voice{}) {
		t.Fatal("gameplay sfx did not play after resume")
	}
}

func TestMusicStartedOrSectionedWhilePausedWaitsForResume(t *testing.T) {
	h := startPauseHarness(t, nil)
	h.mixer.SetVolume("music", 0.7)

	// Music started while paused does not open or play until unpaused.
	h.mixer.SetPaused(true)
	h.music.Play(audio.Source{URI: trackB}, 0)
	h.app.Settle()
	if len(h.app.Players) != 0 {
		t.Fatalf("music started while paused opened %d players, want 0", len(h.app.Players))
	}
	h.mixer.SetPaused(false)
	expectPlayer(t, h.playerByURI(t, trackB), true, 0.7)

	// Sectioned music pauses during intro without prematurely handing off to loop.
	h.music.PlaySections(audio.Source{URI: introURI}, audio.Source{URI: loopURI}, 0)
	intro := h.playerByURI(t, introURI)
	loop := h.playerByURI(t, loopURI)
	expectPlayer(t, intro, true, 0.7)
	h.app.Advance(400 * time.Millisecond)

	h.mixer.SetPaused(true)
	h.app.Settle()
	expectPlayer(t, intro, false, 0)
	introAt := intro.Position
	h.app.Advance(2 * time.Second)
	if intro.Position != introAt {
		t.Fatalf("sectioned intro advanced while paused: %v -> %v", introAt, intro.Position)
	}
	if loop.Playing {
		t.Fatal("sectioned loop started while paused")
	}

	h.mixer.SetPaused(false)
	h.app.Settle()
	expectPlayer(t, intro, true, 0.7)
	h.app.Advance(700 * time.Millisecond)
	if !intro.Closed {
		t.Fatal("sectioned intro did not finish after resume")
	}
	expectPlayer(t, loop, true, 0.7)
}

func TestPauseExemptGroupsKeepPlayingWhilePaused(t *testing.T) {
	h := startPauseHarness(t, nil)
	h.mixer.SetPauseExempt("ui", true)
	if !h.mixer.PauseExempt("ui") || h.mixer.PauseExempt("effects") || h.mixer.PauseExempt("music") {
		t.Fatalf("PauseExempt flags: ui=%v effects=%v music=%v",
			h.mixer.PauseExempt("ui"), h.mixer.PauseExempt("effects"), h.mixer.PauseExempt("music"))
	}

	h.mixer.SetVolume("ui", 0.9)
	h.mixer.SetVolume("effects", 0.8)
	h.mixer.SetVolume("music", 0.6)

	h.sfx.Play(audio.Voice{Volume: 0.5, Loop: true})
	h.uiFX.Play(audio.Voice{Volume: 0.5, Loop: true})
	h.music.Play(audio.Source{URI: trackA}, 0)
	h.uiMusic.Play(audio.Source{URI: uiMusicURI}, 0)

	sfxSound := h.soundByURI(t, clickURI)
	uiSound := h.soundByURI(t, uiClickURI)
	gameMusic := h.playerByURI(t, trackA)
	menuMusic := h.playerByURI(t, uiMusicURI)

	h.app.Advance(200 * time.Millisecond)
	gameMusicPos := gameMusic.Position
	menuMusicPos := menuMusic.Position

	// Pause game: gameplay stops, UI effects and UI music keep playing.
	h.mixer.SetPaused(true)
	expectPB(t, h.app, sfxSound.Playbacks[0], 0)
	expectPlayer(t, gameMusic, false, 0)
	expectPB(t, h.app, uiSound.Playbacks[0], 0.45)
	expectPlayer(t, menuMusic, true, 0.9)

	// New UI sounds play normally while paused; gameplay sounds do not.
	if h.sfx.Play(audio.Voice{}) {
		t.Fatal("gameplay sfx played while paused")
	}
	if !h.uiFX.Play(audio.Voice{Volume: 1}) {
		t.Fatal("UI sfx did not play while paused")
	}
	h.app.Settle()
	expectPB(t, h.app, uiSound.Playbacks[1], 0.9)

	h.app.Advance(500 * time.Millisecond)
	if gameMusic.Position != gameMusicPos {
		t.Fatalf("gameplay music advanced while paused: %v -> %v", gameMusicPos, gameMusic.Position)
	}
	if menuMusic.Position <= menuMusicPos {
		t.Fatalf("UI music did not advance while paused: %v -> %v", menuMusicPos, menuMusic.Position)
	}

	// Removing exemption while paused silences UI immediately; restoring it resumes UI immediately.
	h.mixer.SetPauseExempt("ui", false)
	if h.mixer.PauseExempt("ui") {
		t.Fatal("ui remained pause-exempt after SetPauseExempt(false)")
	}
	expectPB(t, h.app, uiSound.Playbacks[0], 0)
	expectPlayer(t, menuMusic, false, 0)
	if h.uiFX.Play(audio.Voice{}) {
		t.Fatal("non-exempt ui sfx played while paused")
	}

	menuPausedPos := menuMusic.Position
	h.app.Advance(300 * time.Millisecond)
	if menuMusic.Position != menuPausedPos {
		t.Fatalf("non-exempt UI music advanced while paused: %v -> %v", menuPausedPos, menuMusic.Position)
	}

	h.mixer.SetPauseExempt("ui", true)
	expectPB(t, h.app, uiSound.Playbacks[0], 0.45)
	expectPlayer(t, menuMusic, true, 0.9)
	expectPB(t, h.app, sfxSound.Playbacks[0], 0)
	expectPlayer(t, gameMusic, false, 0)

	// Suspend silences everything including exempt UI groups; resuming Suspend keeps gameplay paused.
	resumeSuspend := h.mixer.Suspend()
	expectPB(t, h.app, uiSound.Playbacks[0], 0)
	expectPlayer(t, menuMusic, false, 0)
	if h.uiFX.Play(audio.Voice{}) {
		t.Fatal("UI sfx played while suspended")
	}
	resumeSuspend()
	expectPB(t, h.app, uiSound.Playbacks[0], 0.45)
	expectPlayer(t, menuMusic, true, 0.9)
	expectPB(t, h.app, sfxSound.Playbacks[0], 0)
	expectPlayer(t, gameMusic, false, 0)

	// Resuming game restores gameplay sounds without disrupting UI.
	h.mixer.SetPaused(false)
	expectPB(t, h.app, sfxSound.Playbacks[0], 0.4)
	expectPlayer(t, gameMusic, true, 0.6)
	expectPB(t, h.app, uiSound.Playbacks[0], 0.45)
	expectPlayer(t, menuMusic, true, 0.9)
}

func TestBackgroundPausesAndResumesInSyncWithLoop(t *testing.T) {
	loop := &game.Loop{Rate: 60}
	h := startPauseHarness(t, loop)
	h.mixer.SetPauseExempt("ui", true)
	h.mixer.SetVolume("effects", 0.8)
	h.mixer.SetVolume("music", 0.5)
	h.mixer.SetVolume("ui", 0.9)

	h.sfx.Play(audio.Voice{Volume: 0.5, Loop: true})
	h.uiFX.Play(audio.Voice{Volume: 0.5, Loop: true})
	h.music.Play(audio.Source{URI: trackA}, 0)
	h.uiMusic.Play(audio.Source{URI: uiMusicURI}, 0)

	sfxSound := h.soundByURI(t, clickURI)
	uiSound := h.soundByURI(t, uiClickURI)
	gameMusic := h.playerByURI(t, trackA)
	menuMusic := h.playerByURI(t, uiMusicURI)

	// Active gameplay: loop steps and all audio plays.
	h.app.Advance(500 * time.Millisecond)
	if h.updates != 30 || h.draws != 30 {
		t.Fatalf("active: updates=%d draws=%d, want 30 and 30", h.updates, h.draws)
	}
	expectPB(t, h.app, sfxSound.Playbacks[0], 0.4)
	expectPB(t, h.app, uiSound.Playbacks[0], 0.45)
	expectPlayer(t, gameMusic, true, 0.5)
	expectPlayer(t, menuMusic, true, 0.9)

	// Background during active gameplay pauses loop and ALL audio (including pause-exempt UI).
	bgGamePos := gameMusic.Position
	bgMenuPos := menuMusic.Position
	h.app.SetEnvironment(background)
	h.app.Settle()

	expectPB(t, h.app, sfxSound.Playbacks[0], 0)
	expectPB(t, h.app, uiSound.Playbacks[0], 0)
	expectPlayer(t, gameMusic, false, 0)
	expectPlayer(t, menuMusic, false, 0)
	if h.sfx.Play(audio.Voice{}) || h.uiFX.Play(audio.Voice{}) {
		t.Fatal("sounds played in background")
	}

	h.app.Advance(2 * time.Second)
	if h.updates != 30 || h.draws != 30 {
		t.Fatalf("background ran loop: updates=%d draws=%d, want 30 and 30", h.updates, h.draws)
	}
	if gameMusic.Position != bgGamePos || menuMusic.Position != bgMenuPos {
		t.Fatalf("music advanced in background: game %v->%v, ui %v->%v",
			bgGamePos, gameMusic.Position, bgMenuPos, menuMusic.Position)
	}

	// Returning to foreground resumes loop (no catch-up) and all audio where it paused.
	h.app.SetEnvironment(playsAudio)
	h.app.Settle()
	expectPB(t, h.app, sfxSound.Playbacks[0], 0.4)
	expectPB(t, h.app, uiSound.Playbacks[0], 0.45)
	expectPlayer(t, gameMusic, true, 0.5)
	expectPlayer(t, menuMusic, true, 0.9)

	h.app.Advance(500 * time.Millisecond)
	if h.updates != 60 || h.draws != 60 {
		t.Fatalf("after foreground resume: updates=%d draws=%d, want 60 and 60", h.updates, h.draws)
	}
	if gameMusic.Position <= bgGamePos {
		t.Fatalf("game music did not advance after foreground: %v <= %v", gameMusic.Position, bgGamePos)
	}

	// LifecycleInactive (visible, unfocused desktop window) keeps both loop and mixer running.
	h.app.SetEnvironment(core.Environment{Audio: true, Lifecycle: core.LifecycleInactive})
	h.app.Advance(500 * time.Millisecond)
	if h.updates != 90 {
		t.Fatalf("inactive paused loop: updates=%d, want 90", h.updates)
	}
	expectPlayer(t, gameMusic, true, 0.5)
	h.app.SetEnvironment(playsAudio)

	// Open pause menu through loop.SetPaused(true): gameplay stops, UI keeps playing.
	pauseGamePos := gameMusic.Position
	h.app.Post(func() { loop.SetPaused(true) })
	h.app.Settle()
	if !h.mixer.Paused() {
		t.Fatal("loop.SetPaused(true) did not pause mixer via OnPause")
	}
	expectPB(t, h.app, sfxSound.Playbacks[0], 0)
	expectPlayer(t, gameMusic, false, 0)
	expectPB(t, h.app, uiSound.Playbacks[0], 0.45)
	expectPlayer(t, menuMusic, true, 0.9)
	if !h.uiFX.Play(audio.Voice{}) {
		t.Fatal("UI click did not play on pause menu")
	}

	drawsBeforePause := h.draws
	h.app.Advance(500 * time.Millisecond)
	if h.updates != 90 || h.draws <= drawsBeforePause {
		t.Fatalf("pause menu: updates=%d (want 90), draws=%d (want > %d)", h.updates, h.draws, drawsBeforePause)
	}
	if gameMusic.Position != pauseGamePos {
		t.Fatalf("game music advanced under pause menu: %v -> %v", pauseGamePos, gameMusic.Position)
	}

	// Background while pause menu is open: loop stops drawing and ALL audio (including UI) pauses.
	menuBeforeBG := menuMusic.Position
	drawsBeforeBG := h.draws
	h.app.SetEnvironment(background)
	h.app.Settle()
	expectPB(t, h.app, sfxSound.Playbacks[0], 0)
	expectPB(t, h.app, uiSound.Playbacks[0], 0)
	expectPlayer(t, gameMusic, false, 0)
	expectPlayer(t, menuMusic, false, 0)

	h.app.Advance(2 * time.Second)
	if h.updates != 90 || h.draws != drawsBeforeBG {
		t.Fatalf("background while paused ran loop: updates=%d draws=%d", h.updates, h.draws)
	}
	if gameMusic.Position != pauseGamePos || menuMusic.Position != menuBeforeBG {
		t.Fatalf("music advanced in background while paused")
	}

	// Return to foreground with pause menu STILL open:
	// loop resumes drawing without running updates, UI resumes, gameplay STAYS paused!
	h.app.SetEnvironment(playsAudio)
	h.app.Settle()
	if !h.mixer.Paused() {
		t.Fatal("returning from background cleared mixer.Paused()")
	}
	expectPB(t, h.app, sfxSound.Playbacks[0], 0)
	expectPlayer(t, gameMusic, false, 0)
	expectPB(t, h.app, uiSound.Playbacks[0], 0.45)
	expectPlayer(t, menuMusic, true, 0.9)
	if h.sfx.Play(audio.Voice{}) {
		t.Fatal("gameplay sfx played after foreground while pause menu still open")
	}
	if !h.uiFX.Play(audio.Voice{}) {
		t.Fatal("UI click did not play after foreground while pause menu still open")
	}

	h.app.Advance(500 * time.Millisecond)
	if h.updates != 90 {
		t.Fatalf("updates ran after foreground while still paused: %d", h.updates)
	}
	if gameMusic.Position != pauseGamePos {
		t.Fatalf("game music advanced after foreground while still paused: %v -> %v", pauseGamePos, gameMusic.Position)
	}

	// Close pause menu through loop.SetPaused(false): gameplay updates and audio resume where they left off.
	h.app.Post(func() { loop.SetPaused(false) })
	h.app.Settle()
	if h.mixer.Paused() {
		t.Fatal("loop.SetPaused(false) did not resume mixer via OnPause")
	}
	expectPB(t, h.app, sfxSound.Playbacks[0], 0.4)
	expectPlayer(t, gameMusic, true, 0.5)
	if gameMusic.Position != pauseGamePos {
		t.Fatalf("game music lost position across pause+background: got %v, want %v", gameMusic.Position, pauseGamePos)
	}

	h.app.Advance(500 * time.Millisecond)
	if h.updates != 120 {
		t.Fatalf("after closing pause menu: updates=%d, want 120", h.updates)
	}
	if gameMusic.Position <= pauseGamePos {
		t.Fatalf("game music did not advance after closing pause menu")
	}
}

func TestLoopStartingPausedInBackgroundSyncsMixerOnForeground(t *testing.T) {
	loop := &game.Loop{Rate: 60, Paused: true}
	h := startPauseHarness(t, loop, nsgotest.Environment(background))
	h.mixer.SetPauseExempt("ui", true)

	if !h.mixer.Paused() {
		t.Fatal("mixer did not start paused when loop started with Paused: true")
	}
	h.music.Play(audio.Source{URI: trackA}, 0)
	h.app.Settle()
	if h.sfx.Play(audio.Voice{}) || h.uiFX.Play(audio.Voice{}) {
		t.Fatal("sounds played while starting in background")
	}

	// Moving to foreground while still paused enables UI only; gameplay stays paused.
	h.app.SetEnvironment(playsAudio)
	h.app.Advance(200 * time.Millisecond)
	if h.updates != 0 {
		t.Fatalf("paused loop ran %d updates on foreground", h.updates)
	}
	if h.sfx.Play(audio.Voice{}) {
		t.Fatal("gameplay sfx played on foreground while loop still paused")
	}
	if !h.uiFX.Play(audio.Voice{}) {
		t.Fatal("UI sfx did not play on foreground while paused")
	}
	if len(h.app.Players) != 0 {
		t.Fatal("gameplay music started on foreground while loop still paused")
	}

	// Unpausing starts both simulation and gameplay audio.
	h.app.Post(func() { loop.SetPaused(false) })
	h.app.Settle()
	expectPlayer(t, h.playerByURI(t, trackA), true, 1)
	if !h.sfx.Play(audio.Voice{}) {
		t.Fatal("gameplay sfx did not play after unpausing")
	}
	h.app.Advance(500 * time.Millisecond)
	if h.updates != 30 {
		t.Fatalf("after unpausing: updates=%d, want 30", h.updates)
	}
}
