package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func TestCountSceneInstancesCachesDiskScansAndOverlaysAllTabs(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		project.FileName: `version: 1
name: Cache Demo
id: dev.nsgo.cache
appVersion: 0.1.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level1
  scenes:
    - name: level1
      path: scenes/level1.scene.yaml
    - name: level2
      path: scenes/level2.scene.yaml
    - name: slime
      path: scenes/slime.scene.yaml
`,
		"scenes/slime.scene.yaml": `version: 2
layers:
  - name: actors
    objects:
      - id: body
        x: 10
        "y": 20
`,
		"scenes/level1.scene.yaml": `version: 2
layers:
  - name: actors
    objects:
      - id: slime_a
        scene: scenes/slime.scene.yaml
`,
		"scenes/level2.scene.yaml": `version: 2
layers:
  - name: actors
    objects:
      - id: slime_b
        scene: scenes/slime.scene.yaml
      - id: slime_c
        scene: scenes/slime.scene.yaml
`,
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ed, err := New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.ProjectAssets = true
	t.Cleanup(ed.WaitForProblems)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	// First call scans disk once; repeated calls (simulating 60 Hz UI ticks) must NOT re-scan disk.
	insts, scenes := ed.CountSceneInstances("scenes/slime.scene.yaml")
	if insts != 3 || scenes != 2 {
		t.Fatalf("CountSceneInstances initial = (%d, %d), want (3, 2)", insts, scenes)
	}
	if ed.instanceCounts.diskScans != 1 {
		t.Fatalf("diskScans after first call = %d, want 1", ed.instanceCounts.diskScans)
	}
	for range 50 {
		insts, scenes = ed.CountSceneInstances("scenes/slime.scene.yaml")
		if insts != 3 || scenes != 2 {
			t.Fatalf("CountSceneInstances cached = (%d, %d), want (3, 2)", insts, scenes)
		}
	}
	if ed.instanceCounts.diskScans != 1 {
		t.Fatalf("diskScans after 50 cached calls = %d, want 1", ed.instanceCounts.diskScans)
	}

	// Open level2 in a second tab, add an unsaved instance there, and switch back to level1.
	// CountSceneInstances must overlay BOTH open tabs' live in-memory docs without re-scanning disk!
	if err := ed.OpenFile(filepath.Join(dir, "scenes", "level2.scene.yaml")); err != nil {
		t.Fatalf("OpenFile(level2): %v", err)
	}
	ed.SetActiveLayer("actors")
	if _, err := ed.PlaceScene("actors", "scenes/slime.scene.yaml", 20, 20); err != nil {
		t.Fatalf("PlaceScene in level2: %v", err)
	}
	if err := ed.SelectTab(0); err != nil {
		t.Fatalf("SelectTab(0): %v", err)
	}
	// Also add an unsaved instance in active tab level1.
	ed.SetActiveLayer("actors")
	if _, err := ed.PlaceScene("actors", "scenes/slime.scene.yaml", 30, 30); err != nil {
		t.Fatalf("PlaceScene in level1: %v", err)
	}
	scansBefore := ed.instanceCounts.diskScans
	insts, scenes = ed.CountSceneInstances("scenes/slime.scene.yaml")
	if insts != 5 || scenes != 2 {
		t.Fatalf("CountSceneInstances with active+inactive tab edits = (%d, %d), want (5, 2)", insts, scenes)
	}
	if ed.instanceCounts.diskScans != scansBefore {
		t.Fatalf("diskScans bumped during in-memory tab overlay: got %d, want %d", ed.instanceCounts.diskScans, scansBefore)
	}

	// Bumping scenesRev (e.g. saving or external reload) triggers a single fresh disk scan.
	ed.scenesRev++
	insts, scenes = ed.CountSceneInstances("scenes/slime.scene.yaml")
	if insts != 5 || scenes != 2 {
		t.Fatalf("CountSceneInstances after scenesRev bump = (%d, %d), want (5, 2)", insts, scenes)
	}
	if ed.instanceCounts.diskScans != scansBefore+1 {
		t.Fatalf("diskScans after scenesRev bump = %d, want %d", ed.instanceCounts.diskScans, scansBefore+1)
	}

	// Verify standalone (no-project) mode also overlays all open tabs.
	standalone, err := New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	standalone.Doc.Layers[0].Objects = append(standalone.Doc.Layers[0].Objects, sceneformat.ObjectDoc{
		ID:    "s1",
		Scene: "scenes/slime.scene.yaml",
	})
	if gotI, gotS := standalone.CountSceneInstances("scenes/slime.scene.yaml"); gotI != 1 || gotS != 1 {
		t.Fatalf("standalone CountSceneInstances = (%d, %d), want (1, 1)", gotI, gotS)
	}
}
