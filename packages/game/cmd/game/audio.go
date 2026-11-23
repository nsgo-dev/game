package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/tooling/audioconv"
)

// audioCommand normalizes, resamples and converts WAV game audio to WAV, AAC or MP3.
var audioCommand = &command{
	name:    "audio",
	summary: "Normalize, resample and convert a WAV file to WAV, AAC or MP3",
	define:  defineAudio,
	complete: map[string]completion{
		"format": oneOf(string(audioconv.FormatWAV), string(audioconv.FormatAAC), string(audioconv.FormatMP3)),
	},
}

func defineAudio(a *app, fs *flag.FlagSet) func() error {
	inPath := inFlag(fs, "", "input WAV file path (required)")
	outPath := outFlag(fs, "", "output audio file path (required)")
	formatFlag := fs.String("format", "wav", "output format: wav, aac, or mp3")
	rate := fs.Int("rate", 0, "target sample rate in Hz (0 keeps input rate)")
	channels := fs.Int("channels", 0, "target channel count: 1 (mono), 2 (stereo), or 0 (keep input)")
	peakDB := fs.Float64("peak-db", 0, "normalize peak amplitude to this level in dBFS (e.g. -1)")
	rmsDB := fs.Float64("rms-db", 0, "normalize RMS loudness to this level in dBFS with soft-clip protection (e.g. -14)")
	bitrate := fs.Int("bitrate", 128, "target bitrate in kbps for aac and mp3 output")

	return func() error {
		if *inPath == "" || *outPath == "" {
			return usagef("-in and -out are required")
		}
		if fs.NArg() > 0 {
			return usagef("unexpected arguments %q", fs.Args())
		}
		format, err := audioconv.ParseFormat(*formatFlag)
		if err != nil {
			return usagef("%v", err)
		}

		opts := audioconv.Options{
			SampleRate: *rate,
			Channels:   *channels,
			Bitrate:    *bitrate,
		}
		// A level of 0 dBFS is a real target, so whether a level was asked for is whether its flag was set.
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "peak-db":
				v := *peakDB
				opts.PeakDBFS = &v
			case "rms-db":
				v := *rmsDB
				opts.RMSDBFS = &v
			}
		})
		if opts.PeakDBFS != nil && opts.RMSDBFS != nil {
			return usagef("-peak-db and -rms-db are mutually exclusive")
		}

		if err := a.audio.ConvertFile(context.Background(), *inPath, *outPath, format, opts); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "wrote %s (%s)\n", *outPath, format)
		return nil
	}
}
