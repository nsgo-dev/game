package editormcp_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
)

const mcpItemsBefore = `version: 1
rows:
  - id: iron_sword
    name: Iron sword
    floor: 1
    price: 120
    power: 4
  - id: plate_armor
    name: Plate armor
    floor: 3
    price: 600
    power: 8
  - id: torch
    name: Torch
    floor: 1
    price: 15
    power: 0
`

const mcpItemsAfter = `version: 1
rows:
  - id: iron_sword
    name: Iron sword
    floor: 1
    price: 90
    power: 4
  - id: plate_armor
    name: Plate armor
    floor: 3
    price: 350
    power: 7
  - id: torch
    name: Torch
    floor: 1
    price: 15
    power: 0
`

const mcpEmptyShop5 = `version: 1
rows: []
`

func TestDataTableReviewMCPProposeChecksStatusAndRefresh(t *testing.T) {
	f := openFixture(t)
	dataDir := filepath.Join(f.dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	itemsPath := filepath.Join(dataDir, "items.yaml")
	if err := os.WriteFile(itemsPath, []byte(mcpItemsBefore), 0o644); err != nil {
		t.Fatal(err)
	}

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// Invalid YAML is refused without modifying disk or opening a review.
	bad := call(t, cs, editormcp.ToolDataTableReviewPropose, map[string]any{
		"prompt": "bad",
		"files": []map[string]any{
			{"file": "data/items.yaml", "yaml": "version: 1\nunknown: true\n"},
		},
	})
	if !bad.IsError {
		t.Fatal("expected invalid datatable YAML to be refused")
	}
	if f.ed.DataTableReviewPending() {
		t.Fatal("invalid propose should not open review")
	}

	// Propose valid turn with 2 files (1 modified, 1 new empty) and named agent check figures.
	propRes := call(t, cs, editormcp.ToolDataTableReviewPropose, map[string]any{
		"prompt": "Rebalance floor 1–3 shop prices",
		"files": []map[string]any{
			{"file": "data/items.yaml", "yaml": mcpItemsAfter},
			{"file": "data/shop_floor5.yaml", "yaml": mcpEmptyShop5},
		},
		"checks": []map[string]any{
			{
				"label":  "Over 12 min, floors 1–3",
				"value":  "none",
				"before": "1",
				"basis":  "Coin rate from 20 playtests of floor 3: 30 coins a minute.",
			},
		},
	})
	if propRes.IsError {
		raw, _ := json.Marshal(propRes.Content)
		t.Fatalf("datatable_review_propose failed: %s", string(raw))
	}
	if !f.ed.DataTableReviewOpen() {
		t.Fatal("expected DataTableReviewOpen after datatable_review_propose")
	}
	// Disk must NOT be modified before Keep.
	if raw, _ := os.ReadFile(itemsPath); !bytes.Equal(raw, []byte(mcpItemsBefore)) {
		t.Fatal("datatable_review_propose modified file on disk before Keep")
	}

	// Status reports checksOutOfDate=false initially.
	readStatus := func() editormcp.DataTableReviewStatusResult {
		t.Helper()
		res := call(t, cs, editormcp.ToolDataTableReviewStatus, map[string]any{})
		if res.IsError {
			t.Fatalf("datatable_review_status failed: %+v", res.Content)
		}
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var out editormcp.DataTableReviewStatusResult
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	st := readStatus()
	if !st.Open || st.TotalChanged != 2 || st.KeptCount != 2 || st.ChecksOutOfDate {
		t.Fatalf("initial status = %+v", st)
	}

	// Person reverts plate_armor -> status reports checksOutOfDate=true!
	if err := f.ed.RevertDataTableReviewRecord("plate_armor"); err != nil {
		t.Fatalf("Revert(plate_armor): %v", err)
	}
	st = readStatus()
	if !st.ChecksOutOfDate || st.KeptCount != 1 {
		t.Fatalf("status after revert = %+v, want ChecksOutOfDate=true and KeptCount=1", st)
	}

	// Agent refreshes checks via datatable_review_checks -> checksOutOfDate becomes false!
	refRes := call(t, cs, editormcp.ToolDataTableReviewChecks, map[string]any{
		"checks": []map[string]any{
			{
				"label":  "Over 12 min, floors 1–3",
				"value":  "1",
				"before": "1",
				"basis":  "Coin rate from 20 playtests of floor 3: 30 coins a minute.",
			},
		},
	})
	if refRes.IsError {
		t.Fatalf("datatable_review_checks failed: %+v", refRes.Content)
	}
	st = readStatus()
	if st.ChecksOutOfDate || len(st.Checks) != 1 || st.Checks[0].Value != "1" {
		t.Fatalf("status after refresh = %+v", st)
	}

	// Keep writes only iron_sword's changed line, and one Undo restores original bytes!
	if err := f.ed.KeepDataTableReview(); err != nil {
		t.Fatalf("KeepDataTableReview: %v", err)
	}
	saved, _ := os.ReadFile(itemsPath)
	if !bytes.Contains(saved, []byte("price: 90")) || !bytes.Contains(saved, []byte("price: 600")) {
		t.Fatalf("unexpected kept file:\n%s", string(saved))
	}
	f.ed.Undo()
	if undone, _ := os.ReadFile(itemsPath); !bytes.Equal(undone, []byte(mcpItemsBefore)) {
		t.Fatal("one Undo did not restore data/items.yaml byte for byte")
	}
}

func TestDataTableReviewMCPSecurityPermissionsAndFilteredStatus(t *testing.T) {
	f := openFixture(t)
	dataDir := filepath.Join(f.dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	itemsPath := filepath.Join(dataDir, "items.yaml")
	if err := os.WriteFile(itemsPath, []byte(mcpItemsBefore), 0o644); err != nil {
		t.Fatal(err)
	}

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. You-only file (permissions.yaml) must be refused even under Autonomous profile.
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileAutonomous); err != nil {
		t.Fatal(err)
	}
	youOnlyRes := call(t, cs, editormcp.ToolDataTableReviewPropose, map[string]any{
		"prompt": "try modifying permissions",
		"files": []map[string]any{
			{"file": "permissions.yaml", "yaml": mcpEmptyShop5},
		},
	})
	if !youOnlyRes.IsError || !strings.Contains(text(youOnlyRes), "You only") {
		t.Fatalf("expected You-only refusal, got isError=%v text=%q", youOnlyRes.IsError, text(youOnlyRes))
	}

	// 2. Deny edit data/** permission rule must block datatable_review_propose on data/items.yaml
	// (verifies per-file targets resolver evaluates files[] rather than falling back to open scene).
	perms := editor.PermissionPreset(editor.PermissionProfileBalanced)
	perms.Rules = append([]editor.PermissionRule{{
		Action:   editor.PermissionActionEdit,
		Patterns: []string{"data/**"},
		Decision: editor.PermissionDeny,
	}}, perms.Rules...)
	if err := f.ed.SetPermissions(perms); err != nil {
		t.Fatal(err)
	}
	deniedRes := call(t, cs, editormcp.ToolDataTableReviewPropose, map[string]any{
		"prompt": "rebalance",
		"files": []map[string]any{
			{"file": "data/items.yaml", "yaml": mcpItemsAfter},
		},
	})
	if !deniedRes.IsError || !strings.Contains(strings.ToLower(text(deniedRes)), "denied") {
		t.Fatalf("expected permission deny for data/items.yaml, got isError=%v text=%q", deniedRes.IsError, text(deniedRes))
	}
	if f.ed.DataTableReviewPending() {
		t.Fatal("denied propose must not open review")
	}

	// Restore Balanced profile.
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileBalanced); err != nil {
		t.Fatal(err)
	}

	// 3. Symlink escape outside project root must be refused.
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.yaml")
	if err := os.WriteFile(outsideFile, []byte(mcpItemsBefore), 0o644); err != nil {
		t.Fatal(err)
	}
	escapeLink := filepath.Join(dataDir, "escape.yaml")
	if err := os.Symlink(outsideFile, escapeLink); err == nil {
		escRes := call(t, cs, editormcp.ToolDataTableReviewPropose, map[string]any{
			"prompt": "symlink escape",
			"files": []map[string]any{
				{"file": "data/escape.yaml", "yaml": mcpItemsAfter},
			},
		})
		if !escRes.IsError {
			t.Fatalf("expected symlink escape to be refused, got %s", text(escRes))
		}
	}

	// 4. Open valid review, apply a restrictive UI filter & search query, and verify datatable_review_status
	// still returns ALL changed ActiveRecords!
	mustOK(t, call(t, cs, editormcp.ToolDataTableReviewPropose, map[string]any{
		"prompt": "rebalance",
		"files": []map[string]any{
			{"file": "data/items.yaml", "yaml": mcpItemsAfter},
		},
	}))
	f.ed.SetDataTableReviewFilter(editor.DataTableReviewFilterReverted)
	f.ed.SetDataTableReviewSearch("nonexistent_item_xyz")

	statusRes := call(t, cs, editormcp.ToolDataTableReviewStatus, map[string]any{})
	mustOK(t, statusRes)
	var st editormcp.DataTableReviewStatusResult
	structured(t, statusRes, &st)
	if len(st.ActiveRecords) != 2 {
		t.Fatalf("ActiveRecords while UI filtered = %+v, want 2 changed records", st.ActiveRecords)
	}

	// 5. Switching off SceneAndData privacy must refuse datatable_review_status, datatable_review_checks,
	// and datatable_review_propose.
	if err := f.ed.SetAgentPrivacy(editor.PrivacySceneAndData, false); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{editormcp.ToolDataTableReviewStatus, map[string]any{}},
		{editormcp.ToolDataTableReviewChecks, map[string]any{"checks": []map[string]any{{"label": "Check", "value": "ok"}}}},
		{editormcp.ToolDataTableReviewPropose, map[string]any{"files": []map[string]any{{"file": "data/items.yaml", "yaml": mcpItemsAfter}}}},
	} {
		res := call(t, cs, tc.tool, tc.args)
		if !res.IsError {
			t.Fatalf("%s succeeded with SceneAndData disabled; want privacy refusal", tc.tool)
		}
	}
	if err := f.ed.SetAgentPrivacy(editor.PrivacySceneAndData, true); err != nil {
		t.Fatal(err)
	}
}
