package audioconv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Format names an output audio format.
type Format string

const (
	FormatWAV Format = "wav"
	FormatAAC Format = "aac"
	FormatMP3 Format = "mp3"
)

// ParseFormat parses a case-insensitive format name ("wav", "aac", or "mp3").
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "wav":
		return FormatWAV, nil
	case "aac", "m4a":
		return FormatAAC, nil
	case "mp3":
		return FormatMP3, nil
	default:
		return "", fmt.Errorf("audioconv: unsupported format %q (want wav, aac, or mp3)", s)
	}
}

// ErrEncoderNotFound is returned when no external encoder (ffmpeg, or afconvert on macOS for AAC) is found on PATH.
var ErrEncoderNotFound = errors.New("audioconv: external audio encoder not found on PATH")

// CommandRunner locates and executes external encoder commands. Provide a custom implementation on Encoder to test encoding without spawning real subprocesses.
type CommandRunner interface {
	LookPath(file string) (string, error)
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Encoder converts PCM audio to WAV, AAC, or MP3. WAV encoding runs in pure Go; AAC and MP3 delegate to ffmpeg (or macOS afconvert for AAC) via Runner.
type Encoder struct {
	// Runner invokes external encoder binaries; uses os/exec when nil.
	Runner CommandRunner
	// GOOS overrides runtime.GOOS when deciding whether macOS afconvert may be used.
	GOOS string
}

var defaultEncoder = &Encoder{}

func (e *Encoder) runner() CommandRunner {
	if e != nil && e.Runner != nil {
		return e.Runner
	}
	return execRunner{}
}

func (e *Encoder) goos() string {
	if e != nil && e.GOOS != "" {
		return e.GOOS
	}
	return runtime.GOOS
}

// ConvertToAAC converts the WAV file at wavPath to AAC at outPath using ffmpeg or macOS afconvert.
func ConvertToAAC(ctx context.Context, wavPath, outPath string, bitrateKbps int) error {
	return defaultEncoder.ConvertToAAC(ctx, wavPath, outPath, bitrateKbps)
}

// ConvertToMP3 converts the WAV file at wavPath to MP3 at outPath using ffmpeg.
func ConvertToMP3(ctx context.Context, wavPath, outPath string, bitrateKbps int) error {
	return defaultEncoder.ConvertToMP3(ctx, wavPath, outPath, bitrateKbps)
}

// ConvertBuffer processes buf with opts and writes it to outPath in format.
func ConvertBuffer(ctx context.Context, buf *Buffer, outPath string, format Format, opts Options) error {
	return defaultEncoder.ConvertBuffer(ctx, buf, outPath, format, opts)
}

// ConvertFile reads the WAV file at inPath, applies opts, and writes it to outPath in format.
func ConvertFile(ctx context.Context, inPath, outPath string, format Format, opts Options) error {
	return defaultEncoder.ConvertFile(ctx, inPath, outPath, format, opts)
}

// ConvertToAAC converts the WAV file at wavPath to AAC at outPath using ffmpeg, falling back to afconvert on macOS.
func (e *Encoder) ConvertToAAC(ctx context.Context, wavPath, outPath string, bitrateKbps int) error {
	if bitrateKbps <= 0 {
		bitrateKbps = 128
	}
	name, bin, err := e.tool(FormatAAC)
	if err != nil {
		return err
	}
	args := []string{"-y", "-i", wavPath, "-c:a", "aac", "-b:a", strconv.Itoa(bitrateKbps) + "k", outPath}
	if name == "afconvert" {
		args = []string{"-f", "m4af", "-d", "aac", "-b", strconv.Itoa(bitrateKbps * 1000), wavPath, outPath}
	}
	if out, err := e.runner().Run(ctx, bin, args...); err != nil {
		return fmt.Errorf("audioconv: %s aac: %w (%s)", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// tool returns the name and path of the external encoder ConvertBuffer runs for format, an AAC or MP3 format: ffmpeg when it is on PATH, otherwise afconvert for AAC on macOS. It returns an error wrapping ErrEncoderNotFound when neither is available.
func (e *Encoder) tool(format Format) (name, bin string, err error) {
	r := e.runner()
	if bin, err := r.LookPath("ffmpeg"); err == nil {
		return "ffmpeg", bin, nil
	}
	if format == FormatMP3 {
		return "", "", fmt.Errorf("%w: MP3 requires ffmpeg", ErrEncoderNotFound)
	}
	if e.goos() == "darwin" {
		if bin, err := r.LookPath("afconvert"); err == nil {
			return "afconvert", bin, nil
		}
	}
	return "", "", fmt.Errorf("%w: AAC requires ffmpeg (or afconvert on macOS)", ErrEncoderNotFound)
}

// Tool describes the encoder ConvertBuffer uses for format on this machine, so a caller that caches converted files can tell when the same input would now convert to different bytes. It is "" for WAV, which is encoded in Go. For AAC and MP3 it names the external encoder and, for ffmpeg, the first line of its -version output, such as "ffmpeg version 7.1.1"; afconvert reports no version, so it is only "afconvert". It returns an error wrapping ErrEncoderNotFound when no encoder is available.
func (e *Encoder) Tool(ctx context.Context, format Format) (string, error) {
	if format == FormatWAV {
		return "", nil
	}
	if format != FormatAAC && format != FormatMP3 {
		return "", fmt.Errorf("audioconv: unsupported format %q", format)
	}
	name, bin, err := e.tool(format)
	if err != nil || name != "ffmpeg" {
		return name, err
	}
	out, err := e.runner().Run(ctx, bin, "-version")
	if err != nil {
		return "", fmt.Errorf("audioconv: ffmpeg -version: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(line), nil
}

// ConvertToMP3 converts the WAV file at wavPath to MP3 at outPath using ffmpeg.
func (e *Encoder) ConvertToMP3(ctx context.Context, wavPath, outPath string, bitrateKbps int) error {
	if bitrateKbps <= 0 {
		bitrateKbps = 128
	}
	_, bin, err := e.tool(FormatMP3)
	if err != nil {
		return err
	}
	args := []string{"-y", "-i", wavPath, "-c:a", "libmp3lame", "-b:a", strconv.Itoa(bitrateKbps) + "k", outPath}
	if out, err := e.runner().Run(ctx, bin, args...); err != nil {
		return fmt.Errorf("audioconv: ffmpeg mp3: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ConvertBuffer processes buf according to opts and writes it to outPath in format.
func (e *Encoder) ConvertBuffer(ctx context.Context, buf *Buffer, outPath string, format Format, opts Options) error {
	processed, err := Process(buf, opts)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	switch format {
	case FormatWAV:
		return WriteWAVFile(outPath, processed)
	case FormatAAC, FormatMP3:
		tmp, err := os.CreateTemp("", "gameaudio-*.wav")
		if err != nil {
			return err
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)
		if err := EncodeWAV(tmp, processed); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		if format == FormatAAC {
			return e.ConvertToAAC(ctx, tmpPath, outPath, opts.Bitrate)
		}
		return e.ConvertToMP3(ctx, tmpPath, outPath, opts.Bitrate)
	default:
		return fmt.Errorf("audioconv: unsupported format %q", format)
	}
}

// ConvertFile reads the WAV file at inPath, applies opts, and writes it to outPath in format.
func (e *Encoder) ConvertFile(ctx context.Context, inPath, outPath string, format Format, opts Options) error {
	buf, err := ReadWAVFile(inPath)
	if err != nil {
		return err
	}
	return e.ConvertBuffer(ctx, buf, outPath, format, opts)
}
