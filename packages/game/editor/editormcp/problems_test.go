package editormcp_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

const bossDoorsScene = `version: 2
name: Doors
layers:
  - name: actors
    objects:
      - id: door_3
        name: door_3
        x: 420
        "y": 210
        components:
          door: {}
`

const bossRoomScene = `version: 2
name: Boss Room
inherits: scenes/doors.scene.yaml
layers:
  - name: actors
    objects:
      - id: lever
        name: lever
        x: 160
        "y": 240
        components:
          lever: {}
connections:
  - from: lever
    event: pulled
    to: door_3
    handler: open
`

func bossConnectionsRegistry(t *testing.T) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	if err := reg.Register(sceneformat.ComponentType{
		Name:   "lever",
		Events: []sceneformat.Event{{Name: "pulled"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name:     "door",
		Handlers: []sceneformat.Handler{{Name: "open"}},
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func openBossRoomProject(t *testing.T) (*editor.Editor, string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		project.FileName: `version: 1
name: DungeonRun
id: dev.example.dungeonrun
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: boss_room
  scenes:
    - name: boss_room
      path: scenes/boss_room.scene.yaml
`,
		project.NSGoFileName:          "name: DungeonRun\nid: dev.example.dungeonrun\nversion: 1.0.0\n",
		"scenes/doors.scene.yaml":     bossDoorsScene,
		"scenes/boss_room.scene.yaml": bossRoomScene,
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	ed.ProblemCheckDebounce = 15 * time.Millisecond
	t.Cleanup(ed.WaitForProblems)
	if err := ed.SetComponentRegistry(bossConnectionsRegistry(t)); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	if got := ed.Problems(); len(got) != 0 {
		t.Fatalf("clean project has initial problems: %+v", got)
	}
	return ed, dir
}

// TestLiveProblemRenameAgentProposeFixApplyAndUndo verifies the full #1036
// acceptance flow: renaming an object a connection targets surfaces the broken
// connection problem BEFORE saving with its 1-based line and column, the agent
// sees it via game_check and proposes a fix through problem_propose_fix without
// mutating anything, the person applies the fix in the editor as one undo step,
// the resulting YAML points to the renamed object, and ONE Undo brings the
// problem right back.
func TestLiveProblemRenameAgentProposeFixApplyAndUndo(t *testing.T) {
	ed, dir := openBossRoomProject(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// Open scenes/doors.scene.yaml in a tab and rename door_3 -> gate WITHOUT saving!
	if err := ed.OpenScene("scenes/doors.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetObjectID("door_3", "gate"); err != nil {
		t.Fatal(err)
	}
	if !ed.SceneDirty() {
		t.Fatal("expected scenes/doors.scene.yaml to have unsaved edits")
	}
	// Confirm disk still has the old ID (not saved).
	diskDoors, err := os.ReadFile(filepath.Join(dir, "scenes", "doors.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(diskDoors), "id: door_3") {
		t.Fatalf("doors.scene.yaml was unexpectedly written to disk:\n%s", diskDoors)
	}

	// Wait for the automatic debounced live check to finish.
	ed.WaitForProblems()
	probs := ed.Problems()
	if len(probs) != 1 {
		t.Fatalf("live problems before saving = %+v, want 1 broken connection problem", probs)
	}
	p := probs[0]
	if p.File != "scenes/boss_room.scene.yaml" || p.Line <= 0 || p.Column <= 0 || !strings.Contains(p.Message, "door_3") {
		t.Fatalf("problem = %+v, want scenes/boss_room.scene.yaml with Line>0, Column>0, mentioning door_3", p)
	}

	// Agent inspects problems via MCP game_check and gets exact file, line, and column.
	checkRes := call(t, cs, editormcp.ToolGameCheck, map[string]any{})
	checkText := mustOK(t, checkRes)
	var checkOut editormcp.Check
	structured(t, checkRes, &checkOut)
	if len(checkOut.Problems) != 1 {
		t.Fatalf("game_check problems = %+v, want 1", checkOut.Problems)
	}
	cp := checkOut.Problems[0]
	if cp.File != p.File || cp.Line != p.Line || cp.Column != p.Column || !strings.Contains(checkText, p.Location()) {
		t.Fatalf("game_check = %+v (text:\n%s), want %+v", cp, checkText, p)
	}

	// Agent proposes a fix through MCP problem_propose_fix.
	bossDiskBefore, err := os.ReadFile(filepath.Join(dir, "scenes", "boss_room.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	proposeRes := call(t, cs, editormcp.ToolProblemProposeFix, map[string]any{
		"problem": map[string]any{
			"file":    cp.File,
			"line":    cp.Line,
			"column":  cp.Column,
			"message": cp.Message,
		},
		"explanation": "The lever was wired to door_3, which was renamed to gate in your last edit. I can point the connection at gate.",
		"edit": map[string]any{
			"kind":            "update_connection",
			"connectionIndex": 0,
			"to":              "gate",
		},
	})
	proposeText := mustOK(t, proposeRes)
	var propOut editormcp.ProblemProposeFixResult
	structured(t, proposeRes, &propOut)
	if propOut.Applied || propOut.Status != "proposed" {
		t.Fatalf("propose result = %+v, want status=proposed applied=false", propOut)
	}
	if !strings.Contains(proposeText, "- ") || !strings.Contains(proposeText, "to: door_3") || !strings.Contains(proposeText, "+ ") || !strings.Contains(proposeText, "to: gate") {
		t.Fatalf("propose text missing expected diff:\n%s", proposeText)
	}

	// Verify MCP proposal did NOT apply the fix or mutate boss_room.scene.yaml!
	bossDiskAfterPropose, err := os.ReadFile(filepath.Join(dir, "scenes", "boss_room.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(bossDiskAfterPropose) != string(bossDiskBefore) {
		t.Fatal("problem_propose_fix unexpectedly modified boss_room.scene.yaml on disk")
	}
	if ed.ProblemCount() != 1 {
		t.Fatalf("ProblemCount after propose = %d, want 1 (fix must not be applied by MCP)", ed.ProblemCount())
	}
	if !ed.ProblemAgentPanelOpen() {
		t.Fatal("expected Agent problem panel to open when fix is proposed")
	}

	// Person applies the proposed fix in the editor.
	if err := ed.ApplyProposedFix(propOut.Fix.ID); err != nil {
		t.Fatalf("ApplyProposedFix: %v", err)
	}
	ed.WaitForProblems()

	// Check toast with Undo and resulting YAML of boss_room.scene.yaml.
	toast, hasToast := ed.ActiveToast()
	if !hasToast || toast.ActionLabel != "Undo" {
		t.Fatalf("ActiveToast = %+v (ok=%v), want toast with Undo action", toast, hasToast)
	}
	if ed.ScenePath != "scenes/boss_room.scene.yaml" {
		t.Fatalf("active scene after ApplyProposedFix = %q, want scenes/boss_room.scene.yaml", ed.ScenePath)
	}
	fixedYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML after fix: %v", err)
	}
	if !strings.Contains(string(fixedYAML), "to: gate") || strings.Contains(string(fixedYAML), "to: door_3") {
		t.Fatalf("fixed YAML did not update target to gate:\n%s", fixedYAML)
	}
	if got := ed.Problems(); len(got) != 0 {
		t.Fatalf("problems after ApplyProposedFix = %+v, want empty", got)
	}

	// ONE Undo restores to: door_3 and brings the problem right back!
	if !ed.Undo() {
		t.Fatal("Undo returned false")
	}
	ed.WaitForProblems()

	revertedYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML after Undo: %v", err)
	}
	if !strings.Contains(string(revertedYAML), "to: door_3") {
		t.Fatalf("YAML after one Undo did not restore to: door_3:\n%s", revertedYAML)
	}
	afterUndo := ed.Problems()
	if len(afterUndo) != 1 || afterUndo[0].File != "scenes/boss_room.scene.yaml" || afterUndo[0].Line <= 0 || afterUndo[0].Column <= 0 || !strings.Contains(afterUndo[0].Message, "door_3") {
		t.Fatalf("problems after one Undo = %+v, want broken door_3 problem with line and column back", afterUndo)
	}
}

// TestNoMCPToolAppliesAProblemFix verifies the hard safety invariant from #1036:
// no MCP tool applies a fix directly, and problem_propose_fix is read-only and
// rejects any "apply" argument.
func TestNoMCPToolAppliesAProblemFix(t *testing.T) {
	ed, _ := openBossRoomProject(t)
	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	foundPropose := false
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(tool.Name)
		if strings.Contains(lower, "problem") || strings.Contains(lower, "fix") {
			if tool.Name != editormcp.ToolProblemProposeFix {
				t.Fatalf("unexpected problem/fix MCP tool %q: only %q may exist and no MCP tool may apply a fix", tool.Name, editormcp.ToolProblemProposeFix)
			}
			foundPropose = true
			if tool.Meta["nsgo.dev/action"] != string(editormcp.ActionRead) {
				t.Fatalf("%s action = %v, want %q", tool.Name, tool.Meta["nsgo.dev/action"], editormcp.ActionRead)
			}
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("%s must declare ReadOnlyHint=true", tool.Name)
			}
		}
	}
	if !foundPropose {
		t.Fatalf("missing MCP tool %q", editormcp.ToolProblemProposeFix)
	}

	// Unknown "apply": true argument is explicitly refused.
	res := call(t, cs, editormcp.ToolProblemProposeFix, map[string]any{
		"problem":     map[string]any{"file": "scenes/boss_room.scene.yaml"},
		"explanation": "try to apply",
		"edit":        map[string]any{"kind": "update_connection", "connectionIndex": 0, "to": "gate"},
		"apply":       true,
	})
	if !res.IsError {
		t.Fatal("problem_propose_fix accepted an unknown apply flag; expected refusal")
	}
}
