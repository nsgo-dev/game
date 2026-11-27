package main

import (
	"bytes"
	_ "embed"
	"os"
	"testing"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

//go:embed nsgo.yaml
var nsgoYAML []byte

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// TestProjectFileIsValidAndCanonical checks that game.yaml is in the form game project fmt writes and that nsgo.yaml agrees with it, as game project check and game project -check sync do. It reads both from the test binary, so it runs on every nsgo test target.
func TestProjectFileIsValidAndCanonical(t *testing.T) {
	// nsgo.yaml lists only the targets every host can build without macOS or Xcode, while game.yaml also carries the macOS and iOS release presets for game release.
	synced := Project.Clone()
	if synced.Export != nil {
		var portable []project.Preset
		for _, p := range synced.Export.Presets {
			if p.Platform != "macos" && p.Platform != "ios" && p.Platform != "tvos" && p.Platform != "watchos" {
				portable = append(portable, p)
			}
		}
		synced.Export.Presets = portable
	}
	if err := synced.CheckNSGo(nsgoYAML); err != nil {
		t.Fatal(err)
	}
	if canonical, err := project.Encode(Project); err != nil || !bytes.Equal(canonical, gameYAML) {
		t.Fatalf("game.yaml is not canonical (%v); run game project fmt:\n%s", err, canonical)
	}
}

// TestAutoloadsLoad checks that the scenes and resources game.yaml autoloads exist and that the scenes are valid, as game project check does. It reads them from the project directory, so it is skipped where the test does not run in it, as on a phone, a simulator or the web under nsgo test -target.
func TestAutoloadsLoad(t *testing.T) {
	if _, err := os.Stat(project.FileName); err != nil {
		t.Skipf("the project directory is not readable here: %v", err)
	}
	if _, err := Project.LoadAutoloads(os.DirFS(".")); err != nil {
		t.Fatal(err)
	}
}

// TestScenesCheck checks every scene, reference and connection in the project against levelComponents, so a broken reference or typo in an event, handler or component field fails tests. It reads the project directory from disk, so it is skipped where the test does not run in it, as on a phone, a simulator or the web under nsgo test -target.
func TestScenesCheck(t *testing.T) {
	if _, err := os.Stat(project.FileName); err != nil {
		t.Skipf("the project directory is not readable here: %v", err)
	}
	report, err := assetcheck.CheckWith(os.DirFS("."), assetcheck.Options{Components: levelComponents()})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range report.Problems {
		t.Error(p)
	}
}

func TestRootTitlesTheWindowFromTheProject(t *testing.T) {
	app := nsgotest.Start(t, Root)
	if app.Title != Project.Title() {
		t.Fatalf("window title = %q, want %q", app.Title, Project.Title())
	}
}
