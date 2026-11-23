package main

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
