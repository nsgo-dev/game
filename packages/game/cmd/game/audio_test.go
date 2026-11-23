package main

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/audioconv"
)

func writeTestWAV(t *testing.T, path string, rate, channels int, amp float64) {
	t.Helper()
	frames := rate / 10 // 100 ms
	samples := make([]float64, frames*channels)
	for i := range frames {
		v := amp * math.Sin(2*math.Pi*440*float64(i)/float64(rate))
		for ch := range channels {
			samples[i*channels+ch] = v
		}
	}
	buf := &audioconv.Buffer{SampleRate: rate, Channels: channels, Samples: samples}
	if err := audioconv.WriteWAVFile(path, buf); err != nil {
		t.Fatal(err)
	}
}

type stubRunner struct {
	bins    map[string]string
	lastBin string
	lastArg []string
}

func (s *stubRunner) LookPath(file string) (string, error) {
	if p, ok := s.bins[file]; ok {
		return p, nil
	}
	return "", errors.New("not found")
}

func (s *stubRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	s.lastBin = name
	s.lastArg = append([]string(nil), args...)
	outPath := args[len(args)-1]
	return nil, os.WriteFile(outPath, []byte("encoded"), 0o644)
}

func TestAudioConvertsWAVWithDSPFlags(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "sfx.wav")
	outPath := filepath.Join(dir, "sfx_out.wav")
	writeTestWAV(t, inPath, 48000, 2, 0.2)

	code, stderr := runAudio(t, &audioconv.Encoder{}, []string{
		"-in", inPath,
		"-out", outPath,
		"-format", "wav",
		"-rate", "22050",
		"-channels", "1",
		"-peak-db", "-2.0",
	})
	if code != 0 {
		t.Fatalf("exit %d, stderr=%s", code, stderr)
	}

	got, err := audioconv.ReadWAVFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.SampleRate != 22050 || got.Channels != 1 {
		t.Fatalf("got rate=%d ch=%d, want 22050/1", got.SampleRate, got.Channels)
	}
	if diff := math.Abs(got.PeakDBFS() - (-2.0)); diff > 0.01 {
		t.Fatalf("PeakDBFS = %v, want -2.0", got.PeakDBFS())
	}
}

func TestAudioRMSFlagAndExternalFormats(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "music.wav")
	writeTestWAV(t, inPath, 44100, 1, 0.1)

	t.Run("rms wav", func(t *testing.T) {
		outPath := filepath.Join(dir, "music_rms.wav")
		code, stderr := runAudio(t, &audioconv.Encoder{}, []string{
			"-in", inPath,
			"-out", outPath,
			"-rms-db", "-14",
		})
		if code != 0 {
			t.Fatalf("exit %d, stderr=%s", code, stderr)
		}
		got, err := audioconv.ReadWAVFile(outPath)
		if err != nil {
			t.Fatal(err)
		}
		if diff := math.Abs(got.RMSDBFS() - (-14.0)); diff > 0.02 {
			t.Fatalf("RMSDBFS = %v, want -14.0", got.RMSDBFS())
		}
	})

	t.Run("aac via injected runner", func(t *testing.T) {
		outPath := filepath.Join(dir, "music.m4a")
		r := &stubRunner{bins: map[string]string{"ffmpeg": "/usr/bin/ffmpeg"}}
		code, stderr := runAudio(t, &audioconv.Encoder{Runner: r}, []string{
			"-in", inPath,
			"-out", outPath,
			"-format", "aac",
			"-bitrate", "192",
		})
		if code != 0 {
			t.Fatalf("exit %d, stderr=%s", code, stderr)
		}
		if r.lastBin != "/usr/bin/ffmpeg" {
			t.Fatalf("runner bin = %q, want ffmpeg", r.lastBin)
		}
	})

	t.Run("mp3 reports missing encoder cleanly", func(t *testing.T) {
		outPath := filepath.Join(dir, "music.mp3")
		r := &stubRunner{bins: map[string]string{}}
		code, stderr := runAudio(t, &audioconv.Encoder{Runner: r}, []string{
			"-in", inPath,
			"-out", outPath,
			"-format", "mp3",
		})
		if code == 0 {
			t.Fatal("expected non-zero exit when encoder is missing")
		}
		if !strings.Contains(stderr, "not found") {
			t.Fatalf("stderr = %q, want missing encoder error", stderr)
		}
	})
}

func TestAudioValidationErrors(t *testing.T) {
	if code, _ := runAudio(t, &audioconv.Encoder{}, nil); code != 2 {
		t.Fatalf("missing flags exit %d, want 2", code)
	}
	if code, _ := runAudio(t, &audioconv.Encoder{}, []string{"-in", "a.wav", "-out", "b.ogg", "-format", "ogg"}); code != 2 {
		t.Fatalf("bad format exit %d, want 2", code)
	}
	dir := t.TempDir()
	inPath := filepath.Join(dir, "a.wav")
	writeTestWAV(t, inPath, 44100, 1, 0.2)
	if code, _ := runAudio(t, &audioconv.Encoder{}, []string{"-in", inPath, "-out", filepath.Join(dir, "b.wav"), "-peak-db", "-1", "-rms-db", "-14"}); code != 2 {
		t.Fatalf("conflicting peak/rms exit %d, want 2", code)
	}
}

// runAudio runs game audio with args and enc, and returns its exit code and what it wrote to stderr.
func runAudio(t *testing.T, enc *audioconv.Encoder, args []string) (int, string) {
	t.Helper()
	a, _, stderr := newTestApp(t)
	a.audio = enc
	return run(append([]string{"audio"}, args...), a), stderr.String()
}
