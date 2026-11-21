package sound

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/audio"
)

// BusEffect transforms interleaved normalized PCM samples in [-1, 1] in place.
// Implementations must keep per-run filter and delay state local to each call so an effect can be reused across buffers, sample rates, and channel layouts.
type BusEffect interface {
	ProcessSamples(samples []float64, sampleRate, channels int)
}

// EffectChain applies a sequence of bus effects in order and itself satisfies BusEffect.
type EffectChain []BusEffect

// ProcessSamples runs every non-nil effect in c in order on interleaved samples.
func (c EffectChain) ProcessSamples(samples []float64, sampleRate, channels int) {
	ProcessSamples(samples, sampleRate, channels, c...)
}

// ProcessWAV decodes a 16-bit PCM WAV buffer, runs c over its samples, and re-encodes a 16-bit PCM WAV buffer.
func (c EffectChain) ProcessWAV(wav []byte) ([]byte, error) {
	return ProcessWAV(wav, c...)
}

// ProcessSamples runs each non-nil effect in chain in order on interleaved normalized PCM samples in [-1, 1].
func ProcessSamples(samples []float64, sampleRate, channels int, chain ...BusEffect) {
	if sampleRate <= 0 || channels <= 0 || len(samples) < channels || len(chain) == 0 {
		return
	}
	for _, fx := range chain {
		if fx != nil {
			fx.ProcessSamples(samples, sampleRate, channels)
		}
	}
}

// ProcessWAV decodes a 16-bit PCM WAV buffer, applies chain to its samples, and returns a re-encoded 16-bit PCM WAV buffer.
func ProcessWAV(wav []byte, chain ...BusEffect) ([]byte, error) {
	samples, sampleRate, channels, err := DecodeWAV(wav)
	if err != nil {
		return nil, err
	}
	ProcessSamples(samples, sampleRate, channels, chain...)
	return EncodeWAV(samples, sampleRate, channels)
}

// DecodeWAV decodes a 16-bit signed linear PCM RIFF/WAVE buffer into interleaved normalized samples in [-1, 1].
func DecodeWAV(wav []byte) (samples []float64, sampleRate, channels int, err error) {
	if len(wav) < 12 || string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return nil, 0, 0, errors.New("sound: invalid WAV header")
	}
	var (
		hasFmt  bool
		hasData bool
		pcm     []byte
	)
	offset := 12
	for offset+8 <= len(wav) {
		id := string(wav[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(wav[offset+4 : offset+8]))
		dataStart := offset + 8
		if size < 0 || dataStart+size > len(wav) {
			return nil, 0, 0, fmt.Errorf("sound: truncated WAV %q chunk", id)
		}
		chunk := wav[dataStart : dataStart+size]
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, 0, errors.New("sound: WAV fmt chunk too short")
			}
			format := binary.LittleEndian.Uint16(chunk[0:2])
			if format != 1 {
				return nil, 0, 0, fmt.Errorf("sound: unsupported WAV format %d, want PCM (1)", format)
			}
			channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
			sampleRate = int(binary.LittleEndian.Uint32(chunk[4:8]))
			bitsPerSample := int(binary.LittleEndian.Uint16(chunk[14:16]))
			if bitsPerSample != 16 {
				return nil, 0, 0, fmt.Errorf("sound: unsupported WAV bit depth %d, want 16", bitsPerSample)
			}
			if channels <= 0 || sampleRate <= 0 {
				return nil, 0, 0, errors.New("sound: invalid WAV channels or sample rate")
			}
			hasFmt = true
		case "data":
			pcm = chunk
			hasData = true
		}
		offset = dataStart + size + (size & 1)
	}
	if !hasFmt {
		return nil, 0, 0, errors.New("sound: WAV missing fmt chunk")
	}
	if !hasData {
		return nil, 0, 0, errors.New("sound: WAV missing data chunk")
	}
	frameBytes := channels * 2
	if len(pcm)%frameBytes != 0 {
		return nil, 0, 0, errors.New("sound: WAV data length not aligned to frame size")
	}
	n := len(pcm) / 2
	samples = make([]float64, n)
	for i := range n {
		s := int16(binary.LittleEndian.Uint16(pcm[i*2 : i*2+2]))
		samples[i] = float64(s) / 32768.0
	}
	return samples, sampleRate, channels, nil
}

// EncodeWAV encodes interleaved normalized samples in [-1, 1] as a canonical 16-bit signed linear PCM RIFF/WAVE buffer.
func EncodeWAV(samples []float64, sampleRate, channels int) ([]byte, error) {
	if sampleRate <= 0 || channels <= 0 {
		return nil, errors.New("sound: sampleRate and channels must be positive")
	}
	if len(samples)%channels != 0 {
		return nil, errors.New("sound: sample count must be a multiple of channels")
	}
	dataSize := uint64(len(samples)) * 2
	if dataSize+36 > math.MaxUint32 {
		return nil, errors.New("sound: WAV buffer exceeds maximum RIFF size")
	}
	out := make([]byte, 44+int(dataSize))
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+dataSize))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(out[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(out[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(sampleRate*channels*2))
	binary.LittleEndian.PutUint16(out[32:34], uint16(channels*2))
	binary.LittleEndian.PutUint16(out[34:36], 16)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(dataSize))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[44+i*2:46+i*2], uint16(floatToPCM16(s)))
	}
	return out, nil
}

func floatToPCM16(x float64) int16 {
	if math.IsNaN(x) {
		return 0
	}
	v := math.Round(x * 32768.0)
	if v >= 32767 {
		return 32767
	}
	if v <= -32768 {
		return -32768
	}
	return int16(v)
}

// SetGroupEffects configures the DSP bus effect chain for group, replacing any previous chain. Passing no effects clears group's chain.
func (m *Mixer) SetGroupEffects(group string, effects ...BusEffect) {
	if len(effects) == 0 {
		delete(m.effects, group)
		return
	}
	if m.effects == nil {
		m.effects = map[string]EffectChain{}
	}
	chain := make(EffectChain, len(effects))
	copy(chain, effects)
	m.effects[group] = chain
}

// GroupEffects returns a copy of the DSP bus effect chain configured for group, or nil when none is set.
func (m *Mixer) GroupEffects(group string) EffectChain {
	chain := m.effects[group]
	if len(chain) == 0 {
		return nil
	}
	out := make(EffectChain, len(chain))
	copy(out, chain)
	return out
}

// ProcessSource applies group's configured DSP bus chain to an in-memory 16-bit PCM WAV audio.Source and returns a new audio.Source containing the processed WAV bytes. When group has no effects configured, src is returned unchanged.
func (m *Mixer) ProcessSource(group string, src audio.Source) (audio.Source, error) {
	chain := m.effects[group]
	if len(chain) == 0 {
		return src, nil
	}
	if len(src.Data) == 0 {
		return audio.Source{}, errors.New("sound: ProcessSource requires in-memory WAV audio.Source.Data")
	}
	processed, err := ProcessWAV(src.Data, chain...)
	if err != nil {
		return audio.Source{}, err
	}
	return audio.Source{Data: processed}, nil
}

type biquadKind int

const (
	biquadLowPass biquadKind = iota
	biquadHighPass
)

type biquadEffect struct {
	kind     biquadKind
	cutoffHz float64
	q        float64
}

// LowPass returns a 2nd-order RBJ biquad low-pass filter attenuating frequencies above cutoffHz. A non-positive q defaults to Butterworth (1/sqrt(2)).
func LowPass(cutoffHz, q float64) BusEffect {
	return biquadEffect{kind: biquadLowPass, cutoffHz: cutoffHz, q: q}
}

// HighPass returns a 2nd-order RBJ biquad high-pass filter attenuating frequencies below cutoffHz. A non-positive q defaults to Butterworth (1/sqrt(2)).
func HighPass(cutoffHz, q float64) BusEffect {
	return biquadEffect{kind: biquadHighPass, cutoffHz: cutoffHz, q: q}
}

func (b biquadEffect) ProcessSamples(samples []float64, sampleRate, channels int) {
	if sampleRate <= 0 || channels <= 0 || len(samples) < channels || math.IsNaN(b.cutoffHz) {
		return
	}
	nyquist := 0.5 * float64(sampleRate)
	fc := b.cutoffHz
	if fc <= 0 {
		if b.kind == biquadLowPass {
			clear(samples)
		}
		return
	}
	if fc >= nyquist {
		if b.kind == biquadLowPass {
			return
		}
		fc = 0.499 * float64(sampleRate)
	}
	q := b.q
	if math.IsNaN(q) || q <= 0 {
		q = 1 / math.Sqrt2
	}

	w0 := 2 * math.Pi * fc / float64(sampleRate)
	cosW0 := math.Cos(w0)
	sinW0 := math.Sin(w0)
	alpha := sinW0 / (2 * q)

	var b0, b1, b2 float64
	switch b.kind {
	case biquadLowPass:
		b0 = (1 - cosW0) / 2
		b1 = 1 - cosW0
		b2 = b0
	case biquadHighPass:
		b0 = (1 + cosW0) / 2
		b1 = -(1 + cosW0)
		b2 = b0
	}
	a0 := 1 + alpha
	a1 := -2 * cosW0
	a2 := 1 - alpha

	b0 /= a0
	b1 /= a0
	b2 /= a0
	a1 /= a0
	a2 /= a0

	for c := range channels {
		var z1, z2 float64
		for i := c; i < len(samples); i += channels {
			x := samples[i]
			y := b0*x + z1
			z1 = b1*x - a1*y + z2
			z2 = b2*x - a2*y
			samples[i] = y
		}
	}
}

type delayEffect struct {
	delay    time.Duration
	feedback float64
	wet      float64
}

// Delay returns a feedback delay line with echo spacing delay, feedback clamped to [0, 0.99], and dry/wet mix wet clamped to [0, 1].
func Delay(delay time.Duration, feedback, wet float64) BusEffect {
	return delayEffect{delay: delay, feedback: feedback, wet: wet}
}

func (d delayEffect) ProcessSamples(samples []float64, sampleRate, channels int) {
	if sampleRate <= 0 || channels <= 0 || len(samples) < channels || d.delay <= 0 {
		return
	}
	wet := clampUnit(d.wet)
	if wet <= 0 {
		return
	}
	feedback := clampRange(d.feedback, 0, 0.99)
	delayFrames := int(math.Round(d.delay.Seconds() * float64(sampleRate)))
	if delayFrames <= 0 {
		return
	}
	dry := 1 - wet
	frames := len(samples) / channels
	for c := range channels {
		buf := make([]float64, delayFrames)
		pos := 0
		for f := range frames {
			idx := f*channels + c
			in := samples[idx]
			delayed := buf[pos]
			buf[pos] = in + delayed*feedback
			pos++
			if pos >= delayFrames {
				pos = 0
			}
			samples[idx] = in*dry + delayed*wet
		}
	}
}

type reverbEffect struct {
	roomSize float64
	damping  float64
	wet      float64
}

// Reverb returns a Schroeder/Freeverb-style comb and allpass reverb with roomSize, damping, and wet clamped to [0, 1].
func Reverb(roomSize, damping, wet float64) BusEffect {
	return reverbEffect{roomSize: roomSize, damping: damping, wet: wet}
}

var (
	freeverbCombTunings    = [8]int{1116, 1188, 1277, 1356, 1422, 1491, 1557, 1617}
	freeverbAllpassTunings = [4]int{556, 441, 341, 225}
)

type combFilter struct {
	buf      []float64
	idx      int
	filterSt float64
}

func (c *combFilter) process(in, feedback, damp1, damp2 float64) float64 {
	out := c.buf[c.idx]
	c.filterSt = out*damp2 + c.filterSt*damp1
	c.buf[c.idx] = in + c.filterSt*feedback
	c.idx++
	if c.idx >= len(c.buf) {
		c.idx = 0
	}
	return out
}

type allpassFilter struct {
	buf []float64
	idx int
}

func (a *allpassFilter) process(in float64) float64 {
	bufOut := a.buf[a.idx]
	out := -in + bufOut
	a.buf[a.idx] = in + bufOut*0.5
	a.idx++
	if a.idx >= len(a.buf) {
		a.idx = 0
	}
	return out
}

func (r reverbEffect) ProcessSamples(samples []float64, sampleRate, channels int) {
	if sampleRate <= 0 || channels <= 0 || len(samples) < channels {
		return
	}
	wet := clampUnit(r.wet)
	if wet <= 0 {
		return
	}
	roomSize := clampUnit(r.roomSize)
	damping := clampUnit(r.damping)

	const (
		fixedGain  = 0.015
		scaleWet   = 3.0
		scaleDamp  = 0.4
		scaleRoom  = 0.28
		offsetRoom = 0.7
		stereoDiff = 23
	)
	feedback := offsetRoom + roomSize*scaleRoom
	damp1 := damping * scaleDamp
	damp2 := 1 - damp1
	dry := 1 - wet
	wetGain := wet * scaleWet
	rateScale := float64(sampleRate) / 44100.0
	frames := len(samples) / channels

	for c := range channels {
		spread := 0
		if c%2 == 1 {
			spread = stereoDiff
		}
		var combs [8]combFilter
		for k, base := range freeverbCombTunings {
			n := max(1, int(math.Round(float64(base+spread)*rateScale)))
			combs[k] = combFilter{buf: make([]float64, n)}
		}
		var allpasses [4]allpassFilter
		for k, base := range freeverbAllpassTunings {
			n := max(1, int(math.Round(float64(base+spread)*rateScale)))
			allpasses[k] = allpassFilter{buf: make([]float64, n)}
		}
		for f := range frames {
			idx := f*channels + c
			in := samples[idx]
			scaled := in * fixedGain
			var acc float64
			for k := range combs {
				acc += combs[k].process(scaled, feedback, damp1, damp2)
			}
			for k := range allpasses {
				acc = allpasses[k].process(acc)
			}
			samples[idx] = in*dry + acc*wetGain
		}
	}
}

type gainEffect struct {
	factor float64
}

// Gain returns an effect that scales every sample by factor.
func Gain(factor float64) BusEffect {
	return gainEffect{factor: factor}
}

func (g gainEffect) ProcessSamples(samples []float64, sampleRate, channels int) {
	if sampleRate <= 0 || channels <= 0 || len(samples) < channels || math.IsNaN(g.factor) || math.IsInf(g.factor, 0) {
		return
	}
	for i := range samples {
		samples[i] *= g.factor
	}
}

type bitcrushEffect struct {
	bits       int
	downsample int
}

// Bitcrush returns a retro lo-fi effect that quantizes samples to bits precision (1 to 15; values outside that range leave bit depth unchanged) and holds every downsample-th frame (values <= 1 disable downsampling).
func Bitcrush(bits int, downsample int) BusEffect {
	return bitcrushEffect{bits: bits, downsample: downsample}
}

func (b bitcrushEffect) ProcessSamples(samples []float64, sampleRate, channels int) {
	if sampleRate <= 0 || channels <= 0 || len(samples) < channels {
		return
	}
	quantize := b.bits >= 1 && b.bits < 16
	levels := math.Exp2(float64(b.bits - 1))
	hold := max(b.downsample, 1)
	if !quantize && hold <= 1 {
		return
	}
	frames := len(samples) / channels
	for c := range channels {
		var held float64
		for f := range frames {
			idx := f*channels + c
			if f%hold == 0 {
				v := samples[idx]
				if quantize {
					v = math.Round(v*levels) / levels
				}
				held = v
			}
			samples[idx] = held
		}
	}
}

func clampUnit(v float64) float64 {
	return clampRange(v, 0, 1)
}

func clampRange(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return lo
	}
	return min(max(v, lo), hi)
}
