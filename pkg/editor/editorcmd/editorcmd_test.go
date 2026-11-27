package editorcmd_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editorcmd"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const gameYAML = "version: 1\nname: Demo\nid: dev.example.demo\nappVersion: 1.0.0\nwindow:\n  width: 320\n  height: 180\nautoload:\n  mainScene: room\n  scenes:\n    - name: room\n      path: scenes/room.scene.yaml\n"

// writeProject writes files into a new directory and returns it.
func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// healthComponents registers health, with an hp field.
func healthComponents() *sceneformat.ComponentRegistry {
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{Name: "health", Fields: []sceneformat.Field{{Name: "hp", Kind: sceneformat.FieldInt, Default: 1}}})
	return reg
}

func TestOpenChecksTheProjectsComponents(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"game.yaml":              gameYAML,
		"scenes/room.scene.yaml": "version: 2\nname: Room\nlayers:\n  - name: main\n    objects:\n      - id: hero\n        components:\n          health: {hp: 3}\n",
		"scenes/bad.scene.yaml":  "version: 2\nname: Bad\nlayers:\n  - name: main\n    objects:\n      - id: hero\n        components:\n          health: {armor: 3}\n",
	})
	ed, err := editorcmd.New(editorcmd.Options{Components: healthComponents(), AppName: "Demo Editor"})
	if err != nil {
		t.Fatal(err)
	}
	if !ed.ProjectAssets || ed.AppName != "Demo Editor" || ed.ComponentRegistry() == nil {
		t.Fatalf("New = ProjectAssets %t, AppName %q, registry %v", ed.ProjectAssets, ed.AppName, ed.ComponentRegistry())
	}
	if err := editorcmd.Open(ed, []string{filepath.Join(dir, "scenes", "bad.scene.yaml")}); err == nil || !strings.Contains(err.Error(), `"armor"`) {
		t.Fatalf("Open of a scene with an unregistered field = %v, want it refused", err)
	}
	if err := editorcmd.Open(ed, []string{filepath.Join(dir, "game.yaml")}); err != nil {
		t.Fatalf("Open(game.yaml): %v", err)
	}
	if ed.Doc.Name != "Room" || ed.ScenePath != "scenes/room.scene.yaml" {
		t.Fatalf("opened %q at %q", ed.Doc.Name, ed.ScenePath)
	}

	// The game's registry is checked together with the built-in physics components, which the registry itself need not register.
	body := filepath.Join(dir, "scenes", "body.scene.yaml")
	if err := os.WriteFile(body, []byte("version: 2\nname: Body\nlayers:\n  - name: main\n    objects:\n      - id: crate\n        components:\n          health: {hp: 2}\n          body2d: {kind: static}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := editorcmd.Open(ed, []string{body}); err != nil {
		t.Fatalf("Open of a scene with a physics body: %v", err)
	}
	if _, ok := ed.ComponentRegistry().Lookup("body2d"); !ok {
		t.Fatalf("registry %v lacks body2d", ed.ComponentRegistry().Names())
	}
	game := healthComponents()
	if _, err := editorcmd.New(editorcmd.Options{Components: game}); err != nil {
		t.Fatal(err)
	}
	if names := game.Names(); len(names) != 1 {
		t.Fatalf("New changed the game's registry to %v", names)
	}

	// Without a registry, components are free-form.
	free, err := editorcmd.New(editorcmd.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := editorcmd.Open(free, []string{filepath.Join(dir, "scenes", "bad.scene.yaml")}); err != nil {
		t.Fatalf("Open without a registry: %v", err)
	}
	if free.Project == nil || free.ScenePath != "scenes/bad.scene.yaml" {
		t.Fatalf("a scene in the project opened without it: project %v, ScenePath %q", free.Project != nil, free.ScenePath)
	}
}

func TestOpenASceneOutsideAnyProject(t *testing.T) {
	dir := writeProject(t, map[string]string{"room.scene.yaml": "version: 2\nname: Loose\nlayers:\n  - name: main\n    objects:\n      - id: hero\n        sprite: {atlas: actors, frame: hero}\n"})
	ed, err := editorcmd.New(editorcmd.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := editorcmd.Open(ed, []string{filepath.Join(dir, "room.scene.yaml")}); err != nil {
		t.Fatal(err)
	}
	if ed.Project != nil || ed.ScenePath != "" || ed.Doc.Name != "Loose" || ed.BuildErr == nil {
		t.Fatalf("opened %q with project %v, ScenePath %q, build error %v; want it alone, its atlas unresolved", ed.Doc.Name, ed.Project != nil, ed.ScenePath, ed.BuildErr)
	}
}

func TestOpenErrors(t *testing.T) {
	dir := writeProject(t, map[string]string{"game.yaml": gameYAML, "scenes/room.scene.yaml": "version: 2\nname: Empty\nlayers: []\n"})
	ed, err := editorcmd.New(editorcmd.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"a", "b", "c", "d"}, "at most"},
		{[]string{filepath.Join(dir, "missing.scene.yaml")}, "open "},
		{[]string{dir, filepath.Join(dir, "hop.animation.yaml")}, "has no object to play it on"},
	} {
		if err := editorcmd.Open(ed, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Open(%q) = %v, want an error containing %q", tc.args, err, tc.want)
		}
	}
	if err := editorcmd.Open(ed, nil); err != nil {
		t.Errorf("Open(nil) = %v", err)
	}
}

func TestProjectRoot(t *testing.T) {
	dir := writeProject(t, map[string]string{"game.yaml": gameYAML, "scenes/deep/room.scene.yaml": "", "plain/notes.txt": "", "mod/go.mod": "module mod\n", "mod/room.scene.yaml": "", "app/nsgo.yaml": "", "app/scenes/room.scene.yaml": ""})
	alone := writeProject(t, map[string]string{"room.scene.yaml": ""})
	for _, tc := range []struct {
		path, root string
		ok         bool
	}{
		{dir, dir, true},
		{filepath.Join(dir, "game.yaml"), dir, true},
		{filepath.Join(dir, "scenes", "deep", "room.scene.yaml"), dir, true},
		{filepath.Join(dir, "scenes", "deep", "new.scene.yaml"), dir, true},
		{filepath.Join(dir, "plain"), filepath.Join(dir, "plain"), false},
		{filepath.Join(alone, "room.scene.yaml"), "", false},
		// A Go module or NSGo app of its own is the root of its files, so they do not open with the project above it.
		{filepath.Join(dir, "mod", "room.scene.yaml"), "", false},
		{filepath.Join(dir, "app", "scenes", "room.scene.yaml"), "", false},
	} {
		root, ok := editorcmd.ProjectRoot(tc.path)
		if ok != tc.ok || ok && root != tc.root {
			t.Errorf("ProjectRoot(%s) = %q, %t; want %q, %t", tc.path, root, ok, tc.root, tc.ok)
		}
	}
}

func TestRunOpensTheWorkingDirectorysProjectAndShowsIt(t *testing.T) {
	dir := writeProject(t, map[string]string{"game.yaml": gameYAML, "scenes/room.scene.yaml": "version: 2\nname: Room\nlayers:\n  - name: main\n    objects:\n      - id: hero\n        components:\n          health: {}\n"})
	t.Chdir(dir)
	var shown *editor.Editor
	var stderr bytes.Buffer
	if err := editorcmd.Run(nil, editorcmd.Options{Components: healthComponents()}, &stderr, func(ed *editor.Editor) { shown = ed }); err != nil {
		t.Fatalf("Run: %v (stderr %s)", err, &stderr)
	}
	if shown == nil || shown.Doc.Name != "Room" || shown.ComponentRegistry() == nil {
		t.Fatalf("Run showed %+v", shown)
	}
	if v, ok := shown.ComponentField("hero", "health", "hp"); !ok || v != 1 {
		t.Fatalf("hero's hp = %v, %t; want the registered default 1", v, ok)
	}

	// The window shows the project's scene.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, shown)
	}, nsgotest.Size(1120, 680))
	app.Find("id:editor-viewport")
	if !strings.Contains(shown.WindowTitle(), "scenes/room.scene.yaml") {
		t.Fatalf("WindowTitle = %q, want the scene's path in the project", shown.WindowTitle())
	}
}

func TestRunUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"-bogus"}, {"a", "b", "c", "d"}} {
		var stderr bytes.Buffer
		err := editorcmd.Run(args, editorcmd.Options{}, &stderr, func(*editor.Editor) { t.Error("shown") })
		if err == nil || !strings.Contains(stderr.String(), "usage: editor") {
			t.Errorf("Run(%q) = %v, stderr %q; want a usage error", args, err, &stderr)
		}
	}
	var stderr bytes.Buffer
	if err := editorcmd.Run([]string{"-h"}, editorcmd.Options{}, &stderr, nil); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("Run(-h) = %v, want flag.ErrHelp", err)
	}
	if err := editorcmd.Run([]string{filepath.Join(t.TempDir(), "missing.scene.yaml")}, editorcmd.Options{}, &stderr, nil); err == nil || errors.Is(err, flag.ErrHelp) {
		t.Errorf("Run on a missing scene = %v", err)
	}
}

func TestRunRequirePluginsRefusesAProjectListingPluginsItLacks(t *testing.T) {
	dir := writeProject(t, map[string]string{"game.yaml": gameYAML + "editor:\n  plugins: [terrain]\n", "scenes/room.scene.yaml": "version: 2\nname: Room\nlayers: []\n"})
	var stderr bytes.Buffer
	err := editorcmd.Run([]string{dir}, editorcmd.Options{RequirePlugins: true}, &stderr, func(*editor.Editor) { t.Error("shown") })
	if err == nil || !strings.Contains(err.Error(), "terrain") {
		t.Fatalf("Run with RequirePlugins = %v, want an error naming terrain", err)
	}
	var shown *editor.Editor
	if err := editorcmd.Run([]string{dir}, editorcmd.Options{}, &stderr, func(ed *editor.Editor) { shown = ed }); err != nil || shown == nil {
		t.Fatalf("Run without RequirePlugins = %v, showed %v", err, shown)
	}
}

func TestRunSchemaFlagWritesSceneSchemaWithoutWindow(t *testing.T) {
	reg := healthComponents()
	// Also override a physics component name to confirm game's type wins in withPhysicsComponents.
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "body2d",
		Doc:    "Custom body2d.",
		Fields: []sceneformat.Field{{Name: "customMass", Kind: sceneformat.FieldFloat, Default: 2.0}},
	})

	var stdout, stderr bytes.Buffer
	opts := editorcmd.Options{Components: reg, Stdout: &stdout}
	if err := editorcmd.Run([]string{"-schema"}, opts, &stderr, func(*editor.Editor) {
		t.Error("launch called when -schema was passed")
	}); err != nil {
		t.Fatalf("Run(-schema): %v (stderr: %s)", err, &stderr)
	}

	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON from -schema: %v\n%s", err, &stdout)
	}
	defs := doc["definitions"].(map[string]any)
	comps := defs["Components"].(map[string]any)
	props := comps["properties"].(map[string]any)
	for _, want := range []string{"health", "body2d", "collider2d", "skeletal"} {
		if _, ok := props[want]; !ok {
			t.Errorf("-schema missing component %q", want)
		}
	}
	bodyProps := props["body2d"].(map[string]any)["properties"].(map[string]any)
	if _, ok := bodyProps["customMass"]; !ok {
		t.Errorf("body2d did not keep game's custom type: %#v", bodyProps)
	}
	if comps["additionalProperties"] != false {
		t.Errorf("Components.additionalProperties = %v, want false", comps["additionalProperties"])
	}

	// Deterministic output across repeated runs.
	var second bytes.Buffer
	if err := editorcmd.Run([]string{"-schema"}, editorcmd.Options{Components: reg, Stdout: &second}, &stderr, func(*editor.Editor) {
		t.Error("launch called")
	}); err != nil {
		t.Fatalf("second Run(-schema): %v", err)
	}
	if !bytes.Equal(stdout.Bytes(), second.Bytes()) {
		t.Fatal("-schema output is not deterministic")
	}

	// Nil Components produces the built-in free-form schema.
	var builtin bytes.Buffer
	if err := editorcmd.Run([]string{"-schema"}, editorcmd.Options{Stdout: &builtin}, &stderr, func(*editor.Editor) {
		t.Error("launch called")
	}); err != nil {
		t.Fatalf("Run(-schema) with nil Components: %v", err)
	}
	var builtinDoc map[string]any
	if err := json.Unmarshal(builtin.Bytes(), &builtinDoc); err != nil {
		t.Fatalf("invalid JSON from builtin -schema: %v", err)
	}
	builtinComps := builtinDoc["definitions"].(map[string]any)["Components"].(map[string]any)
	if _, isMap := builtinComps["additionalProperties"].(map[string]any); !isMap {
		t.Errorf("builtin Components.additionalProperties = %#v, want empty object", builtinComps["additionalProperties"])
	}

	// Extra arguments with -schema return a usage error.
	stderr.Reset()
	if err := editorcmd.Run([]string{"-schema", "extra"}, editorcmd.Options{Stdout: &builtin}, &stderr, func(*editor.Editor) {
		t.Error("launch called")
	}); err == nil || !strings.Contains(stderr.String(), "-schema takes no arguments") {
		t.Fatalf("Run(-schema extra) = %v, stderr %q; want usage error", err, &stderr)
	}
}

func TestRunCheckFlagChecksConnectionsAndBuiltins(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "button",
		Events: []sceneformat.Event{{Name: "pressed"}},
	})
	reg.MustRegister(sceneformat.ComponentType{
		Name: "door",
		Handlers: []sceneformat.Handler{{
			Name:   "open",
			Params: []sceneformat.Field{{Name: "speed", Kind: sceneformat.FieldFloat, Default: 1.0, Min: sceneformat.Float64(0)}},
		}},
	})
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "switch",
		Events: []sceneformat.Event{{Name: "pressed"}},
	})

	pairScene := `version: 2
layers:
  - name: actors
    objects:
      - id: button
        components:
          button: {}
      - id: door
        components:
          door: {}
          body2d: {kind: static}
connections:
  - from: button
    event: pressed
    to: door
    handler: open
`
	levelScene := `version: 2
layers:
  - name: actors
    objects:
      - id: west
        scene: scenes/pair.scene.yaml
      - id: lever
        components:
          button: {}
connections:
  - from: lever
    event: pressed
    to: west/door
    handler: open
    args:
      speed: 2
`
	derivedScene := `version: 2
inherits: scenes/pair.scene.yaml
layers:
  - name: actors
    objects:
      - id: lever
        components:
          button: {}
connections:
  - from: lever
    event: pressed
    to: door
    handler: open
`

	dir := writeProject(t, map[string]string{
		"game.yaml":                 gameYAML,
		"scenes/room.scene.yaml":    "version: 2\nname: Room\nlayers: []\n",
		"scenes/pair.scene.yaml":    pairScene,
		"scenes/level.scene.yaml":   levelScene,
		"scenes/derived.scene.yaml": derivedScene,
	})

	// Clean project passes, prints summary to stdout, and never launches a window.
	var stdout, stderr bytes.Buffer
	if err := editorcmd.Run([]string{"-check", dir}, editorcmd.Options{Components: reg, Stdout: &stdout}, &stderr, func(*editor.Editor) {
		t.Error("launch called when -check was passed")
	}); err != nil {
		t.Fatalf("Run(-check) clean: %v (stderr: %s)", err, &stderr)
	}
	if !strings.Contains(stdout.String(), "4 scenes, 0 maps, 0 tilesets, 0 atlases: no problems") {
		t.Fatalf("stdout = %q, want summary", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr not empty on clean check: %q", stderr.String())
	}

	// Missing event, missing handler, unknown handler argument, and an override breaking a brought connection all fail with file and line.
	badPair := strings.Replace(pairScene, "handler: open", "handler: slam", 1)
	badLevel := strings.Replace(levelScene, "event: pressed", "event: held", 1)
	badDerived := strings.Replace(derivedScene, "handler: open\n", "handler: open\n    args:\n      force: 2\n", 1)
	badOverride := `version: 2
inherits: scenes/pair.scene.yaml
overrides:
  button:
    components:
      switch: {}
`
	badDir := writeProject(t, map[string]string{
		"game.yaml":                  gameYAML,
		"scenes/room.scene.yaml":     "version: 2\nname: Room\nlayers: []\n",
		"scenes/pair.scene.yaml":     pairScene,
		"scenes/own.scene.yaml":      badPair,
		"scenes/level.scene.yaml":    badLevel,
		"scenes/derived.scene.yaml":  badDerived,
		"scenes/override.scene.yaml": badOverride,
	})
	stdout.Reset()
	stderr.Reset()
	err := editorcmd.Run([]string{"-check", badDir}, editorcmd.Options{Components: reg, Stdout: &stdout}, &stderr, func(*editor.Editor) {
		t.Error("launch called when -check was passed")
	})
	if err == nil || err.Error() != "4 problems" {
		t.Fatalf("Run(-check) bad = %v, want 4 problems (stderr: %s)", err, &stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout not empty on failed check: %q", stdout.String())
	}
	got := stderr.String()
	for _, want := range []string{
		filepath.Join(badDir, "scenes", "own.scene.yaml") + `:16: sceneformat: connection 0 ("button" pressed -> "door" slam): no registered component of object "door" declares handler "slam"`,
		filepath.Join(badDir, "scenes", "level.scene.yaml") + `:12: sceneformat: connection 0 ("lever" held -> "west/door" open): no registered component of object "lever" declares event "held"`,
		filepath.Join(badDir, "scenes", "derived.scene.yaml") + `:15: sceneformat: connection 0 ("lever" pressed -> "door" open) argument "force": unknown argument`,
		filepath.Join(badDir, "scenes", "override.scene.yaml") + `:4: sceneformat: the override of object "button" breaks connection ("button" pressed -> "door" open)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr missing %q:\n%s", want, got)
		}
	}

	// Usage and path errors.
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-schema", "-check", dir}, "cannot be used together"},
		{[]string{"-check", dir, "extra"}, "-check takes no extra arguments"},
		{[]string{"-check", filepath.Join(dir, "game.yaml")}, "is not a directory"},
	} {
		stderr.Reset()
		err := editorcmd.Run(tc.args, editorcmd.Options{Components: reg, Stdout: &stdout}, &stderr, func(*editor.Editor) {
			t.Error("launch called")
		})
		if err == nil || (!strings.Contains(err.Error(), tc.want) && !strings.Contains(stderr.String(), tc.want)) {
			t.Errorf("Run(%q) = %v, stderr %q; want %q", tc.args, err, &stderr, tc.want)
		}
	}
}
