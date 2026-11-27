package editorcmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/internal/releaseplan"
	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
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
input:
  actions:
    - name: left
      bindings:
        - kind: key
          key: a
    - name: right
      bindings:
        - kind: key
          key: d
    - name: jump
      bindings:
        - kind: key
          key: space
  axes:
    - name: move_x
      negative: left
      positive: right
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
	// Provide NSGo's default assets/Icon.png so the iOS READINESS panel reflects real nsgo.yaml checks:
	// bundle ID, icon, and NSGo's built-in privacy manifest pass; sign.profile / sign.identity need person setup;
	// touch controls offers generating controls.touch.yaml from game.yaml's actions.
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.SetRGBA(x, y, color.RGBA{R: 59, G: 123, B: 255, A: 255})
		}
	}
	f, err := os.Create(filepath.Join(dir, "assets", "Icon.png"))
	if err != nil {
		t.Fatal(err)
	}
	_ = png.Encode(f, img)
	_ = f.Close()

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

// TestBuildReleaseNotesAndReviewUploadsMacOSScreenshots captures real native macOS screenshots of
// RELEASE NOTES ("Drafted from 14 commits since 0.3.2") + Publish footer and the Review uploads
// confirmation dialog listing itch.io, Google Play internal testing, and TestFlight (#1050).
func TestBuildReleaseNotesAndReviewUploadsMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(buildReleaseScreenshotGameYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte("name: dungeon-run\nid: dev.dungeonrun.game\nversion: 0.4.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.Keychain = editor.NewMemoryKeychain()
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
		case "log --pretty=format:%s -n 32 v0.3.2..HEAD":
			return strings.Join([]string{
				"feat(coins): coins are worth 25 points instead of 10",
				"feat(ai): slimes turn at ledges instead of walking off",
				"feat(audio): the door creaks, and stays locked until you pull the lever",
				"fix(render): the Knight was drawn behind slimes",
			}, "\n") + "\n", nil
		default:
			return "", nil
		}
	}
	ed.ReleaseCheckRunner = func(ctx context.Context, d string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, 4 * time.Second, nil
	}
	ed.ReleaseReadinessInspector = func(platform, _ string, _ *project.File) []editor.BuildReadinessItem {
		return nil
	}
	ed.ExportCommandRunner = func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		return nil
	}
	ed.ReleaseMeasureRunner = func(ctx context.Context, platform string, plan *releaseplan.Plan) (editor.BuildArtifactResult, error) {
		switch platform {
		case "web":
			return editor.BuildArtifactResult{
				ArtifactPath:   filepath.Join(plan.OutputDir, "dungeon-run-0.4.0-web.zip"),
				ArtifactName:   "dungeon-run-0.4.0-web.zip",
				SizeBytes:      mbToBytes(18.9),
				DeltaBytes:     mbToBytes(0.4),
				HasLastRelease: true,
				Breakdown: editor.BuildSizeBreakdown{
					CodeBytes: mbToBytes(6.1), TexturesBytes: mbToBytes(9.8), AudioBytes: mbToBytes(2.6), OtherBytes: mbToBytes(0.4),
					GrowthSummary: "Grew 0.4 MB: audio/door_creak.ogg.",
				},
				FrameTimeP95Ms: 9.8,
				TestURL:        "http://192.168.1.24:8731/",
			}, nil
		case "android":
			return editor.BuildArtifactResult{
				ArtifactPath:   filepath.Join(plan.OutputDir, "dungeon-run-0.4.0.aab"),
				ArtifactName:   "dungeon-run-0.4.0.aab",
				SizeBytes:      mbToBytes(34.2),
				DeltaBytes:     mbToBytes(0.8),
				HasLastRelease: true,
				FrameTimeP95Ms: 11.6,
			}, nil
		case "ios":
			return editor.BuildArtifactResult{
				ArtifactPath:   filepath.Join(plan.OutputDir, "dungeon-run-0.4.0.ipa"),
				ArtifactName:   "dungeon-run-0.4.0.ipa",
				SizeBytes:      mbToBytes(38.6),
				DeltaBytes:     mbToBytes(0.9),
				HasLastRelease: true,
				FrameTimeP95Ms: 10.4,
			}, nil
		default:
			return editor.BuildArtifactResult{
				ArtifactPath:   filepath.Join(plan.OutputDir, "dungeon-run-0.4.0-"+platform+".zip"),
				ArtifactName:   "dungeon-run-0.4.0-" + platform + ".zip",
				SizeBytes:      mbToBytes(41.7),
				DeltaBytes:     mbToBytes(1.2),
				HasLastRelease: true,
				FrameTimeP95Ms: 11.2,
			}, nil
		}
	}

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SetLastReleaseBaseline(editor.LastReleaseBaseline{Version: "0.3.2"})
	ed.SetReleaseKeptReviews([]string{"Floor 1 has a slime guarding the plank and a coin trail to the door"})
	_ = ed.SetAgentPermissionProfile(editor.PermissionProfileAutonomous)
	_ = ed.StorePublishCredential(editor.PublishDestItchIO, "dungeon-studio", "itch-demo-token")
	_ = ed.StorePublishCredential(editor.PublishDestGooglePlay, "dungeon-studio", "gplay-demo-token")

	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	_ = ed.SetBuildPresetIncluded("macos", false)
	_ = ed.SetBuildPresetIncluded("windows", false)
	_ = ed.SetBuildPresetIncluded("linux", false)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Press("id:build-release-primary-action")
	app.WaitUntil(5*time.Second, func() bool { return !ed.ReleaseBuildRunning() })
	app.Settle()

	app.Press("id:build-preset-pick-web")
	app.Settle()
	app.Screenshot("build-release-notes-and-publish")

	app.Press("id:build-release-review-uploads")
	app.Settle()
	app.Screenshot("build-release-review-uploads-dialog")
}

// TestBuildReleaseWebQRAndDevicesMacOSScreenshots renders real macOS screenshots of the
// #1049 Web build result card with a live LAN QR code, MCP bridge phone connection,
// Stop/Start serving toggle, and RUN ON DEVICE live states (Mac browser, QR phone, Android USB).
func TestBuildReleaseWebQRAndDevicesMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(buildReleaseScreenshotGameYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte("name: dungeon-run\nid: dev.dungeonrun.game\nversion: 0.4.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })

	ed.LANAddressResolver = func() (string, error) { return "127.0.0.1", nil }
	ed.GitRunner = func(_ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "rev-parse --short HEAD":
			return "5b0e8a4\n", nil
		case "rev-parse --abbrev-ref HEAD":
			return "main\n", nil
		case "describe --tags --abbrev=0":
			return "v0.3.2\n", nil
		case "rev-list --count v0.3.2..HEAD":
			return "14\n", nil
		default:
			return "", nil
		}
	}
	ed.ReleaseCheckRunner = func(_ context.Context, _ string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, 4 * time.Second, nil
	}
	ed.ExportCommandRunner = func(_ context.Context, args []string, stdout, _ io.Writer) error {
		if slices.Contains(args, "web") {
			webOut := filepath.Join(dir, "dist", "web")
			if err := os.MkdirAll(webOut, 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(webOut, "index.html"), []byte("<!doctype html><h1>dungeon-run</h1>"), 0o644); err != nil {
				return err
			}
		}
		_, _ = io.WriteString(stdout, "built ok\n")
		return nil
	}
	ed.ReleaseMeasureRunner = func(_ context.Context, platform string, plan *releaseplan.Plan) (editor.BuildArtifactResult, error) {
		if platform == "android" {
			return editor.BuildArtifactResult{
				ArtifactPath:   filepath.Join(plan.OutputDir, "dungeon-run-0.4.0.aab"),
				ArtifactName:   "dungeon-run-0.4.0.aab",
				SizeBytes:      mbToBytes(28.4),
				DeltaBytes:     mbToBytes(0.6),
				HasLastRelease: true,
				Breakdown: editor.BuildSizeBreakdown{
					CodeBytes:     mbToBytes(8.2),
					TexturesBytes: mbToBytes(14.6),
					AudioBytes:    mbToBytes(4.9),
					OtherBytes:    mbToBytes(0.7),
					GrowthSummary: "Grew 0.6 MB: ETC2 dungeon sprites.",
				},
				FrameTimeP95Ms:       12.1,
				SmokePlaytestWhere:   "Pixel 7 emulator · Low profile",
				SmokePlaytestSummary: "0 errors, 3 of 3 coins collected.",
			}, nil
		}
		return editor.BuildArtifactResult{
			ArtifactPath:   filepath.Join(plan.OutputDir, "dungeon-run-0.4.0-web.zip"),
			ArtifactName:   "dungeon-run-0.4.0-web.zip",
			WebBuildDir:    filepath.Join(plan.OutputDir, "web"),
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
		}, nil
	}
	ed.DeviceDiscoverer = func(_ context.Context, _ string) ([]editor.ConnectedDevice, error) {
		return []editor.ConnectedDevice{{
			ID:        "pixel7-usb-01",
			Name:      "Pixel 7",
			Platform:  "android",
			Transport: "usb",
			Online:    true,
		}}, nil
	}
	ed.OpenBrowserURL = func(string) error { return nil }
	ed.DeviceCommandRunner = func(_ context.Context, _ editor.DeviceCommandRequest, _, _ io.Writer) error { return nil }

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SetLastReleaseBaseline(editor.LastReleaseBaseline{Version: "0.3.2"})
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"macos", "windows", "linux"} {
		_ = ed.SetBuildPresetIncluded(p, false)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Press("id:build-release-primary-action")
	app.WaitUntil(5*time.Second, func() bool { return !ed.ReleaseBuildRunning() })
	app.Settle()

	app.Press("id:build-preset-pick-web")
	app.Settle()

	// Connect a simulated iPhone 15 Play-mode session to the LAN MCP bridge.
	ws := ed.WebBuildServerState()
	if !ws.Serving {
		t.Fatalf("expected web build server serving, got %+v", ws)
	}
	handshake, _ := json.Marshal(map[string]any{
		"token":    ws.BridgeToken,
		"clientId": "iphone-15-safari",
		"device":   "iPhone 15",
		"playMode": true,
	})
	resp, err := http.Post(ws.URL+"__nsgo_mcp/session", "application/json", bytes.NewReader(handshake))
	if err != nil {
		t.Fatalf("POST /__nsgo_mcp/session: %v", err)
	}
	_ = resp.Body.Close()
	app.Settle()

	// 1. Live LAN serving + scannable QR code + MCP bridge connected + Pixel 7 USB ready.
	app.Screenshot("build-release-web-qr-serving")

	// 2. Launch on Pixel 7 USB + Stop serving -> Stopped state + Running on Pixel 7.
	app.Press("id:build-device-android-usb")
	if err := ed.WaitAndroidDeviceRun(); err != nil {
		t.Fatalf("WaitAndroidDeviceRun: %v", err)
	}
	app.Press("id:build-qr-toggle-serve")
	app.Settle()
	app.Screenshot("build-release-web-qr-stopped")
}
