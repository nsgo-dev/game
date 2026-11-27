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
	// Loop is the region a player repeats, or nil for none. EncodeWAV writes it in a smpl chunk and DecodeWAV reads the first forward loop of one; Resample scales it to the new rate, and ConvertChannels, Process and Clone keep it.
	Loop *Loop
}

// Loop is a region of a Buffer that a player repeats, in sample frames: from Start up to, but not including, End.
type Loop struct {
	// Start is the first frame of the loop.
	Start int
	// End is the frame after the loop's last one; it must be above Start and at most the buffer's Frames.
	End int
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
	if l := b.Loop; l != nil && (l.Start < 0 || l.End <= l.Start || l.End > b.Frames()) {
		return fmt.Errorf("audioconv: loop [%d, %d) is not a non-empty region of the %d frames", l.Start, l.End, b.Frames())
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
		Loop:       b.Loop.clone(),
	}
}

// clone returns a copy of l, or nil when l is nil.
func (l *Loop) clone() *Loop {
	if l == nil {
		return nil
	}
	c := *l
	return &c
}

// scale returns l moved to a buffer of frames frames resampled by ratio, rounding each end to the nearest frame and keeping the loop at least one frame long and inside the buffer; it returns nil when l is nil or the buffer has no frames.
func (l *Loop) scale(ratio float64, frames int) *Loop {
	if l == nil || frames <= 0 {
		return nil
	}
	start := min(max(int(math.Round(float64(l.Start)*ratio)), 0), frames-1)
	end := min(max(int(math.Round(float64(l.End)*ratio)), start+1), frames)
	return &Loop{Start: start, End: end}
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
