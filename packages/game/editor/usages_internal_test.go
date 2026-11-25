package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUsagePreviewCacheAvoidsDiskRereadAcrossKeystrokes(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"game.yaml": `version: 1
name: Cache Test
id: dev.example.cache
appVersion: 1.0.0
window:
  width: 640
  height: 360
`,
		"prefabs/slime.prefab.yaml": "version: 1\nname: slime\n",
		"game/spawn.go":             "package game\n\nconst slimePrefab = \"prefabs/slime.prefab.yaml\"\n",
	}
	for rel, content := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ed, err := New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.PreviewSafeRename("prefabs/slime.prefab.yaml", "slime_green"); err != nil {
		t.Fatalf("PreviewSafeRename: %v", err)
	}
	ed.FocusUsage("game/spawn.go", 0)

	// Populate the preview cache on first render.
	st1 := ed.ReferencesScreen()
	_, _, lines1 := ed.buildUsagePreviewLines(st1)
	if len(lines1) == 0 {
		t.Fatal("expected Go preview lines")
	}

	// Overwrite game/spawn.go on disk without bumping filesRev, then type into Rename input.
	// Because the preview cache is keyed on (file, focus, filesRev, report), typing must serve cached lines with 0 disk reads.
	goAbs := filepath.Join(dir, "game", "spawn.go")
	if err := os.WriteFile(goAbs, []byte("package game\n\n// CHANGED ON DISK\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = ed.SetSafeRenameInput("slime_v2")
	st2 := ed.ReferencesScreen()
	_, _, lines2 := ed.buildUsagePreviewLines(st2)
	joined := ""
	for _, l := range lines2 {
		joined += l.Text + "\n"
	}
	if !strings.Contains(joined, "slimePrefab") || strings.Contains(joined, "CHANGED ON DISK") {
		t.Fatalf("expected cached preview lines without re-reading disk on keystroke, got:\n%s", joined)
	}

	// Bumping filesRev invalidates the cache and reads fresh contents from disk.
	ed.filesRev++
	_, _, lines3 := ed.buildUsagePreviewLines(ed.ReferencesScreen())
	joined3 := ""
	for _, l := range lines3 {
		joined3 += l.Text + "\n"
	}
	if !strings.Contains(joined3, "CHANGED ON DISK") {
		t.Fatalf("expected cache invalidation after filesRev bump, got:\n%s", joined3)
	}
}
