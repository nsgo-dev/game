package audioconv

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// Buffer holds interleaved floating-point PCM audio samples normalized to [-1, 1].
type Buffer struct {
	// SampleRate is the number of frames per second, such as 44100 or 22050.
	SampleRate int
	// Channels is 1 for mono or 2 for stereo.
	Channels int
	// Samples stores interleaved channel samples of length Frames() * Channels.
	Samples []float64
}

// Validate checks that b describes a supported mono or stereo PCM buffer.
func (b *Buffer) Validate() error {
	if b == nil {
		return errors.New("audioconv: nil audio buffer")
	}
	if b.SampleRate <= 0 {
		return fmt.Errorf("audioconv: invalid sample rate %d", b.SampleRate)
	}
	if b.Channels != 1 && b.Channels != 2 {
		return fmt.Errorf("audioconv: unsupported channel count %d (want 1 or 2)", b.Channels)
	}
	if len(b.Samples)%b.Channels != 0 {
		return fmt.Errorf("audioconv: sample count %d is not a multiple of %d channels", len(b.Samples), b.Channels)
	}
	return nil
}

// Frames returns the number of multi-channel sample frames in b.
func (b *Buffer) Frames() int {
	if b == nil || b.Channels <= 0 {
		return 0
	}
	return len(b.Samples) / b.Channels
}

// Duration returns the playback duration of b.
func (b *Buffer) Duration() time.Duration {
	if b == nil || b.SampleRate <= 0 || b.Channels <= 0 {
		return 0
	}
	return time.Duration(float64(b.Frames()) / float64(b.SampleRate) * float64(time.Second))
}

// Clone returns a deep copy of b.
func (b *Buffer) Clone() *Buffer {
	if b == nil {
		return nil
	}
	cp := make([]float64, len(b.Samples))
	copy(cp, b.Samples)
	return &Buffer{
		SampleRate: b.SampleRate,
		Channels:   b.Channels,
		Samples:    cp,
	}
}

// Peak returns the maximum absolute sample amplitude in b.
func (b *Buffer) Peak() float64 {
	if b == nil || len(b.Samples) == 0 {
		return 0
	}
	var peak float64
	for _, s := range b.Samples {
		if a := math.Abs(s); a > peak {
			peak = a
		}
	}
	return peak
}

// PeakDBFS returns the peak amplitude in dBFS, or -Inf for silence.
func (b *Buffer) PeakDBFS() float64 {
	return linearToDB(b.Peak())
}

// RMS returns the root-mean-square amplitude across all samples in b.
func (b *Buffer) RMS() float64 {
	if b == nil || len(b.Samples) == 0 {
		return 0
	}
	var sumSq float64
	for _, s := range b.Samples {
		sumSq += s * s
	}
	return math.Sqrt(sumSq / float64(len(b.Samples)))
}

// RMSDBFS returns the root-mean-square amplitude in dBFS, or -Inf for silence.
func (b *Buffer) RMSDBFS() float64 {
	return linearToDB(b.RMS())
}

func dbToLinear(db float64) float64 {
	return math.Pow(10, db/20.0)
}

func linearToDB(v float64) float64 {
	if v <= 0 {
		return math.Inf(-1)
	}
	return 20.0 * math.Log10(v)
}
