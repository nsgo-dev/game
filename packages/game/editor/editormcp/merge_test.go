package editormcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMergeConflictMCPTools(t *testing.T) {
	f := openFixture(t)

	baseDoc, err := sceneformat.Parse([]byte(`version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    objects:
      - id: door
        name: Door
        x: 584
        y: 260
      - id: knight
        name: Knight
        x: 400
        y: 222
        z: 10
`))
	if err != nil {
		t.Fatal(err)
	}
	mineDoc, err := sceneformat.Parse([]byte(`version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    objects:
      - id: door
        name: Door
        x: 560
        y: 260
        properties:
          open_sfx: audio/door_creak.ogg
      - id: knight
        name: Knight
        x: 400
        y: 222
        z: 12
`))
	if err != nil {
		t.Fatal(err)
	}
	theirsDoc, err := sceneformat.Parse([]byte(`version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    objects:
      - id: door
        name: Door
        x: 600
        y: 260
        properties:
          locked_by: lever_01
      - id: knight
        name: Knight
        x: 400
        y: 222
        z: 11
      - id: lever_01
        x: 360
        y: 238
`))
	if err != nil {
		t.Fatal(err)
	}

	f.ed.MergeCheckRunner = func(dir, scenePath string, doc *sceneformat.SceneDoc) ([]assetcheck.Problem, error) {
		return nil, nil
	}
	f.ed.MergePlaytestRunner = func(dir, scenePath string, doc *sceneformat.SceneDoc) (editor.MergePlaytestResult, error) {
		return editor.MergePlaytestResult{
			Duration: 10 * time.Second,
			Steps:    600,
			Summary:  "Ran game check and a 10 s playtest on the result: no problems.",
		}, nil
	}
	if err := f.ed.OpenMergeConflictSession(editor.MergeConflictSessionConfig{
		ScenePath:   "scenes/level_01.scene.yaml",
		ProjectName: "dungeon-run",
		SceneName:   "level_01",
		Mine:        editor.MergeSideMeta{Branch: "feat/slime-patrol", Author: "you", Commit: "9f3c2d1"},
		Theirs:      editor.MergeSideMeta{Branch: "origin/main", Author: "mia", Commit: "7c21f0e"},
		BaseDoc:     baseDoc,
		MineDoc:     mineDoc,
		TheirsDoc:   theirsDoc,
	}); err != nil {
		t.Fatalf("OpenMergeConflictSession: %v", err)
	}

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. merge_status reports 0 of 2 resolved.
	stRes := call(t, cs, editormcp.ToolMergeStatus, map[string]any{})
	if stRes.IsError || !strings.Contains(text(stRes), "0 of 2 conflicts resolved") {
		t.Fatalf("merge_status = %q (err=%v)", text(stRes), stRes.IsError)
	}

	// 2. merge_resolve can update agent suggestion reason and take all agent suggestions.
	resResolve := call(t, cs, editormcp.ToolMergeResolve, map[string]any{
		"id": "door",
		"suggest": map[string]any{
			"meta":   "fields from both",
			"reason": "Keep Mia's door position and lock, plus your creak sound.",
		},
		"allAgent": true,
	})
	if resResolve.IsError || !strings.Contains(text(resResolve), "Resolved 2 of 2") {
		t.Fatalf("merge_resolve = %q (err=%v)", text(resResolve), resResolve.IsError)
	}

	// 3. merge_finish completes cleanly and preserves door (metadata-only suggest must not delete it).
	finRes := call(t, cs, editormcp.ToolMergeFinish, map[string]any{})
	if finRes.IsError || !strings.Contains(text(finRes), "no problems") {
		t.Fatalf("merge_finish = %q (err=%v)", text(finRes), finRes.IsError)
	}
	doorSurvived := false
	for _, l := range f.ed.Doc.Layers {
		for _, o := range l.Objects {
			if o.ID == "door" {
				doorSurvived = true
			}
		}
	}
	if !doorSurvived {
		t.Fatal("door was deleted after metadata-only suggest + merge_finish")
	}

	// 4. merge_abort closes the conflict session cleanly.
	abortRes := call(t, cs, editormcp.ToolMergeAbort, map[string]any{})
	if abortRes.IsError || f.ed.MergeConflictOpen() {
		t.Fatalf("merge_abort = %q (err=%v open=%v)", text(abortRes), abortRes.IsError, f.ed.MergeConflictOpen())
	}

	// 5. merge_status(open: true) refuses absolute paths, ../ traversal, symlinks leaving project, and Play mode.
	for _, bad := range []string{"/etc/passwd", "../outside.scene.yaml"} {
		res := call(t, cs, editormcp.ToolMergeStatus, map[string]any{"open": true, "file": bad})
		if !res.IsError {
			t.Fatalf("merge_status(open=true, file=%q) succeeded, want error", bad)
		}
	}
	outside := filepath.Join(t.TempDir(), "escape.scene.yaml")
	_ = os.WriteFile(outside, []byte("version: 3\n"), 0o644)
	linkFile := filepath.Join(filepath.Dir(f.ed.ProjectPath), "scenes", "escape_link.scene.yaml")
	if err := os.Symlink(outside, linkFile); err == nil {
		res := call(t, cs, editormcp.ToolMergeStatus, map[string]any{"open": true, "file": "scenes/escape_link.scene.yaml"})
		if !res.IsError || !strings.Contains(text(res), "outside the project") {
			t.Fatalf("merge_status on escaping symlink = %q (err=%v), want outside-project error", text(res), res.IsError)
		}
	}
	var rootScope *core.Scope
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		rootScope = cx
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(800, 600))
	if err := f.ed.PlayIn(rootScope); err != nil {
		t.Fatalf("PlayIn: %v", err)
	}
	playRes := call(t, cs, editormcp.ToolMergeStatus, map[string]any{"open": true})
	if !playRes.IsError {
		t.Fatalf("merge_status(open=true) during Play succeeded, want ErrPlaying error")
	}
	_ = f.ed.Stop()
}
