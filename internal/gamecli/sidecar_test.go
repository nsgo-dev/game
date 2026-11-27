package gamecli

import (
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/audioconv"
	"github.com/nsgo-dev/game/pkg/tooling/importsettings"
	"github.com/nsgo-dev/game/pkg/tooling/ktx2"
)

// texOf reads the KTX2 file at name: its format, sRGB, level count and filter.
func texOf(t *testing.T, name string) (ktx2.Format, bool, uint32, string) {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	h, _, err := ktx2.DecodeHeader(raw)
	if err != nil {
		t.Fatal(err)
	}
	format, srgb, _ := ktx2.FormatFromVk(h.VkFormat)
	kvd, err := ktx2.DecodeKVD(raw[h.KVDByteOffset : h.KVDByteOffset+h.KVDByteLength])
	if err != nil {
		t.Fatal(err)
	}
	return format, srgb, h.LevelCount, string(kvd[importsettings.FilterKey])
}

func TestTexReadsTheSidecarBesideItsInput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "albedo.png")
	f, err := os.Create(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, makeTestImage(8, 8)); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	out := filepath.Join(dir, "albedo.ktx2")
	tex := func(args ...string) string {
		t.Helper()
		a, stdout, stderr := newTestApp(t)
		if code := run(append([]string{"tex"}, args...), a); code != 0 {
			t.Fatalf("exit %d, stderr %s", code, stderr)
		}
		return stdout.String()
	}

	tex(in)
	if format, srgb, levels, filter := texOf(t, out); format != ktx2.FormatRGBA8 || srgb || levels != 1 || filter != "" {
		t.Fatalf("without a sidecar: %s srgb=%v %d levels filter %q", format, srgb, levels, filter)
	}

	// The sidecar's settings apply as game assets applies them; slice is an atlas's and is not read.
	if err := os.WriteFile(in+importsettings.Suffix, []byte("version: 1\nimage:\n  format: bc1\n  mipmaps: true\n  srgb: true\n  filter: nearest\n  slice: {width: 4, height: 4}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := tex(in); !strings.Contains(got, "format=bc1 mipmaps=true srgb=true filter=nearest") {
		t.Fatalf("stdout = %q", got)
	}
	if format, srgb, levels, filter := texOf(t, out); format != ktx2.FormatBC1 || !srgb || levels != 4 || filter != "nearest" {
		t.Fatalf("with the sidecar: %s srgb=%v %d levels filter %q", format, srgb, levels, filter)
	}

	// A flag given wins over the sidecar.
	tex("-format", "etc2", "-mipmaps=false", in)
	if format, srgb, levels, filter := texOf(t, out); format != ktx2.FormatETC2 || !srgb || levels != 1 || filter != "nearest" {
		t.Fatalf("with flags over the sidecar: %s srgb=%v %d levels filter %q", format, srgb, levels, filter)
	}

	for name, data := range map[string]string{
		"audio settings": "version: 1\naudio: {rate: 8000}\n",
		"invalid":        "version: 1\nimage: {filter: cubic}\n",
	} {
		if err := os.WriteFile(in+importsettings.Suffix, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		a, _, stderr := newTestApp(t)
		if code := run([]string{"tex", in}, a); code != 1 || !strings.Contains(stderr.String(), "albedo.png.import.yaml") {
			t.Fatalf("%s: exit %d, stderr %s", name, code, stderr)
		}
	}
}

func TestAudioReadsTheSidecarBesideItsInput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "theme.wav")
	out := filepath.Join(dir, "theme_out.wav")
	writeTestWAV(t, in, 16000, 2, 0.2) // 1600 frames
	if err := os.WriteFile(in+importsettings.Suffix, []byte("version: 1\naudio:\n  rate: 8000\n  channels: 1\n  peakDB: -3\n  loop: {start: 400, end: 1200}\ncredit: {author: Ana, license: CC0-1.0}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, stderr := runAudio(t, &audioconv.Encoder{}, []string{"-in", in, "-out", out}); code != 0 {
		t.Fatalf("exit %d, stderr %s", code, stderr)
	}
	got, err := audioconv.ReadWAVFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if got.SampleRate != 8000 || got.Channels != 1 || math.Abs(got.PeakDBFS()+3) > 0.01 || !reflect.DeepEqual(got.Loop, &audioconv.Loop{Start: 200, End: 600}) {
		t.Fatalf("with the sidecar: %d Hz, %d channels, peak %.2f dBFS, loop %+v", got.SampleRate, got.Channels, got.PeakDBFS(), got.Loop)
	}

	// Flags given win: a level flag replaces the sidecar's level.
	if code, stderr := runAudio(t, &audioconv.Encoder{}, []string{"-in", in, "-out", out, "-rate", "4000", "-rms-db", "-20"}); code != 0 {
		t.Fatalf("exit %d, stderr %s", code, stderr)
	}
	if got, err = audioconv.ReadWAVFile(out); err != nil {
		t.Fatal(err)
	}
	if got.SampleRate != 4000 || got.Channels != 1 || math.Abs(got.PeakDBFS()+3) < 0.5 || !reflect.DeepEqual(got.Loop, &audioconv.Loop{Start: 100, End: 300}) {
		t.Fatalf("with flags over the sidecar: %d Hz, %d channels, peak %.2f dBFS, loop %+v", got.SampleRate, got.Channels, got.PeakDBFS(), got.Loop)
	}

	// A loop needs WAV output.
	code, stderr := runAudio(t, &audioconv.Encoder{}, []string{"-in", in, "-out", filepath.Join(dir, "theme.mp3"), "-format", "mp3"})
	if code != 1 || !strings.Contains(stderr, "loop points need wav output") {
		t.Fatalf("mp3 with a loop: exit %d, stderr %s", code, stderr)
	}

	// A loop past the end of the sound is refused.
	if err := os.WriteFile(in+importsettings.Suffix, []byte("version: 1\naudio: {loop: {start: 0, end: 5000}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, stderr := runAudio(t, &audioconv.Encoder{}, []string{"-in", in, "-out", out}); code != 1 || !strings.Contains(stderr, "not inside") {
		t.Fatalf("loop past the end: exit %d, stderr %s", code, stderr)
	}

	// Image settings beside a sound are refused.
	if err := os.WriteFile(in+importsettings.Suffix, []byte("version: 1\nimage: {filter: nearest}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, stderr := runAudio(t, &audioconv.Encoder{}, []string{"-in", in, "-out", out}); code != 1 || !strings.Contains(stderr, "holds image settings") {
		t.Fatalf("image settings: exit %d, stderr %s", code, stderr)
	}
}
