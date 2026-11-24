package assetbuild_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/assetbuild"
	"github.com/nsgo-dev/game/packages/game/tooling/audioconv"
	"github.com/nsgo-dev/game/packages/game/tooling/ktx2"
)

// sampleProject declares two atlases, a texture, an audio file and the manifest of the folder they are written to.
const sampleProject = `version: 1
name: Sample
id: dev.example.sample
appVersion: '1'
window: {width: 320, height: 240}
assets:
  atlases:
    - name: ui
      in: art/ui
      out: assets/atlases
    - name: hero
      in: art/hero
      out: assets/atlases
  textures:
    - in: art/albedo.png
      out: assets/albedo.ktx2
      format: bc1
      mipmaps: true
  audio:
    - in: sfx/jump.wav
      out: assets/jump.wav
      rate: 8000
      peakDB: -1
  manifest:
    in: assets
    out: gen/assets_gen.go
`

// epoch is the time every file is stamped with between runs, so a test sees which files a run wrote.
var epoch = time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

// newProject writes sampleProject and its inputs into a new directory and returns it.
func newProject(t testing.TB) string {
	t.Helper()
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "art/ui/button.png"), 12, 6, color.NRGBA{R: 200, A: 255})
	writePNG(t, filepath.Join(root, "art/ui/icons/star.png"), 8, 8, color.NRGBA{G: 200, A: 255})
	writePNG(t, filepath.Join(root, "art/hero/idle.png"), 16, 16, color.NRGBA{B: 200, A: 255})
	writePNG(t, filepath.Join(root, "art/hero/run.png"), 16, 16, color.NRGBA{R: 90, B: 90, A: 255})
	writePNG(t, filepath.Join(root, "art/albedo.png"), 16, 16, color.NRGBA{R: 30, G: 60, B: 90, A: 255})
	writeWAV(t, filepath.Join(root, "sfx/jump.wav"))
	writeFile(t, filepath.Join(root, "game.yaml"), []byte(sampleProject))
	return root
}

func writeFile(t testing.TB, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePNG(t testing.TB, name string, w, h int, c color.NRGBA) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	writeFile(t, name, buf.Bytes())
}

func writeWAV(t testing.TB, name string) {
	t.Helper()
	samples := make([]float64, 1600)
	for i := range samples {
		samples[i] = float64(i%40-20) / 40
	}
	var buf bytes.Buffer
	if err := audioconv.EncodeWAV(&buf, &audioconv.Buffer{SampleRate: 16000, Channels: 1, Samples: samples}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, name, buf.Bytes())
}

// build loads root's game.yaml and builds it.
func build(t testing.TB, root string, b assetbuild.Builder) *assetbuild.Report {
	t.Helper()
	r, err := tryBuild(root, b)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func tryBuild(root string, b assetbuild.Builder) (*assetbuild.Report, error) {
	a, err := loadAssets(root)
	if err != nil {
		return nil, err
	}
	b.Root = root
	return b.Build(context.Background(), a)
}

// loadAssets reads the assets section of root's game.yaml through package project, as game assets does.
func loadAssets(root string) (*project.Assets, error) {
	data, err := os.ReadFile(filepath.Join(root, project.FileName))
	if err != nil {
		return nil, err
	}
	f, err := project.Parse(data)
	if err != nil {
		return nil, err
	}
	return f.Assets, nil
}

// statuses maps each target's ID to its status.
func statuses(r *assetbuild.Report) map[string]assetbuild.Status {
	m := map[string]assetbuild.Status{}
	for _, t := range r.Targets {
		m[t.ID] = t.Status
	}
	return m
}

func wantStatuses(t *testing.T, r *assetbuild.Report, want map[string]assetbuild.Status) {
	t.Helper()
	if got := statuses(r); !maps.Equal(got, want) {
		t.Fatalf("statuses = %v, want %v", got, want)
	}
}

func all(s assetbuild.Status) map[string]assetbuild.Status {
	return map[string]assetbuild.Status{
		"atlas assets/atlases/ui":    s,
		"atlas assets/atlases/hero":  s,
		"texture assets/albedo.ktx2": s,
		"audio assets/jump.wav":      s,
		"manifest gen/assets_gen.go": s,
	}
}

// stamp sets every file under root to epoch and returns the files' paths.
func stamp(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files = append(files, filepath.ToSlash(strings.TrimPrefix(p, root+string(filepath.Separator))))
		return os.Chtimes(p, epoch, epoch)
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// touched returns the files under root modified since stamp, and fails if the set of files changed.
func touched(t *testing.T, root string, before []string) []string {
	t.Helper()
	var files, changed []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, root+string(filepath.Separator)))
		files = append(files, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.ModTime().Equal(epoch) {
			changed = append(changed, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(files, before) {
		t.Fatalf("files changed:\n%v\nwant:\n%v", files, before)
	}
	return changed
}

func TestFirstRunBuildsEverything(t *testing.T) {
	root := newProject(t)
	r := build(t, root, assetbuild.Builder{})
	wantStatuses(t, r, all(assetbuild.Built))
	if r.Targets[len(r.Targets)-1].Kind != assetbuild.KindManifest {
		t.Fatalf("the manifest did not run last: %+v", r.Targets)
	}
	for _, name := range []string{
		"assets/atlases/ui.png", "assets/atlases/ui.json", "assets/atlases/hero.png", "assets/atlases/hero.json",
		"assets/albedo.ktx2", "assets/jump.wav", "gen/assets_gen.go", project.DefaultAssetsCache,
	} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	src, err := os.ReadFile(filepath.Join(root, "gen/assets_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, uri := range []string{`"asset:atlases/ui.json"`, `"asset:albedo.ktx2"`, `"asset:jump.wav"`} {
		if !bytes.Contains(src, []byte(uri)) {
			t.Errorf("manifest does not list %s:\n%s", uri, src)
		}
	}
	if got := r.Targets[0].Outputs; !slices.Equal(got, []string{"assets/atlases/ui.json", "assets/atlases/ui.png"}) {
		t.Errorf("ui outputs = %v", got)
	}
}

func TestSecondRunDoesNothing(t *testing.T) {
	root := newProject(t)
	build(t, root, assetbuild.Builder{})
	before := stamp(t, root)
	r := build(t, root, assetbuild.Builder{})
	wantStatuses(t, r, all(assetbuild.UpToDate))
	if got := touched(t, root, before); len(got) != 0 {
		t.Fatalf("an unchanged run wrote %v", got)
	}
}

func TestChangingOneImageRepacksOnlyItsAtlas(t *testing.T) {
	root := newProject(t)
	build(t, root, assetbuild.Builder{})
	writePNG(t, filepath.Join(root, "art/hero/run.png"), 16, 16, color.NRGBA{R: 255, A: 255})
	before := stamp(t, root)
	r := build(t, root, assetbuild.Builder{})
	want := all(assetbuild.UpToDate)
	want["atlas assets/atlases/hero"] = assetbuild.Built
	wantStatuses(t, r, want)
	got := touched(t, root, before)
	slices.Sort(got)
	if wantFiles := []string{project.DefaultAssetsCache, "assets/atlases/hero.json", "assets/atlases/hero.png"}; !slices.Equal(got, wantFiles) {
		t.Fatalf("wrote %v, want %v", got, wantFiles)
	}
}

func TestRebuildsWhat(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, root string)
		built  []string
	}{
		{"an added image", func(t *testing.T, root string) {
			writePNG(t, filepath.Join(root, "art/ui/new.png"), 4, 4, color.NRGBA{A: 255})
		}, []string{"atlas assets/atlases/ui"}},
		{"a removed image", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "art/ui/icons/star.png")); err != nil {
				t.Fatal(err)
			}
		}, []string{"atlas assets/atlases/ui"}},
		{"a changed setting", func(t *testing.T, root string) {
			editProject(t, root, "      out: assets/atlases\n    - name: hero", "      out: assets/atlases\n      padding: 4\n    - name: hero")
		}, []string{"atlas assets/atlases/ui"}},
		{"a changed texture", func(t *testing.T, root string) {
			writePNG(t, filepath.Join(root, "art/albedo.png"), 16, 16, color.NRGBA{R: 1, A: 255})
		}, []string{"texture assets/albedo.ktx2"}},
		{"a changed sound", func(t *testing.T, root string) {
			editProject(t, root, "rate: 8000", "rate: 11025")
		}, []string{"audio assets/jump.wav"}},
		{"an edited output", func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "assets/albedo.ktx2"), []byte("not a texture"))
		}, []string{"texture assets/albedo.ktx2"}},
		{"a deleted output", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "assets/atlases/hero.json")); err != nil {
				t.Fatal(err)
			}
		}, []string{"atlas assets/atlases/hero"}},
		{"a deleted manifest", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "gen/assets_gen.go")); err != nil {
				t.Fatal(err)
			}
		}, []string{"manifest gen/assets_gen.go"}},
		{"a new asset in the manifest's folder", func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "assets/readme.txt"), []byte("hi"))
		}, []string{"manifest gen/assets_gen.go"}},
		{"a corrupt cache", func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, project.DefaultAssetsCache), []byte("version: 1\nbogus: true\n"))
		}, []string{"atlas assets/atlases/ui", "atlas assets/atlases/hero", "texture assets/albedo.ktx2", "audio assets/jump.wav"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newProject(t)
			build(t, root, assetbuild.Builder{})
			tc.change(t, root)
			want := all(assetbuild.UpToDate)
			for _, id := range tc.built {
				want[id] = assetbuild.Built
			}
			wantStatuses(t, build(t, root, assetbuild.Builder{}), want)
			wantStatuses(t, build(t, root, assetbuild.Builder{}), all(assetbuild.UpToDate))
		})
	}
}

func editProject(t *testing.T, root, old, repl string) {
	t.Helper()
	name := filepath.Join(root, "game.yaml")
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(old)) {
		t.Fatalf("game.yaml has no %q", old)
	}
	writeFile(t, name, bytes.Replace(data, []byte(old), []byte(repl), 1))
}

func TestForceRebuildsEverything(t *testing.T) {
	root := newProject(t)
	build(t, root, assetbuild.Builder{})
	wantStatuses(t, build(t, root, assetbuild.Builder{Force: true}), all(assetbuild.Built))
	wantStatuses(t, build(t, root, assetbuild.Builder{}), all(assetbuild.UpToDate))
}

func TestNewToolVersionRebuilds(t *testing.T) {
	root := newProject(t)
	build(t, root, assetbuild.Builder{Version: "v0.1.0"})
	want := all(assetbuild.Built)
	want["manifest gen/assets_gen.go"] = assetbuild.UpToDate
	wantStatuses(t, build(t, root, assetbuild.Builder{Version: "v0.2.0"}), want)
	wantStatuses(t, build(t, root, assetbuild.Builder{Version: "v0.2.0"}), all(assetbuild.UpToDate))
}

func TestCheckWritesNothing(t *testing.T) {
	root := newProject(t)
	before := stamp(t, root)
	r := build(t, root, assetbuild.Builder{Check: true})
	wantStatuses(t, r, all(assetbuild.Stale))
	if got := touched(t, root, before); len(got) != 0 {
		t.Fatalf("a check wrote %v", got)
	}

	build(t, root, assetbuild.Builder{})
	writePNG(t, filepath.Join(root, "art/ui/button.png"), 12, 6, color.NRGBA{G: 1, A: 255})
	before = stamp(t, root)
	r = build(t, root, assetbuild.Builder{Check: true})
	want := all(assetbuild.UpToDate)
	want["atlas assets/atlases/ui"] = assetbuild.Stale
	wantStatuses(t, r, want)
	if got := r.Targets[0].Outputs; !slices.Equal(got, []string{"assets/atlases/ui.json", "assets/atlases/ui.png"}) {
		t.Errorf("stale outputs = %v, want the previous build's", got)
	}
	if got := touched(t, root, before); len(got) != 0 {
		t.Fatalf("a check wrote %v", got)
	}
	if n := r.Count(assetbuild.Stale); n != 1 {
		t.Fatalf("Count(Stale) = %d, want 1", n)
	}
}

func TestCheckWithoutCacheComparesOutputs(t *testing.T) {
	root := newProject(t)
	build(t, root, assetbuild.Builder{})
	// A fresh checkout has the committed outputs but not the ignored cache.
	if err := os.RemoveAll(filepath.Join(root, ".game")); err != nil {
		t.Fatal(err)
	}
	before := stamp(t, root)
	r := build(t, root, assetbuild.Builder{Check: true})
	wantStatuses(t, r, all(assetbuild.UpToDate))
	if got := r.Targets[0].Outputs; !slices.Equal(got, []string{"assets/atlases/ui.json", "assets/atlases/ui.png"}) {
		t.Errorf("outputs = %v, want the files the atlas writes", got)
	}
	if got := touched(t, root, before); len(got) != 0 {
		t.Fatalf("a check wrote %v", got)
	}

	writePNG(t, filepath.Join(root, "art/ui/button.png"), 12, 6, color.NRGBA{G: 1, A: 255})
	writeFile(t, filepath.Join(root, "assets/jump.wav"), []byte("edited"))
	before = stamp(t, root)
	want := all(assetbuild.UpToDate)
	want["atlas assets/atlases/ui"] = assetbuild.Stale
	want["audio assets/jump.wav"] = assetbuild.Stale
	wantStatuses(t, build(t, root, assetbuild.Builder{Check: true}), want)
	if got := touched(t, root, before); len(got) != 0 {
		t.Fatalf("a check wrote %v", got)
	}
}

func TestPagesInADotFolderOfTheirImages(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "art/ui/button.png"), 12, 6, color.NRGBA{R: 200, A: 255})
	writeFile(t, filepath.Join(dir, "game.yaml"), []byte(sampleProject[:strings.Index(sampleProject, "assets:")]+"assets:\n  atlases:\n    - name: ui\n      in: art/ui\n      out: art/ui/.pages\n"))
	build(t, dir, assetbuild.Builder{})
	r := build(t, dir, assetbuild.Builder{})
	wantStatuses(t, r, map[string]assetbuild.Status{"atlas art/ui/.pages/ui": assetbuild.UpToDate})
}

func TestForceAndCheckConflict(t *testing.T) {
	root := newProject(t)
	if _, err := tryBuild(root, assetbuild.Builder{Force: true, Check: true}); err == nil {
		t.Fatal("Build accepted Force with Check")
	}
}

func TestRemovesPagesAnAtlasNoLongerWrites(t *testing.T) {
	root := newProject(t)
	editProject(t, root, "      out: assets/atlases\n    - name: hero", "      out: assets/atlases\n      maxWidth: 12\n      maxHeight: 8\n    - name: hero")
	r := build(t, root, assetbuild.Builder{})
	if got := r.Targets[0].Outputs; !slices.Equal(got, []string{"assets/atlases/ui-0.json", "assets/atlases/ui-0.png", "assets/atlases/ui-1.json", "assets/atlases/ui-1.png"}) {
		t.Fatalf("two-page outputs = %v", got)
	}
	editProject(t, root, "      maxWidth: 12\n      maxHeight: 8\n", "")
	r = build(t, root, assetbuild.Builder{})
	if got := r.Targets[0].Outputs; !slices.Equal(got, []string{"assets/atlases/ui.json", "assets/atlases/ui.png"}) {
		t.Fatalf("one-page outputs = %v", got)
	}
	for _, name := range []string{"ui-0.json", "ui-0.png", "ui-1.json", "ui-1.png"} {
		if _, err := os.Stat(filepath.Join(root, "assets/atlases", name)); !os.IsNotExist(err) {
			t.Errorf("stale page %s is still there (%v)", name, err)
		}
	}
	src, err := os.ReadFile(filepath.Join(root, "gen/assets_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(src, []byte("ui-0")) {
		t.Errorf("manifest still lists the removed pages:\n%s", src)
	}
}

func TestFailureKeepsWhatWasBuilt(t *testing.T) {
	root := newProject(t)
	hero := filepath.Join(root, "art/hero")
	if err := os.Rename(hero, hero+"-moved"); err != nil {
		t.Fatal(err)
	}
	r, err := tryBuild(root, assetbuild.Builder{})
	if err == nil || !strings.Contains(err.Error(), "atlas assets/atlases/hero") {
		t.Fatalf("error = %v, want one naming the hero atlas", err)
	}
	wantStatuses(t, r, map[string]assetbuild.Status{"atlas assets/atlases/ui": assetbuild.Built})
	if err := os.Rename(hero+"-moved", hero); err != nil {
		t.Fatal(err)
	}
	want := all(assetbuild.Built)
	want["atlas assets/atlases/ui"] = assetbuild.UpToDate
	wantStatuses(t, build(t, root, assetbuild.Builder{}), want)

	// A failure after targets that were current keeps their entries.
	writeFile(t, filepath.Join(root, "art/albedo.png"), []byte("not an image"))
	if _, err := tryBuild(root, assetbuild.Builder{}); err == nil || !strings.Contains(err.Error(), "texture assets/albedo.ktx2") {
		t.Fatalf("error = %v, want one naming the texture", err)
	}
	// Restoring the image makes the texture current again: its output was never replaced.
	writePNG(t, filepath.Join(root, "art/albedo.png"), 16, 16, color.NRGBA{R: 30, G: 60, B: 90, A: 255})
	wantStatuses(t, build(t, root, assetbuild.Builder{}), all(assetbuild.UpToDate))
}

func TestCacheIsDeterministicText(t *testing.T) {
	a, b := newProject(t), newProject(t)
	build(t, a, assetbuild.Builder{})
	build(t, b, assetbuild.Builder{})
	ca, err := os.ReadFile(filepath.Join(a, project.DefaultAssetsCache))
	if err != nil {
		t.Fatal(err)
	}
	cb, err := os.ReadFile(filepath.Join(b, project.DefaultAssetsCache))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ca, cb) {
		t.Fatalf("two builds of the same project wrote different caches:\n%s\n%s", ca, cb)
	}
	if !bytes.HasPrefix(ca, []byte("version: 1\ntargets:\n  - target: atlas assets/atlases/hero\n")) {
		t.Fatalf("cache is not sorted YAML:\n%s", ca)
	}
	if bytes.Contains(ca, []byte(filepath.ToSlash(a))) {
		t.Fatalf("cache holds an absolute path:\n%s", ca)
	}
}

func TestDroppedTargetLeavesTheCache(t *testing.T) {
	dir := newProject(t)
	build(t, dir, assetbuild.Builder{})
	editProject(t, dir, "    - name: hero\n      in: art/hero\n      out: assets/atlases\n", "")
	build(t, dir, assetbuild.Builder{})
	c, err := os.ReadFile(filepath.Join(dir, project.DefaultAssetsCache))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(c, []byte("hero")) {
		t.Fatalf("the cache still records the dropped atlas:\n%s", c)
	}
	wantGone(t, dir, "assets/atlases/hero.png", "assets/atlases/hero.json")
	wantManifestWithout(t, dir, "hero")
}

// wantGone fails unless none of the slash-separated paths exists under root.
func wantGone(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); !os.IsNotExist(err) {
			t.Errorf("%s is still there (%v)", p, err)
		}
	}
}

// wantManifestWithout fails if the sample project's generated manifest mentions s.
func wantManifestWithout(t *testing.T, root, s string) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(root, "gen/assets_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(src, []byte(s)) {
		t.Errorf("manifest still mentions %q:\n%s", s, src)
	}
}

func TestRenamedAtlasRemovesItsOldPages(t *testing.T) {
	dir := newProject(t)
	build(t, dir, assetbuild.Builder{})
	editProject(t, dir, "    - name: hero\n", "    - name: player\n")
	r := build(t, dir, assetbuild.Builder{})
	if got := statuses(r)["atlas assets/atlases/player"]; got != assetbuild.Built {
		t.Fatalf("renamed atlas status = %q, want built", got)
	}
	wantGone(t, dir, "assets/atlases/hero.png", "assets/atlases/hero.json")
	wantManifestWithout(t, dir, "hero")
	if _, err := os.Stat(filepath.Join(dir, "assets/atlases/player.png")); err != nil {
		t.Fatal(err)
	}
	before := stamp(t, dir)
	wantStatuses(t, build(t, dir, assetbuild.Builder{}), map[string]assetbuild.Status{
		"atlas assets/atlases/ui":     assetbuild.UpToDate,
		"atlas assets/atlases/player": assetbuild.UpToDate,
		"texture assets/albedo.ktx2":  assetbuild.UpToDate,
		"audio assets/jump.wav":       assetbuild.UpToDate,
		"manifest gen/assets_gen.go":  assetbuild.UpToDate,
	})
	if got := touched(t, dir, before); len(got) != 0 {
		t.Fatalf("a second run wrote %v", got)
	}
}

func TestDroppedTargetIsRemovedAfterAFailedRun(t *testing.T) {
	dir := newProject(t)
	build(t, dir, assetbuild.Builder{})
	editProject(t, dir, "    - name: hero\n      in: art/hero\n      out: assets/atlases\n", "")
	writeFile(t, filepath.Join(dir, "art/albedo.png"), []byte("not an image"))
	if _, err := tryBuild(dir, assetbuild.Builder{}); err == nil {
		t.Fatal("Build accepted a broken texture")
	}
	writePNG(t, filepath.Join(dir, "art/albedo.png"), 8, 8, color.NRGBA{R: 1, A: 255})
	build(t, dir, assetbuild.Builder{})
	wantGone(t, dir, "assets/atlases/hero.png", "assets/atlases/hero.json")
	wantManifestWithout(t, dir, "hero")
}

func TestCheckFindsLeftoverPages(t *testing.T) {
	dir := newProject(t)
	build(t, dir, assetbuild.Builder{})
	// A build that needed two pages left ui-1 behind, committed without the cache.
	writePNG(t, filepath.Join(dir, "assets/atlases/ui-1.png"), 4, 4, color.NRGBA{R: 1, A: 255})
	writeFile(t, filepath.Join(dir, "assets/atlases/ui-1.json"), []byte("{}"))
	want := all(assetbuild.UpToDate)
	want["atlas assets/atlases/ui"] = assetbuild.Stale
	// The manifest's scan now finds the leftover pages too.
	want["manifest gen/assets_gen.go"] = assetbuild.Stale
	wantStatuses(t, build(t, dir, assetbuild.Builder{Check: true}), want)
	if err := os.RemoveAll(filepath.Join(dir, ".game")); err != nil {
		t.Fatal(err)
	}
	before := stamp(t, dir)
	wantStatuses(t, build(t, dir, assetbuild.Builder{Check: true}), want)
	if got := touched(t, dir, before); len(got) != 0 {
		t.Fatalf("a check wrote %v", got)
	}

	build(t, dir, assetbuild.Builder{})
	wantGone(t, dir, "assets/atlases/ui-1.png", "assets/atlases/ui-1.json")
	wantManifestWithout(t, dir, "ui-1")
	wantStatuses(t, build(t, dir, assetbuild.Builder{Check: true}), all(assetbuild.UpToDate))
}

// fakeEncoder stands in for ffmpeg: its version names what it writes, so two versions write different bytes for the same input.
type fakeEncoder struct {
	version string
	// missing makes LookPath find no encoder.
	missing bool
	runs    int
}

func (f *fakeEncoder) LookPath(file string) (string, error) {
	if f.missing || file != "ffmpeg" {
		return "", fs.ErrNotExist
	}
	return "/fake/ffmpeg", nil
}

func (f *fakeEncoder) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if slices.Equal(args, []string{"-version"}) {
		return []byte("ffmpeg version " + f.version + "\n"), nil
	}
	f.runs++
	return nil, os.WriteFile(args[len(args)-1], []byte("aac by "+f.version), 0o644)
}

func TestExternallyEncodedAudio(t *testing.T) {
	dir := t.TempDir()
	writeWAV(t, filepath.Join(dir, "sfx/jump.wav"))
	writeFile(t, filepath.Join(dir, "game.yaml"), []byte(sampleProject[:strings.Index(sampleProject, "assets:")]+"assets:\n  audio:\n    - in: sfx/jump.wav\n      out: assets/jump.m4a\n      format: aac\n"))
	const id = "audio assets/jump.m4a"
	with := func(f *fakeEncoder, check bool) assetbuild.Status {
		t.Helper()
		r := build(t, dir, assetbuild.Builder{Audio: &audioconv.Encoder{Runner: f, GOOS: "linux"}, Check: check})
		return statuses(r)[id]
	}

	v1 := &fakeEncoder{version: "1"}
	if got := with(v1, false); got != assetbuild.Built {
		t.Fatalf("first build = %q, want built", got)
	}
	if got := with(v1, false); got != assetbuild.UpToDate || v1.runs != 1 {
		t.Fatalf("second build = %q after %d encodes, want up to date after 1", got, v1.runs)
	}
	// Another encoder version writes other bytes, so the key covers it.
	if got := with(&fakeEncoder{version: "2"}, false); got != assetbuild.Built {
		t.Fatalf("build with a new encoder = %q, want built", got)
	}

	// A fresh checkout on a machine whose encoder writes other bytes, or that has none, passes a check while the output exists.
	if err := os.RemoveAll(filepath.Join(dir, ".game")); err != nil {
		t.Fatal(err)
	}
	before := stamp(t, dir)
	for _, f := range []*fakeEncoder{{version: "3"}, {missing: true}} {
		if got := with(f, true); got != assetbuild.UpToDate {
			t.Fatalf("check with encoder %+v = %q, want up to date", f, got)
		}
		if f.runs != 0 {
			t.Fatalf("check with encoder %+v encoded %d times", f, f.runs)
		}
	}
	if got := touched(t, dir, before); len(got) != 0 {
		t.Fatalf("a check wrote %v", got)
	}
	if err := os.Remove(filepath.Join(dir, "assets/jump.m4a")); err != nil {
		t.Fatal(err)
	}
	if got := with(&fakeEncoder{version: "3"}, true); got != assetbuild.Stale {
		t.Fatalf("check without the output = %q, want stale", got)
	}
}

func TestCustomCachePath(t *testing.T) {
	dir := newProject(t)
	editProject(t, dir, "\nassets:\n", "\nassets:\n  cache: build/cache.yaml\n")
	build(t, dir, assetbuild.Builder{})
	if _, err := os.Stat(filepath.Join(dir, "build/cache.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, project.DefaultAssetsCache)); !os.IsNotExist(err) {
		t.Fatalf("default cache written too (%v)", err)
	}
	wantStatuses(t, build(t, dir, assetbuild.Builder{}), all(assetbuild.UpToDate))
}

func TestCanceledContext(t *testing.T) {
	dir := newProject(t)
	p, err := loadAssets(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := &assetbuild.Builder{Root: dir}
	if _, err := b.Build(ctx, p); err != context.Canceled {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func BenchmarkUpToDate(b *testing.B) {
	dir := newProject(b)
	build(b, dir, assetbuild.Builder{})
	p, err := loadAssets(dir)
	if err != nil {
		b.Fatal(err)
	}
	bl := &assetbuild.Builder{Root: dir}
	for b.Loop() {
		if _, err := bl.Build(context.Background(), p); err != nil {
			b.Fatal(err)
		}
	}
}

func TestNoAssetsSectionBuildsNothing(t *testing.T) {
	dir := t.TempDir()
	b := &assetbuild.Builder{Root: dir}
	r, err := b.Build(context.Background(), nil)
	if err != nil || len(r.Targets) != 0 {
		t.Fatalf("Build(nil) = %+v, %v", r, err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("Build(nil) wrote %v (%v)", entries, err)
	}
}

func TestBuildRefusesInvalidAssets(t *testing.T) {
	b := &assetbuild.Builder{Root: t.TempDir()}
	a := &project.Assets{Textures: []project.TextureTarget{{In: "../a.png", Out: "a.ktx2"}}}
	if _, err := b.Build(context.Background(), a); err == nil || !strings.Contains(err.Error(), "inside the project") {
		t.Fatalf("Build error = %v, want the path refused", err)
	}
}

// TestFormatsMatchTheEncoders checks that every format game.yaml may name is one the encoders accept, under the same name.
func TestFormatsMatchTheEncoders(t *testing.T) {
	for _, f := range project.TextureFormats {
		if got, err := ktx2.ParseFormat(f); err != nil || string(got) != f {
			t.Errorf("ktx2.ParseFormat(%q) = %q, %v", f, got, err)
		}
	}
	for _, f := range project.AudioFormats {
		if _, err := audioconv.ParseFormat(f); err != nil {
			t.Errorf("audioconv.ParseFormat(%q): %v", f, err)
		}
	}
}
