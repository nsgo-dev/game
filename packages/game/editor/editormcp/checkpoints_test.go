package editormcp_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
)

// MCP edits record persistent turn checkpoints visible via checkpoint_list and restorable via checkpoint_restore with Undo.
func TestMCPCheckpointsListAndRestore(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	sceneOrig, err := os.ReadFile(f.scenePath())
	if err != nil {
		t.Fatal(err)
	}
	prefabFile := filepath.Join(f.dir, "prefabs", "coin.prefab.yaml")
	if err := os.MkdirAll(filepath.Dir(prefabFile), 0o755); err != nil {
		t.Fatal(err)
	}
	prefabOrig := []byte("version: 1\nname: Coin\n")
	if err := os.WriteFile(prefabFile, prefabOrig, 0o644); err != nil {
		t.Fatal(err)
	}

	// Run an agent turn that calls scene_edit through MCP and edits a prefab.
	if _, err := f.ed.BeginCheckpointTurn("Move hero and update coin"); err != nil {
		t.Fatal(err)
	}
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{
			map[string]any{"op": "move", "id": "hero", "x": 180, "y": 90},
		},
	}))
	if err := f.ed.SaveFile(f.scenePath()); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.WriteAgentFile("prefabs/coin.prefab.yaml", []byte("version: 1\nname: CoinGold\n")); err != nil {
		t.Fatal(err)
	}
	cp, err := f.ed.EndCheckpointTurn("2 files")
	if err != nil || cp == nil {
		t.Fatalf("EndCheckpointTurn: %v, %+v", err, cp)
	}

	// checkpoint_list returns the persistent checkpoint.
	listRes := call(t, cs, editormcp.ToolCheckpointList, map[string]any{"filter": "agent"})
	listText := mustOK(t, listRes)
	var listOut editormcp.CheckpointListResult
	structured(t, listRes, &listOut)
	if len(listOut.Entries) != 1 || listOut.Entries[0].CheckpointID != cp.ID || !strings.Contains(listText, cp.ID) {
		t.Fatalf("checkpoint_list = %+v\n%s", listOut, listText)
	}

	// checkpoint_restore restores both scene and prefab byte-for-byte.
	restRes := call(t, cs, editormcp.ToolCheckpointRestore, map[string]any{"id": cp.ID})
	mustOK(t, restRes)
	var restOut editormcp.CheckpointRestoreResult
	structured(t, restRes, &restOut)
	if restOut.ID != cp.ID || len(restOut.Files) != 2 {
		t.Fatalf("checkpoint_restore = %+v", restOut)
	}
	gotScene, _ := os.ReadFile(f.scenePath())
	gotPrefab, _ := os.ReadFile(prefabFile)
	if !bytes.Equal(gotScene, sceneOrig) || !bytes.Equal(gotPrefab, prefabOrig) {
		t.Fatalf("restored files did not match originals byte-for-byte")
	}

	// Undo puts the turn back!
	if !f.ed.Undo() {
		t.Fatal("Undo returned false")
	}
	if obj, _, _ := f.ed.FindObject("hero"); obj == nil || obj.X != 180 {
		t.Fatalf("hero after Undo = %+v, want X=180", obj)
	}
	gotPrefabAfterUndo, _ := os.ReadFile(prefabFile)
	if string(gotPrefabAfterUndo) != "version: 1\nname: CoinGold\n" {
		t.Fatalf("prefab after Undo = %q", string(gotPrefabAfterUndo))
	}
}

// Permission enforcement: checkpoint_restore checks every file in the checkpoint manifest against
// the project's permission rules (delete -> Deny in Balanced, .go -> Ask in Balanced).
func TestMCPCheckpointRestoreEnforcesPermissions(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// Checkpoint 1 created a new file (existed: false), so restoring deletes it -> Balanced denies delete!
	cpNew, err := f.ed.RunCheckpointTurn("Create sparkle", func() error {
		return f.ed.WriteAgentFile("fx/sparkle.particles.yaml", []byte("version: 1\nname: sparkle\n"))
	})
	if err != nil {
		t.Fatal(err)
	}
	resDel := call(t, cs, editormcp.ToolCheckpointRestore, map[string]any{"id": cpNew.ID})
	if !resDel.IsError {
		t.Fatalf("expected Balanced permission Deny for checkpoint_restore deleting newly created file, got ok")
	}
	if _, err := os.Stat(filepath.Join(f.dir, "fx", "sparkle.particles.yaml")); err != nil {
		t.Fatalf("denied checkpoint_restore must not remove file: %v", err)
	}

	// Checkpoint 2 edited a Go file -> Balanced requires Ask (pending permission error) for code!
	goPath := filepath.Join(f.dir, "scripts", "patrol.go")
	if err := os.MkdirAll(filepath.Dir(goPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goPath, []byte("package scripts\nconst Speed = 10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cpGo, err := f.ed.RunCheckpointTurn("Tune patrol speed", func() error {
		return f.ed.WriteAgentFile("scripts/patrol.go", []byte("package scripts\nconst Speed = 20\n"))
	})
	if err != nil {
		t.Fatal(err)
	}
	resGo := call(t, cs, editormcp.ToolCheckpointRestore, map[string]any{"id": cpGo.ID})
	if !resGo.IsError {
		t.Fatalf("expected Balanced permission Ask for checkpoint_restore touching .go file, got ok")
	}
	if raw, _ := os.ReadFile(goPath); !strings.Contains(string(raw), "Speed = 20") {
		t.Fatalf("blocked checkpoint_restore mutated patrol.go: %q", string(raw))
	}
}
