package gamecli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestGeneratesAndChecks(t *testing.T) {
	inDir := t.TempDir()
	outFile := filepath.Join(t.TempDir(), "manifest_gen.go")

	if err := os.WriteFile(filepath.Join(inDir, "hero.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stdout mode.
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"manifest", "-in", inDir, "-pkg", "assets"}, a); code != 0 {
		t.Fatalf("stdout mode exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "ImageHero") {
		t.Fatalf("expected ImageHero in stdout:\n%s", stdout)
	}

	// Write file mode, then -check passes.
	a, _, stderr = newTestApp(t)
	if code := run([]string{"manifest", "-in", inDir, "-out", outFile, "-pkg", "assets"}, a); code != 0 {
		t.Fatalf("write exit %d (stderr: %s)", code, stderr)
	}
	a, _, stderr = newTestApp(t)
	if code := run([]string{"manifest", "-in", inDir, "-out", outFile, "-pkg", "assets", "-check"}, a); code != 0 {
		t.Fatalf("-check on an up-to-date manifest exit %d (stderr: %s)", code, stderr)
	}

	// Adding an asset makes -check fail.
	if err := os.WriteFile(filepath.Join(inDir, "jump.wav"), []byte("wav"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _, stderr = newTestApp(t)
	if code := run([]string{"manifest", "-in", inDir, "-out", outFile, "-pkg", "assets", "-check"}, a); code != 1 {
		t.Fatalf("-check after adding jump.wav exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "rerun game manifest") {
		t.Fatalf("stderr = %q, want the rerun hint", stderr)
	}
}

func TestManifestCheckNeedsOut(t *testing.T) {
	a, _, stderr := newTestApp(t)
	if code := run([]string{"manifest", "-in", t.TempDir(), "-check"}, a); code != 2 {
		t.Fatalf("-check without -out exit %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "-out is required with -check") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestManifestResourcesScansResourcesDirAndRefusesEmbed(t *testing.T) {
	root := t.TempDir()
	resDir := filepath.Join(root, "resources")
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resDir, "hero.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resDir, "level1.tmj"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"manifest", "-resources", "-pkg", "main"}, a); code != 0 {
		t.Fatalf("-resources exit %d (stderr: %s)", code, stderr)
	}
	got := stdout.String()
	if strings.Contains(got, "//go:embed") {
		t.Fatalf("-resources must not emit //go:embed:\n%s", got)
	}
	if !strings.Contains(got, `ImageHero = core.ImageSource{URI: "asset:hero.png"}`) || !strings.Contains(got, `MapLevel1 = "asset:level1.tmj"`) {
		t.Fatalf("unexpected manifest output:\n%s", got)
	}

	for _, args := range [][]string{
		{"manifest", "-resources", "-embed"},
		{"manifest", "-resources", "-embed-dir", "assets"},
	} {
		a, _, stderr = newTestApp(t)
		if code := run(args, a); code != 2 {
			t.Fatalf("%v exit %d, want 2 (stderr: %s)", args, code, stderr)
		}
		if !strings.Contains(stderr.String(), "-resources cannot be used with -embed") {
			t.Fatalf("%v stderr = %q", args, stderr)
		}
	}
}
