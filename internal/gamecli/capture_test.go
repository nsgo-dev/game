package gamecli

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCaptureFile writes data to dir/rel, creating its directory.
func writeCaptureFile(t *testing.T, dir, rel string, data []byte) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// capturePNG returns a blank PNG of w by h pixels.
func capturePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestCaptureChecksPlanAndShot(t *testing.T) {
	dir := t.TempDir()
	writeCaptureFile(t, dir, "store/capture.yaml", []byte("version: 1\nshots:\n  - name: title\n    scene: title.scene.yaml\n    output: title\n    sizes:\n      - width: 8\n        height: 4\n  - name: play\n    scene: play.scene.yaml\n    output: play\n    sizes:\n      - width: 8\n        height: 4\n"))
	writeCaptureFile(t, dir, "title.scene.yaml", []byte("version: 1\n"))
	writeCaptureFile(t, dir, "play.scene.yaml", []byte("version: 1\n"))
	writeCaptureFile(t, dir, "store/media/title-8x4.png", capturePNG(t, 8, 4))

	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"capture", dir}, a); code != 1 {
		t.Fatalf("capture with a missing image exited %d, want 1; stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr.String(), "play-8x4.png") {
		t.Errorf("stderr does not name the missing image:\n%s", stderr)
	}

	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"capture", "-shot", "title", dir}, a); code != 0 {
		t.Fatalf("capture -shot title exited %d; stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout.String(), "1 image matches") {
		t.Errorf("stdout = %q, want the one image of the title shot", stdout)
	}

	a, _, stderr = newTestApp(t)
	if code := run([]string{"capture", "-shot", "credits", dir}, a); code != 1 || !strings.Contains(stderr.String(), `shot "credits" not found`) {
		t.Errorf("capture -shot credits exited %d; stderr:\n%s", code, stderr)
	}

	a, _, stderr = newTestApp(t)
	if code := run([]string{"capture", t.TempDir()}, a); code != 1 || !strings.Contains(stderr.String(), "does not exist") {
		t.Errorf("capture without a plan exited %d; stderr:\n%s", code, stderr)
	}
}
