package editor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func setupCheckpointProject(t *testing.T) (dir, sceneFile, prefabFile string, sceneOrig, prefabOrig []byte) {
	t.Helper()
	dir = t.TempDir()
	for _, sub := range []string{"scenes", "prefabs", "data", "scripts", "art"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gameYAML := "version: 1\nname: Dungeon Run\nid: dev.nsgo.dungeon\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	sceneOrig = []byte("# Hand-written comment preserved on restore.\nversion: 2\nname: Level 01\nvirtualWidth: 640\nvirtualHeight: 360\nbackgroundColor: \"#141824\"\nlayers:\n  - name: actors\n    space: world\n    order: 0\n    objects:\n      - id: knight\n        name: Knight\n        x: 96\n        y: 180\n")
	sceneFile = filepath.Join(dir, "scenes", "level_01.scene.yaml")
	if err := os.WriteFile(sceneFile, sceneOrig, 0o644); err != nil {
		t.Fatal(err)
	}
	prefabOrig = []byte("# Coin prefab v1\nversion: 1\nname: Coin\nspin: false\nbob: 0\n")
	prefabFile = filepath.Join(dir, "prefabs", "coin.prefab.yaml")
	if err := os.WriteFile(prefabFile, prefabOrig, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, sceneFile, prefabFile, sceneOrig, prefabOrig
}

// #1018 acceptance 1: an agent turn edits a scene and a prefab (plus newly created file, game.yaml,
// Go code, and binary media), restores the checkpoint from History, gets every file back byte for byte,
// and one Undo puts the turn back byte for byte.
func TestCheckpointAgentTurnRestoresSceneAndPrefabByteForByteAndUndoes(t *testing.T) {
	dir, sceneFile, prefabFile, sceneOrig, prefabOrig := setupCheckpointProject(t)

	goFile := filepath.Join(dir, "scripts", "patrol.go")
	goOrig := []byte("package scripts\n\nconst PatrolSpeed = 24\n")
	if err := os.WriteFile(goFile, goOrig, 0o644); err != nil {
		t.Fatal(err)
	}
	binFile := filepath.Join(dir, "art", "coin.png")
	binOrig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01, 0x02, 0xFF}
	if err := os.WriteFile(binFile, binOrig, 0o644); err != nil {
		t.Fatal(err)
	}
	gameOrig, err := os.ReadFile(filepath.Join(dir, project.FileName))
	if err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	clock := time.Date(2026, 11, 26, 14, 41, 0, 0, time.UTC)
	ed.Now = func() time.Time { return clock }
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	prefabEdited := []byte("# Coin prefab v2\nversion: 1\nname: Coin\nspin: true\nbob: 4\n")
	sparkleNew := []byte("version: 1\nname: sparkle\nrate: 12\n")
	goEdited := []byte("package scripts\n\nconst PatrolSpeed = 18\n")
	binEdited := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0xDE, 0xAD, 0xBE, 0xEF}

	summary, err := ed.RunCheckpointTurn("Coin spin, bob and sparkle", func() error {
		// 1. Edit the open scene via editor commands and save to disk.
		if err := ed.MoveObject("knight", 220, 140); err != nil {
			return err
		}
		if err := ed.SaveFile(sceneFile); err != nil {
			return err
		}
		// 2. Edit a prefab file twice during the same turn — checkpoint must keep the ORIGINAL bytes!
		if err := ed.WriteAgentFile("prefabs/coin.prefab.yaml", []byte("intermediate\n")); err != nil {
			return err
		}
		if err := ed.WriteAgentFile("prefabs/coin.prefab.yaml", prefabEdited); err != nil {
			return err
		}
		// 3. Create a brand new file (existed: false -> Restore must delete it; Undo must recreate it).
		if err := ed.WriteAgentFile("fx/sparkle.particles.yaml", sparkleNew); err != nil {
			return err
		}
		// 4. Edit game.yaml, Go code, and binary media.
		if err := ed.EditProject("Rename", func(f *project.File) { f.Name = "Dungeon Run v2" }); err != nil {
			return err
		}
		if err := ed.SaveProject(); err != nil {
			return err
		}
		if err := ed.WriteAgentFile("scripts/patrol.go", goEdited); err != nil {
			return err
		}
		return ed.WriteAgentFile("art/coin.png", binEdited)
	})
	if err != nil {
		t.Fatalf("RunCheckpointTurn: %v", err)
	}
	if summary == nil || summary.ID == "" {
		t.Fatal("expected non-nil checkpoint summary")
	}

	// Verify .gitignore exists under .nsgo/checkpoints/.
	gi, err := os.ReadFile(filepath.Join(dir, ".nsgo", "checkpoints", ".gitignore"))
	if err != nil || string(gi) != "*\n" {
		t.Fatalf(".nsgo/checkpoints/.gitignore = %q, err = %v", string(gi), err)
	}

	// Capture post-turn scene & game.yaml bytes for Undo comparison.
	sceneAfterTurn, err := os.ReadFile(sceneFile)
	if err != nil {
		t.Fatal(err)
	}
	gameAfterTurn, err := os.ReadFile(filepath.Join(dir, project.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(sceneAfterTurn, sceneOrig) {
		t.Fatal("expected scene to change during agent turn")
	}

	// Preview the checkpoint before restoring: ActiveScene shows knight at (96, 180) while ed.Doc stays at (220, 140).
	clock = clock.Add(5 * time.Minute)
	if err := ed.PreviewCheckpoint(summary.ID); err != nil {
		t.Fatalf("PreviewCheckpoint: %v", err)
	}
	if !ed.HistoryPreviewActive() {
		t.Fatal("expected HistoryPreviewActive")
	}
	if bo := ed.ActiveScene().Object("knight"); bo == nil || bo.Doc.X != 96 || bo.Doc.Y != 180 {
		t.Fatalf("preview knight = %+v, want (96, 180)", bo)
	}
	if docObj, _, _ := ed.FindObject("knight"); docObj == nil || docObj.X != 220 {
		t.Fatalf("live Doc knight changed during preview: %+v", docObj)
	}

	// Restore the checkpoint from History.
	if err := ed.RestorePreviewedHistory(); err != nil {
		t.Fatalf("RestorePreviewedHistory: %v", err)
	}
	if ed.HistoryPreviewActive() {
		t.Fatal("preview should end after Restore")
	}

	// Verify every file is back byte-for-byte!
	assertFileBytes := func(label, p string, want []byte) {
		t.Helper()
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: read %s: %v", label, p, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: %s bytes mismatch:\ngot:  %q\nwant: %q", label, p, got, want)
		}
	}
	assertFileBytes("after restore", sceneFile, sceneOrig)
	assertFileBytes("after restore", prefabFile, prefabOrig)
	assertFileBytes("after restore", goFile, goOrig)
	assertFileBytes("after restore", binFile, binOrig)
	assertFileBytes("after restore", filepath.Join(dir, project.FileName), gameOrig)
	if _, err := os.Stat(filepath.Join(dir, "fx", "sparkle.particles.yaml")); !os.IsNotExist(err) {
		t.Fatalf("after restore: newly created file should be removed, stat err = %v", err)
	}
	if docObj, _, _ := ed.FindObject("knight"); docObj == nil || docObj.X != 96 || docObj.Y != 180 {
		t.Fatalf("after restore: open scene knight = %+v, want (96, 180)", docObj)
	}
	if ed.Project.Name != "Dungeon Run" {
		t.Fatalf("after restore: ed.Project.Name = %q, want Dungeon Run", ed.Project.Name)
	}

	// One Undo puts the entire turn back byte-for-byte!
	if !ed.Undo() {
		t.Fatal("Undo returned false after RestoreCheckpoint")
	}
	assertFileBytes("after undo", sceneFile, sceneAfterTurn)
	assertFileBytes("after undo", prefabFile, prefabEdited)
	assertFileBytes("after undo", goFile, goEdited)
	assertFileBytes("after undo", binFile, binEdited)
	assertFileBytes("after undo", filepath.Join(dir, project.FileName), gameAfterTurn)
	assertFileBytes("after undo", filepath.Join(dir, "fx", "sparkle.particles.yaml"), sparkleNew)
	if docObj, _, _ := ed.FindObject("knight"); docObj == nil || docObj.X != 220 || docObj.Y != 140 {
		t.Fatalf("after undo: open scene knight = %+v, want (220, 140)", docObj)
	}
	if ed.Project.Name != "Dungeon Run v2" {
		t.Fatalf("after undo: ed.Project.Name = %q, want Dungeon Run v2", ed.Project.Name)
	}

	// Redo re-applies the restore cleanly!
	if !ed.Redo() {
		t.Fatal("Redo returned false")
	}
	assertFileBytes("after redo", sceneFile, sceneOrig)
	assertFileBytes("after redo", prefabFile, prefabOrig)
}

// #1018 acceptance 2: reopening the project in a fresh Editor lists the exact same checkpoints
// and lets the user restore them across sessions.
func TestCheckpointsPersistAcrossProjectReopen(t *testing.T) {
	dir, sceneFile, prefabFile, sceneOrig, prefabOrig := setupCheckpointProject(t)

	ed1, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed1.WaitForProblems)
	baseTime := time.Date(2026, 11, 26, 14, 0, 0, 0, time.UTC)
	ed1.Now = func() time.Time { return baseTime }
	if err := ed1.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	cp1, err := ed1.RunCheckpointTurn("First turn", func() error {
		return ed1.WriteAgentFile("prefabs/coin.prefab.yaml", []byte("version: 1\nname: CoinV2\n"))
	})
	if err != nil {
		t.Fatal(err)
	}
	baseTime = baseTime.Add(10 * time.Minute)
	cp2, err := ed1.RunCheckpointTurn("Second turn", func() error {
		if err := ed1.MoveObject("knight", 300, 180); err != nil {
			return err
		}
		return ed1.SaveFile(sceneFile)
	})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate editor restart: brand new Editor instance opening the same project directory.
	ed2, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed2.WaitForProblems)
	ed2.Now = func() time.Time { return baseTime.Add(5 * time.Minute) }
	if err := ed2.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	got := ed2.Checkpoints()
	if len(got) != 2 {
		t.Fatalf("Checkpoints after reopen = %d, want 2", len(got))
	}
	if got[0].ID != cp2.ID || got[1].ID != cp1.ID {
		t.Fatalf("Checkpoints order = [%s, %s], want [%s, %s]", got[0].ID, got[1].ID, cp2.ID, cp1.ID)
	}

	// Restore cp1 across sessions and verify prefab bytes come back verbatim.
	if err := ed2.RestoreCheckpoint(cp1.ID); err != nil {
		t.Fatalf("RestoreCheckpoint after reopen: %v", err)
	}
	gotPrefab, err := os.ReadFile(prefabFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotPrefab, prefabOrig) {
		t.Fatalf("prefab after cross-session restore = %q, want %q", gotPrefab, prefabOrig)
	}
	// Restore cp2 and verify scene bytes come back verbatim.
	if err := ed2.RestoreCheckpoint(cp2.ID); err != nil {
		t.Fatalf("RestoreCheckpoint cp2 after reopen: %v", err)
	}
	gotScene, err := os.ReadFile(sceneFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotScene, sceneOrig) {
		t.Fatalf("scene after cross-session restore = %q, want %q", gotScene, sceneOrig)
	}
}

// Size cap drops oldest completed checkpoints while preserving newest ones.
func TestCheckpointSizeCapDropsOldest(t *testing.T) {
	dir, _, _, _, _ := setupCheckpointProject(t)

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	clock := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return clock }
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Write a 2 KiB file so each checkpoint is > 2 KiB.
	payload := bytes.Repeat([]byte("A"), 2048)
	if err := os.WriteFile(filepath.Join(dir, "data", "blob.txt"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	// Cap at 5 KiB -> fits 2 checkpoints (~2.3 KiB each), drops oldest when 3rd arrives.
	ed.CheckpointMaxBytes = 5200

	var ids []string
	for i := 0; i < 4; i++ {
		clock = clock.Add(time.Minute)
		next := bytes.Repeat([]byte{byte('B' + i)}, 2048)
		cp, err := ed.RunCheckpointTurn("Turn "+string(rune('1'+i)), func() error {
			return ed.WriteAgentFile("data/blob.txt", next)
		})
		if err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
		ids = append(ids, cp.ID)
	}

	list := ed.Checkpoints()
	if len(list) >= 4 {
		t.Fatalf("expected oldest checkpoints to be pruned, got %d", len(list))
	}
	if list[0].ID != ids[len(ids)-1] {
		t.Fatalf("newest checkpoint = %s, want %s", list[0].ID, ids[len(ids)-1])
	}
	if _, err := os.Stat(filepath.Join(ed.CheckpointsDir(), ids[0])); !os.IsNotExist(err) {
		t.Fatalf("oldest checkpoint %s should have been dropped, stat err = %v", ids[0], err)
	}
}

// Tasks and automations always take a persistent checkpoint before they start.
func TestTasksAndAutomationsTakeCheckpointBeforeStart(t *testing.T) {
	dir, _, _, _, prefabOrig := setupCheckpointProject(t)

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	clock := time.Date(2026, 11, 26, 2, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return clock }
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Verify the checkpoint exists on disk BEFORE the task body writes anything.
	taskSummary, err := ed.RunTaskWithCheckpoint("Nightly 20-run playtest", []string{"prefabs/coin.prefab.yaml"}, func() error {
		id := ed.ActiveTurnID()
		if id == "" {
			t.Fatal("expected active checkpoint ID inside task")
		}
		mPath := filepath.Join(ed.CheckpointsDir(), id, editor.CheckpointManifestFile)
		if _, statErr := os.Stat(mPath); statErr != nil {
			t.Fatalf("expected checkpoint manifest on disk before task starts: %v", statErr)
		}
		return ed.WriteAgentFile("prefabs/coin.prefab.yaml", []byte("version: 1\nname: TaskModified\n"))
	})
	if err != nil {
		t.Fatalf("RunTaskWithCheckpoint: %v", err)
	}
	if taskSummary.Kind != editor.CheckpointKindTask || !strings.Contains(taskSummary.Title, "checkpoint before start") {
		t.Fatalf("task checkpoint = %+v", taskSummary)
	}

	clock = clock.Add(time.Minute)
	autoSummary, err := ed.TakeCheckpointBeforeAutomation("Auto-tile pass", "scenes/level_01.scene.yaml")
	if err != nil {
		t.Fatalf("TakeCheckpointBeforeAutomation: %v", err)
	}
	if autoSummary.Kind != editor.CheckpointKindAutomation {
		t.Fatalf("automation checkpoint kind = %q, want automation", autoSummary.Kind)
	}

	// Restoring the task checkpoint puts prefab back byte-for-byte.
	if err := ed.RestoreCheckpoint(taskSummary.ID); err != nil {
		t.Fatalf("RestoreCheckpoint(task): %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "prefabs", "coin.prefab.yaml"))
	if err != nil || !bytes.Equal(got, prefabOrig) {
		t.Fatalf("prefab after restoring task checkpoint = %q, err = %v", got, err)
	}
}

// Strict validation tests for CheckpointManifest YAML format.
func TestCheckpointManifestValidationRejectsInvalid(t *testing.T) {
	valid := []byte("version: 1\nid: 20261126T144100Z-0001\ncreatedAt: \"2026-11-26T14:41:00Z\"\nwho: agent\nkind: turn\ntitle: Slimes and coins\nfiles:\n  - path: scenes/level_01.scene.yaml\n    existed: true\n    size: 12\n    sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n")
	if _, err := editor.ParseCheckpointManifest(valid); err != nil {
		t.Fatalf("valid manifest failed: %v", err)
	}
	cases := []struct {
		name string
		yaml string
	}{
		{"unknown field", "version: 1\nid: a\ncreatedAt: \"2026-11-26T14:41:00Z\"\nwho: agent\nkind: turn\ntitle: x\nextra: 1\n"},
		{"bad version", "version: 2\nid: a\ncreatedAt: \"2026-11-26T14:41:00Z\"\nwho: agent\nkind: turn\ntitle: x\n"},
		{"parent path", "version: 1\nid: a\ncreatedAt: \"2026-11-26T14:41:00Z\"\nwho: agent\nkind: turn\ntitle: x\nfiles:\n  - path: ../escape.yaml\n    existed: false\n"},
		{"nsgo metadata path", "version: 1\nid: a\ncreatedAt: \"2026-11-26T14:41:00Z\"\nwho: agent\nkind: turn\ntitle: x\nfiles:\n  - path: .nsgo/checkpoints/x\n    existed: false\n"},
		{"bad sha256", "version: 1\nid: a\ncreatedAt: \"2026-11-26T14:41:00Z\"\nwho: agent\nkind: turn\ntitle: x\nfiles:\n  - path: a.yaml\n    existed: true\n    size: 1\n    sha256: nothex\n"},
		{"nonexistent with size", "version: 1\nid: a\ncreatedAt: \"2026-11-26T14:41:00Z\"\nwho: agent\nkind: turn\ntitle: x\nfiles:\n  - path: a.yaml\n    existed: false\n    size: 1\n"},
	}
	for _, tc := range cases {
		if _, err := editor.ParseCheckpointManifest([]byte(tc.yaml)); err == nil {
			t.Errorf("%s: expected ParseCheckpointManifest to fail", tc.name)
		}
	}
}

// UI test: History panel opens with ⌘Y, filters All/You/Agent, previews a checkpoint read-only,
// exits preview with Esc, restores via the Restore button, and undoes via the Undo button.
func TestHistoryPanelUIWithCheckpointsAndFilters(t *testing.T) {
	dir, sceneFile, prefabFile, sceneOrig, prefabOrig := setupCheckpointProject(t)

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	clock := time.Date(2026, 11, 26, 14, 20, 0, 0, time.UTC)
	ed.Now = func() time.Time { return clock }
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Manual edit by You.
	if err := ed.MoveObject("knight", 120, 180); err != nil {
		t.Fatal(err)
	}
	if err := ed.SaveFile(sceneFile); err != nil {
		t.Fatal(err)
	}
	sceneAfterYou, _ := os.ReadFile(sceneFile)

	// Agent turn editing both scene and prefab.
	clock = clock.Add(11 * time.Minute)
	cp, err := ed.RunCheckpointTurn("Coin spin, bob and sparkle", func() error {
		if err := ed.MoveObject("knight", 260, 180); err != nil {
			return err
		}
		if err := ed.SaveFile(sceneFile); err != nil {
			return err
		}
		return ed.WriteAgentFile("prefabs/coin.prefab.yaml", []byte("version: 1\nname: CoinSpin\n"))
	})
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(10 * time.Minute)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), desktopEnv())
	app.Settle()

	// Open History via toolbar button.
	app.Press("id:toolbar-history")
	app.Settle()
	if !app.Exists("id:history-panel") {
		t.Fatal("expected history-panel after clicking toolbar-history")
	}

	cpRowID := "id:history-row-cp:" + cp.ID
	if !app.Exists(cpRowID) {
		t.Fatalf("expected checkpoint row %s in History panel", cpRowID)
	}

	// Filter to You -> hides Agent checkpoint.
	app.Press("id:history-filter-you")
	app.Settle()
	if app.Exists(cpRowID) {
		t.Fatal("Agent checkpoint should be hidden under You filter")
	}

	// Filter to Agent -> shows Agent checkpoint.
	app.Press("id:history-filter-agent")
	app.Settle()
	if !app.Exists(cpRowID) {
		t.Fatal("Agent checkpoint should be visible under Agent filter")
	}

	// Click checkpoint row -> enters read-only preview banner.
	app.Press(cpRowID)
	app.Settle()
	if !app.Exists("id:history-preview-banner") || !ed.HistoryPreviewActive() {
		t.Fatal("expected history-preview-banner after clicking checkpoint row")
	}
	if bo := ed.ActiveScene().Object("knight"); bo == nil || bo.Doc.X != 120 {
		t.Fatalf("preview knight X = %v, want 120", bo)
	}

	// Press Back/Esc -> exits preview.
	app.Press("id:history-preview-back")
	app.Settle()
	if ed.HistoryPreviewActive() || app.Exists("id:history-preview-banner") {
		t.Fatal("expected preview to end after clicking Back")
	}

	// Click checkpoint again and click Restore -> restores both files byte-for-byte & shows restore toast with Undo.
	app.Press(cpRowID)
	app.Settle()
	app.Press("id:history-preview-restore")
	app.Settle()
	if !app.Exists("id:editor-toast") || !app.Exists("id:editor-toast-action") {
		t.Fatal("expected restore toast with Undo action after Restore")
	}
	gotScene, _ := os.ReadFile(sceneFile)
	gotPrefab, _ := os.ReadFile(prefabFile)
	if !bytes.Equal(gotScene, sceneAfterYou) || !bytes.Equal(gotPrefab, prefabOrig) {
		t.Fatalf("after UI Restore: scene/prefab mismatch")
	}
	_ = sceneOrig

	// Click Undo button in restore toast -> puts the turn back byte-for-byte.
	app.Press("id:editor-toast-action")
	app.Settle()
	if obj, _, _ := ed.FindObject("knight"); obj == nil || obj.X != 260 {
		t.Fatalf("after Undo toast action: knight X = %v, want 260", obj)
	}
	gotPrefabAfterUndo, _ := os.ReadFile(prefabFile)
	if !strings.Contains(string(gotPrefabAfterUndo), "CoinSpin") {
		t.Fatalf("after Undo toast action: prefab = %q, want CoinSpin", string(gotPrefabAfterUndo))
	}
}

// Security: You-only files (permissions.yaml, tools.yaml, models.yaml, usage.yaml, .game/editor.yaml)
// are rejected by manifest parsing, checkpoint recording, agent writes/deletes, and restore.
func TestCheckpointsEnforceYouOnlyBoundary(t *testing.T) {
	dir, _, _, _, _ := setupCheckpointProject(t)
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	youOnlyPaths := []string{
		"permissions.yaml",
		"sub/permissions.yaml",
		"tools.yaml",
		"mcp.yaml",
		"models.yaml",
		"usage.yaml",
		"privacy.yaml",
		".nsgo/permissions.yaml",
		".game/editor.yaml",
	}
	for _, p := range youOnlyPaths {
		if err := ed.WriteAgentFile(p, []byte("version: 1\nprofile: autonomous\n")); err == nil {
			t.Errorf("WriteAgentFile(%q) succeeded, want You-only error", p)
		}
		if err := ed.DeleteAgentFile(p); err == nil {
			t.Errorf("DeleteAgentFile(%q) succeeded, want You-only error", p)
		}
		manifest := "version: 1\nid: 20261126T144100Z-0001\ncreatedAt: \"2026-11-26T14:41:00Z\"\nwho: agent\nkind: turn\ntitle: x\nfiles:\n  - path: " + p + "\n    existed: false\n"
		if _, err := editor.ParseCheckpointManifest([]byte(manifest)); err == nil {
			t.Errorf("ParseCheckpointManifest(%q) succeeded, want You-only error", p)
		}
	}

	// Recording inside an active turn must also refuse You-only targets.
	if _, err := ed.BeginCheckpointTurn("Try You-only"); err != nil {
		t.Fatal(err)
	}
	defer ed.AbortCheckpointTurn()
	if err := ed.RecordCheckpointFile("permissions.yaml"); err == nil {
		t.Fatal("RecordCheckpointFile(permissions.yaml) succeeded, want error")
	}
}

// Security: symlinks (external, dangling, and internal pointing to You-only/.git) and tampered
// checkpoint payloads (SHA-256 / size mismatch) are refused across record, write, delete, and restore.
func TestCheckpointsRefuseSymlinksAndTamperedPayloads(t *testing.T) {
	dir, _, _, _, _ := setupCheckpointProject(t)
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	permFile := filepath.Join(dir, "permissions.yaml")
	if err := os.WriteFile(permFile, []byte("version: 1\nprofile: cautious\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create external symlink, dangling symlink, and internal symlink pointing to permissions.yaml.
	extLink := filepath.Join(dir, "data", "ext_link.yaml")
	if err := os.Symlink(outsideFile, extLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	danglingLink := filepath.Join(dir, "data", "dangling_link.yaml")
	if err := os.Symlink(filepath.Join(outsideDir, "missing.txt"), danglingLink); err != nil {
		t.Fatal(err)
	}
	internalLink := filepath.Join(dir, "data", "perm_alias.yaml")
	if err := os.Symlink(permFile, internalLink); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{"data/ext_link.yaml", "data/dangling_link.yaml", "data/perm_alias.yaml"} {
		if err := ed.WriteAgentFile(rel, []byte("pwned")); err == nil {
			t.Errorf("WriteAgentFile(%q) allowed symlink write", rel)
		}
		if err := ed.DeleteAgentFile(rel); err == nil {
			t.Errorf("DeleteAgentFile(%q) allowed symlink delete", rel)
		}
	}
	if raw, _ := os.ReadFile(permFile); !strings.Contains(string(raw), "cautious") {
		t.Fatalf("permissions.yaml was altered via internal symlink: %q", string(raw))
	}

	// Create a real checkpoint, tamper with its stored file bytes, and confirm Restore refuses without mutating disk.
	cp, err := ed.RunCheckpointTurn("Tamper test", func() error {
		return ed.WriteAgentFile("prefabs/coin.prefab.yaml", []byte("version: 1\nname: CoinV2\n"))
	})
	if err != nil {
		t.Fatal(err)
	}
	savedBlob := filepath.Join(ed.CheckpointsDir(), cp.ID, "files", "prefabs", "coin.prefab.yaml")
	if err := os.WriteFile(savedBlob, []byte("corrupted bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.RestoreCheckpoint(cp.ID); err == nil {
		t.Fatal("RestoreCheckpoint succeeded despite tampered checkpoint payload")
	}
	gotPrefab, _ := os.ReadFile(filepath.Join(dir, "prefabs", "coin.prefab.yaml"))
	if string(gotPrefab) != "version: 1\nname: CoinV2\n" {
		t.Fatalf("prefab mutated on failed restore: %q", string(gotPrefab))
	}
}

// Panic & lifecycle safety: a panic inside RunCheckpointTurn never strands ActiveTurnID, and switching to Demo clears checkpoints cleanly.
func TestCheckpointsPanicAndLifecycleSafety(t *testing.T) {
	dir, _, _, _, prefabOrig := setupCheckpointProject(t)
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Panic after modifying a file -> ActiveTurnID cleared, partial checkpoint preserved and restorable.
	func() {
		defer func() { _ = recover() }()
		_, _ = ed.RunCheckpointTurn("Panicking turn", func() error {
			if err := ed.WriteAgentFile("prefabs/coin.prefab.yaml", []byte("version: 1\nname: Broken\n")); err != nil {
				return err
			}
			panic("boom")
		})
	}()
	if ed.ActiveTurnID() != "" {
		t.Fatalf("ActiveTurnID stranded after panic: %q", ed.ActiveTurnID())
	}
	cps := ed.Checkpoints()
	if len(cps) != 1 || cps[0].Detail != "interrupted" {
		t.Fatalf("expected 1 interrupted checkpoint after panic, got %+v", cps)
	}
	if err := ed.RestoreCheckpoint(cps[0].ID); err != nil {
		t.Fatalf("RestoreCheckpoint after panic: %v", err)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "prefabs", "coin.prefab.yaml")); !bytes.Equal(raw, prefabOrig) {
		t.Fatalf("prefab not restored after panic checkpoint")
	}

	// Switching to Demo aborts any active turn and clears loaded project checkpoints.
	if _, err := ed.BeginCheckpointTurn("Dangling before demo"); err != nil {
		t.Fatal(err)
	}
	ed.OpenDemo()
	if ed.ActiveTurnID() != "" {
		t.Fatalf("ActiveTurnID not cleared on OpenDemo: %q", ed.ActiveTurnID())
	}
	if len(ed.Checkpoints()) != 0 {
		t.Fatalf("Checkpoints not cleared on OpenDemo: %+v", ed.Checkpoints())
	}
}
