package sound

import (
	"math"
	"math/bits"

	"github.com/nsgo-dev/nsgo/audio"
)

// RandSource is a source of uniformly distributed pseudo-random uint64 values.
// Any seeded stream implementing Uint64() uint64 (such as math/rand/v2.Source
// or a game/random stream) satisfies RandSource without introducing extra imports.
type RandSource interface {
	Uint64() uint64
}

// xoshiro256ss is a pure-Go deterministic Xoshiro256** generator seeded via SplitMix64.
type xoshiro256ss struct {
	s [4]uint64
}

func newXoshiro256ss(seed uint64) xoshiro256ss {
	var r xoshiro256ss
	z := seed
	for i := range r.s {
		z += 0x9e3779b97f4a7c15
		x := z
		x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
		x = (x ^ (x >> 27)) * 0x94d049bb133111eb
		r.s[i] = x ^ (x >> 31)
	}
	return r
}

func (r *xoshiro256ss) Uint64() uint64 {
	res := bits.RotateLeft64(r.s[1]*5, 7) * 9
	t := r.s[1] << 17
	r.s[2] ^= r.s[0]
	r.s[3] ^= r.s[1]
	r.s[1] ^= r.s[2]
	r.s[0] ^= r.s[3]
	r.s[2] ^= t
	r.s[3] = bits.RotateLeft64(r.s[3], 45)
	return res
}

func randIntN(src RandSource, n int) int {
	if n <= 1 {
		return 0
	}
	un := uint64(n)
	hi, lo := bits.Mul64(src.Uint64(), un)
	if lo < un {
		thresh := -un % un
		for lo < thresh {
			hi, lo = bits.Mul64(src.Uint64(), un)
		}
	}
	return int(hi)
}

func randFloat64(src RandSource) float64 {
	return float64(src.Uint64()>>11) * (1.0 / (1 << 53))
}

func sampleRange(src RandSource, lo, hi float64) float64 {
	u := randFloat64(src)
	if lo == hi {
		return lo
	}
	v := lo + (hi-lo)*u
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// PCM holds interleaved normalized audio samples in [-1, 1] at a fixed sample rate and channel count.
type PCM struct {
	Samples    []float64
	SampleRate int
	Channels   int
}

// DecodePCM decodes a 16-bit signed linear PCM RIFF/WAVE buffer into a PCM buffer.
func DecodePCM(wav []byte) (PCM, error) {
	samples, sr, ch, err := DecodeWAV(wav)
	if err != nil {
		return PCM{}, err
	}
	return PCM{Samples: samples, SampleRate: sr, Channels: ch}, nil
}

// EncodeWAV encodes p as a canonical 16-bit signed linear PCM RIFF/WAVE buffer.
func (p PCM) EncodeWAV() ([]byte, error) {
	return EncodeWAV(p.Samples, p.SampleRate, p.Channels)
}

// Frames returns the number of complete multi-channel frames in p.
func (p PCM) Frames() int {
	if p.Channels <= 0 || len(p.Samples) < p.Channels {
		return 0
	}
	return len(p.Samples) / p.Channels
}

// ResamplePitch resamples pcm by pitch using Catmull-Rom cubic interpolation without mutating pcm.
// A pitch > 1 plays higher and faster (fewer output frames, approximately Frames()/pitch);
// a pitch < 1 plays lower and slower (more output frames). Invalid non-positive or non-finite pitch defaults to 1.
func ResamplePitch(pcm PCM, pitch float64) PCM {
	ch := pcm.Channels
	inFrames := pcm.Frames()
	if pcm.SampleRate <= 0 || ch <= 0 || inFrames == 0 {
		return PCM{SampleRate: pcm.SampleRate, Channels: ch}
	}
	if math.IsNaN(pitch) || math.IsInf(pitch, 0) || pitch <= 0 {
		pitch = 1
	}
	if pitch == 1 {
		cp := make([]float64, inFrames*ch)
		copy(cp, pcm.Samples[:inFrames*ch])
		return PCM{Samples: cp, SampleRate: pcm.SampleRate, Channels: ch}
	}

	outFrames := max(1, int(math.Round(float64(inFrames)/pitch)))
	out := make([]float64, outFrames*ch)
	if inFrames == 1 {
		for f := range outFrames {
			copy(out[f*ch:(f+1)*ch], pcm.Samples[:ch])
		}
		return PCM{Samples: out, SampleRate: pcm.SampleRate, Channels: ch}
	}

	lastFrame := inFrames - 1
	maxPos := float64(lastFrame)
	for f := range outFrames {
		srcPos := float64(f) * pitch
		if srcPos >= maxPos {
			copy(out[f*ch:(f+1)*ch], pcm.Samples[lastFrame*ch:inFrames*ch])
			continue
		}
		idx := int(srcPos)
		t := srcPos - float64(idx)
		i0 := max(idx-1, 0)
		i1 := idx
		i2 := min(idx+1, lastFrame)
		i3 := min(idx+2, lastFrame)
		for c := range ch {
			y0 := pcm.Samples[i0*ch+c]
			y1 := pcm.Samples[i1*ch+c]
			y2 := pcm.Samples[i2*ch+c]
			y3 := pcm.Samples[i3*ch+c]
			a0 := -0.5*y0 + 1.5*y1 - 1.5*y2 + 0.5*y3
			a1 := y0 - 2.5*y1 + 2.0*y2 - 0.5*y3
			a2 := -0.5*y0 + 0.5*y2
			a3 := y1
			out[f*ch+c] = ((a0*t+a1)*t+a2)*t + a3
		}
	}
	return PCM{Samples: out, SampleRate: pcm.SampleRate, Channels: ch}
}

// ApplyGain returns a copy of pcm with every sample scaled by volume.
// Negative volumes clamp to 0; NaN or infinite volumes leave samples unchanged.
func ApplyGain(pcm PCM, volume float64) PCM {
	ch := pcm.Channels
	frames := pcm.Frames()
	if pcm.SampleRate <= 0 || ch <= 0 || frames == 0 {
		return PCM{SampleRate: pcm.SampleRate, Channels: ch}
	}
	n := frames * ch
	out := make([]float64, n)
	if math.IsNaN(volume) || math.IsInf(volume, 0) {
		copy(out, pcm.Samples[:n])
		return PCM{Samples: out, SampleRate: pcm.SampleRate, Channels: ch}
	}
	if volume <= 0 {
		return PCM{Samples: out, SampleRate: pcm.SampleRate, Channels: ch}
	}
	for i := range n {
		out[i] = pcm.Samples[i] * volume
	}
	return PCM{Samples: out, SampleRate: pcm.SampleRate, Channels: ch}
}

// Clip is a single sound variant inside a Cue. It may reference a Mixer Effect
// for engine playback, hold raw PCM samples for pure-Go pitch resampling, or both.
type Clip struct {
	Name   string
	Effect *Effect
	PCM    PCM
}

// EffectClip wraps an Effect as a Clip.
func EffectClip(effect *Effect) Clip { return Clip{Effect: effect} }

// PCMClip wraps a PCM buffer as a Clip.
func PCMClip(pcm PCM) Clip { return Clip{PCM: pcm} }

// CueOptions configures a Cue's clips, pitch and volume randomization ranges,
// repeat avoidance, relative clip weights, and optional external random source.
type CueOptions struct {
	// Clips are the sound variants selected by the cue.
	Clips []Clip

	// PitchMin and PitchMax define the inclusive pitch multiplier range.
	// When both are 0, pitch defaults to [1, 1]. If PitchMin > PitchMax they are swapped.
	PitchMin float64
	PitchMax float64

	// VolumeMin and VolumeMax define the inclusive volume range, clamped to [0, 1].
	// When both are 0, volume defaults to [1, 1]. If VolumeMin > VolumeMax they are swapped.
	VolumeMin float64
	VolumeMax float64

	// NoRepeatWindow is the number of most recent picks excluded from being chosen again.
	// When 0 and AllowRepeat is false, it defaults to 1 (never repeats the same clip twice
	// in a row when len(Clips) >= 2). Values larger than len(Clips)-1 are clamped to len(Clips)-1.
	// Negative values disable repeat avoidance.
	NoRepeatWindow int

	// AllowRepeat disables the default immediate no-repeat behavior when NoRepeatWindow is 0.
	AllowRepeat bool

	// Weights gives optional relative selection weights per clip. When omitted, all clips
	// have equal weight. Missing trailing entries default to 1; non-positive or non-finite
	// entries are treated as 0.
	Weights []float64

	// Source optionally supplies an external deterministic random stream.
	// When nil, Cue uses its internal Xoshiro256** generator seeded with seed.
	Source RandSource

	// Concurrency is the optional concurrency group name applied to Effects created by Mixer.Cue
	// and used as a fallback when playing a Clip whose Effect leaves Concurrency empty.
	Concurrency string

	// Priority is the playback priority applied to Effects created by Mixer.Cue
	// and used as a fallback when playing a Clip whose Effect leaves Priority at 0.
	Priority int
}

// CuePick is the outcome of a single Cue.Pick call.
type CuePick struct {
	Index  int
	Clip   Clip
	Pitch  float64
	Volume float64
}

// Valid reports whether p selected a clip.
func (p CuePick) Valid() bool { return p.Index >= 0 }

// RenderPCM resamples p.Clip.PCM by p.Pitch and scales it by p.Volume.
func (p CuePick) RenderPCM() PCM {
	if !p.Valid() || p.Clip.PCM.Frames() == 0 {
		return PCM{SampleRate: p.Clip.PCM.SampleRate, Channels: p.Clip.PCM.Channels}
	}
	return ApplyGain(ResamplePitch(p.Clip.PCM, p.Pitch), p.Volume)
}

// RenderWAV resamples p.Clip.PCM by p.Pitch, scales it by p.Volume, and encodes it as a 16-bit PCM WAV buffer.
func (p CuePick) RenderWAV() ([]byte, error) {
	return p.RenderPCM().EncodeWAV()
}

// Cue selects and plays sound variants with deterministic pitch and volume variation
// and configurable repeat avoidance.
type Cue struct {
	clips          []Clip
	weights        []float64
	weighted       bool
	pitchMin       float64
	pitchMax       float64
	volumeMin      float64
	volumeMax      float64
	noRepeatWindow int
	concurrency    string
	priority       int

	seed      uint64
	rng       xoshiro256ss
	src       RandSource
	lastIndex int
	history   []int
}

// NewCue creates a Cue seeded with seed and configured by opts.
func NewCue(seed uint64, opts CueOptions) *Cue {
	clips := make([]Clip, len(opts.Clips))
	copy(clips, opts.Clips)

	pitchMin, pitchMax := normalizePitchRange(opts.PitchMin, opts.PitchMax)
	volumeMin, volumeMax := normalizeVolumeRange(opts.VolumeMin, opts.VolumeMax)

	window := opts.NoRepeatWindow
	if window == 0 && !opts.AllowRepeat {
		window = 1
	}
	if window < 0 || len(clips) <= 1 {
		window = 0
	} else if window >= len(clips) {
		window = len(clips) - 1
	}

	var weights []float64
	weighted := false
	if len(opts.Weights) > 0 && len(clips) > 0 {
		weights = make([]float64, len(clips))
		first := -1.0
		allSamePositive := true
		for i := range clips {
			w := 1.0
			if i < len(opts.Weights) {
				w = opts.Weights[i]
			}
			if math.IsNaN(w) || math.IsInf(w, 0) || w < 0 {
				w = 0
			}
			weights[i] = w
			if w <= 0 {
				allSamePositive = false
			} else if first < 0 {
				first = w
			} else if w != first {
				allSamePositive = false
			}
		}
		if !allSamePositive {
			weighted = true
		}
	}

	return &Cue{
		clips:          clips,
		weights:        weights,
		weighted:       weighted,
		pitchMin:       pitchMin,
		pitchMax:       pitchMax,
		volumeMin:      volumeMin,
		volumeMax:      volumeMax,
		noRepeatWindow: window,
		concurrency:    opts.Concurrency,
		priority:       opts.Priority,
		seed:           seed,
		rng:            newXoshiro256ss(seed),
		src:            opts.Source,
		lastIndex:      -1,
		history:        make([]int, 0, window),
	}
}

func normalizePitchRange(lo, hi float64) (float64, float64) {
	if math.IsNaN(lo) || math.IsInf(lo, 0) {
		lo = 0
	}
	if math.IsNaN(hi) || math.IsInf(hi, 0) {
		hi = 0
	}
	if lo <= 0 && hi <= 0 {
		return 1, 1
	}
	if lo <= 0 {
		lo = 1
	}
	if hi <= 0 {
		hi = 1
	}
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

func normalizeVolumeRange(lo, hi float64) (float64, float64) {
	if math.IsNaN(lo) || math.IsInf(lo, 0) {
		lo = 1
	}
	if math.IsNaN(hi) || math.IsInf(hi, 0) {
		hi = 1
	}
	if lo == 0 && hi == 0 {
		return 1, 1
	}
	lo = clampUnit(lo)
	hi = clampUnit(hi)
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

func (c *Cue) source() RandSource {
	if c.src != nil {
		return c.src
	}
	return &c.rng
}

// SetSource replaces the cue's random source. Passing nil restores the internal seeded stream.
func (c *Cue) SetSource(src RandSource) {
	c.src = src
}

// Reset re-seeds the internal generator to seed, restores the internal source,
// and clears the repeat history for deterministic replay.
func (c *Cue) Reset(seed uint64) {
	c.seed = seed
	c.rng = newXoshiro256ss(seed)
	c.src = nil
	c.lastIndex = -1
	c.history = c.history[:0]
}

// Len returns the number of clips in the cue.
func (c *Cue) Len() int { return len(c.clips) }

// LastIndex returns the index of the most recent pick, or -1 if Pick has not been called since creation or Reset.
func (c *Cue) LastIndex() int { return c.lastIndex }

// PitchRange returns the normalized inclusive [min, max] pitch range.
func (c *Cue) PitchRange() (min, max float64) { return c.pitchMin, c.pitchMax }

// VolumeRange returns the normalized inclusive [min, max] volume range.
func (c *Cue) VolumeRange() (min, max float64) { return c.volumeMin, c.volumeMax }

// NoRepeatWindow returns the effective repeat-avoidance window size (0 when disabled).
func (c *Cue) NoRepeatWindow() int { return c.noRepeatWindow }

// Pick deterministically selects the next clip according to the cue's repeat window and weights,
// samples Pitch in [PitchMin, PitchMax] and Volume in [VolumeMin, VolumeMax], and updates repeat history.
// On an empty cue, Pick returns CuePick{Index: -1}.
func (c *Cue) Pick() CuePick {
	if c == nil || len(c.clips) == 0 {
		return CuePick{Index: -1}
	}
	src := c.source()

	eligible := make([]int, 0, len(c.clips))
	for i := range c.clips {
		if !inSlice(c.history, i) {
			eligible = append(eligible, i)
		}
	}
	if len(eligible) == 0 {
		for i := range c.clips {
			eligible = append(eligible, i)
		}
	}

	idx := c.pickIndex(src, eligible)
	pitch := sampleRange(src, c.pitchMin, c.pitchMax)
	volume := sampleRange(src, c.volumeMin, c.volumeMax)

	c.lastIndex = idx
	if c.noRepeatWindow > 0 {
		if len(c.history) < c.noRepeatWindow {
			c.history = append(c.history, idx)
		} else {
			copy(c.history, c.history[1:])
			c.history[len(c.history)-1] = idx
		}
	}

	return CuePick{
		Index:  idx,
		Clip:   c.clips[idx],
		Pitch:  pitch,
		Volume: volume,
	}
}

func inSlice(history []int, idx int) bool {
	for _, h := range history {
		if h == idx {
			return true
		}
	}
	return false
}

func (c *Cue) pickIndex(src RandSource, eligible []int) int {
	if !c.weighted {
		if len(eligible) == 1 {
			return eligible[0]
		}
		return eligible[randIntN(src, len(eligible))]
	}

	var total float64
	for _, idx := range eligible {
		total += c.weights[idx]
	}
	if total > 0 {
		if len(eligible) == 1 {
			return eligible[0]
		}
		return weightedDraw(src, eligible, c.weights, total)
	}

	// All eligible clips have zero weight; relax the repeat window from oldest to newest
	// so positive-weight clips still avoid repeating the most recent pick when possible.
	relaxed := make([]int, 0, len(c.clips))
	for drop := 1; drop <= len(c.history); drop++ {
		excluded := c.history[drop:]
		relaxed = relaxed[:0]
		var relaxedTotal float64
		for i, w := range c.weights {
			if w > 0 && !inSlice(excluded, i) {
				relaxed = append(relaxed, i)
				relaxedTotal += w
			}
		}
		if relaxedTotal > 0 {
			if len(relaxed) == 1 {
				return relaxed[0]
			}
			return weightedDraw(src, relaxed, c.weights, relaxedTotal)
		}
	}

	if len(eligible) == 1 {
		return eligible[0]
	}
	return eligible[randIntN(src, len(eligible))]
}

func weightedDraw(src RandSource, indices []int, weights []float64, total float64) int {
	r := randFloat64(src) * total
	var acc float64
	chosen := -1
	for _, idx := range indices {
		w := weights[idx]
		if w <= 0 {
			continue
		}
		chosen = idx
		acc += w
		if r < acc {
			return idx
		}
	}
	if chosen >= 0 {
		return chosen
	}
	return indices[0]
}

func (c *Cue) mergePlayOptions(effect *Effect, opts PlayOptions) PlayOptions {
	if opts.Concurrency == "" && (effect == nil || effect.Concurrency == "") && c.concurrency != "" {
		opts.Concurrency = c.concurrency
	}
	if !opts.HasPriority && opts.Priority == 0 && (effect == nil || effect.Priority == 0) && c.priority != 0 {
		opts.Priority = c.priority
	}
	return opts
}

// Play selects the next variant via Pick and plays its Effect with the sampled volume and pitch rate,
// respecting the effect's mixer group volume, master volume, background mute, concurrency group, and voice limit.
func (c *Cue) Play() (CuePick, bool) {
	return c.PlayWith(PlayOptions{})
}

// PlayWith behaves like Play while applying per-call concurrency, owner, and priority overrides from opts.
func (c *Cue) PlayWith(opts PlayOptions) (CuePick, bool) {
	if c == nil {
		return CuePick{Index: -1}, false
	}
	pick := c.Pick()
	if !pick.Valid() || pick.Clip.Effect == nil {
		return pick, false
	}
	vol := float32(pick.Volume)
	if vol <= 0 {
		return pick, false
	}
	played := pick.Clip.Effect.PlayWith(audio.Voice{
		Volume: vol,
		Rate:   float32(pick.Pitch),
	}, c.mergePlayOptions(pick.Clip.Effect, opts))
	return pick, played
}

// PlayPCM selects the next variant via Pick and returns its resampled and gain-scaled PCM buffer.
func (c *Cue) PlayPCM() (CuePick, PCM) {
	if c == nil {
		return CuePick{Index: -1}, PCM{}
	}
	pick := c.Pick()
	return pick, pick.RenderPCM()
}

// Stop stops every Effect referenced by c's clips.
func (c *Cue) Stop() {
	if c == nil {
		return
	}
	for _, clip := range c.clips {
		if clip.Effect != nil {
			clip.Effect.Stop()
		}
	}
}

// Cue builds a Cue whose sounds play through m in group with per-sound voice limit.
// Any nil sounds are skipped; any Clips already present in opts are preserved before sounds.
func (m *Mixer) Cue(group string, limit int, seed uint64, opts CueOptions, sounds ...*audio.Sound) *Cue {
	clips := make([]Clip, 0, len(opts.Clips)+len(sounds))
	clips = append(clips, opts.Clips...)
	for _, s := range sounds {
		if s == nil {
			continue
		}
		eff := m.Effect(s, group, limit)
		eff.Concurrency = opts.Concurrency
		eff.Priority = opts.Priority
		clips = append(clips, EffectClip(eff))
	}
	opts.Clips = clips
	return NewCue(seed, opts)
}

// PlayCue plays the next variant of cue through its Mixer Effect.
func (m *Mixer) PlayCue(cue *Cue) (CuePick, bool) {
	if m == nil || cue == nil {
		return CuePick{Index: -1}, false
	}
	return cue.Play()
}

// RenderCuePCM picks the next variant of cue, resamples its PCM by the sampled pitch,
// applies the sampled volume times group's effective mixer gain (0 when muted or in the background),
// and runs group's configured DSP effect chain if any.
func (m *Mixer) RenderCuePCM(group string, cue *Cue) (CuePick, PCM) {
	if m == nil || cue == nil {
		return CuePick{Index: -1}, PCM{}
	}
	pick := cue.Pick()
	if !pick.Valid() || pick.Clip.PCM.Frames() == 0 {
		return pick, PCM{}
	}
	gain := float64(m.gain(group)) * pick.Volume
	out := ApplyGain(ResamplePitch(pick.Clip.PCM, pick.Pitch), gain)
	if chain := m.effects[group]; len(chain) > 0 {
		chain.ProcessSamples(out.Samples, out.SampleRate, out.Channels)
	}
	return pick, out
}
