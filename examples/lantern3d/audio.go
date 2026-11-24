package main

import (
	"math"
	"sync"

	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	audioSampleRate = 22050

	ambientMusicURI = "asset:audio/lantern_ambient.wav"
	switchChimeURI  = "asset:audio/lantern_switch.wav"
	actionTickURI   = "asset:audio/lantern_tick.wav"
)

type synthSources struct {
	ambient audio.Source
	chime   audio.Source
	tick    audio.Source
}

var (
	synthOnce   sync.Once
	synthCached synthSources
	synthErr    error
)

// audioSources returns the deterministically synthesized WAV audio clips shared across the 2D and 3D scenes.
func audioSources() (synthSources, error) {
	synthOnce.Do(func() {
		ambientWAV, err := sound.EncodeWAV(synthAmbientLoop(4.0), audioSampleRate, 1)
		if err != nil {
			synthErr = err
			return
		}
		chimeWAV, err := sound.EncodeWAV(synthSwitchChime(), audioSampleRate, 1)
		if err != nil {
			synthErr = err
			return
		}
		tickWAV, err := sound.EncodeWAV(synthActionTick(), audioSampleRate, 1)
		if err != nil {
			synthErr = err
			return
		}
		synthCached = synthSources{
			ambient: audio.Source{URI: ambientMusicURI, Data: ambientWAV},
			chime:   audio.Source{URI: switchChimeURI, Data: chimeWAV},
			tick:    audio.Source{URI: actionTickURI, Data: tickWAV},
		}
	})
	return synthCached, synthErr
}

// Audio owns the shared sound.Mixer, looping ambient music, and UI/gameplay SFX on the scene.Manager's parent scope so audio survives every 2D <-> 3D scene transition without interruption.
type Audio struct {
	Mixer     *sound.Mixer
	Music     *sound.Music
	SwitchSFX *sound.Effect
	ActionSFX *sound.Effect

	mutedSig *core.Signal[bool]
}

// NewAudio creates a shared sound.Mixer on cx (the Manager's parent scope), starts the looping ambient music, and loads the scene-switch chime and action SFX.
func NewAudio(cx *core.Scope) (*Audio, error) {
	srcs, err := audioSources()
	if err != nil {
		return nil, err
	}

	mixer := sound.NewMixer(cx, "lantern3d.sound")
	mixer.SetDefaultMaster(1)
	mixer.SetDefaultVolume("music", 0.45)
	mixer.SetDefaultVolume("sfx", 0.75)
	mixer.SetDefaultVolume("ui", 0.75)
	mixer.SetPauseExempt("ui", true)

	music := mixer.Music("music")
	music.Loop = true

	a := &Audio{
		Mixer:    mixer,
		Music:    music,
		mutedSig: core.NewSignal(cx, mixer.Master() == 0),
	}

	audio.LoadSound(cx, srcs.chime, func(s *audio.Sound, err error) {
		if err == nil && s != nil {
			a.SwitchSFX = mixer.Effect(s, "ui", 4)
		}
	})
	audio.LoadSound(cx, srcs.tick, func(s *audio.Sound, err error) {
		if err == nil && s != nil {
			a.ActionSFX = mixer.Effect(s, "sfx", 4)
		}
	})

	music.Play(srcs.ambient, 0)
	return a, nil
}

// PlaySwitch plays the temple chime SFX when switching between 2D and 3D scenes.
func (a *Audio) PlaySwitch() {
	if a != nil && a.SwitchSFX != nil {
		a.SwitchSFX.Play(audio.Voice{Volume: 0.9})
	}
}

// PlayAction plays a subtle click SFX for gameplay/UI toggles.
func (a *Audio) PlayAction() {
	if a != nil && a.ActionSFX != nil {
		a.ActionSFX.Play(audio.Voice{Volume: 0.6})
	}
}

// Muted reports whether master audio output is currently muted.
func (a *Audio) Muted() bool {
	if a == nil || a.mutedSig == nil {
		return false
	}
	return a.mutedSig.Get()
}

// ToggleMute mutes or unmutes the shared mixer across both 2D and 3D scenes.
func (a *Audio) ToggleMute() {
	if a == nil {
		return
	}
	next := !a.mutedSig.GetUntracked()
	if next {
		_ = a.Mixer.SetMaster(0)
	} else {
		_ = a.Mixer.SetMaster(1)
	}
	a.mutedSig.Set(next)
}

// synthAmbientLoop builds a seamless looping twilight pentatonic drone so looping produces no click at the boundary.
func synthAmbientLoop(seconds float64) []float64 {
	n := int(seconds * audioSampleRate)
	out := make([]float64, n)
	// Frequencies chosen as exact integer cycle counts over `seconds` for click-free looping.
	f1 := math.Round(130.81*seconds) / seconds // ~C3
	f2 := math.Round(196.00*seconds) / seconds // ~G3
	f3 := math.Round(293.66*seconds) / seconds // ~D4
	lfo := 1.0 / seconds
	for i := range out {
		t := float64(i) / audioSampleRate
		swell := 0.72 + 0.28*math.Sin(2*math.Pi*lfo*t)
		sample := 0.20*math.Sin(2*math.Pi*f1*t) +
			0.13*math.Sin(2*math.Pi*f2*t) +
			0.07*swell*math.Sin(2*math.Pi*f3*t)
		out[i] = sample
	}
	return out
}

// synthSwitchChime builds a warm two-note temple bell chime (C5 -> G5).
func synthSwitchChime() []float64 {
	part1 := synthTone(523.25, 523.25, 0.11, 0.42)
	part2 := synthTone(783.99, 783.99, 0.18, 0.46)
	out := make([]float64, 0, len(part1)+len(part2))
	out = append(out, part1...)
	out = append(out, part2...)
	return out
}

// synthActionTick builds a crisp soft wood-block tap.
func synthActionTick() []float64 {
	return synthTone(640, 420, 0.06, 0.35)
}

func synthTone(f0, f1, seconds, volume float64) []float64 {
	n := int(seconds * audioSampleRate)
	out := make([]float64, n)
	attack := max(1, audioSampleRate/200)
	phase := 0.0
	for i := range out {
		u := float64(i) / float64(max(1, n))
		freq := f0 + (f1-f0)*u
		phase += 2 * math.Pi * freq / audioSampleRate
		var env float64
		if i < attack {
			env = float64(i) / float64(attack)
		} else {
			env = 1.0 - float64(i-attack)/float64(max(1, n-attack))
		}
		out[i] = volume * env * math.Sin(phase)
	}
	return out
}
