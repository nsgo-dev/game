package gamecli

import (
	"context"
	"flag"
	"fmt"

	"github.com/nsgo-dev/game/pkg/tooling/audioconv"
	"github.com/nsgo-dev/game/pkg/tooling/importsettings"
)

// audioCommand normalizes, resamples and converts WAV game audio to WAV, AAC or MP3, taking the settings of the import settings sidecar beside the input under the flags given.
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
		// The sidecar beside the input sets what the flags given leave alone, as game assets applies it to an audio target; a level flag given replaces the sidecar's level of either kind.
		side, err := readSidecar(*inPath, "audio")
		if err != nil {
			return err
		}
		set := setFlags(fs)
		var loop *importsettings.Loop
		var sidePeak, sideRMS *float64
		if side != nil {
			s := side.Audio
			if s.Format != "" && !set["format"] {
				*formatFlag = s.Format
			}
			if s.Rate != 0 && !set["rate"] {
				*rate = s.Rate
			}
			if s.Channels != 0 && !set["channels"] {
				*channels = s.Channels
			}
			if s.Bitrate != 0 && !set["bitrate"] {
				*bitrate = s.Bitrate
			}
			if !set["peak-db"] && !set["rms-db"] {
				sidePeak, sideRMS = s.PeakDB, s.RMSDB
			}
			loop = s.Loop
		}
		format, err := audioconv.ParseFormat(*formatFlag)
		if err != nil {
			return usagef("%v", err)
		}
		if loop != nil && format != audioconv.FormatWAV {
			return fmt.Errorf("%s: loop points need wav output, but %s is %s, which carries none", *inPath+importsettings.Suffix, *outPath, format)
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
		if opts.PeakDBFS == nil && opts.RMSDBFS == nil {
			opts.PeakDBFS, opts.RMSDBFS = sidePeak, sideRMS
		}

		buf, err := audioconv.ReadWAVFile(*inPath)
		if err != nil {
			return err
		}
		if loop != nil {
			end := loop.End
			if end == 0 {
				end = buf.Frames()
			}
			if loop.Start >= end || end > buf.Frames() {
				return fmt.Errorf("%s: loop from frame %d to %d is not inside %s's %d frames", *inPath+importsettings.Suffix, loop.Start, end, *inPath, buf.Frames())
			}
			buf.Loop = &audioconv.Loop{Start: loop.Start, End: end}
		}
		if err := a.audio.ConvertBuffer(context.Background(), buf, *outPath, format, opts); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "wrote %s (%s)\n", *outPath, format)
		return nil
	}
}
