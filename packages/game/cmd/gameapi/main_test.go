package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/apiref"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func writeModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":     "module example.com/m\n\ngo 1.27\n",
		"m.go":       "// Package m is a fixture.\npackage m\n\n// Hello greets [World].\nfunc Hello() {}\n\n// World is greeted.\nconst World = \"world\"\n",
		"cmd/x/x.go": "package main\n\nfunc main() {}\n",
	}
	for name, src := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestStdoutAndFile(t *testing.T) {
	root := writeModule(t)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-root", root}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, stderr.String())
	}
	ref, err := apiref.Unmarshal(stdout.Bytes())
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(ref.Packages) != 1 || ref.Packages[0].ImportPath != "example.com/m" {
		t.Fatalf("packages = %+v, want example.com/m alone", ref.Packages)
	}
	if ref.Packages[0].Funcs[0].DocHTML != "" {
		t.Fatal("DocHTML set without -html")
	}

	out := filepath.Join(t.TempDir(), "api.json")
	stdout.Reset()
	if err := run([]string{"-root", root, "-html", "-out", out}, &stdout, &stderr); err != nil {
		t.Fatalf("run -out: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q with -out, want nothing", stdout.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	ref, err = apiref.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal file: %v", err)
	}
	if got, want := ref.Packages[0].Funcs[0].DocHTML, "<p>Hello greets <a href=\"#World\">World</a>.\n"; got != want {
		t.Fatalf("DocHTML = %q, want %q", got, want)
	}
}

func TestRefusesBadInput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-root", t.TempDir()}, &stdout, &stderr); err == nil {
		t.Fatal("run on a directory without go.mod succeeded")
	}
	if err := run([]string{"-root", writeModule(t), "extra"}, &stdout, &stderr); err == nil {
		t.Fatal("run with a positional argument succeeded")
	}
}
