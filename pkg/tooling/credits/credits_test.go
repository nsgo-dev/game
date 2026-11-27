package credits_test

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/credits"
	"github.com/nsgo-dev/game/pkg/tooling/importsettings"
)

// sampleProject declares an atlas, a texture, an audio file and the manifest of the folder they are written to, which also holds a font and a map written by hand.
const sampleProject = `version: 1
name: Sample
id: dev.example.sample
appVersion: '1'
window: {width: 320, height: 240}
assets:
  atlases:
    - name: hero
      in: art/hero
      out: assets/atlases
  textures:
    - in: art/sky.png
      out: assets/sky.ktx2
  audio:
    - in: sfx/jump.wav
      out: assets/jump.wav
  manifest:
    in: assets
    out: gen/assets_gen.go
`

func writeFile(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// newProject writes sampleProject, its inputs and the outputs game assets would write, and returns the project's root and assets section.
func newProject(t *testing.T) (string, *project.Assets) {
	t.Helper()
	root := t.TempDir()
	img := pngBytes(t)
	for _, rel := range []string{"art/hero/idle.png", "art/hero/run.png", "art/hero/.draft.png", "art/sky.png", "assets/atlases/hero.png", "assets/atlases/hero-1.png"} {
		writeFile(t, root, rel, img)
	}
	for rel, data := range map[string]string{
		"art/hero/notes.txt":       "not packed",
		"sfx/jump.wav":             "RIFF",
		"assets/atlases/hero.json": "{}",
		"assets/sky.ktx2":          "KTX",
		"assets/jump.wav":          "RIFF",
		"assets/fonts/pixel.ttf":   "font",
		"assets/levels/one.tmj":    "{}",
		"assets/doc.go":            "package assets",
		"game.yaml":                sampleProject,
	} {
		writeFile(t, root, rel, []byte(data))
	}
	f, err := project.Parse([]byte(sampleProject))
	if err != nil {
		t.Fatal(err)
	}
	return root, f.Assets
}

var wantAssets = []string{"art/hero/idle.png", "art/hero/run.png", "art/sky.png", "assets/fonts/pixel.ttf", "assets/levels/one.tmj", "sfx/jump.wav"}

func TestAssets(t *testing.T) {
	root, a := newProject(t)
	got, err := credits.Assets(root, a)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, wantAssets) {
		t.Fatalf("Assets = %v, want %v", got, wantAssets)
	}
	if got, err := credits.Assets(root, nil); got != nil || err != nil {
		t.Fatalf("Assets of no assets section = %v, %v", got, err)
	}

	// An asset the manifest lists that a target also reads is listed once.
	a.Manifest.In = "."
	got, err = credits.Assets(root, a)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range wantAssets {
		if n := strings.Count(strings.Join(got, "\n")+"\n", p+"\n"); n != 1 {
			t.Errorf("Assets lists %s %d times: %v", p, n, got)
		}
	}
	for _, p := range got {
		if a.IsOutput(p) || importsettings.IsSidecar(p) {
			t.Errorf("Assets lists %s, which a target writes or which is a sidecar", p)
		}
	}
}

func TestAssetsBeforeTheManifestFolderExists(t *testing.T) {
	root, a := newProject(t)
	if err := os.RemoveAll(filepath.Join(root, "assets")); err != nil {
		t.Fatal(err)
	}
	got, err := credits.Assets(root, a)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"art/hero/idle.png", "art/hero/run.png", "art/sky.png", "sfx/jump.wav"}; !slices.Equal(got, want) {
		t.Fatalf("Assets = %v, want %v", got, want)
	}
}

// fontModelProject declares a font and a model whose inputs live outside the manifest's folder, the model naming a buffer and an image beside it and embedding another image as a data URI.
const fontModelProject = `version: 1
name: Sample
id: dev.example.sample
appVersion: '1'
window: {width: 320, height: 240}
assets:
  fonts:
    - in: fonts/Inter.ttf
      out: assets/inter.fnt
  models:
    - in: models/ship.gltf
      out: assets/ship.glb
  manifest:
    in: assets
    out: gen/assets_gen.go
`

func TestAssetsFontsAndModels(t *testing.T) {
	root := t.TempDir()
	for rel, data := range map[string]string{
		"game.yaml":                 fontModelProject,
		"fonts/Inter.ttf":           "font",
		"models/ship.gltf":          `{"asset":{"version":"2.0"},"buffers":[{"uri":"ship.bin","byteLength":4}],"images":[{"uri":"tex/hull%20paint.png"},{"uri":"ship.bin"},{"uri":"data:image/png;base64,AA=="}]}`,
		"models/ship.bin":           "data",
		"models/tex/hull paint.png": "png",
		"assets/inter.fnt":          "info",
		"assets/inter.png":          "png",
		"assets/ship.glb":           "glTF",
	} {
		writeFile(t, root, rel, []byte(data))
	}
	f, err := project.Parse([]byte(fontModelProject))
	if err != nil {
		t.Fatal(err)
	}
	got, err := credits.Assets(root, f.Assets)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"fonts/Inter.ttf", "models/ship.bin", "models/ship.gltf", "models/tex/hull paint.png"}; !slices.Equal(got, want) {
		t.Fatalf("Assets = %v, want %v", got, want)
	}

	// An uncredited font is reported rather than left out of the credits.
	writeFile(t, root, "models/ship.gltf.import.yaml", []byte("version: 1\ncredit: {author: A, license: CC0-1.0}\n"))
	writeFile(t, root, "models/ship.bin.import.yaml", []byte("version: 1\ncredit: {author: A, license: CC0-1.0}\n"))
	writeFile(t, root, "models/tex/hull paint.png.import.yaml", []byte("version: 1\ncredit: {author: B, license: CC-BY-4.0}\n"))
	entries, err := credits.Collect(root, f.Assets)
	var missing *credits.MissingError
	if !errors.As(err, &missing) || !slices.Equal(missing.Assets, []string{"fonts/Inter.ttf"}) {
		t.Fatalf("Collect error = %v, want the font named as uncredited", err)
	}
	if len(entries) != 3 || entries[2].Asset != "models/tex/hull paint.png" || entries[2].Credit.Author != "B" {
		t.Fatalf("Collect entries = %+v, want the model, its buffer and its image credited", entries)
	}

	// A model naming a file outside the project is refused.
	writeFile(t, root, "models/ship.gltf", []byte(`{"asset":{"version":"2.0"},"images":[{"uri":"../../secret.png"}]}`))
	if _, err := credits.Assets(root, f.Assets); err == nil || !strings.Contains(err.Error(), "leaves the project") {
		t.Fatalf("Assets error = %v, want the URI leaving the project refused", err)
	}
}

func TestAssetsMissingInput(t *testing.T) {
	root, a := newProject(t)
	if err := os.Remove(filepath.Join(root, "sfx/jump.wav")); err != nil {
		t.Fatal(err)
	}
	if _, err := credits.Assets(root, a); err == nil || !strings.Contains(err.Error(), "jump.wav") {
		t.Fatalf("Assets error = %v, want the missing sound named", err)
	}
}

func TestCollect(t *testing.T) {
	root, a := newProject(t)
	writeFile(t, root, "art/hero/idle.png.import.yaml", []byte("version: 1\ncredit: {author: Ana, license: CC-BY-4.0, source: 'https://example.com/hero'}\n"))
	writeFile(t, root, "art/sky.png.import.yaml", []byte("version: 1\nimage: {filter: nearest}\n"))
	writeFile(t, root, "assets/fonts/pixel.ttf.import.yaml", []byte("version: 1\ncredit:\n  author: Bo\n  license: OFL-1.1\n  notice: Copyright 2026 Bo\n"))
	// A case-insensitive file system opens this one for run.png, but it credits run.png on none.
	writeFile(t, root, "art/hero/RUN.png.import.yaml", []byte("version: 1\ncredit: {author: Ana, license: CC0-1.0}\n"))

	entries, err := credits.Collect(root, a)
	var missing *credits.MissingError
	if !errors.As(err, &missing) {
		t.Fatalf("Collect error = %v, want a *MissingError", err)
	}
	if want := []string{"art/hero/run.png", "art/sky.png", "assets/levels/one.tmj", "sfx/jump.wav"}; !slices.Equal(missing.Assets, want) {
		t.Fatalf("missing = %v, want %v", missing.Assets, want)
	}
	for _, s := range []string{"4 assets have no credit", "\n\tart/sky.png.import.yaml", "\n\tsfx/jump.wav.import.yaml", "leftover of an earlier build"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q does not mention %q", err, s)
		}
	}
	if len(entries) != 2 || entries[0].Asset != "art/hero/idle.png" || entries[1].Credit.Notice != "Copyright 2026 Bo" {
		t.Fatalf("entries = %+v", entries)
	}

	if err := os.Remove(filepath.Join(root, "art/hero/RUN.png.import.yaml")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range missing.Assets {
		writeFile(t, root, importsettings.Path(rel), []byte("version: 1\ncredit: {author: Studio, license: LicenseRef-Studio}\n"))
	}
	entries, err = credits.Collect(root, a)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Asset)
	}
	if !slices.Equal(got, wantAssets) {
		t.Fatalf("credited %v, want every asset %v", got, wantAssets)
	}
	if err := (&credits.MissingError{Assets: []string{"a.png"}}).Error(); !strings.Contains(err, "1 asset has no credit") {
		t.Fatalf("one missing: %q", err)
	}
}

func TestCollectInvalidSidecar(t *testing.T) {
	root, a := newProject(t)
	writeFile(t, root, "sfx/jump.wav.import.yaml", []byte("version: 1\ncredit: {author: Ana, license: CC BY}\n"))
	if _, err := credits.Collect(root, a); err == nil || !strings.Contains(err.Error(), "sfx/jump.wav.import.yaml: importsettings: credit: license") {
		t.Fatalf("Collect error = %v, want the invalid sidecar named", err)
	}
}

func TestMarkdown(t *testing.T) {
	entries := []credits.Entry{
		{Asset: "sfx/jump.wav", Credit: importsettings.Credit{Author: "Kenney", License: "CC0-1.0", Source: "https://kenney.nl/assets"}},
		{Asset: "art/hero|1.png", Credit: importsettings.Credit{Author: "Ana *Souza* <ana>", License: "CC-BY-4.0", Source: "Made for the jam | 2026", Notice: "Hero by Ana Souza\n```\nCC BY 4.0"}},
		{Asset: "assets/fonts/pixel.ttf", Credit: importsettings.Credit{Author: "Bo", License: "MIT OR Apache-2.0"}},
		{Asset: "assets/music/theme.ogg", Credit: importsettings.Credit{Author: "Studio", License: "LicenseRef-Studio-EULA", Notice: "All rights reserved."}},
		{Asset: "art/sky.png", Credit: importsettings.Credit{Author: "Ana", License: "CC-BY-4.0", Source: "https://example.com/a|b"}},
	}
	want := "# Credits\n\n<!-- Generated from the credit section of each asset's import settings sidecar (*.import.yaml). DO NOT EDIT. -->\n\n" +
		"| Asset | Author | License | Source |\n| --- | --- | --- | --- |\n" +
		"| `art/hero\\|1.png` | Ana \\*Souza\\* \\<ana\\> | CC-BY-4.0 | Made for the jam \\| 2026 |\n" +
		"| `art/sky.png` | Ana | CC-BY-4.0 | <https://example.com/a%7Cb> |\n" +
		"| `assets/fonts/pixel.ttf` | Bo | MIT OR Apache-2.0 |  |\n" +
		"| `assets/music/theme.ogg` | Studio | LicenseRef-Studio-EULA |  |\n" +
		"| `sfx/jump.wav` | Kenney | CC0-1.0 | <https://kenney.nl/assets> |\n" +
		"\n## Licenses\n\n| License | Assets |\n| --- | --- |\n" +
		"| [CC-BY-4.0](https://spdx.org/licenses/CC-BY-4.0.html) | 2 |\n" +
		"| [CC0-1.0](https://spdx.org/licenses/CC0-1.0.html) | 1 |\n" +
		"| LicenseRef-Studio-EULA | 1 |\n" +
		"| MIT OR Apache-2.0 | 1 |\n" +
		"\n## Notices\n" +
		"\n### `art/hero|1.png`\n\n````text\nHero by Ana Souza\n```\nCC BY 4.0\n````\n" +
		"\n### `assets/music/theme.ogg`\n\n```text\nAll rights reserved.\n```\n"
	got := string(credits.Markdown(entries))
	if got != want {
		t.Fatalf("Markdown =\n%s\nwant\n%s", got, want)
	}
	if entries[0].Asset != "sfx/jump.wav" {
		t.Fatal("Markdown reordered its argument")
	}
	slices.Reverse(entries)
	if again := string(credits.Markdown(entries)); again != want {
		t.Fatalf("Markdown depends on the entries' order:\n%s", again)
	}
	if got := string(credits.Markdown(nil)); !strings.HasSuffix(got, "The game uses no assets that need crediting.\n") {
		t.Fatalf("Markdown(nil) =\n%s", got)
	}
}

func TestWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dist")
	entries := []credits.Entry{{Asset: "a.png", Credit: importsettings.Credit{Author: "Ana", License: "MIT"}}}
	name, err := credits.Write(dir, entries)
	if err != nil {
		t.Fatal(err)
	}
	if name != filepath.Join(dir, credits.FileName) {
		t.Fatalf("Write wrote %s", name)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, credits.Markdown(entries)) {
		t.Fatalf("Write wrote\n%s", data)
	}
}

func BenchmarkMarkdown(b *testing.B) {
	entries := make([]credits.Entry, 500)
	for i := range entries {
		entries[i] = credits.Entry{Asset: "art/sprites/frame" + strings.Repeat("x", i%7) + ".png", Credit: importsettings.Credit{Author: "Ana", License: "CC-BY-4.0", Source: "https://example.com", Notice: "Copyright Ana"}}
	}
	b.ReportAllocs()
	for b.Loop() {
		credits.Markdown(entries)
	}
}
