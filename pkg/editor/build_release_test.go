package editor_test

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/internal/releaseplan"
	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
	"github.com/nsgo-dev/game/pkg/tooling/credits"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func writeBuildReleaseProject(t *testing.T, extraYAML string) string {
	t.Helper()
	dir := writeEditorProject(t)
	if extraYAML != "" {
		if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(projectYAML+extraYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writeSolidPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{R: 59, G: 123, B: 255, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// #1048 Acceptance 1: A dry-run build of two presets shows each step in order and produces the exact same commands `game release -dry-run` prints.
func TestBuildReleaseScreenDryRunTwoPresetsMatchesGameReleaseCLI(t *testing.T) {
	dir := writeBuildReleaseProject(t, `export:
  output: dist/release
  budgets:
    qualityProfile: low
    downloadSizeMB: 80
    frameTimeP95Ms: 16.7
  presets:
    - platform: macos
      targets: [macos_arm64]
    - platform: web
assets:
  audio:
    - in: sfx/jump.wav
      out: assets/jump.wav
`)
	if err := os.MkdirAll(filepath.Join(dir, "sfx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sfx", "jump.wav"), []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sfx", "jump.wav.import.yaml"), []byte("version: 1\ncredit: {author: Kenney, license: CC0-1.0}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
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
			return "Coins worth 25 points\nSlimes turn at ledges\n", nil
		default:
			return "", nil
		}
	}
	ed.ReleaseCheckRunner = func(ctx context.Context, d string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, 4 * time.Second, nil
	}
	runnerCalls := 0
	ed.ExportCommandRunner = func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		runnerCalls++
		return nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	var cmdErr error
	app.Post(func() {
		cmdErr = ed.ExecuteCommand(editor.CmdBuildRelease)
	})
	app.Settle()
	if cmdErr != nil {
		t.Fatalf("ExecuteCommand(CmdBuildRelease): %v", cmdErr)
	}

	if !ed.BuildReleaseScreenOpen() {
		t.Fatal("BuildReleaseScreenOpen() = false, want true")
	}
	if !app.Exists("id:build-release-screen") {
		t.Fatal("build-release-screen node missing")
	}

	snap := ed.BuildReleaseSnapshot()
	if snap.GitCommit != "5b0e8a4" || snap.GitBranch != "main" || snap.LastReleaseVersion != "0.3.2" {
		t.Fatalf("header metadata = (%q, %q, %q), want (5b0e8a4, main, 0.3.2)", snap.GitCommit, snap.GitBranch, snap.LastReleaseVersion)
	}
	if snap.Preflight.DownloadSizeBudgetMB != 80 || snap.Preflight.FrameTimeP95BudgetMs != 16.7 || snap.Preflight.QualityProfile != "Low" {
		t.Fatalf("preflight budgets = %+v", snap.Preflight)
	}

	// Toggle Dry Run and press Build.
	app.Press("id:build-release-dry-run")
	app.Settle()
	app.Press("id:build-release-primary-action")
	app.WaitUntil(5*time.Second, func() bool { return !ed.ReleaseBuildRunning() })
	app.Settle()

	if runnerCalls != 0 {
		t.Fatalf("dry-run invoked ExportCommandRunner %d times, want 0", runnerCalls)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist", "release", credits.FileName)); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not write CREDITS.md: %v", err)
	}

	// Verify exact 6 ordered steps ran and completed on both presets.
	wantStepOrder := []string{"Check project", "Compile Go", "Pack assets", "Sign", "Smoke playtest", "Measure"}
	snap = ed.BuildReleaseSnapshot()
	if len(snap.Presets) != 2 {
		t.Fatalf("presets count = %d, want 2", len(snap.Presets))
	}
	for _, p := range snap.Presets {
		if p.Status != editor.BuildPresetDone {
			t.Fatalf("preset %s status = %q, want done", p.Platform, p.Status)
		}
		var gotLabels []string
		for _, st := range p.Steps {
			gotLabels = append(gotLabels, st.Label)
			if st.Status != editor.BuildStepDone {
				t.Fatalf("preset %s step %s status = %q, want done", p.Platform, st.Label, st.Status)
			}
		}
		if !slices.Equal(gotLabels, wantStepOrder) {
			t.Fatalf("preset %s steps = %v, want %v", p.Platform, gotLabels, wantStepOrder)
		}
	}

	// Compare dry-run commands byte-for-byte against `game release -dry-run`.
	gameBin := buildGameTool(t, "../../cmd/game", "game")
	cliCombined, cliErr := runBuiltTool(t, gameBin, "release", "-dry-run", dir)
	if cliErr != "" {
		t.Fatalf("game release -dry-run stderr = %q", cliErr)
	}
	if got := ed.ReleaseDryRunOutput(); got != cliCombined {
		t.Fatalf("ReleaseDryRunOutput mismatch:\ngot:\n%s\nwant:\n%s", got, cliCombined)
	}

	cliMac, _ := runBuiltTool(t, gameBin, "release", "-dry-run", "-target", "macos_arm64", dir)
	cliWeb, _ := runBuiltTool(t, gameBin, "release", "-dry-run", "-target", "web", dir)
	if got := strings.Join(snap.Presets[0].PlanCommands, "\n") + "\n"; got != cliMac {
		t.Fatalf("macOS preset PlanCommands mismatch:\ngot:\n%s\nwant:\n%s", got, cliMac)
	}
	if got := strings.Join(snap.Presets[1].PlanCommands, "\n") + "\n"; got != cliWeb {
		t.Fatalf("web preset PlanCommands mismatch:\ngot:\n%s\nwant:\n%s", got, cliWeb)
	}
}

// #1048 Acceptance 2: A preset whose signing key is missing fails at Sign while the second checked preset finishes (Done)
// and "Other platforms keep building. Nothing was uploaded." is shown; applying the agent's preset fix rebuilds it to Done.
func TestBuildReleaseScreenMissingSigningKeyFailsAtSignWhileOtherFinishes(t *testing.T) {
	dir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: android
      targets: [android_arm64]
    - platform: web
`)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.ReleaseCheckRunner = func(ctx context.Context, d string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, 2 * time.Second, nil
	}
	ed.ExportCommandRunner = func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		_, _ = io.WriteString(stdout, "compiled ok\n")
		return nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}

	// Point Android signing at a missing keystore file.
	missingKeystore := filepath.Join(t.TempDir(), "missing-dungeon.jks")
	ed.SetBuildPresetSigning("android", editor.BuildPresetSigningConfig{
		Keystore: missingKeystore,
		Alias:    "upload",
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Press("id:build-release-primary-action")
	app.WaitUntil(5*time.Second, func() bool { return !ed.ReleaseBuildRunning() })
	app.Settle()

	snap := ed.BuildReleaseSnapshot()
	var androidP, webP *editor.BuildPresetEntry
	for i := range snap.Presets {
		switch snap.Presets[i].Platform {
		case "android":
			androidP = &snap.Presets[i]
		case "web":
			webP = &snap.Presets[i]
		}
	}
	if androidP == nil || webP == nil {
		t.Fatalf("missing presets in snapshot: %+v", snap.Presets)
	}

	// Android failed specifically at Sign (step 4 of 6).
	if androidP.Status != editor.BuildPresetFailed {
		t.Fatalf("android status = %q, want failed", androidP.Status)
	}
	if androidP.Failure == nil {
		t.Fatal("android Failure is nil")
	}
	if androidP.Failure.StepID != editor.BuildStepSign || androidP.Failure.StepIndex != 4 {
		t.Fatalf("android failed at step %q (#%d), want sign (#4)", androidP.Failure.StepID, androidP.Failure.StepIndex)
	}
	if androidP.Failure.Notice != editor.BuildFailureNotice {
		t.Fatalf("android failure notice = %q, want %q", androidP.Failure.Notice, editor.BuildFailureNotice)
	}
	if androidP.Steps[0].Status != editor.BuildStepDone || androidP.Steps[1].Status != editor.BuildStepDone || androidP.Steps[2].Status != editor.BuildStepDone || androidP.Steps[3].Status != editor.BuildStepFailed {
		t.Fatalf("unexpected android step statuses: %+v", androidP.Steps)
	}

	// Second checked preset (web) continued and finished Done!
	if webP.Status != editor.BuildPresetDone {
		t.Fatalf("web status = %q, want done (other platforms must keep building)", webP.Status)
	}

	// UI renders the failure notice on the selected Android preset.
	app.Press("id:build-preset-pick-android")
	app.Settle()
	if !app.Exists("id:build-failure-card") {
		t.Fatal("build-failure-card not rendered for failed Android preset")
	}
	if !app.Exists("id:build-failure-notice") {
		t.Fatal("build-failure-notice not rendered")
	}

	// Attach an agent-proposed preset fix diff and press Apply and rebuild Android.
	var fixErr error
	app.Post(func() {
		fixErr = ed.SetBuildPresetFixProposal("android", &editor.BuildPresetFixProposal{
			Platform: "android",
			Title:    "The agent found the key",
			Summary:  "Point the preset at the keychain item instead of the moved file.",
			FilePath: "export/android.export.yaml",
			Footnote: "Edits export/android.export.yaml · the key never leaves the keychain",
			Diff: []editor.BuildDiffLine{
				{LineNumber: 14, Kind: "context", Text: "signing:"},
				{LineNumber: 15, Kind: "remove", Text: "  keystore: " + missingKeystore},
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
	if fixErr != nil {
		t.Fatalf("SetBuildPresetFixProposal: %v", fixErr)
	}
	if !app.Exists("id:build-fix-card") || !app.Exists("id:build-apply-fix") {
		t.Fatal("agent fix proposal card or Apply button missing")
	}

	app.Press("id:build-apply-fix")
	app.WaitUntil(5*time.Second, func() bool { return !ed.ReleaseBuildRunning() })
	app.Settle()

	snap = ed.BuildReleaseSnapshot()
	for _, p := range snap.Presets {
		if p.Platform == "android" && p.Status != editor.BuildPresetDone {
			t.Fatalf("android status after Apply and rebuild = %q (failure=%+v), want done", p.Status, p.Failure)
		}
	}
}

// Verifies sequential execution, Stop preserving finished steps, and Resume continuing from the interrupted step.
func TestBuildReleaseScreenStopKeepsCompletedStepsAndResumeContinues(t *testing.T) {
	dir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: macos
      targets: [macos_arm64]
    - platform: web
`)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var executed []string
	enteredPack := make(chan struct{}, 1)
	stopOnce := true

	ed.ReleaseStepRunner = func(ctx context.Context, req editor.ReleaseStepRequest, stdout, stderr io.Writer) error {
		mu.Lock()
		shouldBlock := req.Platform == "macos" && req.StepID == editor.BuildStepPack && stopOnce
		if shouldBlock {
			stopOnce = false
		}
		if !shouldBlock {
			executed = append(executed, req.Platform+":"+string(req.StepID))
		}
		mu.Unlock()

		if shouldBlock {
			enteredPack <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}

	if err := ed.StartReleaseBuild(editor.BuildReleaseOptions{DryRun: true}); err != nil {
		t.Fatalf("StartReleaseBuild: %v", err)
	}
	select {
	case <-enteredPack:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for macos Pack assets step")
	}

	ed.StopReleaseBuild()
	_ = ed.WaitReleaseBuild()

	if !ed.ReleaseBuildStopped() {
		t.Fatal("ReleaseBuildStopped() = false after StopReleaseBuild")
	}
	snap := ed.BuildReleaseSnapshot()
	mac := snap.Presets[0]
	if mac.Steps[0].Status != editor.BuildStepDone || mac.Steps[1].Status != editor.BuildStepDone {
		t.Fatalf("completed steps not kept on Stop: %+v", mac.Steps)
	}
	if mac.NextStepIdx != 2 {
		t.Fatalf("NextStepIdx = %d, want 2 (Pack assets)", mac.NextStepIdx)
	}

	// Resume continues from Pack assets without re-running Check project or Compile Go!
	if err := ed.ResumeReleaseBuild(); err != nil {
		t.Fatalf("ResumeReleaseBuild: %v", err)
	}
	_ = ed.WaitReleaseBuild()

	mu.Lock()
	got := slices.Clone(executed)
	mu.Unlock()

	want := []string{
		"macos:check", "macos:compile",
		"macos:pack", "macos:sign", "macos:smoke_playtest", "macos:measure",
		"web:check", "web:compile", "web:pack", "web:sign", "web:smoke_playtest", "web:measure",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("executed steps across Stop+Resume = %v, want %v", got, want)
	}
}

func mbBytes(mb float64) int64 { return int64(mb * 1024 * 1024) }

// Verifies finished build result breakdown, delta against last release, budgets met/missed, Show in Finder, and Edit preset.
func TestBuildReleaseScreenFinishedResultBreakdownBudgetsAndFinder(t *testing.T) {
	dir := writeBuildReleaseProject(t, `export:
  budgets:
    qualityProfile: low
    downloadSizeMB: 80
    frameTimeP95Ms: 16.7
  presets:
    - platform: windows
      targets: [windows_x64]
    - platform: web
`)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.ReleaseCheckRunner = func(ctx context.Context, d string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, 3 * time.Second, nil
	}
	ed.ReleaseMeasureRunner = func(ctx context.Context, platform string, plan *releaseplan.Plan) (editor.BuildArtifactResult, error) {
		if platform == "windows" {
			return editor.BuildArtifactResult{
				ArtifactPath:   filepath.Join(plan.OutputDir, "dungeon-run-0.4.0-setup.exe"),
				ArtifactName:   "dungeon-run-0.4.0-setup.exe",
				SizeBytes:      mbBytes(41.7),
				DeltaBytes:     mbBytes(1.2),
				HasLastRelease: true,
				Breakdown: editor.BuildSizeBreakdown{
					CodeBytes:     mbBytes(9.4),
					TexturesBytes: mbBytes(22.1),
					AudioBytes:    mbBytes(9.6),
					OtherBytes:    mbBytes(0.6),
					GrowthSummary: "Grew 1.2 MB: audio/door_creak.ogg and 4 new torch sprites.",
				},
				FrameTimeP95Ms:       11.2,
				SmokePlaytestWhere:   "Windows 11 VM · Low profile",
				SmokePlaytestSummary: "0 errors, 3 of 3 coins collected.",
			}, nil
		}
		return editor.BuildArtifactResult{
			ArtifactPath:   filepath.Join(plan.OutputDir, "dungeon-run-0.4.0-web.zip"),
			ArtifactName:   "dungeon-run-0.4.0-web.zip",
			SizeBytes:      mbBytes(18.9),
			DeltaBytes:     mbBytes(0.4),
			HasLastRelease: true,
			Breakdown: editor.BuildSizeBreakdown{
				CodeBytes:     mbBytes(6.1),
				TexturesBytes: mbBytes(9.8),
				AudioBytes:    mbBytes(2.6),
				OtherBytes:    mbBytes(0.4),
				GrowthSummary: "Grew 0.4 MB: audio/door_creak.ogg.",
			},
			FrameTimeP95Ms:       19.4, // misses 16.7 ms budget
			SmokePlaytestWhere:   "headless Chromium · Low profile",
			SmokePlaytestSummary: "0 errors, door opened after lever.",
			TestURL:              "http://192.168.1.24:8731/",
		}, nil
	}
	var openedFolders []string
	ed.OpenOutputFolder = func(d string) error {
		openedFolders = append(openedFolders, d)
		return nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SetLastReleaseBaseline(editor.LastReleaseBaseline{Version: "0.3.2"})
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Press("id:build-release-dry-run")
	app.Press("id:build-release-primary-action")
	app.WaitUntil(5*time.Second, func() bool { return !ed.ReleaseBuildRunning() })
	app.Settle()

	app.Press("id:build-preset-pick-windows")
	app.Settle()
	if !app.Exists("id:build-detail-done") || !app.Exists("id:build-result-artifact") || !app.Exists("id:build-budget-download") || !app.Exists("id:build-budget-frame") {
		t.Fatal("finished build detail cards missing")
	}
	app.Press("id:build-detail-show-finder")
	app.Settle()
	if len(openedFolders) != 1 {
		t.Fatalf("Show in Finder opened %d folders, want 1", len(openedFolders))
	}

	// Verify Edit preset drawer toggles cleanly.
	app.Press("id:build-detail-edit-preset")
	app.Settle()
	if !app.Exists("id:build-inline-preset-editor") {
		t.Fatal("Edit preset did not open inline preset editor")
	}

	// Verify web preset shows QR card and misses frame budget (19.4 > 16.7 ms).
	app.Press("id:build-preset-pick-web")
	app.Settle()
	if !app.Exists("id:build-result-qr") {
		t.Fatal("web finished build should show QR test link card")
	}
	snap := ed.BuildReleaseSnapshot()
	for _, p := range snap.Presets {
		if p.Platform == "windows" && (!p.Result.DownloadBudgetMet || !p.Result.FrameTimeBudgetMet) {
			t.Fatalf("windows budgets should both be met: %+v", p.Result)
		}
		if p.Platform == "web" && (!p.Result.DownloadBudgetMet || p.Result.FrameTimeBudgetMet) {
			t.Fatalf("web should meet download budget and miss frame time budget: %+v", p.Result)
		}
	}
}

// Verifies iOS READINESS panel shows all 6 requirements, distinguishes agent vs person actions,
// writes resources/PrivacyInfo.xcprivacy (and merges cleanly with apple.privacy), generates
// controls.touch.yaml for existing game.yaml actions, and delegates Apple sign-in to person callback.
func TestBuildReleaseScreenIOSReadinessAgentVsPerson(t *testing.T) {
	dir := writeBuildReleaseProject(t, `input:
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
  presets:
    - platform: ios
    - platform: web
`)
	writeSolidPNG(t, filepath.Join(dir, "assets", "Icon.png"), 64, 64)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	var personActions []string
	ed.OnReleaseReadinessPersonAction = func(platform string, item editor.BuildReadinessItem) error {
		personActions = append(personActions, platform+":"+item.ID)
		return nil
	}
	reviewUploadsCalled := 0
	ed.OnReviewReleaseUploads = func(snap editor.BuildReleaseSnapshot) error {
		reviewUploadsCalled++
		return nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Press("id:build-preset-pick-ios")
	app.Settle()

	if !app.Exists("id:build-detail-readiness") {
		t.Fatal("iOS readiness panel not shown")
	}

	snap := ed.BuildReleaseSnapshot()
	ios := snap.SelectedPreset()
	if ios == nil || ios.Platform != "ios" || ios.Status != editor.BuildPresetSetup || ios.Included {
		t.Fatalf("iOS should start in Set up & unchecked: %+v", ios)
	}
	if len(ios.Readiness) != 6 {
		t.Fatalf("iOS readiness items = %d, want 6", len(ios.Readiness))
	}

	// Verify privacy manifest starts OK (NSGo writes it) with optional agent action,
	// touch_controls offers agent generation from game.yaml actions, and provisioning_profile requires person.
	var iconItem, privacyItem, profileItem, touchItem editor.BuildReadinessItem
	for _, it := range ios.Readiness {
		switch it.ID {
		case "app_icon":
			iconItem = it
		case "privacy_manifest":
			privacyItem = it
		case "provisioning_profile":
			profileItem = it
		case "touch_controls":
			touchItem = it
		}
	}
	if iconItem.Status != editor.BuildReadinessOK || !strings.Contains(iconItem.Detail, "assets/Icon.png") {
		t.Fatalf("app_icon = %+v, want ok for assets/Icon.png", iconItem)
	}
	if privacyItem.Status != editor.BuildReadinessOK || !privacyItem.AgentCanHandle || privacyItem.RequiresPerson {
		t.Fatalf("privacy_manifest must be Status=ok, AgentCanHandle=true, RequiresPerson=false: %+v", privacyItem)
	}
	if !profileItem.RequiresPerson || profileItem.AgentCanHandle || !strings.Contains(profileItem.Detail, "sign.profile") {
		t.Fatalf("provisioning_profile must be RequiresPerson=true, AgentCanHandle=false, mention sign.profile: %+v", profileItem)
	}
	if touchItem.Status != editor.BuildReadinessWarn || !touchItem.AgentCanHandle || touchItem.RequiresPerson {
		t.Fatalf("touch_controls must be Status=warn, AgentCanHandle=true when game.yaml has actions: %+v", touchItem)
	}

	// Click privacy_manifest action -> writes resources/PrivacyInfo.xcprivacy (never project root).
	app.Press("id:build-readiness-action-privacy_manifest")
	app.Settle()
	if _, err := os.Stat(filepath.Join(dir, "PrivacyInfo.xcprivacy")); !os.IsNotExist(err) {
		t.Fatalf("privacy_manifest fix must not write to project root PrivacyInfo.xcprivacy: %v", err)
	}
	resPrivacyPath := filepath.Join(dir, "resources", "PrivacyInfo.xcprivacy")
	resPrivacyBytes, err := os.ReadFile(resPrivacyPath)
	if err != nil {
		t.Fatalf("expected resources/PrivacyInfo.xcprivacy to be written: %v", err)
	}
	// Verify NSGo-compatible merged manifest includes NSGo's required-reason APIs plus apple.privacy declarations.
	merged, err := releaseplan.MergePrivacyManifest(resPrivacyBytes, releaseplan.NSGoPrivacyConfig{
		AccessedAPIs: map[string][]string{
			"NSPrivacyAccessedAPICategoryDiskSpace": {"E174.1"},
		},
	})
	if err != nil {
		t.Fatalf("MergePrivacyManifest(resources/PrivacyInfo.xcprivacy): %v", err)
	}
	for _, wantKey := range []string{
		"NSPrivacyAccessedAPICategoryDiskSpace",
		"NSPrivacyAccessedAPICategoryFileTimestamp",
		"NSPrivacyAccessedAPICategorySystemBootTime",
		"NSPrivacyAccessedAPICategoryUserDefaults",
	} {
		if !strings.Contains(string(merged), wantKey) {
			t.Fatalf("merged PrivacyInfo.xcprivacy missing %s:\n%s", wantKey, merged)
		}
	}

	// Click touch_controls action -> writes canonical controls.touch.yaml for existing game.yaml actions (no fake touch_action).
	app.Press("id:build-readiness-action-touch_controls")
	app.Settle()
	touchBytes, err := os.ReadFile(filepath.Join(dir, "controls.touch.yaml"))
	if err != nil {
		t.Fatalf("expected controls.touch.yaml to be written: %v", err)
	}
	layout, err := input.ParseTouchLayout(touchBytes)
	if err != nil {
		t.Fatalf("ParseTouchLayout(controls.touch.yaml): %v", err)
	}
	if len(layout.Controls) != 2 {
		t.Fatalf("generated controls = %d, want 2 (dpad + jump): %+v", len(layout.Controls), layout.Controls)
	}
	if strings.Contains(string(touchBytes), "touch_action") {
		t.Fatalf("generated controls.touch.yaml must not contain fake touch_action:\n%s", touchBytes)
	}
	for _, a := range ed.Project.Input.Actions {
		if a.Name == "touch_action" {
			t.Fatal("touch_controls action must not add fake touch_action to game.yaml")
		}
	}

	snap = ed.BuildReleaseSnapshot()
	for _, it := range snap.SelectedPreset().Readiness {
		if it.ID == "privacy_manifest" && (it.Status != editor.BuildReadinessOK || !strings.Contains(it.Detail, "resources/PrivacyInfo.xcprivacy")) {
			t.Fatalf("privacy_manifest after write = %+v", it)
		}
		if it.ID == "touch_controls" && (it.Status != editor.BuildReadinessOK || !strings.Contains(it.Detail, "controls.touch.yaml")) {
			t.Fatalf("touch_controls after write = %+v", it)
		}
	}

	// Click "Sign in to Apple…" -> delegates to person action callback.
	app.Press("id:build-readiness-action-provisioning_profile")
	app.Settle()
	if !slices.Equal(personActions, []string{"ios:provisioning_profile"}) {
		t.Fatalf("personActions = %v, want [ios:provisioning_profile]", personActions)
	}

	// Click "Review uploads…" -> delegates cleanly without automatic upload.
	app.Press("id:build-release-review-uploads")
	app.Settle()
	if reviewUploadsCalled != 1 {
		t.Fatalf("reviewUploadsCalled = %d, want 1", reviewUploadsCalled)
	}
}

func TestBuildReleaseScreenIOSReadinessConfiguredThroughNSGoYAMLAndEmptyProject(t *testing.T) {
	// 1. Project configured through nsgo.yaml passes every readiness item and iOS preset is Queued & Included.
	dir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: ios
`)
	writeSolidPNG(t, filepath.Join(dir, "branding", "AppIcon.png"), 1024, 1024)
	if err := os.MkdirAll(filepath.Join(dir, "certs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "certs", "AppStore.mobileprovision"), []byte("profile"), 0o644); err != nil {
		t.Fatal(err)
	}
	nsgoYAML := `name: Demo
id: dev.example.demo
version: 1.0.0
icon:
  png: branding/AppIcon.png
sign:
  profile: certs/AppStore.mobileprovision
apple:
  privacy:
    accessed_apis:
      NSPrivacyAccessedAPICategoryDiskSpace:
        - E174.1
`
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte(nsgoYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	touchYAML := `version: 1
reference: {width: 844, height: 390}
controls:
  - id: jump
    kind: button
    anchor: bottomRight
    size: {width: 96, height: 96}
    action: jump
    text: JUMP
`
	if err := os.WriteFile(filepath.Join(dir, "controls.touch.yaml"), []byte(touchYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.ReleaseCheckRunner = func(context.Context, string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	snap := ed.BuildReleaseSnapshot()
	if len(snap.Presets) != 1 || snap.Presets[0].Platform != "ios" {
		t.Fatalf("unexpected presets: %+v", snap.Presets)
	}
	ios := snap.Presets[0]
	if ios.Status != editor.BuildPresetQueued || !ios.Included {
		t.Fatalf("configured iOS preset = (status=%q, included=%v), want (queued, true); readiness=%+v", ios.Status, ios.Included, ios.Readiness)
	}
	for _, it := range ios.Readiness {
		if it.Status != editor.BuildReadinessOK {
			t.Errorf("readiness item %s = %q (%s), want ok", it.ID, it.Status, it.Detail)
		}
	}

	// 2. Empty project (missing nsgo.yaml config and no touch actions) fails readiness with details naming nsgo.yaml keys.
	emptyDir := t.TempDir()
	emptyEd, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	emptyEd.Project = &project.File{}
	emptyEd.ProjectPath = filepath.Join(emptyDir, project.FileName)
	emptyItems := emptyEd.EvaluateBuildReadiness("ios")
	if len(emptyItems) != 6 {
		t.Fatalf("empty project readiness items = %d, want 6", len(emptyItems))
	}
	for _, it := range emptyItems {
		switch it.ID {
		case "bundle_id":
			if it.Status != editor.BuildReadinessFail || !strings.Contains(it.Detail, "nsgo.yaml") {
				t.Errorf("empty bundle_id = %+v", it)
			}
		case "app_icon":
			if it.Status != editor.BuildReadinessWarn || !strings.Contains(it.Detail, "icon.png") {
				t.Errorf("empty app_icon = %+v", it)
			}
		case "distribution_cert":
			if it.Status != editor.BuildReadinessWarn || !strings.Contains(it.Detail, "sign.identity") {
				t.Errorf("empty distribution_cert = %+v", it)
			}
		case "provisioning_profile":
			if it.Status != editor.BuildReadinessFail || !strings.Contains(it.Detail, "sign.profile") {
				t.Errorf("empty provisioning_profile = %+v", it)
			}
		case "privacy_manifest":
			if it.Status != editor.BuildReadinessOK || !strings.Contains(it.Detail, "apple.privacy") {
				t.Errorf("empty privacy_manifest = %+v", it)
			}
		case "touch_controls":
			if it.Status != editor.BuildReadinessWarn || it.AgentCanHandle {
				t.Errorf("empty touch_controls = %+v, want advisory warn", it)
			}
		}
	}
}

// #1667 Acceptance: The privacy fix writes under resources/ and a real nsgo build merges it
// with NSGo's required-reason APIs and nsgo.yaml's apple.privacy into the Apple bundle.
func TestBuildReleasePrivacyFixBuildsMergedManifestWithNSGo(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("requires a desktop host Go toolchain")
	}
	dir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: ios
`)
	nsgoYAML := `name: Demo
id: dev.example.demo
version: 1.0.0
apple:
  privacy:
    accessed_apis:
      NSPrivacyAccessedAPICategoryDiskSpace:
        - E174.1
`
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte(nsgoYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/privacytest\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.ReleaseCheckRunner = func(context.Context, string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	if err := ed.RunBuildReadinessAction("ios", "privacy_manifest"); err != nil {
		t.Fatalf("RunBuildReadinessAction(privacy_manifest): %v", err)
	}

	resPath := filepath.Join(dir, "resources", "PrivacyInfo.xcprivacy")
	resData, err := os.ReadFile(resPath)
	if err != nil {
		t.Fatalf("resources/PrivacyInfo.xcprivacy missing: %v", err)
	}

	// Build an Apple bundle with nsgo build on macOS so NSGo itself merges resources/PrivacyInfo.xcprivacy + apple.privacy.
	if runtime.GOOS == "darwin" {
		target := "macos_arm64"
		if runtime.GOARCH == "amd64" {
			target = "macos_x64"
		}
		outDir := filepath.Join(dir, "dist-privacy")
		cmd := exec.Command("go", "tool", "nsgo", "build", "-target", target, "-installers=false", "-output", outDir, dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go tool nsgo build failed: %v\n%s", err, out)
		}
		bundledPath := filepath.Join(outDir, "Demo-1.0.0-"+target, "Demo.app", "Contents", "Resources", "PrivacyInfo.xcprivacy")
		bundled, err := os.ReadFile(bundledPath)
		if err != nil {
			t.Fatalf("bundled PrivacyInfo.xcprivacy missing at %s: %v", bundledPath, err)
		}
		decoded, err := releaseplan.DecodePlist(bundled)
		if err != nil {
			t.Fatalf("DecodePlist(bundled): %v\n%s", err, bundled)
		}
		manifest, ok := decoded.(map[string]any)
		if !ok {
			t.Fatalf("bundled manifest type = %T, want map[string]any", decoded)
		}
		if err := releaseplan.ValidateResourcePrivacy(manifest); err != nil {
			t.Fatalf("ValidateResourcePrivacy(bundled): %v", err)
		}
		for _, wantKey := range []string{
			"NSPrivacyAccessedAPICategoryDiskSpace",
			"NSPrivacyAccessedAPICategoryFileTimestamp",
			"NSPrivacyAccessedAPICategorySystemBootTime",
			"NSPrivacyAccessedAPICategoryUserDefaults",
		} {
			if !strings.Contains(string(bundled), wantKey) {
				t.Fatalf("nsgo-built PrivacyInfo.xcprivacy missing %s:\n%s", wantKey, bundled)
			}
		}
	}

	// Cross-platform merge verification via releaseplan.MergePrivacyManifest (runs on macOS, Linux, and Windows).
	merged, err := releaseplan.MergePrivacyManifest(resData, releaseplan.NSGoPrivacyConfig{
		AccessedAPIs: map[string][]string{
			"NSPrivacyAccessedAPICategoryDiskSpace": {"E174.1"},
		},
	})
	if err != nil {
		t.Fatalf("MergePrivacyManifest: %v", err)
	}
	for _, wantKey := range []string{
		"NSPrivacyAccessedAPICategoryDiskSpace",
		"NSPrivacyAccessedAPICategoryFileTimestamp",
		"NSPrivacyAccessedAPICategorySystemBootTime",
		"NSPrivacyAccessedAPICategoryUserDefaults",
	} {
		if !strings.Contains(string(merged), wantKey) {
			t.Fatalf("merged PrivacyInfo.xcprivacy missing %s:\n%s", wantKey, merged)
		}
	}
}

func TestBuildReleaseHardeningSymlinksKnownFieldsAndFinder(t *testing.T) {
	outside := t.TempDir()
	outsideSecret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideSecret, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := writeBuildReleaseProject(t, `input:
  actions:
    - name: jump
export:
  presets:
    - platform: android
    - platform: ios
    - platform: web
`)
	// Symlink resources/PrivacyInfo.xcprivacy and controls.touch.yaml to an outside file -> agent writes must fail without touching outside file.
	if err := os.MkdirAll(filepath.Join(dir, "resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideSecret, filepath.Join(dir, "resources", "PrivacyInfo.xcprivacy")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink(outsideSecret, filepath.Join(dir, "controls.touch.yaml")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	// Symlink export/android.export.yaml to an outside file -> ApplyBuildPresetFixAndRebuild must fail.
	if err := os.MkdirAll(filepath.Join(dir, "export"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideSecret, filepath.Join(dir, "export", "android.export.yaml")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	// Put an unknown field in export/web.export.yaml and build/last-release.yaml -> KnownFields(true) must ignore them.
	if err := os.WriteFile(filepath.Join(dir, "export", "web.export.yaml"), []byte("summary: custom\nunknownField: 123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build", "last-release.yaml"), []byte("version: \"9.9.9\"\nbogusKey: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.ReleaseCheckRunner = func(_ context.Context, _ string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	ed.ReleaseReadinessInspector = func(platform, _ string, _ *project.File) []editor.BuildReadinessItem {
		if platform == "ios" {
			return []editor.BuildReadinessItem{
				{
					ID:             "privacy_manifest",
					Label:          "Privacy manifest",
					Status:         editor.BuildReadinessWarn,
					AgentCanHandle: true,
				},
				{
					ID:             "touch_controls",
					Label:          "Touch controls",
					Status:         editor.BuildReadinessWarn,
					AgentCanHandle: true,
				},
			}
		}
		return nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}

	// Verify KnownFields(true) refused the malformed web.export.yaml and last-release.yaml.
	snap := ed.BuildReleaseSnapshot()
	if snap.LastReleaseVersion == "9.9.9" {
		t.Fatalf("last-release.yaml with unknown field should have been rejected, got version %q", snap.LastReleaseVersion)
	}
	for _, p := range snap.Presets {
		if p.Platform == "web" && p.Summary == "custom" {
			t.Fatal("web.export.yaml with unknown field should have been rejected")
		}
	}

	// Verify symlinked resources/PrivacyInfo.xcprivacy and controls.touch.yaml writes are refused and outsideSecret is untouched.
	if err := ed.RunBuildReadinessAction("ios", "privacy_manifest"); err == nil {
		t.Fatal("expected symlinked resources/PrivacyInfo.xcprivacy write to fail")
	}
	if err := ed.RunBuildReadinessAction("ios", "touch_controls"); err == nil {
		t.Fatal("expected symlinked controls.touch.yaml write to fail")
	}
	if got, _ := os.ReadFile(outsideSecret); string(got) != "untouched" {
		t.Fatalf("outsideSecret modified: %q", got)
	}

	// Verify symlinked export/android.export.yaml write in ApplyBuildPresetFixAndRebuild is refused.
	if err := ed.SetBuildPresetFixProposal("android", &editor.BuildPresetFixProposal{
		Platform:     "android",
		Title:        "Fix keystore",
		SigningPatch: &editor.BuildPresetSigningConfig{Keystore: "keychain:test"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ed.ApplyBuildPresetFixAndRebuild("android"); err == nil {
		t.Fatal("expected ApplyBuildPresetFixAndRebuild through symlink to fail")
	}
	if got, _ := os.ReadFile(outsideSecret); string(got) != "untouched" {
		t.Fatalf("outsideSecret modified after fix: %q", got)
	}

	// Verify ShowBuildArtifactInFinder rejects an artifact path escaping the project root.
	opened := false
	ed.OpenOutputFolder = func(string) error {
		opened = true
		return nil
	}
	ed.ReleaseMeasureRunner = func(_ context.Context, platform string, _ *releaseplan.Plan) (editor.BuildArtifactResult, error) {
		return editor.BuildArtifactResult{
			ArtifactPath: filepath.Join(outside, "evil.zip"),
			ArtifactName: "evil.zip",
			SizeBytes:    1024,
		}, nil
	}
	if err := ed.SetBuildPresetIncluded("android", false); err != nil {
		t.Fatal(err)
	}
	if err := ed.StartReleaseBuild(editor.BuildReleaseOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if err := ed.WaitReleaseBuild(); err != nil {
		t.Fatal(err)
	}
	if err := ed.ShowBuildArtifactInFinder("web"); err == nil {
		t.Fatal("expected ShowBuildArtifactInFinder to reject path outside project dir")
	}
	if opened {
		t.Fatal("OpenOutputFolder should not be called for outside path")
	}
}

func TestBuildReleaseHardeningConcurrencyPrioritizePlayAndInputFocus(t *testing.T) {
	dir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: macos
    - platform: windows
    - platform: linux
`)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.ReleaseCheckRunner = func(_ context.Context, _ string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Verify sibling full-window screens mutually exclude Build & Release.
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	ed.SetPlaytestReportOpen(true)
	if ed.BuildReleaseScreenOpen() || !ed.PlaytestReportOpen() {
		t.Fatalf("expected PlaytestReport to close BuildRelease (build=%v playtest=%v)", ed.BuildReleaseScreenOpen(), ed.PlaytestReportOpen())
	}
	ed.SetHUDLayoutOpen(true)
	if ed.PlaytestReportOpen() || !ed.HUDLayoutOpen() {
		t.Fatalf("expected HUDLayout to close PlaytestReport (hud=%v playtest=%v)", ed.HUDLayoutOpen(), ed.PlaytestReportOpen())
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	if ed.HUDLayoutOpen() || !ed.BuildReleaseScreenOpen() {
		t.Fatalf("expected BuildRelease to close HUDLayout (build=%v hud=%v)", ed.BuildReleaseScreenOpen(), ed.HUDLayoutOpen())
	}

	// Live PrioritizeBuildPreset + concurrent -race exercise (scope == nil headless).
	macosStarted := make(chan struct{})
	releaseMacos := make(chan struct{})
	var buildOrderMu sync.Mutex
	var buildOrder []string
	var workerExited atomic.Bool

	ed.ReleaseStepRunner = func(ctx context.Context, req editor.ReleaseStepRequest, stdout, _ io.Writer) error {
		if req.StepID == editor.BuildStepCheck {
			buildOrderMu.Lock()
			buildOrder = append(buildOrder, req.Platform)
			buildOrderMu.Unlock()
		}
		if req.Platform == "macos" && req.StepID == editor.BuildStepCheck {
			close(macosStarted)
			select {
			case <-releaseMacos:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		_, _ = io.WriteString(stdout, "step "+req.Platform+" "+string(req.StepID)+"\n")
		return nil
	}
	if err := ed.StartReleaseBuild(editor.BuildReleaseOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	<-macosStarted

	// While running: readiness mutations are blocked with ErrReleaseBuildRunning.
	if err := ed.RunBuildReadinessAction("macos", "any"); err != editor.ErrReleaseBuildRunning {
		t.Fatalf("RunBuildReadinessAction while running = %v, want ErrReleaseBuildRunning", err)
	}
	if err := ed.RunBuildReadinessAgentSetup("macos"); err != editor.ErrReleaseBuildRunning {
		t.Fatalf("RunBuildReadinessAgentSetup while running = %v, want ErrReleaseBuildRunning", err)
	}

	// Prioritize linux ahead of windows while macos is still building.
	ed.PrioritizeBuildPreset("linux")
	// Concurrent status/snapshot reads while worker runs (-race check).
	_ = ed.BuildReleaseSnapshot()
	ed.SelectBuildPreset("linux")
	close(releaseMacos)

	if err := ed.WaitReleaseBuild(); err != nil {
		t.Fatalf("WaitReleaseBuild: %v", err)
	}
	buildOrderMu.Lock()
	gotOrder := slices.Clone(buildOrder)
	buildOrderMu.Unlock()
	if want := []string{"macos", "linux", "windows"}; !slices.Equal(gotOrder, want) {
		t.Fatalf("live prioritized build order = %v, want %v", gotOrder, want)
	}

	// Verify WaitReleaseBuild blocks until the worker goroutine truly exits after StopReleaseBuild.
	inStep := make(chan struct{})
	ed.ReleaseStepRunner = func(ctx context.Context, req editor.ReleaseStepRequest, _ io.Writer, _ io.Writer) error {
		if req.StepID == editor.BuildStepCheck {
			close(inStep)
			<-ctx.Done()
			time.Sleep(25 * time.Millisecond)
			workerExited.Store(true)
			return ctx.Err()
		}
		return nil
	}
	if err := ed.StartReleaseBuild(editor.BuildReleaseOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	<-inStep
	ed.StopReleaseBuild()
	_ = ed.WaitReleaseBuild()
	if !workerExited.Load() {
		t.Fatal("WaitReleaseBuild returned before worker goroutine finished cleanup")
	}

	// Verify NaN frame time & MinInt64 size delta render cleanly and build-release-version keeps focus across brRev ticks.
	ed.ReleaseStepRunner = nil
	ed.ReleaseMeasureRunner = func(_ context.Context, platform string, plan *releaseplan.Plan) (editor.BuildArtifactResult, error) {
		return editor.BuildArtifactResult{
			ArtifactPath:   filepath.Join(plan.OutputDir, platform+".zip"),
			ArtifactName:   platform + ".zip",
			SizeBytes:      -10,
			DeltaBytes:     math.MinInt64,
			HasLastRelease: true,
			FrameTimeP95Ms: math.NaN(),
		}, nil
	}
	if err := ed.SetBuildPresetIncluded("windows", false); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetBuildPresetIncluded("linux", false); err != nil {
		t.Fatal(err)
	}
	if err := ed.StartReleaseBuild(editor.BuildReleaseOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if err := ed.WaitReleaseBuild(); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	// Focus build-release-version, trigger brRev tick via SetReleaseNotesText, and verify focus survives.
	origVer := ed.Project.AppVersion
	app.Focus("id:build-release-version")
	app.Settle()
	app.Post(func() {
		ed.SetReleaseNotesText("typing release notes ticks brRev")
	})
	app.Settle()
	app.Key("9")
	app.Key("enter")
	app.Settle()
	if ed.Project.AppVersion == origVer || !strings.Contains(ed.Project.AppVersion, "9") {
		t.Fatalf("build-release-version lost focus across brRev tick: AppVersion = %q (orig %q)", ed.Project.AppVersion, origVer)
	}

	// Entering Play mode closes Build & Release screen and stops any running build.
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	if ed.BuildReleaseScreenOpen() {
		t.Fatal("Play mode should close BuildReleaseScreenOpen")
	}
	if err := ed.RunBuildReadinessAction("macos", "any"); err != editor.ErrPlaying {
		t.Fatalf("RunBuildReadinessAction while playing = %v, want ErrPlaying", err)
	}
	app.Post(func() {
		_ = ed.Stop()
	})
	app.Settle()
}
