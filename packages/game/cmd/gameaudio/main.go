// Command gameaudio normalizes, resamples, and converts WAV game audio assets to WAV, AAC, or MP3.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/nsgo-dev/game/packages/game/tooling/audioconv"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, &audioconv.Encoder{}))
}

func run(args []string, stdout, stderr io.Writer, enc *audioconv.Encoder) int {
	fs := flag.NewFlagSet("gameaudio", flag.ContinueOnError)
	fs.SetOutput(stderr)

	inPath := fs.String("in", "", "input WAV file path (required)")
	outPath := fs.String("out", "", "output audio file path (required)")
	formatFlag := fs.String("format", "wav", "output format: wav, aac, or mp3")
	rate := fs.Int("rate", 0, "target sample rate in Hz (0 keeps input rate)")
	channels := fs.Int("channels", 0, "target channel count: 1 (mono), 2 (stereo), or 0 (keep input)")
	peakDB := fs.Float64("peak-db", 0, "normalize peak amplitude to this level in dBFS (e.g. -1)")
	rmsDB := fs.Float64("rms-db", 0, "normalize RMS loudness to this level in dBFS with soft-clip protection (e.g. -14)")
	bitrate := fs.Int("bitrate", 128, "target bitrate in kbps for aac and mp3 output")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *inPath == "" || *outPath == "" {
		fmt.Fprintln(stderr, "gameaudio: -in and -out are required")
		fs.Usage()
		return 2
	}

	format, err := audioconv.ParseFormat(*formatFlag)
	if err != nil {
		fmt.Fprintf(stderr, "gameaudio: %v\n", err)
		return 2
	}

	var hasPeak, hasRMS bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "peak-db":
			hasPeak = true
		case "rms-db":
			hasRMS = true
		}
	})

	opts := audioconv.Options{
		SampleRate: *rate,
		Channels:   *channels,
		Bitrate:    *bitrate,
	}
	if hasPeak {
		v := *peakDB
		opts.PeakDBFS = &v
	}
	if hasRMS {
		v := *rmsDB
		opts.RMSDBFS = &v
	}

	if enc == nil {
		enc = &audioconv.Encoder{}
	}
	if err := enc.ConvertFile(context.Background(), *inPath, *outPath, format, opts); err != nil {
		fmt.Fprintf(stderr, "gameaudio: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s (%s)\n", *outPath, format)
	return 0
}
