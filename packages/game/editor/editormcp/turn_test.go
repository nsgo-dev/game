package editormcp_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// Acceptance #1 & #5:
// A test runs a turn through the MCP tools that adds three objects, changes a field and removes an
// object across two scenes, reverts one change, keeps, and checks both scene files' YAML.
// One Undo restores both files byte for byte and Redo brings back the kept state.
// The MCP read of the turn reports the reverted change; there is no MCP tool that keeps or reverts.
func TestTurnAcrossTwoScenesRevertOneKeepUndoRedo(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	roomDiskPath := f.scenePath()
	otherDiskPath := filepath.Join(f.dir, "scenes", "other.scene.yaml")

	roomDiskBefore, err := os.ReadFile(roomDiskPath)
	if err != nil {
		t.Fatal(err)
	}
	otherDiskBefore, err := os.ReadFile(otherDiskPath)
	if err != nil {
		t.Fatal(err)
	}
	roomYAMLBefore, err := f.ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	otherParsedBefore, err := sceneformat.ParseWith(otherDiskBefore, sceneformat.ParseOptions{File: otherDiskPath, Components: f.ed.ComponentRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	otherYAMLBefore, err := sceneformat.Encode(otherParsedBefore)
	if err != nil {
		t.Fatal(err)
	}

	histBefore := f.ed.History.Index()

	// Verify there is NO MCP tool that keeps or reverts an agent turn.
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(tool.Name)
		if strings.HasPrefix(lower, "turn_") && (strings.Contains(lower, "keep") || strings.Contains(lower, "revert")) {
			t.Fatalf("MCP server must not expose a tool that keeps or reverts an agent turn: %s", tool.Name)
		}
	}

	// Start turn through MCP.
	mustOK(t, call(t, cs, editormcp.ToolTurnStart, map[string]any{
		"prompt": "Place slimes across room and other, adjust hero z, remove ghost",
		"step":   "Read scenes/room.scene.yaml and scenes/other.scene.yaml",
	}))

	// Scene 1 (open active scene): add two objects (slime_01, slime_02) and change hero z 0 -> 12 with why.
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"why": "Add lower-floor patrol slimes and raise hero z above them",
		"edits": []any{
			map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "slime_01", "x": 120, "y": 80, "sprite": map[string]any{"atlas": "actors", "frame": "slime"}}, "why": "First patrol slime"},
			map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "slime_02", "x": 180, "y": 80, "sprite": map[string]any{"atlas": "actors", "frame": "slime"}}, "why": "Second patrol slime"},
			map[string]any{"op": "set", "id": "hero", "fields": map[string]any{"z": 12}, "why": "Draw hero in front of slimes"},
		},
	}))

	// Scene 2 (second scene in project, not currently active): add third object (slime_03) and remove ghost.
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"file": "scenes/other.scene.yaml",
		"why":  "Replace ghost with slime_03 in second room",
		"edits": []any{
			map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "slime_03", "x": 96, "y": 64, "sprite": map[string]any{"atlas": "actors", "frame": "slime"}}, "why": "Guard slime in other room"},
			map[string]any{"op": "remove", "id": "ghost", "why": "Remove placeholder ghost"},
		},
	}))

	// End turn through MCP.
	endRes := call(t, cs, editormcp.ToolTurnEnd, map[string]any{
		"summary": "Added 3 slimes, raised hero z to 12, and removed ghost.",
		"step":    "Updated room and other scenes",
	})
	mustOK(t, endRes)

	// Nothing has been written to disk while pending!
	if got, _ := os.ReadFile(roomDiskPath); !bytes.Equal(got, roomDiskBefore) {
		t.Fatal("room.scene.yaml was written to disk before Keep and Save")
	}
	if got, _ := os.ReadFile(otherDiskPath); !bytes.Equal(got, otherDiskBefore) {
		t.Fatal("other.scene.yaml was written to disk before Keep and Save")
	}
	// History has NOT grown yet while pending!
	if f.ed.History.Index() != histBefore {
		t.Fatalf("History index grew before Keep: got %d, want %d", f.ed.History.Index(), histBefore)
	}
	// Active scene reflects edits live in memory immediately.
	if obj, _, _ := f.ed.FindObject("slime_01"); obj == nil {
		t.Fatal("slime_01 missing from live in-memory active scene")
	}
	if obj, _, _ := f.ed.FindObject("slime_02"); obj == nil {
		t.Fatal("slime_02 missing from live in-memory active scene")
	}
	if hero, _, _ := f.ed.FindObject("hero"); hero == nil || hero.Z != 12 {
		t.Fatalf("hero z = %+v, want 12 live in memory", hero)
	}
	// Second scene is readable via scene_read with live in-memory changes.
	otherRead := mustOK(t, call(t, cs, editormcp.ToolSceneRead, map[string]any{"file": "scenes/other.scene.yaml"}))
	if !strings.Contains(otherRead, "slime_03") || strings.Contains(otherRead, "id: ghost") {
		t.Fatalf("scene_read of other.scene.yaml did not reflect live in-memory turn edits:\n%s", otherRead)
	}

	// Read turn via MCP and verify derived structural diff + reasons + lines.
	var st editormcp.TurnStatus
	structured(t, call(t, cs, editormcp.ToolTurnStatus, map[string]any{}), &st)
	if !st.HasTurn || st.Turn == nil || st.Turn.Status != editor.AgentTurnPending {
		t.Fatalf("turn_status = %+v, want pending turn", st)
	}
	if len(st.Turn.Files) != 2 || st.Turn.TotalChanges != 5 || st.Turn.ActiveChanges != 5 {
		t.Fatalf("expected 5 changes across 2 files, got %+v", st.Turn)
	}

	// Find slime_02 change ID and verify hero z detail + why.
	var slime2ID string
	foundHeroZ := false
	for _, ch := range st.Turn.Changes {
		if ch.Line <= 0 {
			t.Errorf("change %s missing 1-based line number", ch.ID)
		}
		if ch.ObjectID == "slime_02" && ch.Kind == editor.AgentChangeAdded {
			slime2ID = ch.ID
			if ch.Reason != "Second patrol slime" {
				t.Errorf("slime_02 reason = %q, want %q", ch.Reason, "Second patrol slime")
			}
		}
		if ch.ObjectID == "hero" && ch.Field == "z" && ch.Kind == editor.AgentChangeChanged {
			foundHeroZ = true
			if ch.Detail != "z 0 → 12" {
				t.Errorf("hero z detail = %q, want %q", ch.Detail, "z 0 → 12")
			}
			if ch.Reason != "Draw hero in front of slimes" {
				t.Errorf("hero z reason = %q, want %q", ch.Reason, "Draw hero in front of slimes")
			}
		}
	}
	if slime2ID == "" || !foundHeroZ {
		t.Fatalf("missing expected changes in %+v", st.Turn.Changes)
	}

	// Person reverts ONE change (slime_02) while pending.
	if err := f.ed.RevertAgentTurnChange(slime2ID); err != nil {
		t.Fatalf("RevertAgentTurnChange: %v", err)
	}
	if obj, _, _ := f.ed.FindObject("slime_02"); obj != nil {
		t.Fatal("reverted slime_02 is still present in live scene")
	}
	// Restore and revert again to prove round-trip fidelity.
	if err := f.ed.RestoreAgentTurnChange(slime2ID); err != nil {
		t.Fatalf("RestoreAgentTurnChange: %v", err)
	}
	if obj, _, _ := f.ed.FindObject("slime_02"); obj == nil {
		t.Fatal("restored slime_02 did not come back")
	}
	if err := f.ed.RevertAgentTurnChange(slime2ID); err != nil {
		t.Fatalf("second RevertAgentTurnChange: %v", err)
	}

	// Acceptance #5: MCP turn_status reports the reverted change!
	var stAfterRevert editormcp.TurnStatus
	resStatus := call(t, cs, editormcp.ToolTurnStatus, map[string]any{})
	statusText := mustOK(t, resStatus)
	structured(t, resStatus, &stAfterRevert)
	if stAfterRevert.Turn.RevertedCount != 1 || stAfterRevert.Turn.ActiveChanges != 4 {
		t.Fatalf("turn_status after revert: %+v", stAfterRevert.Turn)
	}
	if !strings.Contains(statusText, "[reverted]") {
		t.Fatalf("turn_status text missing [reverted] marker:\n%s", statusText)
	}
	var sawReverted bool
	for _, ch := range stAfterRevert.Turn.Changes {
		if ch.ID == slime2ID && ch.Reverted {
			sawReverted = true
		}
	}
	if !sawReverted {
		t.Fatalf("MCP turn_status did not mark %s as reverted", slime2ID)
	}

	// Person clicks Keep: commits remaining 4 changes across BOTH scenes as ONE History step!
	if err := f.ed.KeepAgentTurn(); err != nil {
		t.Fatalf("KeepAgentTurn: %v", err)
	}
	if f.ed.History.Index() != histBefore+1 {
		t.Fatalf("History index after Keep = %d, want %d (one undo step)", f.ed.History.Index(), histBefore+1)
	}
	if f.ed.History.UndoAuthor() != editor.AuthorAgent {
		t.Fatalf("History.UndoAuthor() = %q, want %q", f.ed.History.UndoAuthor(), editor.AuthorAgent)
	}
	if toast, ok := f.ed.ActiveToast(); !ok || toast.ActionLabel != "Undo" {
		t.Fatalf("expected toast with Undo after Keep, got ok=%v toast=%+v", ok, toast)
	}

	// Check both scene files' kept YAML (in memory AND after SaveAll).
	roomKeptYAML, err := f.ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	otherKeptYAML, ok := f.ed.StagedFileYAML("scenes/other.scene.yaml")
	if !ok {
		t.Fatal("expected staged kept YAML for scenes/other.scene.yaml")
	}
	if !strings.Contains(string(roomKeptYAML), "slime_01") || strings.Contains(string(roomKeptYAML), "slime_02") || !strings.Contains(string(roomKeptYAML), "z: 12") {
		t.Fatalf("room kept YAML mismatch:\n%s", roomKeptYAML)
	}
	if !strings.Contains(string(otherKeptYAML), "slime_03") || strings.Contains(string(otherKeptYAML), "id: ghost") {
		t.Fatalf("other kept YAML mismatch:\n%s", otherKeptYAML)
	}

	// Save persists both scenes to disk cleanly.
	if err := f.ed.SaveAll(); err != nil {
		t.Fatalf("SaveAll after Keep: %v", err)
	}
	if diskRoom, _ := os.ReadFile(roomDiskPath); !bytes.Equal(diskRoom, roomKeptYAML) {
		t.Fatalf("saved room.scene.yaml does not match kept YAML")
	}
	if diskOther, _ := os.ReadFile(otherDiskPath); !bytes.Equal(diskOther, otherKeptYAML) {
		t.Fatalf("saved other.scene.yaml does not match kept YAML")
	}

	// ONE Undo restores BOTH scene files byte for byte to pre-turn state!
	if !f.ed.Undo() {
		t.Fatal("Undo returned false")
	}
	if f.ed.History.Index() != histBefore {
		t.Fatalf("History index after one Undo = %d, want %d", f.ed.History.Index(), histBefore)
	}
	roomUndoneYAML, err := f.ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(roomUndoneYAML, roomYAMLBefore) {
		t.Fatalf("room YAML after Undo differs from pre-turn:\ngot:\n%s\nwant:\n%s", roomUndoneYAML, roomYAMLBefore)
	}
	otherUndoneYAML, ok := f.ed.StagedFileYAML("scenes/other.scene.yaml")
	if !ok {
		t.Fatal("expected staged pre-turn YAML for scenes/other.scene.yaml after Undo")
	}
	if !bytes.Equal(otherUndoneYAML, otherDiskBefore) && !bytes.Equal(otherUndoneYAML, otherYAMLBefore) {
		t.Fatalf("other YAML after Undo differs byte for byte from pre-turn:\ngot:\n%s\nwant:\n%s", otherUndoneYAML, otherDiskBefore)
	}
	// Saving after Undo writes both restored files byte-for-byte back to disk too.
	if err := f.ed.SaveAll(); err != nil {
		t.Fatalf("SaveAll after Undo: %v", err)
	}
	if diskOther, _ := os.ReadFile(otherDiskPath); !bytes.Equal(diskOther, otherDiskBefore) {
		t.Fatalf("other.scene.yaml on disk after Undo+Save not byte-for-byte identical:\ngot:\n%s\nwant:\n%s", diskOther, otherDiskBefore)
	}

	// ONE Redo brings back the exact kept state for BOTH scenes!
	if !f.ed.Redo() {
		t.Fatal("Redo returned false")
	}
	roomRedoneYAML, err := f.ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(roomRedoneYAML, roomKeptYAML) {
		t.Fatalf("room YAML after Redo differs from kept state:\ngot:\n%s\nwant:\n%s", roomRedoneYAML, roomKeptYAML)
	}
	otherRedoneYAML, ok := f.ed.StagedFileYAML("scenes/other.scene.yaml")
	if !ok || !bytes.Equal(otherRedoneYAML, otherKeptYAML) {
		t.Fatalf("other YAML after Redo differs from kept state:\ngot (ok=%v):\n%s\nwant:\n%s", ok, otherRedoneYAML, otherKeptYAML)
	}
}

// Acceptance #2 & multi-file kinds (game.yaml, data table, clip, prefab):
// Revert puts every touched file back byte for byte and the toast's Undo brings the turn back as pending.
func TestTurnMultiFileKindsAndRevertToastUndo(t *testing.T) {
	f := openFixture(t)

	// Add a prefab and an animation clip to the fixture project.
	prefabPath := filepath.Join(f.dir, "prefabs", "slime.prefab.yaml")
	if err := os.MkdirAll(filepath.Dir(prefabPath), 0o755); err != nil {
		t.Fatal(err)
	}
	prefabBefore := []byte("version: 2\nname: slime\nlayers:\n  - name: actors\n    objects:\n      - id: slime_root\n        x: 0\n        y: 0\n        components:\n          body:\n            speed: 32\n")
	if err := os.WriteFile(prefabPath, prefabBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	clipPath := filepath.Join(f.dir, "clips", "bob.anim.yaml")
	if err := os.MkdirAll(filepath.Dir(clipPath), 0o755); err != nil {
		t.Fatal(err)
	}
	clipBefore := []byte("type: animation\nversion: 1\nname: bob\nlength: 1\nloop: true\ntracks:\n  - property: y\n    keys:\n      - time: 0\n        value: 0\n      - time: 1\n        value: 4\n")
	if err := os.WriteFile(clipPath, clipBefore, 0o644); err != nil {
		t.Fatal(err)
	}

	s := editormcp.New(f.ed, editormcp.Options{OnPermissionAsk: allowAsk})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	roomBefore, _ := f.ed.SaveYAML()
	gameBefore, _ := os.ReadFile(filepath.Join(f.dir, "game.yaml"))
	itemsBefore, _ := os.ReadFile(filepath.Join(f.dir, "data", "items.yaml"))

	mustOK(t, call(t, cs, editormcp.ToolTurnStart, map[string]any{"prompt": "Tune slime prefab, items table, clip, and game.yaml"}))

	// Edit prefab via scene_edit
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"file": "prefabs/slime.prefab.yaml",
		"why":  "Slow slime patrol speed so the player can jump over",
		"edits": []any{
			map[string]any{"op": "set", "id": "slime_root", "component": "body", "fields": map[string]any{"speed": 24}},
		},
	}))

	// Edit data table via file_edit
	mustOK(t, call(t, cs, editormcp.ToolFileEdit, map[string]any{
		"file": "data/items.yaml",
		"why":  "Buff sword damage and add shield",
		"yaml": "version: 1\nrows:\n  - id: sword\n    damage: 5\n  - id: shield\n    defense: 2\n",
	}))

	// Edit game.yaml via file_edit
	mustOK(t, call(t, cs, editormcp.ToolFileEdit, map[string]any{
		"file": "game.yaml",
		"why":  "Rename game to Dungeon Run",
		"yaml": "version: 1\nname: Dungeon Run\nid: dev.example.fixture\nappVersion: 1.0.0\nwindow:\n  width: 320\n  height: 180\nautoload:\n  mainScene: room\n  scenes:\n    - name: room\n      path: scenes/room.scene.yaml\n    - name: other\n      path: scenes/other.scene.yaml\n",
	}))

	// Edit clip via file_edit
	mustOK(t, call(t, cs, editormcp.ToolFileEdit, map[string]any{
		"file": "clips/bob.anim.yaml",
		"why":  "Double clip length for gentler bob",
		"yaml": "type: animation\nversion: 1\nname: bob\nlength: 2\nloop: true\ntracks:\n  - property: y\n    keys:\n      - time: 0\n        value: 0\n      - time: 2\n        value: 4\n",
	}))

	// Edit active scene too
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{map[string]any{"op": "move", "id": "hero", "x": 100, "y": 40}},
	}))

	mustOK(t, call(t, cs, editormcp.ToolTurnEnd, map[string]any{"summary": "Tuned all 5 files."}))

	var st editormcp.TurnStatus
	structured(t, call(t, cs, editormcp.ToolTurnStatus, map[string]any{}), &st)
	if len(st.Turn.Files) != 5 {
		t.Fatalf("expected 5 touched files (scene, prefab, datatable, game.yaml, clip), got %d: %+v", len(st.Turn.Files), st.Turn.Files)
	}
	if f.ed.Project.Name != "Dungeon Run" {
		t.Fatalf("Project.Name = %q, want Dungeon Run live in memory", f.ed.Project.Name)
	}

	// Zero disk writes before Keep!
	if got, _ := os.ReadFile(filepath.Join(f.dir, "game.yaml")); !bytes.Equal(got, gameBefore) {
		t.Fatal("game.yaml modified on disk before Keep")
	}
	if got, _ := os.ReadFile(filepath.Join(f.dir, "data", "items.yaml")); !bytes.Equal(got, itemsBefore) {
		t.Fatal("data/items.yaml modified on disk before Keep")
	}

	// Revert the entire turn!
	if err := f.ed.RevertAgentTurn(); err != nil {
		t.Fatalf("RevertAgentTurn: %v", err)
	}
	if f.ed.Project.Name != "Fixture" {
		t.Fatalf("Project.Name after Revert = %q, want Fixture", f.ed.Project.Name)
	}
	if roomNow, _ := f.ed.SaveYAML(); !bytes.Equal(roomNow, roomBefore) {
		t.Fatal("active scene not restored byte-for-byte after Revert")
	}
	if _, stillStaged := f.ed.StagedFileYAML("prefabs/slime.prefab.yaml"); stillStaged {
		t.Fatal("prefab still staged after Revert")
	}
	if _, stillStaged := f.ed.StagedFileYAML("data/items.yaml"); stillStaged {
		t.Fatal("data/items.yaml still staged after Revert")
	}
	if _, stillStaged := f.ed.StagedFileYAML("clips/bob.anim.yaml"); stillStaged {
		t.Fatal("clips/bob.anim.yaml still staged after Revert")
	}

	// Toast has Undo action; invoking it brings the turn back as pending!
	toast, ok := f.ed.ActiveToast()
	if !ok || toast.ActionLabel != "Undo" || toast.OnAction == nil {
		t.Fatalf("expected Revert toast with Undo action, got ok=%v toast=%+v", ok, toast)
	}
	toast.OnAction()

	snap, has := f.ed.CurrentAgentTurn()
	if !has || snap.Status != editor.AgentTurnPending || len(snap.Files) != 5 {
		t.Fatalf("after Revert toast Undo: has=%v status=%s files=%d", has, snap.Status, len(snap.Files))
	}
	if f.ed.Project.Name != "Dungeon Run" {
		t.Fatalf("Project.Name after Revert toast Undo = %q, want Dungeon Run", f.ed.Project.Name)
	}
	if data, ok := f.ed.StagedFileYAML("data/items.yaml"); !ok || !strings.Contains(string(data), "shield") {
		t.Fatalf("data/items.yaml not restored as pending: ok=%v\n%s", ok, data)
	}
}

// Acceptance #3 & #4:
// - While a scripted agent works, ⌘Z undoes only the person's own edit.
// - A stopped turn keeps its changes pending and can be kept or reverted.
func TestWorkingTurnUndoOnlyPersonEditAndStoppedTurnPending(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// Scripted agent starts a turn and adds slime_01.
	mustOK(t, call(t, cs, editormcp.ToolTurnStart, map[string]any{"prompt": "Add slimes"}))
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{
			map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "slime_01", "x": 150, "y": 80}},
		},
	}))

	// While the agent is still working, the person moves hero (an untouched object).
	if err := f.ed.MoveObject("hero", 200, 90); err != nil {
		t.Fatal(err)
	}

	// Person presses ⌘Z while agent is working -> undoes ONLY the person's hero move!
	if !f.ed.Undo() {
		t.Fatal("Undo while agent working returned false")
	}
	hero, _, _ := f.ed.FindObject("hero")
	if hero == nil || hero.X != 64 || hero.Y != 40 {
		t.Fatalf("hero after Undo = %+v, want (64, 40)", hero)
	}
	// Agent's slime_01 must still be present!
	if slime, _, _ := f.ed.FindObject("slime_01"); slime == nil {
		t.Fatal("Undo while agent working wrongly undid the agent's slime_01")
	}

	// Now person moves hero again and leaves it, then stops the agent (⌘. / StopAgentTurn).
	if err := f.ed.MoveObject("hero", 210, 95); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.StopAgentTurn(); err != nil {
		t.Fatalf("StopAgentTurn: %v", err)
	}
	snap, ok := f.ed.CurrentAgentTurn()
	if !ok || snap.Status != editor.AgentTurnStopped || !snap.IsPending() || snap.TotalChanges != 1 {
		t.Fatalf("stopped turn snapshot = %+v (ok=%v)", snap, ok)
	}
	// Person's hero edit is NOT listed as an agent change!
	if slices.ContainsFunc(snap.Changes, func(c editor.AgentTurnChange) bool { return c.ObjectID == "hero" }) {
		t.Fatalf("person's edit on hero leaked into agent turn diff: %+v", snap.Changes)
	}

	// Reverting the stopped turn removes slime_01 while preserving the person's own edit on hero!
	if err := f.ed.RevertAgentTurn(); err != nil {
		t.Fatalf("RevertAgentTurn on stopped turn: %v", err)
	}
	if slime, _, _ := f.ed.FindObject("slime_01"); slime != nil {
		t.Fatal("slime_01 still present after reverting stopped turn")
	}
	hero, _, _ = f.ed.FindObject("hero")
	if hero == nil || hero.X != 210 || hero.Y != 95 {
		t.Fatalf("person's untouched hero edit lost after reverting stopped turn: %+v", hero)
	}

	// Undo revert toast brings the stopped turn back as pending, and it can be Kept!
	toast, ok := f.ed.ActiveToast()
	if !ok || toast.OnAction == nil {
		t.Fatal("expected revert toast")
	}
	toast.OnAction()
	if err := f.ed.KeepAgentTurn(); err != nil {
		t.Fatalf("KeepAgentTurn on restored stopped turn: %v", err)
	}
	if slime, _, _ := f.ed.FindObject("slime_01"); slime == nil {
		t.Fatal("slime_01 missing after keeping stopped turn")
	}
	if f.ed.History.UndoAuthor() != editor.AuthorAgent {
		t.Fatalf("kept stopped turn UndoAuthor = %q, want agent", f.ed.History.UndoAuthor())
	}
}
