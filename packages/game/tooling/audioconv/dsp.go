package audioconv

import (
	"errors"
	"fmt"
	"math"
)

// Options configures audio processing before encoding.
type Options struct {
	// SampleRate resamples audio to this rate in Hz when > 0.
	SampleRate int
	// Channels converts audio to 1 (mono) or 2 (stereo) when > 0.
	Channels int
	// PeakDBFS normalizes peak sample amplitude to this level in dBFS when non-nil (for example -1.0).
	PeakDBFS *float64
	// RMSDBFS normalizes RMS loudness to this level in dBFS when non-nil (for example -14.0), applying a soft-knee limiter when peaks would exceed 0 dBFS.
	RMSDBFS *float64
	// Bitrate sets the target bitrate in kbps for lossy output formats (AAC and MP3), defaulting to 128 when <= 0.
	Bitrate int
}

// Process applies channel conversion, resampling, and loudness normalization to a copy of buf according to opts.
func Process(buf *Buffer, opts Options) (*Buffer, error) {
	if err := buf.Validate(); err != nil {
		return nil, err
	}
	if opts.SampleRate < 0 {
		return nil, fmt.Errorf("audioconv: invalid target sample rate %d", opts.SampleRate)
	}
	if opts.Channels != 0 && opts.Channels != 1 && opts.Channels != 2 {
		return nil, fmt.Errorf("audioconv: unsupported target channel count %d (want 1 or 2)", opts.Channels)
	}
	if opts.PeakDBFS != nil && opts.RMSDBFS != nil {
		return nil, errors.New("audioconv: cannot set both PeakDBFS and RMSDBFS")
	}

	out := buf.Clone()
	var err error

	// Downmix before resampling to save work; upmix after resampling.
	if opts.Channels > 0 && opts.Channels < out.Channels {
		out, err = ConvertChannels(out, opts.Channels)
		if err != nil {
			return nil, err
		}
	}
	if opts.SampleRate > 0 && opts.SampleRate != out.SampleRate {
		out, err = Resample(out, opts.SampleRate)
		if err != nil {
			return nil, err
		}
	}
	if opts.Channels > 0 && opts.Channels != out.Channels {
		out, err = ConvertChannels(out, opts.Channels)
		if err != nil {
			return nil, err
		}
	}

	if opts.PeakDBFS != nil {
		out.NormalizePeak(*opts.PeakDBFS)
	} else if opts.RMSDBFS != nil {
		out.NormalizeRMS(*opts.RMSDBFS)
	}
	return out, nil
}

// NormalizePeak scales b's samples in place so the peak amplitude reaches targetDBFS, returning b for chaining.
// Silent buffers are left unchanged. If targetDBFS is above 0 dBFS, peaks are protected by a soft-knee limiter.
func (b *Buffer) NormalizePeak(targetDBFS float64) *Buffer {
	if b == nil || len(b.Samples) == 0 {
		return b
	}
	peak := b.Peak()
	if peak == 0 {
		return b
	}
	target := dbToLinear(targetDBFS)
	gain := target / peak
	needsLimit := target > 1.0
	for i, s := range b.Samples {
		v := s * gain
		if needsLimit {
			v = softClip(v)
		}
		b.Samples[i] = v
	}
	return b
}

// NormalizeRMS scales b's samples in place so the RMS loudness targets targetDBFS, returning b for chaining.
// Silent buffers are left unchanged. When scaling would push any sample past ±1.0, a smooth C1 soft-knee limiter keeps all samples strictly within (-1, 1) without hard clipping.
func (b *Buffer) NormalizeRMS(targetDBFS float64) *Buffer {
	if b == nil || len(b.Samples) == 0 {
		return b
	}
	rms := b.RMS()
	if rms == 0 {
		return b
	}
	gain := dbToLinear(targetDBFS) / rms
	needsLimit := b.Peak()*gain > 1.0
	for i, s := range b.Samples {
		v := s * gain
		if needsLimit {
			v = softClip(v)
		}
		b.Samples[i] = v
	}
	return b
}

// softClip is a C1-continuous tanh soft-knee limiter: linear up to threshold and bounded below full scale above it.
func softClip(x float64) float64 {
	const threshold = 0.9
	const ceiling = 0.999
	const headroom = ceiling - threshold
	ax := math.Abs(x)
	if ax <= threshold {
		return x
	}
	y := threshold + headroom*math.Tanh((ax-threshold)/headroom)
	if x < 0 {
		return -y
	}
	return y
}

// Resample returns a copy of b resampled to targetRate Hz using Hann-windowed sinc interpolation.
func (b *Buffer) Resample(targetRate int) (*Buffer, error) {
	return Resample(b, targetRate)
}

// Resample returns a new Buffer resampled from buf.SampleRate to targetRate Hz using Hann-windowed sinc interpolation with anti-aliasing when downsampling.
func Resample(buf *Buffer, targetRate int) (*Buffer, error) {
	if err := buf.Validate(); err != nil {
		return nil, err
	}
	if targetRate <= 0 {
		return nil, fmt.Errorf("audioconv: invalid target sample rate %d", targetRate)
	}
	if targetRate == buf.SampleRate {
		return buf.Clone(), nil
	}
	srcFrames := buf.Frames()
	if srcFrames == 0 {
		return &Buffer{
			SampleRate: targetRate,
			Channels:   buf.Channels,
		}, nil
	}

	ratio := float64(targetRate) / float64(buf.SampleRate)
	outFrames := max(1, int(math.Round(float64(srcFrames)*ratio)))
	out := make([]float64, outFrames*buf.Channels)

	// Apply low-pass cutoff at the target Nyquist frequency when downsampling.
	cutoff := 1.0
	const baseRadius = 8
	radius := baseRadius
	if ratio < 1.0 {
		cutoff = ratio
		radius = min(64, int(math.Ceil(float64(baseRadius)/ratio)))
	}
	invRadius := 1.0 / float64(radius)

	channels := buf.Channels
	for i := range outFrames {
		srcPos := float64(i) / ratio
		center := int(math.Floor(srcPos))
		kMin := max(0, center-radius+1)
		kMax := min(srcFrames-1, center+radius)

		var wSum float64
		var acc [2]float64
		for k := kMin; k <= kMax; k++ {
			d := srcPos - float64(k)
			if math.Abs(d) >= float64(radius) {
				continue
			}
			// Hann-windowed sinc kernel.
			win := 0.5 * (1.0 + math.Cos(math.Pi*d*invRadius))
			w := cutoff * sinc(cutoff*d) * win
			wSum += w
			base := k * channels
			for ch := range channels {
				acc[ch] += buf.Samples[base+ch] * w
			}
		}

		dstBase := i * channels
		if wSum != 0 {
			invW := 1.0 / wSum
			for ch := range channels {
				out[dstBase+ch] = acc[ch] * invW
			}
		}
	}

	return &Buffer{
		SampleRate: targetRate,
		Channels:   channels,
		Samples:    out,
	}, nil
}

func sinc(x float64) float64 {
	if math.Abs(x) < 1e-12 {
		return 1.0
	}
	px := math.Pi * x
	return math.Sin(px) / px
}

// ConvertChannels returns a copy of b converted to targetChannels (1 for mono, 2 for stereo).
func (b *Buffer) ConvertChannels(targetChannels int) (*Buffer, error) {
	return ConvertChannels(b, targetChannels)
}

// ConvertChannels converts buf between mono (1) and stereo (2), returning a new Buffer.
func ConvertChannels(buf *Buffer, targetChannels int) (*Buffer, error) {
	if err := buf.Validate(); err != nil {
		return nil, err
	}
	if targetChannels != 1 && targetChannels != 2 {
		return nil, fmt.Errorf("audioconv: unsupported target channel count %d (want 1 or 2)", targetChannels)
	}
	if buf.Channels == targetChannels {
		return buf.Clone(), nil
	}

	frames := buf.Frames()
	out := make([]float64, frames*targetChannels)
	if buf.Channels == 2 && targetChannels == 1 {
		for i := range frames {
			out[i] = 0.5 * (buf.Samples[i*2] + buf.Samples[i*2+1])
		}
	} else {
		// 1 -> 2: duplicate mono to both channels.
		for i := range frames {
			s := buf.Samples[i]
			out[i*2] = s
			out[i*2+1] = s
		}
	}

	return &Buffer{
		SampleRate: buf.SampleRate,
		Channels:   targetChannels,
		Samples:    out,
	}, nil
}
