package audioconv_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/audioconv"
)

func sineBuffer(sampleRate, channels int, freqHz, amp float64, frames int) *audioconv.Buffer {
	samples := make([]float64, frames*channels)
	for i := range frames {
		v := amp * math.Sin(2*math.Pi*freqHz*float64(i)/float64(sampleRate))
		for ch := range channels {
			samples[i*channels+ch] = v
		}
	}
	return &audioconv.Buffer{
		SampleRate: sampleRate,
		Channels:   channels,
		Samples:    samples,
	}
}

// buildRawWAV constructs a RIFF/WAVE byte slice with an arbitrary format chunk, optional extra odd-length chunk, and raw sample payload.
func buildRawWAV(audioFormat, channels uint16, sampleRate uint32, bitsPerSample uint16, data []byte, extraChunk bool, subGUID *[16]byte) []byte {
	var fmtChunk bytes.Buffer
	blockAlign := channels * (bitsPerSample / 8)
	byteRate := sampleRate * uint32(blockAlign)
	_ = binary.Write(&fmtChunk, binary.LittleEndian, audioFormat)
	_ = binary.Write(&fmtChunk, binary.LittleEndian, channels)
	_ = binary.Write(&fmtChunk, binary.LittleEndian, sampleRate)
	_ = binary.Write(&fmtChunk, binary.LittleEndian, byteRate)
	_ = binary.Write(&fmtChunk, binary.LittleEndian, blockAlign)
	_ = binary.Write(&fmtChunk, binary.LittleEndian, bitsPerSample)
	if subGUID != nil {
		_ = binary.Write(&fmtChunk, binary.LittleEndian, uint16(22)) // cbSize
		_ = binary.Write(&fmtChunk, binary.LittleEndian, bitsPerSample)
		_ = binary.Write(&fmtChunk, binary.LittleEndian, uint32(3)) // channel mask
		fmtChunk.Write(subGUID[:])
	}

	var body bytes.Buffer
	body.WriteString("WAVE")

	if extraChunk {
		// Odd-sized JUNK chunk (5 bytes + 1 pad byte) to verify RIFF alignment handling.
		body.WriteString("JUNK")
		_ = binary.Write(&body, binary.LittleEndian, uint32(5))
		body.Write([]byte{1, 2, 3, 4, 5, 0})
	}

	body.WriteString("fmt ")
	_ = binary.Write(&body, binary.LittleEndian, uint32(fmtChunk.Len()))
	body.Write(fmtChunk.Bytes())

	body.WriteString("data")
	_ = binary.Write(&body, binary.LittleEndian, uint32(len(data)))
	body.Write(data)
	if len(data)%2 == 1 {
		body.WriteByte(0)
	}

	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

func TestDecodeWAVFormats(t *testing.T) {
	want := []float64{0.0, 0.5, -0.5, 0.25}

	t.Run("8-bit unsigned mono", func(t *testing.T) {
		raw := make([]byte, len(want))
		for i, s := range want {
			raw[i] = byte(math.Round(s*128.0 + 128.0))
		}
		wav := buildRawWAV(1, 1, 22050, 8, raw, true, nil)
		buf, err := audioconv.DecodeWAV(bytes.NewReader(wav))
		if err != nil {
			t.Fatal(err)
		}
		if buf.SampleRate != 22050 || buf.Channels != 1 || len(buf.Samples) != len(want) {
			t.Fatalf("got rate=%d ch=%d len=%d", buf.SampleRate, buf.Channels, len(buf.Samples))
		}
		for i, w := range want {
			if math.Abs(buf.Samples[i]-w) > 0.01 {
				t.Errorf("sample %d = %v, want %v", i, buf.Samples[i], w)
			}
		}
	})

	t.Run("16-bit signed stereo", func(t *testing.T) {
		var raw bytes.Buffer
		for _, s := range want {
			_ = binary.Write(&raw, binary.LittleEndian, int16(math.Round(s*32768.0)))
		}
		wav := buildRawWAV(1, 2, 44100, 16, raw.Bytes(), false, nil)
		buf, err := audioconv.DecodeWAV(bytes.NewReader(wav))
		if err != nil {
			t.Fatal(err)
		}
		if buf.SampleRate != 44100 || buf.Channels != 2 || buf.Frames() != 2 {
			t.Fatalf("got rate=%d ch=%d frames=%d", buf.SampleRate, buf.Channels, buf.Frames())
		}
		for i, w := range want {
			if math.Abs(buf.Samples[i]-w) > 1e-4 {
				t.Errorf("sample %d = %v, want %v", i, buf.Samples[i], w)
			}
		}
	})

	t.Run("24-bit signed stereo", func(t *testing.T) {
		raw := make([]byte, len(want)*3)
		for i, s := range want {
			v := int32(math.Round(s * 8388608.0))
			raw[i*3] = byte(v)
			raw[i*3+1] = byte(v >> 8)
			raw[i*3+2] = byte(v >> 16)
		}
		wav := buildRawWAV(1, 2, 48000, 24, raw, true, nil)
		buf, err := audioconv.DecodeWAV(bytes.NewReader(wav))
		if err != nil {
			t.Fatal(err)
		}
		if buf.SampleRate != 48000 || buf.Channels != 2 {
			t.Fatalf("got rate=%d ch=%d", buf.SampleRate, buf.Channels)
		}
		for i, w := range want {
			if math.Abs(buf.Samples[i]-w) > 1e-6 {
				t.Errorf("sample %d = %v, want %v", i, buf.Samples[i], w)
			}
		}
	})

	t.Run("32-bit IEEE float mono", func(t *testing.T) {
		var raw bytes.Buffer
		for _, s := range want {
			_ = binary.Write(&raw, binary.LittleEndian, math.Float32bits(float32(s)))
		}
		wav := buildRawWAV(3, 1, 44100, 32, raw.Bytes(), false, nil)
		buf, err := audioconv.DecodeWAV(bytes.NewReader(wav))
		if err != nil {
			t.Fatal(err)
		}
		for i, w := range want {
			if math.Abs(buf.Samples[i]-w) > 1e-6 {
				t.Errorf("sample %d = %v, want %v", i, buf.Samples[i], w)
			}
		}
	})

	t.Run("WAVE_FORMAT_EXTENSIBLE 32-bit float", func(t *testing.T) {
		var raw bytes.Buffer
		for _, s := range want {
			_ = binary.Write(&raw, binary.LittleEndian, math.Float32bits(float32(s)))
		}
		guidFloat := [16]byte{
			0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00,
			0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71,
		}
		wav := buildRawWAV(0xFFFE, 2, 48000, 32, raw.Bytes(), false, &guidFloat)
		buf, err := audioconv.DecodeWAV(bytes.NewReader(wav))
		if err != nil {
			t.Fatal(err)
		}
		if buf.SampleRate != 48000 || buf.Channels != 2 || len(buf.Samples) != len(want) {
			t.Fatalf("unexpected decoded extensible buffer: rate=%d ch=%d len=%d", buf.SampleRate, buf.Channels, len(buf.Samples))
		}
	})
}

func TestDecodeWAVRejectsInvalidInput(t *testing.T) {
	if _, err := audioconv.DecodeWAV(bytes.NewReader([]byte("not a wav file at all"))); err == nil {
		t.Fatal("expected error for non-WAV input")
	}
	badCh := buildRawWAV(1, 6, 44100, 16, make([]byte, 12), false, nil)
	if _, err := audioconv.DecodeWAV(bytes.NewReader(badCh)); err == nil {
		t.Fatal("expected error for 6-channel WAV")
	}
}

func TestNormalizePeakAndRMS(t *testing.T) {
	t.Run("peak", func(t *testing.T) {
		buf := sineBuffer(44100, 1, 440, 0.2, 4410)
		buf.NormalizePeak(-3.0)
		if diff := math.Abs(buf.PeakDBFS() - (-3.0)); diff > 1e-6 {
			t.Fatalf("PeakDBFS = %v, want -3.0", buf.PeakDBFS())
		}
	})

	t.Run("rms with headroom", func(t *testing.T) {
		buf := sineBuffer(44100, 1, 440, 0.1, 4410)
		buf.NormalizeRMS(-14.0)
		if diff := math.Abs(buf.RMSDBFS() - (-14.0)); diff > 1e-6 {
			t.Fatalf("RMSDBFS = %v, want -14.0", buf.RMSDBFS())
		}
		if buf.Peak() > 1.0 {
			t.Fatalf("Peak = %v, exceeds 1.0", buf.Peak())
		}
	})

	t.Run("rms soft-clip limiter prevents clipping on high crest factor", func(t *testing.T) {
		// One large transient spike + quiet body: raising RMS to -3 dBFS would push the spike well past 1.0.
		samples := make([]float64, 1000)
		samples[10] = 0.8
		samples[20] = -0.8
		for i := 50; i < len(samples); i++ {
			samples[i] = 0.02 * math.Sin(float64(i))
		}
		buf := &audioconv.Buffer{SampleRate: 44100, Channels: 1, Samples: samples}
		buf.NormalizeRMS(-3.0)
		if buf.Peak() >= 1.0 {
			t.Fatalf("soft limiter failed: peak = %v, want < 1.0", buf.Peak())
		}
		if buf.Samples[10] <= 0.9 || buf.Samples[20] >= -0.9 {
			t.Fatalf("expected transient peaks to be smoothly limited above 0.9, got +%v and %v", buf.Samples[10], buf.Samples[20])
		}
	})

	t.Run("silence unchanged", func(t *testing.T) {
		buf := &audioconv.Buffer{SampleRate: 44100, Channels: 1, Samples: make([]float64, 128)}
		buf.NormalizePeak(-1.0)
		buf.NormalizeRMS(-14.0)
		if buf.Peak() != 0 {
			t.Fatalf("silent buffer changed: peak = %v", buf.Peak())
		}
	})
}

func TestResampleAndConvertChannels(t *testing.T) {
	t.Run("downsample 48000 to 22050 preserves 440Hz sine", func(t *testing.T) {
		src := sineBuffer(48000, 1, 440, 0.75, 4800) // 100 ms
		dst, err := src.Resample(22050)
		if err != nil {
			t.Fatal(err)
		}
		if dst.SampleRate != 22050 || dst.Channels != 1 || dst.Frames() != 2205 {
			t.Fatalf("got rate=%d ch=%d frames=%d, want 22050/1/2205", dst.SampleRate, dst.Channels, dst.Frames())
		}
		// Compare interior samples (away from boundary window) to the ideal 440Hz sine at 22050 Hz.
		var maxErr float64
		for i := 40; i < dst.Frames()-40; i++ {
			want := 0.75 * math.Sin(2*math.Pi*440*float64(i)/22050.0)
			if e := math.Abs(dst.Samples[i] - want); e > maxErr {
				maxErr = e
			}
		}
		if maxErr > 0.01 {
			t.Fatalf("max resampling error = %v, want <= 0.01", maxErr)
		}
	})

	t.Run("upsample 22050 to 44100 preserves DC and sine", func(t *testing.T) {
		dc := &audioconv.Buffer{SampleRate: 22050, Channels: 2, Samples: make([]float64, 200)}
		for i := range dc.Samples {
			dc.Samples[i] = 0.5
		}
		up, err := dc.Resample(44100)
		if err != nil {
			t.Fatal(err)
		}
		if up.SampleRate != 44100 || up.Frames() != 200 {
			t.Fatalf("got rate=%d frames=%d", up.SampleRate, up.Frames())
		}
		for i, s := range up.Samples {
			if math.Abs(s-0.5) > 1e-9 {
				t.Fatalf("sample %d = %v, want 0.5", i, s)
			}
		}
	})

	t.Run("stereo to mono and mono to stereo", func(t *testing.T) {
		stereo := &audioconv.Buffer{
			SampleRate: 44100,
			Channels:   2,
			Samples:    []float64{0.8, -0.2, -0.4, 0.4},
		}
		mono, err := stereo.ConvertChannels(1)
		if err != nil {
			t.Fatal(err)
		}
		if mono.Channels != 1 || len(mono.Samples) != 2 {
			t.Fatalf("got ch=%d len=%d", mono.Channels, len(mono.Samples))
		}
		if math.Abs(mono.Samples[0]-0.3) > 1e-12 || math.Abs(mono.Samples[1]-0.0) > 1e-12 {
			t.Fatalf("mono samples = %v, want [0.3 0]", mono.Samples)
		}

		back, err := mono.ConvertChannels(2)
		if err != nil {
			t.Fatal(err)
		}
		wantBack := []float64{0.3, 0.3, 0, 0}
		for i, w := range wantBack {
			if math.Abs(back.Samples[i]-w) > 1e-12 {
				t.Fatalf("back[%d] = %v, want %v", i, back.Samples[i], w)
			}
		}
	})
}

func TestEndToEndWAVPipelineRoundtrip(t *testing.T) {
	// 48kHz stereo quiet sine -> peak normalize to -1 dBFS, resample to 22050 Hz, convert to mono -> encode 16-bit WAV -> decode & verify!
	orig := sineBuffer(48000, 2, 440, 0.25, 4800)
	peakDB := -1.0
	processed, err := audioconv.Process(orig, audioconv.Options{
		SampleRate: 22050,
		Channels:   1,
		PeakDBFS:   &peakDB,
	})
	if err != nil {
		t.Fatal(err)
	}

	var wavBytes bytes.Buffer
	if err := audioconv.EncodeWAV(&wavBytes, processed); err != nil {
		t.Fatal(err)
	}

	raw := wavBytes.Bytes()
	if len(raw) < 44 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		t.Fatalf("invalid RIFF/WAVE header")
	}
	if binary.LittleEndian.Uint32(raw[4:8]) != uint32(len(raw)-8) {
		t.Fatalf("RIFF chunk size mismatch")
	}
	if binary.LittleEndian.Uint16(raw[20:22]) != 1 || binary.LittleEndian.Uint16(raw[34:36]) != 16 {
		t.Fatalf("encoded WAV is not 16-bit PCM")
	}

	decoded, err := audioconv.DecodeWAV(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.SampleRate != 22050 {
		t.Fatalf("decoded rate = %d, want 22050", decoded.SampleRate)
	}
	if decoded.Channels != 1 {
		t.Fatalf("decoded channels = %d, want 1", decoded.Channels)
	}
	if decoded.Frames() != 2205 {
		t.Fatalf("decoded frames = %d, want 2205", decoded.Frames())
	}
	if diff := math.Abs(decoded.PeakDBFS() - (-1.0)); diff > 0.01 {
		t.Fatalf("decoded PeakDBFS = %v, want -1.0 (±0.01)", decoded.PeakDBFS())
	}
}

type fakeRunner struct {
	bins    map[string]string
	lastBin string
	lastArg []string
	onRun   func(name string, args []string) ([]byte, error)
}

func (f *fakeRunner) LookPath(file string) (string, error) {
	if p, ok := f.bins[file]; ok {
		return p, nil
	}
	return "", errors.New("not found")
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.lastBin = name
	f.lastArg = append([]string(nil), args...)
	if f.onRun != nil {
		return f.onRun(name, args)
	}
	return nil, nil
}

func TestEncoderTool(t *testing.T) {
	ctx := context.Background()
	ffmpeg := &fakeRunner{
		bins: map[string]string{"ffmpeg": "/usr/bin/ffmpeg", "afconvert": "/usr/bin/afconvert"},
		onRun: func(_ string, args []string) ([]byte, error) {
			if !reflect.DeepEqual(args, []string{"-version"}) {
				return nil, errors.New("unexpected arguments")
			}
			return []byte("ffmpeg version 7.1.1 Copyright (c) 2000-2025\nbuilt with clang\n"), nil
		},
	}
	afconvert := &fakeRunner{bins: map[string]string{"afconvert": "/usr/bin/afconvert"}}
	none := &fakeRunner{bins: map[string]string{}}
	for _, tc := range []struct {
		name    string
		runner  *fakeRunner
		goos    string
		format  audioconv.Format
		want    string
		missing bool
	}{
		{"wav needs no encoder", none, "linux", audioconv.FormatWAV, "", false},
		{"aac via ffmpeg", ffmpeg, "darwin", audioconv.FormatAAC, "ffmpeg version 7.1.1 Copyright (c) 2000-2025", false},
		{"mp3 via ffmpeg", ffmpeg, "linux", audioconv.FormatMP3, "ffmpeg version 7.1.1 Copyright (c) 2000-2025", false},
		{"aac via afconvert", afconvert, "darwin", audioconv.FormatAAC, "afconvert", false},
		{"no afconvert off macOS", afconvert, "linux", audioconv.FormatAAC, "", true},
		{"mp3 without ffmpeg", afconvert, "darwin", audioconv.FormatMP3, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enc := &audioconv.Encoder{Runner: tc.runner, GOOS: tc.goos}
			got, err := enc.Tool(ctx, tc.format)
			if tc.missing {
				if !errors.Is(err, audioconv.ErrEncoderNotFound) {
					t.Fatalf("Tool error = %v, want ErrEncoderNotFound", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("Tool = %q, %v, want %q", got, err, tc.want)
			}
		})
	}
}

func TestExternalEncoderRunner(t *testing.T) {
	ctx := context.Background()

	t.Run("AAC via ffmpeg", func(t *testing.T) {
		r := &fakeRunner{bins: map[string]string{"ffmpeg": "/usr/local/bin/ffmpeg"}}
		enc := &audioconv.Encoder{Runner: r, GOOS: "linux"}
		if err := enc.ConvertToAAC(ctx, "in.wav", "out.m4a", 192); err != nil {
			t.Fatal(err)
		}
		wantArgs := []string{"-y", "-i", "in.wav", "-c:a", "aac", "-b:a", "192k", "out.m4a"}
		if r.lastBin != "/usr/local/bin/ffmpeg" || !reflect.DeepEqual(r.lastArg, wantArgs) {
			t.Fatalf("ran %s %v, want ffmpeg %v", r.lastBin, r.lastArg, wantArgs)
		}
	})

	t.Run("AAC falls back to afconvert on macOS", func(t *testing.T) {
		r := &fakeRunner{bins: map[string]string{"afconvert": "/usr/bin/afconvert"}}
		enc := &audioconv.Encoder{Runner: r, GOOS: "darwin"}
		if err := enc.ConvertToAAC(ctx, "in.wav", "out.m4a", 0); err != nil {
			t.Fatal(err)
		}
		wantArgs := []string{"-f", "m4af", "-d", "aac", "-b", "128000", "in.wav", "out.m4a"}
		if r.lastBin != "/usr/bin/afconvert" || !reflect.DeepEqual(r.lastArg, wantArgs) {
			t.Fatalf("ran %s %v, want afconvert %v", r.lastBin, r.lastArg, wantArgs)
		}
	})

	t.Run("AAC returns ErrEncoderNotFound when no encoder is available", func(t *testing.T) {
		r := &fakeRunner{bins: map[string]string{"afconvert": "/usr/bin/afconvert"}}
		enc := &audioconv.Encoder{Runner: r, GOOS: "linux"}
		err := enc.ConvertToAAC(ctx, "in.wav", "out.m4a", 128)
		if !errors.Is(err, audioconv.ErrEncoderNotFound) {
			t.Fatalf("got err %v, want ErrEncoderNotFound", err)
		}
	})

	t.Run("MP3 via ffmpeg", func(t *testing.T) {
		r := &fakeRunner{bins: map[string]string{"ffmpeg": "/usr/local/bin/ffmpeg"}}
		enc := &audioconv.Encoder{Runner: r, GOOS: "darwin"}
		if err := enc.ConvertToMP3(ctx, "in.wav", "out.mp3", 160); err != nil {
			t.Fatal(err)
		}
		wantArgs := []string{"-y", "-i", "in.wav", "-c:a", "libmp3lame", "-b:a", "160k", "out.mp3"}
		if r.lastBin != "/usr/local/bin/ffmpeg" || !reflect.DeepEqual(r.lastArg, wantArgs) {
			t.Fatalf("ran %s %v, want ffmpeg %v", r.lastBin, r.lastArg, wantArgs)
		}
	})

	t.Run("MP3 returns ErrEncoderNotFound without ffmpeg", func(t *testing.T) {
		r := &fakeRunner{bins: map[string]string{}}
		enc := &audioconv.Encoder{Runner: r, GOOS: "darwin"}
		err := enc.ConvertToMP3(ctx, "in.wav", "out.mp3", 128)
		if !errors.Is(err, audioconv.ErrEncoderNotFound) {
			t.Fatalf("got err %v, want ErrEncoderNotFound", err)
		}
	})

	t.Run("ConvertFile writes valid temporary WAV for external encoder and cleans it up", func(t *testing.T) {
		dir := t.TempDir()
		inPath := filepath.Join(dir, "in.wav")
		outPath := filepath.Join(dir, "out.m4a")
		if err := audioconv.WriteWAVFile(inPath, sineBuffer(44100, 2, 440, 0.5, 441)); err != nil {
			t.Fatal(err)
		}

		var tempWAVPath string
		r := &fakeRunner{
			bins: map[string]string{"ffmpeg": "/usr/bin/ffmpeg"},
			onRun: func(_ string, args []string) ([]byte, error) {
				tempWAVPath = args[2]
				buf, err := audioconv.ReadWAVFile(tempWAVPath)
				if err != nil {
					return nil, err
				}
				if buf.SampleRate != 22050 || buf.Channels != 1 {
					return nil, errors.New("unexpected temp wav format")
				}
				return nil, os.WriteFile(outPath, []byte("fake-aac"), 0o644)
			},
		}
		enc := &audioconv.Encoder{Runner: r}
		if err := enc.ConvertFile(ctx, inPath, outPath, audioconv.FormatAAC, audioconv.Options{
			SampleRate: 22050,
			Channels:   1,
		}); err != nil {
			t.Fatal(err)
		}
		if tempWAVPath == "" {
			t.Fatal("runner was not invoked")
		}
		if _, err := os.Stat(tempWAVPath); !os.IsNotExist(err) {
			t.Fatalf("temporary wav %s was not removed", tempWAVPath)
		}
	})
}
