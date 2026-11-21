package sound_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const (
	introURI = "asset:music/boss_intro.m4a"
	loopURI  = "asset:music/boss_loop.m4a"
	shortURI = "asset:music/short_intro.m4a"
)

func startSectioned(t *testing.T, opts ...nsgotest.Option) *mixed {
	t.Helper()
	opts = append([]nsgotest.Option{func(a *nsgotest.App) {
		a.AudioDurations[introURI] = 2 * time.Second
		a.AudioDurations[loopURI] = 10 * time.Second
		a.AudioDurations[shortURI] = 400 * time.Millisecond
	}}, opts...)
	return startMixed(t, opts...)
}

func TestPlaySectionsTransitionsFromIntroToLoop(t *testing.T) {
	m := startSectioned(t)
	m.mixer.SetVolume("music", 0.6)

	m.music.PlaySections(audio.Source{URI: introURI}, audio.Source{URI: loopURI}, 0)
	intro := m.player(t, 0)
	loop := m.player(t, 1)

	expectPlayer(t, intro, true, 0.6)
	if intro.Loop {
		t.Fatal("intro should not loop")
	}
	if loop.Playing || loop.Closed || !loop.Loop {
		t.Fatalf("armed loop before intro ends: playing=%v closed=%v loop=%v", loop.Playing, loop.Closed, loop.Loop)
	}

	// Halfway through the intro, loop has still not started.
	m.app.Advance(time.Second)
	expectPlayer(t, intro, true, 0.6)
	if loop.Playing || loop.Position != 0 {
		t.Fatalf("loop started before intro ended: playing=%v pos=%v", loop.Playing, loop.Position)
	}

	// Once intro finishes, intro closes and loop plays immediately at full group volume.
	m.app.Advance(1100 * time.Millisecond)
	if !intro.Closed {
		t.Fatal("intro was not closed after handoff")
	}
	expectPlayer(t, loop, true, 0.6)
	if !loop.Loop {
		t.Fatal("loop player lost looping flag")
	}

	// PlayWithIntro behaves identically using SectionedTrack.
	m.music.PlayWithIntro(sound.SectionedTrack{
		Intro: audio.Source{URI: shortURI},
		Loop:  audio.Source{URI: loopURI},
	}, 0)
	intro2 := m.player(t, 2)
	loop2 := m.player(t, 3)
	if !loop.Closed {
		t.Fatal("previous loop was not closed by PlayWithIntro")
	}
	expectPlayer(t, intro2, true, 0.6)
	if intro2.Loop {
		t.Fatal("intro2 should not loop")
	}
	m.app.Advance(500 * time.Millisecond)
	if !intro2.Closed {
		t.Fatal("intro2 was not closed after handoff")
	}
	expectPlayer(t, loop2, true, 0.6)
	if !loop2.Loop {
		t.Fatal("loop2 does not loop")
	}
}

func TestPlaySectionsRespectsVolumeAndBackgroundInBothPhases(t *testing.T) {
	m := startSectioned(t)
	m.mixer.SetVolume("music", 0.8)
	m.music.PlaySections(audio.Source{URI: introURI}, audio.Source{URI: loopURI}, 0)

	intro := m.player(t, 0)
	loop := m.player(t, 1)
	expectPlayer(t, intro, true, 0.8)

	// Volume change during intro applies immediately.
	m.app.Advance(500 * time.Millisecond)
	m.mixer.SetVolume("music", 0.5)
	m.app.Settle()
	expectPlayer(t, intro, true, 0.5)

	// Backgrounding during intro pauses it and prevents handoff while paused.
	m.app.SetEnvironment(background)
	m.app.Settle()
	expectPlayer(t, intro, false, 0)
	at := intro.Position
	m.app.Advance(3 * time.Second)
	if intro.Position != at {
		t.Fatalf("intro advanced in background: %v -> %v", at, intro.Position)
	}
	if loop.Playing {
		t.Fatal("loop started while app was in background")
	}

	// Returning to foreground resumes intro and hands off to loop at the updated volume.
	m.app.SetEnvironment(playsAudio)
	m.app.Settle()
	expectPlayer(t, intro, true, 0.5)
	m.app.Advance(1600 * time.Millisecond)
	if !intro.Closed {
		t.Fatal("intro did not finish after resuming")
	}
	expectPlayer(t, loop, true, 0.5)

	// Volume and background changes also work during the loop phase.
	m.app.Advance(time.Second)
	m.mixer.SetMaster(0.5)
	m.app.Settle()
	expectPlayer(t, loop, true, 0.25)

	m.app.SetEnvironment(background)
	m.app.Settle()
	expectPlayer(t, loop, false, 0)
	loopAt := loop.Position
	m.app.Advance(2 * time.Second)
	if loop.Position != loopAt {
		t.Fatalf("loop advanced in background: %v -> %v", loopAt, loop.Position)
	}

	m.app.SetEnvironment(playsAudio)
	m.app.Settle()
	expectPlayer(t, loop, true, 0.25)
	if loop.Position < loopAt {
		t.Fatal("loop restarted from beginning after returning to foreground")
	}
}

func TestPlaySectionsStopOrReplaceDuringIntroCancelsLoop(t *testing.T) {
	// Stop(0) during intro cancels and closes loop immediately.
	m := startSectioned(t)
	m.music.PlaySections(audio.Source{URI: introURI}, audio.Source{URI: loopURI}, 0)
	intro := m.player(t, 0)
	loop := m.player(t, 1)
	m.app.Advance(500 * time.Millisecond)
	m.music.Stop(0)
	m.app.Settle()
	if !intro.Closed || !loop.Closed {
		t.Fatalf("after Stop(0): intro.Closed=%v loop.Closed=%v, want both true", intro.Closed, loop.Closed)
	}
	m.app.Advance(3 * time.Second)
	if loop.Playing || loop.Position != 0 {
		t.Fatalf("cancelled loop played after Stop(0): playing=%v pos=%v", loop.Playing, loop.Position)
	}

	// Stop(fade) during intro cancels loop even if intro ends before fade completes.
	m.music.PlaySections(audio.Source{URI: shortURI}, audio.Source{URI: loopURI}, 0)
	intro2 := m.player(t, 2)
	loop2 := m.player(t, 3)
	m.app.Advance(200 * time.Millisecond)
	m.music.Stop(time.Second)
	m.app.Settle()
	if !loop2.Closed {
		t.Fatal("armed loop was not cancelled on Stop(fade)")
	}
	m.app.Advance(1200 * time.Millisecond)
	if !intro2.Closed {
		t.Fatal("fading-out intro was not closed")
	}
	if loop2.Playing || loop2.Position != 0 {
		t.Fatalf("cancelled loop played during Stop(fade): playing=%v pos=%v", loop2.Playing, loop2.Position)
	}

	// Starting a new track during intro cancels the armed loop.
	m.music.PlaySections(audio.Source{URI: shortURI}, audio.Source{URI: loopURI}, 0)
	intro3 := m.player(t, 4)
	loop3 := m.player(t, 5)
	m.app.Advance(100 * time.Millisecond)
	m.music.Play(audio.Source{URI: trackB}, 500*time.Millisecond)
	m.app.Settle()
	if !loop3.Closed {
		t.Fatal("armed loop was not cancelled when replaced by Play")
	}
	m.app.Advance(600 * time.Millisecond)
	if !intro3.Closed {
		t.Fatal("replaced intro was not closed after crossfade")
	}
	if loop3.Playing || loop3.Position != 0 {
		t.Fatalf("cancelled loop played after Play replacement: playing=%v pos=%v", loop3.Playing, loop3.Position)
	}
	expectPlayer(t, m.player(t, 6), true, 1)
}

func TestPlaySectionsCrossfadeAndMidFadeHandoff(t *testing.T) {
	m := startSectioned(t)
	m.mixer.SetVolume("music", 0.5)
	m.music.Play(audio.Source{URI: trackA}, 0)
	prev := m.player(t, 0)
	expectPlayer(t, prev, true, 0.5)

	// Fade over 1s into a 400ms intro + loop: handoff happens mid-fade and loop finishes fading in.
	m.music.PlaySections(audio.Source{URI: shortURI}, audio.Source{URI: loopURI}, time.Second)
	m.app.Advance(200 * time.Millisecond)
	intro := m.player(t, 1)
	loop := m.player(t, 2)
	if !intro.Playing || loop.Playing {
		t.Fatalf("at 200ms: intro.Playing=%v loop.Playing=%v", intro.Playing, loop.Playing)
	}

	// Advance past 400ms intro end but before 1s fade ends.
	m.app.Advance(300 * time.Millisecond)
	if !intro.Closed {
		t.Fatal("short intro did not close on mid-fade handoff")
	}
	if !loop.Playing || loop.Volume <= 0 || loop.Volume >= 0.45 {
		t.Fatalf("mid-fade loop: playing=%v volume=%v, want playing mid-fade", loop.Playing, loop.Volume)
	}

	// Finish fade: previous track closed, loop at full group volume.
	m.app.Advance(600 * time.Millisecond)
	if !prev.Closed {
		t.Fatal("previous track was not closed after crossfade")
	}
	expectPlayer(t, loop, true, 0.5)
}

func TestPlaySectionsEdgeCases(t *testing.T) {
	m := startSectioned(t)

	// Empty intro plays loop directly with Loop=true.
	m.music.PlaySections(audio.Source{}, audio.Source{URI: loopURI}, 0)
	p0 := m.player(t, 0)
	expectPlayer(t, p0, true, 1)
	if !p0.Loop {
		t.Fatal("empty-intro PlaySections did not loop")
	}

	// Empty loop plays intro once with Loop=false.
	m.music.PlaySections(audio.Source{URI: introURI}, audio.Source{}, 0)
	p1 := m.player(t, 1)
	expectPlayer(t, p1, true, 1)
	if p1.Loop {
		t.Fatal("empty-loop PlaySections should not loop")
	}

	// Silent group defers opening players until audible.
	m.music.Stop(0)
	m.mixer.SetVolume("music", 0)
	before := len(m.app.Players)
	m.music.PlaySections(audio.Source{URI: shortURI}, audio.Source{URI: loopURI}, 0)
	m.app.Settle()
	if len(m.app.Players) != before {
		t.Fatalf("silent group opened %d players, want 0", len(m.app.Players)-before)
	}
	m.mixer.SetVolume("music", 0.75)
	intro := m.player(t, before)
	loop := m.player(t, before+1)
	expectPlayer(t, intro, true, 0.75)
	m.app.Advance(500 * time.Millisecond)
	expectPlayer(t, loop, true, 0.75)
}

func makeSine(freq float64, sampleRate, frames int) []float64 {
	out := make([]float64, frames)
	for i := range frames {
		out[i] = 0.8 * math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate))
	}
	return out
}

func rms(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		sum += s * s
	}
	return math.Sqrt(sum / float64(len(samples)))
}

func TestLowPassAttenuatesHighFrequenciesAndPassesLowFrequencies(t *testing.T) {
	const sr = 44100
	frames := sr / 5 // 200ms
	lp := sound.LowPass(1000, 1/math.Sqrt2)

	low := makeSine(200, sr, frames)
	lowIn := rms(low[frames/2:])
	lp.ProcessSamples(low, sr, 1)
	lowRatio := rms(low[frames/2:]) / lowIn

	high := makeSine(6000, sr, frames)
	highIn := rms(high[frames/2:])
	lp.ProcessSamples(high, sr, 1)
	highRatio := rms(high[frames/2:]) / highIn

	if lowRatio < 0.95 {
		t.Fatalf("LowPass attenuated 200 Hz too much: ratio=%v, want >= 0.95", lowRatio)
	}
	if highRatio > 0.08 {
		t.Fatalf("LowPass failed to attenuate 6000 Hz: ratio=%v, want <= 0.08", highRatio)
	}
}

func TestHighPassAttenuatesLowFrequenciesAndPassesHighFrequencies(t *testing.T) {
	const sr = 44100
	frames := sr / 5 // 200ms
	hp := sound.HighPass(1500, 1/math.Sqrt2)

	low := makeSine(200, sr, frames)
	lowIn := rms(low[frames/2:])
	hp.ProcessSamples(low, sr, 1)
	lowRatio := rms(low[frames/2:]) / lowIn

	high := makeSine(6000, sr, frames)
	highIn := rms(high[frames/2:])
	hp.ProcessSamples(high, sr, 1)
	highRatio := rms(high[frames/2:]) / highIn

	if lowRatio > 0.08 {
		t.Fatalf("HighPass failed to attenuate 200 Hz: ratio=%v, want <= 0.08", lowRatio)
	}
	if highRatio < 0.95 {
		t.Fatalf("HighPass attenuated 6000 Hz too much: ratio=%v, want >= 0.95", highRatio)
	}
}

func TestDelayProducesEchoAtExactSampleOffset(t *testing.T) {
	const sr = 44100
	const offset = 4410 // 100ms
	samples := make([]float64, 10000)
	samples[0] = 1.0

	fx := sound.Delay(100*time.Millisecond, 0.5, 0.4)
	fx.ProcessSamples(samples, sr, 1)

	if math.Abs(samples[0]-0.6) > 1e-9 {
		t.Fatalf("dry impulse at 0 = %v, want 0.6", samples[0])
	}
	for i := 1; i < offset; i++ {
		if samples[i] != 0 {
			t.Fatalf("unexpected non-zero sample at %d before echo: %v", i, samples[i])
		}
	}
	if math.Abs(samples[offset]-0.4) > 1e-9 {
		t.Fatalf("first echo at %d = %v, want 0.4", offset, samples[offset])
	}
	if math.Abs(samples[2*offset]-0.2) > 1e-9 {
		t.Fatalf("second echo at %d = %v, want 0.2", 2*offset, samples[2*offset])
	}

	// Stereo channel isolation: impulse in left channel only stays in left channel.
	stereo := make([]float64, 2*6000)
	stereo[0] = 1.0
	fx.ProcessSamples(stereo, sr, 2)
	if math.Abs(stereo[offset*2]-0.4) > 1e-9 {
		t.Fatalf("stereo left echo = %v, want 0.4", stereo[offset*2])
	}
	for f := range 6000 {
		if stereo[f*2+1] != 0 {
			t.Fatalf("stereo right channel leaked at frame %d: %v", f, stereo[f*2+1])
		}
	}
}

func TestReverbProducesDecayingTail(t *testing.T) {
	const sr = 44100
	frames := sr * 2 // 2 seconds stereo
	samples := make([]float64, frames*2)
	samples[0] = 1.0
	samples[1] = 1.0

	rv := sound.Reverb(0.75, 0.4, 0.5)
	rv.ProcessSamples(samples, sr, 2)

	for i, s := range samples {
		if math.IsNaN(s) || math.IsInf(s, 0) {
			t.Fatalf("reverb produced invalid sample at %d: %v", i, s)
		}
	}

	// Measure tail RMS across three successive windows after the initial reflections arrive.
	w1 := rms(samples[2*(sr/20) : 2*(sr/4)])  // 50ms .. 250ms
	w2 := rms(samples[2*(sr/2) : 2*(3*sr/4)]) // 500ms .. 750ms
	w3 := rms(samples[2*(sr) : 2*(3*sr/2)])   // 1000ms .. 1500ms
	if w1 <= 1e-4 {
		t.Fatalf("reverb produced negligible early tail: w1=%v", w1)
	}
	if !(w1 > w2 && w2 > w3 && w3 > 0) {
		t.Fatalf("reverb tail did not decay monotonically: w1=%v w2=%v w3=%v", w1, w2, w3)
	}
}

func TestGainBitcrushAndEffectChainReuse(t *testing.T) {
	const sr = 44100

	// Gain scales samples accurately.
	g := []float64{-0.8, 0.4, 1.0}
	sound.Gain(0.5).ProcessSamples(g, sr, 1)
	wantG := []float64{-0.4, 0.2, 0.5}
	for i := range g {
		if math.Abs(g[i]-wantG[i]) > 1e-9 {
			t.Fatalf("Gain[%d]=%v, want %v", i, g[i], wantG[i])
		}
	}

	// Bitcrush quantizes to 4-bit (steps of 1/8) and holds every 4th frame.
	bc := []float64{0.31, 0.99, -0.5, 0.1, -0.64, 0.2, 0.3, 0.4}
	sound.Bitcrush(4, 4).ProcessSamples(bc, sr, 1)
	for i := 0; i < 4; i++ {
		if bc[i] != 0.25 {
			t.Fatalf("Bitcrush frame %d = %v, want 0.25", i, bc[i])
		}
	}
	for i := 4; i < 8; i++ {
		if bc[i] != -0.625 {
			t.Fatalf("Bitcrush frame %d = %v, want -0.625", i, bc[i])
		}
	}

	// EffectChain is stateless across calls and ignores nil entries.
	chain := sound.EffectChain{sound.LowPass(1000, 0), nil, sound.Gain(0.5)}
	a := makeSine(440, sr, 1024)
	b := makeSine(440, sr, 1024)
	chain.ProcessSamples(a, sr, 1)
	chain.ProcessSamples(b, sr, 1)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("EffectChain not deterministic across runs at %d: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestProcessWAVAndMixerProcessSourcePlayableWithAudio(t *testing.T) {
	const sr = 44100
	frames := sr / 10 // 100ms
	rawSamples := makeSine(6000, sr, frames)
	rawWAV, err := sound.EncodeWAV(rawSamples, sr, 1)
	if err != nil {
		t.Fatalf("EncodeWAV: %v", err)
	}

	// Empty chain round-trips WAV bit-for-bit on PCM samples.
	roundTrip, err := sound.ProcessWAV(rawWAV)
	if err != nil {
		t.Fatalf("ProcessWAV identity: %v", err)
	}
	if !bytes.Equal(rawWAV, roundTrip) {
		t.Fatal("identity ProcessWAV did not round-trip WAV identically")
	}

	// WAV with an extra odd-sized JUNK chunk before fmt/data still decodes cleanly.
	withJunk := injectJunkChunk(rawWAV)
	if _, err := sound.ProcessWAV(withJunk, sound.Gain(0.5)); err != nil {
		t.Fatalf("ProcessWAV with JUNK chunk failed: %v", err)
	}

	// Invalid WAV returns error.
	if _, err := sound.ProcessWAV([]byte("not a wav")); err == nil {
		t.Fatal("expected error for invalid WAV")
	}

	// Mixer bus effects + ProcessSource produce playable WAV audio.Source for both audio.LoadSound and audio.NewPlayer.
	m := startMixed(t)
	m.mixer.SetGroupEffects("sfx", sound.LowPass(800, 1/math.Sqrt2), sound.Gain(0.9))
	if got := m.mixer.GroupEffects("sfx"); len(got) != 2 {
		t.Fatalf("GroupEffects length = %d, want 2", len(got))
	}

	// Group with no effects passes source through unchanged.
	unchanged, err := m.mixer.ProcessSource("dry", audio.Source{URI: clickURI})
	if err != nil || unchanged.URI != clickURI {
		t.Fatalf("ProcessSource dry group: src=%+v err=%v", unchanged, err)
	}

	// Group with effects rejects a source without in-memory Data.
	if _, err := m.mixer.ProcessSource("sfx", audio.Source{URI: clickURI}); err == nil {
		t.Fatal("expected error when ProcessSource called without Source.Data")
	}

	processed, err := m.mixer.ProcessSource("sfx", audio.Source{Data: rawWAV})
	if err != nil {
		t.Fatalf("ProcessSource: %v", err)
	}
	decoded, gotSR, gotCh, err := sound.DecodeWAV(processed.Data)
	if err != nil {
		t.Fatalf("DecodeWAV(processed): %v", err)
	}
	if gotSR != sr || gotCh != 1 || len(decoded) != frames {
		t.Fatalf("processed WAV header: sr=%d ch=%d samples=%d", gotSR, gotCh, len(decoded))
	}
	if rms(decoded[frames/2:])/rms(rawSamples[frames/2:]) > 0.08 {
		t.Fatal("processed WAV did not attenuate 6000 Hz tone through LowPass bus")
	}

	// Clearing effects removes them.
	m.mixer.SetGroupEffects("sfx")
	if got := m.mixer.GroupEffects("sfx"); got != nil {
		t.Fatalf("GroupEffects after clear = %v, want nil", got)
	}

	// Verify processed audio.Source loads and plays cleanly via audio.LoadSound and audio.NewPlayer in nsgotest.
	var loaded *audio.Sound
	var loadErr error
	var effect *sound.Effect
	var player *audio.Player
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mx := sound.NewMixer(cx, "bus")
		audio.LoadSound(cx, processed, func(s *audio.Sound, err error) {
			loaded, loadErr = s, err
			if err == nil {
				effect = mx.Effect(s, "sfx", 2)
			}
		})
		player = audio.NewPlayer(cx, processed, audio.PlayerOptions{})
		return core.View{}
	}, nsgotest.Environment(playsAudio), func(a *nsgotest.App) {
		a.AudioDurations = map[string]time.Duration{
			string(processed.Data): 100 * time.Millisecond,
		}
	})
	if loadErr != nil || loaded == nil || effect == nil {
		t.Fatalf("audio.LoadSound on processed WAV failed: sound=%v err=%v", loaded, loadErr)
	}
	if !effect.Play(audio.Voice{}) {
		t.Fatal("processed sound effect did not play")
	}
	app2.Settle()
	if len(app2.Sounds) != 1 || len(app2.Sounds[0].Plays) != 1 {
		t.Fatalf("expected 1 sound playback, got %d sounds", len(app2.Sounds))
	}
	player.Play(nil)
	app2.Settle()
	if len(app2.Players) != 1 || !app2.Players[0].Playing {
		t.Fatalf("processed WAV did not play in audio.Player: %+v", app2.Players)
	}
}

func injectJunkChunk(wav []byte) []byte {
	junk := []byte{'J', 'U', 'N', 'K', 3, 0, 0, 0, 'a', 'b', 'c', 0} // odd size 3 + 1 pad byte
	out := make([]byte, 0, len(wav)+len(junk))
	out = append(out, wav[:12]...)
	out = append(out, junk...)
	out = append(out, wav[12:]...)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out
}
