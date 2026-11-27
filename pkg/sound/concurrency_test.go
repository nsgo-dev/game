package sound_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const (
	hitAURI   = "asset:sounds/hit_a.wav"
	hitBURI   = "asset:sounds/hit_b.wav"
	hitCURI   = "asset:sounds/hit_c.wav"
	voiceURI  = "asset:sounds/voice.wav"
	chaffURI  = "asset:sounds/chaff.wav"
	bgmURI    = "asset:music/bgm.m4a"
	speechURI = "asset:music/speech.m4a"
)

type concurrencyHarness struct {
	app    *nsgotest.App
	mixer  *sound.Mixer
	hitA   *sound.Effect
	hitB   *sound.Effect
	hitC   *sound.Effect
	voice  *sound.Effect
	chaff  *sound.Effect
	music  *sound.Music
	speech *sound.Music
	sounds map[string]*nsgotest.Sound
}

func startConcurrencyHarness(t *testing.T, durations map[string]time.Duration) *concurrencyHarness {
	t.Helper()
	h := &concurrencyHarness{sounds: map[string]*nsgotest.Sound{}}
	durs := map[string]time.Duration{
		hitAURI:   500 * time.Millisecond,
		hitBURI:   500 * time.Millisecond,
		hitCURI:   500 * time.Millisecond,
		voiceURI:  400 * time.Millisecond,
		chaffURI:  500 * time.Millisecond,
		bgmURI:    time.Minute,
		speechURI: 400 * time.Millisecond,
	}
	for k, v := range durations {
		durs[k] = v
	}

	h.app = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		h.mixer = sound.NewMixer(cx, "sound")
		load := func(uri, bus string, dst **sound.Effect) {
			audio.LoadSound(cx, audio.Source{URI: uri}, func(s *audio.Sound, err error) {
				if err != nil {
					t.Errorf("load %s: %v", uri, err)
					return
				}
				*dst = h.mixer.Effect(s, bus, 0)
			})
		}
		load(hitAURI, "sfx", &h.hitA)
		load(hitBURI, "sfx", &h.hitB)
		load(hitCURI, "sfx", &h.hitC)
		load(voiceURI, "dialogue", &h.voice)
		load(chaffURI, "ambient", &h.chaff)
		h.music = h.mixer.Music("music")
		h.speech = h.mixer.Music("dialogue")
		return core.View{}
	}, nsgotest.Environment(playsAudio), func(a *nsgotest.App) {
		a.AudioDurations = durs
	})

	h.app.Settle()
	for _, s := range h.app.Sounds {
		h.sounds[s.Source.URI] = s
	}
	if h.hitA == nil || h.hitB == nil || h.hitC == nil || h.voice == nil || h.chaff == nil {
		t.Fatal("failed to load test sounds")
	}
	return h
}

func (h *concurrencyHarness) playbacks(uri string) []*nsgotest.AudioPlayback {
	h.app.Settle()
	return h.sounds[uri].Playbacks
}

func (h *concurrencyHarness) activeCount(uri string) int {
	n := 0
	for _, p := range h.playbacks(uri) {
		if !p.Ended {
			n++
		}
	}
	return n
}

func TestSharedConcurrencyGroupAcrossEffectsAndCuesRefuseAndOldest(t *testing.T) {
	h := startConcurrencyHarness(t, nil)

	// Shared group capped at 2 with ResolveRefuse (default).
	h.mixer.SetConcurrencyGroup("impacts", sound.ConcurrencyGroup{
		Max:     2,
		Resolve: sound.ResolveRefuse,
	})
	h.hitA.Concurrency = "impacts"
	h.hitB.Concurrency = "impacts"

	cue := sound.NewCue(42, sound.CueOptions{
		Clips:       []sound.Clip{sound.EffectClip(h.hitC)},
		Concurrency: "impacts",
	})

	if !h.hitA.Play(audio.Voice{Volume: 0.8}) {
		t.Fatal("hitA did not play")
	}
	if !h.hitB.Play(audio.Voice{Volume: 0.8}) {
		t.Fatal("hitB did not play")
	}
	// Third sound (via Cue sharing the group) must be refused without stopping hitA or hitB.
	if _, ok := cue.Play(); ok {
		t.Fatal("cue played despite ResolveRefuse cap of 2")
	}
	if len(h.playbacks(hitCURI)) != 0 {
		t.Fatalf("refused cue still hit backend: %d playbacks", len(h.playbacks(hitCURI)))
	}
	if h.activeCount(hitAURI) != 1 || h.activeCount(hitBURI) != 1 {
		t.Fatalf("active counts hitA=%d hitB=%d, want 1 and 1", h.activeCount(hitAURI), h.activeCount(hitBURI))
	}

	snap := h.mixer.DebugSnapshot()
	if snap.ActiveVoices != 2 || len(snap.Groups) != 1 || snap.Groups[0].Active != 2 || snap.Groups[0].Refused != 1 {
		t.Fatalf("unexpected snapshot after refuse: %+v", snap)
	}

	// Switch resolution to ResolveOldest: cue now plays and stops hitA (oldest).
	h.mixer.SetConcurrencyGroup("impacts", sound.ConcurrencyGroup{
		Max:     2,
		Resolve: sound.ResolveOldest,
	})
	if _, ok := cue.Play(); !ok {
		t.Fatal("cue did not play under ResolveOldest")
	}
	if !h.playbacks(hitAURI)[0].Ended {
		t.Fatal("oldest voice (hitA) was not stopped")
	}
	if h.playbacks(hitBURI)[0].Ended || h.playbacks(hitCURI)[0].Ended {
		t.Fatal("newer voices (hitB, hitC) should remain active")
	}
	snap = h.mixer.DebugSnapshot()
	if snap.Groups[0].Stolen != 1 || snap.Groups[0].Active != 2 {
		t.Fatalf("unexpected snapshot after steal: %+v", snap)
	}
}

func TestConcurrencyResolveFarthest2DAndLive3D(t *testing.T) {
	t.Run("2D PlayAt", func(t *testing.T) {
		h := startConcurrencyHarness(t, nil)
		h.mixer.SetConcurrencyGroup("hits", sound.ConcurrencyGroup{
			Max:     2,
			Resolve: sound.ResolveFarthest,
		})
		h.hitA.Concurrency = "hits"
		h.hitB.Concurrency = "hits"
		h.hitC.Concurrency = "hits"

		listener := sound.Listener2D{Position: math2d.V2(0, 0), Near: 10, Far: 200}
		if !h.hitA.PlayAt(listener, math2d.V2(20, 0), audio.Voice{}) {
			t.Fatal("near hitA did not play")
		}
		if !h.hitB.PlayAt(listener, math2d.V2(100, 0), audio.Voice{}) {
			t.Fatal("far hitB did not play")
		}
		// Even farther hitC (150) is refused.
		if h.hitC.PlayAt(listener, math2d.V2(150, 0), audio.Voice{}) {
			t.Fatal("farther hitC should have been refused")
		}
		// Closer hitC (50) steals the farthest active voice (hitB at 100), keeping hitA (20).
		if !h.hitC.PlayAt(listener, math2d.V2(50, 0), audio.Voice{}) {
			t.Fatal("closer hitC should have played")
		}
		if h.playbacks(hitAURI)[0].Ended {
			t.Fatal("closer hitA was mistakenly stopped")
		}
		if !h.playbacks(hitBURI)[0].Ended {
			t.Fatal("farthest hitB was not stopped")
		}
		if h.playbacks(hitCURI)[0].Ended {
			t.Fatal("new closer hitC should be playing")
		}
	})

	t.Run("Live 3D Playback3D motion updates distance for ResolveFarthest", func(t *testing.T) {
		h := startConcurrencyHarness(t, nil)
		h.mixer.SetListener3D(sound.Listener3D{
			Position: sound.V3(0, 0, 0),
			Forward:  sound.V3(0, 0, -1),
			Up:       sound.V3(0, 1, 0),
			Near:     1,
			Far:      100,
		})
		h.mixer.SetConcurrencyGroup("engines", sound.ConcurrencyGroup{
			Max:     2,
			Resolve: sound.ResolveFarthest,
		})
		h.hitA.Concurrency = "engines"
		h.hitB.Concurrency = "engines"
		h.hitC.Concurrency = "engines"

		pbA := h.hitA.Play3D(sound.V3(0, 0, -10), audio.Voice{Loop: true}, sound.Spatial3DOptions{})
		pbB := h.hitB.Play3D(sound.V3(0, 0, -40), audio.Voice{Loop: true}, sound.Spatial3DOptions{})
		if !pbA.Playing() || !pbB.Playing() {
			t.Fatal("expected both 3D playbacks playing")
		}
		// Move pbA far away (80) so pbA is now farther than pbB (40).
		pbA.SetPosition(sound.V3(0, 0, -80))

		// Incoming pbC at distance 30 should steal pbA (distance 80), not pbB (distance 40).
		pbC := h.hitC.Play3D(sound.V3(0, 0, -30), audio.Voice{Loop: true}, sound.Spatial3DOptions{})
		if !pbC.Playing() {
			t.Fatal("expected closer pbC to play")
		}
		if pbA.Playing() {
			t.Fatal("moved-far pbA should have been stolen")
		}
		if !pbB.Playing() {
			t.Fatal("closer pbB should still be playing")
		}
		pbB.Stop()
		pbC.Stop()
	})
}

func TestConcurrencyResolveQuietestAcrossBuses(t *testing.T) {
	h := startConcurrencyHarness(t, nil)
	// Give ambient bus half the volume of sfx bus to verify audible volume comparison across buses.
	_ = h.mixer.SetVolume("sfx", 1.0)
	_ = h.mixer.SetVolume("ambient", 0.4)
	h.mixer.SetConcurrencyGroup("world", sound.ConcurrencyGroup{
		Max:     2,
		Resolve: sound.ResolveQuietest,
	})
	h.hitA.Concurrency = "world"  // sfx bus (1.0)
	h.chaff.Concurrency = "world" // ambient bus (0.4)
	h.hitB.Concurrency = "world"  // sfx bus (1.0)

	// hitA plays at audible 0.8; chaff plays at audible 0.4 * 0.5 = 0.2.
	if !h.hitA.Play(audio.Voice{Volume: 0.8}) {
		t.Fatal("hitA failed")
	}
	if !h.chaff.Play(audio.Voice{Volume: 0.5}) {
		t.Fatal("chaff failed")
	}
	// A quieter incoming sound (audible 0.1) is refused.
	if h.hitB.Play(audio.Voice{Volume: 0.1}) {
		t.Fatal("quieter sound should have been refused")
	}
	// A louder incoming sound (audible 0.5) steals chaff (0.2), keeping hitA (0.8).
	if !h.hitB.Play(audio.Voice{Volume: 0.5}) {
		t.Fatal("louder sound should have played")
	}
	if h.playbacks(hitAURI)[0].Ended {
		t.Fatal("louder hitA was mistakenly stopped")
	}
	if !h.playbacks(chaffURI)[0].Ended {
		t.Fatal("quietest chaff was not stopped")
	}
	if h.playbacks(hitBURI)[0].Ended {
		t.Fatal("new hitB should be playing")
	}
}

func TestConcurrencyResolveLowestPriorityAndTieBreaking(t *testing.T) {
	h := startConcurrencyHarness(t, nil)
	h.mixer.SetConcurrencyGroup("combat", sound.ConcurrencyGroup{
		Max:     3,
		Resolve: sound.ResolveLowestPriority,
	})
	h.hitA.Concurrency = "combat"
	h.hitB.Concurrency = "combat"
	h.hitC.Concurrency = "combat"
	h.hitA.Priority = 5
	h.hitB.Priority = 10
	h.hitC.Priority = 5

	if !h.hitA.Play(audio.Voice{}) {
		t.Fatal("hitA failed")
	}
	if !h.hitB.Play(audio.Voice{}) {
		t.Fatal("hitB failed")
	}
	if !h.hitC.Play(audio.Voice{}) {
		t.Fatal("hitC failed")
	}

	// Strictly lower priority (2 < 5) is refused.
	if h.hitA.PlayWith(audio.Voice{}, sound.PlayOptions{Priority: 2}) {
		t.Fatal("lower-priority sound should be refused")
	}
	// Equal priority (5 == 5) steals the oldest among lowest priority (hitA, not hitC or hitB).
	if !h.hitA.PlayWith(audio.Voice{}, sound.PlayOptions{Priority: 5}) {
		t.Fatal("equal-priority sound should steal oldest lowest-priority voice")
	}
	pbsA := h.playbacks(hitAURI)
	if !pbsA[0].Ended || pbsA[1].Ended {
		t.Fatalf("expected first hitA stolen and second hitA playing; ended=%v,%v", pbsA[0].Ended, pbsA[1].Ended)
	}
	if h.playbacks(hitBURI)[0].Ended || h.playbacks(hitCURI)[0].Ended {
		t.Fatal("higher-priority hitB and newer hitC should remain playing")
	}

	// Higher priority (20) steals remaining oldest priority-5 voice (hitC).
	if !h.hitB.PlayWith(audio.Voice{}, sound.PlayOptions{Priority: 20}) {
		t.Fatal("higher-priority sound should steal remaining lowest-priority voice")
	}
	if !h.playbacks(hitCURI)[0].Ended {
		t.Fatal("hitC (oldest priority 5) should have been stolen")
	}
	if h.playbacks(hitBURI)[0].Ended || h.playbacks(hitBURI)[1].Ended {
		t.Fatal("high-priority hitB voices should remain playing")
	}
}

func TestConcurrencyOwnerScopeLimitsRetriggerAndDucking(t *testing.T) {
	h := startConcurrencyHarness(t, nil)
	h.mixer.SetConcurrencyGroup("steps", sound.ConcurrencyGroup{
		Max:       2,
		Resolve:   sound.ResolveRefuse,
		Scope:     sound.ScopeOwner,
		Retrigger: 50 * time.Millisecond,
		Duck: sound.DuckEnvelope{
			Amount: 0.5,
		},
	})
	h.hitA.Concurrency = "steps"

	// Owner 1 plays first step.
	if !h.hitA.PlayWith(audio.Voice{Volume: 1}, sound.PlayOptions{Owner: "enemy-1"}) {
		t.Fatal("enemy-1 first step failed")
	}
	// Owner 1 immediately retriggers within 50ms -> refused.
	if h.hitA.PlayWith(audio.Voice{Volume: 1}, sound.PlayOptions{Owner: "enemy-1"}) {
		t.Fatal("enemy-1 retrigger within cooldown should be refused")
	}
	// Owner 2 plays immediately -> independent scope, allowed!
	if !h.hitA.PlayWith(audio.Voice{Volume: 1}, sound.PlayOptions{Owner: "enemy-2"}) {
		t.Fatal("enemy-2 should not be blocked by enemy-1's cooldown")
	}

	// Advance past retrigger cooldown and play a second step for enemy-1.
	h.app.Advance(60 * time.Millisecond)
	if !h.hitA.PlayWith(audio.Voice{Volume: 1}, sound.PlayOptions{Owner: "enemy-1"}) {
		t.Fatal("enemy-1 second step after cooldown failed")
	}

	// enemy-1's first step is ducked to 0.5; enemy-2's step is NOT ducked (different owner).
	pbs := h.playbacks(hitAURI)
	if !near(pbs[0].Volume, 0.5) {
		t.Fatalf("enemy-1 older step volume = %v, want 0.5 (ducked)", pbs[0].Volume)
	}
	if !near(pbs[1].Volume, 1.0) {
		t.Fatalf("enemy-2 step volume = %v, want 1.0 (not ducked by enemy-1)", pbs[1].Volume)
	}
	if !near(pbs[2].Volume, 1.0) {
		t.Fatalf("enemy-1 newer step volume = %v, want 1.0", pbs[2].Volume)
	}

	// Advance past cooldown again: enemy-1 is at Max=2 so refused, while enemy-2 can still play its 2nd step.
	h.app.Advance(60 * time.Millisecond)
	if h.hitA.PlayWith(audio.Voice{Volume: 1}, sound.PlayOptions{Owner: "enemy-1"}) {
		t.Fatal("enemy-1 third step should be refused at Max=2")
	}
	if !h.hitA.PlayWith(audio.Voice{Volume: 1}, sound.PlayOptions{Owner: "enemy-2"}) {
		t.Fatal("enemy-2 second step should be allowed")
	}

	snap := h.mixer.DebugSnapshot()
	if snap.Groups[0].ActiveOwners != 2 || snap.Groups[0].Retriggered != 1 {
		t.Fatalf("unexpected owner-scope snapshot: %+v", snap.Groups[0])
	}

	// Uncomparable owner (slice) must not panic; falls back cleanly to shared nil bucket.
	if !h.hitA.PlayWith(audio.Voice{Volume: 1}, sound.PlayOptions{Owner: []int{1, 2}}) {
		t.Fatal("uncomparable owner should not panic and should play in fallback bucket")
	}
}

func TestConcurrencyRetriggerCooldownSharedAcrossEffects(t *testing.T) {
	h := startConcurrencyHarness(t, nil)
	h.mixer.SetConcurrencyGroup("coins", sound.ConcurrencyGroup{
		Max:       8,
		Resolve:   sound.ResolveOldest,
		Retrigger: 100 * time.Millisecond,
	})
	h.hitA.Concurrency = "coins"
	h.hitB.Concurrency = "coins"

	if !h.hitA.Play(audio.Voice{}) {
		t.Fatal("initial coin failed")
	}
	h.app.Advance(50 * time.Millisecond)
	if h.hitB.Play(audio.Voice{}) {
		t.Fatal("second effect in same group played during retrigger cooldown")
	}
	h.app.Advance(50 * time.Millisecond)
	if !h.hitB.Play(audio.Voice{}) {
		t.Fatal("second effect did not play once retrigger cooldown elapsed")
	}
	snap := h.mixer.DebugSnapshot()
	if snap.Groups[0].Retriggered != 1 || snap.Groups[0].Refused != 1 {
		t.Fatalf("retrigger counters = %+v, want retriggered=1 refused=1", snap.Groups[0])
	}
}

func TestConcurrencyStealFadeSmoothlyFadesOutAndStops(t *testing.T) {
	h := startConcurrencyHarness(t, nil)
	h.mixer.SetConcurrencyGroup("pads", sound.ConcurrencyGroup{
		Max:       1,
		Resolve:   sound.ResolveOldest,
		StealFade: 100 * time.Millisecond,
	})
	h.hitA.Concurrency = "pads"
	h.hitB.Concurrency = "pads"

	if !h.hitA.Play(audio.Voice{Volume: 0.8}) {
		t.Fatal("hitA failed")
	}
	// Play hitB: hitA is stolen with 100ms StealFade (immediately removed from ActiveVoices, counted in FadingVoices).
	if !h.hitB.Play(audio.Voice{Volume: 0.8}) {
		t.Fatal("hitB failed")
	}
	snap := h.mixer.DebugSnapshot()
	if snap.ActiveVoices != 1 || snap.FadingVoices != 1 {
		t.Fatalf("expected ActiveVoices=1 FadingVoices=1, got %+v", snap)
	}
	pbA := h.playbacks(hitAURI)[0]
	if pbA.Ended {
		t.Fatal("stolen voice ended immediately instead of fading over StealFade")
	}

	// Halfway through 100ms steal fade: hitA volume should be near 0.4 and still playing.
	h.app.Advance(50 * time.Millisecond)
	h.app.Settle()
	if pbA.Ended || math.Abs(float64(pbA.Volume-0.4)) > 0.1 {
		t.Fatalf("halfway through steal fade: ended=%v vol=%v, want ~0.4", pbA.Ended, pbA.Volume)
	}

	// After full steal fade: hitA must be stopped and removed from FadingVoices.
	h.app.Advance(60 * time.Millisecond)
	h.app.Settle()
	if !pbA.Ended {
		t.Fatal("stolen voice did not stop after StealFade completed")
	}
	snap = h.mixer.DebugSnapshot()
	if snap.FadingVoices != 0 || snap.ActiveVoices != 1 {
		t.Fatalf("expected FadingVoices=0 ActiveVoices=1 after fade, got %+v", snap)
	}
}

func TestConcurrencyGroupDuckEnvelopeAttackAndRelease(t *testing.T) {
	h := startConcurrencyHarness(t, map[string]time.Duration{
		hitAURI: 1000 * time.Millisecond,
		hitBURI: 200 * time.Millisecond,
	})
	h.mixer.SetConcurrencyGroup("shots", sound.ConcurrencyGroup{
		Max:     4,
		Resolve: sound.ResolveOldest,
		Duck: sound.DuckEnvelope{
			Amount:  0.5,
			Attack:  100 * time.Millisecond,
			Release: 100 * time.Millisecond,
		},
	})
	h.hitA.Concurrency = "shots"
	h.hitB.Concurrency = "shots"

	if !h.hitA.Play(audio.Voice{Volume: 1.0}) {
		t.Fatal("hitA failed")
	}
	pbA := h.playbacks(hitAURI)[0]
	if !near(pbA.Volume, 1.0) {
		t.Fatalf("initial hitA volume = %v, want 1.0", pbA.Volume)
	}

	// Play newer hitB (lasts 200ms): hitA begins 100ms attack ramp down to 0.5.
	if !h.hitB.Play(audio.Voice{Volume: 1.0}) {
		t.Fatal("hitB failed")
	}
	h.app.Advance(50 * time.Millisecond)
	h.app.Settle()
	if math.Abs(float64(pbA.Volume-0.75)) > 0.1 {
		t.Fatalf("mid-attack hitA volume = %v, want ~0.75", pbA.Volume)
	}
	h.app.Advance(60 * time.Millisecond)
	h.app.Settle()
	if !near(pbA.Volume, 0.5) {
		t.Fatalf("post-attack hitA volume = %v, want 0.5", pbA.Volume)
	}

	// Advance past hitB's 200ms duration (90ms left + 10ms) so hitB finishes naturally -> hitA starts 100ms release back to 1.0.
	h.app.Advance(140 * time.Millisecond)
	h.app.Settle()
	if !h.playbacks(hitBURI)[0].Ended {
		t.Fatal("hitB should have ended naturally")
	}
	// ~50ms into release: hitA should be recovering between 0.5 and 1.0.
	if pbA.Volume <= 0.55 || pbA.Volume >= 0.95 {
		t.Fatalf("mid-release hitA volume = %v, want between 0.55 and 0.95", pbA.Volume)
	}
	h.app.Advance(100 * time.Millisecond)
	h.app.Settle()
	if !near(pbA.Volume, 1.0) {
		t.Fatalf("post-release hitA volume = %v, want 1.0", pbA.Volume)
	}
}

func TestBusDuckingMusicByDialogueAndRelease(t *testing.T) {
	h := startConcurrencyHarness(t, map[string]time.Duration{
		voiceURI: 300 * time.Millisecond,
	})
	_ = h.mixer.SetVolume("music", 1.0)
	h.mixer.SetBusDucking(sound.BusDuckRule{
		Bus:     "music",
		By:      "dialogue",
		Amount:  0.4, // lowers music by 40% -> 0.6x
		Attack:  100 * time.Millisecond,
		Release: 200 * time.Millisecond,
	})

	h.music.Loop = true
	h.music.Play(audio.Source{URI: bgmURI}, 0)
	h.app.Settle()
	bgm := h.app.Players[0]
	expectPlayer(t, bgm, true, 1.0)

	// Start dialogue voice line (300ms): music ducks from 1.0 -> 0.6 over 100ms.
	if !h.voice.Play(audio.Voice{Volume: 1.0}) {
		t.Fatal("voice did not play")
	}
	h.app.Advance(50 * time.Millisecond)
	h.app.Settle()
	if math.Abs(float64(bgm.Volume-0.8)) > 0.08 {
		t.Fatalf("mid-attack music volume = %v, want ~0.8", bgm.Volume)
	}
	h.app.Advance(60 * time.Millisecond)
	h.app.Settle()
	expectPlayer(t, bgm, true, 0.6)

	// Start a second overlapping dialogue line at t=110ms (runs until t=410ms).
	if !h.voice.Play(audio.Voice{Volume: 1.0}) {
		t.Fatal("second voice did not play")
	}
	// Advance to t=320ms: first line ended at 300ms, second still playing -> music MUST stay ducked at 0.6!
	h.app.Advance(210 * time.Millisecond)
	h.app.Settle()
	if !h.playbacks(voiceURI)[0].Ended || h.playbacks(voiceURI)[1].Ended {
		t.Fatal("expected first voice ended and second still playing")
	}
	expectPlayer(t, bgm, true, 0.6)

	// Advance to t=510ms (100ms after second line ended at 410ms): halfway through 200ms release (~0.8).
	h.app.Advance(190 * time.Millisecond)
	h.app.Settle()
	if !h.playbacks(voiceURI)[1].Ended {
		t.Fatal("second voice should have ended")
	}
	if math.Abs(float64(bgm.Volume-0.8)) > 0.08 {
		t.Fatalf("mid-release music volume = %v, want ~0.8", bgm.Volume)
	}

	// Advance past full release: music back to 1.0.
	h.app.Advance(150 * time.Millisecond)
	h.app.Settle()
	expectPlayer(t, bgm, true, 1.0)
}

func TestBusDuckingFullSilenceKeepsMusicPlayingAndStacksRules(t *testing.T) {
	h := startConcurrencyHarness(t, map[string]time.Duration{
		voiceURI: 200 * time.Millisecond,
		hitAURI:  200 * time.Millisecond,
	})
	h.mixer.SetConcurrencyGroup("boom", sound.ConcurrencyGroup{Max: 4})
	h.hitA.Concurrency = "boom"

	// Two rules targeting "music": one by bus "dialogue" (amount 0.5), one by concurrency group "boom" (amount 0.5).
	h.mixer.SetBusDucking(
		sound.BusDuckRule{Bus: "music", By: "dialogue", Amount: 0.5},
		sound.BusDuckRule{Bus: "music", By: "boom", Amount: 0.5},
	)

	h.music.Loop = true
	h.music.Play(audio.Source{URI: bgmURI}, 0)
	h.app.Settle()
	bgm := h.app.Players[0]
	expectPlayer(t, bgm, true, 1.0)

	// Trigger both: 0.5 * 0.5 = 0.25x.
	h.voice.Play(audio.Voice{})
	h.hitA.Play(audio.Voice{})
	h.app.Settle()
	expectPlayer(t, bgm, true, 0.25)

	snap := h.mixer.DebugSnapshot()
	var musicDuck float32 = -1
	for _, b := range snap.Buses {
		if b.Name == "music" {
			musicDuck = b.DuckGain
		}
	}
	if !near(musicDuck, 0.25) {
		t.Fatalf("DebugSnapshot music DuckGain = %v, want 0.25", musicDuck)
	}

	// Replace with Amount=1.0 (full duck): music stays playing (muted at volume 0) so playhead keeps advancing.
	h.mixer.SetBusDucking(sound.BusDuckRule{Bus: "music", By: "dialogue", Amount: 1.0})
	h.app.Settle()
	posBefore := bgm.Position
	if !bgm.Playing || bgm.Volume != 0 {
		t.Fatalf("full duck should mute without pausing: playing=%v volume=%v", bgm.Playing, bgm.Volume)
	}
	h.app.Advance(100 * time.Millisecond)
	h.app.Settle()
	if bgm.Position <= posBefore {
		t.Fatalf("music position did not advance while fully ducked: before=%v after=%v", posBefore, bgm.Position)
	}
	// Let dialogue finish -> music unmutes and returns to 1.0.
	h.app.Advance(150 * time.Millisecond)
	h.app.Settle()
	expectPlayer(t, bgm, true, 1.0)
}

func TestGlobalVoiceBudgetStealsByPriorityAcrossGroups(t *testing.T) {
	h := startConcurrencyHarness(t, nil)
	if got := h.mixer.MaxVoices(); got != sound.DefaultMaxVoices {
		t.Fatalf("default MaxVoices = %d, want %d", got, sound.DefaultMaxVoices)
	}
	h.mixer.SetMaxVoices(3)
	if got := h.mixer.MaxVoices(); got != 3 {
		t.Fatalf("MaxVoices = %d, want 3", got)
	}

	h.chaff.Priority = 0
	h.hitA.Priority = 10
	h.voice.Priority = 100

	// Fill budget with 3 low-priority chaff voices.
	for range 3 {
		if !h.chaff.Play(audio.Voice{}) {
			t.Fatal("chaff did not play")
		}
	}
	// High-priority voice line steals oldest chaff voice.
	if !h.voice.Play(audio.Voice{}) {
		t.Fatal("high-priority voice line should steal low-priority chaff")
	}
	if !h.playbacks(chaffURI)[0].Ended || h.playbacks(chaffURI)[1].Ended || h.playbacks(chaffURI)[2].Ended {
		t.Fatal("expected only oldest chaff voice stolen")
	}

	// Medium-priority gunfire steals the remaining two chaff voices.
	if !h.hitA.Play(audio.Voice{}) || !h.hitA.Play(audio.Voice{}) {
		t.Fatal("medium-priority gunfire should steal chaff")
	}
	if h.activeCount(chaffURI) != 0 || h.activeCount(voiceURI) != 1 || h.activeCount(hitAURI) != 2 {
		t.Fatalf("counts chaff=%d voice=%d hitA=%d, want 0, 1, 2",
			h.activeCount(chaffURI), h.activeCount(voiceURI), h.activeCount(hitAURI))
	}

	// Now all 3 slots hold priority >= 10: a low-priority chaff burst must be REFUSED and never cut gunfire or voice.
	for range 5 {
		if h.chaff.Play(audio.Voice{}) {
			t.Fatal("low-priority chaff stole a higher-priority slot")
		}
	}
	if h.activeCount(voiceURI) != 1 || h.activeCount(hitAURI) != 2 {
		t.Fatal("high/medium-priority voices were disturbed by low-priority burst")
	}

	// Reset budget to default with 0.
	h.mixer.SetMaxVoices(0)
	if got := h.mixer.MaxVoices(); got != sound.DefaultMaxVoices {
		t.Fatalf("SetMaxVoices(0) = %d, want DefaultMaxVoices (%d)", got, sound.DefaultMaxVoices)
	}
}

func TestConcurrencyLimitInteractionAndSanitization(t *testing.T) {
	h := startConcurrencyHarness(t, nil)

	// Effect.Limit=2 combined with ResolveRefuse Max=2 must refuse (never sneak past via Effect.Limit).
	h.mixer.SetConcurrencyGroup("strict", sound.ConcurrencyGroup{
		Max:     2,
		Resolve: sound.ResolveRefuse,
	})
	h.hitA.Limit = 2
	h.hitA.Concurrency = "strict"
	if !h.hitA.Play(audio.Voice{}) || !h.hitA.Play(audio.Voice{}) {
		t.Fatal("first two plays failed")
	}
	if h.hitA.Play(audio.Voice{}) {
		t.Fatal("third play should be refused by ResolveRefuse even when Effect.Limit == 2")
	}

	// Removing concurrency group clears group ducking and lets future plays use Effect.Limit normally.
	h.mixer.RemoveConcurrencyGroup("strict")
	if _, ok := h.mixer.ConcurrencyGroup("strict"); ok {
		t.Fatal("group still present after RemoveConcurrencyGroup")
	}
	if !h.hitA.Play(audio.Voice{}) {
		t.Fatal("play after removing group should succeed via Effect.Limit")
	}

	// Sanitization of invalid values and nil mixer safety.
	var nilMixer *sound.Mixer
	nilMixer.SetMaxVoices(5)
	if nilMixer.MaxVoices() != sound.DefaultMaxVoices {
		t.Fatal("nil mixer MaxVoices wrong")
	}
	nilMixer.SetConcurrencyGroup("x", sound.ConcurrencyGroup{Max: 1})
	nilMixer.RemoveConcurrencyGroup("x")
	nilMixer.SetBusDucking(sound.BusDuckRule{Bus: "a", By: "b", Amount: 0.5})
	nilMixer.AddBusDucking(sound.BusDuckRule{Bus: "a", By: "b", Amount: 0.5})
	if len(nilMixer.BusDucking()) != 0 {
		t.Fatal("nil mixer BusDucking should be empty")
	}
	_ = nilMixer.DebugSnapshot()

	h.mixer.SetBusDucking(
		sound.BusDuckRule{Bus: "music", By: "music", Amount: 0.5}, // self-duck ignored
		sound.BusDuckRule{Bus: "", By: "sfx", Amount: 0.5},        // empty bus ignored
		sound.BusDuckRule{Bus: "music", By: "sfx", Amount: 0},     // zero amount ignored
		sound.BusDuckRule{Bus: "music", By: "sfx", Amount: float32(math.NaN())},
	)
	if got := h.mixer.BusDucking(); len(got) != 0 {
		t.Fatalf("invalid bus duck rules were not filtered: %+v", got)
	}
}
