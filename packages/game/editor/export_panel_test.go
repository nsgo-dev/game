package editor_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/credits"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func buildGameTool(t *testing.T, pkg, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, pkg)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s failed: %v\n%s", pkg, err, out)
	}
	return bin
}

func runBuiltTool(t *testing.T, bin string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v\nstdout:\n%s\nstderr:\n%s", bin, args, err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

// #916 Acceptance 1: adding a preset in the panel, saving, verifying `gameproject targets` prints its targets, and undoing removes the preset.
func TestExportPanelAddPresetSaveGameProjectTargetsAndUndo(t *testing.T) {
	dir := writeEditorProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	if app.Exists("id:export-panel") {
		t.Fatal("export panel should start closed")
	}

	// Open the Export panel from the toolbar.
	app.Press("id:toolbar-export")
	app.Settle()
	if !app.Exists("id:export-panel") {
		t.Fatal("pressing toolbar-export did not open the panel")
	}
	if !app.Exists("id:export-empty-presets") {
		t.Fatal("expected empty presets notice initially")
	}

	// Set export.output to build/release.
	app.Type("id:export-output-input", "build/release")
	app.Press("id:export-output-apply")
	app.Settle()
	if got := ed.ExportOutput(); got != "build/release" {
		t.Fatalf("ExportOutput = %q, want build/release", got)
	}

	// Add macos preset (first available platform) and choose macos_arm64 from its valid targets.
	app.Press("id:export-add-preset")
	app.Settle()
	app.Press("id:export-target-macos-macos_arm64")
	app.Settle()

	// Add windows preset and choose windows_x64.
	app.Press("id:export-add-platform")
	app.Press("role:option windows")
	app.Press("id:export-add-preset")
	app.Settle()
	app.Press("id:export-target-windows-windows_x64")
	app.Settle()

	// Add web preset with default (empty) target list.
	app.Press("id:export-add-platform")
	app.Press("role:option web")
	app.Press("id:export-add-preset")
	app.Settle()

	// Save via toolbar Save.
	app.Press("id:toolbar-save")
	app.Settle()

	saved, err := project.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("project.Check after Save: %v", err)
	}
	if saved.Export == nil || saved.Export.Output != "build/release" {
		t.Fatalf("saved export = %+v", saved.Export)
	}
	wantTargets := []string{"macos_arm64", "windows_x64", "web"}
	if got := saved.BuildTargets(); !reflect.DeepEqual(got, wantTargets) {
		t.Fatalf("saved BuildTargets = %v, want %v", got, wantTargets)
	}

	// Verify `gameproject targets` prints the exact targets.
	gameprojectBin := buildGameTool(t, "../cmd/gameproject", "gameproject")
	stdout, _ := runBuiltTool(t, gameprojectBin, "targets", dir)
	if got, want := strings.TrimSpace(stdout), "macos_arm64,windows_x64,web"; got != want {
		t.Fatalf("gameproject targets = %q, want %q", got, want)
	}

	// Undo every edit (6 steps: set output, add macos, toggle macos_arm64, add windows, toggle windows_x64, add web).
	for range 6 {
		app.Press("id:toolbar-undo")
	}
	app.Settle()
	if ed.Project.Export != nil {
		t.Fatalf("Project.Export after Undo = %+v, want nil", ed.Project.Export)
	}
	if !app.Exists("id:export-empty-presets") {
		t.Fatal("expected empty presets notice after undoing all export edits")
	}

	// Save after Undo and verify `gameproject targets` prints nothing.
	app.Press("id:toolbar-save")
	app.Settle()
	stdout, _ = runBuiltTool(t, gameprojectBin, "targets", dir)
	if got := strings.TrimSpace(stdout); got != "" {
		t.Fatalf("gameproject targets after Undo+Save = %q, want empty", got)
	}

	// Redo brings the presets and output directory back.
	for range 6 {
		app.Press("id:toolbar-redo")
	}
	app.Settle()
	if got := ed.Project.BuildTargets(); !reflect.DeepEqual(got, wantTargets) {
		t.Fatalf("BuildTargets after Redo = %v, want %v", got, wantTargets)
	}
	if got := ed.ExportOutput(); got != "build/release" {
		t.Fatalf("ExportOutput after Redo = %q, want build/release", got)
	}
}

// #916 Acceptance 2: a dry-run build from the editor shows the exact same `nsgo build` command that `go tool game release -dry-run` prints on the same project.
func TestExportPanelDryRunMatchesGameReleaseCLI(t *testing.T) {
	dir := writeEditorProject(t)
	// Add a credited audio asset and a release notes file.
	if err := os.MkdirAll(filepath.Join(dir, "sfx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sfx", "jump.wav"), []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sfx", "jump.wav.import.yaml"), []byte("version: 1\ncredit: {author: Kenney, license: CC0-1.0}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	notesRel := "docs/notes.md"
	notesAbs := filepath.Join(dir, "docs", "notes.md")
	if err := os.WriteFile(notesAbs, []byte("# v1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	gameYAML := projectYAML + `export:
  output: my dist
  presets:
    - platform: macos
      targets: [macos_arm64]
    - platform: web
assets:
  audio:
    - in: sfx/jump.wav
      out: assets/jump.wav
`
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	var runnerCalls int
	ed.ExportCommandRunner = func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		runnerCalls++
		return errors.New("dry run must not execute nsgo build")
	}
	var openedDirs []string
	ed.OpenOutputFolder = func(d string) error {
		openedDirs = append(openedDirs, d)
		return nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	app.Press("id:toolbar-export")
	app.Settle()

	// Configure channel=store, release notes, and Dry Run via the panel controls.
	app.Press("id:export-channel-select")
	app.Press("role:option store")
	app.Type("id:export-notes-input", notesRel)
	app.Press("id:export-dry-run")
	app.Settle()

	// Run dry-run build for all presets.
	app.Press("id:export-build")
	app.WaitUntil(5*time.Second, func() bool { return !ed.ExportRunning() })
	app.Settle()

	if runnerCalls != 0 {
		t.Fatalf("dry-run invoked ExportCommandRunner %d times, want 0", runnerCalls)
	}
	if len(openedDirs) != 0 {
		t.Fatalf("dry-run opened output folder %v, want none", openedDirs)
	}
	if _, err := os.Stat(filepath.Join(dir, "my dist", credits.FileName)); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote CREDITS.md: %v", err)
	}

	gameBin := buildGameTool(t, "../cmd/game", "game")
	cliOut, cliErr := runBuiltTool(t, gameBin, "release", "-dry-run", "-channel", "store", "-release-notes", notesAbs, dir)
	if cliErr != "" {
		t.Fatalf("game release -dry-run stderr = %q", cliErr)
	}
	panelOut := ed.ExportOutputText() + "\n"
	if panelOut != cliOut {
		t.Fatalf("editor dry-run output differs from `game release -dry-run`:\neditor:\n%q\ncli:\n%q", panelOut, cliOut)
	}
	if uiText := app.Find("id:export-output-log").Text + "\n"; uiText != cliOut {
		t.Fatalf("UI output log differs from `game release -dry-run`:\nui:\n%q\ncli:\n%q", uiText, cliOut)
	}

	// Now pick a single chosen target ("web") from the build scope bar and verify parity again.
	app.Press("id:export-scope-target-web")
	app.Settle()
	app.Press("id:export-build")
	app.WaitUntil(5*time.Second, func() bool { return !ed.ExportRunning() })
	app.Settle()

	cliWebOut, cliWebErr := runBuiltTool(t, gameBin, "release", "-dry-run", "-target", "web", "-channel", "store", "-release-notes", notesAbs, dir)
	if cliWebErr != "" {
		t.Fatalf("game release -dry-run -target web stderr = %q", cliWebErr)
	}
	if got := ed.ExportOutputText() + "\n"; got != cliWebOut {
		t.Fatalf("editor chosen-target dry-run differs from CLI:\neditor:\n%q\ncli:\n%q", got, cliWebOut)
	}
}

// #916 Acceptance 3: credits pre-flight catches an asset without a credit before building and clears once credited, then real build streams output, writes CREDITS.md, and opens the output folder.
func TestExportPanelCreditsPreflightBlocksAndClears(t *testing.T) {
	dir := writeEditorProject(t)
	if err := os.MkdirAll(filepath.Join(dir, "sfx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sfx", "jump.wav"), []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}
	gameYAML := projectYAML + `export:
  output: build/release
  presets:
    - platform: web
assets:
  audio:
    - in: sfx/jump.wav
      out: assets/jump.wav
`
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var ranArgs [][]string
	ed.ExportCommandRunner = func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		mu.Lock()
		ranArgs = append(ranArgs, append([]string(nil), args...))
		mu.Unlock()
		fmt.Fprintln(stdout, "compiling web target...")
		fmt.Fprintln(stderr, "bundling assets...")
		return nil
	}
	var openedDirs []string
	ed.OpenOutputFolder = func(d string) error {
		mu.Lock()
		openedDirs = append(openedDirs, d)
		mu.Unlock()
		return nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// Opening the Export panel runs the credits pre-flight automatically.
	app.Press("id:toolbar-export")
	app.Settle()

	report := ed.ExportCredits()
	if !report.Configured || report.OK() || !reflect.DeepEqual(report.Missing, []string{"sfx/jump.wav"}) {
		t.Fatalf("ExportCredits before crediting = %+v, want missing [sfx/jump.wav]", report)
	}
	if !app.Exists("id:export-credits-missing") {
		t.Fatal("expected export-credits-missing warning in the panel")
	}
	if got := app.Find("id:export-credits-missing").Text; !strings.Contains(got, "sfx/jump.wav.import.yaml") {
		t.Fatalf("export-credits-missing = %q, want sidecar path", got)
	}

	// Pressing Build while uncredited must be refused before invoking the runner.
	app.Press("id:export-build")
	app.Settle()

	var missingErr *credits.MissingError
	if !errors.As(ed.ExportLastError(), &missingErr) || !reflect.DeepEqual(missingErr.Assets, []string{"sfx/jump.wav"}) {
		t.Fatalf("ExportLastError = %v, want *credits.MissingError for sfx/jump.wav", ed.ExportLastError())
	}
	mu.Lock()
	if len(ranArgs) != 0 || len(openedDirs) != 0 {
		t.Fatalf("uncredited build ran %v or opened %v", ranArgs, openedDirs)
	}
	mu.Unlock()
	if got := app.Find("id:export-output-log").Text; !strings.Contains(got, "1 asset has no credit") {
		t.Fatalf("output log after blocked build = %q", got)
	}

	// Credit the asset and refresh credits in the panel.
	sidecar := "version: 1\ncredit:\n  author: Kenney\n  license: CC0-1.0\n"
	if err := os.WriteFile(filepath.Join(dir, "sfx", "jump.wav.import.yaml"), []byte(sidecar), 0o644); err != nil {
		t.Fatal(err)
	}
	app.Press("id:export-credits-refresh")
	app.Settle()

	report = ed.ExportCredits()
	if !report.Configured || !report.OK() || len(report.Entries) != 1 {
		t.Fatalf("ExportCredits after crediting = %+v, want 1 credited entry", report)
	}
	if app.Exists("id:export-credits-missing") {
		t.Fatal("export-credits-missing should clear once all assets are credited")
	}
	if got := app.Find("id:export-credits-status").Text; !strings.Contains(got, "Credits OK: 1 asset(s) credited") {
		t.Fatalf("export-credits-status = %q", got)
	}

	// Build now succeeds, streams output, writes CREDITS.md, and opens the output folder.
	app.Press("id:export-build")
	app.WaitUntil(5*time.Second, func() bool { return !ed.ExportRunning() })
	app.Settle()

	if err := ed.ExportLastError(); err != nil {
		t.Fatalf("ExportLastError = %v, want nil", err)
	}
	wantOutDir := filepath.Join(dir, "build", "release")
	wantArgs := []string{"-C", dir, "tool", "nsgo", "build", "-target", "web", "-output", filepath.FromSlash("build/release")}
	mu.Lock()
	if len(ranArgs) != 1 || !reflect.DeepEqual(ranArgs[0], wantArgs) {
		t.Fatalf("runner args = %q, want [%q]", ranArgs, wantArgs)
	}
	if !reflect.DeepEqual(openedDirs, []string{wantOutDir}) {
		t.Fatalf("openedDirs = %v, want [%q]", openedDirs, wantOutDir)
	}
	mu.Unlock()

	creditsPath := filepath.Join(wantOutDir, credits.FileName)
	if data, err := os.ReadFile(creditsPath); err != nil || !strings.Contains(string(data), "Kenney") {
		t.Fatalf("CREDITS.md = %q, err = %v", data, err)
	}
	logText := app.Find("id:export-output-log").Text
	for _, want := range []string{"compiling web target...", "bundling assets...", "wrote the credits of 1 assets to " + creditsPath} {
		if !strings.Contains(logText, want) {
			t.Errorf("output log missing %q:\n%s", want, logText)
		}
	}
}

func TestExportPanelStreamingCancelFailureAndValidation(t *testing.T) {
	dir := writeEditorProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}

	// Operations without an open project fail with ErrNoProject.
	if err := ed.SetExportOutput("dist"); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("SetExportOutput without project = %v, want ErrNoProject", err)
	}
	if err := ed.AddExportPreset("web"); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("AddExportPreset without project = %v, want ErrNoProject", err)
	}
	if err := ed.StartExportBuild(editor.ExportBuildOptions{}); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("StartExportBuild without project = %v, want ErrNoProject", err)
	}

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Invalid outputs, platforms, targets, and channels are refused cleanly without mutating state.
	for _, badOut := range []string{"../dist", "/dist", ".", `a\b`, "C:/dist", "C:dist"} {
		if err := ed.SetExportOutput(badOut); err == nil {
			t.Errorf("SetExportOutput(%q) succeeded, want error", badOut)
		}
	}
	if err := ed.AddExportPreset("dos"); err == nil {
		t.Fatal("AddExportPreset(dos) succeeded, want error")
	}
	if err := ed.AddExportPreset("macos", "windows_x64"); err == nil {
		t.Fatal("AddExportPreset(macos, windows_x64) succeeded, want error")
	}
	if err := ed.AddExportPreset("macos", "macos_arm64"); err != nil {
		t.Fatalf("AddExportPreset(macos, macos_arm64): %v", err)
	}
	if err := ed.AddExportPreset("macos"); err == nil {
		t.Fatal("duplicate AddExportPreset(macos) succeeded, want error")
	}
	if err := ed.ToggleExportPresetTarget("macos", "linux_x64"); err == nil {
		t.Fatal("ToggleExportPresetTarget(macos, linux_x64) succeeded, want error")
	}
	if err := ed.SetExportChannel("beta"); err == nil {
		t.Fatal("SetExportChannel(beta) succeeded, want error")
	}
	if err := ed.AddExportPreset("linux"); err != nil {
		t.Fatal(err)
	}
	if err := ed.MoveExportPreset("linux", -1); err != nil {
		t.Fatal(err)
	}
	if presets := ed.ExportPresets(); len(presets) != 2 || presets[0].Platform != "linux" || presets[1].Platform != "macos" {
		t.Fatalf("presets after MoveExportPreset = %+v", presets)
	}

	// Streaming & cancellation off the UI goroutine.
	lineArrived := make(chan struct{})
	gate := make(chan struct{})
	var openedDirs []string
	var mu sync.Mutex
	ed.OpenOutputFolder = func(d string) error {
		mu.Lock()
		openedDirs = append(openedDirs, d)
		mu.Unlock()
		return nil
	}
	ed.ExportCommandRunner = func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		fmt.Fprintln(stdout, "step 1: preparing build")
		close(lineArrived)
		select {
		case <-gate:
			return errors.New("exit status 1")
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	app.Press("id:toolbar-export")
	app.Settle()

	app.Press("id:export-build")
	<-lineArrived
	app.WaitUntil(5*time.Second, func() bool {
		return strings.Contains(ed.ExportOutputText(), "step 1: preparing build")
	})
	if !ed.ExportRunning() {
		t.Fatal("expected build to still be running while waiting on gate")
	}
	if err := ed.StartExportBuild(editor.ExportBuildOptions{}); !errors.Is(err, editor.ErrExportRunning) {
		t.Fatalf("concurrent StartExportBuild = %v, want ErrExportRunning", err)
	}

	// Cancel via the Cancel button (export-cancel while running).
	app.Press("id:export-cancel")
	app.WaitUntil(5*time.Second, func() bool { return !ed.ExportRunning() })
	if !errors.Is(ed.ExportLastError(), context.Canceled) {
		t.Fatalf("ExportLastError after cancel = %v, want context.Canceled", ed.ExportLastError())
	}
	mu.Lock()
	if len(openedDirs) != 0 {
		t.Fatalf("cancelled build opened %v, want none", openedDirs)
	}
	mu.Unlock()

	// Failed build reports error and does not open output folder.
	ed.ExportCommandRunner = func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		fmt.Fprintln(stderr, "linker error")
		return errors.New("exit status 1")
	}
	app.Press("id:export-build")
	app.WaitUntil(5*time.Second, func() bool { return !ed.ExportRunning() })
	if ed.ExportLastError() == nil || !strings.Contains(ed.ExportOutputText(), "linker error") {
		t.Fatalf("failed build err=%v output=%q", ed.ExportLastError(), ed.ExportOutputText())
	}
	mu.Lock()
	if len(openedDirs) != 0 {
		t.Fatalf("failed build opened %v, want none", openedDirs)
	}
	mu.Unlock()

	// Pressing Open Output explicitly invokes OpenOutputFolder with default dist directory.
	app.Press("id:export-open-output")
	app.Settle()
	mu.Lock()
	wantDefault := filepath.Join(dir, "dist")
	if !reflect.DeepEqual(openedDirs, []string{wantDefault}) {
		t.Fatalf("Open Output opened %v, want [%q]", openedDirs, wantDefault)
	}
	mu.Unlock()
}
