package editor_test

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/internal/releaseplan"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/game/packages/game/tooling/credits"
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
	gameBin := buildGameTool(t, "../cmd/game", "game")
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
// writes PrivacyInfo.xcprivacy on agent action, and delegates Apple sign-in to person action callback.
func TestBuildReleaseScreenIOSReadinessAgentVsPerson(t *testing.T) {
	dir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: ios
    - platform: web
`)
	writeSolidPNG(t, filepath.Join(dir, "art", "icon.png"), 64, 64)
	if err := os.MkdirAll(filepath.Join(dir, "input"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "input", "touch.input.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

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

	// Verify agent-handleable privacy manifest vs person-required provisioning profile.
	var privacyItem, profileItem editor.BuildReadinessItem
	for _, it := range ios.Readiness {
		switch it.ID {
		case "privacy_manifest":
			privacyItem = it
		case "provisioning_profile":
			profileItem = it
		}
	}
	if !privacyItem.AgentCanHandle || privacyItem.RequiresPerson {
		t.Fatalf("privacy_manifest must be AgentCanHandle=true, RequiresPerson=false: %+v", privacyItem)
	}
	if !profileItem.RequiresPerson || profileItem.AgentCanHandle {
		t.Fatalf("provisioning_profile must be RequiresPerson=true, AgentCanHandle=false: %+v", profileItem)
	}

	// Click "Let the agent write it" -> actually writes PrivacyInfo.xcprivacy and turns item OK.
	app.Press("id:build-readiness-action-privacy_manifest")
	app.Settle()
	if _, err := os.Stat(filepath.Join(dir, "PrivacyInfo.xcprivacy")); err != nil {
		t.Fatalf("expected PrivacyInfo.xcprivacy to be written: %v", err)
	}
	snap = ed.BuildReleaseSnapshot()
	for _, it := range snap.SelectedPreset().Readiness {
		if it.ID == "privacy_manifest" && it.Status != editor.BuildReadinessOK {
			t.Fatalf("privacy_manifest status after agent action = %q, want ok", it.Status)
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
