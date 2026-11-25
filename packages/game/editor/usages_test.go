package editor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func setupUsagesFixtureProject(t *testing.T) (string, map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	raw := map[string]string{
		"game.yaml": `version: 1
name: Dungeon Run
id: dev.example.dungeon
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`,
		"prefabs/slime.prefab.yaml": `version: 1
name: slime
components:
  patrol:
    speed: 24
    wait: 0.5
  health:
    max: 2
`,
		"prefabs/slime_blue.prefab.yaml": `version: 1
inherits: prefabs/slime.prefab.yaml
name: slime_blue
components:
  patrol:
    speed: 32
`,
		"prefabs/slime_king.prefab.yaml": `version: 1
inherits: prefabs/slime.prefab.yaml
name: slime_king
components:
  health:
    max: 12
`,
		"scenes/level_01.scene.yaml": `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 220
        y: 340
      - id: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 284
        y: 340
        components:
          patrol:
            speed: 30
          health:
            max: 3
      - id: slime_03
        prefab: prefabs/slime.prefab.yaml
        x: 470
        y: 270
`,
		"scenes/level_02.scene.yaml": `version: 3
name: level_02
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 96
        y: 340
      - id: slime_04
        prefab: prefabs/slime.prefab.yaml
        x: 540
        y: 340
        properties:
          tint: "#9FD8B4"
`,
		"scenes/level_03.scene.yaml": `version: 3
name: level_03
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: slime_pit_a
        prefab: prefabs/slime.prefab.yaml
        x: 176
        y: 340
      - id: slime_pit_b
        prefab: prefabs/slime.prefab.yaml
        x: 530
        y: 150
`,
		"data/spawn_waves.yaml": `waves:
  - id: wave_02
    prefab: prefabs/slime.prefab.yaml
    count: 4
    every: 2.5
  - id: wave_05
    prefab: prefabs/slime.prefab.yaml
    count: 8
    every: 1.5
`,
		"game/spawn.go": `package game

// slimePrefab is dropped into every open pit.
const slimePrefab = "prefabs/slime.prefab.yaml"

func spawnPitSlimes() string {
	return slimePrefab
}
`,
	}
	originals := make(map[string][]byte, len(raw))
	for rel, content := range raw {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		b := []byte(content)
		if err := os.WriteFile(abs, b, 0o644); err != nil {
			t.Fatal(err)
		}
		originals[rel] = b
	}
	return dir, originals
}

// Acceptance #1 and #2:
// - Renaming a prefab used by 3 scenes, 2 prefab variants, and a data table shows every rewritten line before anything is written to disk.
// - A Go constant holding the old path is found via go/parser and listed as NOT rewritten.
// - ONE Undo after Keep restores every file to its exact original path and contents (and restores open tabs).
func TestSafeRenameThreeScenesDataTableGoConstantAndOneUndo(t *testing.T) {
	dir, originals := setupUsagesFixtureProject(t)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	// Open a second scene tab too so we verify multi-tab sync + undo.
	if err := ed.OpenScene("scenes/level_02.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenScene("scenes/level_01.scene.yaml"); err != nil {
		t.Fatal(err)
	}

	// 1. FindUsages groups into Scenes (3), Prefab Variants (2), Data Tables (1), Go Code (1).
	rep, err := ed.FindUsages("prefabs/slime.prefab.yaml")
	if err != nil {
		t.Fatalf("FindUsages: %v", err)
	}
	if len(rep.Scenes) != 3 {
		t.Fatalf("Scenes = %d, want 3", len(rep.Scenes))
	}
	if len(rep.Variants) != 2 {
		t.Fatalf("Variants = %d, want 2", len(rep.Variants))
	}
	if len(rep.DataTables) != 1 || len(rep.DataTables[0].Refs) != 2 {
		t.Fatalf("DataTables = %+v, want 1 file with 2 rows", rep.DataTables)
	}
	if len(rep.GoCode) != 1 || len(rep.GoCode[0].Refs) != 1 {
		t.Fatalf("GoCode = %+v, want 1 file with 1 const", rep.GoCode)
	}
	goRef := rep.GoCode[0].Refs[0]
	if goRef.File != "game/spawn.go" || goRef.Line != 4 || goRef.GoIdent != "slimePrefab" || !goRef.NotRewritten {
		t.Fatalf("unexpected Go ref: %+v", goRef)
	}
	// Verify override counts detected on slime_02 (2 overrides: patrol + health).
	var foundSlime02 bool
	for _, r := range rep.Scenes[0].Refs {
		if r.ObjectID == "slime_02" {
			foundSlime02 = true
			if r.OverrideCount != 2 {
				t.Fatalf("slime_02 OverrideCount = %d, want 2 (%v)", r.OverrideCount, r.Overrides)
			}
		}
	}
	if !foundSlime02 {
		t.Fatalf("slime_02 not found in level_01 usages: %+v", rep.Scenes[0].Refs)
	}

	// 2. PreviewSafeRename computes every exact -/+ line diff WITHOUT writing anything to disk.
	plan, err := ed.PreviewSafeRename("prefabs/slime.prefab.yaml", "slime_green")
	if err != nil {
		t.Fatalf("PreviewSafeRename: %v", err)
	}
	if !plan.Valid || plan.To != "prefabs/slime_green.prefab.yaml" {
		t.Fatalf("plan invalid or wrong target: %+v", plan)
	}
	// 3 scenes + 2 variants + 1 data table = 6 rewritten YAML files + 1 moved file = 7 touched files.
	if plan.RewrittenFileCount != 6 || plan.TotalTouchedFiles != 7 {
		t.Fatalf("RewrittenFileCount=%d TotalTouchedFiles=%d, want 6 and 7", plan.RewrittenFileCount, plan.TotalTouchedFiles)
	}
	// Total YAML refs: 3 (level_01) + 2 (level_02) + 2 (level_03) + 1 (slime_blue) + 1 (slime_king) + 2 (spawn_waves) = 11.
	if plan.RewrittenRefCount != 11 {
		t.Fatalf("RewrittenRefCount = %d, want 11", plan.RewrittenRefCount)
	}
	// Every rewritten file must have exact old/new line diffs.
	for _, expectedPath := range []string{
		"scenes/level_01.scene.yaml",
		"scenes/level_02.scene.yaml",
		"scenes/level_03.scene.yaml",
		"prefabs/slime_blue.prefab.yaml",
		"prefabs/slime_king.prefab.yaml",
		"data/spawn_waves.yaml",
	} {
		fd := plan.FileDiff(expectedPath)
		if fd == nil || len(fd.Diffs) == 0 {
			t.Fatalf("missing diff for %s", expectedPath)
		}
		for _, d := range fd.Diffs {
			if !strings.Contains(d.OldText, "prefabs/slime.prefab.yaml") || !strings.Contains(d.NewText, "prefabs/slime_green.prefab.yaml") {
				t.Fatalf("%s line %d unexpected diff: old=%q new=%q", expectedPath, d.Line, d.OldText, d.NewText)
			}
		}
	}
	// Go constant must be listed as a non-rewritten problem!
	if len(plan.Problems) != 1 || !plan.Problems[0].NotRewritten || plan.Problems[0].File != "game/spawn.go" || plan.Problems[0].Line != 4 {
		t.Fatalf("unexpected plan.Problems: %+v", plan.Problems)
	}
	if !strings.Contains(plan.Problems[0].Message, "game/spawn.go loads the old path and is not rewritten") {
		t.Fatalf("unexpected problem message: %q", plan.Problems[0].Message)
	}

	// Verify ZERO files changed on disk during Preview!
	for rel, orig := range originals {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("file %s missing after preview: %v", rel, err)
		}
		if !bytes.Equal(got, orig) {
			t.Fatalf("file %s modified during preview!", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "prefabs", "slime_green.prefab.yaml")); !os.IsNotExist(err) {
		t.Fatalf("destination file created during preview!")
	}

	// 3. ApplySafeRename (Keep) writes YAML only as ONE undoable step, preserving overrides and leaving Go untouched.
	if err := ed.ApplySafeRename(); err != nil {
		t.Fatalf("ApplySafeRename: %v", err)
	}
	st := ed.ReferencesScreen()
	if st == nil || st.Mode != editor.ReferencesModeDone {
		t.Fatalf("expected Done mode after ApplySafeRename, got %+v", st)
	}
	if _, err := os.Stat(filepath.Join(dir, "prefabs", "slime.prefab.yaml")); !os.IsNotExist(err) {
		t.Fatalf("old prefab still exists after Keep")
	}
	if got, err := os.ReadFile(filepath.Join(dir, "prefabs", "slime_green.prefab.yaml")); err != nil || !bytes.Equal(got, originals["prefabs/slime.prefab.yaml"]) {
		t.Fatalf("moved prefab contents changed: err=%v", err)
	}
	// Check overrides on slime_02 preserved in level_01 on disk AND in open tab!
	l1Disk, err := os.ReadFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(l1Disk), "prefabs/slime.prefab.yaml") || !strings.Contains(string(l1Disk), "speed: 30") || !strings.Contains(string(l1Disk), "max: 3") {
		t.Fatalf("level_01 after Keep did not rewrite cleanly or lost overrides:\n%s", l1Disk)
	}
	if slime02, _, _ := ed.FindObject("slime_02"); slime02 == nil || slime02.Prefab != "prefabs/slime_green.prefab.yaml" {
		t.Fatalf("open tab slime_02.Prefab = %+v, want prefabs/slime_green.prefab.yaml", slime02)
	}
	// Go file MUST NOT have been rewritten!
	if goDisk, err := os.ReadFile(filepath.Join(dir, "game", "spawn.go")); err != nil || !bytes.Equal(goDisk, originals["game/spawn.go"]) {
		t.Fatalf("game/spawn.go should remain untouched, got:\n%s", goDisk)
	}
	if len(st.CheckProblems) != 1 || st.CheckProblems[0].File != "game/spawn.go" {
		t.Fatalf("Done CheckProblems = %+v, want 1 problem in game/spawn.go", st.CheckProblems)
	}

	// 4. ONE Undo restores EVERY file on disk byte-for-byte AND restores open tabs AND restores Usages mode!
	if !ed.Undo() {
		t.Fatalf("Undo failed: %s", ed.Status)
	}
	if _, err := os.Stat(filepath.Join(dir, "prefabs", "slime_green.prefab.yaml")); !os.IsNotExist(err) {
		t.Fatalf("new prefab still exists after Undo")
	}
	for rel, orig := range originals {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("file %s missing after Undo: %v", rel, err)
		}
		if !bytes.Equal(got, orig) {
			t.Fatalf("file %s did not restore byte-for-byte after Undo:\ngot:\n%s\nwant:\n%s", rel, got, orig)
		}
	}
	if slime02, _, _ := ed.FindObject("slime_02"); slime02 == nil || slime02.Prefab != "prefabs/slime.prefab.yaml" {
		t.Fatalf("open tab slime_02.Prefab after Undo = %+v, want prefabs/slime.prefab.yaml", slime02)
	}
	if stAfter := ed.ReferencesScreen(); stAfter == nil || stAfter.Mode != editor.ReferencesModeUsages || stAfter.Target != "prefabs/slime.prefab.yaml" {
		t.Fatalf("ReferencesScreen after Undo = %+v, want Usages mode on prefabs/slime.prefab.yaml", stAfter)
	}
}

func TestReferencesScreenUIKeyboardNavigationCallbacksAndMenu(t *testing.T) {
	dir, _ := setupUsagesFixtureProject(t)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SelectObject("slime_02")

	var asked []editor.SafeRenameAgentRequest
	ed.OnAskAgentUpdateRef = func(req editor.SafeRenameAgentRequest) error {
		asked = append(asked, req)
		return nil
	}
	var openedGo []string
	ed.OnOpenSourceFile = func(file string, line int) error {
		openedGo = append(openedGo, file)
		return nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// Verify Project > Find Usages exists in desktop menu with shift+f12.
	item, ok := findMenuItem(app.Menu, "Project", "Find Usages")
	if !ok {
		t.Fatal("Project > Find Usages menu item missing")
	}
	if item.Shortcut != "shift+f12" {
		t.Fatalf("Project > Find Usages shortcut = %q, want shift+f12", item.Shortcut)
	}

	// Press shift+f12 to open Find Usages screen for slime_02's prefab.
	app.Key("shift+f12")
	if !ed.UsagesScreenOpen() {
		t.Fatal("shift+f12 did not open References screen")
	}
	if got := app.Find("refs-title").Text; got != "Usages of slime.prefab.yaml" {
		t.Fatalf("refs-title = %q, want Usages of slime.prefab.yaml", got)
	}

	// Arrow navigation moves through usages.
	initial := ed.ReferencesScreen()
	app.Key("down")
	next := ed.ReferencesScreen()
	if next.FocusedFile == initial.FocusedFile && next.FocusedRefIdx == initial.FocusedRefIdx {
		t.Fatalf("down arrow did not advance focus: %+v", next)
	}

	// Clicking a scene card switches focus to that scene.
	app.Press("id:refs-scene-card-scenes/level_02.scene.yaml")
	if cur := ed.ReferencesScreen(); cur.FocusedFile != "scenes/level_02.scene.yaml" {
		t.Fatalf("clicking scene card did not focus level_02: %+v", cur)
	}

	// Pressing Enter opens the scene with the referencing object selected and closes the References screen.
	app.Key("enter")
	if ed.UsagesScreenOpen() {
		t.Fatal("Enter did not close References screen after opening scene usage")
	}
	if ed.ScenePath != "scenes/level_02.scene.yaml" || ed.SelectedID != "slime_01" {
		t.Fatalf("Enter opened ScenePath=%q SelectedID=%q, want level_02 / slime_01", ed.ScenePath, ed.SelectedID)
	}

	// Reopen, test F2 rename live validation + Go warning callbacks + Keep + Undo button.
	app.Post(func() {
		_ = ed.OpenUsagesScreen("prefabs/slime.prefab.yaml")
	})
	app.Settle()

	// Click Go file row and verify warning callout buttons work.
	app.Press("id:refs-file-game/spawn.go")
	app.Press("id:refs-ask-agent")
	if len(asked) != 1 || asked[0].File != "game/spawn.go" || asked[0].Ident != "slimePrefab" {
		t.Fatalf("OnAskAgentUpdateRef = %+v", asked)
	}
	app.Press("id:refs-open-go-file")
	if len(openedGo) != 1 || openedGo[0] != "game/spawn.go" {
		t.Fatalf("OnOpenSourceFile = %+v", openedGo)
	}

	// Enter Rename mode via F2 and validate live.
	app.Key("f2")
	if cur := ed.ReferencesScreen(); cur.Mode != editor.ReferencesModeRename {
		t.Fatalf("F2 did not enter Rename mode: %+v", cur)
	}
	// Invalid existing target is rejected live.
	app.Post(func() {
		_ = ed.SetSafeRenameInput("slime_blue")
	})
	app.Settle()
	if cur := ed.ReferencesScreen(); cur.Plan == nil || cur.Plan.Valid {
		t.Fatalf("expected existing name slime_blue to be rejected, got %+v", cur.Plan)
	}
	// Valid free name enables Keep.
	app.Post(func() {
		_ = ed.SetSafeRenameInput("slime_green")
	})
	app.Settle()
	if cur := ed.ReferencesScreen(); cur.Plan == nil || !cur.Plan.Valid {
		t.Fatalf("expected slime_green to be valid, got %+v", cur.Plan)
	}

	// Press Keep -> Done -> Undo · ⌘Z -> Usages -> Esc closes.
	app.Press("id:refs-apply-rename")
	if cur := ed.ReferencesScreen(); cur.Mode != editor.ReferencesModeDone {
		t.Fatalf("Keep did not transition to Done mode: %+v", cur)
	}
	app.Press("id:refs-undo-rename")
	if cur := ed.ReferencesScreen(); cur.Mode != editor.ReferencesModeUsages || cur.Target != "prefabs/slime.prefab.yaml" {
		t.Fatalf("Undo button did not restore Usages mode: %+v", cur)
	}
	app.Key("escape")
	if ed.UsagesScreenOpen() {
		t.Fatal("Escape did not close References screen")
	}
}

func TestReferencesRenameInputKeepsFocusWhileTyping(t *testing.T) {
	dir, _ := setupUsagesFixtureProject(t)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	app.Post(func() {
		if err := ed.OpenUsagesScreen("prefabs/slime.prefab.yaml"); err != nil {
			t.Errorf("OpenUsagesScreen: %v", err)
		}
	})
	app.Settle()

	app.Key("f2")
	app.Focus("id:refs-new-name-input")
	before := app.Find("id:refs-new-name-input")
	if !before.Focused {
		t.Fatal("refs-new-name-input should be focused")
	}

	// Typing character by character appends to "slime" -> "slime_green" without remounting ui.Input or dropping focus.
	for _, ch := range []string{"_", "g", "r", "e", "e", "n"} {
		app.Type("id:refs-new-name-input", ch)
		after := app.Find("id:refs-new-name-input")
		if after.ID != before.ID {
			t.Fatalf("typing %q remounted refs-new-name-input: node ID changed %d -> %d", ch, before.ID, after.ID)
		}
		if !after.Focused {
			t.Fatalf("typing %q dropped focus on refs-new-name-input", ch)
		}
	}
	if cur := ed.ReferencesScreen(); cur == nil || cur.Plan == nil || !cur.Plan.Valid || cur.Plan.To != "prefabs/slime_green.prefab.yaml" {
		t.Fatalf("plan after typing = %+v, want valid plan to prefabs/slime_green.prefab.yaml", cur)
	}
}

func TestReferencesLifecycleResetAndModalGuards(t *testing.T) {
	dirA, _ := setupUsagesFixtureProject(t)
	dirB, _ := setupUsagesFixtureProject(t)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dirA); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenUsagesScreen("prefabs/slime.prefab.yaml"); err != nil {
		t.Fatalf("OpenUsagesScreen: %v", err)
	}
	if !ed.UsagesScreenOpen() {
		t.Fatal("expected Usages screen open")
	}

	// OpenProject switches project and closes/clears usages state.
	if err := ed.OpenProject(dirB); err != nil {
		t.Fatal(err)
	}
	if ed.UsagesScreenOpen() || ed.ReferencesScreen() != nil {
		t.Fatal("OpenProject should close and clear References screen")
	}

	// OpenDemo closes and clears usages state.
	if err := ed.OpenUsagesScreen("prefabs/slime.prefab.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if ed.UsagesScreenOpen() || ed.ReferencesScreen() != nil {
		t.Fatal("OpenDemo should close and clear References screen")
	}

	// Fresh editor for Play and modal guards.
	ed, err = editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dirB); err != nil {
		t.Fatal(err)
	}

	// Play closes usages and blocks Open/Start/Preview/Apply while playing.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Post(func() {
		if _, err := ed.PreviewSafeRename("prefabs/slime.prefab.yaml", "slime_green"); err != nil {
			t.Errorf("PreviewSafeRename: %v", err)
		}
	})
	app.Settle()
	if !ed.UsagesScreenOpen() {
		t.Fatal("expected Usages screen open before Play")
	}
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	if ed.UsagesScreenOpen() {
		t.Fatal("Play should close References screen")
	}
	if err := ed.OpenUsagesScreen("prefabs/slime.prefab.yaml"); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("OpenUsagesScreen while playing = %v, want ErrPlaying", err)
	}
	if err := ed.StartSafeRename(); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("StartSafeRename while playing = %v, want ErrPlaying", err)
	}
	if _, err := ed.PreviewSafeRename("prefabs/slime.prefab.yaml", "slime_green"); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("PreviewSafeRename while playing = %v, want ErrPlaying", err)
	}
	if err := ed.ApplySafeRename(); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("ApplySafeRename while playing = %v, want ErrPlaying", err)
	}

	app.Post(func() {
		if err := ed.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	app.Settle()

	// StartScreen blocks Open/Start/Preview/Apply.
	app.Post(func() {
		ed.SetStartScreenOpen(true)
	})
	app.Settle()
	if err := ed.OpenUsagesScreen("prefabs/slime.prefab.yaml"); err == nil {
		t.Fatal("expected OpenUsagesScreen to fail while StartScreen is open")
	}
	if err := ed.StartSafeRename(); err == nil {
		t.Fatal("expected StartSafeRename to fail while StartScreen is open")
	}
	if _, err := ed.PreviewSafeRename("prefabs/slime.prefab.yaml", "slime_green"); err == nil {
		t.Fatal("expected PreviewSafeRename to fail while StartScreen is open")
	}
	if err := ed.ApplySafeRename(); err == nil {
		t.Fatal("expected ApplySafeRename to fail while StartScreen is open")
	}
	app.Post(func() {
		ed.SetStartScreenOpen(false)
	})
	app.Settle()

	// PlaytestReportOpen blocks Open/Start/Preview/Apply and disables CmdFindUsages.
	app.Post(func() {
		ed.SetPlaytestReportOpen(true)
	})
	app.Settle()
	if !ed.PlaytestReportOpen() {
		t.Fatal("expected PlaytestReportOpen")
	}
	if err := ed.OpenUsagesScreen("prefabs/slime.prefab.yaml"); err == nil {
		t.Fatal("expected OpenUsagesScreen to fail while PlaytestReport is open")
	}
	if _, err := ed.PreviewSafeRename("prefabs/slime.prefab.yaml", "slime_green"); err == nil {
		t.Fatal("expected PreviewSafeRename to fail while PlaytestReport is open")
	}
	cmd, ok := ed.Command(editor.CmdFindUsages)
	if !ok {
		t.Fatal("CmdFindUsages not registered")
	}
	if cmd.Enabled != nil && cmd.Enabled() {
		t.Fatal("CmdFindUsages should be disabled while PlaytestReport is open")
	}
}

func TestUsagesAndSafeRenameRejectTraversalAndSymlinkEscape(t *testing.T) {
	dir, _ := setupUsagesFixtureProject(t)
	outside := t.TempDir()

	outsideGo := filepath.Join(outside, "secret.go")
	if err := os.WriteFile(outsideGo, []byte("package outside\nconst p = \"prefabs/slime.prefab.yaml\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outsidePrefab := filepath.Join(outside, "secret.prefab.yaml")
	if err := os.WriteFile(outsidePrefab, []byte("version: 1\nname: secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideGo, filepath.Join(dir, "game", "escape.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outsidePrefab, filepath.Join(dir, "prefabs", "escape.prefab.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape_dir")); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// scanGoUsages skips symlinked Go files pointing outside the project root.
	rep, err := ed.FindUsages("prefabs/slime.prefab.yaml")
	if err != nil {
		t.Fatalf("FindUsages: %v", err)
	}
	for _, gf := range rep.GoCode {
		if gf.File == "game/escape.go" {
			t.Fatal("FindUsages must not scan symlinked Go file outside project root")
		}
	}

	// FindUsages rejects a target that escapes root via symlink.
	if _, err := ed.FindUsages("prefabs/escape.prefab.yaml"); err == nil {
		t.Fatal("FindUsages should reject symlink target escaping project root")
	}

	// OpenUsageItem rejects traversal and symlink escape without calling OnOpenSourceFile.
	var opened []string
	ed.OnOpenSourceFile = func(file string, line int) error {
		opened = append(opened, file)
		return nil
	}
	if err := ed.OpenUsageItem(editor.UsageItem{File: "../../etc/passwd", Group: editor.UsageGroupGoCode, Line: 1}); err == nil {
		t.Fatal("OpenUsageItem should reject path traversal")
	}
	if err := ed.OpenUsageItem(editor.UsageItem{File: "game/escape.go", Group: editor.UsageGroupGoCode, Line: 1}); err == nil {
		t.Fatal("OpenUsageItem should reject symlink escape")
	}
	if len(opened) != 0 {
		t.Fatalf("OnOpenSourceFile was called for rejected paths: %v", opened)
	}

	// PreviewSafeRename rejects symlinked destination parent, colons in bare stem, and symlinked source.
	if plan, err := ed.PreviewSafeRename("prefabs/slime.prefab.yaml", "escape_dir/new_slime.prefab.yaml"); err == nil || (plan != nil && plan.Valid) {
		t.Fatalf("PreviewSafeRename should reject symlinked destination parent, got plan=%+v err=%v", plan, err)
	}
	if plan, err := ed.PreviewSafeRename("prefabs/slime.prefab.yaml", "bad:name"); err == nil || (plan != nil && plan.Valid) {
		t.Fatalf("PreviewSafeRename should reject colon in bare stem, got plan=%+v err=%v", plan, err)
	}
	if plan, err := ed.PreviewSafeRename("prefabs/escape.prefab.yaml", "slime_new"); err == nil || (plan != nil && plan.Valid) {
		t.Fatalf("PreviewSafeRename should reject symlinked source, got plan=%+v err=%v", plan, err)
	}
}

func TestSafeRenamePreservesDirtyDataTableEdits(t *testing.T) {
	dir, _ := setupUsagesFixtureProject(t)

	// Add a structured versioned data table file that editor.OpenDataTableProjectFile opens.
	tableYAML := `version: 1
rows:
  - id: slime_grunt
    hp: 20
    prefab: prefabs/slime.prefab.yaml
`
	if err := os.WriteFile(filepath.Join(dir, "data", "enemies.yaml"), []byte(tableYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenDataTableProjectFile("data/enemies.yaml"); err != nil {
		t.Fatalf("OpenDataTableProjectFile: %v", err)
	}
	if ed.DataTableDirty() {
		t.Fatal("freshly opened data table should not be dirty")
	}

	// 1. Clean table stays clean after Safe Rename + Undo, with updated path in memory.
	if _, err := ed.PreviewSafeRename("prefabs/slime.prefab.yaml", "slime_green"); err != nil {
		t.Fatalf("PreviewSafeRename: %v", err)
	}
	if err := ed.ApplySafeRename(); err != nil {
		t.Fatalf("ApplySafeRename: %v", err)
	}
	if ed.DataTableDirty() {
		t.Fatal("clean data table should remain clean after Safe Rename")
	}
	if cell, ok := ed.DataTable().Cell(0, "prefab"); !ok || cell.Text != "prefabs/slime_green.prefab.yaml" {
		t.Fatalf("clean table prefab cell = %+v (%v), want prefabs/slime_green.prefab.yaml", cell, ok)
	}
	if !ed.UndoSafeRename() {
		t.Fatal("UndoSafeRename failed")
	}
	if ed.DataTableDirty() {
		t.Fatal("clean data table should remain clean after Undo")
	}

	// 2. Dirty table keeps unsaved edits in memory while rewriting moved path references.
	if err := ed.SetDataTableCellText(0, "hp", "999"); err != nil {
		t.Fatalf("SetDataTableCellText: %v", err)
	}
	if !ed.DataTableDirty() {
		t.Fatal("edited data table should be dirty")
	}
	if _, err := ed.PreviewSafeRename("prefabs/slime.prefab.yaml", "slime_green"); err != nil {
		t.Fatalf("PreviewSafeRename (dirty): %v", err)
	}
	if err := ed.ApplySafeRename(); err != nil {
		t.Fatalf("ApplySafeRename (dirty): %v", err)
	}
	if !ed.DataTableDirty() {
		t.Fatal("dirty data table should remain dirty after Safe Rename")
	}
	if hp, ok := ed.DataTable().Cell(0, "hp"); !ok || hp.Text != "999" {
		t.Fatalf("unsaved hp edit was clobbered: got %+v (%v), want 999", hp, ok)
	}
	if prefab, ok := ed.DataTable().Cell(0, "prefab"); !ok || prefab.Text != "prefabs/slime_green.prefab.yaml" {
		t.Fatalf("dirty table prefab cell = %+v (%v), want prefabs/slime_green.prefab.yaml", prefab, ok)
	}

	// Undo restores the prefab path in memory while still preserving unsaved hp=999.
	if !ed.UndoSafeRename() {
		t.Fatal("UndoSafeRename (dirty) failed")
	}
	if hp, ok := ed.DataTable().Cell(0, "hp"); !ok || hp.Text != "999" {
		t.Fatalf("unsaved hp edit was clobbered on Undo: got %+v (%v), want 999", hp, ok)
	}
	if prefab, ok := ed.DataTable().Cell(0, "prefab"); !ok || prefab.Text != "prefabs/slime.prefab.yaml" {
		t.Fatalf("dirty table prefab cell after Undo = %+v (%v), want prefabs/slime.prefab.yaml", prefab, ok)
	}
}

func TestMoveFileUpdatesSceneProbesAndSkyHDR(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"game.yaml": `version: 1
name: Probe Move
id: dev.example.probes
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: main
  scenes:
    - name: main
      path: scenes/main.scene.yaml
`,
		"env/sky.hdr":             "#?RADIANCE\n\n-Y 1 +X 1\n\x00\x00\x00\x00",
		"probes/room.hdr":         "#?RADIANCE\n\n-Y 1 +X 1\n\x00\x00\x00\x00",
		"probes/room.probes.yaml": "version: 1\n",
		"scenes/base.scene.yaml": `version: 4
name: base
virtualWidth: 640
virtualHeight: 360
layers:
  - name: world
    objects:
      - id: probe_ref
        reflectionProbe:
          cubemap: probes/room.hdr
      - id: probe_grid
        lightProbeGrid:
          data: probes/room.probes.yaml
`,
		"scenes/main.scene.yaml": `version: 4
name: main
inherits: scenes/base.scene.yaml
virtualWidth: 640
virtualHeight: 360
environment:
  sky:
    kind: hdr
    hdr: env/sky.hdr
overrides:
  probe_ref:
    reflectionProbe:
      cubemap: probes/room.hdr
  probe_grid:
    lightProbeGrid:
      data: probes/room.probes.yaml
layers:
  - name: world
    objects:
      - id: local_ref
        reflectionProbe:
          cubemap: probes/room.hdr
      - id: local_grid
        lightProbeGrid:
          data: probes/room.probes.yaml
`,
	}
	for rel, content := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	if err := ed.MoveFile("env/sky.hdr", "env/sunset.hdr"); err != nil {
		t.Fatalf("MoveFile sky: %v", err)
	}
	if err := ed.MoveFile("probes/room.hdr", "probes/hall.hdr"); err != nil {
		t.Fatalf("MoveFile cubemap: %v", err)
	}
	if err := ed.MoveFile("probes/room.probes.yaml", "probes/hall.probes.yaml"); err != nil {
		t.Fatalf("MoveFile light probe grid: %v", err)
	}

	if ed.Doc.Environment == nil || ed.Doc.Environment.Sky == nil || ed.Doc.Environment.Sky.HDR != "env/sunset.hdr" {
		t.Fatalf("Doc.Environment.Sky.HDR = %+v, want env/sunset.hdr", ed.Doc.Environment)
	}
	if obj, _, _ := ed.FindObject("local_ref"); obj == nil || obj.ReflectionProbe == nil || obj.ReflectionProbe.Cubemap != "probes/hall.hdr" {
		t.Fatalf("local_ref.ReflectionProbe = %+v, want probes/hall.hdr", obj)
	}
	if obj, _, _ := ed.FindObject("local_grid"); obj == nil || obj.LightProbeGrid == nil || obj.LightProbeGrid.Data != "probes/hall.probes.yaml" {
		t.Fatalf("local_grid.LightProbeGrid = %+v, want probes/hall.probes.yaml", obj)
	}
	if ov := ed.Doc.Overrides["probe_ref"]; ov.ReflectionProbe == nil || ov.ReflectionProbe.Cubemap != "probes/hall.hdr" {
		t.Fatalf("Overrides[probe_ref].ReflectionProbe = %+v, want probes/hall.hdr", ov.ReflectionProbe)
	}
	if ov := ed.Doc.Overrides["probe_grid"]; ov.LightProbeGrid == nil || ov.LightProbeGrid.Data != "probes/hall.probes.yaml" {
		t.Fatalf("Overrides[probe_grid].LightProbeGrid = %+v, want probes/hall.probes.yaml", ov.LightProbeGrid)
	}

	// Undo restores the moved light probe grid path in open Doc too.
	if !ed.Undo() {
		t.Fatal("Undo MoveFile failed")
	}
	if obj, _, _ := ed.FindObject("local_grid"); obj == nil || obj.LightProbeGrid == nil || obj.LightProbeGrid.Data != "probes/room.probes.yaml" {
		t.Fatalf("local_grid.LightProbeGrid after Undo = %+v, want probes/room.probes.yaml", obj)
	}
	var _ = sceneformat.CurrentVersion
}
