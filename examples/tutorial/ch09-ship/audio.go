package main

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/audio"
)

// sampleRate is the rate the effects are synthesized at, in samples a second.
const sampleRate = 22050

// effectsGroup is the mixer group the effects play in.
const effectsGroup = "effects"

// makeSounds synthesizes a WAV for each event that makes a sound, so the game ships without audio files. A game with recorded sounds puts them under resources/ and loads them by "asset:" URI instead.
func makeSounds() (map[event]audio.Source, error) {
	clips := map[event][]float64{
		eventCaught: join(tone(880, 880, 0.06, 0.5), tone(1320, 1320, 0.09, 0.5)),
		eventHit:    thud(0.22, 0.7),
		eventMissed: tone(440, 220, 0.25, 0.45),
		eventOver:   join(tone(660, 660, 0.15, 0.45), tone(495, 495, 0.15, 0.45), tone(330, 300, 0.35, 0.45)),
	}
	sources := make(map[event]audio.Source, len(clips))
	for e, samples := range clips {
		wav, err := sound.EncodeWAV(samples, sampleRate, 1)
		if err != nil {
			return nil, err
		}
		sources[e] = audio.Source{Data: wav}
	}
	return sources, nil
}

// tone is seconds of a sine wave gliding from f0 to f1 hertz, at volume, with a short fade in and a fade out over its length.
func tone(f0, f1, seconds, volume float64) []float64 {
	n := int(seconds * sampleRate)
	out := make([]float64, n)
	phase := 0.0
	for i := range out {
		t := float64(i) / float64(n)
		phase += 2 * math.Pi * (f0 + (f1-f0)*t) / sampleRate
		out[i] = volume * envelope(i, n) * math.Sin(phase)
	}
	return out
}

// thud is seconds of noise, smoothed to a low rumble, at volume: a rock landing in the basket.
func thud(seconds, volume float64) []float64 {
	n := int(seconds * sampleRate)
	out := make([]float64, n)
	// A fixed seed makes the same thud on every launch.
	rng := random.New(1)
	smooth := 0.0
	for i := range out {
		smooth += (rng.Range(-1, 1) - smooth) * 0.15
		out[i] = max(-1, min(1, volume*envelope(i, n)*smooth*3))
	}
	return out
}

// envelope is the loudness of sample i of n: up over the first 5 ms, then down to silence at the end, so a clip neither clicks in nor stops short.
func envelope(i, n int) float64 {
	attack := sampleRate / 200
	if i < attack {
		return float64(i) / float64(attack)
	}
	return 1 - float64(i-attack)/float64(n-attack)
}

// join plays clips one after another.
func join(clips ...[]float64) []float64 {
	var out []float64
	for _, c := range clips {
		out = append(out, c...)
	}
	return out
}
