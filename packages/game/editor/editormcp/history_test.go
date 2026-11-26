package editormcp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
)

// #1017: The agent reads recent history (manual edits, agent turns, and saved points) through MCP,
// including author filtering (all, you, agent) and preview state.
func TestHistoryListThroughMCP(t *testing.T) {
	f := openFixture(t)
	now := time.Date(2026, 11, 26, 14, 30, 0, 0, time.UTC)
	f.ed.SetHistoryClock(func() time.Time { return now })

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. Save point at 14:30.
	if err := f.ed.MoveObject("hero", 42, 40); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.Save(); err != nil {
		t.Fatal(err)
	}

	// 2. Manual edit at 14:39.
	now = time.Date(2026, 11, 26, 14, 39, 0, 0, time.UTC)
	if err := f.ed.MoveObject("hero", 88, 40); err != nil {
		t.Fatal(err)
	}
	manualID := f.ed.HistoryEntries()[0].ID

	// 3. Agent turn through scene_edit at 14:41.
	now = time.Date(2026, 11, 26, 14, 41, 0, 0, time.UTC)
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"label": "slimes and coins",
		"edits": []any{
			map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "slime_1", "x": 120, "y": 40}},
			map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "coin_1", "x": 150, "y": 40}},
		},
	}))

	now = time.Date(2026, 11, 26, 14, 43, 0, 0, time.UTC)

	// Read all history entries newest first.
	res := call(t, cs, editormcp.ToolHistoryList, map[string]any{})
	out := mustOK(t, res)
	var all editormcp.HistoryListResult
	structured(t, res, &all)
	if !all.Unsaved || all.Total != 4 || len(all.Entries) != 4 {
		t.Fatalf("history_list(all) = %+v\n%s", all, out)
	}
	if all.Entries[0].Author != "agent" || all.Entries[0].Summary != "slimes and coins" || all.Entries[0].ChangeCount != 2 {
		t.Fatalf("newest entry = %+v", all.Entries[0])
	}
	if all.Entries[1].Author != "you" || !strings.Contains(all.Entries[1].Detail, "42 → 88") {
		t.Fatalf("second entry = %+v", all.Entries[1])
	}
	if all.Entries[2].Kind != "saved" {
		t.Fatalf("third entry = %+v, want saved marker", all.Entries[2])
	}

	// Filter to Agent only: lists only agent turns.
	resAgent := call(t, cs, editormcp.ToolHistoryList, map[string]any{"author": "agent"})
	mustOK(t, resAgent)
	var onlyAgent editormcp.HistoryListResult
	structured(t, resAgent, &onlyAgent)
	if onlyAgent.Total != 1 || len(onlyAgent.Entries) != 1 || onlyAgent.Entries[0].Author != "agent" {
		t.Fatalf("history_list(agent) = %+v", onlyAgent)
	}

	// Filter to You + limit 1.
	resYou := call(t, cs, editormcp.ToolHistoryList, map[string]any{"author": "you", "limit": 1})
	mustOK(t, resYou)
	var onlyYou editormcp.HistoryListResult
	structured(t, resYou, &onlyYou)
	if onlyYou.Total != 2 || len(onlyYou.Entries) != 1 || onlyYou.Entries[0].ID != manualID {
		t.Fatalf("history_list(you, limit=1) = %+v", onlyYou)
	}

	// Previewing an entry is reported in previewingId.
	if err := f.ed.PreviewHistoryEntry(manualID); err != nil {
		t.Fatal(err)
	}
	resPrev := call(t, cs, editormcp.ToolHistoryList, map[string]any{})
	mustOK(t, resPrev)
	var prev editormcp.HistoryListResult
	structured(t, resPrev, &prev)
	if prev.PreviewingID != manualID {
		t.Fatalf("previewingId = %d, want %d", prev.PreviewingID, manualID)
	}
	f.ed.ExitHistoryPreview()

	// Reject unknown author filter cleanly.
	if bad := call(t, cs, editormcp.ToolHistoryList, map[string]any{"author": "bot"}); !bad.IsError {
		t.Fatal("expected error for unknown author filter")
	}
}
