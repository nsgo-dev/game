package repocheck_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/internal/releaseplan"
	"github.com/nsgo-dev/game/pkg/project"
)

// exampleTargetsExempt lists the work-in-progress Star Catcher tutorial chapters, which intentionally omit a build section until ch09-ship; the apple CI job still vets every chapter for iOS.
var exampleTargetsExempt = map[string]bool{
	"tutorial/ch01-empty-project": true,
	"tutorial/ch02-game-loop":     true,
	"tutorial/ch03-input":         true,
	"tutorial/ch04-sprites":       true,
	"tutorial/ch05-rules":         true,
	"tutorial/ch06-screens":       true,
	"tutorial/ch07-sound":         true,
	"tutorial/ch08-save":          true,
}

// checkExampleTargets walks examplesDir and reports every non-exempt example whose nsgo.yaml build.targets or game.yaml export.presets misses a platform in releaseplan.DefaultPlatforms. It returns the number of non-exempt example directories checked alongside the list of problems.
func checkExampleTargets(examplesDir string, exempt map[string]bool) (int, []string, error) {
	dirs := map[string]bool{}
	err := filepath.WalkDir(examplesDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != examplesDir && (name == "dist" || name == "testdata" || name == "tools" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == project.NSGoFileName || d.Name() == project.FileName {
			dirs[filepath.Dir(p)] = true
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}

	sorted := make([]string, 0, len(dirs))
	for dir := range dirs {
		sorted = append(sorted, dir)
	}
	slices.Sort(sorted)

	var problems []string
	checked := 0
	for _, dir := range sorted {
		rel, err := filepath.Rel(examplesDir, dir)
		if err != nil {
			return 0, nil, err
		}
		rel = filepath.ToSlash(rel)
		if exempt[rel] {
			continue
		}
		checked++

		nsgoPath := filepath.Join(dir, project.NSGoFileName)
		nsgoData, err := os.ReadFile(nsgoPath)
		if err == nil {
			var nsgo struct {
				Build struct {
					Targets []string `yaml:"targets"`
				} `yaml:"build"`
			}
			if err := yaml.Unmarshal(nsgoData, &nsgo); err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", filepath.ToSlash(nsgoPath), err))
			} else {
				for _, platform := range releaseplan.DefaultPlatforms {
					if !slices.Contains(nsgo.Build.Targets, platform) {
						problems = append(problems, fmt.Sprintf("%s: build.targets (%v) misses default platform %q", filepath.ToSlash(nsgoPath), nsgo.Build.Targets, platform))
					}
				}
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return 0, nil, err
		}

		gamePath := filepath.Join(dir, project.FileName)
		gameData, err := os.ReadFile(gamePath)
		if err == nil {
			f, err := project.Parse(gameData)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", filepath.ToSlash(gamePath), err))
			} else if f.Export != nil && len(f.Export.Presets) > 0 {
				for _, platform := range releaseplan.DefaultPlatforms {
					if _, ok := f.Preset(platform); !ok {
						problems = append(problems, fmt.Sprintf("%s: export.presets misses default platform %q", filepath.ToSlash(gamePath), platform))
					}
				}
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return 0, nil, err
		}
	}
	return checked, problems, nil
}

// TestExamplesDeclareAllDefaultPlatforms checks that every example under examples/ (outside tutorial chapters 1 to 8) declares all releaseplan.DefaultPlatforms in its nsgo.yaml build.targets and, when it defines export presets, in its game.yaml export.presets.
func TestExamplesDeclareAllDefaultPlatforms(t *testing.T) {
	for rel := range exampleTargetsExempt {
		if _, err := os.Stat(filepath.Join("examples", filepath.FromSlash(rel), project.NSGoFileName)); err != nil {
			t.Errorf("exampleTargetsExempt lists %s, which has no %s: %v", rel, project.NSGoFileName, err)
		}
	}

	checked, problems, err := checkExampleTargets("examples", exampleTargetsExempt)
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("found no example directories under examples/; the scan is broken")
	}
	for _, p := range problems {
		t.Error(p)
	}
}

func TestCheckExampleTargetsDetectsDrift(t *testing.T) {
	write := func(root, rel, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	const validNSGo = "name: Sample\nid: dev.nsgo.sample\nversion: 0.1.0\nbuild:\n  output: dist\n  targets: [macos, windows, linux, android, web, ios]\n"
	const validGame = "version: 1\nname: Sample\nid: dev.nsgo.sample\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\nexport:\n  output: dist\n  presets:\n    - platform: macos\n    - platform: windows\n    - platform: linux\n    - platform: android\n    - platform: web\n    - platform: ios\n"

	t.Run("all platforms present passes", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, "good/nsgo.yaml", validNSGo)
		write(dir, "good/game.yaml", validGame)
		write(dir, "tutorial/ch01-empty-project/nsgo.yaml", "name: Ch1\nid: dev.nsgo.ch1\nversion: 0.1.0\n")

		checked, problems, err := checkExampleTargets(dir, exampleTargetsExempt)
		if err != nil {
			t.Fatal(err)
		}
		if checked != 1 {
			t.Fatalf("checked = %d, want 1", checked)
		}
		if len(problems) != 0 {
			t.Fatalf("unexpected problems: %v", problems)
		}
	})

	t.Run("nsgo.yaml missing ios fails", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, "missing/nsgo.yaml", "name: Sample\nid: dev.nsgo.sample\nversion: 0.1.0\nbuild:\n  output: dist\n  targets: [macos, windows, linux, android, web]\n")

		_, problems, err := checkExampleTargets(dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) != 1 || !strings.Contains(problems[0], `misses default platform "ios"`) {
			t.Fatalf("problems = %v, want one missing ios error", problems)
		}
	})

	t.Run("nsgo.yaml missing build section fails", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, "nobuild/nsgo.yaml", "name: Sample\nid: dev.nsgo.sample\nversion: 0.1.0\n")

		_, problems, err := checkExampleTargets(dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) != len(releaseplan.DefaultPlatforms) {
			t.Fatalf("got %d problems (%v), want %d", len(problems), problems, len(releaseplan.DefaultPlatforms))
		}
	})

	t.Run("game.yaml export presets missing ios fails", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, "presetdrift/nsgo.yaml", validNSGo)
		write(dir, "presetdrift/game.yaml", "version: 1\nname: Sample\nid: dev.nsgo.sample\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\nexport:\n  output: dist\n  presets:\n    - platform: macos\n    - platform: windows\n    - platform: linux\n    - platform: android\n    - platform: web\n")

		_, problems, err := checkExampleTargets(dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) != 1 || !strings.Contains(problems[0], `export.presets misses default platform "ios"`) {
			t.Fatalf("problems = %v, want one export.presets missing ios error", problems)
		}
	})

	t.Run("game.yaml without export presets passes", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, "nopresets/nsgo.yaml", validNSGo)
		write(dir, "nopresets/game.yaml", "version: 1\nname: Sample\nid: dev.nsgo.sample\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n")

		_, problems, err := checkExampleTargets(dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) != 0 {
			t.Fatalf("unexpected problems: %v", problems)
		}
	})
}
