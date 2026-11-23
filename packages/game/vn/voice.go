package vn

import (
	"time"

	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/audio"
)

// VoicePlayer plays a scene's voice lines. A Presenter calls it on the engine's goroutine, as audio's methods are.
type VoicePlayer interface {
	// PlayVoice starts the voice line id and returns how long it plays; ok is false when there is no such line or nothing played, such as while the voice volume is 0.
	PlayVoice(id string) (length time.Duration, ok bool)
	// StopVoice stops the voice line playing, if any.
	StopVoice()
}

// VoiceClip is one voice line played through a sound.Mixer.
type VoiceClip struct {
	// Effect plays the line, in the mixer's voice group, such as mixer.Effect(snd, "voice", 1).
	Effect *sound.Effect
	// Length is how long the line plays, such as the loaded audio.Sound's Duration; auto mode waits this long before it counts its delay.
	Length time.Duration
}

// EffectVoices is a VoicePlayer of mixer effects by voice id. It plays one line at a time, stopping the line before it.
type EffectVoices struct {
	// Clips are the voice lines by id.
	Clips   map[string]VoiceClip
	playing *sound.Effect
}

// PlayVoice stops the line playing and starts the clip id, returning its Length; ok is false when Clips has no such clip or its effect played nothing.
func (v *EffectVoices) PlayVoice(id string) (time.Duration, bool) {
	v.StopVoice()
	clip, ok := v.Clips[id]
	if !ok || clip.Effect == nil {
		return 0, false
	}
	if !clip.Effect.Play(audio.Voice{}) {
		return 0, false
	}
	v.playing = clip.Effect
	return clip.Length, true
}

// StopVoice stops the line playing, if any.
func (v *EffectVoices) StopVoice() {
	if v.playing != nil {
		v.playing.Stop()
		v.playing = nil
	}
}
