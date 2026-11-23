package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func TestEditorOpensDemoScene(t *testing.T) {
	a, _, stderr := newTestApp(t)
	var opened *editor.Editor
	a.launch = func(ed *editor.Editor) { opened = ed }
	if code := run([]string{"editor"}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if opened == nil || opened.Doc.Name != "Demo Room" || opened.FilePath != "" {
		t.Fatalf("launched %+v, want the demo room", opened)
	}
}

func TestEditorOpensSceneFile(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Cave",
		VirtualWidth:  320,
		VirtualHeight: 180,
	}
	data, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cave.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	a, _, stderr := newTestApp(t)
	var opened *editor.Editor
	a.launch = func(ed *editor.Editor) { opened = ed }
	if code := run([]string{"editor", path}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if opened == nil || opened.Doc.Name != "Cave" || opened.FilePath != path {
		t.Fatalf("launched %+v, want %s", opened, path)
	}
}

func TestEditorErrors(t *testing.T) {
	a, _, stderr := newTestApp(t)
	if code := run([]string{"editor", filepath.Join(t.TempDir(), "missing.yaml")}, a); code != 1 {
		t.Fatalf("missing scene exit %d, want 1", code)
	}
	if !strings.HasPrefix(stderr.String(), "game editor: open ") {
		t.Fatalf("stderr = %q", stderr)
	}

	a, _, _ = newTestApp(t)
	if code := run([]string{"editor", "a.yaml", "b.yaml"}, a); code != 2 {
		t.Fatalf("two scenes exit %d, want 2", code)
	}
}
