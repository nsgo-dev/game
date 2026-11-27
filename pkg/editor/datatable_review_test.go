package editor_test

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/datatable"
	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type shopItemSeed struct {
	id    string
	name  string
	kind  string
	floor int
	p0    int
	p1    int
	w0    int
	w1    int
	d0    string
	d1    string
}

// 40 shop items matching design/editor/ReviewData.dc.html (27 of 40 changed).
var reviewShopItems = []shopItemSeed{
	{"rusty_sword", "Rusty sword", "weapon", 1, 40, 40, 2, 2, "6", "6"},
	{"dagger", "Dagger", "weapon", 1, 60, 60, 3, 3, "5", "5"},
	{"iron_sword", "Iron sword", "weapon", 1, 120, 90, 4, 4, "3", "3.5"},
	{"short_bow", "Short bow", "weapon", 1, 150, 120, 3, 3, "3", "3"},
	{"steel_sword", "Steel sword", "weapon", 2, 260, 190, 6, 6, "2", "2.5"},
	{"war_axe", "War axe", "weapon", 2, 300, 240, 8, 7, "1.5", "1.5"},
	{"long_bow", "Long bow", "weapon", 2, 340, 260, 6, 6, "1.5", "2"},
	{"crossbow", "Crossbow", "weapon", 3, 480, 340, 8, 8, "1", "1.5"},
	{"knight_blade", "Knight's blade", "weapon", 3, 520, 350, 9, 9, "1", "1"},
	{"flame_sword", "Flame sword", "weapon", 4, 900, 900, 12, 12, "0.5", "0.5"},
	{"frost_spear", "Frost spear", "weapon", 4, 860, 860, 11, 11, "0.5", "0.5"},
	{"leather_cap", "Leather cap", "armor", 1, 30, 30, 1, 1, "6", "6"},
	{"leather_vest", "Leather vest", "armor", 1, 80, 70, 2, 2, "4", "4"},
	{"iron_helm", "Iron helm", "armor", 2, 160, 130, 3, 3, "2.5", "2.5"},
	{"chainmail", "Chainmail", "armor", 2, 280, 220, 5, 5, "1.5", "2"},
	{"iron_shield", "Iron shield", "armor", 2, 220, 180, 4, 4, "2", "2"},
	{"plate_armor", "Plate armor", "armor", 3, 600, 350, 8, 7, "0.5", "1"},
	{"tower_shield", "Tower shield", "armor", 3, 450, 330, 6, 6, "1", "1"},
	{"dragon_scale", "Dragon scale", "armor", 5, 1400, 1400, 12, 12, "0.2", "0.2"},
	{"small_potion", "Small potion", "potion", 1, 20, 20, 2, 2, "12", "12"},
	{"potion", "Potion", "potion", 1, 45, 35, 4, 4, "8", "9"},
	{"big_potion", "Big potion", "potion", 2, 90, 70, 6, 6, "4", "5"},
	{"elixir", "Elixir", "potion", 3, 400, 300, 12, 12, "1", "1.5"},
	{"antidote", "Antidote", "potion", 2, 35, 35, 0, 0, "6", "6"},
	{"bomb", "Bomb", "tool", 1, 50, 40, 5, 5, "5", "5"},
	{"rope", "Rope", "tool", 1, 25, 25, 0, 0, "7", "7"},
	{"lantern", "Lantern", "tool", 2, 140, 110, 0, 0, "2", "2"},
	{"lockpick", "Lockpick", "tool", 2, 70, 60, 0, 0, "4", "4"},
	{"grapple", "Grappling hook", "tool", 3, 380, 320, 0, 0, "0.5", "1"},
	{"bomb_bag", "Bomb bag", "tool", 3, 420, 340, 0, 0, "0.5", "0.5"},
	{"lucky_coin", "Lucky coin", "trinket", 1, 100, 90, 1, 1, "2", "2"},
	{"feather", "Feather", "trinket", 2, 240, 200, 1, 1, "1", "1.5"},
	{"dash_boots", "Dash boots", "trinket", 3, 560, 345, 1, 1, "0.3", "0.6"},
	{"ring_of_life", "Ring of life", "trinket", 3, 500, 350, 1, 1, "0.3", "0.3"},
	{"guard_amulet", "Guard amulet", "trinket", 4, 800, 800, 2, 2, "0.2", "0.2"},
	{"bronze_key", "Bronze key", "key", 1, 0, 0, 0, 0, "100", "100"},
	{"silver_key", "Silver key", "key", 3, 0, 0, 0, 0, "100", "100"},
	{"floor3_map", "Floor 3 map", "tool", 3, 150, 120, 0, 0, "0", "0"},
	{"crown", "Crown of the deep", "trinket", 5, 2000, 2000, 0, 0, "0", "0"},
	{"torch", "Torch", "tool", 1, 15, 15, 0, 0, "9", "10"},
}

func buildShopItemsYAML(proposed bool) []byte {
	var b strings.Builder
	b.WriteString("# Dungeon shop economy table — keep comments intact!\n")
	b.WriteString("version: 1\n")
	b.WriteString("rows:\n")
	for _, it := range reviewShopItems {
		price, power, drop := it.p0, it.w0, it.d0
		if proposed {
			price, power, drop = it.p1, it.w1, it.d1
		}
		fmt.Fprintf(&b, "  - id: %s\n", it.id)
		fmt.Fprintf(&b, "    name: %q\n", it.name)
		fmt.Fprintf(&b, "    kind: %s\n", it.kind)
		fmt.Fprintf(&b, "    floor: %d\n", it.floor)
		fmt.Fprintf(&b, "    price: %d\n", price)
		fmt.Fprintf(&b, "    power: %d\n", power)
		fmt.Fprintf(&b, "    drop: %s\n", drop)
	}
	return []byte(b.String())
}

func openShopReviewFixture(t *testing.T) (*editor.Editor, string, string, []byte) {
	t.Helper()
	dir := t.TempDir()
	scenesDir := filepath.Join(dir, "scenes")
	if err := os.MkdirAll(scenesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenesDir, "level_01.scene.yaml"), []byte("version: 2\nname: level_01\nvirtualWidth: 320\nvirtualHeight: 180\nlayers:\n  - name: main\n    objects: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte("version: 1\nname: dungeon-run\nid: dev.nsgo.dungeonrun\nappVersion: 0.1.0\nwindow:\n  width: 320\n  height: 180\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	itemsPath := filepath.Join(dataDir, "items.yaml")
	shop5Path := filepath.Join(dataDir, "shop_floor5.yaml")
	origBytes := buildShopItemsYAML(false)
	if err := os.WriteFile(itemsPath, origBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetLanguage(editor.LanguageEnglish)
	ed.SetAppearance(editor.AppearanceDark)
	if err := ed.OpenProject(filepath.Join(dir, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	baseDoc, err := datatable.ParseDocument("data/items.yaml", origBytes)
	if err != nil {
		t.Fatalf("ParseDocument(base): %v", err)
	}
	propDoc, err := datatable.ParseDocument("data/items.yaml", buildShopItemsYAML(true))
	if err != nil {
		t.Fatalf("ParseDocument(proposed): %v", err)
	}
	emptyDoc := datatable.NewDocument()

	if err := ed.OpenDataTableReview(editor.DataTableReviewRequest{
		Prompt: "Rebalance the shop so nothing on floors 1–3 takes more than 12 minutes of coins to buy.",
		Files: []editor.DataTableReviewFileSpec{
			{
				Path:     itemsPath,
				RelPath:  "data/items.yaml",
				Base:     baseDoc,
				Proposed: propDoc,
			},
			{
				Path:     shop5Path,
				RelPath:  "data/shop_floor5.yaml",
				Proposed: emptyDoc,
				IsNew:    true,
			},
		},
		Checks: []editor.DataTableReviewCheck{
			{
				Label:  "Over 12 min, floors 1–3",
				Value:  "none",
				Before: "9",
				Basis:  "Coin rate from 20 playtests of floor 3: 30 coins a minute.",
			},
			{
				Label: "Slowest to buy",
				Value: "Plate armor · 11.7 min",
				Basis: "Coin rate from 20 playtests of floor 3: 30 coins a minute.",
			},
			{
				Label: "Median price",
				Value: "−20%",
			},
		},
	}); err != nil {
		t.Fatalf("OpenDataTableReview: %v", err)
	}
	return ed, itemsPath, shop5Path, origBytes
}

// Acceptance criterion 1:
// Opens a turn that changed 27 of 40 records, sorts by price Δ, reverts two records, edits one value,
// keeps, and checks that the saved file differs from the original only on the kept and edited lines,
// and that one Undo restores it byte for byte.
func TestDataTableReviewSortRevertEditKeepLosslessAndSingleUndo(t *testing.T) {
	ed, itemsPath, shop5Path, origBytes := openShopReviewFixture(t)

	if !ed.DataTableReviewOpen() || !ed.HasOpenReview() {
		t.Fatal("expected DataTableReviewOpen and HasOpenReview to be true")
	}

	// Verify file metas & chip counts.
	files := ed.DataTableReviewFiles()
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
	if files[0].Meta != "27 of 40 changed" {
		t.Fatalf("files[0].Meta = %q, want '27 of 40 changed'", files[0].Meta)
	}
	if files[1].Meta != "new · 0 records" {
		t.Fatalf("files[1].Meta = %q, want 'new · 0 records'", files[1].Meta)
	}
	all, chg, unchg, rev, yours := ed.DataTableReviewChipCounts()
	if all != 40 || chg != 27 || unchg != 13 || rev != 0 || yours != 0 {
		t.Fatalf("chip counts = (%d,%d,%d,%d,%d), want (40,27,13,0,0)", all, chg, unchg, rev, yours)
	}

	// Verify zero numeric value displays as em dash "—".
	if got := editor.FormatReviewCellValue(datatable.IntValue(0)); got != "—" {
		t.Fatalf("FormatReviewCellValue(0) = %q, want '—'", got)
	}
	if got := editor.FormatReviewCellValue(datatable.FloatValue(0)); got != "—" {
		t.Fatalf("FormatReviewCellValue(0.0) = %q, want '—'", got)
	}

	// Cycle sort on price (PRICE Δ): ascending -> descending -> none (file order).
	if got := ed.DataTableReviewAriaSort("price"); got != "none" {
		t.Fatalf("initial aria-sort = %q, want none", got)
	}
	ed.CycleDataTableReviewSort("price")
	if got := ed.DataTableReviewAriaSort("price"); got != "ascending" {
		t.Fatalf("after 1st click aria-sort = %q, want ascending", got)
	}
	ascRows := ed.DataTableReviewVisibleRecords()
	// Plate armor has the steepest price drop: 600 -> 350 = -42%.
	if ascRows[0].Key != "plate_armor" || ascRows[0].Cells["price"].DeltaPct != -42 {
		t.Fatalf("asc first row = %s (delta %d%%), want plate_armor (-42%%)", ascRows[0].Key, ascRows[0].Cells["price"].DeltaPct)
	}

	ed.CycleDataTableReviewSort("price")
	if got := ed.DataTableReviewAriaSort("price"); got != "descending" {
		t.Fatalf("after 2nd click aria-sort = %q, want descending", got)
	}
	descRows := ed.DataTableReviewVisibleRecords()
	if descRows[len(descRows)-1].Key != "plate_armor" {
		t.Fatalf("desc last row = %s, want plate_armor", descRows[len(descRows)-1].Key)
	}

	ed.CycleDataTableReviewSort("price")
	if got := ed.DataTableReviewAriaSort("price"); got != "none" {
		t.Fatalf("after 3rd click aria-sort = %q, want none", got)
	}
	fileRows := ed.DataTableReviewVisibleRecords()
	if fileRows[0].Key != "rusty_sword" {
		t.Fatalf("file order first row = %s, want rusty_sword", fileRows[0].Key)
	}

	// Sort by price Δ ascending while performing revert & edit (verifying sort never mutates file order).
	ed.CycleDataTableReviewSort("price")

	// Revert two records: war_axe and torch.
	if err := ed.RevertDataTableReviewRecord("war_axe"); err != nil {
		t.Fatalf("Revert(war_axe): %v", err)
	}
	if err := ed.RevertDataTableReviewRecord("torch"); err != nil {
		t.Fatalf("Revert(torch): %v", err)
	}
	kept, total := ed.DataTableReviewKeptCount()
	if kept != 25 || total != 27 {
		t.Fatalf("KeptCount = (%d, %d), want (25, 27)", kept, total)
	}

	// Edit one value in place: knight_blade price 350 -> 340.
	if err := ed.SetDataTableReviewCellText("knight_blade", "price", "340"); err != nil {
		t.Fatalf("SetDataTableReviewCellText: %v", err)
	}
	_, _, _, rev, yours = ed.DataTableReviewChipCounts()
	if rev != 2 || yours != 1 {
		t.Fatalf("reverted=%d yours=%d, want 2 and 1", rev, yours)
	}

	// Filter by Yours -> only knight_blade.
	ed.SetDataTableReviewFilter(editor.DataTableReviewFilterYours)
	yoursRows := ed.DataTableReviewVisibleRecords()
	if len(yoursRows) != 1 || yoursRows[0].Key != "knight_blade" {
		t.Fatalf("Yours rows = %v, want [knight_blade]", yoursRows)
	}
	ed.SetDataTableReviewFilter(editor.DataTableReviewFilterAll)

	// Focused YAML for knight_blade must show - price: 520 and + price: 340 tagged "you".
	ed.FocusDataTableReviewRecord("knight_blade")
	title, yamlLines := ed.DataTableReviewFocusedYAML()
	if title != "knight_blade" {
		t.Fatalf("YAML focus title = %q, want knight_blade", title)
	}
	foundMinus, foundPlusYou := false, false
	for _, ln := range yamlLines {
		if ln.Sign == "-" && strings.Contains(ln.Text, "price: 520") {
			foundMinus = true
		}
		if ln.Sign == "+" && strings.Contains(ln.Text, "price: 340") && ln.Tag == "you" {
			foundPlusYou = true
		}
	}
	if !foundMinus || !foundPlusYou {
		t.Fatalf("expected - price: 520 and + price: 340 tagged 'you', got %+v", yamlLines)
	}

	// Disk file must still be byte-identical to original before Keep!
	preKeepDisk, err := os.ReadFile(itemsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(preKeepDisk, origBytes) {
		t.Fatal("data/items.yaml was modified on disk before Keep")
	}

	// Keep 25 of 27.
	if err := ed.KeepDataTableReview(); err != nil {
		t.Fatalf("KeepDataTableReview: %v", err)
	}
	if ed.DataTableReviewPending() || ed.HasOpenReview() {
		t.Fatal("review should close after Keep")
	}

	// Verify saved data/items.yaml differs from original ONLY on the kept and edited lines!
	savedBytes, err := os.ReadFile(itemsPath)
	if err != nil {
		t.Fatal(err)
	}
	origLines := strings.Split(string(origBytes), "\n")
	savedLines := strings.Split(string(savedBytes), "\n")
	if len(origLines) != len(savedLines) {
		t.Fatalf("line count changed: orig=%d saved=%d", len(origLines), len(savedLines))
	}

	// Build exact set of expected changed 0-based line indices (3 header lines + 7 lines per item).
	revertedSet := map[string]bool{"war_axe": true, "torch": true}
	expectedDiffs := make(map[int]string)
	for i, it := range reviewShopItems {
		if revertedSet[it.id] {
			continue
		}
		baseLine := 3 + i*7
		pNew := it.p1
		if it.id == "knight_blade" {
			pNew = 340
		}
		if it.p0 != pNew {
			expectedDiffs[baseLine+4] = fmt.Sprintf("    price: %d", pNew)
		}
		if it.w0 != it.w1 {
			expectedDiffs[baseLine+5] = fmt.Sprintf("    power: %d", it.w1)
		}
		if it.d0 != it.d1 {
			expectedDiffs[baseLine+6] = fmt.Sprintf("    drop: %s", it.d1)
		}
	}

	for i := range origLines {
		wantChanged, isExpected := expectedDiffs[i]
		if origLines[i] == savedLines[i] {
			if isExpected {
				t.Errorf("line %d unchanged (%q), want %q", i+1, savedLines[i], wantChanged)
			}
			continue
		}
		if !isExpected {
			t.Errorf("unexpected diff on line %d: orig=%q saved=%q", i+1, origLines[i], savedLines[i])
		} else if savedLines[i] != wantChanged {
			t.Errorf("line %d = %q, want %q", i+1, savedLines[i], wantChanged)
		}
	}

	// Newly created empty shop_floor5.yaml should exist on disk.
	if _, err := os.Stat(shop5Path); err != nil {
		t.Fatalf("expected new empty shop_floor5.yaml to be written on Keep: %v", err)
	}

	// ONE Undo must restore data/items.yaml byte for byte and remove shop_floor5.yaml!
	if !ed.History.CanUndo() {
		t.Fatal("expected History.CanUndo after KeepDataTableReview")
	}
	ed.Undo()
	afterUndo, err := os.ReadFile(itemsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterUndo, origBytes) {
		t.Fatal("one Undo did not restore data/items.yaml byte for byte")
	}
	if _, err := os.Stat(shop5Path); !os.IsNotExist(err) {
		t.Fatalf("one Undo should remove newly created shop_floor5.yaml, stat err=%v", err)
	}

	// Redo reapplies both cleanly.
	ed.Redo()
	afterRedo, err := os.ReadFile(itemsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterRedo, savedBytes) {
		t.Fatal("Redo did not restore kept data/items.yaml byte for byte")
	}
}

// Acceptance criterion 2:
// Checks that reverting, editing, or reordering a record marks the agent's figures out of date
// until the agent refreshes them.
func TestDataTableReviewChecksOutOfDateOnRevertEditAndReorder(t *testing.T) {
	ed, _, _, _ := openShopReviewFixture(t)

	if ed.DataTableReviewChecksOutOfDate() {
		t.Fatal("checks should start fresh (not out of date)")
	}
	if len(ed.DataTableReviewChecks()) != 3 {
		t.Fatalf("len(checks) = %d, want 3", len(ed.DataTableReviewChecks()))
	}

	// 1. Reverting a record marks checks out of date.
	if err := ed.RevertDataTableReviewRecord("war_axe"); err != nil {
		t.Fatalf("Revert(war_axe): %v", err)
	}
	if !ed.DataTableReviewChecksOutOfDate() {
		t.Fatal("reverting a record must mark agent's check figures out of date")
	}

	// Agent refreshes checks -> clears out-of-date flag.
	if err := ed.SetDataTableReviewChecks([]editor.DataTableReviewCheck{
		{Label: "Over 12 min, floors 1–3", Value: "1", Before: "9", Basis: "Coin rate from 20 playtests of floor 3: 30 coins a minute."},
	}); err != nil {
		t.Fatalf("SetDataTableReviewChecks: %v", err)
	}
	if ed.DataTableReviewChecksOutOfDate() {
		t.Fatal("refreshing checks should clear out-of-date flag")
	}

	// 2. Editing a value in place marks checks out of date.
	if err := ed.SetDataTableReviewCellText("knight_blade", "price", "340"); err != nil {
		t.Fatalf("SetDataTableReviewCellText: %v", err)
	}
	if !ed.DataTableReviewChecksOutOfDate() {
		t.Fatal("editing a record value must mark agent's check figures out of date")
	}
	_ = ed.SetDataTableReviewChecks(ed.DataTableReviewChecks())
	if ed.DataTableReviewChecksOutOfDate() {
		t.Fatal("refreshing checks after edit should clear out-of-date flag")
	}

	// 3. Reordering a record joins as user edit (Yours + YAML "you" tag) and marks checks out of date.
	if err := ed.ReorderDataTableReviewRecord("iron_sword", 0); err != nil {
		t.Fatalf("ReorderDataTableReviewRecord: %v", err)
	}
	if !ed.DataTableReviewChecksOutOfDate() {
		t.Fatal("reordering a record must mark agent's check figures out of date")
	}
	ed.SetDataTableReviewFilter(editor.DataTableReviewFilterYours)
	yours := ed.DataTableReviewVisibleRecords()
	foundReordered := false
	for _, r := range yours {
		if r.Key == "iron_sword" && r.UserReordered {
			foundReordered = true
		}
	}
	if !foundReordered {
		t.Fatalf("expected reordered iron_sword in Yours chip, got %+v", yours)
	}
	ed.FocusDataTableReviewRecord("iron_sword")
	_, lines := ed.DataTableReviewFocusedYAML()
	if len(lines) == 0 || lines[0].Tag != "you" {
		t.Fatalf("expected reordered iron_sword YAML header line tagged 'you', got %+v", lines)
	}
}

func TestDataTableReviewPauseResumeAndRevertAll(t *testing.T) {
	ed, itemsPath, _, origBytes := openShopReviewFixture(t)

	ed.FocusDataTableReviewRecord("plate_armor")
	ed.SetDataTableReviewFilter(editor.DataTableReviewFilterChanged)
	ed.CycleDataTableReviewSort("price")

	// Pause (Esc) keeps review pending and shows "Review paused" toast with Resume action.
	ed.PauseDataTableReview()
	if ed.DataTableReviewOpen() {
		t.Fatal("expected DataTableReviewOpen false while paused")
	}
	if !ed.DataTableReviewPending() || !ed.DataTableReviewPaused() || !ed.HasOpenReview() {
		t.Fatal("expected paused review to remain pending and pause autosave")
	}
	toast, ok := ed.ActiveToast()
	if !ok || !strings.Contains(toast.Message, "Review paused") || toast.ActionLabel != "Resume" {
		t.Fatalf("paused toast = %+v (ok=%v), want 'Review paused' with 'Resume' action", toast, ok)
	}

	// Invoke toast Resume action -> restores exact state!
	toast.OnAction()
	if !ed.DataTableReviewOpen() || ed.DataTableReviewPaused() {
		t.Fatal("Resume action did not reopen review")
	}
	if ed.DataTableReviewFocusKey() != "plate_armor" || ed.DataTableReviewFilter() != editor.DataTableReviewFilterChanged || ed.DataTableReviewAriaSort("price") != "ascending" {
		t.Fatalf("state not preserved across Pause/Resume: focus=%q filter=%q sort=%q",
			ed.DataTableReviewFocusKey(), ed.DataTableReviewFilter(), ed.DataTableReviewAriaSort("price"))
	}

	// Revert all closes review, keeps original file intact, and Undo brings review back.
	if err := ed.RevertAllDataTableReview(); err != nil {
		t.Fatalf("RevertAllDataTableReview: %v", err)
	}
	if ed.DataTableReviewPending() {
		t.Fatal("review should close after RevertAll")
	}
	if raw, _ := os.ReadFile(itemsPath); !bytes.Equal(raw, origBytes) {
		t.Fatal("RevertAll should not modify disk")
	}
	ed.Undo()
	if !ed.DataTableReviewOpen() {
		t.Fatal("Undo after RevertAll should restore the review")
	}
}

// Acceptance criterion 4:
// Real nsgotest UI verification + macOS screenshots of DataTableReviewView in Dark & Light themes.
func TestDataTableReviewUIAndScreenshots(t *testing.T) {
	ed, itemsPath, _, origBytes := openShopReviewFixture(t)

	// Match ReviewData.dc.html initial state: war_axe & torch reverted, knight_blade edited to 340, plate_armor focused.
	_ = ed.RevertDataTableReviewRecord("war_axe")
	_ = ed.RevertDataTableReviewRecord("torch")
	_ = ed.SetDataTableReviewCellText("knight_blade", "price", "340")
	// Refresh checks so the first open screenshot matches the clean ReviewData.dc.html artboard.
	_ = ed.SetDataTableReviewChecks([]editor.DataTableReviewCheck{
		{
			Label:  "Over 12 min, floors 1–3",
			Value:  "none",
			Before: "9",
			Basis:  "Coin rate from 20 playtests of floor 3: 30 coins a minute.",
		},
		{
			Label: "Slowest to buy",
			Value: "Plate armor · 11.7 min",
			Basis: "Coin rate from 20 playtests of floor 3: 30 coins a minute.",
		},
		{
			Label: "Median price",
			Value: "−20%",
		},
	})
	ed.FocusDataTableReviewRecord("plate_armor")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	// Verify core review nodes mount cleanly.
	for _, id := range []string{
		"id:dt-review-screen",
		"id:dt-review-header",
		"id:dt-review-back",
		"id:dt-review-revert-all",
		"id:dt-review-keep",
		"id:dt-review-sidebar",
		"id:dt-review-file-0",
		"id:dt-review-file-1",
		"id:dt-review-checks-card",
		"id:dt-review-chip-all",
		"id:dt-review-chip-changed",
		"id:dt-review-chip-unchanged",
		"id:dt-review-chip-reverted",
		"id:dt-review-chip-yours",
		"id:dt-review-filter",
		"id:dt-review-table",
		"id:dt-review-sort-price",
		"id:dt-review-yaml-pane",
	} {
		if !app.Exists(id) {
			t.Fatalf("missing expected review UI node %s", id)
		}
	}
	if got := app.Find("id:dt-review-yaml-focus").Text; got != "plate_armor" {
		t.Fatalf("yaml focus = %q, want plate_armor", got)
	}

	// Capture macOS Dark screenshot matching ReviewData.dc.html.
	app.Screenshot("dt-review-dark-open")

	// Click PRICE Δ column header to sort ascending and verify aria-sort.
	app.Press("id:dt-review-sort-price")
	if got := ed.DataTableReviewAriaSort("price"); got != "ascending" {
		t.Fatalf("aria-sort after click = %q, want ascending", got)
	}

	// Click second file (shop_floor5.yaml) -> shows empty state "No records — ask the agent to add some".
	app.Press("id:dt-review-file-1")
	if !app.Exists("id:dt-review-empty") {
		t.Fatal("expected dt-review-empty when viewing empty shop_floor5.yaml")
	}
	app.Press("id:dt-review-file-0")

	// Revert another record (iron_sword) -> marks Agent's check card Out of date!
	app.Press("id:dt-review-toggle-iron_sword")
	if !app.Exists("id:dt-review-checks-stale") {
		t.Fatal("expected dt-review-checks-stale badge after reverting iron_sword")
	}
	app.Screenshot("dt-review-dark-stale")

	// Switch to Light mode and capture Light theme parity screenshot.
	ed.SetAppearance(editor.AppearanceLight)
	app.Find("id:dt-review-screen")
	app.Screenshot("dt-review-light-open")
	ed.SetAppearance(editor.AppearanceDark)
	app.Find("id:dt-review-screen")

	// Press Escape -> pauses review, shows "Review paused" toast, and Agent panel shows Resume review card.
	app.Key("escape")
	if ed.DataTableReviewOpen() || !ed.DataTableReviewPaused() {
		t.Fatal("Escape should pause review")
	}
	if !app.Exists("id:editor-toast") {
		t.Fatal("expected Review paused toast after Escape")
	}
	if !app.Exists("id:right-tab-agent-review-dot") {
		t.Fatal("expected review-waiting dot on Agent tab while review is paused")
	}
	app.Press("id:right-tab-agent")
	if !app.Exists("id:agent-dt-review-resume") {
		t.Fatal("expected agent-dt-review-resume card in Agent panel while review is paused")
	}

	// Resume from Agent panel, then press Keep.
	app.Press("id:agent-dt-review-resume")
	if !ed.DataTableReviewOpen() {
		t.Fatal("pressing Resume review in Agent panel did not reopen review")
	}
	app.Press("id:dt-review-keep")
	if ed.DataTableReviewPending() {
		t.Fatal("pressing Keep should close review")
	}
	// Undo once restores original bytes cleanly.
	ed.Undo()
	if raw, _ := os.ReadFile(itemsPath); !bytes.Equal(raw, origBytes) {
		t.Fatal("Undo after UI Keep did not restore data/items.yaml byte for byte")
	}
}

func TestDataTableReviewHardeningEdgeCases(t *testing.T) {
	// 1. Removed records, reverted added records (lossless fast path), negative/zero/NaN/Inf Δ%, and History integration.
	dir := t.TempDir()
	scenesDir := filepath.Join(dir, "scenes")
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(scenesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenesDir, "level_01.scene.yaml"), []byte("version: 2\nname: level_01\nvirtualWidth: 320\nvirtualHeight: 180\nlayers:\n  - name: main\n    objects: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte("version: 1\nname: edge-test\nid: dev.nsgo.edgetest\nappVersion: 0.1.0\nwindow:\n  width: 320\n  height: 180\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origYAML := `# keep this comment intact
version: 1
rows:
  - id: neg_item
    name: Neg item
    delta: -10
    zero_base: 0
  - id: removed_item
    name: Removed item
    delta: 20
    zero_base: 5
`
	propYAML := `version: 1
rows:
  - id: added_first
    name: Added first
    delta: 99
    zero_base: 1
  - id: neg_item
    name: Neg item
    delta: -5
    zero_base: 50
`
	tblPath := filepath.Join(dataDir, "edge.yaml")
	if err := os.WriteFile(tblPath, []byte(origYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(filepath.Join(dir, "game.yaml")); err != nil {
		t.Fatal(err)
	}

	// Security guards: refuse You-only, .git, game.yaml, and path traversal.
	emptyDoc := datatable.NewDocument()
	for _, bad := range []editor.DataTableReviewFileSpec{
		{Path: filepath.Join(dir, "permissions.yaml"), RelPath: "permissions.yaml", Proposed: emptyDoc, IsNew: true},
		{Path: filepath.Join(dir, ".git", "config"), RelPath: ".git/config", Proposed: emptyDoc, IsNew: true},
		{Path: filepath.Join(dir, "game.yaml"), RelPath: "game.yaml", Proposed: emptyDoc},
		{Path: filepath.Join(dir, "..", "escape.yaml"), RelPath: "../escape.yaml", Proposed: emptyDoc, IsNew: true},
	} {
		if err := ed.OpenDataTableReview(editor.DataTableReviewRequest{Files: []editor.DataTableReviewFileSpec{bad}}); err == nil {
			t.Fatalf("expected OpenDataTableReview to refuse %q", bad.RelPath)
		}
	}

	baseDoc, err := datatable.ParseDocument("data/edge.yaml", []byte(origYAML))
	if err != nil {
		t.Fatal(err)
	}
	propDoc, err := datatable.ParseDocument("data/edge.yaml", []byte(propYAML))
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenDataTableReview(editor.DataTableReviewRequest{
		Prompt: "Edge balance check",
		Files: []editor.DataTableReviewFileSpec{
			{Path: tblPath, RelPath: "data/edge.yaml", Base: baseDoc, Proposed: propDoc},
		},
	}); err != nil {
		t.Fatalf("OpenDataTableReview: %v", err)
	}

	// Verify Δ% for negative base (-10 -> -5 = +50%), zero base (no delta), and removed row cells.
	allRecs := ed.DataTableReviewAllRecords()
	byKey := map[string]editor.DataTableReviewRecord{}
	for _, r := range allRecs {
		byKey[r.Key] = r
	}
	negRec := byKey["neg_item"]
	if !negRec.Cells["delta"].HasDeltaPct || negRec.Cells["delta"].DeltaPct != 50 {
		t.Fatalf("neg_item delta = (%v, %d), want (true, +50)", negRec.Cells["delta"].HasDeltaPct, negRec.Cells["delta"].DeltaPct)
	}
	if negRec.Cells["zero_base"].HasDeltaPct {
		t.Fatalf("zero_base should not have HasDeltaPct when base is 0")
	}
	remRec := byKey["removed_item"]
	if !remRec.Removed || remRec.DisplayName != "Removed item" {
		t.Fatalf("removed_item view = %+v", remRec)
	}
	if !remRec.Cells["delta"].Changed || remRec.Cells["delta"].HasCurrent {
		t.Fatalf("removed_item delta cell = %+v, want Changed=true and HasCurrent=false", remRec.Cells["delta"])
	}

	// FormatReviewDeltaPct must never overflow on math.MinInt.
	if s := editor.FormatReviewDeltaPct(math.MinInt); !strings.HasPrefix(s, "−") || !strings.HasSuffix(s, "%") {
		t.Fatalf("FormatReviewDeltaPct(MinInt) = %q", s)
	}

	// StepDataTableReviewCellNumber rejects NaN/Inf and removed records.
	if err := ed.StepDataTableReviewCellNumber("neg_item", "delta", math.NaN()); err == nil {
		t.Fatal("expected error for NaN step")
	}
	if err := ed.StepDataTableReviewCellNumber("neg_item", "delta", math.Inf(1)); err == nil {
		t.Fatal("expected error for +Inf step")
	}
	if err := ed.StepDataTableReviewCellNumber("removed_item", "delta", 1); err == nil {
		t.Fatal("expected error stepping removed record")
	}

	// Inline edit draft is preserved when validation fails.
	ed.StartDataTableReviewCellEdit("neg_item", "delta")
	if err := ed.SetDataTableReviewCellText("neg_item", "delta", "not_a_number"); err == nil {
		t.Fatal("expected parse error for non-numeric cell text")
	}
	if got := ed.DataTableReviewEditDraft(); got != "not_a_number" {
		t.Fatalf("DataTableReviewEditDraft() = %q, want 'not_a_number'", got)
	}
	ed.FinishDataTableReviewCellEdit()

	// Revert the leading added row AND revert the removed row -> lossless fast path preserves comment byte-for-byte!
	if err := ed.RevertDataTableReviewRecord("added_first"); err != nil {
		t.Fatal(err)
	}
	if err := ed.RevertDataTableReviewRecord("removed_item"); err != nil {
		t.Fatal(err)
	}

	// Play pauses an open review silently and Resume refuses while playing.
	var playScope *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		playScope = cx
		return core.View{}
	}, nsgotest.Size(320, 180))
	var playErr error
	var pausedDuringPlay, reopenedDuringPlay bool
	app.Post(func() {
		playErr = ed.PlayIn(playScope)
		if playErr == nil {
			pausedDuringPlay = !ed.DataTableReviewOpen() && ed.DataTableReviewPaused()
			ed.ResumeDataTableReview()
			reopenedDuringPlay = ed.DataTableReviewOpen()
			playErr = ed.Stop()
		}
	})
	app.Settle()
	if playErr != nil {
		t.Fatalf("PlayIn/Stop: %v", playErr)
	}
	if !pausedDuringPlay {
		t.Fatal("PlayIn should pause open Data Table review")
	}
	if reopenedDuringPlay {
		t.Fatal("ResumeDataTableReview must not reopen while playing")
	}
	ed.ResumeDataTableReview()
	if !ed.DataTableReviewOpen() {
		t.Fatal("ResumeDataTableReview should reopen after Stop")
	}

	// Keep writes only neg_item's changed lines while keeping the header comment intact, and records a History entry.
	if err := ed.KeepDataTableReview(); err != nil {
		t.Fatalf("KeepDataTableReview: %v", err)
	}
	keptBytes, err := os.ReadFile(tblPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(keptBytes), "# keep this comment intact\n") {
		t.Fatalf("expected header comment preserved via lossless fast path, got:\n%s", string(keptBytes))
	}
	if !strings.Contains(string(keptBytes), "    delta: -5\n") || !strings.Contains(string(keptBytes), "    zero_base: 50\n") {
		t.Fatalf("expected kept edits in file, got:\n%s", string(keptBytes))
	}
	entries := ed.HistoryEntries()
	if len(entries) == 0 || entries[0].Author != editor.HistoryAuthorAgent || entries[0].Summary != "Edge balance check" {
		t.Fatalf("expected Agent History timeline entry, got %+v", entries)
	}

	// HistoryPreviewActive blocks opening a new Data Table review.
	if err := ed.PreviewHistoryEntry(entries[0].ID); err != nil {
		t.Fatalf("PreviewHistoryEntry: %v", err)
	}
	if err := ed.OpenDataTableReview(editor.DataTableReviewRequest{
		Files: []editor.DataTableReviewFileSpec{{Path: tblPath, RelPath: "data/edge.yaml", Base: baseDoc, Proposed: propDoc}},
	}); !errors.Is(err, editor.ErrHistoryPreview) {
		t.Fatalf("OpenDataTableReview during history preview = %v, want ErrHistoryPreview", err)
	}
	ed.ExitHistoryPreview()
}
