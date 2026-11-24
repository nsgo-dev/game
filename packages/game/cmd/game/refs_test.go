package main

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

func readFile(t *testing.T, name string) string {
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
	if code := run([]string{"index", "-n"}, a); code != 0 {
		t.Fatalf("index -n exit %d (stderr: %s)", code, stderr)
	}
	if want := "../materials/stone.material.yaml: add a uid\ncave.scene.yaml: add a uid\nwrite ../game.index.yaml\nwrite ../materials/stone.material.yaml\nwrite cave.scene.yaml\n"; stdout.String() != want {
		t.Fatalf("index -n stdout = %q, want %q", stdout, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "game.index.yaml")); !os.IsNotExist(err) {
		t.Fatalf("index -n wrote the index: %v", err)
	}
	a, _, stderr = newTestApp(t)
	if code := run([]string{"index"}, a); code != 0 {
		t.Fatalf("index exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, "materials", "stone.material.yaml")), "\nversion: 1\nuid: ") {
		t.Fatalf("material has no uid:\n%s", readFile(t, filepath.Join(dir, "materials", "stone.material.yaml")))
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
	if got := readFile(t, "cave.scene.yaml"); !strings.HasSuffix(got, "        material: materials/rock.material.yaml\n") {
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
