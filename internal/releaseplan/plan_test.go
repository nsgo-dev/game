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
	if plan.OutputDir != filepath.Join(dir, "build", "out") {
		t.Fatalf("OutputDir = %q", plan.OutputDir)
	}
	var buf bytes.Buffer
	WriteDryRun(&buf, plan)
	if want := ShellQuote(append([]string{"go"}, wantArgs...)) + "\n"; buf.String() != want {
		t.Fatalf("WriteDryRun = %q, want %q", buf.String(), want)
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
