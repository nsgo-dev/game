package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editorcmd"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestNewOpensTheWardWithTheHealthPlugin(t *testing.T) {
	ed, err := New("../..")
	if err != nil {
		t.Fatal(err)
	}
	if got := ed.ActivePlugins(); !reflect.DeepEqual(got, []string{"health"}) {
		t.Fatalf("ActivePlugins = %v, want [health]", got)
	}
	if ed.Doc == nil || ed.Doc.Name != "Ward" || ed.ActiveLayer != "patients" {
		t.Fatalf("opened %+v on layer %q", ed.Doc, ed.ActiveLayer)
	}
	ed.SelectObject("ada")
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	for _, id := range []string{"id:plugin-panel-health-ward", "id:inspector-plugin-health-health", "id:tool-health-admit"} {
		if !app.Exists(id) {
			t.Errorf("the editor shows no %s", id)
		}
	}
}

func TestNewRefusesAProjectListingPluginsItLacks(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"game.yaml", "scenes/ward.scene.yaml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if name == "game.yaml" {
			data = []byte(strings.Replace(string(data), "plugins: [health]", "plugins: [health, terrain]", 1))
		}
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := New(dir); err == nil || !strings.Contains(err.Error(), "terrain") {
		t.Fatalf("New = %v, want an error naming terrain", err)
	}
	if _, err := New(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("New opened a directory without a project")
	}
}

func TestNewOpensAProjectByItsGameYAML(t *testing.T) {
	ed, err := New(filepath.Join("..", "..", "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := ed.ActivePlugins(); !reflect.DeepEqual(got, []string{"health"}) || ed.Doc == nil || ed.Doc.Name != "Ward" {
		t.Fatalf("opened %+v with plugins %v", ed.Doc, got)
	}
}

func TestTheCommandLineOpensAClipOnAPatient(t *testing.T) {
	clip := filepath.Join(t.TempDir(), "pulse.animation.yaml")
	for _, args := range [][]string{
		{"../..", clip, "ada"},
		{filepath.Join("..", "..", "scenes", "ward.scene.yaml"), clip, "ada"},
	} {
		var stderr bytes.Buffer
		var shown *editor.Editor
		if err := editorcmd.Run(args, options(), &stderr, func(ed *editor.Editor) { shown = ed }); err != nil {
			t.Fatalf("Run(%q): %v (stderr %s)", args, err, &stderr)
		}
		if shown == nil || shown.Doc.Name != "Ward" || shown.AppName != "Field Medic Editor" || !reflect.DeepEqual(shown.ActivePlugins(), []string{"health"}) || shown.OnSaveClip == nil {
			t.Fatalf("Run(%q) showed %+v", args, shown)
		}
		if o := options(); o.WindowWidth != 1280 || o.WindowHeight != 800 {
			t.Fatalf("the editor opens a %vx%v window, want 1280x800", o.WindowWidth, o.WindowHeight)
		}
		if got := shown.SelectedID; got != "ada" {
			t.Fatalf("Run(%q) selected %q, want ada", args, got)
		}
	}
}
