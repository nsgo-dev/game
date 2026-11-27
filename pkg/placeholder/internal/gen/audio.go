package gen

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/game/pkg/tooling/importsettings"
)

// SampleRate is the sample rate of every sound in the pack, in Hz. The sounds are mono, 16-bit PCM WAV.
const SampleRate = 22050

// Music's tempo: MusicBeats beats at MusicBPM, so the loop is a whole number of samples and every note starts on a sample.
const (
	MusicBPM   = 150
	MusicBeats = 16
	beat       = SampleRate * 60 / MusicBPM
	eighth     = beat / 2
)

// effects are the pack's sound effects by name, each written as sounds/<name>.wav.
var effects = []struct {
	name string
	make func() []float64
}{
	{"jump", jump},
	{"coin", coin},
	{"hit", hit},
	{"explosion", explosion},
	{"shoot", shoot},
	{"powerup", powerup},
	{"click", click},
	{"select", selectSound},
}

// addSounds synthesizes the effects and the music loop and writes them with their sidecars. Every effect's sidecar normalizes its peak to the same level; the music's keeps it quieter and loops all of it.
func addSounds(files map[string][]byte) error {
	sfxPeak := -3.0
	for _, e := range effects {
		path := "sounds/" + e.name + ".wav"
		if err := addWAV(files, path, e.make()); err != nil {
			return err
		}
		if err := addSidecar(files, path, &importsettings.File{Audio: &importsettings.Audio{PeakDB: &sfxPeak}}); err != nil {
			return err
		}
	}
	musicPeak := -6.0
	const path = "sounds/music_loop.wav"
	if err := addWAV(files, path, music()); err != nil {
		return err
	}
	return addSidecar(files, path, &importsettings.File{Audio: &importsettings.Audio{Format: "wav", PeakDB: &musicPeak, Loop: &importsettings.Loop{}}})
}

// addWAV scales pcm to a peak of 0.8 and writes it as a WAV file.
func addWAV(files map[string][]byte, path string, pcm []float64) error {
	peak := 0.0
	for _, s := range pcm {
		peak = max(peak, s, -s)
	}
	if peak == 0 {
		return fmt.Errorf("%s is silent", path)
	}
	gain := 0.8 / peak
	for i, s := range pcm {
		pcm[i] = s * gain
	}
	data, err := sound.EncodeWAV(pcm, SampleRate, 1)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	files[path] = data
	return nil
}

// The oscillators take a phase in cycles.

func square(phase, duty float64) float64 {
	if phase-float64(int(phase)) < duty {
		return 1
	}
	return -1
}

func triangle(phase float64) float64 {
	f := phase - float64(int(phase))
	if f < 0.5 {
		return 4*f - 1
	}
	return 3 - 4*f
}

// noise is a linear congruential generator of white noise in [-1, 1).
type noise uint32

func (n *noise) next() float64 {
	*n = *n*1664525 + 1013904223
	return float64(int32(*n)) / (1 << 31)
}

// samples returns the length of seconds in samples.
func samples(seconds float64) int {
	return int(seconds * SampleRate)
}

// decay is an envelope that rises over the first 32 samples and falls linearly to 0 at sample n.
func decay(i, n int) float64 {
	env := 1 - float64(i)/float64(n)
	if i < 32 {
		env = min(env, float64(i)/32)
	}
	return env
}

// sweep is a square wave gliding from f0 to f1 Hz over seconds, with a linear decay.
func sweep(seconds, f0, f1, duty float64) []float64 {
	n := samples(seconds)
	out := make([]float64, n)
	phase := 0.0
	for i := range out {
		t := float64(i) / float64(n)
		freq := f0 + float64((f1-f0)*t)
		phase += freq / SampleRate
		out[i] = float64(decay(i, n) * square(phase, duty))
	}
	return out
}

// notes plays freqs one after another, each for seconds, as a mix of a square and a triangle wave with a decay per note.
func notes(seconds float64, freqs ...float64) []float64 {
	n := samples(seconds)
	var out []float64
	for _, f := range freqs {
		phase := 0.0
		for i := range n {
			phase += f / SampleRate
			v := float64(0.5*square(phase, 0.25)) + float64(0.5*triangle(phase))
			out = append(out, float64(decay(i, n)*v))
		}
	}
	return out
}

func jump() []float64 { return sweep(0.18, 260, 720, 0.5) }

func shoot() []float64 { return sweep(0.15, 1200, 300, 0.5) }

func click() []float64 { return sweep(0.03, 1800, 1800, 0.5) }

func coin() []float64 { return notes(0.08, 988, 1319, 1319) }

func powerup() []float64 { return notes(0.07, 523.25, 659.26, 783.99, 1046.5) }

func selectSound() []float64 { return notes(0.05, 659.26, 880) }

// hit is a short burst of noise over a low square falling in pitch.
func hit() []float64 {
	n := samples(0.2)
	out := make([]float64, n)
	var rng noise = 7
	phase := 0.0
	for i := range out {
		t := float64(i) / float64(n)
		phase += (140 - float64(80*t)) / SampleRate
		env := decay(i, n)
		env = float64(env * env)
		v := float64(0.6*rng.next()) + float64(0.4*square(phase, 0.5))
		out[i] = float64(env * v)
	}
	return out
}

// explosion is noise through a one-pole low-pass filter that closes as it fades.
func explosion() []float64 {
	n := samples(0.7)
	out := make([]float64, n)
	var rng noise = 11
	y := 0.0
	for i := range out {
		t := float64(i) / float64(n)
		k := 0.5 - float64(0.45*t)
		y += float64(k * (rng.next() - y))
		env := decay(i, n)
		out[i] = float64(float64(env*env) * y)
	}
	return out
}

// Note frequencies in Hz, rounded to hundredths, for the music.
const (
	f2  = 87.31
	g2  = 98.00
	a2  = 110.00
	c3  = 130.81
	f3  = 174.61
	g3  = 196.00
	a3  = 220.00
	c4  = 261.63
	f4  = 349.23
	g4  = 392.00
	a4  = 440.00
	b4  = 493.88
	c5  = 523.25
	d5  = 587.33
	e5  = 659.26
	rst = 0
)

// music is four bars of a loop in A minor, Am F C G: a square bass in eighths, a triangle lead and a noise hat on the off-beats. Every note fades to silence by its end, so the end of the loop meets its start without a click.
func music() []float64 {
	bass := [][2]float64{{a2, a3}, {f2, f3}, {c3, c4}, {g2, g3}}
	lead := [][8]float64{
		{e5, rst, c5, rst, a4, c5, e5, rst},
		{c5, rst, a4, rst, f4, a4, c5, rst},
		{g4, rst, c5, rst, e5, rst, d5, c5},
		{b4, rst, d5, rst, g4, rst, b4, rst},
	}
	out := make([]float64, MusicBeats*beat)
	var rng noise = 3
	for bar := range 4 {
		for step := range 8 {
			start := (bar*8 + step) * eighth
			bf := bass[bar][step%2]
			lf := lead[bar][step]
			bp, lp := 0.0, 0.0
			for i := range eighth {
				env := decay(i, eighth)
				bp += bf / SampleRate
				v := float64(0.3 * square(bp, 0.5))
				if lf != rst {
					lp += lf / SampleRate
					v += float64(0.45 * triangle(lp))
				}
				v = float64(env * v)
				if step%2 == 1 && i < eighth/8 {
					v += float64(0.12 * float64(decay(i, eighth/8)*rng.next()))
				}
				out[start+i] = v
			}
		}
	}
	return out
}
