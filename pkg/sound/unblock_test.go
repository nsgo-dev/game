package sound_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMusicBlockedByNotAllowedPlaysAfterUnblock(t *testing.T) {
	m := startMixed(t, func(a *nsgotest.App) {
		a.RefusePlayback = true
	})
	m.mixer.SetVolume("music", 0.5)
	m.music.Loop = true
	m.music.Play(audio.Source{URI: trackA}, 0)

	p := m.player(t, 0)
	if p.Playing || p.Closed || p.Position != 0 || !p.Loop {
		t.Fatalf("blocked track: playing=%v closed=%v pos=%v loop=%v; want paused/unclosed at 0, looping", p.Playing, p.Closed, p.Position, p.Loop)
	}
	if !m.music.Blocked() {
		t.Fatal("music.Blocked() = false after NotAllowed, want true")
	}

	// Changing volume while blocked updates the player's level without spamming retries.
	resumesBefore := countCommand(p.Commands, "resume")
	m.mixer.SetVolume("music", 0.4)
	m.app.Settle()
	if !near(p.Volume, 0.4) {
		t.Fatalf("blocked track volume = %v, want 0.4", p.Volume)
	}
	if got := countCommand(p.Commands, "resume"); got != resumesBefore {
		t.Fatalf("volume change while blocked sent extra resume: %d -> %d", resumesBefore, got)
	}

	// Unblock while playback is still refused retries once and remains blocked.
	m.mixer.Unblock()
	m.app.Settle()
	if p.Playing || !m.music.Blocked() {
		t.Fatalf("still-refused Unblock: playing=%v blocked=%v; want false, true", p.Playing, m.music.Blocked())
	}

	// Once audio is allowed, Unblock starts the track from position 0 at its group volume with loop intact.
	m.app.RefusePlayback = false
	m.mixer.Unblock()
	expectPlayer(t, m.player(t, 0), true, 0.4)
	if m.music.Blocked() {
		t.Fatal("music.Blocked() = true after Unblock succeeded")
	}
	if !p.Loop || p.Position != 0 {
		t.Fatalf("unblocked track: loop=%v pos=%v; want true, 0", p.Loop, p.Position)
	}

	// Calling Unblock again when nothing is blocked sends no extra resume.
	resumesAfter := countCommand(p.Commands, "resume")
	m.mixer.Unblock()
	m.app.Settle()
	if got := countCommand(p.Commands, "resume"); got != resumesAfter {
		t.Fatalf("redundant Unblock sent resume: %d -> %d", resumesAfter, got)
	}
}

func TestMusicBlockedPreservesCrossfadeAndDecodeStillEnds(t *testing.T) {
	// 1. Fade-in interrupted by NotAllowed resumes mid-fade at the current level and finishes at full group volume.
	m := startMixed(t, func(a *nsgotest.App) {
		a.RefusePlayback = true
	})
	m.mixer.SetVolume("music", 0.5)
	m.music.Play(audio.Source{URI: trackA}, time.Second)
	a := m.player(t, 0)
	if a.Playing || !m.music.Blocked() {
		t.Fatalf("fade-in blocked: playing=%v blocked=%v; want false, true", a.Playing, m.music.Blocked())
	}

	resumesAtStart := countCommand(a.Commands, "resume")
	m.app.Advance(500 * time.Millisecond)
	if a.Playing || !m.music.Blocked() {
		t.Fatalf("mid-fade while blocked: playing=%v blocked=%v; want false, true", a.Playing, m.music.Blocked())
	}
	if got := countCommand(a.Commands, "resume"); got != resumesAtStart {
		t.Fatalf("crossfade frames spammed resume while blocked: %d -> %d", resumesAtStart, got)
	}

	m.app.RefusePlayback = false
	m.mixer.Unblock()
	m.app.Settle()
	if !a.Playing || m.music.Blocked() || a.Volume < 0.2 || a.Volume > 0.3 {
		t.Fatalf("mid-fade after Unblock: playing=%v blocked=%v vol=%v; want playing near 0.25", a.Playing, m.music.Blocked(), a.Volume)
	}

	m.app.Advance(600 * time.Millisecond)
	expectPlayer(t, a, true, 0.5)

	// 2. Two-track crossfade while both tracks are blocked resumes both at their mid-fade levels and closes the old track on completion.
	m2 := startMixed(t, func(a *nsgotest.App) {
		a.RefusePlayback = true
	})
	m2.mixer.SetVolume("music", 0.5)
	m2.music.Play(audio.Source{URI: trackA}, 0)
	pA := m2.player(t, 0)
	m2.music.Play(audio.Source{URI: trackB}, time.Second)
	pB := m2.player(t, 1)
	if pA.Playing || pB.Playing || !m2.music.Blocked() {
		t.Fatalf("two-track blocked: pA.Playing=%v pB.Playing=%v blocked=%v", pA.Playing, pB.Playing, m2.music.Blocked())
	}

	m2.app.Advance(500 * time.Millisecond)
	m2.app.RefusePlayback = false
	m2.mixer.Unblock()
	m2.app.Settle()
	if !pA.Playing || !pB.Playing || math.Abs(float64(pA.Volume+pB.Volume-0.5)) > 0.03 {
		t.Fatalf("two-track mid-fade after Unblock: pA=(%v,%v) pB=(%v,%v)", pA.Playing, pA.Volume, pB.Playing, pB.Volume)
	}
	m2.app.Advance(600 * time.Millisecond)
	if !pA.Closed {
		t.Fatal("old track was not closed when crossfade finished after Unblock")
	}
	expectPlayer(t, pB, true, 0.5)

	// 3. A decode failure (source absent from AudioDurations) ends the track; Blocked stays false and Unblock does not retry it.
	m3 := startMixed(t)
	m3.music.Play(audio.Source{URI: "asset:music/corrupt.m4a"}, 0)
	bad := m3.player(t, 0)
	if bad.Playing || m3.music.Blocked() {
		t.Fatalf("decode failure: playing=%v blocked=%v; want false, false", bad.Playing, m3.music.Blocked())
	}
	resumesBad := countCommand(bad.Commands, "resume")
	m3.mixer.Unblock()
	m3.app.Settle()
	if bad.Playing || countCommand(bad.Commands, "resume") != resumesBad {
		t.Fatalf("Unblock retried decode-failed track: playing=%v commands=%v", bad.Playing, bad.Commands)
	}
}

func TestMusicBlockedReactiveUIAndLifecycle(t *testing.T) {
	var mixer *sound.Mixer
	var music *sound.Music
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mixer = sound.NewMixer(cx, "sound")
		music = mixer.Music("music")
		music.Loop = true
		return core.View{
			Children: []core.Node{
				core.Show{
					When: music.Blocked,
					Then: func(*core.Scope) core.Node {
						return core.Text{TestID: "tap-hint", Content: "Tap for sound"}
					},
				},
			},
		}
	},
		nsgotest.Environment(playsAudio),
		func(a *nsgotest.App) {
			a.AudioDurations = map[string]time.Duration{trackA: time.Minute}
			a.RefusePlayback = true
		},
	)

	if len(app.Query("id:tap-hint")) != 0 {
		t.Fatal("tap-hint shown before Play")
	}

	music.Play(audio.Source{URI: trackA}, 0)
	app.Settle()
	if len(app.Query("id:tap-hint")) != 1 {
		t.Fatal("tap-hint did not appear when music became blocked")
	}

	// Unblocking while paused clears Blocked and hides the hint, waiting until unpaused to play.
	mixer.SetPaused(true)
	app.Settle()
	app.RefusePlayback = false
	mixer.Unblock()
	app.Settle()
	if music.Blocked() || len(app.Query("id:tap-hint")) != 0 {
		t.Fatal("tap-hint stayed visible after Unblock while paused")
	}
	if app.Players[0].Playing {
		t.Fatal("music started while mixer was paused")
	}
	mixer.SetPaused(false)
	app.Settle()
	expectPlayer(t, app.Players[0], true, 1)

	// Stop clears Blocked and closes the track so a later Unblock does nothing.
	app.RefusePlayback = true
	music.Play(audio.Source{URI: trackA}, 0)
	app.Settle()
	if !music.Blocked() || len(app.Query("id:tap-hint")) != 1 {
		t.Fatal("second blocked track did not show tap-hint")
	}
	music.Stop(0)
	app.Settle()
	if music.Blocked() || len(app.Query("id:tap-hint")) != 0 {
		t.Fatal("Stop(0) did not clear blocked state")
	}
	app.RefusePlayback = false
	mixer.Unblock()
	app.Settle()
	if !app.Players[1].Closed || app.Players[1].Playing {
		t.Fatalf("stopped blocked track resumed after Unblock: closed=%v playing=%v", app.Players[1].Closed, app.Players[1].Playing)
	}

	// Nil receivers are safe.
	var nilMixer *sound.Mixer
	nilMixer.Unblock()
	var nilMusic *sound.Music
	if nilMusic.Blocked() {
		t.Fatal("nil Music.Blocked() = true")
	}
}

func TestPlayLoopedAndPlaySectionsSurviveNotAllowed(t *testing.T) {
	// PlayLooped preserves Loop and LoopStart across NotAllowed + Unblock.
	m := startSectioned(t, func(a *nsgotest.App) {
		a.RefusePlayback = true
	})
	m.mixer.SetVolume("music", 0.6)
	m.music.PlayLooped(audio.Source{URI: trackA}, 15*time.Second, 0)
	p := m.player(t, 0)
	if p.Playing || !m.music.Blocked() {
		t.Fatalf("PlayLooped blocked: playing=%v blocked=%v", p.Playing, m.music.Blocked())
	}
	m.app.RefusePlayback = false
	m.mixer.Unblock()
	m.app.Settle()
	expectPlayer(t, p, true, 0.6)
	if !p.Loop || p.LoopStart != 15*time.Second {
		t.Fatalf("PlayLooped after Unblock: loop=%v loopStart=%v; want true, 15s", p.Loop, p.LoopStart)
	}

	// PlaySections keeps its armed loop track when intro fails with NotAllowed, and hands off after Unblock.
	m2 := startSectioned(t, func(a *nsgotest.App) {
		a.RefusePlayback = true
	})
	m2.mixer.SetVolume("music", 0.7)
	m2.music.PlaySections(audio.Source{URI: shortURI}, audio.Source{URI: loopURI}, 0)
	intro := m2.player(t, 0)
	loop := m2.player(t, 1)
	if intro.Playing || loop.Closed || !m2.music.Blocked() {
		t.Fatalf("PlaySections blocked: intro.Playing=%v loop.Closed=%v blocked=%v", intro.Playing, loop.Closed, m2.music.Blocked())
	}
	m2.app.RefusePlayback = false
	m2.mixer.Unblock()
	m2.app.Settle()
	expectPlayer(t, intro, true, 0.7)
	m2.app.Advance(500 * time.Millisecond)
	if !intro.Closed {
		t.Fatal("intro did not hand off and close after Unblock")
	}
	expectPlayer(t, loop, true, 0.7)

	// A decode failure on PlaySections' intro still cancels the armed loop.
	m3 := startSectioned(t)
	m3.music.PlaySections(audio.Source{URI: "asset:music/missing_intro.m4a"}, audio.Source{URI: loopURI}, 0)
	badIntro := m3.player(t, 0)
	cancelledLoop := m3.player(t, 1)
	if badIntro.Playing || !cancelledLoop.Closed || m3.music.Blocked() {
		t.Fatalf("decode-failed intro: playing=%v loopClosed=%v blocked=%v; want false, true, false", badIntro.Playing, cancelledLoop.Closed, m3.music.Blocked())
	}
}

func countCommand(cmds []string, name string) int {
	n := 0
	for _, c := range cmds {
		if c == name {
			n++
		}
	}
	return n
}
