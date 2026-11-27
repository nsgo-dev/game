package gamecli

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeFiles writes files, keyed by slash-separated path, under dir.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const checkScene = "version: 1\nlayers:\n  - name: a\n    tilemap:\n      source: level.tmj\n"

func TestCheckReportsFileAndLine(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"room.scene.yaml": checkScene})

	for _, args := range [][]string{{"check", dir}, {"check", "-in", dir}} {
		a, stdout, stderr := newTestApp(t)
		if code := run(args, a); code != 1 {
			t.Fatalf("%q exit %d, want 1 (stderr: %s)", args, code, stderr)
		}
		want := filepath.Join(dir, "room.scene.yaml") + `:5: tilemap source "level.tmj": not found` + "\ngame check: 1 problem\n"
		if stderr.String() != want {
			t.Fatalf("%q stderr = %q, want %q", args, stderr, want)
		}
		if stdout.Len() != 0 {
			t.Fatalf("%q wrote to stdout: %q", args, stdout)
		}
	}
}

func TestCheckPassesAndSummarizes(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"room.scene.yaml": checkScene,
		"level.tmj":       `{"width": 1, "height": 1, "tilewidth": 16, "tileheight": 16, "layers": []}`,
	})
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if got, want := stdout.String(), "1 scene, 1 map, 0 tilesets, 0 atlases: no problems\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestCheckDefaultsToTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a/room.scene.yaml": checkScene, "b/room.scene.yaml": checkScene})
	t.Chdir(dir)
	a, _, stderr := newTestApp(t)
	if code := run([]string{"check"}, a); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.HasPrefix(stderr.String(), "a/room.scene.yaml:5: ") || !strings.Contains(stderr.String(), "\nb/room.scene.yaml:5: ") || !strings.HasSuffix(stderr.String(), "game check: 2 problems\n") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestCheckErrors(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"check", "-in", dir, dir}, 2, "specify either -in or a positional directory"},
		{[]string{"check", dir, dir}, 2, "expected at most one directory"},
		{[]string{"check", filepath.Join(dir, "missing")}, 1, "game check: "},
		{[]string{"check", filepath.Join(dir, "file.txt")}, 1, "is not a directory"},
	}
	writeFiles(t, dir, map[string]string{"file.txt": "x"})
	for _, tc := range cases {
		a, _, stderr := newTestApp(t)
		if code := run(tc.args, a); code != tc.code {
			t.Errorf("%q exit %d, want %d", tc.args, code, tc.code)
		}
		if !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("%q stderr = %q, want %q", tc.args, stderr, tc.want)
		}
	}
}

// A connection to an object an instanced scene does not hold fails game check at the line that names it.
func TestCheckReportsConnectionsAcrossScenes(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"nsgo.yaml":              "name: test\n",
		"scenes/pair.scene.yaml": "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: button\n      - id: door\n",
		"scenes/level.scene.yaml": "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: west\n        scene: scenes/pair.scene.yaml\n" +
			"connections:\n  - from: west/button\n    event: pressed\n    to: west/gate\n    handler: open\n",
	})
	a, _, stderr := newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 1 {
		t.Fatalf("exit %d, want 1 (stderr: %s)", code, stderr)
	}
	want := filepath.Join(dir, "scenes", "level.scene.yaml") + `:10: sceneformat: connection 0 ("west/button" pressed -> "west/gate" open): the scene has no object "west/gate", counting those of the scenes it instances and inherits` +
		"\ncheck: note: connection events and handlers were not checked without a project editor command; run `game generate editor` to enable it" +
		"\ngame check: 1 problem\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
}

// Acceptance: A missing line id is reported by game check with file and line.
func TestCheckReportsMissingCutsceneLineID(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"nsgo.yaml":                  "name: test\n",
		"scenes/level_01.scene.yaml": "version: 2\nlayers:\n  - name: world\n    objects:\n      - id: knight\n",
		"loc/en.yaml":                "version: 1\nlocale: en\nstrings:\n  intro.knight.01: Hello\n",
		"cutscenes/floor1_intro.cutscene.yaml": "version: 2\nkind: timeline\nscene: scenes/level_01.scene.yaml\nlength: 5s\ntracks:\n" +
			"  - kind: dialogue\n    clips:\n      - start: 1s\n        duration: 2s\n        line: intro.knight.missing\n",
	})
	a, _, stderr := newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 1 {
		t.Fatalf("exit %d, want 1 (stderr: %s)", code, stderr)
	}
	want := filepath.Join(dir, "cutscenes", "floor1_intro.cutscene.yaml") + `:10: dialogue line "intro.knight.missing": not found in localization tables` + "\ngame check: 1 problem\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
}

func TestCheckReportsChineseScreenOverflow(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"game.yaml": "version: 1\nname: Test\nid: dev.nsgo.test\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n",
		"screens/hud.screen.yaml": "version: 1\ndesignWidth: 640\ndesignHeight: 360\nnodes:\n" +
			"  - id: hint\n    type: label\n    anchor: bottom_center\n    width: 160\n    height: 22\n" +
			"    padding:\n      left: 6\n      right: 6\n    textKey: hud.hint.lever\n    textColor: '#ffffff'\n    background: '#101216'\n",
		"locales/en.yaml": "version: 1\nlocale: en\nstrings:\n  hud.hint.lever: Jump at levers to pull\n",
		"locales/zh.yaml": "version: 1\nlocale: zh\nstrings:\n  hud.hint.lever: 靠近拉杆时按下跳跃键即可拉动\n",
	})
	a, _, stderr := newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 1 {
		t.Fatalf("exit %d, want 1 (stderr: %s)", code, stderr)
	}
	got := stderr.String()
	for _, want := range []string{
		filepath.Join(dir, "screens", "hud.screen.yaml") + `:8: node "hint" (locale "zh")`,
		`overflows 160 pt box`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("stderr = %q, want substring %q", got, want)
		}
	}
}

func TestCheckReportsSceneAboveFallbackLightCap(t *testing.T) {
	var b strings.Builder
	b.WriteString("version: 4\nname: Torch Corridor\nlayers:\n  - name: world\n    objects:\n")
	for i := range 30 {
		b.WriteString("      - id: torch_")
		b.WriteString(strings.Repeat("0", 2-len(string(rune('0'+i/10))+string(rune('0'+i%10)))))
		b.WriteByte(byte('0' + i/10))
		b.WriteByte(byte('0' + i%10))
		b.WriteString("\n        light:\n          kind: point\n          range: 4\n")
	}
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"scenes/corridor.scene.yaml": b.String(),
	})
	a, _, stderr := newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 1 {
		t.Fatalf("exit %d, want 1 (stderr: %s)", code, stderr)
	}
	got := stderr.String()
	for _, want := range []string{
		filepath.Join(dir, "scenes", "corridor.scene.yaml") + ":",
		"warning: scene has 30 local lights",
		"16-light fallback cap",
		"14 dropped",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("stderr = %q, want substring %q", got, want)
		}
	}
}

func TestCheckRunsProjectsOwnEditor(t *testing.T) {
	const summary = "1 scene, 0 maps, 0 tilesets, 0 atlases: no problems\n"
	for _, tc := range []struct {
		name  string
		files map[string]string
		run   []string
	}{
		{"gameeditor tag", map[string]string{
			"main.go":   "//go:build !gameeditor\n\npackage main\n\nfunc main() {}\n",
			"editor.go": "//go:build gameeditor\n\npackage main\n\nfunc main() {}\n",
		}, []string{"run", "-tags", "gameeditor", "."}},
		{"cmd/editor", map[string]string{
			"main.go":            "package main\n\nfunc main() {}\n",
			"cmd/editor/main.go": "package main\n\nimport \"github.com/nsgo-dev/game/pkg/editor/editorcmd\"\n\nfunc main() { editorcmd.Main(editorcmd.Options{}) }\n",
		}, []string{"run", "./cmd/editor"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := generateProject(t, tc.files)

			// Explicit dir and -in both invoke the project's editor command with -check <absDir>.
			for _, args := range [][]string{{"check", dir}, {"check", "-in", dir}} {
				a, stdout, stderr := newTestApp(t)
				var gotDir string
				var gotArgs []string
				a.goRun = func(d string, args []string, out, _ io.Writer) error {
					gotDir, gotArgs = d, args
					_, err := io.WriteString(out, summary)
					return err
				}
				if code := run(args, a); code != 0 {
					t.Fatalf("%q exit %d (stderr: %s)", args, code, stderr)
				}
				want := append(slices.Clone(tc.run), "-check", dir)
				if gotDir != dir || !slices.Equal(gotArgs, want) {
					t.Fatalf("%q ran go %q in %s, want go %q in %s", args, gotArgs, gotDir, want, dir)
				}
				if stdout.String() != summary {
					t.Fatalf("%q stdout = %q, want %q", args, stdout.String(), summary)
				}
			}

			// Default working directory invokes -check . in .
			t.Chdir(dir)
			a, stdout, stderr := newTestApp(t)
			var gotDir string
			var gotArgs []string
			a.goRun = func(d string, args []string, out, _ io.Writer) error {
				gotDir, gotArgs = d, args
				_, err := io.WriteString(out, summary)
				return err
			}
			if code := run([]string{"check"}, a); code != 0 {
				t.Fatalf("no-arg exit %d (stderr: %s)", code, stderr)
			}
			wantDot := append(slices.Clone(tc.run), "-check", ".")
			if gotDir != "." || !slices.Equal(gotArgs, wantDot) {
				t.Fatalf("no-arg ran go %q in %s, want go %q in .", gotArgs, gotDir, wantDot)
			}
			if stdout.String() != summary {
				t.Fatalf("no-arg stdout = %q, want %q", stdout.String(), summary)
			}

			// Failing project editor exits 1 and names the editor command.
			a, _, stderr = newTestApp(t)
			a.goRun = func(_ string, _ []string, _, errOut io.Writer) error {
				_, _ = io.WriteString(errOut, "scenes/level.scene.yaml:12: bad handler\n")
				return errors.New("exit status 1")
			}
			if code := run([]string{"check", dir}, a); code != 1 || !strings.Contains(stderr.String(), "bad handler") || !strings.Contains(stderr.String(), "the project's editor (go "+strings.Join(tc.run, " ")) {
				t.Fatalf("failing editor: exit %d: %s", code, stderr)
			}
		})
	}
}

func TestCheckWithoutOwnEditorNotesUncheckedConnectionsAndChecksObjects(t *testing.T) {
	// A project without an editor command still checks connection objects and prints one note explaining how to enable event/handler checks.
	dir := generateProject(t, nil)
	writeFiles(t, dir, map[string]string{
		"scenes/pair.scene.yaml": "version: 2\nlayers:\n  - name: actors\n    objects:\n      - id: button\n      - id: door\nconnections:\n  - from: button\n    event: typo_event\n    to: door\n    handler: typo_handler\n",
	})
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if got, want := stdout.String(), "1 scene, 0 maps, 0 tilesets, 0 atlases: no problems\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if !strings.Contains(stderr.String(), "connection events and handlers were not checked") || !strings.Contains(stderr.String(), "game generate editor") {
		t.Fatalf("stderr = %q, want note about events/handlers and game generate editor", stderr)
	}

	// Missing object in a standalone scene still fails and still prints the note.
	writeFiles(t, dir, map[string]string{
		"scenes/pair.scene.yaml": "version: 2\nlayers:\n  - name: actors\n    objects:\n      - id: button\nconnections:\n  - from: button\n    event: pressed\n    to: ghost\n    handler: open\n",
	})
	a, _, stderr = newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 1 {
		t.Fatalf("missing object exit %d, want 1 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr.String(), `"ghost"`) || !strings.Contains(stderr.String(), "names no object") || !strings.Contains(stderr.String(), "game generate editor") {
		t.Fatalf("stderr = %q, want missing object error and note", stderr)
	}

	// cmd/editor without package editorcmd prints a note and falls back to object-only checks.
	other := generateProject(t, map[string]string{"cmd/editor/main.go": "package main\n\nfunc main() {}\n"})
	writeFiles(t, other, map[string]string{
		"scenes/pair.scene.yaml": "version: 2\nlayers:\n  - name: actors\n    objects:\n      - id: button\n      - id: door\nconnections:\n  - from: button\n    event: pressed\n    to: door\n    handler: open\n",
	})
	a, _, stderr = newTestApp(t)
	a.goRun = func(string, []string, io.Writer, io.Writer) error {
		t.Error("ran a cmd/editor without editorcmd")
		return nil
	}
	if code := run([]string{"check", other}, a); code != 0 {
		t.Fatalf("cmd/editor without editorcmd exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr.String(), "does not use package editorcmd") {
		t.Fatalf("stderr = %q, want note about editorcmd", stderr)
	}
}

// End-to-end acceptance test for #1513: a project generated with `game new` and `game generate editor`
// runs its own editor command on `game check`, passing valid connections (including built-in physics)
// and failing with file and line on a missing event, missing handler, unknown handler argument, or
// override breaking a brought connection across instanced and inherited scenes.
func TestCheckWithGeneratedEditorChecksEventsHandlersAndArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a project")
	}
	gameDir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	stubGo(t, nil)
	dir := filepath.Join(t.TempDir(), "mygame")
	a, _, stderr := newTestApp(t)
	if code := run([]string{"new", "-module", "example.com/mygame", dir}, a); code != 0 {
		t.Fatalf("game new: exit %d: %s", code, stderr)
	}
	if code, _, stderr := runGenerate(t, dir, "editor"); code != 0 {
		t.Fatalf("game generate editor: exit %d: %s", code, stderr)
	}

	writeFiles(t, dir, map[string]string{
		"components.go": `package main

import "github.com/nsgo-dev/game/pkg/sceneformat"

var Components = sceneformat.NewComponentRegistry()

func init() {
	Components.MustRegister(sceneformat.ComponentType{
		Name:   "button",
		Events: []sceneformat.Event{{Name: "pressed"}},
	})
	Components.MustRegister(sceneformat.ComponentType{
		Name: "door",
		Handlers: []sceneformat.Handler{{
			Name:   "open",
			Params: []sceneformat.Field{{Name: "speed", Kind: sceneformat.FieldFloat, Default: 1.0, Min: sceneformat.Float64(0)}},
		}},
	})
	Components.MustRegister(sceneformat.ComponentType{
		Name:   "switch",
		Events: []sceneformat.Event{{Name: "pressed"}},
	})
}
`,
	})

	env := append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOFLAGS="+strings.TrimSpace(os.Getenv("GOFLAGS")+" -mod=mod"))
	editCmd := exec.Command("go", "mod", "edit", "-replace="+gameModule+"="+gameDir)
	editCmd.Dir, editCmd.Env = dir, env
	if out, err := editCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod edit: %v\n%s", err, out)
	}

	offlineGoRun := func(runDir string, args []string, stdout, errOut io.Writer) error {
		cmd := exec.Command("go", args...)
		cmd.Dir, cmd.Env = runDir, env
		cmd.Stdout, cmd.Stderr = stdout, errOut
		return cmd.Run()
	}

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
	writeFiles(t, dir, map[string]string{
		"scenes/pair.scene.yaml":    pairScene,
		"scenes/level.scene.yaml":   levelScene,
		"scenes/derived.scene.yaml": derivedScene,
	})

	// Clean scenes pass.
	a, stdout, stderr := newTestApp(t)
	a.goRun = offlineGoRun
	if code := run([]string{"check", dir}, a); code != 0 {
		t.Fatalf("clean game check: exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout.String(), "no problems") {
		t.Fatalf("clean stdout = %q, want no problems", stdout)
	}

	// Break event, handler, unknown arg, and brought connection via override.
	writeFiles(t, dir, map[string]string{
		"scenes/own.scene.yaml":     strings.Replace(pairScene, "handler: open", "handler: opne", 1),
		"scenes/level.scene.yaml":   strings.Replace(levelScene, "event: pressed", "event: held", 1),
		"scenes/derived.scene.yaml": strings.Replace(derivedScene, "handler: open\n", "handler: open\n    args:\n      force: 2\n", 1),
		"scenes/override.scene.yaml": `version: 2
inherits: scenes/pair.scene.yaml
overrides:
  button:
    components:
      switch: {}
`,
	})
	a, stdout, stderr = newTestApp(t)
	a.goRun = offlineGoRun
	if code := run([]string{"check", dir}, a); code != 1 {
		t.Fatalf("broken game check: exit %d, want 1\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	got := stderr.String()
	for _, want := range []string{
		filepath.Join(dir, "scenes", "own.scene.yaml") + `:16: sceneformat: connection 0 ("button" pressed -> "door" opne): no registered component of object "door" declares handler "opne"`,
		filepath.Join(dir, "scenes", "level.scene.yaml") + `:12: sceneformat: connection 0 ("lever" held -> "west/door" open): no registered component of object "lever" declares event "held"`,
		filepath.Join(dir, "scenes", "derived.scene.yaml") + `:15: sceneformat: connection 0 ("lever" pressed -> "door" open) argument "force": unknown argument`,
		filepath.Join(dir, "scenes", "override.scene.yaml") + `:4: sceneformat: the override of object "button" breaks connection ("button" pressed -> "door" open)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr missing %q:\n%s", want, got)
		}
	}
}
