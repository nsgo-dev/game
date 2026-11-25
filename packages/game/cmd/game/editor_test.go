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
	if code := run([]string{"editor", "a.yaml", "b.animation.yaml", "hero", "extra"}, a); code != 2 {
		t.Fatalf("four arguments exit %d, want 2", code)
	}
}

const heroClip = `type: animation
version: 1
name: Hop
length: 1
tracks:
  - property: x
    keys:
      - time: 0
        value: 64
      - time: 1
        value: 96
`

func TestOpenArgsOpensASavedSceneAndAnExistingClip(t *testing.T) {
	dir := t.TempDir()
	scene := filepath.Join(dir, "room.scene.yaml")
	clip := filepath.Join(dir, "hop.animation.yaml")
	demo, err := newDemoEditor()
	if err != nil {
		t.Fatalf("newDemoEditor: %v", err)
	}
	if err := demo.SaveFile(scene); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	if err := os.WriteFile(clip, []byte(heroClip), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := newDemoEditor()
	if err != nil {
		t.Fatalf("newDemoEditor: %v", err)
	}
	if err := openArgs(ed, []string{scene, clip}); err != nil {
		t.Fatalf("openArgs(scene, clip): %v", err)
	}
	if ed.Clip() == nil || ed.ClipTarget() != "hero" || ed.SelectedID != "hero" || ed.ClipPath() != clip {
		t.Fatalf("after openArgs: clip %v target %q selected %q path %q, want the clip on hero from %s", ed.Clip() != nil, ed.ClipTarget(), ed.SelectedID, ed.ClipPath(), clip)
	}
	if ed.OnSaveClip == nil {
		t.Fatal("OnSaveClip is not set")
	}
	data, err := ed.SaveClipYAML()
	if err != nil {
		t.Fatalf("SaveClipYAML: %v", err)
	}
	if string(data) != heroClip {
		t.Fatalf("clip after open and save:\n%s\nwant:\n%s", data, heroClip)
	}

	// Naming the object explicitly opens the clip on it.
	ed, _ = newDemoEditor()
	if err := openArgs(ed, []string{scene, clip, "hero"}); err != nil || ed.ClipTarget() != "hero" {
		t.Fatalf("openArgs with target hero: %v, target %q", err, ed.ClipTarget())
	}

	// A clip file that does not exist yet opens nothing but selects the object, so the Animate button starts a clip on it, and Save Clip writes the file.
	fresh := filepath.Join(dir, "new.animation.yaml")
	ed, _ = newDemoEditor()
	if err := openArgs(ed, []string{scene, fresh}); err != nil {
		t.Fatalf("openArgs with a new clip file: %v", err)
	}
	if ed.Clip() != nil || ed.SelectedID != "hero" || ed.OnSaveClip == nil {
		t.Fatalf("after openArgs with a new clip file: clip %v selected %q OnSaveClip %t", ed.Clip() != nil, ed.SelectedID, ed.OnSaveClip != nil)
	}
	if err := ed.OnSaveClip([]byte(heroClip)); err != nil {
		t.Fatalf("OnSaveClip: %v", err)
	}
	if got, err := os.ReadFile(fresh); err != nil || string(got) != heroClip {
		t.Fatalf("saved clip file: %q, %v", got, err)
	}

	ed, _ = newDemoEditor()
	if err := openArgs(ed, []string{scene, clip, "ghost"}); err == nil || !strings.Contains(err.Error(), `no object "ghost"`) {
		t.Fatalf("openArgs with a missing object: %v", err)
	}

	// The editor subcommand passes its arguments through openArgs.
	a, _, stderr := newTestApp(t)
	var opened *editor.Editor
	a.launch = func(ed *editor.Editor) { opened = ed }
	if code := run([]string{"editor", scene, clip}, a); code != 0 {
		t.Fatalf("editor scene clip exit %d (stderr: %s)", code, stderr)
	}
	if opened == nil || opened.ClipPath() != clip {
		t.Fatalf("editor scene clip launched %+v, want the clip %s open", opened, clip)
	}
}

const layeredProjectYAML = `version: 1
name: Cave
id: dev.example.cave
appVersion: 1.0.0
window:
  width: 320
  height: 180
autoload:
  mainScene: cave
  scenes:
    - name: cave
      path: scenes/cave.scene.yaml
`

const layeredCaveScene = `version: 2
name: Cave
layers:
  - name: ground
    tilemap:
      tileWidth: 16
      tileHeight: 16
      width: 2
      height: 1
      tilesetFile: tilesets/cave.tileset.yaml
      tileLayers:
        - name: floor
          rows:
            - 0 1
`

func TestEditorReadsTheOpenProjectsFiles(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{
		"game.yaml":                  layeredProjectYAML,
		"scenes/cave.scene.yaml":     layeredCaveScene,
		"tilesets/cave.tileset.yaml": "version: 1\ntileWidth: 16\ntileHeight: 16\nimage: tiles/cave.png\ncolumns: 2\ntileCount: 2\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The project directory and a scene file inside it both read the tileset the scene names from the project root.
	for _, arg := range []string{dir, filepath.Join(dir, "scenes", "cave.scene.yaml")} {
		a, _, stderr := newTestApp(t)
		var opened *editor.Editor
		a.launch = func(ed *editor.Editor) { opened = ed }
		if code := run([]string{"editor", arg}, a); code != 0 {
			t.Fatalf("%s: exit %d (stderr: %s)", arg, code, stderr)
		}
		if opened.BuildErr != nil {
			t.Fatalf("%s: build: %v", arg, opened.BuildErr)
		}
		if m := opened.Built.Layer("ground").Map; m == nil || m.TileAt(0, 1, 0) != 2 {
			t.Fatalf("%s: ground map = %+v", arg, m)
		}
		opened.SetActiveLayer("ground")
		if p := opened.TilePalette(); len(p.Tiles) != 2 || p.Message != "" {
			t.Fatalf("%s: palette = %+v", arg, p)
		}
	}
}
