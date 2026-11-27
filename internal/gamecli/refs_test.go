package gamecli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// refsProject is a project whose scene names a material, which has no uid yet.
var refsProject = map[string]string{
	"game.yaml":                     "version: 1\nname: Cave\nid: dev.example.cave\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n",
	"scenes/cave.scene.yaml":        "version: 2\nlayers:\n  - name: walls\n    objects:\n      - id: wall\n        material: materials/stone.material.yaml\n",
	"materials/stone.material.yaml": "type: material\nversion: 1\nname: Stone\n",
}

// readPath returns the content of the file at name, a path from the working directory.
func readPath(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestIndexMvAndCheck(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, refsProject)
	t.Chdir(filepath.Join(dir, "scenes"))

	// game index, run below the project root, finds the root, gives both files a uid and writes the index.
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"index", "-dry-run"}, a); code != 0 {
		t.Fatalf("index -dry-run exit %d (stderr: %s)", code, stderr)
	}
	if want := "../materials/stone.material.yaml: add a uid\ncave.scene.yaml: add a uid\nwrite ../game.index.yaml\nwrite ../materials/stone.material.yaml\nwrite cave.scene.yaml\n"; stdout.String() != want {
		t.Fatalf("index -dry-run stdout = %q, want %q", stdout, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "game.index.yaml")); !os.IsNotExist(err) {
		t.Fatalf("index -dry-run wrote the index: %v", err)
	}
	a, _, stderr = newTestApp(t)
	if code := run([]string{"index"}, a); code != 0 {
		t.Fatalf("index exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(readPath(t, filepath.Join(dir, "materials", "stone.material.yaml")), "\nversion: 1\nuid: ") {
		t.Fatalf("material has no uid:\n%s", readPath(t, filepath.Join(dir, "materials", "stone.material.yaml")))
	}
	a, stdout, _ = newTestApp(t)
	if code := run([]string{"index", dir}, a); code != 0 || stdout.String() != "the index is up to date\n" {
		t.Fatalf("second index exit %d, stdout %q", code, stdout)
	}

	// game mv takes paths from the working directory and rewrites the scene.
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"mv", "../materials/stone.material.yaml", "../materials/rock.material.yaml"}, a); code != 0 {
		t.Fatalf("mv exit %d (stderr: %s)", code, stderr)
	}
	if want := "move ../materials/stone.material.yaml to ../materials/rock.material.yaml\ncave.scene.yaml:7: update material \"materials/stone.material.yaml\"\nwrite ../game.index.yaml\nwrite cave.scene.yaml\n"; stdout.String() != want {
		t.Fatalf("mv stdout = %q, want %q", stdout, want)
	}
	if got := readPath(t, "cave.scene.yaml"); !strings.HasSuffix(got, "        material: materials/rock.material.yaml\n") {
		t.Fatalf("scene:\n%s", got)
	}

	// A move made without the tools fails game check at the reference, saying where the file went, until game index follows it.
	if err := os.Rename(filepath.Join(dir, "materials", "rock.material.yaml"), filepath.Join(dir, "materials", "granite.material.yaml")); err != nil {
		t.Fatal(err)
	}
	a, _, stderr = newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 1 {
		t.Fatalf("check exit %d, want 1 (stderr: %s)", code, stderr)
	}
	want := filepath.Join(dir, "scenes", "cave.scene.yaml") + `:7: material "materials/rock.material.yaml": not found; it moved to "materials/granite.material.yaml"; run game index to update the reference` + "\ngame check: 1 problem\n"
	if stderr.String() != want {
		t.Fatalf("check stderr = %q, want %q", stderr, want)
	}
	a, _, stderr = newTestApp(t)
	if code := run([]string{"index", "-in", dir}, a); code != 0 {
		t.Fatalf("index exit %d (stderr: %s)", code, stderr)
	}
	a, _, stderr = newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 0 {
		t.Fatalf("check after index exit %d (stderr: %s)", code, stderr)
	}
}

func TestMvAndIndexErrors(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, refsProject)
	outside := t.TempDir()
	cases := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"mv", "a"}, 2, "expected a file to move and where to move it, got 1 arguments"},
		{[]string{"mv", "-in", dir, filepath.Join(dir, "game.yaml"), filepath.Join(dir, "x", "game.yaml")}, 1, "game.yaml and game.index.yaml stay at the project root"},
		{[]string{"mv", "-in", dir, filepath.Join(outside, "a.png"), filepath.Join(dir, "a.png")}, 1, "is not inside the project directory"},
		{[]string{"mv", "-in", dir, dir, filepath.Join(dir, "x")}, 1, "is not inside the project directory"},
		{[]string{"mv", "-in", filepath.Join(dir, "game.yaml"), "a", "b"}, 1, "is not a directory"},
		{[]string{"mv", "-in", filepath.Join(dir, "none"), "a", "b"}, 1, "game mv: "},
		{[]string{"index", "-in", dir, dir}, 2, "specify either -in or a positional directory"},
		{[]string{"index", dir, dir}, 2, "expected at most one directory"},
	}
	for _, tc := range cases {
		a, _, stderr := newTestApp(t)
		if code := run(tc.args, a); code != tc.code {
			t.Errorf("%q exit %d, want %d (stderr: %s)", tc.args, code, tc.code, stderr)
		}
		if !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("%q stderr = %q, want %q", tc.args, stderr, tc.want)
		}
	}
}

func TestMvScreenSpriteAndTextKey(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"game.yaml":               "version: 1\nname: Test\nid: dev.nsgo.test\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n",
		"art/heart.png":           "png",
		"screens/hud.screen.yaml": "version: 1\ndesignWidth: 640\ndesignHeight: 360\nnodes:\n  - id: hearts\n    type: sprite_row\n    sprite: art/heart.png\n  - id: hint\n    type: label\n    textKey: hud.hint\n",
		"locales/en.yaml":         "version: 1\nlocale: en\nstrings:\n  hud.hint: Jump\n",
	})

	// Move sprite file referenced by screen.
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"mv", "-in", dir, filepath.Join(dir, "art", "heart.png"), filepath.Join(dir, "art", "ui", "heart.png")}, a); code != 0 {
		t.Fatalf("mv sprite exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), `update sprite "art/heart.png"`) {
		t.Fatalf("mv sprite stdout = %q", stdout)
	}
	if got := readPath(t, filepath.Join(dir, "screens", "hud.screen.yaml")); !strings.Contains(got, "sprite: art/ui/heart.png") {
		t.Fatalf("screen not rewritten:\n%s", got)
	}

	// Rename text key across screen + locale table with -text-key.
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"mv", "-in", dir, "-text-key", "hud.hint", "hud.tip"}, a); code != 0 {
		t.Fatalf("mv -text-key exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), `update text key "hud.hint"`) || !strings.Contains(stdout.String(), `update string key "hud.hint"`) {
		t.Fatalf("mv -text-key stdout = %q", stdout)
	}
	if got := readPath(t, filepath.Join(dir, "screens", "hud.screen.yaml")); !strings.Contains(got, "textKey: hud.tip") {
		t.Fatalf("screen textKey not rewritten:\n%s", got)
	}
	if got := readPath(t, filepath.Join(dir, "locales", "en.yaml")); !strings.Contains(got, "hud.tip: Jump") {
		t.Fatalf("locale string key not rewritten:\n%s", got)
	}
}

func TestMvPackFolderAndRefusal(t *testing.T) {
	dir := t.TempDir()
	const initialGameYAML = `version: 1
name: PackDemo
id: dev.nsgo.packdemo
appVersion: 0.1.0
window:
  width: 640
  height: 360
packs:
  - name: tiles
    kind: asset
    packVersion: 1.0.0
    sha256: 0000000000000000000000000000000000000000000000000000000000000000
    dir: packs/tiles
    files:
      - pack.yaml
      - tiles.png
`
	writeFiles(t, dir, map[string]string{
		"game.yaml":             initialGameYAML,
		"packs/tiles/pack.yaml": "version: 1\n",
		"packs/tiles/tiles.png": "png",
	})

	// Moving a single tracked pack file out is refused before anything changes.
	a, _, stderr := newTestApp(t)
	if code := run([]string{"mv", "-in", dir, filepath.Join(dir, "packs", "tiles", "tiles.png"), filepath.Join(dir, "art", "tiles.png")}, a); code != 1 {
		t.Fatalf("mv tracked pack file exit %d, want 1 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr.String(), `belongs to installed pack "tiles"`) {
		t.Fatalf("stderr = %q", stderr)
	}

	// Dry-run reports the folder move and game.yaml pack dir update without changing disk.
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"mv", "-in", dir, "-dry-run", filepath.Join(dir, "packs", "tiles"), filepath.Join(dir, "art", "tiles")}, a); code != 0 {
		t.Fatalf("mv -dry-run pack folder exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), `update pack dir "packs/tiles"`) || !strings.Contains(stdout.String(), "write ") {
		t.Fatalf("mv -dry-run stdout = %q", stdout)
	}
	if got := readPath(t, filepath.Join(dir, "game.yaml")); got != initialGameYAML {
		t.Fatalf("dry-run modified game.yaml:\n%s", got)
	}

	// Moving the pack folder rewrites packs[].dir in place and passes game check.
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"mv", "-in", dir, filepath.Join(dir, "packs", "tiles"), filepath.Join(dir, "art", "tiles")}, a); code != 0 {
		t.Fatalf("mv pack folder exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), `update pack dir "packs/tiles"`) {
		t.Fatalf("mv stdout = %q", stdout)
	}
	if got, want := readPath(t, filepath.Join(dir, "game.yaml")), strings.Replace(initialGameYAML, "dir: packs/tiles", "dir: art/tiles", 1); got != want {
		t.Fatalf("game.yaml:\n%s\nwant:\n%s", got, want)
	}
	a, _, stderr = newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 0 {
		t.Fatalf("check after pack move exit %d (stderr: %s)", code, stderr)
	}
}
