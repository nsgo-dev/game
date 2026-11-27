package gamecli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSchemaRunsProjectsOwnEditor(t *testing.T) {
	const fakeJSON = "{\"$schema\":\"http://json-schema.org/draft-07/schema#\",\"title\":\"Project\"}\n"
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

			// Prints to stdout when -out is omitted.
			a, stdout, stderr := newTestApp(t)
			var gotDir string
			var gotArgs []string
			a.goRun = func(d string, args []string, out, _ io.Writer) error {
				gotDir, gotArgs = d, args
				_, err := io.WriteString(out, fakeJSON)
				return err
			}
			if code := run([]string{"schema", dir}, a); code != 0 {
				t.Fatalf("exit %d (stderr: %s)", code, stderr)
			}
			want := append(slices.Clone(tc.run), "-schema")
			if gotDir != dir || !slices.Equal(gotArgs, want) {
				t.Fatalf("ran go %q in %s, want go %q in %s", gotArgs, gotDir, want, dir)
			}
			if stdout.String() != fakeJSON {
				t.Fatalf("stdout = %q, want %q", stdout.String(), fakeJSON)
			}

			// Writes to -out file (creating parent dirs) and leaves stdout empty.
			outFile := filepath.Join(t.TempDir(), "schemas", "scene.json")
			a, stdout, stderr = newTestApp(t)
			a.goRun = func(_ string, _ []string, out, _ io.Writer) error {
				_, err := io.WriteString(out, fakeJSON)
				return err
			}
			if code := run([]string{"schema", "-out", outFile, filepath.Join(dir, "game.yaml")}, a); code != 0 {
				t.Fatalf("-out exit %d (stderr: %s)", code, stderr)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout not empty with -out: %q", stdout.String())
			}
			got, err := os.ReadFile(outFile)
			if err != nil || string(got) != fakeJSON {
				t.Fatalf("read -out = %q, %v; want %q", got, err, fakeJSON)
			}

			// Failing project editor exits 1 and does not write -out.
			failOut := filepath.Join(t.TempDir(), "should-not-exist.json")
			a, _, stderr = newTestApp(t)
			a.goRun = func(string, []string, io.Writer, io.Writer) error { return errors.New("exit status 1") }
			if code := run([]string{"schema", "-out", failOut, dir}, a); code != 1 || !strings.Contains(stderr.String(), "the project's editor (go "+strings.Join(tc.run, " ")) {
				t.Fatalf("failing editor: exit %d: %s", code, stderr)
			}
			if _, err := os.Stat(failOut); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("-out file was created on editor failure: %v", err)
			}
		})
	}
}

func TestSchemaFallsBackToBuiltinWithoutOwnEditor(t *testing.T) {
	// Project without an editor command gets the built-in scene schema.
	dir := generateProject(t, nil)
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"schema", dir}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("invalid built-in schema JSON: %v\n%s", err, stdout)
	}
	if doc["$schema"] != "http://json-schema.org/draft-07/schema#" {
		t.Errorf("$schema = %v", doc["$schema"])
	}
	defs := doc["definitions"].(map[string]any)
	comps := defs["Components"].(map[string]any)
	props := comps["properties"].(map[string]any)
	for _, name := range []string{"body2d", "collider2d", "navregion2d", "skeletal"} {
		if _, ok := props[name]; !ok {
			t.Errorf("built-in schema missing %q", name)
		}
	}

	// Deterministic across repeated runs.
	a2, stdout2, stderr2 := newTestApp(t)
	if code := run([]string{"schema", dir}, a2); code != 0 {
		t.Fatalf("second exit %d (stderr: %s)", code, stderr2)
	}
	if !bytes.Equal(stdout.Bytes(), stdout2.Bytes()) {
		t.Fatal("built-in schema output is not deterministic")
	}

	// Default target (no args) inspects current working directory.
	t.Chdir(dir)
	a3, stdout3, stderr3 := newTestApp(t)
	if code := run([]string{"schema"}, a3); code != 0 {
		t.Fatalf("no-arg exit %d (stderr: %s)", code, stderr3)
	}
	if !bytes.Equal(stdout.Bytes(), stdout3.Bytes()) {
		t.Fatal("no-arg schema differed from explicit dir")
	}

	// cmd/editor without editorcmd prints a note and falls back to built-in schema.
	other := generateProject(t, map[string]string{"cmd/editor/main.go": "package main\n\nfunc main() {}\n"})
	a4, stdout4, stderr4 := newTestApp(t)
	if code := run([]string{"schema", other}, a4); code != 0 {
		t.Fatalf("cmd/editor without editorcmd exit %d (stderr: %s)", code, stderr4)
	}
	if !strings.Contains(stderr4.String(), "does not use package editorcmd") {
		t.Errorf("stderr = %q, want note about editorcmd", stderr4)
	}
	if !bytes.Equal(stdout.Bytes(), stdout4.Bytes()) {
		t.Fatal("fallback schema differed from built-in schema")
	}
}

func TestSchemaErrors(t *testing.T) {
	a, _, stderr := newTestApp(t)
	if code := run([]string{"schema", "a", "b"}, a); code != 2 {
		t.Fatalf("two args exit %d, want 2 (stderr: %s)", code, stderr)
	}
	a, _, stderr = newTestApp(t)
	if code := run([]string{"schema", filepath.Join(t.TempDir(), "missing")}, a); code != 1 {
		t.Fatalf("missing path exit %d, want 1 (stderr: %s)", code, stderr)
	}
}
