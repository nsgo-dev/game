package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/project"
)

// assetsProjectHead is the identity and window every game.yaml carries, which the assets tests extend.
const assetsProjectHead = "version: 1\nname: Test Game\nid: dev.example.test\nappVersion: 0.1.0\nwindow: {width: 320, height: 240}\n"

// newAssetsProject writes a game.yaml declaring one atlas and the manifest of its output folder, with the atlas's one image, and returns the game.yaml path.
func newAssetsProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "art/ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDot(t, filepath.Join(root, "art/ui/dot.png"), color.NRGBA{R: 255, A: 255})
	yaml := assetsProjectHead + "assets:\n  atlases:\n    - name: ui\n      in: art/ui\n      out: assets\n  manifest:\n    in: assets\n    out: gen/assets_gen.go\n"
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
		{"missing file", []string{"-in", filepath.Join(t.TempDir(), "game.yaml")}, 1, "game assets: open"},
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
	if err := os.WriteFile(project, []byte(assetsProjectHead+"assets:\n  atlases:\n    - name: ui\n      in: missing\n      out: assets\n"), 0o644); err != nil {
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

func TestAssetsRefusesProjectsWithoutAssets(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"no assets section", assetsProjectHead, "has no assets section"},
		{"invalid project file", "version: 1\nassets: {}\n", "project: "},
		{"misspelt assets field", assetsProjectHead + "assets:\n  atlas: []\n", "field atlas not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "game.yaml")
			if err := os.WriteFile(path, []byte(tc.src), 0o644); err != nil {
				t.Fatal(err)
			}
			a, _, stderr := newTestApp(t)
			if code := run([]string{"assets", "-in", path}, a); code != 1 {
				t.Fatalf("exit %d, want 1 (stderr: %s)", code, stderr)
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("stderr = %q, want %q", stderr, tc.want)
			}
		})
	}
}

// fullGameYAML has an export section, which game release reads, an autoload section naming the main scene the editor opens, and an assets section, which game assets reads.
const fullGameYAML = assetsProjectHead + `autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.yaml
export:
  output: build/release
  presets:
    - platform: web
assets:
  atlases:
    - name: ui
      in: art/ui
      out: assets
  manifest:
    in: assets
    out: gen/assets_gen.go
`

// TestOneGameYAMLServesReleaseEditorAndAssets checks that a game.yaml with export, editor and assets settings is accepted by game release, the editor and game assets alike, and that saving it from the editor keeps the assets section.
func TestOneGameYAMLServesReleaseEditorAndAssets(t *testing.T) {
	dir := newGameDir(t, fullGameYAML)
	if err := os.MkdirAll(filepath.Join(dir, "art/ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDot(t, filepath.Join(dir, "art/ui/dot.png"), color.NRGBA{R: 255, A: 255})
	// game release lists the atlas's image in the credits file, so it needs a credit.
	if err := os.WriteFile(filepath.Join(dir, "art/ui/dot.png.import.yaml"), []byte("version: 1\ncredit: {author: Ana, license: CC0-1.0}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes/room.yaml"), []byte("version: 1\nname: room\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gameYAML := filepath.Join(dir, "game.yaml")

	calls := recordNSGo(t, nil)
	a, _, stderr := newTestApp(t)
	if code := run([]string{"release", dir}, a); code != 0 {
		t.Fatalf("release exit %d (stderr: %s)", code, stderr)
	}
	want := [][]string{{"-C", dir, "tool", "nsgo", "build", "-target", "web", "-output", filepath.FromSlash("build/release")}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("release ran %q, want %q", *calls, want)
	}

	ed, err := newDemoEditor()
	if err != nil {
		t.Fatal(err)
	}
	if err := open(ed, gameYAML); err != nil {
		t.Fatalf("editor: %v", err)
	}
	if ed.Project.Assets == nil || len(ed.Project.Assets.Atlases) != 1 || ed.Project.Export == nil {
		t.Fatalf("editor opened assets %+v and export %+v", ed.Project.Assets, ed.Project.Export)
	}
	if err := ed.EditProject("rename", func(f *project.File) { f.Window.Title = "Renamed" }); err != nil {
		t.Fatal(err)
	}
	if err := ed.SaveProject(); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(gameYAML)
	if err != nil {
		t.Fatal(err)
	}
	if f, err := project.Parse(saved); err != nil || f.Window.Title != "Renamed" || !reflect.DeepEqual(f.Assets, ed.Project.Assets) {
		t.Fatalf("the editor saved %s (%v)", saved, err)
	}

	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"assets", "-in", gameYAML}, a); code != 0 {
		t.Fatalf("assets exit %d (stderr: %s)", code, stderr)
	}
	if got := stdout.String(); !strings.HasSuffix(got, "2 built, 0 up to date\n") {
		t.Fatalf("assets stdout = %q", got)
	}
}

// TestAssetsRereadsAnEditedSidecar holds issue #487's acceptance: changing a setting in the import settings sidecar beside an image changes the imported asset on the next game assets run.
func TestAssetsRereadsAnEditedSidecar(t *testing.T) {
	project := newAssetsProject(t)
	root := filepath.Dir(project)
	sidecar := filepath.Join(root, "art/ui/dot.png.import.yaml")
	frames := func() string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, "assets/ui.json"))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, name := range []string{`"dot.png"`, `"dot_0.png"`, `"dot_1.png"`, `"dot_3.png"`} {
			if strings.Contains(string(data), name) {
				names = append(names, name)
			}
		}
		return strings.Join(names, " ")
	}
	runAssets := func(want string) {
		t.Helper()
		a, stdout, stderr := newTestApp(t)
		if code := run([]string{"assets", "-in", project}, a); code != 0 {
			t.Fatalf("exit %d (stderr: %s)", code, stderr)
		}
		if stdout.String() != want {
			t.Fatalf("stdout = %q, want %q", stdout, want)
		}
	}

	runAssets("built atlas assets/ui\nbuilt manifest gen/assets_gen.go\n2 built, 0 up to date\n")
	if got := frames(); got != `"dot.png"` {
		t.Fatalf("frames without a sidecar: %s", got)
	}
	if err := os.WriteFile(sidecar, []byte("version: 1\nimage:\n  slice: {width: 4, height: 4}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runAssets("built atlas assets/ui\n1 built, 1 up to date\n")
	if got := frames(); got != `"dot_0.png" "dot_1.png" "dot_3.png"` {
		t.Fatalf("frames after slicing into 4x4 cells: %s", got)
	}
	if err := os.WriteFile(sidecar, []byte("version: 1\nimage:\n  slice: {width: 4, height: 4, count: 2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runAssets("built atlas assets/ui\n1 built, 1 up to date\n")
	if got := frames(); got != `"dot_0.png" "dot_1.png"` {
		t.Fatalf("frames after editing the count to 2: %s", got)
	}
	runAssets("0 built, 2 up to date\n")
}
