package editor_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestRecentProjectsPersistenceMissingAndRemoval(t *testing.T) {
	recentFile := filepath.Join(t.TempDir(), "config", "recent-projects.yaml")
	projA := writeEditorProject(t)
	projB := writeBareProject(t, "Sky Drift")

	clock := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.Now = func() time.Time { return clock }
	if err := ed.SetRecentProjectsPath(recentFile); err != nil {
		t.Fatalf("SetRecentProjectsPath: %v", err)
	}
	if got := ed.RecentProjects(); len(got) != 0 {
		t.Fatalf("initial RecentProjects = %+v, want empty", got)
	}

	if err := ed.OpenProject(projA); err != nil {
		t.Fatalf("OpenProject(projA): %v", err)
	}
	clock = clock.Add(2 * time.Hour)
	if err := ed.OpenProject(projB); err != nil {
		t.Fatalf("OpenProject(projB): %v", err)
	}

	// Verify persisted strict YAML on disk round-trips cleanly.
	raw, err := os.ReadFile(recentFile)
	if err != nil {
		t.Fatalf("read recent-projects.yaml: %v", err)
	}
	parsed, err := editor.ParseRecentProjects(raw)
	if err != nil {
		t.Fatalf("ParseRecentProjects: %v\n%s", err, raw)
	}
	if len(parsed.Projects) != 2 || parsed.Projects[0].Name != "Sky Drift" || parsed.Projects[1].Name != "Demo" {
		t.Fatalf("persisted projects = %+v, want [Sky Drift, Demo]", parsed.Projects)
	}

	// Reopening projA moves it to the top without duplicating.
	clock = clock.Add(30 * time.Minute)
	if err := ed.OpenProject(projA); err != nil {
		t.Fatalf("re-OpenProject(projA): %v", err)
	}

	// Simulate an editor restart loading the same per-user file.
	restarted, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.SetRecentProjectsPath(recentFile); err != nil {
		t.Fatalf("restarted.SetRecentProjectsPath: %v", err)
	}
	entries := restarted.RecentProjects()
	if len(entries) != 2 {
		t.Fatalf("restarted RecentProjects len = %d, want 2: %+v", len(entries), entries)
	}
	if entries[0].Name != "Demo" || entries[0].Path != filepath.Clean(projA) || entries[0].Missing {
		t.Fatalf("entries[0] = %+v, want Demo at %s (present)", entries[0], projA)
	}
	if entries[1].Name != "Sky Drift" || entries[1].Path != filepath.Clean(projB) || entries[1].Missing {
		t.Fatalf("entries[1] = %+v, want Sky Drift at %s (present)", entries[1], projB)
	}

	// Removing projB on disk marks it Missing.
	if err := os.RemoveAll(projB); err != nil {
		t.Fatal(err)
	}
	entries = restarted.RecentProjects()
	if entries[0].Missing || !entries[1].Missing {
		t.Fatalf("after removing projB: missing = [%v, %v], want [false, true]", entries[0].Missing, entries[1].Missing)
	}

	// RemoveRecentProject deletes the missing entry and persists.
	if err := restarted.RemoveRecentProject(projB); err != nil {
		t.Fatalf("RemoveRecentProject: %v", err)
	}
	reloaded, err := editor.LoadRecentProjectsFile(recentFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Projects) != 1 || reloaded.Projects[0].Path != filepath.Clean(projA) {
		t.Fatalf("after removal reloaded = %+v", reloaded.Projects)
	}
}

func TestStartScreenUIRecentVisibilityTemplatesFiltersAndValidation(t *testing.T) {
	parentDir := t.TempDir()
	existingDir := filepath.Join(parentDir, "dungeon-run")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existingYAML := "version: 1\nname: Dungeon Run\nid: dev.example.dungeonrun\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n"
	if err := os.WriteFile(filepath.Join(existingDir, project.FileName), []byte(existingYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	ed.SetProjectTemplates([]editor.ProjectTemplate{
		{Name: "starter", Title: "Starter", Summary: "An empty scene and a loop", Dimension: "2d", Default: true},
		{Name: "platformer2d", Title: "2D platformer", Summary: "Rooms, checkpoints, coyote time", Dimension: "2d"},
		{Name: "racing3d", Title: "3D racer", Summary: "Laps, ghosts, AI drivers", Dimension: "3d"},
	})
	ed.SetStartProjectParentDir(parentDir)
	ed.SetStartProjectName("my-game")
	ed.SetStartScreenOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	if !app.Exists("id:editor-start-screen") {
		t.Fatal("Start Screen did not render")
	}
	// RECENT section is hidden when there are no recent projects.
	if app.Exists("id:start-recent-section") {
		t.Fatal("RECENT section rendered with no recent projects")
	}
	if !app.Exists("id:start-template-starter") || !app.Exists("id:start-template-platformer2d") || !app.Exists("id:start-template-racing3d") {
		t.Fatal("all template cards should appear under All filter")
	}

	// Filter to 3D -> only racing3d shows.
	app.Press("id:start-filter-3d")
	if app.Exists("id:start-template-starter") || !app.Exists("id:start-template-racing3d") {
		t.Fatal("3D filter did not isolate 3D templates")
	}
	app.Press("id:start-template-racing3d")
	if ed.SelectedProjectTemplate() != "racing3d" {
		t.Fatalf("SelectedProjectTemplate = %q, want racing3d", ed.SelectedProjectTemplate())
	}

	// Back to All -> starter reappears.
	app.Press("id:start-filter-all")
	if !app.Exists("id:start-template-starter") {
		t.Fatal("All filter did not restore starter card")
	}

	// Invalid characters disable Create.
	app.Post(func() { ed.SetStartProjectName("Bad Name!") })
	app.Settle()
	if st := ed.StartNameStatus(); st.OK || !st.InvalidChars {
		t.Fatalf("StartNameStatus(Bad Name!) = %+v, want InvalidChars=true", st)
	}
	if app.Enabled("id:start-create") {
		t.Fatal("Create button should be disabled for invalid characters")
	}

	// Existing folder shows "Open it" and "Use dungeon-run-2".
	app.Post(func() { ed.SetStartProjectName("dungeon-run") })
	app.Settle()
	if st := ed.StartNameStatus(); st.OK || !st.Exists || st.SuggestedName != "dungeon-run-2" {
		t.Fatalf("StartNameStatus(dungeon-run) = %+v, want Exists=true SuggestedName=dungeon-run-2", st)
	}
	if !app.Exists("id:start-name-open-existing") || !app.Exists("id:start-name-use-suggestion") {
		t.Fatal("existing folder actions (Open it / Use dungeon-run-2) did not appear")
	}
	if app.Enabled("id:start-create") {
		t.Fatal("Create button should be disabled when target folder exists")
	}

	// Pressing "Use dungeon-run-2" switches to a valid free name and enables Create.
	app.Press("id:start-name-use-suggestion")
	if ed.StartProjectName() != "dungeon-run-2" || !ed.StartNameStatus().OK {
		t.Fatalf("after Use suggestion: name=%q status=%+v", ed.StartProjectName(), ed.StartNameStatus())
	}
	if !app.Enabled("id:start-create") {
		t.Fatal("Create button should be enabled for valid free name")
	}

	// Read-only parent directory check (Unix non-root).
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		roDir := filepath.Join(t.TempDir(), "readonly")
		if err := os.MkdirAll(roDir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })
		st := editor.ValidateNewProjectInputs("fresh-game", roDir, "")
		if st.OK || !st.NotWritable {
			t.Fatalf("ValidateNewProjectInputs on read-only dir = %+v, want NotWritable=true", st)
		}
	}

	// Pressing "Open it" on the existing folder opens its game.yaml and leaves Start Screen.
	app.Post(func() { ed.SetStartProjectName("dungeon-run") })
	app.Settle()
	app.Press("id:start-name-open-existing")
	if ed.StartScreenOpen() {
		t.Fatal("Start Screen remained open after Open it")
	}
	if ed.Project == nil || ed.Project.Name != "Dungeon Run" {
		t.Fatalf("Project = %+v, want Dungeon Run", ed.Project)
	}

	// Reopening Start Screen now shows RECENT and allows Open Demo.
	app.Post(func() { ed.SetStartScreenOpen(true) })
	app.Settle()
	if !app.Exists("id:start-recent-section") || !app.Exists("id:start-recent-0") {
		t.Fatal("RECENT section did not appear after opening a project")
	}
	app.Press("id:start-open-demo")
	if ed.StartScreenOpen() || ed.Project != nil || ed.Doc.Name != "Demo Room" {
		t.Fatalf("after Open demo: startOpen=%v project=%+v scene=%q", ed.StartScreenOpen(), ed.Project, ed.Doc.Name)
	}
}

func TestStartScreenFolderPickerAndMissingRecentUI(t *testing.T) {
	projDir := writeBareProject(t, "Demo")
	recentFile := filepath.Join(t.TempDir(), "recent.yaml")

	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetRecentProjectsPath(recentFile); err != nil {
		t.Fatal(err)
	}
	ed.SetStartScreenOpen(true)

	pickedParent := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(pickedParent, 0o755); err != nil {
		t.Fatal(err)
	}

	var lastTitle string
	ed.PickProjectFolder = func(title, _ string, done func(string, bool, error)) {
		lastTitle = title
		if strings.Contains(title, "Projects Folder") {
			done(pickedParent, true, nil)
			return
		}
		done(projDir, true, nil)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	// Pick parent folder for New Project.
	app.Press("id:start-new-folder-browse")
	if ed.StartProjectParentDir() != pickedParent {
		t.Fatalf("StartProjectParentDir = %q, want %q (title=%q)", ed.StartProjectParentDir(), pickedParent, lastTitle)
	}

	// Open folder via injectable picker hook.
	app.Press("id:start-open-folder")
	if ed.StartScreenOpen() || ed.Project == nil || ed.Project.Name != "Demo" {
		t.Fatalf("after Open folder: startOpen=%v project=%+v", ed.StartScreenOpen(), ed.Project)
	}

	// Remove the project on disk, return to Start Screen, verify Missing badge and remove button.
	if err := os.RemoveAll(projDir); err != nil {
		t.Fatal(err)
	}
	app.Post(func() { ed.SetStartScreenOpen(true) })
	app.Settle()
	if !app.Exists("id:start-recent-missing-0") || !app.Exists("id:start-recent-remove-0") {
		t.Fatal("missing recent project badge or remove button did not render")
	}
	app.Press("id:start-recent-0")
	if !ed.StartScreenOpen() || !strings.Contains(ed.Status, "missing") {
		t.Fatalf("pressing missing recent project: startOpen=%v status=%q", ed.StartScreenOpen(), ed.Status)
	}
	app.Press("id:start-recent-remove-0")
	if len(ed.RecentProjects()) != 0 || app.Exists("id:start-recent-section") {
		t.Fatalf("after removing last recent project: count=%d sectionExists=%v", len(ed.RecentProjects()), app.Exists("id:start-recent-section"))
	}
}

func TestStartScreenFolderPickerFallsBackToNSGoPickDirectories(t *testing.T) {
	projDir := writeBareProject(t, "Demo")
	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	ed.SetStartScreenOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))
	app.PickDirectories = func(nsgotest.DirectoryPick) []core.Directory {
		return []core.Directory{{Name: filepath.Base(projDir), Path: projDir}}
	}

	app.Press("id:start-open-folder")
	if len(app.DirectoryPicks) != 1 {
		t.Fatalf("DirectoryPicks = %d, want 1", len(app.DirectoryPicks))
	}
	if ed.StartScreenOpen() || ed.Project == nil || ed.Project.Name != "Demo" {
		t.Fatalf("after PickDirectories: startOpen=%v project=%+v", ed.StartScreenOpen(), ed.Project)
	}
}

func TestProjectSwitchingUnsavedEditsPromptFromFileMenuAndStartScreen(t *testing.T) {
	projA := writeEditorProject(t)
	projB := writeBareProject(t, "Project B")

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(projA); err != nil {
		t.Fatal(err)
	}

	ed.PickProjectFolder = func(_, _ string, done func(string, bool, error)) {
		done(projB, true, nil)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// Edit both project metadata and open scene so HasUnsavedChanges is true.
	app.Post(func() {
		if err := ed.EditProject("rename A", func(f *project.File) { f.Name = "Demo Saved" }); err != nil {
			t.Error(err)
		}
		if err := ed.MoveObject("hero", 99, 77); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if !ed.HasUnsavedChanges() {
		t.Fatal("expected HasUnsavedChanges after editing project and scene")
	}

	// File > Open Project... prompts instead of switching immediately.
	app.ChooseMenu("File", "Open Project...")
	if !ed.UnsavedPromptOpen() || !app.Exists("id:unsaved-dialog") {
		t.Fatal("unsaved edits confirmation dialog did not open")
	}
	if ed.Project.Name != "Demo Saved" {
		t.Fatalf("project switched before confirmation: %q", ed.Project.Name)
	}

	// Cancel keeps project A and its unsaved edits intact.
	app.Press("id:unsaved-cancel")
	if ed.UnsavedPromptOpen() || ed.Project.Name != "Demo Saved" || !ed.HasUnsavedChanges() {
		t.Fatalf("after Cancel: prompt=%v project=%q dirty=%v", ed.UnsavedPromptOpen(), ed.Project.Name, ed.HasUnsavedChanges())
	}

	// Open Project... again and choose Save & Switch.
	app.ChooseMenu("File", "Open Project...")
	app.Press("id:unsaved-save")
	if ed.UnsavedPromptOpen() {
		t.Fatal("prompt still open after Save & Switch")
	}
	if ed.Project == nil || ed.Project.Name != "Project B" {
		t.Fatalf("after Save & Switch: project=%+v, want Project B", ed.Project)
	}
	if ed.HasUnsavedChanges() || ed.ScenePath != "" {
		t.Fatalf("after switching to bare Project B: dirty=%v scenePath=%q", ed.HasUnsavedChanges(), ed.ScenePath)
	}
	// Verify project A's edits were written to disk before switching.
	savedA, err := project.Check(os.DirFS(projA))
	if err != nil || savedA.Name != "Demo Saved" {
		t.Fatalf("project A on disk after Save & Switch: %+v, %v", savedA, err)
	}

	// Now edit Project B, go to Start Screen via File menu, click Project A in Recent, and Discard.
	app.Post(func() {
		if err := ed.EditProject("rename B", func(f *project.File) { f.Name = "Project B Unsaved" }); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	app.ChooseMenu("File", "Start Screen")
	if !ed.StartScreenOpen() || !app.Exists("id:editor-start-screen") {
		t.Fatal("File > Start Screen did not open Start Screen")
	}
	// Recent[0] is Project B, Recent[1] is Demo Saved (projA).
	app.Press("id:start-recent-1")
	if !ed.UnsavedPromptOpen() {
		t.Fatal("switching from Start Screen with unsaved edits did not prompt")
	}
	app.Press("id:unsaved-discard")
	if ed.StartScreenOpen() || ed.Project == nil || ed.Project.Name != "Demo Saved" {
		t.Fatalf("after Discard: startOpen=%v project=%+v, want Demo Saved", ed.StartScreenOpen(), ed.Project)
	}
	// Verify Project B's discarded edit was NOT written to disk.
	diskB, err := project.Check(os.DirFS(projB))
	if err != nil || diskB.Name != "Project B" {
		t.Fatalf("project B on disk after Discard = %+v, %v (want original Project B)", diskB, err)
	}
}

func TestStartScreenCreateProjectStreamsOutputAndHandlesFailure(t *testing.T) {
	parentDir := t.TempDir()
	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	ed.SetProjectTemplates([]editor.ProjectTemplate{
		{Name: "starter", Title: "Starter", Summary: "Smallest starter game", Dimension: "2d", Default: true},
	})
	ed.SetStartProjectParentDir(parentDir)
	ed.SetStartProjectName("new-quest")
	ed.SetStartProjectModule("example.com/new-quest")
	ed.SetStartScreenOpen(true)

	// 1) Failure path keeps Start Screen open and shows streamed stderr + error.
	ed.CreateProject = func(_ context.Context, req editor.CreateProjectRequest, stdout, stderr io.Writer) error {
		fmt.Fprintln(stdout, "writing files for", req.Template)
		fmt.Fprintln(stderr, "go mod tidy: offline")
		return errors.New("go mod tidy failed")
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	app.Press("id:start-create")
	waitForCreateInApp(t, app, ed)

	if !ed.StartScreenOpen() {
		t.Fatal("Start Screen should stay open when CreateProject fails")
	}
	if ed.CreateProjectErr() == nil || !app.Exists("id:start-create-error") {
		t.Fatalf("expected visible create error, err=%v", ed.CreateProjectErr())
	}
	outText := strings.Join(ed.CreateProjectOutput(), "\n")
	if !strings.Contains(outText, "writing files for starter") || !strings.Contains(outText, "go mod tidy: offline") {
		t.Fatalf("CreateProjectOutput = %q", outText)
	}

	// 2) Success path writes game.yaml, streams output, opens project, and closes Start Screen.
	ed.CreateProject = func(_ context.Context, req editor.CreateProjectRequest, stdout, _ io.Writer) error {
		dir := req.TargetDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		yamlData := fmt.Sprintf("version: 1\nname: %s\nid: dev.nsgo.newquest\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n", req.Name)
		if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(yamlData), 0o644); err != nil {
			return err
		}
		fmt.Fprintln(stdout, filepath.Join(dir, project.FileName))
		fmt.Fprintln(stdout, "go get -tool complete")
		return nil
	}

	app.Press("id:start-create")
	waitForCreateInApp(t, app, ed)

	if ed.StartScreenOpen() {
		t.Fatalf("Start Screen stayed open after successful create (err=%v, status=%q)", ed.CreateProjectErr(), ed.Status)
	}
	wantPath := filepath.Join(parentDir, "new-quest", project.FileName)
	if ed.Project == nil || ed.ProjectPath != wantPath || ed.Project.Name != "new-quest" {
		t.Fatalf("after create: project=%+v path=%q, want new-quest at %s", ed.Project, ed.ProjectPath, wantPath)
	}
	if got := ed.RecentProjects(); len(got) != 1 || got[0].Path != filepath.Join(parentDir, "new-quest") {
		t.Fatalf("RecentProjects after create = %+v", got)
	}
}

func TestStartScreenConcurrencyNilSafetyTildeExpansionAndClose(t *testing.T) {
	// 1) Nil receiver safety on RequestCreateProject and StartCreateProject.
	var nilEd *editor.Editor
	if err := nilEd.RequestCreateProject(editor.CreateProjectRequest{Name: "Bad!"}); err == nil {
		t.Fatal("nilEd.RequestCreateProject = nil, want error")
	}
	if err := nilEd.StartCreateProject(editor.CreateProjectRequest{Name: "good"}); err == nil {
		t.Fatal("nilEd.StartCreateProject = nil, want error")
	}

	// 2) Tilde expansion in TargetDir() and ValidateNewProjectInputs.
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		req := editor.CreateProjectRequest{Name: "cave-run", ParentDir: "~/my-nsgo-games"}
		want := filepath.Join(home, "my-nsgo-games", "cave-run")
		if got := req.TargetDir(); got != want {
			t.Fatalf("TargetDir(~/my-nsgo-games) = %q, want %q", got, want)
		}
		v := editor.ValidateNewProjectInputs("cave-run", "~/my-nsgo-games", "")
		if v.TargetDir != want {
			t.Fatalf("ValidateNewProjectInputs TargetDir = %q, want %q", v.TargetDir, want)
		}
	}

	// 3a) Concurrent StartCreateProject calls allow only one runner (no TOCTOU).
	parentDir := t.TempDir()
	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	ed.SetStartProjectParentDir(parentDir)
	ed.SetStartProjectName("race-game")
	ed.SetStartScreenOpen(true)

	started := make(chan struct{})
	release := make(chan struct{})
	var runs atomic.Int32
	ed.CreateProject = func(_ context.Context, req editor.CreateProjectRequest, _ io.Writer, _ io.Writer) error {
		if runs.Add(1) == 1 {
			close(started)
		}
		<-release
		dir := req.TargetDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		yamlData := "version: 1\nname: race-game\nid: dev.nsgo.racegame\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n"
		return os.WriteFile(filepath.Join(dir, project.FileName), []byte(yamlData), 0o644)
	}

	const workers = 4
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = ed.StartCreateProject(editor.CreateProjectRequest{
				Name:      "race-game",
				ParentDir: parentDir,
			})
		}(i)
	}
	<-started
	wg.Wait()
	close(release)
	ed.WaitForCreateProject()

	if ed.CreatingProject() || ed.StartScreenOpen() || ed.Project == nil || ed.Project.Name != "race-game" {
		t.Fatalf("headless WaitForCreateProject state: creating=%v open=%v project=%+v err=%v", ed.CreatingProject(), ed.StartScreenOpen(), ed.Project, ed.CreateProjectErr())
	}

	succeeded := 0
	alreadyRunning := 0
	for _, e := range errs {
		switch {
		case e == nil:
			succeeded++
		case errors.Is(e, editor.ErrCreateProjectRunning):
			alreadyRunning++
		default:
			t.Fatalf("unexpected concurrent StartCreateProject error: %v", e)
		}
	}
	if succeeded != 1 || alreadyRunning != workers-1 || runs.Load() != 1 {
		t.Fatalf("concurrent creates: succeeded=%d alreadyRunning=%d runs=%d, want 1 / %d / 1", succeeded, alreadyRunning, runs.Load(), workers-1)
	}

	// 3b) With a mounted EditorView, WaitForCreateProject does not return until finishOnMain has opened the project.
	edUI, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	edUI.SetStartProjectParentDir(parentDir)
	edUI.SetStartProjectName("ui-race-game")
	edUI.SetStartScreenOpen(true)
	uiRelease := make(chan struct{})
	uiRunnerReturned := make(chan struct{})
	edUI.CreateProject = func(_ context.Context, req editor.CreateProjectRequest, _ io.Writer, _ io.Writer) error {
		<-uiRelease
		dir := req.TargetDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		yamlData := "version: 1\nname: ui-race-game\nid: dev.nsgo.uiracegame\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n"
		err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(yamlData), 0o644)
		close(uiRunnerReturned)
		return err
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, edUI)
	}, nsgotest.Size(1280, 800))
	var startErr error
	app.Post(func() {
		startErr = edUI.StartCreateProject(editor.CreateProjectRequest{
			Name:      "ui-race-game",
			ParentDir: parentDir,
		})
	})
	app.Settle()
	if startErr != nil {
		t.Fatalf("UI StartCreateProject: %v", startErr)
	}
	waitDone := make(chan struct{})
	go func() {
		edUI.WaitForCreateProject()
		close(waitDone)
	}()
	close(uiRelease)
	<-uiRunnerReturned
	// Before app.Settle pumps scope.Post(finishOnMain), WaitForCreateProject must still be blocked.
	select {
	case <-waitDone:
		t.Fatal("WaitForCreateProject returned before finishOnMain ran on the UI scope")
	case <-time.After(25 * time.Millisecond):
	}
	// Pump posted callbacks until finishOnMain completes and unblocks WaitForCreateProject.
	deadline := time.After(5 * time.Second)
	for {
		app.Settle()
		select {
		case <-waitDone:
			goto uiCreateDone
		case <-time.After(5 * time.Millisecond):
		case <-deadline:
			t.Fatal("timed out waiting for finishOnMain to unblock WaitForCreateProject")
		}
	}
uiCreateDone:
	if edUI.CreatingProject() {
		t.Fatal("CreatingProject is still true after WaitForCreateProject returned")
	}
	if edUI.StartScreenOpen() {
		t.Fatalf("StartScreenOpen is still true after WaitForCreateProject (err=%v)", edUI.CreateProjectErr())
	}
	if edUI.Project == nil || edUI.Project.Name != "ui-race-game" {
		t.Fatalf("Project after WaitForCreateProject = %+v, want ui-race-game", edUI.Project)
	}
	app.Settle()

	// 4) Editor.Close cancels an active StartCreateProject run.
	ed2, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	ed2.SetStartProjectParentDir(parentDir)
	ed2.SetStartProjectName("cancel-game")
	ed2.SetStartScreenOpen(true)

	inRunner := make(chan struct{})
	cancelled := make(chan struct{})
	ed2.CreateProject = func(ctx context.Context, _ editor.CreateProjectRequest, _, _ io.Writer) error {
		close(inRunner)
		<-ctx.Done()
		close(cancelled)
		return ctx.Err()
	}
	if err := ed2.StartCreateProject(editor.CreateProjectRequest{Name: "cancel-game", ParentDir: parentDir}); err != nil {
		t.Fatalf("StartCreateProject: %v", err)
	}
	<-inRunner
	if err := ed2.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("Editor.Close did not cancel active StartCreateProject context")
	}
	ed2.WaitForCreateProject()
	if ed2.CreatingProject() {
		t.Fatal("CreatingProject remained true after Close + WaitForCreateProject")
	}
}

func waitForCreateInApp(t *testing.T, app *nsgotest.App, ed *editor.Editor) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		ed.WaitForCreateProject()
		close(done)
	}()
	deadline := time.After(30 * time.Second)
	for {
		app.Settle()
		select {
		case <-done:
			app.Settle()
			return
		case <-time.After(5 * time.Millisecond):
		case <-deadline:
			t.Fatal("timed out waiting for StartCreateProject to finish")
		}
	}
}

func TestStartScreenTemplateCardDrawsDeclaredCoverOnly(t *testing.T) {
	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	cover := filepath.Join(t.TempDir(), "cover.png")
	ed.SetProjectTemplates([]editor.ProjectTemplate{
		{Name: "starter", Title: "Starter", Summary: "An empty scene and a loop", Dimension: "2d", Cover: cover, Default: true},
		{Name: "racing3d", Title: "3D racer", Summary: "Laps, ghosts, AI drivers", Dimension: "3d"},
	})
	ed.SetStartScreenOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	if !app.Exists("id:start-template-cover-starter") {
		t.Fatal("template with a Cover rendered no cover image")
	}
	if app.Exists("id:start-template-cover-racing3d") {
		t.Fatal("template without a Cover rendered a cover image")
	}
}
