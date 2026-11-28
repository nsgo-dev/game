package gamecli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/internal/releaseplan"
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

// recordNSGo replaces runNSGo for the test with one that records its arguments, asserts the release tag env is present, and returns err. It also stubs verifyRelease since no real binaries are produced and stubs releaseHost to AllTargetsHost so mock tests behave identically on macOS, Linux, and Windows CI.
func recordNSGo(t *testing.T, err error) *[][]string {
	t.Helper()
	var calls [][]string
	saved := runNSGo
	savedVerify := verifyRelease
	savedHost := releaseHost
	runNSGo = func(args, env []string, stdout, stderr io.Writer) error {
		if !slices.Contains(env, "GOFLAGS=-tags=release") {
			t.Errorf("runNSGo env = %q, want GOFLAGS=-tags=release", env)
		}
		calls = append(calls, slices.Clone(args))
		return err
	}
	verifyRelease = func(*releaseplan.Plan) error { return nil }
	releaseHost = releaseplan.AllTargetsHost
	t.Cleanup(func() {
		runNSGo = saved
		verifyRelease = savedVerify
		releaseHost = savedHost
	})
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
	// Without sign.profile and an assets section, the command notes both the unsigned iOS build and the absent credits file.
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "ios is unsigned") || !strings.Contains(stderr.String(), noCreditsNote) {
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

func TestReleaseIgnoresNSGoBuildTargets(t *testing.T) {
	dir := newGameDir(t, "")
	if err := os.WriteFile(filepath.Join(dir, "nsgo.yaml"), []byte("name: Test Game\nbuild:\n  output: dist\n  targets: [macos_arm64, ios_simulator, web]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := recordNSGo(t, nil)
	a, _, stderr := newTestApp(t)
	if code := run([]string{"release", dir}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	want := [][]string{{"-C", dir, "tool", "nsgo", "build", "-target", "macos,windows,linux,web,ios,android"}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("ran %q, want %q", *calls, want)
	}
}

func TestReleaseBuildsDesktopForSteam(t *testing.T) {
	cases := []struct {
		name, nsgoYAML, gameYAML string
		args                     []string
		want                     []string
	}{
		{name: "nsgo.yaml channel", nsgoYAML: "updates:\n  channel: steam\n", want: []string{"-target", "macos,windows,linux"}},
		{name: "-channel flag", args: []string{"-channel", "steam"}, want: []string{"-target", "macos,windows,linux", "-channel", "steam"}},
		{name: "-channel overrides nsgo.yaml", nsgoYAML: "updates:\n  channel: steam\n", args: []string{"-channel", "direct"}, want: []string{"-target", "macos,windows,linux,web,ios,android", "-channel", "direct"}},
		{name: "presets win", nsgoYAML: "updates:\n  channel: steam\n", gameYAML: "version: 1\nexport:\n  presets:\n    - platform: windows\n", want: []string{"-target", "windows"}},
		{name: "-target wins", args: []string{"-channel", "steam", "-target", "linux_x64"}, want: []string{"-target", "linux_x64", "-channel", "steam"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newGameDir(t, tc.gameYAML)
			if err := os.WriteFile(filepath.Join(dir, "nsgo.yaml"), []byte("name: Test Game\n"+tc.nsgoYAML), 0o644); err != nil {
				t.Fatal(err)
			}
			calls := recordNSGo(t, nil)
			a, _, stderr := newTestApp(t)
			if code := run(append(append([]string{"release"}, tc.args...), dir), a); code != 0 {
				t.Fatalf("exit %d (stderr: %s)", code, stderr)
			}
			want := [][]string{append([]string{"-C", dir, "tool", "nsgo", "build"}, tc.want...)}
			if !reflect.DeepEqual(*calls, want) {
				t.Fatalf("ran %q, want %q", *calls, want)
			}
		})
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
		args, err := releaseArgs("/game", releaseOptions{}, settings, nsgoSettings{})
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
	want := "GOFLAGS=-tags=release go -C " + shellQuote([]string{dir}) + " tool nsgo build -target web -output 'my dist'\n"
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
		{name: "misspelt export", gameYAML: "version: 1\nexprot:\n  presets: [{platform: web}]\n", code: 1, want: `line 2: unknown field "exprot" in game.yaml`},
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
	if got := stderr.String(); !strings.Contains(got, noCreditsNote) || !strings.HasSuffix(got, "game release: nsgo build: exit status 1\n") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestReleaseHostSkippingAndUnsignedIOS(t *testing.T) {
	const templatePresets = `version: 1
export:
  output: dist
  presets:
    - platform: macos
      targets: [macos_arm64]
    - platform: windows
      targets: [windows_x64]
    - platform: linux
      targets: [linux_x64, linux_arm64]
    - platform: ios
      targets: [ios]
    - platform: android
      targets: [android_arm64]
    - platform: web
`
	t.Run("linux host skips Apple presets with messages and builds the rest", func(t *testing.T) {
		dir := newGameDir(t, templatePresets)
		calls := recordNSGo(t, nil)
		releaseHost = func() *releaseplan.Host { return &releaseplan.Host{GOOS: "linux"} }
		a, stdout, stderr := newTestApp(t)
		if code := run([]string{"release", dir}, a); code != 0 {
			t.Fatalf("exit %d (stderr: %s)", code, stderr)
		}
		want := [][]string{{"-C", dir, "tool", "nsgo", "build", "-target", "windows_x64,linux_x64,linux_arm64,android_arm64,web", "-output", "dist"}}
		if !reflect.DeepEqual(*calls, want) {
			t.Fatalf("ran %q, want %q", *calls, want)
		}
		if stdout.Len() != 0 {
			t.Fatalf("stdout = %q, want empty", stdout)
		}
		errText := stderr.String()
		for _, wantLine := range []string{
			"game release: skipped macos_arm64: needs macOS",
			"game release: skipped ios: needs macOS with Xcode",
		} {
			if !strings.Contains(errText, wantLine) {
				t.Errorf("stderr missing %q:\n%s", wantLine, errText)
			}
		}
		if strings.Contains(errText, "ios is unsigned") {
			t.Errorf("stderr must not report ios unsigned when ios was skipped:\n%s", errText)
		}
	})

	t.Run("macOS without Xcode skips ios and builds macos plus cross-host targets", func(t *testing.T) {
		dir := newGameDir(t, templatePresets)
		calls := recordNSGo(t, nil)
		releaseHost = func() *releaseplan.Host {
			return &releaseplan.Host{GOOS: "darwin", HasAppleSDK: func(string) bool { return false }}
		}
		a, _, stderr := newTestApp(t)
		if code := run([]string{"release", dir}, a); code != 0 {
			t.Fatalf("exit %d (stderr: %s)", code, stderr)
		}
		want := [][]string{{"-C", dir, "tool", "nsgo", "build", "-target", "macos_arm64,windows_x64,linux_x64,linux_arm64,android_arm64,web", "-output", "dist"}}
		if !reflect.DeepEqual(*calls, want) {
			t.Fatalf("ran %q, want %q", *calls, want)
		}
		if !strings.Contains(stderr.String(), "game release: skipped ios: needs Xcode (iphoneos SDK unavailable)") {
			t.Errorf("stderr = %q, want skipped ios message", stderr)
		}
	})

	t.Run("explicit -target ios fails clearly without Xcode", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			host *releaseplan.Host
			want string
		}{
			{"linux", &releaseplan.Host{GOOS: "linux"}, "needs macOS with Xcode"},
			{"darwin no xcode", &releaseplan.Host{GOOS: "darwin", HasAppleSDK: func(string) bool { return false }}, "needs Xcode (iphoneos SDK unavailable)"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				dir := newGameDir(t, templatePresets)
				calls := recordNSGo(t, nil)
				releaseHost = func() *releaseplan.Host { return tc.host }
				a, stdout, stderr := newTestApp(t)
				if code := run([]string{"release", "-target", "ios", dir}, a); code != 1 {
					t.Fatalf("exit %d, want 1 (stderr: %s)", code, stderr)
				}
				if len(*calls) != 0 || stdout.Len() != 0 {
					t.Fatalf("ran %q, stdout %q; want nothing", *calls, stdout)
				}
				if !strings.Contains(stderr.String(), `cannot build requested target "ios" on this host: `+tc.want) {
					t.Fatalf("stderr = %q, want clear ios error (%q)", stderr, tc.want)
				}
			})
		}
	})

	t.Run("macOS with Xcode builds unsigned ios with a note and keeps ios when sign.profile is set", func(t *testing.T) {
		dir := newGameDir(t, templatePresets)
		calls := recordNSGo(t, nil)
		a, _, stderr := newTestApp(t)
		if code := run([]string{"release", dir}, a); code != 0 {
			t.Fatalf("exit %d (stderr: %s)", code, stderr)
		}
		want := [][]string{{"-C", dir, "tool", "nsgo", "build", "-target", "macos_arm64,windows_x64,linux_x64,linux_arm64,ios,android_arm64,web", "-output", "dist"}}
		if !reflect.DeepEqual(*calls, want) {
			t.Fatalf("ran %q, want %q", *calls, want)
		}
		if !strings.Contains(stderr.String(), "game release: note: ios is unsigned") {
			t.Fatalf("stderr = %q, want unsigned ios note", stderr)
		}

		// With sign.profile configured in nsgo.yaml, no unsigned note is printed.
		if err := os.WriteFile(filepath.Join(dir, "nsgo.yaml"), []byte("name: Test Game\nid: dev.example.test\nversion: 0.1.0\nsign:\n  profile: certs/appstore.mobileprovision\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		a, _, stderr = newTestApp(t)
		if code := run([]string{"release", dir}, a); code != 0 {
			t.Fatalf("exit %d (stderr: %s)", code, stderr)
		}
		if strings.Contains(stderr.String(), "ios is unsigned") {
			t.Fatalf("stderr = %q, want no unsigned ios note when sign.profile is set", stderr)
		}
	})
}

func TestParseReleaseSettingsRefusesBadExports(t *testing.T) {
	cases := []struct{ yaml, want string }{
		{"", "empty file"},
		{"name: x\n", "version is required"},
		{"version: 3\n", "unsupported version 3"},
		{"version: 1.5\n", `version "1.5" must be the integer 1`},
		{"version: 1.0\n", `version "1.0" must be the integer 1`},
		{"version: '1'\n", `version "1" must be the integer 1`},
		{"version:\n", "must be the integer 1"},
		{"version: [1]\n", "must be the integer 1"},
		{"- version: 1\n", "line 1: the file must be a mapping of fields"},
		{"version: 1\nexprot:\n  presets: [{platform: web}]\n", `line 2: unknown field "exprot" in game.yaml`},
		{"version: 1\nexports: {}\n", `line 2: unknown field "exports" in game.yaml`},
		{"version: 1\nversion: 1\n", "already"},
		{"version: 1\nexport:\n  presets: []\n", "line 3: export presets must name at least one platform"},
		{"version: 1\n---\nversion: 1\n", "trailing YAML document"},
		{"version: [\n", "yaml"},
		{"version: 1\nexport:\n  out: dist\n", `line 3: unknown field "out" in export`},
		{"version: 1\nexport:\n  presets:\n    - platform: web\n      target: [web]\n", `line 5: unknown field "target" in export preset`},
		{"version: 1\nexport: 5\n", "export: yaml"},
		{"version: 1\nexport:\n  output: ../dist\n", "inside the project"},
		{"version: 1\nexport:\n  output: /dist\n", "inside the project"},
		{"version: 1\nexport:\n  output: .\n", "inside the project"},
		{"version: 1\nexport:\n  output: 'a\\b'\n", "inside the project"},
		{"version: 1\nexport:\n  output: C:/builds\n", "inside the project"},
		{"version: 1\nexport:\n  output: 'C:builds'\n", "inside the project"},
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
	settings, err := parseReleaseSettings([]byte("version: 1\ndisplay: &web {platform: web}\nexport:\n  presets: [*web, {platform: ios, targets: [ios_simulator]}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"web", "ios_simulator"}; !reflect.DeepEqual(settings.Targets, want) {
		t.Fatalf("targets %q, want %q", settings.Targets, want)
	}
	if _, err := parseReleaseSettings([]byte("version: 1\ndisplay: &bad {platform: web, nope: 1}\nexport:\n  presets: [*bad]\n")); err == nil || !strings.Contains(err.Error(), `unknown field "nope"`) {
		t.Fatalf("aliased unknown field: %v", err)
	}
}

func TestParseReleaseSettingsAcceptsEveryProjectSection(t *testing.T) {
	settings, err := parseReleaseSettings([]byte(assetsProjectHead + "physics:\n  gravity2D: {\"y\": 980}\neditor:\n  plugins: [health]\nexport:\n  presets: [{platform: web}]\nassets:\n  manifest: {in: assets, out: gen/assets.go}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"web"}; !reflect.DeepEqual(settings.Targets, want) {
		t.Fatalf("targets %q, want %q", settings.Targets, want)
	}
	if settings.Assets == nil || settings.Assets.Manifest == nil || settings.Assets.Manifest.Out != "gen/assets.go" {
		t.Fatalf("assets %+v, want the manifest target", settings.Assets)
	}
	// The assets section is read through package project, which validates the whole file, as game assets does.
	if _, err := parseReleaseSettings([]byte("version: 1\nname: Demo\nassets:\n  manifest: {in: assets, out: gen/assets.go}\n")); err == nil || !strings.Contains(err.Error(), "project: id") {
		t.Fatalf("an invalid project file with assets: %v", err)
	}
	if settings, err := parseReleaseSettings([]byte("version: 1\nname: Demo\nassets: null\n")); err != nil || settings.Assets != nil {
		t.Fatalf("a null assets section: %+v, %v", settings, err)
	}
	want := []string{"version", "name", "id", "appVersion", "window", "display", "input", "audio", "physics", "autoload", "export", "editor", "assets", "navigation", "packs", "origin"}
	if !reflect.DeepEqual(gameFileKeys, want) {
		t.Fatalf("gameFileKeys = %q, want %q", gameFileKeys, want)
	}
}

func TestParseReleaseSettingsValidatesExportBudgets(t *testing.T) {
	valid := "version: 1\nexport:\n  budgets:\n    qualityProfile: low\n    downloadSizeMB: 80\n    frameTimeP95Ms: 16.7\n  presets: [{platform: web}]\n"
	if _, err := parseReleaseSettings([]byte(valid)); err != nil {
		t.Fatalf("valid export.budgets rejected: %v", err)
	}
	if _, err := parseReleaseSettings([]byte("version: 1\nexport:\n  budgets: null\n")); err != nil {
		t.Fatalf("null export.budgets rejected: %v", err)
	}
	bad := []struct {
		name string
		yaml string
		want string
	}{
		{"scalar", "version: 1\nexport:\n  budgets: bad\n", "mapping"},
		{"sequence", "version: 1\nexport:\n  budgets: [1, 2]\n", "mapping"},
		{"unknown field", "version: 1\nexport:\n  budgets:\n    unknown: 1\n", `unknown field "unknown"`},
		{"wrong type", "version: 1\nexport:\n  budgets:\n    downloadSizeMB: nope\n", "export:"},
		{"negative download", "version: 1\nexport:\n  budgets:\n    downloadSizeMB: -5\n", "downloadSizeMB"},
		{"nan frame", "version: 1\nexport:\n  budgets:\n    frameTimeP95Ms: .nan\n", "frameTimeP95Ms"},
		{"inf download", "version: 1\nexport:\n  budgets:\n    downloadSizeMB: .inf\n", "downloadSizeMB"},
		{"bad quality profile", "version: 1\nexport:\n  budgets:\n    qualityProfile: \"low spec\"\n", "qualityProfile"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseReleaseSettings([]byte(tc.yaml)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseReleaseSettings(%s) = %v, want error containing %q", tc.name, err, tc.want)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote([]string{"go", "-C", "/a b/it's", "", "-target", "web,ios", "$HOME"})
	want := `go -C '/a b/it'\''s' '' -target web,ios '$HOME'`
	if got != want {
		t.Fatalf("shellQuote = %s, want %s", got, want)
	}
}

func TestReleaseWarnsOncePerExperimentalPlatform(t *testing.T) {
	t.Run("game.yaml presets deduplicate sub-targets per platform", func(t *testing.T) {
		dir := newGameDir(t, `version: 1
export:
  presets:
    - platform: web
    - platform: tvos
      targets: [tvos, tvos_simulator]
    - platform: miniprogram
      targets: [miniprogram_wechat, miniprogram_qq]
`)
		calls := recordNSGo(t, nil)
		a, stdout, stderr := newTestApp(t)
		if code := run([]string{"release", dir}, a); code != 0 {
			t.Fatalf("exit %d (stderr: %s)", code, stderr)
		}
		wantCalls := [][]string{{"-C", dir, "tool", "nsgo", "build", "-target", "web,tvos,tvos_simulator,miniprogram_wechat,miniprogram_qq"}}
		if !reflect.DeepEqual(*calls, wantCalls) {
			t.Fatalf("ran %q, want %q", *calls, wantCalls)
		}
		if stdout.Len() != 0 {
			t.Fatalf("stdout = %q, want empty", stdout)
		}
		errText := stderr.String()
		if got := strings.Count(errText, `target platform "tvos" is experimental`); got != 1 {
			t.Errorf("tvos warning count = %d, want 1\nstderr:\n%s", got, errText)
		}
		if got := strings.Count(errText, `target platform "miniprogram" is experimental`); got != 1 {
			t.Errorf("miniprogram warning count = %d, want 1\nstderr:\n%s", got, errText)
		}
		if strings.Contains(errText, `target platform "web"`) {
			t.Errorf("web must not trigger experimental warning:\n%s", errText)
		}
		for _, want := range []string{
			"no audio",
			"no GPU view",
			releaseplan.PlatformSupportURL,
			"https://github.com/nsgo-dev/nsgo/issues/1317",
			"https://github.com/nsgo-dev/nsgo/issues/1505",
		} {
			if !strings.Contains(errText, want) {
				t.Errorf("stderr missing %q:\n%s", want, errText)
			}
		}
	})

	t.Run("-target flag warns for harmonyos and watchos", func(t *testing.T) {
		dir := newGameDir(t, "")
		calls := recordNSGo(t, nil)
		a, _, stderr := newTestApp(t)
		if code := run([]string{"release", "-target", "harmonyos,watchos,watchos_simulator", dir}, a); code != 0 {
			t.Fatalf("exit %d (stderr: %s)", code, stderr)
		}
		if len(*calls) != 1 {
			t.Fatalf("calls = %q, want 1", *calls)
		}
		errText := stderr.String()
		if got := strings.Count(errText, `target platform "harmonyos" is experimental`); got != 1 {
			t.Errorf("harmonyos warning count = %d, want 1\nstderr:\n%s", got, errText)
		}
		if got := strings.Count(errText, `target platform "watchos" is experimental`); got != 1 {
			t.Errorf("watchos warning count = %d, want 1\nstderr:\n%s", got, errText)
		}
		for _, want := range []string{
			releaseplan.PlatformSupportURL,
			"https://github.com/nsgo-dev/nsgo/issues/1521",
			"https://github.com/nsgo-dev/nsgo/issues/1318",
		} {
			if !strings.Contains(errText, want) {
				t.Errorf("stderr missing %q:\n%s", want, errText)
			}
		}
	})

	t.Run("-dry-run warns on stderr and keeps stdout clean", func(t *testing.T) {
		dir := newGameDir(t, "")
		calls := recordNSGo(t, errors.New("must not run"))
		a, stdout, stderr := newTestApp(t)
		if code := run([]string{"release", "-dry-run", "-target", "tvos", dir}, a); code != 0 {
			t.Fatalf("exit %d (stderr: %s)", code, stderr)
		}
		if len(*calls) != 0 {
			t.Fatalf("-dry-run ran %q", *calls)
		}
		wantStdout := "GOFLAGS=-tags=release go -C " + shellQuote([]string{dir}) + " tool nsgo build -target tvos\n"
		if stdout.String() != wantStdout {
			t.Fatalf("stdout = %q, want %q", stdout, wantStdout)
		}
		if !strings.Contains(stderr.String(), `target platform "tvos" is experimental`) {
			t.Fatalf("stderr = %q, want tvos warning", stderr)
		}
	})
}

func TestReleaseStrictFailsOnExperimentalPlatforms(t *testing.T) {
	t.Run("preset experimental platform fails before running nsgo", func(t *testing.T) {
		dir := newGameDir(t, "version: 1\nexport:\n  presets:\n    - platform: harmonyos\n")
		calls := recordNSGo(t, nil)
		a, stdout, stderr := newTestApp(t)
		if code := run([]string{"release", "-strict", dir}, a); code != 1 {
			t.Fatalf("exit %d, want 1 (stderr: %s)", code, stderr)
		}
		if len(*calls) != 0 || stdout.Len() != 0 {
			t.Fatalf("ran %q, stdout %q; want nothing", *calls, stdout)
		}
		errText := stderr.String()
		for _, want := range []string{
			"game release: ",
			"harmonyos",
			"-strict",
			"no audio",
			"no GPU view",
			releaseplan.PlatformSupportURL,
			"https://github.com/nsgo-dev/nsgo/issues/1521",
		} {
			if !strings.Contains(errText, want) {
				t.Errorf("stderr missing %q:\n%s", want, errText)
			}
		}
	})

	t.Run("multiple experimental targets with -dry-run and -strict fail", func(t *testing.T) {
		dir := newGameDir(t, "")
		calls := recordNSGo(t, nil)
		a, stdout, stderr := newTestApp(t)
		if code := run([]string{"release", "-strict", "-dry-run", "-target", "tvos,tvos_simulator,miniprogram_wechat", dir}, a); code != 1 {
			t.Fatalf("exit %d, want 1 (stderr: %s)", code, stderr)
		}
		if len(*calls) != 0 || stdout.Len() != 0 {
			t.Fatalf("ran %q, stdout %q; want nothing", *calls, stdout)
		}
		errText := stderr.String()
		for _, want := range []string{
			"tvos",
			"miniprogram",
			releaseplan.PlatformSupportURL,
			"https://github.com/nsgo-dev/nsgo/issues/1317",
			"https://github.com/nsgo-dev/nsgo/issues/1505",
		} {
			if !strings.Contains(errText, want) {
				t.Errorf("stderr missing %q:\n%s", want, errText)
			}
		}
	})

	t.Run("-strict succeeds for primary platforms", func(t *testing.T) {
		dir := newGameDir(t, "")
		calls := recordNSGo(t, nil)
		a, _, stderr := newTestApp(t)
		if code := run([]string{"release", "-strict", dir}, a); code != 0 {
			t.Fatalf("exit %d, want 0 (stderr: %s)", code, stderr)
		}
		if len(*calls) != 1 {
			t.Fatalf("calls = %q, want 1", *calls)
		}
		if strings.Contains(stderr.String(), "experimental") {
			t.Fatalf("unexpected experimental message on primary platforms:\n%s", stderr)
		}
	})
}

func TestReleaseFailsWhenBinaryVerificationFails(t *testing.T) {
	dir := newGameDir(t, assetsProjectHead+`export:
  presets:
    - platform: linux
      targets: [linux_x64]
assets:
  audio:
    - in: sfx/beep.wav
      out: assets/beep.wav
`)
	if err := os.MkdirAll(filepath.Join(dir, "sfx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sfx", "beep.wav"), []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sfx", "beep.wav.import.yaml"), []byte("version: 1\ncredit: {author: Ana, license: CC0-1.0}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotEnv []string
	var verifiedPlan *releaseplan.Plan
	savedRun := runNSGo
	savedVerify := verifyRelease
	runNSGo = func(args, env []string, stdout, stderr io.Writer) error {
		gotEnv = slices.Clone(env)
		return nil
	}
	verifyRelease = func(p *releaseplan.Plan) error {
		verifiedPlan = p
		return releaseplan.ErrDebugSymbolLinked
	}
	t.Cleanup(func() {
		runNSGo = savedRun
		verifyRelease = savedVerify
	})

	a, _, stderr := newTestApp(t)
	if code := run([]string{"release", dir}, a); code != 1 {
		t.Fatalf("exit %d, want 1 (stderr: %s)", code, stderr)
	}
	if !slices.Contains(gotEnv, "GOFLAGS=-tags=release") {
		t.Fatalf("runNSGo env = %q, want GOFLAGS=-tags=release", gotEnv)
	}
	if verifiedPlan == nil {
		t.Fatal("verifyRelease was not called")
	}
	if !strings.Contains(stderr.String(), "game release: verify release: ") || !strings.Contains(stderr.String(), releaseplan.ErrDebugSymbolLinked.Error()) {
		t.Fatalf("stderr = %q, want verify release error", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist", "CREDITS.md")); !os.IsNotExist(err) {
		t.Fatalf("CREDITS.md should not be written when release verification fails: %v", err)
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
		if _, err := releaseArgs("/game", releaseOptions{}, settings, nsgoSettings{}); err != nil {
			t.Fatal(err)
		}
	})
}
