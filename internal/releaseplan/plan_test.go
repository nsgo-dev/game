package releaseplan

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/credits"
)

func TestSettingsFromProjectAndBuildPlan(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte("name: Demo\nid: dev.example.demo\nversion: 1.0.0\nupdates:\n  channel: steam\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := project.MustParse([]byte(`version: 1
name: Demo
id: dev.example.demo
appVersion: 1.0.0
window: {width: 640, height: 360}
export:
  output: build/out
  presets:
    - platform: macos
      targets: [macos_arm64]
    - platform: web
`))
	settings := SettingsFromProject(f)
	if settings.Output != "build/out" || !reflect.DeepEqual(settings.Targets, []string{"macos_arm64", "web"}) {
		t.Fatalf("SettingsFromProject = %+v", settings)
	}
	plan, err := Build(dir, Options{Channel: "store"}, settings)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	wantArgs := []string{"-C", dir, "tool", "nsgo", "build", "-target", "macos_arm64,web", "-output", filepath.FromSlash("build/out"), "-channel", "store"}
	if !reflect.DeepEqual(plan.Args, wantArgs) {
		t.Fatalf("Args = %q, want %q", plan.Args, wantArgs)
	}
	wantEnv := []string{"GOFLAGS=-tags=release"}
	if !reflect.DeepEqual(plan.Env, wantEnv) {
		t.Fatalf("Env = %q, want %q", plan.Env, wantEnv)
	}
	if plan.AppName != "Demo" || plan.AppVersion != "1.0.0" {
		t.Fatalf("AppName/AppVersion = (%q, %q), want (Demo, 1.0.0)", plan.AppName, plan.AppVersion)
	}
	if plan.OutputDir != filepath.Join(dir, "build", "out") {
		t.Fatalf("OutputDir = %q", plan.OutputDir)
	}
	var buf bytes.Buffer
	WriteDryRun(&buf, plan)
	if want := "GOFLAGS=-tags=release " + ShellQuote(append([]string{"go"}, wantArgs...)) + "\n"; buf.String() != want {
		t.Fatalf("WriteDryRun = %q, want %q", buf.String(), want)
	}
}

func TestReleaseBuildTagEnvMerging(t *testing.T) {
	cases := []struct {
		name     string
		existing string
		want     string
	}{
		{name: "empty", existing: "", want: "-tags=release"},
		{name: "other flag preserved", existing: "-mod=mod", want: "-mod=mod -tags=release"},
		{name: "existing tag merged", existing: "-tags=custom", want: "-tags=custom,release"},
		{name: "existing tags and other flags", existing: "-mod=mod -tags=foo,bar -race", want: "-mod=mod -race -tags=foo,bar,release"},
		{name: "double dash tags", existing: "--tags=custom", want: "-tags=custom,release"},
		{name: "already has release", existing: "-mod=mod -tags=foo,release", want: "-mod=mod -tags=foo,release"},
		{name: "later tags replaces earlier per Go rules", existing: "-tags=old --tags=active", want: "-tags=active,release"},
		{name: "empty tags value", existing: "-tags=", want: "-tags=release"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := GOFLAGSWithReleaseTag(tc.existing); got != tc.want {
				t.Fatalf("GOFLAGSWithReleaseTag(%q) = %q, want %q", tc.existing, got, tc.want)
			}
		})
	}

	env := WithReleaseEnv([]string{"PATH=/usr/bin", "GOFLAGS=-mod=mod -tags=custom", "HOME=/tmp"})
	want := []string{"PATH=/usr/bin", "HOME=/tmp", "GOFLAGS=-mod=mod -tags=custom,release"}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("WithReleaseEnv = %q, want %q", env, want)
	}

	// Empty non-nil base env produces only GOFLAGS=-tags=release.
	if got := WithReleaseEnv([]string{}); !reflect.DeepEqual(got, []string{"GOFLAGS=-tags=release"}) {
		t.Fatalf("WithReleaseEnv(empty) = %q", got)
	}
}

func TestBuildRefusesMissingCredits(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte("name: Demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sfx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sfx", "beep.wav"), []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := project.MustParse([]byte(`version: 1
name: Demo
id: dev.example.demo
appVersion: 1.0.0
window: {width: 640, height: 360}
assets:
  audio:
    - in: sfx/beep.wav
      out: assets/beep.wav
`))
	_, err := Build(dir, Options{}, SettingsFromProject(f))
	var missing *credits.MissingError
	if !errors.As(err, &missing) || !reflect.DeepEqual(missing.Assets, []string{"sfx/beep.wav"}) {
		t.Fatalf("Build error = %v, want MissingError for sfx/beep.wav", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sfx", "beep.wav.import.yaml"), []byte("version: 1\ncredit: {author: Ana, license: CC0-1.0}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := Build(dir, Options{}, SettingsFromProject(f))
	if err != nil {
		t.Fatalf("Build after crediting: %v", err)
	}
	var buf bytes.Buffer
	WriteDryRun(&buf, plan)
	if !strings.Contains(buf.String(), "# then writes the credits of 1 assets to "+filepath.Join(dir, "dist", credits.FileName)) {
		t.Fatalf("WriteDryRun = %q", buf.String())
	}
}

func TestBuildRefusesGeneratedAssetWithUnknownLicense(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte("name: Demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "audio", "sfx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audio", "sfx", "torch_flicker.wav"), []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audio", "sfx", "torch_flicker.wav.source.yaml"), []byte("version: 1\ngenerator: sfx-gen\nprompt: torch flicker\nlicense: unknown\ncommercialUse: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := project.MustParse([]byte(`version: 1
name: Demo
id: dev.example.demo
appVersion: 1.0.0
window: {width: 640, height: 360}
`))
	_, err := Build(dir, Options{}, SettingsFromProject(f))
	var unk *credits.UnknownLicenseError
	if !errors.As(err, &unk) {
		t.Fatalf("Build error = %v, want *credits.UnknownLicenseError", err)
	}
	if !strings.Contains(err.Error(), "audio/sfx/torch_flicker.wav") {
		t.Fatalf("Build error %q does not include blocked asset path", err.Error())
	}

	// Resolving the license in .source.yaml unblocks the release build.
	if err := os.WriteFile(filepath.Join(dir, "audio", "sfx", "torch_flicker.wav.source.yaml"), []byte("version: 1\ngenerator: sfx-gen\nprompt: torch flicker\nlicense: CC0-1.0\ncommercialUse: true\napprovedBy: kim\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(dir, Options{}, SettingsFromProject(f)); err != nil {
		t.Fatalf("Build after resolving license: %v", err)
	}
}

func TestExperimentalPlatformsAndDeduplication(t *testing.T) {
	for _, plat := range DefaultPlatforms {
		if IsExperimental(plat) {
			t.Errorf("default platform %q must not be experimental", plat)
		}
	}
	for _, target := range []string{
		"tvos", "tvos_simulator",
		"watchos", "watchos_simulator",
		"harmonyos",
		"miniprogram", "miniprogram_wechat", "miniprogram_qq", "miniprogram_alipay", "miniprogram_douyin",
	} {
		info, ok := ExperimentalInfoFor(target)
		if !ok {
			t.Errorf("ExperimentalInfoFor(%q) = false, want true", target)
			continue
		}
		if info.Missing == "" || !strings.HasPrefix(info.IssueURL, "https://github.com/nsgo-dev/nsgo/issues/") {
			t.Errorf("ExperimentalInfoFor(%q) = %+v, want missing capabilities and upstream issue URL", target, info)
		}
	}

	got := ExperimentalPlatforms([]string{"macos", "tvos", "tvos_simulator", "miniprogram_wechat", "web", "miniprogram_qq", "harmonyos", "watchos_simulator"})
	wantPlats := []string{"tvos", "miniprogram", "harmonyos", "watchos"}
	if len(got) != len(wantPlats) {
		t.Fatalf("ExperimentalPlatforms returned %d entries (%+v), want %d", len(got), got, len(wantPlats))
	}
	for i, want := range wantPlats {
		if got[i].Platform != want {
			t.Errorf("ExperimentalPlatforms[%d] = %q, want %q", i, got[i].Platform, want)
		}
		w := FormatExperimentalWarning(got[i])
		if !strings.Contains(w, want) || !strings.Contains(w, PlatformSupportURL) || !strings.Contains(w, got[i].IssueURL) {
			t.Errorf("FormatExperimentalWarning(%q) = %q, want platform, docs URL, and issue URL", want, w)
		}
	}
	if u := ExperimentalDocsURL("en"); u != PlatformSupportURL {
		t.Errorf("ExperimentalDocsURL(en) = %q, want %q", u, PlatformSupportURL)
	}
	if u := ExperimentalDocsURL("zh"); u != PlatformSupportURLZH {
		t.Errorf("ExperimentalDocsURL(zh) = %q, want %q", u, PlatformSupportURLZH)
	}
}

func TestBuildStrictRefusesExperimentalTargets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte("name: Demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := Settings{Targets: []string{"web", "tvos", "tvos_simulator"}}

	// Non-strict builds succeed and report unique experimental platforms.
	plan, err := Build(dir, Options{}, settings)
	if err != nil {
		t.Fatalf("non-strict Build: %v", err)
	}
	if len(plan.Experimental) != 1 || plan.Experimental[0].Platform != "tvos" {
		t.Fatalf("plan.Experimental = %+v, want [tvos]", plan.Experimental)
	}

	// Strict fails with *ExperimentalTargetsError wrapping ErrExperimentalTarget.
	_, err = Build(dir, Options{Strict: true}, settings)
	if !errors.Is(err, ErrExperimentalTarget) {
		t.Fatalf("strict Build err = %v, want ErrExperimentalTarget", err)
	}
	var expErr *ExperimentalTargetsError
	if !errors.As(err, &expErr) || len(expErr.Platforms) != 1 || expErr.Platforms[0].Platform != "tvos" {
		t.Fatalf("strict Build expErr = %+v, want [tvos]", expErr)
	}
	if !strings.Contains(err.Error(), PlatformSupportURL) || !strings.Contains(err.Error(), Experimental["tvos"].IssueURL) {
		t.Fatalf("strict Build error %q missing links", err.Error())
	}

	// Multiple experimental platforms are all listed in strict error.
	_, err = Build(dir, Options{Targets: "harmonyos,miniprogram_wechat,miniprogram_qq", Strict: true}, Settings{})
	if !errors.Is(err, ErrExperimentalTarget) {
		t.Fatalf("multi strict Build err = %v, want ErrExperimentalTarget", err)
	}
	if !strings.Contains(err.Error(), "harmonyos") || !strings.Contains(err.Error(), "miniprogram") || !strings.Contains(err.Error(), PlatformSupportURL) {
		t.Fatalf("multi strict Build error %q missing platforms or link", err.Error())
	}

	// Strict succeeds when only primary targets are built.
	if _, err := Build(dir, Options{Strict: true}, Settings{Targets: []string{"macos", "web"}}); err != nil {
		t.Fatalf("strict Build on primary targets: %v", err)
	}
}
func TestReadNSGoSettingsItchConfig(t *testing.T) {
	dir := t.TempDir()
	yamlText := `name: Demo
id: dev.example.demo
version: 1.0.0
build_number: 42
updates:
  channel: none
build:
  output: out/rel
itch:
  project: studio/demo-game
  channels:
    macos_arm64: osx-arm64-beta
    web: html5-demo
`
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte(yamlText), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadNSGoSettings(dir)
	if err != nil {
		t.Fatalf("ReadNSGoSettings: %v", err)
	}
	if got.Channel != "none" || got.Output != "out/rel" || got.ItchProject != "studio/demo-game" || got.BuildNumber != "42" {
		t.Fatalf("ReadNSGoSettings = %+v", got)
	}
	wantChannels := map[string]string{
		"macos_arm64": "osx-arm64-beta",
		"web":         "html5-demo",
	}
	if !reflect.DeepEqual(got.ItchChannels, wantChannels) {
		t.Fatalf("ItchChannels = %v, want %v", got.ItchChannels, wantChannels)
	}
}

func TestBuildHostFilteringAndUnsignedIOS(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte("name: Demo\nid: dev.example.demo\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	allPresets := Settings{
		Output:  "dist",
		Targets: []string{"macos_arm64", "windows_x64", "linux_x64", "linux_arm64", "ios", "android_arm64", "web"},
	}

	t.Run("linux host skips macOS and iOS presets and keeps cross-host targets", func(t *testing.T) {
		linuxHost := &Host{GOOS: "linux"}
		plan, err := Build(dir, Options{Host: linuxHost}, allPresets)
		if err != nil {
			t.Fatalf("Build on linux: %v", err)
		}
		wantTargets := []string{"windows_x64", "linux_x64", "linux_arm64", "android_arm64", "web"}
		if !reflect.DeepEqual(plan.Targets, wantTargets) {
			t.Fatalf("Targets = %q, want %q", plan.Targets, wantTargets)
		}
		wantSkipped := []SkippedTarget{
			{Target: "macos_arm64", Reason: "needs macOS"},
			{Target: "ios", Reason: "needs macOS with Xcode"},
		}
		if !reflect.DeepEqual(plan.Skipped, wantSkipped) {
			t.Fatalf("Skipped = %+v, want %+v", plan.Skipped, wantSkipped)
		}
		if len(plan.Unsigned) != 0 {
			t.Fatalf("Unsigned = %+v, want none when ios was skipped", plan.Unsigned)
		}
		if !strings.Contains(plan.CommandLine, "-target windows_x64,linux_x64,linux_arm64,android_arm64,web") {
			t.Fatalf("CommandLine = %q", plan.CommandLine)
		}
	})

	t.Run("macOS host without Xcode keeps macOS and skips iOS", func(t *testing.T) {
		macNoXcode := &Host{GOOS: "darwin", HasAppleSDK: func(string) bool { return false }}
		plan, err := Build(dir, Options{Host: macNoXcode}, allPresets)
		if err != nil {
			t.Fatalf("Build on macOS without Xcode: %v", err)
		}
		wantTargets := []string{"macos_arm64", "windows_x64", "linux_x64", "linux_arm64", "android_arm64", "web"}
		if !reflect.DeepEqual(plan.Targets, wantTargets) {
			t.Fatalf("Targets = %q, want %q", plan.Targets, wantTargets)
		}
		wantSkipped := []SkippedTarget{
			{Target: "ios", Reason: "needs Xcode (iphoneos SDK unavailable)"},
		}
		if !reflect.DeepEqual(plan.Skipped, wantSkipped) {
			t.Fatalf("Skipped = %+v, want %+v", plan.Skipped, wantSkipped)
		}
	})

	t.Run("explicit -target ios fails clearly without Xcode", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			host *Host
			want string
		}{
			{"linux", &Host{GOOS: "linux"}, "needs macOS with Xcode"},
			{"windows", &Host{GOOS: "windows"}, "needs macOS with Xcode"},
			{"darwin without xcode", &Host{GOOS: "darwin", HasAppleSDK: func(string) bool { return false }}, "needs Xcode (iphoneos SDK unavailable)"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := Build(dir, Options{Targets: "ios", Host: tc.host}, allPresets)
				if !errors.Is(err, ErrHostUnavailableTarget) {
					t.Fatalf("err = %v, want ErrHostUnavailableTarget", err)
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("err = %q, want %q", err, tc.want)
				}
			})
		}
	})

	t.Run("all presets skipped returns ErrNoBuildableTargets", func(t *testing.T) {
		_, err := Build(dir, Options{Host: &Host{GOOS: "linux"}}, Settings{Targets: []string{"ios"}})
		if !errors.Is(err, ErrNoBuildableTargets) {
			t.Fatalf("err = %v, want ErrNoBuildableTargets", err)
		}
	})

	t.Run("macOS with Xcode reports ios without sign.profile as unsigned instead of dropping", func(t *testing.T) {
		plan, err := Build(dir, Options{Host: AllTargetsHost()}, allPresets)
		if err != nil {
			t.Fatalf("Build on macOS with Xcode: %v", err)
		}
		if !reflect.DeepEqual(plan.Targets, allPresets.Targets) {
			t.Fatalf("Targets = %q, want %q", plan.Targets, allPresets.Targets)
		}
		if len(plan.Skipped) != 0 {
			t.Fatalf("Skipped = %+v, want none", plan.Skipped)
		}
		if len(plan.Unsigned) != 1 || plan.Unsigned[0].Target != "ios" {
			t.Fatalf("Unsigned = %+v, want one ios entry", plan.Unsigned)
		}
		if note := FormatUnsignedNote(plan.Unsigned[0]); !strings.Contains(note, "sign.profile") || !strings.Contains(note, "unsigned") {
			t.Fatalf("FormatUnsignedNote = %q", note)
		}
	})

	t.Run("macOS with Xcode and sign.profile reports no unsigned note", func(t *testing.T) {
		signedDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(signedDir, project.NSGoFileName), []byte("name: Demo\nid: dev.example.demo\nversion: 1.0.0\nsign:\n  profile: certs/appstore.mobileprovision\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		plan, err := Build(signedDir, Options{Host: AllTargetsHost()}, allPresets)
		if err != nil {
			t.Fatalf("Build with sign.profile: %v", err)
		}
		if len(plan.Unsigned) != 0 {
			t.Fatalf("Unsigned = %+v, want none when sign.profile is configured", plan.Unsigned)
		}
	})
}
