package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// newGameDir returns a project directory with an nsgo.yaml and, when gameYAML is not empty, a game.yaml holding it.
func newGameDir(t *testing.T, gameYAML string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nsgo.yaml"), []byte("name: Test Game\nid: dev.example.test\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if gameYAML != "" {
		if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(gameYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// recordNSGo replaces runNSGo for the test with one that records its arguments and returns err.
func recordNSGo(t *testing.T, err error) *[][]string {
	t.Helper()
	var calls [][]string
	saved := runNSGo
	runNSGo = func(args []string, stdout, stderr io.Writer) error {
		calls = append(calls, slices.Clone(args))
		return err
	}
	t.Cleanup(func() { runNSGo = saved })
	return &calls
}

func TestReleaseBuildsEveryPlatformByDefault(t *testing.T) {
	dir := newGameDir(t, "")
	calls := recordNSGo(t, nil)
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"release", dir}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	want := [][]string{{"-C", dir, "tool", "nsgo", "build", "-target", "macos,windows,linux,web,ios,android"}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("ran %q, want %q", *calls, want)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("wrote stdout %q, stderr %q", stdout, stderr)
	}
}

func TestReleaseReadsGameYAMLExport(t *testing.T) {
	dir := newGameDir(t, `version: 1
name: Cave Run
window: {width: 960, height: 540}
export:
  output: build/release
  presets:
    - platform: macos
    - platform: windows
      targets: [windows_x64, windows_arm64]
    - platform: web
    - platform: ios
    - platform: android
      targets: [android_arm64]
    - platform: linux
`)
	calls := recordNSGo(t, nil)
	a, _, stderr := newTestApp(t)
	if code := run([]string{"release", "-channel", "store", "-jobs", "3", dir}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	want := [][]string{{"-C", dir, "tool", "nsgo", "build",
		"-target", "macos,windows_x64,windows_arm64,web,ios,android_arm64,linux",
		"-output", filepath.FromSlash("build/release"),
		"-channel", "store",
		"-jobs", "3",
	}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("ran %q, want %q", *calls, want)
	}
}

func TestReleaseFlagsOverrideGameYAML(t *testing.T) {
	dir := newGameDir(t, "version: 1\nexport:\n  output: dist\n  presets:\n    - platform: web\n")
	calls := recordNSGo(t, nil)
	out := filepath.Join(t.TempDir(), "out")
	notes := filepath.Join(t.TempDir(), "notes.md")
	a, _, stderr := newTestApp(t)
	if code := run([]string{"release", "-target", "macos_arm64,android", "-output", out, "-release-notes", notes, dir}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	want := [][]string{{"-C", dir, "tool", "nsgo", "build", "-target", "macos_arm64,android", "-output", out, "-release-notes", notes}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("ran %q, want %q", *calls, want)
	}
}

func TestReleaseWithoutPresetsKeepsTheDefaultPlatforms(t *testing.T) {
	for _, gameYAML := range []string{
		"version: 1\nname: x\n",
		"version: 1\nexport:\n",
		"version: 1\nexport:\n  output: out\n",
	} {
		settings, err := parseReleaseSettings([]byte(gameYAML))
		if err != nil {
			t.Fatalf("%q: %v", gameYAML, err)
		}
		if settings.Targets != nil {
			t.Errorf("%q: targets %q, want none", gameYAML, settings.Targets)
		}
		args, err := releaseArgs("/game", releaseOptions{}, settings)
		if err != nil {
			t.Fatal(err)
		}
		if args[6] != "macos,windows,linux,web,ios,android" {
			t.Errorf("%q: -target %q", gameYAML, args[6])
		}
	}
}

func TestReleaseDryRunPrintsTheCommand(t *testing.T) {
	dir := newGameDir(t, "version: 1\nexport:\n  output: my dist\n  presets:\n    - platform: web\n")
	calls := recordNSGo(t, errors.New("ran"))
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"release", "-dry-run", dir}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if len(*calls) != 0 {
		t.Fatalf("-dry-run ran %q", *calls)
	}
	want := "go -C " + shellQuote([]string{dir}) + " tool nsgo build -target web -output 'my dist'\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestReleaseRunsInTheCurrentDirectory(t *testing.T) {
	dir := newGameDir(t, "")
	t.Chdir(dir)
	calls := recordNSGo(t, nil)
	a, _, stderr := newTestApp(t)
	if code := run([]string{"release"}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0][1] != wd {
		t.Fatalf("ran %q, want go -C %s", *calls, wd)
	}
}

func TestReleaseErrors(t *testing.T) {
	cases := []struct {
		name     string
		gameYAML string
		noNSGo   bool
		args     []string
		code     int
		want     string
	}{
		{name: "stray argument", args: []string{"extra"}, code: 2, want: "takes one directory"},
		{name: "negative jobs", args: []string{"-jobs", "-1"}, code: 2, want: "-jobs -1 must not be negative"},
		{name: "empty target", args: []string{"-target", "web,"}, code: 2, want: "names an empty target"},
		{name: "no nsgo.yaml", noNSGo: true, code: 1, want: "has no nsgo.yaml"},
		{name: "bad game.yaml", gameYAML: "version: 2\n", code: 1, want: "unsupported version 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newGameDir(t, tc.gameYAML)
			if tc.noNSGo {
				dir = t.TempDir()
			}
			calls := recordNSGo(t, nil)
			a, stdout, stderr := newTestApp(t)
			args := append([]string{"release"}, tc.args...)
			if tc.name == "stray argument" {
				args = []string{"release", dir, "extra"}
			} else {
				args = append(args, dir)
			}
			if code := run(args, a); code != tc.code {
				t.Fatalf("exit %d, want %d (stderr: %s)", code, tc.code, stderr)
			}
			if !strings.HasPrefix(stderr.String(), "game release: ") || !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("stderr = %q, want %q", stderr, tc.want)
			}
			if tc.code == 2 && !strings.Contains(stderr.String(), "usage: game release [flags] [dir]") {
				t.Errorf("stderr = %q, want the usage", stderr)
			}
			if len(*calls) != 0 || stdout.Len() != 0 {
				t.Errorf("ran %q, wrote %q", *calls, stdout)
			}
		})
	}
}

func TestReleaseReportsAFailedBuild(t *testing.T) {
	dir := newGameDir(t, "")
	recordNSGo(t, errors.New("exit status 1"))
	a, _, stderr := newTestApp(t)
	if code := run([]string{"release", dir}, a); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if got := stderr.String(); got != "game release: nsgo build: exit status 1\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestParseReleaseSettingsRefusesBadExports(t *testing.T) {
	cases := []struct{ yaml, want string }{
		{"", "empty file"},
		{"name: x\n", "version is required"},
		{"version: 3\n", "unsupported version 3"},
		{"version: 1\n---\nversion: 1\n", "trailing YAML document"},
		{"version: [\n", "yaml"},
		{"version: 1\nexport:\n  out: dist\n", `line 3: unknown field "out" in export`},
		{"version: 1\nexport:\n  presets:\n    - platform: web\n      target: [web]\n", `line 5: unknown field "target" in export preset`},
		{"version: 1\nexport: 5\n", "export: yaml"},
		{"version: 1\nexport:\n  output: ../dist\n", "inside the project"},
		{"version: 1\nexport:\n  output: /dist\n", "inside the project"},
		{"version: 1\nexport:\n  output: .\n", "inside the project"},
		{"version: 1\nexport:\n  output: 'a\\b'\n", "inside the project"},
		{"version: 1\nexport:\n  presets:\n    - platform: dos\n", `export platform "dos" must be one of`},
		{"version: 1\nexport:\n  presets:\n    - platform: web\n    - platform: web\n", `duplicate export preset for platform "web"`},
		{"version: 1\nexport:\n  presets:\n    - platform: macos\n      targets: [windows_x64]\n", `target "windows_x64" must be one of macos_arm64, macos_x64, macos_universal`},
		{"version: 1\nexport:\n  presets:\n    - platform: linux\n      targets: [linux_x64, linux_x64]\n", `lists target "linux_x64" twice`},
		{"version: 1\nexport:\n  presets:\n    - platform: ios\n      targets: [ios_device]\n", `target "ios_device" must be one of ios, ios_simulator`},
	}
	for _, tc := range cases {
		_, err := parseReleaseSettings([]byte(tc.yaml))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: error %v, want %q", tc.yaml, err, tc.want)
		}
	}
}

func TestParseReleaseSettingsFollowsAliases(t *testing.T) {
	settings, err := parseReleaseSettings([]byte("version: 1\nweb: &web {platform: web}\nexport:\n  presets: [*web, {platform: ios, targets: [ios_simulator]}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"web", "ios_simulator"}; !reflect.DeepEqual(settings.Targets, want) {
		t.Fatalf("targets %q, want %q", settings.Targets, want)
	}
	if _, err := parseReleaseSettings([]byte("version: 1\nbad: &bad {platform: web, nope: 1}\nexport:\n  presets: [*bad]\n")); err == nil || !strings.Contains(err.Error(), `unknown field "nope"`) {
		t.Fatalf("aliased unknown field: %v", err)
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote([]string{"go", "-C", "/a b/it's", "", "-target", "web,ios", "$HOME"})
	want := `go -C '/a b/it'\''s' '' -target web,ios '$HOME'`
	if got != want {
		t.Fatalf("shellQuote = %s, want %s", got, want)
	}
}

func FuzzParseReleaseSettings(f *testing.F) {
	f.Add([]byte("version: 1\nexport:\n  output: dist\n  presets:\n    - platform: macos\n    - platform: windows\n      targets: [windows_x64]\n"))
	f.Add([]byte("version: 1\nexport: {presets: [{platform: web}]}\n"))
	f.Add([]byte("version: 1\nexport:\n  nope: 1\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		settings, err := parseReleaseSettings(data)
		if err != nil {
			return
		}
		again, err := parseReleaseSettings(data)
		if err != nil || !reflect.DeepEqual(settings, again) {
			t.Fatalf("second parse differs: %+v, %v", again, err)
		}
		for _, target := range settings.Targets {
			if !slices.Contains(exportPlatforms, target) && !slices.Contains(exportTargets, target) {
				t.Fatalf("target %q is neither a platform nor a target", target)
			}
		}
		if _, err := releaseArgs("/game", releaseOptions{}, settings); err != nil {
			t.Fatal(err)
		}
	})
}
