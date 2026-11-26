package editor_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/hotreload"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const fixtureGameDevToolSource = `package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "child-sleep" {
		if len(os.Args) > 2 {
			_ = os.WriteFile(os.Args[2], []byte(strconv.Itoa(os.Getpid())), 0o644)
		}
		for {
			time.Sleep(time.Second)
		}
	}
	if len(os.Args) < 2 || os.Args[1] != "dev" {
		fmt.Fprintf(os.Stderr, "unexpected command: %v\n", os.Args[1:])
		os.Exit(2)
	}
	fs := flag.NewFlagSet("dev", flag.ExitOnError)
	scene := fs.String("scene", "", "start scene")
	_ = fs.Parse(os.Args[2:])

	wd, _ := os.Getwd()
	_ = os.WriteFile(filepath.Join(wd, "parent.pid"), []byte(strconv.Itoa(os.Getpid())), 0o644)

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "executable: %v\n", err)
		os.Exit(1)
	}
	child := exec.Command(exe, "child-sleep", filepath.Join(wd, "grandchild.pid"))
	if err := child.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "spawn child: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("fixture game dev running scene=%s env=%s\n", *scene, os.Getenv("NSGO_GAME_SCENE"))
	fmt.Fprintf(os.Stderr, "scenes/other.scene.yaml:3: runtime notice from game\n")

	for {
		time.Sleep(time.Second)
	}
}
`

func writeRunnableFixtureProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		"go.mod":             "module example.com/runfixture\n\ngo 1.24\n\ntool example.com/runfixture/cmd/game\n",
		"cmd/game/main.go":   fixtureGameDevToolSource,
		project.FileName:     fixtureProjectYAML,
		project.NSGoFileName: fixtureNSGoYAML,
		"art/actors.json":    fixtureAtlasJSON,
		"scenes/room.scene.yaml": `version: 2
name: Room
layers:
  - name: actors
    objects:
      - id: hero
        sprite:
          atlas: actors
          frame: hero
`,
		"scenes/other.scene.yaml": fixtureCleanOtherScene,
	}, map[string][]byte{
		"art/actors.png": encodeTestPNG(t, 32, 16),
	})
	return dir
}

func readPIDFile(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func waitForCondition(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, desc)
}

// #910 Acceptance 1: runs a fixture project's `go tool game dev` through the editor,
// streams its stdout and stderr live into the Output panel, opens a scene from a
// file:line log line, and stops it leaving zero processes (including grandchildren) behind.
func TestRunFixtureGameDevStreamsOutputAndStopsCleanly(t *testing.T) {
	if runtime.GOOS == "android" || runtime.GOOS == "ios" || runtime.GOOS == "js" || runtime.GOOS == "wasip1" {
		t.Skip("desktop subprocess test")
	}

	dir := writeRunnableFixtureProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ed.Close()
	})
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	if !app.Exists("id:toolbar-play") {
		t.Fatal("expected existing toolbar-play button to remain untouched")
	}
	// Output panel starts collapsed so it does not crowd other docked panels, and opens via the status bar Output button or Run.
	if app.Exists("id:output-panel") {
		t.Fatal("expected Output panel collapsed before toggling Output or running the game")
	}
	app.Press("id:status-output")
	app.Settle()
	if !app.Exists("id:output-panel") {
		t.Fatal("expected Output panel docked below the viewport after toggling Output")
	}

	// Launch real `go tool game dev` via CmdRunGame.
	runEditorCommand(t, app, ed, editor.CmdRunGame)

	if !ed.GameRunning() {
		t.Fatalf("expected game to be running; output=%+v", ed.OutputEntries())
	}

	// Wait for streamed stdout, stderr, and both parent + grandchild PID files.
	var parentPID, childPID int
	waitForCondition(t, 20*time.Second, "streamed output and grandchild process", func() bool {
		app.Settle()
		var hasStdout, hasStderr bool
		for _, entry := range ed.OutputEntries() {
			if entry.Source == editor.OutputStdout && strings.Contains(entry.Text, "fixture game dev running") {
				hasStdout = true
			}
			if entry.Source == editor.OutputStderr && strings.Contains(entry.Text, "scenes/other.scene.yaml:3:") {
				hasStderr = true
			}
		}
		p, okP := readPIDFile(filepath.Join(dir, "parent.pid"))
		c, okC := readPIDFile(filepath.Join(dir, "grandchild.pid"))
		if okP && okC {
			parentPID, childPID = p, c
		}
		return hasStdout && hasStderr && okP && okC
	})

	if !testProcessAlive(parentPID) {
		t.Fatalf("expected parent pid %d to be alive while running", parentPID)
	}
	if !testProcessAlive(childPID) {
		t.Fatalf("expected grandchild pid %d to be alive while running", childPID)
	}

	// Output panel UI must display both the stdout and stderr lines.
	panelText := allText(app.Find("id:output-panel"))
	if !strings.Contains(panelText, "fixture game dev running") {
		t.Fatalf("output panel missing stdout line:\n%s", panelText)
	}
	if !strings.Contains(panelText, "scenes/other.scene.yaml:3: runtime notice from game") {
		t.Fatalf("output panel missing stderr line:\n%s", panelText)
	}

	// Stop the game via CmdStopGame.
	runEditorCommand(t, app, ed, editor.CmdStopGame)

	if ed.GameRunning() {
		t.Fatal("expected GameRunning() == false after stopping game")
	}

	// Verify neither the `game dev` process nor its grandchild process survived.
	waitForCondition(t, 3*time.Second, "parent and grandchild processes to exit", func() bool {
		return !testProcessAlive(parentPID) && !testProcessAlive(childPID)
	})

	// Clicking the stderr `file:line` row in the Output panel opens scenes/other.scene.yaml.
	entries := ed.FilteredOutputEntries()
	targetIdx := -1
	for i, entry := range entries {
		if entry.File == "scenes/other.scene.yaml" && entry.Line == 3 {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		t.Fatalf("expected parsed file:line entry for scenes/other.scene.yaml:3 in %+v", entries)
	}
	app.Press(fmt.Sprintf("id:output-row-%d", targetIdx))
	app.Settle()

	if ed.Doc == nil || ed.Doc.Name != "Other" {
		t.Fatalf("clicking scene log line did not open Other scene: doc=%+v status=%q", ed.Doc, ed.Status)
	}
	if ed.ScenePath != "scenes/other.scene.yaml" {
		t.Fatalf("ScenePath = %q, want scenes/other.scene.yaml", ed.ScenePath)
	}
}

type fakeGameProcess struct {
	mu       sync.Mutex
	stopped  bool
	done     chan struct{}
	waitErr  error
	stopOnce sync.Once
}

func newFakeGameProcess() *fakeGameProcess {
	return &fakeGameProcess{done: make(chan struct{})}
}

func (f *fakeGameProcess) Wait() error {
	<-f.done
	return f.waitErr
}

func (f *fakeGameProcess) Stop() error {
	f.stopOnce.Do(func() {
		f.mu.Lock()
		f.stopped = true
		f.mu.Unlock()
		close(f.done)
	})
	return nil
}

func (f *fakeGameProcess) Finish(err error) {
	f.stopOnce.Do(func() {
		f.waitErr = err
		close(f.done)
	})
}

func (f *fakeGameProcess) Stopped() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopped
}

func TestRunGameShortcutF5AndStartAtOpenScene(t *testing.T) {
	dir := writeRunnableFixtureProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	var mu sync.Mutex
	var specs []editor.RunGameSpec
	var procs []*fakeGameProcess
	ed.RunGameCommand = func(spec editor.RunGameSpec, stdout, stderr io.Writer) (editor.GameProcess, error) {
		proc := newFakeGameProcess()
		mu.Lock()
		specs = append(specs, spec)
		procs = append(procs, proc)
		mu.Unlock()
		_, _ = io.WriteString(stdout, "hello from fake game\n")
		return proc, nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// F5 starts the game without -scene by default.
	app.Key("f5")
	app.Settle()

	if !ed.GameRunning() {
		t.Fatal("expected F5 to start the game")
	}
	mu.Lock()
	if len(specs) != 1 {
		mu.Unlock()
		t.Fatalf("launched %d runs, want 1", len(specs))
	}
	firstSpec := specs[0]
	firstProc := procs[0]
	mu.Unlock()

	if firstSpec.Dir != dir {
		t.Fatalf("spec.Dir = %q, want %q", firstSpec.Dir, dir)
	}
	if firstSpec.Name != "go" || !slices.Equal(firstSpec.Args, []string{"tool", "game", "dev"}) {
		t.Fatalf("spec command = %s %v, want go [tool game dev]", firstSpec.Name, firstSpec.Args)
	}
	if firstSpec.Scene != "" {
		t.Fatalf("spec.Scene = %q, want empty", firstSpec.Scene)
	}

	// Shift+F5 stops the game.
	app.Key("shift+f5")
	app.Settle()
	if ed.GameRunning() {
		t.Fatal("expected Shift+F5 to stop the game")
	}
	if !firstProc.Stopped() {
		t.Fatal("expected fake game process Stop() to be called")
	}

	// Open scenes/other.scene.yaml, enable Current Scene toggle, and press F5.
	if err := ed.OpenFile(filepath.Join(dir, "scenes", "other.scene.yaml")); err != nil {
		t.Fatal(err)
	}
	runEditorCommand(t, app, ed, editor.CmdRunOpenScene)
	if !ed.RunStartAtOpenScene() {
		t.Fatal("expected CmdRunOpenScene to enable RunStartAtOpenScene")
	}
	app.Key("f5")
	app.Settle()

	mu.Lock()
	if len(specs) != 2 {
		mu.Unlock()
		t.Fatalf("launched %d runs, want 2", len(specs))
	}
	secondSpec := specs[1]
	mu.Unlock()

	wantArgs := []string{"tool", "game", "dev", "-scene", "scenes/other.scene.yaml"}
	if !slices.Equal(secondSpec.Args, wantArgs) {
		t.Fatalf("second spec args = %v, want %v", secondSpec.Args, wantArgs)
	}
	if secondSpec.Scene != "scenes/other.scene.yaml" {
		t.Fatalf("second spec scene = %q", secondSpec.Scene)
	}
	wantEnv := hotreload.SceneEnv + "=scenes/other.scene.yaml"
	if !slices.Contains(secondSpec.Env, wantEnv) {
		t.Fatalf("second spec env missing %q", wantEnv)
	}

	if err := ed.StopGame(); err != nil {
		t.Fatal(err)
	}

	// In-editor Play preview still works independently.
	app.Press("id:toolbar-play")
	app.Settle()
	if !ed.Playing() {
		t.Fatal("expected toolbar-play to start in-editor Play preview")
	}
	app.Press("id:toolbar-play")
	app.Settle()
	if ed.Playing() {
		t.Fatal("expected toolbar-play to stop in-editor Play preview")
	}
}

// #910 Acceptance 2: two editor errors in a row both stay in the Output panel with
// timestamps; Clear and Errors-only filter work; clicking a file:line scene line
// opens that scene, and a non-scene file selects it in the file browser.
func TestOutputPanelKeepsTwoEditorErrorsFilterClearAndFileLineNav(t *testing.T) {
	dir := writeRunnableFixtureProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	ed.ClearOutput()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// Record one info status followed by two editor errors in a row (including two distinct errors).
	ed.SetTool(editor.ToolMove)
	ed.Report(errors.New("scenes/other.scene.yaml:4: first editor error"))
	ed.Report(errors.New("game.yaml:2: second editor error"))
	app.Settle()

	all := ed.OutputEntries()
	if len(all) != 3 {
		t.Fatalf("OutputEntries count = %d, want 3: %+v", len(all), all)
	}
	for i, entry := range all {
		if entry.Time.IsZero() {
			t.Fatalf("entry %d has zero timestamp", i)
		}
		if entry.Source != editor.OutputEditor {
			t.Fatalf("entry %d source = %v, want OutputEditor", i, entry.Source)
		}
	}
	if all[0].Level != editor.OutputInfo {
		t.Fatalf("entry 0 level = %v, want OutputInfo", all[0].Level)
	}
	if all[1].Level != editor.OutputError || !strings.Contains(all[1].Text, "first editor error") {
		t.Fatalf("entry 1 = %+v, want first editor error", all[1])
	}
	if all[2].Level != editor.OutputError || !strings.Contains(all[2].Text, "second editor error") {
		t.Fatalf("entry 2 = %+v, want second editor error", all[2])
	}

	// Even a repeated identical error in a row must stay as a distinct entry.
	ed.Report(errors.New("game.yaml:2: second editor error"))
	app.Settle()
	if got := len(ed.OutputEntries()); got != 4 {
		t.Fatalf("OutputEntries after repeated identical error = %d, want 4", got)
	}

	// Open the Output panel via the status bar toggle; errors recorded while closed must be present.
	app.Press("id:status-output")
	app.Settle()
	panelText := allText(app.Find("id:output-panel"))
	if !strings.Contains(panelText, "first editor error") || !strings.Contains(panelText, "second editor error") {
		t.Fatalf("output panel missing errors:\n%s", panelText)
	}

	// Toggle Errors-only filter: info line disappears, error lines remain.
	app.Press("id:output-filter-errors")
	app.Settle()
	if !ed.OutputErrorsOnly() {
		t.Fatal("expected OutputErrorsOnly() == true")
	}
	filtered := ed.FilteredOutputEntries()
	if len(filtered) != 3 {
		t.Fatalf("FilteredOutputEntries count = %d, want 3 errors: %+v", len(filtered), filtered)
	}
	panelText = allText(app.Find("id:output-panel"))
	if strings.Contains(panelText, "Tool: Move") {
		t.Fatalf("info line should be hidden when Errors filter is on:\n%s", panelText)
	}
	if !strings.Contains(panelText, "first editor error") || !strings.Contains(panelText, "second editor error") {
		t.Fatalf("error lines missing when Errors filter is on:\n%s", panelText)
	}

	// Press filtered row 0 (scenes/other.scene.yaml:4) -> opens Other scene.
	app.Press("id:output-row-0")
	app.Settle()
	if ed.Doc == nil || ed.Doc.Name != "Other" {
		t.Fatalf("expected clicking scene error row to open Other scene, got %+v", ed.Doc)
	}

	// Press filtered row 1 (game.yaml:2) -> selects game.yaml in file browser.
	app.Press("id:output-row-1")
	app.Settle()
	if ed.SelectedFile != "game.yaml" {
		t.Fatalf("SelectedFile = %q, want game.yaml", ed.SelectedFile)
	}

	// Clear removes all entries.
	app.Press("id:output-clear")
	app.Settle()
	if len(ed.OutputEntries()) != 0 {
		t.Fatalf("expected empty OutputEntries after Clear, got %+v", ed.OutputEntries())
	}
	if !app.Exists("id:output-empty") {
		t.Fatal("expected output-empty text after Clear")
	}
}

func TestRunGameErrorsAndLifecycleCleanup(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.RunGame(); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("RunGame without project = %v, want ErrNoProject", err)
	}

	dir := writeRunnableFixtureProject(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	var current *fakeGameProcess
	ed.RunGameCommand = func(spec editor.RunGameSpec, stdout, stderr io.Writer) (editor.GameProcess, error) {
		current = newFakeGameProcess()
		return current, nil
	}

	if err := ed.RunGame(); err != nil {
		t.Fatalf("RunGame failed: %v", err)
	}
	if err := ed.RunGame(); !errors.Is(err, editor.ErrGameRunning) {
		t.Fatalf("second RunGame = %v, want ErrGameRunning", err)
	}

	// Close() stops the running game.
	if err := ed.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if ed.GameRunning() || !current.Stopped() {
		t.Fatal("expected Close() to stop running game")
	}

	// Natural non-zero exit records an error entry and clears GameRunning().
	ed.ClearOutput()
	if err := ed.RunGame(); err != nil {
		t.Fatalf("RunGame failed: %v", err)
	}
	current.Finish(errors.New("exit status 2"))
	if err := ed.WaitGame(); err == nil || !strings.Contains(err.Error(), "exit status 2") {
		t.Fatalf("WaitGame = %v, want exit status 2", err)
	}
	if ed.GameRunning() {
		t.Fatal("expected GameRunning() == false after exit")
	}
	hasExitErr := false
	for _, entry := range ed.OutputEntries() {
		if entry.Level == editor.OutputError && strings.Contains(entry.Text, "exit status 2") {
			hasExitErr = true
		}
	}
	if !hasExitErr {
		t.Fatalf("expected error output entry for non-zero exit, got %+v", ed.OutputEntries())
	}

	// Re-opening a project stops any running game.
	if err := ed.RunGame(); err != nil {
		t.Fatalf("RunGame failed: %v", err)
	}
	prev := current
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("re-OpenProject failed: %v", err)
	}
	ed.WaitForProblems()
	if !prev.Stopped() || ed.GameRunning() {
		t.Fatal("expected OpenProject to stop previously running game")
	}
}
