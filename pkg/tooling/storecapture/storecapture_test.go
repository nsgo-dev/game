package storecapture_test

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/storecapture"
)

const planYAML = `version: 1
shots:
  - name: title
    scene: scenes/title.scene.yaml
    frame: 30
    output: title
    sizes:
      - preset: itch_cover
  - name: boss
    scene: scenes/boss.scene.yaml
    recording: recordings/boss.replay.yaml
    time: 2.5
    output: play/boss
    sizes:
      - preset: google_play
      - width: 32
        height: 16
`

func TestPlanRoundTripsByteForByte(t *testing.T) {
	p, err := storecapture.Decode([]byte(planYAML))
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != planYAML {
		t.Fatalf("round trip changed the plan:\n%s", got)
	}
}

func TestDecodeRefuses(t *testing.T) {
	cases := map[string]string{
		"unknown top-level field": planYAML + "unknown: true\n",
		"unknown shot field":      strings.Replace(planYAML, "    frame: 30\n", "    frame: 30\n    fps: 60\n", 1),
		"unsupported version":     strings.Replace(planYAML, "version: 1", "version: 2", 1),
		"second document":         planYAML + "---\nversion: 1\n",
		"frame and time":          strings.Replace(planYAML, "    frame: 30\n", "    frame: 30\n    time: 1\n", 1),
		"negative frame":          strings.Replace(planYAML, "frame: 30", "frame: -1", 1),
		"unknown preset":          strings.Replace(planYAML, "preset: itch_cover", "preset: steam_capsule", 1),
		"preset and width":        strings.Replace(planYAML, "      - preset: itch_cover\n", "      - preset: itch_cover\n        width: 10\n", 1),
		"output escapes":          strings.Replace(planYAML, "output: title", "output: ../title", 1),
		"absolute scene":          strings.Replace(planYAML, "scene: scenes/title.scene.yaml", "scene: /scenes/title.scene.yaml", 1),
		"recording not a replay":  strings.Replace(planYAML, "boss.replay.yaml", "boss.yaml", 1),
		"duplicate shot":          strings.Replace(planYAML, "name: boss", "name: title", 1),
		"same image twice":        strings.Replace(planYAML, "output: play/boss", "output: title", 1) + "      - preset: itch_cover\n",
	}
	for name, raw := range cases {
		if _, err := storecapture.Decode([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestImagesNamesEachShotAtEachSize(t *testing.T) {
	p, err := storecapture.Decode([]byte(planYAML))
	if err != nil {
		t.Fatal(err)
	}
	want := []storecapture.Image{
		{Shot: "title", Path: "title-630x500.png", Size: storecapture.Size{Width: 630, Height: 500}},
		{Shot: "boss", Path: "play/boss-1920x1080.png", Size: storecapture.Size{Width: 1920, Height: 1080}},
		{Shot: "boss", Path: "play/boss-32x16.png", Size: storecapture.Size{Width: 32, Height: 16}},
	}
	got := p.Images()
	if len(got) != len(want) {
		t.Fatalf("Images() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Images()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// writeFile writes data to dir/rel, creating its directory.
func writeFile(t *testing.T, dir, rel string, data []byte) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// pngOf returns a blank PNG of w by h pixels.
func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestCheck(t *testing.T) {
	p, err := storecapture.Decode([]byte(planYAML))
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	media := filepath.Join(project, "store", "media")
	for _, f := range []string{"scenes/title.scene.yaml", "scenes/boss.scene.yaml", "recordings/boss.replay.yaml"} {
		writeFile(t, project, f, []byte("version: 1\n"))
	}
	for _, img := range p.Images() {
		writeFile(t, media, img.Path, pngOf(t, img.Size.Width, img.Size.Height))
	}
	if err := storecapture.Check(*p, project, media); err != nil {
		t.Fatalf("Check of complete media: %v", err)
	}

	writeFile(t, media, "play/boss-32x16.png", pngOf(t, 16, 32))
	if err := os.Remove(filepath.Join(media, "title-630x500.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(project, "recordings", "boss.replay.yaml")); err != nil {
		t.Fatal(err)
	}
	err = storecapture.Check(*p, project, media)
	if err == nil {
		t.Fatal("Check accepted a missing image, a wrongly sized image and a missing recording")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Check error %v does not wrap fs.ErrNotExist", err)
	}
	msg := err.Error()
	for _, want := range []string{"title-630x500.png", "boss-32x16.png is 16x32, want 32x16", "boss.replay.yaml"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Check error does not mention %q:\n%s", want, msg)
		}
	}

	writeFile(t, media, "play/boss-32x16.png", []byte("not an image"))
	if err := storecapture.Check(*p, project, media); err == nil || !strings.Contains(err.Error(), "not a PNG") {
		t.Errorf("Check of a non-PNG image = %v", err)
	}
}
