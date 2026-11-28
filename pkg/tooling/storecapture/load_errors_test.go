package storecapture_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/tooling/storecapture"
)

func TestDecodeLoadErrors(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		data := []byte("version: 1\nshots:\n  - name: title\n    scene: title.scene.yaml\n    fps: 60\n    output: title\n    sizes:\n      - preset: itch_cover\n")
		_, err := storecapture.Decode(data)
		loaderrtest.Expect(t, err, "", 5)
	})
	t.Run("unsupported version", func(t *testing.T) {
		data := []byte("version: 2\nshots:\n  - name: title\n    scene: title.scene.yaml\n    output: title\n    sizes:\n      - preset: itch_cover\n")
		_, err := storecapture.Decode(data)
		loaderrtest.ExpectField(t, err, "", data, "version")
	})
	t.Run("unknown preset", func(t *testing.T) {
		data := []byte("version: 1\nshots:\n  - name: title\n    scene: title.scene.yaml\n    output: title\n    sizes:\n      - preset: itch_cover\n      - preset: steam_capsule\n")
		_, err := storecapture.Decode(data)
		loaderrtest.ExpectField(t, err, "", data, "shots[0].sizes[1]")
	})
	t.Run("output outside the media directory", func(t *testing.T) {
		data := []byte("version: 1\nshots:\n  - name: title\n    scene: title.scene.yaml\n    output: ../title\n    sizes:\n      - preset: itch_cover\n")
		_, err := storecapture.Decode(data)
		loaderrtest.ExpectField(t, err, "", data, "shots[0].output")
	})
	t.Run("file", func(t *testing.T) {
		data := []byte("version: 1\nshots:\n  - name: title\n    scene: /title.scene.yaml\n    output: title\n    sizes:\n      - preset: itch_cover\n")
		path := filepath.Join(t.TempDir(), "capture.yaml")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := storecapture.LoadFile(path)
		loaderrtest.ExpectField(t, err, path, data, "shots[0].scene")
	})
}
