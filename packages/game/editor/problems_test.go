package editor_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// encodeTestPNG encodes a solid w x h RGBA PNG image for fixture projects.
func encodeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{R: 0x40, G: 0x80, B: 0xc0, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// writeFixtureFiles writes text and binary files into dir.
func writeFixtureFiles(t *testing.T, dir string, text map[string]string, bin map[string][]byte) {
	t.Helper()
	writeFiles(t, dir, text)
	for name, data := range bin {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const fixtureAtlasJSON = `{
  "frames": {
    "hero": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}},
    "slime": {"frame": {"x": 16, "y": 0, "w": 16, "h": 16}}
  },
  "meta": {
    "image": "actors.png",
    "size": {"w": 32, "h": 16}
  }
}
`

const fixtureProjectYAML = `version: 1
name: Fixture
id: dev.example.fixture
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
`

const fixtureNSGoYAML = `name: Fixture
id: dev.example.fixture
version: 1.0.0
`

const fixtureBrokenFrameScene = `version: 2
name: Room
layers:
  - name: actors
    objects:
      - id: hero
        sprite:
          atlas: actors
          frame: ghost
`

const fixtureCleanOtherScene = `version: 2
name: Other
layers:
  - name: actors
    objects:
      - id: slime
        sprite:
          atlas: actors
          frame: slime
`

// TestProblemsPanelMissingAtlasFrameFixAndSave verifies the primary acceptance
// flow: opening a fixture project with a scene naming a missing atlas frame
// lists the problem with its file and line, displays the count in the status
// bar, pressing the row opens the scene, and fixing and saving the scene clears
// the problem and updates the status bar count to 0.
func TestProblemsPanelMissingAtlasFrameFixAndSave(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:          fixtureProjectYAML,
		project.NSGoFileName:      fixtureNSGoYAML,
		"art/actors.json":         fixtureAtlasJSON,
		"scenes/room.scene.yaml":  fixtureBrokenFrameScene,
		"scenes/other.scene.yaml": fixtureCleanOtherScene,
	}, map[string][]byte{
		"art/actors.png": encodeTestPNG(t, 32, 16),
	})

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	if app.Exists("id:problems-panel") {
		t.Fatal("problems panel shown before a project is open")
	}
	if app.Exists("id:status-problems") {
		t.Fatal("status-problems shown before a project is open")
	}

	app.Post(func() {
		if err := ed.OpenProject(dir); err != nil {
			t.Error(err)
		}
	})
	app.WaitUntil(10*time.Second, func() bool {
		return !ed.CheckingProblems() && ed.ProblemCount() == 1
	})
	app.Settle()

	if !app.Exists("id:problems-panel") {
		t.Fatal("problems panel not shown after opening project")
	}
	if got := strings.TrimSpace(allText(app.Find("id:status-problems"))); got != "Problems: 1" {
		t.Fatalf("status-problems = %q, want %q", got, "Problems: 1")
	}

	probs := ed.Problems()
	if len(probs) != 1 {
		t.Fatalf("Problems = %+v, want 1 problem", probs)
	}
	if probs[0].File != "scenes/room.scene.yaml" || probs[0].Line != 9 || !strings.Contains(probs[0].Message, "ghost") {
		t.Fatalf("problem = %+v, want scenes/room.scene.yaml:9 mentioning ghost", probs[0])
	}

	rowText := allText(app.Find("id:problems-row-0"))
	if !strings.Contains(rowText, "scenes/room.scene.yaml:9") || !strings.Contains(rowText, "ghost") {
		t.Fatalf("problems-row-0 text = %q, want file:line and message", rowText)
	}

	// Switch to a different scene, then press the problem row to verify it opens
	// the offending scene back into the editor.
	otherPath := filepath.Join(dir, "scenes", "other.scene.yaml")
	app.Post(func() {
		if err := ed.OpenFile(otherPath); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	if ed.FilePath != otherPath || ed.Doc.Name != "Other" {
		t.Fatalf("after opening other scene: FilePath=%q Name=%q", ed.FilePath, ed.Doc.Name)
	}

	app.Press("id:problems-row-0")
	app.Settle()
	roomPath := filepath.Join(dir, "scenes", "room.scene.yaml")
	if ed.FilePath != roomPath || ed.ScenePath != "scenes/room.scene.yaml" || ed.Doc.Name != "Room" {
		t.Fatalf("after pressing problem row: FilePath=%q ScenePath=%q Name=%q", ed.FilePath, ed.ScenePath, ed.Doc.Name)
	}

	// Fix the missing atlas frame on hero and save via the toolbar Save button.
	app.Post(func() {
		hero, _, _ := ed.FindObject("hero")
		if hero == nil || hero.Sprite == nil {
			t.Error("hero sprite missing")
			return
		}
		hero.Sprite.Frame = "hero"
		ed.Rebuild()
	})
	app.Settle()
	app.Press("id:toolbar-save")
	app.WaitUntil(10*time.Second, func() bool {
		return !ed.CheckingProblems() && ed.ProblemCount() == 0
	})
	app.Settle()

	if len(ed.Problems()) != 0 {
		t.Fatalf("problems after fix and save = %+v, want empty", ed.Problems())
	}
	if got := strings.TrimSpace(allText(app.Find("id:status-problems"))); got != "Problems: 0" {
		t.Fatalf("status-problems after fix = %q, want %q", got, "Problems: 0")
	}
	if !app.Exists("id:problems-empty") {
		t.Fatal("problems-empty not shown after clearing all problems")
	}
}

// TestProblemsPanelPressSelectsNonSceneInFileBrowser verifies that pressing a
// problem row for a non-scene file selects that file in the file browser, and
// saving the project clears project.Check discrepancies such as a stale nsgo.yaml.
func TestProblemsPanelPressSelectsNonSceneInFileBrowser(t *testing.T) {
	dir := t.TempDir()
	badAtlas := `{
  "frames": {
    "hero": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}}
  },
  "meta": {
    "image": "missing.png",
    "size": {"w": 16, "h": 16}
  }
}
`
	staleNSGo := "name: Old Name\nid: dev.example.fixture\nversion: 1.0.0\n"
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:         fixtureProjectYAML,
		project.NSGoFileName:     staleNSGo,
		"art/actors.json":        badAtlas,
		"scenes/room.scene.yaml": fixtureCleanOtherScene,
	}, nil)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	app.Post(func() {
		if err := ed.OpenProject(dir); err != nil {
			t.Error(err)
		}
	})
	app.WaitUntil(10*time.Second, func() bool {
		return !ed.CheckingProblems() && ed.ProblemCount() >= 2
	})
	app.Settle()

	probs := ed.Problems()
	atlasIdx := slices.IndexFunc(probs, func(p assetcheck.Problem) bool { return p.File == "art/actors.json" })
	nsgoIdx := slices.IndexFunc(probs, func(p assetcheck.Problem) bool { return p.File == project.NSGoFileName })
	if atlasIdx < 0 || nsgoIdx < 0 {
		t.Fatalf("expected art/actors.json and nsgo.yaml problems, got %+v", probs)
	}

	// Pressing the atlas problem row selects art/actors.json in the file browser.
	app.Press("id:problems-row-" + itoa(atlasIdx))
	app.Settle()
	if ed.SelectedFile != "art/actors.json" {
		t.Fatalf("SelectedFile = %q, want art/actors.json", ed.SelectedFile)
	}
	if got := app.Find("id:files-path").Text; got != "art/actors.json" {
		t.Fatalf("files-path = %q, want art/actors.json", got)
	}

	// Pressing the nsgo.yaml problem row selects nsgo.yaml in the file browser.
	app.Press("id:problems-row-" + itoa(nsgoIdx))
	app.Settle()
	if ed.SelectedFile != project.NSGoFileName {
		t.Fatalf("SelectedFile = %q, want %s", ed.SelectedFile, project.NSGoFileName)
	}
	if got := app.Find("id:files-path").Text; got != project.NSGoFileName {
		t.Fatalf("files-path = %q, want %s", got, project.NSGoFileName)
	}

	// Saving the project syncs nsgo.yaml and automatically re-checks, clearing the nsgo.yaml problem.
	app.Post(func() {
		if err := ed.SaveProject(); err != nil {
			t.Error(err)
		}
	})
	app.WaitUntil(10*time.Second, func() bool {
		if ed.CheckingProblems() {
			return false
		}
		return !slices.ContainsFunc(ed.Problems(), func(p assetcheck.Problem) bool {
			return p.File == project.NSGoFileName
		})
	})
	app.Settle()
}

// TestProblemsParityWithGameCheck verifies that on a project with broken
// references, missing atlas frames, missing materials, missing tilemaps and
// missing autoloads, the Problems panel lists the exact same problems as
// `go tool game check` (assetcheck.Check), and with a component registry set it
// matches assetcheck.CheckWith using that registry.
func TestProblemsParityWithGameCheck(t *testing.T) {
	dir := t.TempDir()
	gameYAML := `version: 1
name: Parity
id: dev.example.parity
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
  resources:
    - name: missing_res
      path: data/missing.yaml
`
	nsgoYAML := "name: Parity\nid: dev.example.parity\nversion: 1.0.0\n"
	levelScene := `version: 2
name: Level
layers:
  - name: floor
    tilemap:
      source: maps/missing.tmj
  - name: actors
    objects:
      - id: wall
        material: materials/missing.material.yaml
`
	brokenMaterial := "type: material\nversion: 1\nalbedo: art/missing_tex.png\n"
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:                gameYAML,
		project.NSGoFileName:            nsgoYAML,
		"art/actors.json":               fixtureAtlasJSON,
		"scenes/room.scene.yaml":        fixtureBrokenFrameScene,
		"scenes/level.scene.yaml":       levelScene,
		"materials/stone.material.yaml": brokenMaterial,
	}, map[string][]byte{
		"art/actors.png": encodeTestPNG(t, 32, 16),
	})

	// Compare editor Problems() (no component registry) with assetcheck.Check (what `go tool game check` runs).
	wantReport, err := assetcheck.Check(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(wantReport.Problems) < 4 {
		t.Fatalf("expected at least 4 fixture problems, got %+v", wantReport.Problems)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	got := ed.Problems()
	if !slices.Equal(got, wantReport.Problems) {
		t.Fatalf("editor Problems() mismatch vs assetcheck.Check:\ngot:  %+v\nwant: %+v", got, wantReport.Problems)
	}

	// Now add a scene with an invalid component field and set the editor's
	// ComponentRegistry: editor Problems() must match assetcheck.CheckWith.
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name: "body",
		Fields: []sceneformat.Field{
			{Name: "speed", Kind: sceneformat.FieldFloat, Default: 10.0},
		},
	})
	badCompScene := `version: 2
name: BadComp
layers:
  - name: actors
    objects:
      - id: runner
        components:
          body:
            sped: 25
`
	writeFiles(t, dir, map[string]string{"scenes/badcomp.scene.yaml": badCompScene})

	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	wantWithReg, err := assetcheck.CheckWith(os.DirFS(dir), assetcheck.Options{Components: reg})
	if err != nil {
		t.Fatal(err)
	}
	gotWithReg := ed.Problems()
	if !slices.Equal(gotWithReg, wantWithReg.Problems) {
		t.Fatalf("editor Problems() with registry mismatch vs assetcheck.CheckWith:\ngot:  %+v\nwant: %+v", gotWithReg, wantWithReg.Problems)
	}
	if !slices.ContainsFunc(gotWithReg, func(p assetcheck.Problem) bool {
		return p.File == "scenes/badcomp.scene.yaml" && strings.Contains(p.Message, "sped")
	}) {
		t.Fatalf("missing invalid component problem in %+v", gotWithReg)
	}
}

// TestProblemsProjectCheckIssues verifies that problems reported only by
// project.Check (missing installed pack file, non-.scene.yaml invalid autoload
// scene) are surfaced in the Problems panel, and that OpenProblem returns
// ErrNoProject without an open project and falls back to selecting a scene file
// that fails to parse.
func TestProblemsProjectCheckIssues(t *testing.T) {
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	if err := ed.OpenProblem(assetcheck.Problem{File: "game.yaml"}); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("OpenProblem without project = %v, want ErrNoProject", err)
	}

	dir := t.TempDir()
	gameYAML := `version: 1
name: PackCheck
id: dev.example.packcheck
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
    - name: broken
      path: scenes/broken.yaml
packs:
  - name: missing-pack
    kind: asset
    packVersion: 1.0.0
    dir: packs/missing-pack
    sha256: 0000000000000000000000000000000000000000000000000000000000000000
    files:
      - missing.png
`
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:         gameYAML,
		project.NSGoFileName:     "name: PackCheck\nid: dev.example.packcheck\nversion: 1.0.0\n",
		"art/actors.json":        fixtureAtlasJSON,
		"scenes/room.scene.yaml": fixtureCleanOtherScene,
		"scenes/broken.yaml":     "version: 99\n",
	}, map[string][]byte{
		"art/actors.png": encodeTestPNG(t, 32, 16),
	})

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	probs := ed.Problems()
	hasPack := slices.ContainsFunc(probs, func(p assetcheck.Problem) bool {
		return p.File == project.FileName && strings.Contains(p.Message, "missing-pack")
	})
	hasBrokenAutoload := slices.ContainsFunc(probs, func(p assetcheck.Problem) bool {
		return p.File == "scenes/broken.yaml" && strings.Contains(p.Message, "autoload scene")
	})
	if !hasPack || !hasBrokenAutoload {
		t.Fatalf("expected missing pack and broken autoload scene problems, got %+v", probs)
	}

	// OpenProblem on the unparseable autoload scene returns an error and still
	// selects the file in the file browser so the user can locate it.
	brokenProb := probs[slices.IndexFunc(probs, func(p assetcheck.Problem) bool { return p.File == "scenes/broken.yaml" })]
	if err := ed.OpenProblem(brokenProb); err == nil {
		t.Fatal("OpenProblem on invalid scene succeeded")
	}
	if ed.SelectedFile != "scenes/broken.yaml" {
		t.Fatalf("SelectedFile after failed scene open = %q, want scenes/broken.yaml", ed.SelectedFile)
	}
}

// TestProblemsRechecksAfterFileMove verifies that MoveFile (and undoing it)
// automatically re-runs the project check and updates Problems().
func TestProblemsRechecksAfterFileMove(t *testing.T) {
	dir := t.TempDir()
	scene := `version: 2
name: Room
layers:
  - name: actors
    objects:
      - id: wall
        material: materials/stone.material.yaml
`
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:                fixtureProjectYAML,
		project.NSGoFileName:            fixtureNSGoYAML,
		"scenes/room.scene.yaml":        scene,
		"materials/wrong.material.yaml": "type: material\nversion: 1\n",
	}, nil)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	if got := ed.Problems(); len(got) != 1 || got[0].File != "scenes/room.scene.yaml" || !strings.Contains(got[0].Message, "materials/stone.material.yaml") {
		t.Fatalf("before move: Problems = %+v, want 1 missing material problem", got)
	}

	// Moving materials/wrong.material.yaml to the expected path clears the problem.
	if err := ed.MoveFile("materials/wrong.material.yaml", "materials/stone.material.yaml"); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	if got := ed.Problems(); len(got) != 0 {
		t.Fatalf("after move: Problems = %+v, want empty", got)
	}

	// Undoing the move restores the missing material problem.
	if !ed.Undo() {
		t.Fatalf("Undo failed: %s", ed.Status)
	}
	ed.WaitForProblems()
	if got := ed.Problems(); len(got) != 1 || !strings.Contains(got[0].Message, "materials/stone.material.yaml") {
		t.Fatalf("after undo: Problems = %+v, want missing material problem back", got)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// TestProblemRowAsksBeforeOpeningAnotherScene presses a problem in another scene while the open one has unsaved edits: the editor asks first, Cancel keeps the edits and Discard opens the problem's scene and clears the OnSave of the scene open before, and a problem in the scene already open selects it without reloading it.
func TestProblemRowAsksBeforeOpeningAnotherScene(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:          fixtureProjectYAML,
		project.NSGoFileName:      fixtureNSGoYAML,
		"art/actors.json":         fixtureAtlasJSON,
		"scenes/room.scene.yaml":  fixtureBrokenFrameScene,
		"scenes/other.scene.yaml": fixtureCleanOtherScene,
	}, map[string][]byte{
		"art/actors.png": encodeTestPNG(t, 32, 16),
	})
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	app.Post(func() {
		if err := ed.OpenProject(dir); err != nil {
			t.Error(err)
		}
	})
	app.WaitUntil(10*time.Second, func() bool {
		return !ed.CheckingProblems() && ed.ProblemCount() == 1
	})
	app.Settle()

	edit := func(id string, x float32) {
		app.Post(func() {
			if err := ed.MoveObject(id, x, x); err != nil {
				t.Error(err)
			}
		})
		app.Settle()
	}
	app.Post(func() {
		if err := ed.OpenScene("scenes/other.scene.yaml"); err != nil {
			t.Error(err)
		}
		// An app that persists the open scene itself: the problem's scene must not go to this callback.
		ed.OnSave = func([]byte) error { return nil }
	})
	app.Settle()
	edit("slime", 33)

	app.Press("id:problems-row-0")
	app.Settle()
	if !app.Exists("id:unsaved-dialog") {
		t.Fatal("opening the problem's scene over unsaved edits did not ask")
	}
	app.Press("id:unsaved-cancel")
	app.Settle()
	if ed.ScenePath != "scenes/other.scene.yaml" || !ed.SceneDirty() || !ed.History.CanUndo() {
		t.Fatalf("after Cancel: scene %q, dirty %v, undo %v", ed.ScenePath, ed.SceneDirty(), ed.History.CanUndo())
	}

	app.Press("id:problems-row-0")
	app.Settle()
	app.Press("id:unsaved-discard")
	app.Settle()
	if ed.ScenePath != "scenes/room.scene.yaml" || ed.Doc.Name != "Room" || ed.HasUnsavedChanges() || ed.OnSave != nil {
		t.Fatalf("after Discard: scene %q named %q, unsaved %v, OnSave kept %v", ed.ScenePath, ed.Doc.Name, ed.HasUnsavedChanges(), ed.OnSave != nil)
	}

	edit("hero", 44)
	app.Press("id:problems-row-0")
	app.Settle()
	if app.Exists("id:unsaved-dialog") || !ed.SceneDirty() || !ed.History.CanUndo() || ed.SelectedFile != "scenes/room.scene.yaml" {
		t.Fatalf("a problem in the open scene: prompt %v, dirty %v, undo %v, selected %q", app.Exists("id:unsaved-dialog"), ed.SceneDirty(), ed.History.CanUndo(), ed.SelectedFile)
	}
}
