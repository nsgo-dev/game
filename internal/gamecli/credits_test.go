package gamecli

import (
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/credits"
)

// creditedGameYAML declares an atlas of art/ui, an audio target and the manifest of the assets folder, which also holds a font written by hand, and the release's output folder.
const creditedGameYAML = assetsProjectHead + `export:
  output: build/release
  presets:
    - platform: web
assets:
  atlases:
    - name: ui
      in: art/ui
      out: assets
  audio:
    - in: sfx/jump.wav
      out: assets/jump.wav
  manifest:
    in: assets
    out: gen/assets_gen.go
`

// newCreditedGame writes creditedGameYAML and its assets, each credited unless named in uncredited, and returns the project directory.
func newCreditedGame(t *testing.T, uncredited ...string) string {
	t.Helper()
	dir := newGameDir(t, creditedGameYAML)
	for _, d := range []string{"art/ui", "sfx", "assets/fonts"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeDot(t, filepath.Join(dir, "art/ui/dot.png"), color.NRGBA{R: 255, A: 255})
	// The atlas page in the manifest's folder is built from dot.png, so it is not credited on its own.
	writeDot(t, filepath.Join(dir, "assets/ui.png"), color.NRGBA{R: 255, A: 255})
	for name, data := range map[string]string{"sfx/jump.wav": "RIFF", "assets/fonts/pixel.ttf": "font"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sidecars := map[string]string{
		"art/ui/dot.png":         "version: 1\nimage: {slice: {width: 4, height: 4}}\ncredit: {author: Ana, license: CC-BY-4.0, source: 'https://example.com/ui'}\n",
		"sfx/jump.wav":           "version: 1\ncredit: {author: Kenney, license: CC0-1.0}\n",
		"assets/fonts/pixel.ttf": "version: 1\ncredit:\n  author: Bo\n  license: OFL-1.1\n  notice: Copyright 2026 Bo\n",
	}
	for asset, data := range sidecars {
		if slices.Contains(uncredited, asset) {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, asset+".import.yaml"), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// wantCredits is the credits file of newCreditedGame's assets.
const wantCredits = "# Credits\n\n<!-- Generated from the credit section of each asset's import settings sidecar (*.import.yaml). DO NOT EDIT. -->\n\n" +
	"| Asset | Author | License | Source |\n| --- | --- | --- | --- |\n" +
	"| `art/ui/dot.png` | Ana | CC-BY-4.0 | <https://example.com/ui> |\n" +
	"| `assets/fonts/pixel.ttf` | Bo | OFL-1.1 |  |\n" +
	"| `sfx/jump.wav` | Kenney | CC0-1.0 |  |\n" +
	"\n## Licenses\n\n| License | Assets |\n| --- | --- |\n" +
	"| [CC-BY-4.0](https://spdx.org/licenses/CC-BY-4.0.html) | 1 |\n" +
	"| [CC0-1.0](https://spdx.org/licenses/CC0-1.0.html) | 1 |\n" +
	"| [OFL-1.1](https://spdx.org/licenses/OFL-1.1.html) | 1 |\n" +
	"\n## Notices\n\n### `assets/fonts/pixel.ttf`\n\n```text\nCopyright 2026 Bo\n```\n"

// TestReleaseWritesTheCredits holds issue #513's acceptance: a release build includes a credits file listing every asset's license, beside the packages nsgo build writes.
func TestReleaseWritesTheCredits(t *testing.T) {
	dir := newCreditedGame(t)
	calls := recordNSGo(t, nil)
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"release", dir}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	want := [][]string{{"-C", dir, "tool", "nsgo", "build", "-target", "web", "-output", filepath.FromSlash("build/release")}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("ran %q, want %q", *calls, want)
	}
	name := filepath.Join(dir, "build", "release", credits.FileName)
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != wantCredits {
		t.Fatalf("credits =\n%s\nwant\n%s", data, wantCredits)
	}
	if got, want := stdout.String(), "wrote the credits of 3 assets to "+name+"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestReleaseCreditsFollowTheOutput(t *testing.T) {
	for _, tc := range []struct {
		name, nsgoYAML string
		args           []string
		want           func(dir, wd string) string
	}{
		{"flag", "", []string{"-output", "out"}, func(dir, wd string) string { return filepath.Join(wd, "out") }},
		{"nsgo.yaml", "build:\n  output: packages\n", nil, func(dir, wd string) string { return filepath.Join(dir, "packages") }},
		{"nsgo default", "", nil, func(dir, wd string) string { return filepath.Join(dir, "dist") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newCreditedGame(t)
			gameYAML := strings.Replace(creditedGameYAML, "  output: build/release\n", "", 1)
			if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(gameYAML), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "nsgo.yaml"), []byte("name: Test Game\nid: dev.example.test\nversion: 0.1.0\n"+tc.nsgoYAML), 0o644); err != nil {
				t.Fatal(err)
			}
			wd := t.TempDir()
			t.Chdir(wd)
			recordNSGo(t, nil)
			a, _, stderr := newTestApp(t)
			if code := run(append(append([]string{"release"}, tc.args...), dir), a); code != 0 {
				t.Fatalf("exit %d (stderr: %s)", code, stderr)
			}
			if _, err := os.Stat(filepath.Join(tc.want(dir, wd), credits.FileName)); err != nil {
				t.Fatalf("no credits where the packages go: %v", err)
			}
		})
	}
}

func TestReleaseRefusesUncreditedAssets(t *testing.T) {
	dir := newCreditedGame(t, "sfx/jump.wav", "assets/fonts/pixel.ttf")
	calls := recordNSGo(t, nil)
	a, _, stderr := newTestApp(t)
	if code := run([]string{"release", dir}, a); code != 1 {
		t.Fatalf("exit %d, want 1 (stderr: %s)", code, stderr)
	}
	if len(*calls) != 0 {
		t.Fatalf("built %q although assets have no credit", *calls)
	}
	for _, s := range []string{"2 assets have no credit", "\tassets/fonts/pixel.ttf.import.yaml", "\tsfx/jump.wav.import.yaml"} {
		if !strings.Contains(stderr.String(), s) {
			t.Errorf("stderr %q does not mention %q", stderr, s)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "build", "release", credits.FileName)); err == nil {
		t.Fatal("a refused release wrote credits")
	}
}

func TestReleaseWritesNoCreditsWhenTheBuildFails(t *testing.T) {
	dir := newCreditedGame(t)
	recordNSGo(t, os.ErrPermission)
	a, _, _ := newTestApp(t)
	if code := run([]string{"release", dir}, a); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "build", "release", credits.FileName)); err == nil {
		t.Fatal("a failed build wrote credits")
	}
}

func TestReleaseDryRunNamesTheCredits(t *testing.T) {
	dir := newCreditedGame(t)
	calls := recordNSGo(t, nil)
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"release", "-dry-run", dir}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if len(*calls) != 0 {
		t.Fatalf("-dry-run ran %q", *calls)
	}
	want := "# then writes the credits of 3 assets to " + filepath.Join(dir, "build", "release", credits.FileName) + "\n"
	if !strings.HasSuffix(stdout.String(), want) {
		t.Fatalf("stdout = %q, want it to end with %q", stdout, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "build", "release", credits.FileName)); err == nil {
		t.Fatal("-dry-run wrote credits")
	}
}

func TestCreditsPrintsAndWrites(t *testing.T) {
	dir := newCreditedGame(t)
	gameYAML := filepath.Join(dir, "game.yaml")
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"credits", "-in", gameYAML}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if stdout.String() != wantCredits {
		t.Fatalf("stdout =\n%s\nwant\n%s", stdout, wantCredits)
	}

	out := filepath.Join(t.TempDir(), "docs", "CREDITS.md")
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"credits", "-in", gameYAML, "-out", out}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if data, err := os.ReadFile(out); err != nil || string(data) != wantCredits {
		t.Fatalf("wrote %q (%v)", data, err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q with -out", stdout)
	}
}

func TestCreditsErrors(t *testing.T) {
	uncredited := newCreditedGame(t, "sfx/jump.wav")
	noAssets := newGameDir(t, assetsProjectHead)
	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"uncredited", []string{"-in", filepath.Join(uncredited, "game.yaml")}, 1, "game credits: credits: 1 asset has no credit"},
		{"no assets section", []string{"-in", filepath.Join(noAssets, "game.yaml")}, 1, "has no assets section"},
		{"missing file", []string{"-in", filepath.Join(noAssets, "nope.yaml")}, 1, "nope.yaml"},
		{"empty -in", []string{"-in", ""}, 2, "-in is required"},
		{"argument", []string{"-in", filepath.Join(noAssets, "game.yaml"), "extra"}, 2, "unexpected arguments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, stdout, stderr := newTestApp(t)
			if code := run(append([]string{"credits"}, tc.args...), a); code != tc.code {
				t.Fatalf("exit %d, want %d (stderr: %s)", code, tc.code, stderr)
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("stderr = %q, want it to mention %q", stderr, tc.want)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q", stdout)
			}
		})
	}
}
