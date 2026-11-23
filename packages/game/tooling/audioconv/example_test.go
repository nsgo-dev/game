package audioconv_test

import (
	"bytes"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/tooling/audioconv"
)

// Process prepares a recording for the game: it resamples, downmixes and normalizes the peak level, all in Go without external tools.
func ExampleProcess() {
	src := &audioconv.Buffer{SampleRate: 44100, Channels: 2, Samples: make([]float64, 2*44100)}
	for i := range 44100 {
		v := 0.25 * math.Sin(2*math.Pi*440*float64(i)/44100)
		src.Samples[2*i], src.Samples[2*i+1] = v, v
	}
	peak := -1.0
	out, err := audioconv.Process(src, audioconv.Options{SampleRate: 22050, Channels: 1, PeakDBFS: &peak})
	if err != nil {
		fmt.Println(err)
		return
	}
	maxAbs := 0.0
	for _, s := range out.Samples {
		maxAbs = max(maxAbs, math.Abs(s))
	}
	fmt.Printf("%d Hz, %d channel, %d frames, peak %.1f dBFS\n", out.SampleRate, out.Channels, out.Frames(), 20*math.Log10(maxAbs))
	// Output:
	// 22050 Hz, 1 channel, 22050 frames, peak -1.0 dBFS
}

// EncodeWAV writes a Buffer as a WAV file and DecodeWAV reads one back.
func ExampleEncodeWAV() {
	buf := &audioconv.Buffer{SampleRate: 8000, Channels: 1, Samples: []float64{0, 0.5, -0.5, 0}}
	var wav bytes.Buffer
	if err := audioconv.EncodeWAV(&wav, buf); err != nil {
		fmt.Println(err)
		return
	}
	back, err := audioconv.DecodeWAV(&wav)
	fmt.Println(err, back.SampleRate, back.Channels, back.Frames())
	// Output:
	// <nil> 8000 1 4
}
