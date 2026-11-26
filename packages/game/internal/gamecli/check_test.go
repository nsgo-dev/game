package gamecli

import (
	"os"
	"path/filepath"
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
	want := filepath.Join(dir, "scenes", "level.scene.yaml") + `:10: sceneformat: connection 0 ("west/button" pressed -> "west/gate" open): the scene has no object "west/gate", counting those of the scenes it instances and inherits` + "\ngame check: 1 problem\n"
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
