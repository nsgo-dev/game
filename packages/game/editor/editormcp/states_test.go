package editormcp_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestStatesWriteAndScriptedInputTest(t *testing.T) {
	f := openFixture(t)

	// Write fixture knight clips into the project directory so states_test resolves them from disk.
	clips := map[string]struct {
		frames []string
		ms     int
		loop   bool
	}{
		"anim/knight_idle.animation.yaml":   {[]string{"hero", "other"}, 150, true},
		"anim/knight_run.animation.yaml":    {[]string{"hero", "other"}, 100, true},
		"anim/knight_jump.animation.yaml":   {[]string{"hero", "other"}, 120, false},
		"anim/knight_fall.animation.yaml":   {[]string{"hero", "other"}, 120, true},
		"anim/knight_attack.animation.yaml": {[]string{"hero", "other"}, 80, false},
		"anim/knight_hurt.animation.yaml":   {[]string{"hero", "other"}, 90, false},
	}
	if err := os.MkdirAll(filepath.Join(f.dir, "anim"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, spec := range clips {
		c, err := animclip.FromFrames(filepath.Base(rel), spec.frames, 1000.0/float64(spec.ms), spec.loop)
		if err != nil {
			t.Fatal(err)
		}
		data, err := resource.Encode(c)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.dir, filepath.FromSlash(rel)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	knightStates, err := os.ReadFile(filepath.Join("..", "..", "animclip", "testdata", "knight.states.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. states_write validates, writes canonical YAML to disk, and opens the graph in the editor.
	writeRes := call(t, cs, editormcp.ToolStatesWrite, map[string]any{
		"file":   "anim/knight.states.yaml",
		"yaml":   string(knightStates),
		"target": "hero",
	})
	writeOut := mustOK(t, writeRes)
	var wrote editormcp.StatesWriteResult
	structured(t, writeRes, &wrote)
	if wrote.File != "anim/knight.states.yaml" || wrote.States != 6 || wrote.Transitions != 11 || !wrote.Opened {
		t.Fatalf("states_write = %+v (%s)", wrote, writeOut)
	}
	if f.ed.States() == nil || f.ed.StatesCurrentState() != "idle" {
		t.Fatalf("editor did not open state machine: %+v", f.ed.States())
	}

	// 2. states_test plays 8 s of scripted input and reports "Played 8 s of scripted input · no stuck states".
	testRes := call(t, cs, editormcp.ToolStatesTest, map[string]any{
		"file": "anim/knight.states.yaml",
	})
	testOut := mustOK(t, testRes)
	var report editormcp.StatesTestResult
	structured(t, testRes, &report)
	if report.Summary != "Played 8 s of scripted input · no stuck states" {
		t.Fatalf("Summary = %q, want %q\nfull output:\n%s", report.Summary, "Played 8 s of scripted input · no stuck states", testOut)
	}
	wantPrefix := []string{"idle", "run", "jump", "fall", "run"}
	if len(report.Trace) < len(wantPrefix) || !slices.Equal(report.Trace[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("Trace = %v, want prefix %v", report.Trace, wantPrefix)
	}
	if len(report.StuckStates) != 0 {
		t.Fatalf("StuckStates = %v, want none", report.StuckStates)
	}

	// 3. states_write refuses invalid state machine (missing target state) and leaves file untouched.
	badRes := call(t, cs, editormcp.ToolStatesWrite, map[string]any{
		"file": "anim/bad.states.yaml",
		"yaml": "version: 1\nentry: idle\nstates:\n  - name: idle\n    clip: anim/knight_idle.animation.yaml\ntransitions:\n  - from: idle\n    to: ghost\n    onEnd: true\n",
	})
	if !badRes.IsError {
		t.Fatalf("states_write accepted missing target state: %+v", badRes)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "anim", "bad.states.yaml")); !os.IsNotExist(err) {
		t.Fatalf("refused states_write created anim/bad.states.yaml")
	}

	// 4. states_write refuses paths leaving the project root.
	escRes := call(t, cs, editormcp.ToolStatesWrite, map[string]any{
		"file": "../escape.states.yaml",
		"yaml": string(knightStates),
	})
	if !escRes.IsError || !strings.Contains(text(escRes), "leaves the project") {
		t.Fatalf("states_write escape = %+v", escRes)
	}

	// 5. states_write refuses symlinks escaping the project root without touching outside files.
	outsideDir := t.TempDir()
	linkDir := filepath.Join(f.dir, "anim", "outlink")
	if err := os.Symlink(outsideDir, linkDir); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}
	symRes := call(t, cs, editormcp.ToolStatesWrite, map[string]any{
		"file": "anim/outlink/evil.states.yaml",
		"yaml": string(knightStates),
	})
	if !symRes.IsError || !strings.Contains(text(symRes), "outside the project") {
		t.Fatalf("states_write symlink dir escape = %+v", symRes)
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "evil.states.yaml")); !os.IsNotExist(err) {
		t.Fatalf("symlink escape wrote outside file")
	}

	outsideFile := filepath.Join(outsideDir, "target.states.yaml")
	if err := os.WriteFile(outsideFile, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileLink := filepath.Join(f.dir, "anim", "filelink.states.yaml")
	if err := os.Symlink(outsideFile, fileLink); err != nil {
		t.Fatalf("symlink file: %v", err)
	}
	symFileRes := call(t, cs, editormcp.ToolStatesWrite, map[string]any{
		"file": "anim/filelink.states.yaml",
		"yaml": string(knightStates),
	})
	if !symFileRes.IsError || !strings.Contains(text(symFileRes), "outside the project") {
		t.Fatalf("states_write symlink file escape = %+v", symFileRes)
	}
	if got, err := os.ReadFile(outsideFile); err != nil || string(got) != "untouched" {
		t.Fatalf("outside file modified: %q, err=%v", got, err)
	}

	// 6. states_write refuses during Play mode without modifying the file on disk.
	sentinel := filepath.Join(f.dir, "anim", "knight.states.yaml")
	beforeBytes, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	var rootScope *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		rootScope = cx
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(1120, 720))
	var playErr error
	app.Post(func() { playErr = f.ed.PlayIn(rootScope) })
	app.Settle()
	if playErr != nil {
		t.Fatalf("ed.PlayIn: %v", playErr)
	}
	t.Cleanup(func() {
		app.Post(func() { f.ed.Stop() })
		app.Settle()
	})
	playRes := call(t, cs, editormcp.ToolStatesWrite, map[string]any{
		"file": "anim/knight.states.yaml",
		"yaml": "version: 1\nentry: idle\nstates:\n  - name: idle\n    clip: anim/knight_idle.animation.yaml\n",
	})
	if !playRes.IsError {
		t.Fatalf("states_write accepted write during Play mode: %+v", playRes)
	}
	afterBytes, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterBytes) != string(beforeBytes) {
		t.Fatal("states_write modified file on disk while in Play mode")
	}
	app.Post(func() { f.ed.Stop() })
	app.Settle()

	// 7. states_test validates durationSeconds and negative script times.
	shortRes := call(t, cs, editormcp.ToolStatesTest, map[string]any{
		"file":            "anim/knight.states.yaml",
		"durationSeconds": 0.01,
	})
	if !shortRes.IsError {
		t.Fatalf("states_test accepted durationSeconds=0.01: %+v", shortRes)
	}
	negRes := call(t, cs, editormcp.ToolStatesTest, map[string]any{
		"file":   "anim/knight.states.yaml",
		"script": []map[string]any{{"atMs": -5, "jump": true}},
	})
	if !negRes.IsError {
		t.Fatalf("states_test accepted negative atMs: %+v", negRes)
	}
}
