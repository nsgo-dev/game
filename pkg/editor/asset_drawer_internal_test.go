package editor

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func makeInternalTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{R: 0x3B, G: 0x7B, B: 0xFF, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeInternalTestWAV(sampleRate int, frames int) []byte {
	dataSize := frames * 2
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(dataSize))
	for range frames {
		_ = binary.Write(&buf, binary.LittleEndian, int16(8000))
	}
	return buf.Bytes()
}

func TestAssetDrawerDragAndFrameTicksAvoidRemountAndDiskIO(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string, data []byte) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("game.yaml", []byte(`version: 1
name: CachePerf
id: dev.nsgo.cacheperf
appVersion: 0.1.0
window:
  width: 640
  height: 360
assets:
  textures:
    - in: art/hero.png
      out: built/hero.ktx2
    - in: art/coin.png
      out: built/coin.ktx2
  audio:
    - in: audio/jump.wav
      out: built/jump.ogg
autoload:
  mainScene: main
  scenes:
    - name: main
      path: scenes/main.scene.yaml
`))
	write("scenes/main.scene.yaml", []byte(`version: 2
layers:
  - name: Actors
    objects: []
`))
	// 32x16 sheet with 16x16 slice sidecar so hero.png has a 2-frame playable clip.
	write("art/hero.png", makeInternalTestPNG(t, 32, 16))
	write("art/hero.png.import.yaml", []byte("version: 1\nimage:\n  filter: nearest\n  slice:\n    width: 16\n    height: 16\n"))
	write("art/coin.png", makeInternalTestPNG(t, 16, 16))
	write("audio/jump.wav", makeInternalTestWAV(22050, 256))

	ed, err := New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SetLeftRailMode(LeftRailAssets)
	ed.SelectAssetPath("art/hero.png")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return EditorView(cx, ed)
	}, nsgotest.Size(1280, 720))
	app.Settle()

	readsBefore := ed.assetDrawer.importDiskReads
	railBefore := ed.assetDrawerRailKey()
	inspBefore := ed.assetDrawerInspectorKey()
	prevBefore := ed.assetDrawerPreviewKey()
	if readsBefore == 0 {
		t.Fatal("expected initial AssetInspectorView mount to inspect import settings once")
	}

	// Simulate 20 drag-move events (60 Hz drag path): zero extra import disk reads and zero rail/drawer/inspector key changes.
	app.Post(func() {
		if err := ed.BeginAssetDrag("art/coin.png"); err != nil {
			t.Errorf("BeginAssetDrag: %v", err)
			return
		}
		for i := range 20 {
			ed.UpdateAssetDrag(float32(100+i*4), float32(80+i*2))
		}
	})
	app.Settle()
	if ed.assetDrawer.importDiskReads != readsBefore {
		t.Fatalf("drag moves triggered %d extra InspectAssetImport disk reads", ed.assetDrawer.importDiskReads-readsBefore)
	}
	if ed.assetDrawerRailKey() != railBefore || ed.assetDrawerInspectorKey() != inspBefore || ed.assetDrawerPreviewKey() != prevBefore {
		t.Fatal("drag moves invalidated rail/inspector/preview memo keys")
	}
	app.Post(func() { ed.CancelAssetDrag() })
	app.Settle()

	// Simulate 5 clip frame-step ticks (8 Hz playback path): zero extra import disk reads and zero rail/inspector key changes.
	app.Post(func() {
		for range 5 {
			ed.StepAssetPreviewFrame(1)
		}
	})
	app.Settle()
	if ed.assetDrawer.importDiskReads != readsBefore {
		t.Fatalf("frame ticks triggered %d extra InspectAssetImport disk reads", ed.assetDrawer.importDiskReads-readsBefore)
	}
	if ed.assetDrawerRailKey() != railBefore || ed.assetDrawerInspectorKey() != inspBefore {
		t.Fatal("frame ticks invalidated rail or outer inspector memo keys")
	}
	if ed.assetDrawerPreviewKey() == prevBefore {
		t.Fatal("expected frame ticks to advance preview key")
	}

	// Switching selected asset stops active clip preview loop.
	app.Post(func() {
		ed.SetAssetPreviewPlaying(true)
		ed.SelectAssetPath("audio/jump.wav")
	})
	app.Settle()
	if ed.AssetPreviewPlaying() {
		t.Fatal("SelectAssetPath did not stop clip preview playback")
	}

	// Evict removed files from thumbMap and audioCache on refresh.
	if _, ok := ed.assetDrawer.thumbMap["art/coin.png"]; !ok {
		t.Fatal("expected art/coin.png in thumbMap")
	}
	if _, ok := ed.assetDrawer.audioCache["audio/jump.wav"]; !ok {
		t.Fatal("expected audio/jump.wav in audioCache")
	}
	if err := os.Remove(filepath.Join(dir, "art/coin.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "audio/jump.wav")); err != nil {
		t.Fatal(err)
	}
	app.Post(func() { ed.RefreshAssetDrawer() })
	app.Settle()
	_ = ed.AssetDrawerSnapshot()
	if _, ok := ed.assetDrawer.thumbMap["art/coin.png"]; ok {
		t.Fatal("removed art/coin.png was not evicted from thumbMap")
	}
	if _, ok := ed.assetDrawer.audioCache["audio/jump.wav"]; ok {
		t.Fatal("removed audio/jump.wav was not evicted from audioCache")
	}
}

func TestDecodeWAVPreviewRejectsOverflowChunkSize(t *testing.T) {
	// Crafted RIFF/WAVE header with 0xFFFFFFFF chunk size (must not wrap or panic on 32-bit/64-bit).
	data := make([]byte, 64)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], 56)
	copy(data[8:12], "WAVE")
	copy(data[12:16], "fmt ")
	binary.LittleEndian.PutUint32(data[16:20], 0xFFFFFFFF)
	if _, _, _, _, ok := decodeWAVPreview(data, 48); ok {
		t.Fatal("decodeWAVPreview accepted malformed 0xFFFFFFFF chunk size")
	}

	// Also verify activePreviewFrameInterval honors clip.FrameMS.
	ed, err := New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ed.activePreviewFrameInterval(); got != 120*time.Millisecond {
		t.Fatalf("default activePreviewFrameInterval = %v, want 120ms", got)
	}
}
