package editor

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func TestModelPreviewPausedOrbitDoesNotRebuildRenderer(t *testing.T) {
	dir := t.TempDir()
	gameYAML := "version: 1\nname: SkinnedOrbit\nid: dev.example.skinnedorbit\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: room\n  scenes:\n    - name: room\n      path: scenes/room.scene.yaml\n"
	nsgoYAML := "version: 1\nname: SkinnedOrbit\n"
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte(nsgoYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "room.scene.yaml"), []byte("version: 2\nname: Room\nlayers:\n  - name: actors\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join("..", "gltf", "testdata", "khronos", "RiggedSimple", "glTF")
	dstDir := filepath.Join(dir, "models", "rigged")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, ent.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dstDir, ent.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ed, err := New(nil, sceneformat.AtlasMap{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	rigPath := "models/rigged/RiggedSimple.gltf"
	if _, err := ed.RenderModelPreview(rigPath, 96, 96); err != nil {
		t.Fatalf("initial RenderModelPreview: %v", err)
	}
	if !ed.modelPreview.hasDeform {
		t.Fatal("expected RiggedSimple to have deformed parts")
	}
	if got := ed.modelPreview.rendererBuilds; got != 1 {
		t.Fatalf("initial rendererBuilds = %d, want 1", got)
	}
	origRenderer := ed.modelPreview.renderer

	// Paused orbit, pan, zoom, reset, and scale changes must NOT rebuild the GPU renderer or re-skin.
	ed.OrbitModelPreview(0.3, 0.1)
	if _, err := ed.RenderModelPreview(rigPath, 96, 96); err != nil {
		t.Fatalf("orbit RenderModelPreview: %v", err)
	}
	ed.PanModelPreview(10, -5, 190)
	if _, err := ed.RenderModelPreview(rigPath, 96, 96); err != nil {
		t.Fatalf("pan RenderModelPreview: %v", err)
	}
	ed.ZoomModelPreview(1.2)
	if _, err := ed.RenderModelPreview(rigPath, 96, 96); err != nil {
		t.Fatalf("zoom RenderModelPreview: %v", err)
	}
	ed.ResetModelPreviewCamera(rigPath)
	if _, err := ed.RenderModelPreview(rigPath, 96, 96); err != nil {
		t.Fatalf("reset RenderModelPreview: %v", err)
	}
	if err := ed.SetModelImportScale(rigPath, 0.5); err != nil {
		t.Fatalf("SetModelImportScale: %v", err)
	}
	if _, err := ed.RenderModelPreview(rigPath, 96, 96); err != nil {
		t.Fatalf("scaled RenderModelPreview: %v", err)
	}
	if got := ed.modelPreview.rendererBuilds; got != 1 {
		t.Fatalf("rendererBuilds after paused camera/scale edits = %d, want 1 (must reuse GPU renderer)", got)
	}
	if ed.modelPreview.renderer != origRenderer {
		t.Fatal("renderer pointer changed during paused camera/scale edits")
	}

	// Advancing animation pose MUST re-skin and rebuild the renderer once.
	if err := ed.SeekModelPreviewAnimation(rigPath, 500*time.Millisecond); err != nil {
		t.Fatalf("SeekModelPreviewAnimation: %v", err)
	}
	if _, err := ed.RenderModelPreview(rigPath, 96, 96); err != nil {
		t.Fatalf("animated RenderModelPreview: %v", err)
	}
	if got := ed.modelPreview.rendererBuilds; got != 2 {
		t.Fatalf("rendererBuilds after animation seek = %d, want 2", got)
	}
}

func TestModelPreviewDurationClampAndNonFiniteGuards(t *testing.T) {
	if got := secondsToDurationClamped(math.NaN()); got != 0 {
		t.Fatalf("NaN duration = %v, want 0", got)
	}
	if got := secondsToDurationClamped(-3); got != 0 {
		t.Fatalf("negative duration = %v, want 0", got)
	}
	if got := secondsToDurationClamped(1e12); got != time.Duration(math.MaxInt64) {
		t.Fatalf("1e12 duration = %v, want MaxInt64", got)
	}
	if got := secondsToDurationClamped(math.Inf(1)); got != time.Duration(math.MaxInt64) {
		t.Fatalf("+Inf duration = %v, want MaxInt64", got)
	}
	if got := secondsToDurationClamped(1.5); got != 1500*time.Millisecond {
		t.Fatalf("1.5s duration = %v, want 1.5s", got)
	}

	ed, err := New(nil, sceneformat.AtlasMap{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.PanModelPreview(10, 10, float32(math.NaN()))
	cam := ed.ModelPreviewCamera()
	for i, c := range cam.Target {
		if !isFiniteFloat32(c) {
			t.Fatalf("PanModelPreview with NaN canvasH poisoned Target[%d] = %v", i, c)
		}
	}
}
