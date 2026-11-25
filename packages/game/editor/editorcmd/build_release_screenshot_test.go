package editorcmd_test

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/internal/releaseplan"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const buildReleaseScreenshotGameYAML = `version: 1
name: dungeon-run
id: dev.dungeonrun.game
appVersion: 0.4.0
window:
  width: 640
  height: 360
export:
  output: dist
  budgets:
    qualityProfile: low
    downloadSizeMB: 80
    frameTimeP95Ms: 16.7
  presets:
    - platform: macos
      targets: [macos_arm64, macos_x64]
    - platform: windows
      targets: [windows_x64]
    - platform: web
    - platform: android
      targets: [android_arm64]
    - platform: ios
    - platform: linux
      targets: [linux_x64]
`

func mbToBytes(mb float64) int64 { return int64(mb * 1024 * 1024) }

// TestBuildReleaseScreenMacOSScreenshots renders the Build and Release screen on the real native macOS host
// and captures failed+agent-fix, finished-result+QR, and iOS readiness screenshots beside Build.dc.html.
func TestBuildReleaseScreenMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(buildReleaseScreenshotGameYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte("name: dungeon-run\nid: dev.dungeonrun.game\nversion: 0.4.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Provide icon + touch controls so iOS shows exact 2 readiness items (provisioning profile + privacy manifest).
	if err := os.MkdirAll(filepath.Join(dir, "art"), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.SetRGBA(x, y, color.RGBA{R: 59, G: 123, B: 255, A: 255})
		}
	}
	f, err := os.Create(filepath.Join(dir, "art", "icon.png"))
	if err != nil {
		t.Fatal(err)
	}
	_ = png.Encode(f, img)
	_ = f.Close()
	if err := os.MkdirAll(filepath.Join(dir, "input"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "input", "touch.input.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.GitRunner = func(d string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "rev-parse --short HEAD":
			return "5b0e8a4\n", nil
		case "rev-parse --abbrev-ref HEAD":
			return "main\n", nil
		case "describe --tags --abbrev=0":
			return "v0.3.2\n", nil
		case "rev-list --count v0.3.2..HEAD":
			return "14\n", nil
		case "log --pretty=format:%s -n 8 v0.3.2..HEAD":
			return "Coins are worth 25 points instead of 10\nSlimes turn at ledges instead of walking off\nThe door creaks and stays locked until you pull the lever\n", nil
		default:
			return "", nil
		}
	}
	ed.ReleaseCheckRunner = func(ctx context.Context, d string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, 4 * time.Second, nil
	}
	ed.ExportCommandRunner = func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		_, _ = io.WriteString(stdout, "go build ok\n")
		return nil
	}
	ed.ReleaseMeasureRunner = func(ctx context.Context, platform string, plan *releaseplan.Plan) (editor.BuildArtifactResult, error) {
		switch platform {
		case "windows":
			return editor.BuildArtifactResult{
				ArtifactPath:   filepath.Join(plan.OutputDir, "dungeon-run-0.4.0-setup.exe"),
				ArtifactName:   "dungeon-run-0.4.0-setup.exe",
				SizeBytes:      mbToBytes(41.7),
				DeltaBytes:     mbToBytes(1.2),
				HasLastRelease: true,
				Breakdown: editor.BuildSizeBreakdown{
					CodeBytes:     mbToBytes(9.4),
					TexturesBytes: mbToBytes(22.1),
					AudioBytes:    mbToBytes(9.6),
					OtherBytes:    mbToBytes(0.6),
					GrowthSummary: "Grew 1.2 MB: audio/door_creak.ogg and 4 new torch sprites.",
				},
				FrameTimeP95Ms:       11.2,
				SmokePlaytestWhere:   "Windows 11 VM · Low profile",
				SmokePlaytestSummary: "0 errors, 0 falls through the floor, 3 of 3 coins collected, door opened after the lever.",
			}, nil
		default:
			return editor.BuildArtifactResult{
				ArtifactPath:   filepath.Join(plan.OutputDir, "dungeon-run-0.4.0-web.zip"),
				ArtifactName:   "dungeon-run-0.4.0-web.zip",
				SizeBytes:      mbToBytes(18.9),
				DeltaBytes:     mbToBytes(0.4),
				HasLastRelease: true,
				Breakdown: editor.BuildSizeBreakdown{
					CodeBytes:     mbToBytes(6.1),
					TexturesBytes: mbToBytes(9.8),
					AudioBytes:    mbToBytes(2.6),
					OtherBytes:    mbToBytes(0.4),
					GrowthSummary: "Grew 0.4 MB: audio/door_creak.ogg. Audio is Opus in the web build.",
				},
				FrameTimeP95Ms:       9.8,
				SmokePlaytestWhere:   "headless Chromium · Low profile",
				SmokePlaytestSummary: "0 errors, 0 falls through the floor, 3 of 3 coins collected, door opened after the lever.",
				TestURL:              "http://192.168.1.24:8731/",
			}, nil
		}
	}

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SetLastReleaseBaseline(editor.LastReleaseBaseline{Version: "0.3.2"})
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	ed.SetBuildPresetSigning("android", editor.BuildPresetSigningConfig{
		Keystore: "~/keys/dungeon.jks",
		Alias:    "upload",
	})
	_ = ed.SetBuildPresetIncluded("linux", false)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Press("id:build-release-primary-action")
	app.WaitUntil(5*time.Second, func() bool { return !ed.ReleaseBuildRunning() })
	app.Settle()

	// 1. Android failed at Sign + Agent fix proposal diff (matches Build.dc.html default view).
	var propErr error
	app.Post(func() {
		propErr = ed.SetBuildPresetFixProposal("android", &editor.BuildPresetFixProposal{
			Platform: "android",
			Title:    "The agent found the key",
			Summary:  "The keystore moved when keys were put in the keychain. The keychain item “dungeon-run upload” has alias upload, so the preset can point at it instead of a file.",
			FilePath: "export/android.export.yaml",
			Footnote: "Edits export/android.export.yaml · the key never leaves the keychain",
			Diff: []editor.BuildDiffLine{
				{LineNumber: 14, Kind: "context", Text: "signing:"},
				{LineNumber: 15, Kind: "remove", Text: "  keystore: ~/keys/dungeon.jks"},
				{LineNumber: 15, Kind: "add", Text: "  keystore: keychain:dungeon-run upload"},
				{LineNumber: 16, Kind: "context", Text: "  alias: upload"},
			},
			SigningPatch: &editor.BuildPresetSigningConfig{
				Keystore: "keychain:dungeon-run upload",
				Alias:    "upload",
			},
		})
	})
	app.Settle()
	if propErr != nil {
		t.Fatal(propErr)
	}
	app.Press("id:build-preset-pick-android")
	app.Settle()
	app.Screenshot("build-release-failed")

	// 2. Web finished result with breakdown bar, delta vs 0.3.2, budgets, smoke playtest, and QR card.
	app.Press("id:build-preset-pick-web")
	app.Settle()
	app.Screenshot("build-release-done-web")

	// 3. iOS Readiness panel showing 6 requirements and Agent vs Person guidance.
	app.Press("id:build-preset-pick-ios")
	app.Settle()
	app.Screenshot("build-release-readiness-ios")
}
