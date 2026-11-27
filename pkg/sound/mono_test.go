package sound_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/audio"
)

func TestMixerMonoCentersPositionalAndDirectVoicesKeepingGain(t *testing.T) {
	m := startMixed(t)
	m.click.Limit = 16
	m.mixer.SetListener3D(listener3D)

	if m.mixer.Mono() || m.mixer.Balance() != 0 {
		t.Fatalf("defaults: mono=%v balance=%v, want false and 0", m.mixer.Mono(), m.mixer.Balance())
	}

	// Far-right 2D position: 80 right of camera (100, 50) -> gain 0.3, pan +0.8 in stereo.
	farRight2D := math2d.V2(180, 50)
	if !m.click.PlayAt(camera, farRight2D, audio.Voice{Volume: 0.8}) {
		t.Fatal("stereo PlayAt did not play")
	}

	// Enable mono: PlayAt at the exact same far-right position starts with pan 0 and the same gain.
	m.mixer.SetMono(true)
	if !m.mixer.Mono() {
		t.Fatal("Mono() = false after SetMono(true)")
	}
	if !m.click.PlayAt(camera, farRight2D, audio.Voice{Volume: 0.8}) {
		t.Fatal("mono PlayAt did not play")
	}
	// Listener2D.Pan still reports the true direction so directional captions keep it.
	if !near(camera.Pan(farRight2D), 0.8) {
		t.Fatalf("Listener2D.Pan = %v, want 0.8", camera.Pan(farRight2D))
	}

	// Far-left 2D position and Cue.PlayAt also start with pan 0 and unchanged gain.
	farLeft2D := math2d.V2(0, 50)
	if !m.click.PlayAt(camera, farLeft2D, audio.Voice{}) {
		t.Fatal("mono far-left PlayAt did not play")
	}
	cue := sound.NewCue(1, sound.CueOptions{
		Clips:     []sound.Clip{sound.EffectClip(m.click)},
		PitchMin:  1.25,
		PitchMax:  1.25,
		VolumeMin: 0.5,
		VolumeMax: 0.5,
	})
	if _, ok := cue.PlayAt(camera, farRight2D); !ok {
		t.Fatal("mono Cue.PlayAt did not play")
	}

	// Direct Effect.Play with explicit pan starts centered under mono.
	if !m.click.Play(audio.Voice{Volume: 0.7, Pan: -0.9}) {
		t.Fatal("mono Play did not play")
	}

	// 3D positional playback (PlayAt3D and live Play3D) starts at pan 0 with identical attenuation,
	// while Listener3D.Pan and Playback3D.Pan still report the geometric direction.
	right3D := sound.V3(10, 0, 0)
	behindRight3D := sound.V3(6, 0, 6)
	if !m.click.PlayAt3D(listener3D, behindRight3D, audio.Voice{Volume: 0.9}) {
		t.Fatal("mono PlayAt3D did not play")
	}
	pb3d := m.click.Play3D(right3D, audio.Voice{Volume: 0.6, Loop: true}, sound.Spatial3DOptions{})
	if !pb3d.Playing() {
		t.Fatal("mono Play3D did not start")
	}
	defer pb3d.Stop()
	if !near(pb3d.Pan(), 1.0) || !near(listener3D.Pan(right3D), 1.0) {
		t.Fatalf("geometric 3D pan under mono = pb:%v listener:%v, want 1.0", pb3d.Pan(), listener3D.Pan(right3D))
	}

	plays := m.plays()
	if len(plays) != 7 {
		t.Fatalf("plays count = %d, want 7", len(plays))
	}
	// Stereo vs mono PlayAt at far right: identical volume, pan +0.8 vs 0.
	stereoRight, monoRight := plays[0], plays[1]
	if !near(stereoRight.Pan, 0.8) {
		t.Fatalf("stereo right pan = %v, want 0.8", stereoRight.Pan)
	}
	if monoRight.Pan != 0 {
		t.Fatalf("mono right pan = %v, want 0", monoRight.Pan)
	}
	if !near(monoRight.Volume, stereoRight.Volume) {
		t.Fatalf("mono right volume = %v, want stereo volume %v", monoRight.Volume, stereoRight.Volume)
	}
	// Far left PlayAt: pan 0, distance gain 0.1.
	if plays[2].Pan != 0 || !near(plays[2].Volume, 0.1) {
		t.Fatalf("mono far-left PlayAt = %+v, want pan 0 vol 0.1", plays[2])
	}
	// Cue.PlayAt at far right: pan 0, volume 0.3 * 0.5 = 0.15.
	if plays[3].Pan != 0 || !near(plays[3].Volume, 0.15) || !near(plays[3].Rate, 1.25) {
		t.Fatalf("mono Cue.PlayAt = %+v, want pan 0 vol 0.15 rate 1.25", plays[3])
	}
	// Direct Play: pan 0, volume 0.7.
	if plays[4].Pan != 0 || !near(plays[4].Volume, 0.7) {
		t.Fatalf("mono direct Play = %+v, want pan 0 vol 0.7", plays[4])
	}
	// PlayAt3D behind-right: pan 0, volume preserves distance + rear head-shadow attenuation.
	wantBehindVol := 0.9 * listener3D.Gain(behindRight3D)
	if plays[5].Pan != 0 || !near(plays[5].Volume, wantBehindVol) {
		t.Fatalf("mono PlayAt3D = %+v, want pan 0 vol %v", plays[5], wantBehindVol)
	}
	// Play3D full-right: backend voice pan 0, volume preserves 3D distance gain.
	wantRight3DVol := 0.6 * listener3D.Gain(right3D)
	if plays[6].Pan != 0 || !near(plays[6].Volume, wantRight3DVol) {
		t.Fatalf("mono Play3D = %+v, want pan 0 vol %v", plays[6], wantRight3DVol)
	}

	// FollowSystemMono returns to platform default (false on NSGo v0.3.4) and restores stereo panning.
	m.mixer.FollowSystemMono()
	if m.mixer.Mono() {
		t.Fatal("Mono() = true after FollowSystemMono on default environment")
	}
	if !m.click.PlayAt(camera, farRight2D, audio.Voice{Volume: 0.8}) {
		t.Fatal("restored stereo PlayAt did not play")
	}
	plays = m.plays()
	if !near(plays[len(plays)-1].Pan, 0.8) {
		t.Fatalf("restored stereo pan = %v, want 0.8", plays[len(plays)-1].Pan)
	}
}

func TestMixerBalanceOffsetsPanAndClamps(t *testing.T) {
	m := startMixed(t)
	m.click.Limit = 16
	m.mixer.SetListener3D(listener3D)

	// Out-of-range and non-finite balance inputs clamp to [-1, 1] (NaN -> 0).
	m.mixer.SetBalance(2.5)
	if m.mixer.Balance() != 1 {
		t.Fatalf("Balance(2.5) = %v, want 1", m.mixer.Balance())
	}
	m.mixer.SetBalance(-3.0)
	if m.mixer.Balance() != -1 {
		t.Fatalf("Balance(-3.0) = %v, want -1", m.mixer.Balance())
	}
	m.mixer.SetBalance(float32(math.Inf(1)))
	if m.mixer.Balance() != 1 {
		t.Fatalf("Balance(+Inf) = %v, want 1", m.mixer.Balance())
	}
	m.mixer.SetBalance(float32(math.Inf(-1)))
	if m.mixer.Balance() != -1 {
		t.Fatalf("Balance(-Inf) = %v, want -1", m.mixer.Balance())
	}
	m.mixer.SetBalance(float32(math.NaN()))
	if m.mixer.Balance() != 0 {
		t.Fatalf("Balance(NaN) = %v, want 0", m.mixer.Balance())
	}

	// Positive balance shifts centered, left, and right voices rightward and clamps at +1.
	m.mixer.SetBalance(0.4)
	if !near(m.mixer.Balance(), 0.4) {
		t.Fatalf("Balance() = %v, want 0.4", m.mixer.Balance())
	}
	// Centered direct play: 0 + 0.4 = 0.4
	m.click.Play(audio.Voice{})
	// 2D PlayAt 50 left (pan -0.5): -0.5 + 0.4 = -0.1
	m.click.PlayAt(camera, math2d.V2(50, 50), audio.Voice{})
	// 2D PlayAt 80 right (pan +0.8): 0.8 + 0.4 = 1.2 -> clamped to 1.0
	m.click.PlayAt(camera, math2d.V2(180, 50), audio.Voice{})
	// 3D Play3D at 45 deg front-left (pan -0.7071): -0.7071 + 0.4 ≈ -0.3071
	pb := m.click.Play3D(sound.V3(-5, 0, -5), audio.Voice{}, sound.Spatial3DOptions{})
	if !pb.Playing() {
		t.Fatal("Play3D did not start")
	}

	// Negative balance shifts leftward and clamps at -1.
	m.mixer.SetBalance(-0.6)
	// 2D PlayAt 70 left (pan -0.7): -0.7 - 0.6 = -1.3 -> clamped to -1.0
	m.click.PlayAt(camera, math2d.V2(30, 50), audio.Voice{})

	// Enabling mono overrides balance to pan 0 while preserving Balance() for when mono turns off.
	m.mixer.SetMono(true)
	m.click.PlayAt(camera, math2d.V2(150, 50), audio.Voice{})
	if !near(m.mixer.Balance(), -0.6) {
		t.Fatalf("Balance() changed under SetMono(true): got %v, want -0.6", m.mixer.Balance())
	}
	m.mixer.SetMono(false)
	// 2D PlayAt 50 right (pan +0.5): 0.5 - 0.6 = -0.1
	m.click.PlayAt(camera, math2d.V2(150, 50), audio.Voice{})

	plays := m.plays()
	if len(plays) != 7 {
		t.Fatalf("plays count = %d, want 7", len(plays))
	}
	wantPans := []float32{
		0.4,
		-0.1,
		1.0,
		float32(-math.Sqrt2/2) + 0.4,
		-1.0,
		0.0,
		-0.1,
	}
	for i, want := range wantPans {
		if !near(plays[i].Pan, want) {
			t.Errorf("play %d pan = %v, want %v", i, plays[i].Pan, want)
		}
	}

	// Nil mixer methods are safe no-ops.
	var nilMixer *sound.Mixer
	nilMixer.SetMono(true)
	nilMixer.FollowSystemMono()
	nilMixer.SetBalance(0.5)
	if nilMixer.Mono() || nilMixer.Balance() != 0 {
		t.Fatalf("nil mixer returned mono=%v balance=%v, want false and 0", nilMixer.Mono(), nilMixer.Balance())
	}
}
