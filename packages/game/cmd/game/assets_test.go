package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newAssetsProject writes a game.yaml declaring one atlas and the manifest of its output folder, with the atlas's one image, and returns the game.yaml path.
func newAssetsProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "art/ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDot(t, filepath.Join(root, "art/ui/dot.png"), color.NRGBA{R: 255, A: 255})
	yaml := "version: 1\nassets:\n  atlases:\n    - name: ui\n      in: art/ui\n      out: assets\n  manifest:\n    in: assets\n    out: gen/assets_gen.go\n"
	path := filepath.Join(root, "game.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeDot(t *testing.T, name string, c color.NRGBA) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	img.SetNRGBA(2, 2, c)
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAssetsBuildsThenDoesNothing(t *testing.T) {
	project := newAssetsProject(t)

	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"assets", "-in", project}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if want := "built atlas assets/ui\nbuilt manifest gen/assets_gen.go\n2 built, 0 up to date\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}

	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"assets", "-in", project}, a); code != 0 {
		t.Fatalf("second run exit %d (stderr: %s)", code, stderr)
	}
	if want := "0 built, 2 up to date\n"; stdout.String() != want {
		t.Fatalf("second run stdout = %q, want %q", stdout, want)
	}

	a, stdout, _ = newTestApp(t)
	if code := run([]string{"assets", "-in", project, "-check"}, a); code != 0 {
		t.Fatalf("check of a current project exit %d", code)
	}
	if want := "2 up to date\n"; stdout.String() != want {
		t.Fatalf("check stdout = %q, want %q", stdout, want)
	}

	a, stdout, _ = newTestApp(t)
	if code := run([]string{"assets", "-in", project, "-force"}, a); code != 0 {
		t.Fatalf("force exit %d", code)
	}
	if !strings.HasSuffix(stdout.String(), "2 built, 0 up to date\n") {
		t.Fatalf("force stdout = %q", stdout)
	}
}

func TestAssetsCheckFailsWhenStale(t *testing.T) {
	project := newAssetsProject(t)
	a, _, stderr := newTestApp(t)
	if code := run([]string{"assets", "-in", project}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	writeDot(t, filepath.Join(filepath.Dir(project), "art/ui/dot.png"), color.NRGBA{G: 255, A: 255})

	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"assets", "-in", project, "-check"}, a); code != 1 {
		t.Fatalf("check of a stale project exit %d, want 1", code)
	}
	if stdout.String() != "stale atlas assets/ui\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	if want := "game assets: 1 of 2 targets are out of date; rerun game assets\n"; stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
}

func TestAssetsCheckPassesWithoutCache(t *testing.T) {
	project := newAssetsProject(t)
	a, _, stderr := newTestApp(t)
	if code := run([]string{"assets", "-in", project}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if err := os.RemoveAll(filepath.Join(filepath.Dir(project), ".game")); err != nil {
		t.Fatal(err)
	}
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"assets", "-in", project, "-check"}, a); code != 0 {
		t.Fatalf("check without the cache exit %d (stderr: %s)", code, stderr)
	}
	if want := "2 up to date\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestAssetsDefaultsToGameYAMLHere(t *testing.T) {
	project := newAssetsProject(t)
	t.Chdir(filepath.Dir(project))
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"assets"}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if !strings.HasSuffix(stdout.String(), "2 built, 0 up to date\n") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestAssetsErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"force with check", []string{"-force", "-check"}, 2, "game assets: -force and -check are mutually exclusive"},
		{"stray argument", []string{"extra"}, 2, "game assets: unexpected arguments"},
		{"empty -in", []string{"-in", ""}, 2, "game assets: -in is required"},
		{"missing file", []string{"-in", filepath.Join(t.TempDir(), "game.yaml")}, 1, "game assets: assetbuild: open"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, stderr := newTestApp(t)
			if code := run(append([]string{"assets"}, tc.args...), a); code != tc.code {
				t.Fatalf("exit %d, want %d (stderr: %s)", code, tc.code, stderr)
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("stderr = %q, want %q", stderr, tc.want)
			}
		})
	}

	project := filepath.Join(t.TempDir(), "game.yaml")
	if err := os.WriteFile(project, []byte("version: 1\nassets:\n  atlases:\n    - name: ui\n      in: missing\n      out: assets\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _, stderr := newTestApp(t)
	if code := run([]string{"assets", "-in", project}, a); code != 1 {
		t.Fatalf("missing input exit %d, want 1", code)
	}
	if !strings.HasPrefix(stderr.String(), "game assets: assetbuild: atlas assets/ui: ") {
		t.Fatalf("stderr = %q, want the target named", stderr)
	}
}
