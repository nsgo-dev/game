package editorcmd_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/datatable"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type dtReviewSeed struct {
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

var dtReviewShotItems = []dtReviewSeed{
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

func buildShotItemsYAML(proposed bool) []byte {
	var b strings.Builder
	b.WriteString("version: 1\n")
	b.WriteString("rows:\n")
	for _, it := range dtReviewShotItems {
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

// TestDataTableReviewMacOSScreenshots renders the full-window row-by-row Data Table review screen
// on native macOS in Dark and Light appearances beside docs/design/editor/ReviewData.dc.html (#1033).
func TestDataTableReviewMacOSScreenshots(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte("version: 1\nname: dungeon-run\nid: dev.nsgo.dungeonrun\nappVersion: 0.1.0\nwindow:\n  width: 320\n  height: 180\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	itemsPath := filepath.Join(dataDir, "items.yaml")
	shop5Path := filepath.Join(dataDir, "shop_floor5.yaml")
	origBytes := buildShotItemsYAML(false)
	if err := os.WriteFile(itemsPath, origBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetLanguage(editor.LanguageEnglish)
	ed.SetAppearance(editor.AppearanceDark)
	if err := ed.OpenProject(filepath.Join(dir, project.FileName)); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	baseDoc, err := datatable.ParseDocument("data/items.yaml", origBytes)
	if err != nil {
		t.Fatal(err)
	}
	propDoc, err := datatable.ParseDocument("data/items.yaml", buildShotItemsYAML(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenDataTableReview(editor.DataTableReviewRequest{
		Prompt: "Rebalance the shop so nothing on floors 1–3 takes more than 12 minutes of coins to buy.",
		Files: []editor.DataTableReviewFileSpec{
			{Path: itemsPath, RelPath: "data/items.yaml", Base: baseDoc, Proposed: propDoc},
			{Path: shop5Path, RelPath: "data/shop_floor5.yaml", Proposed: datatable.NewDocument(), IsNew: true},
		},
		Checks: []editor.DataTableReviewCheck{
			{Label: "Over 12 min, floors 1–3", Value: "none", Before: "9", Basis: "Coin rate from 20 playtests of floor 3: 30 coins a minute."},
			{Label: "Slowest to buy", Value: "Plate armor · 11.7 min", Basis: "Coin rate from 20 playtests of floor 3: 30 coins a minute."},
			{Label: "Median price", Value: "−20%"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	_ = ed.RevertDataTableReviewRecord("war_axe")
	_ = ed.RevertDataTableReviewRecord("torch")
	_ = ed.SetDataTableReviewCellText("knight_blade", "price", "340")
	_ = ed.SetDataTableReviewChecks(ed.DataTableReviewChecks())
	ed.FocusDataTableReviewRecord("plate_armor")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))
	app.Settle()

	// 1. Dark appearance matching ReviewData.dc.html.
	app.Screenshot("dt-review-dark-open")

	// 2. Sort by PRICE Δ ascending and revert iron_sword to show Out of date Agent's check badge.
	app.Press("id:dt-review-sort-price")
	app.Press("id:dt-review-toggle-iron_sword")
	app.Settle()
	app.Screenshot("dt-review-dark-stale-sorted")

	// 3. Light appearance parity.
	app.Post(func() { _ = ed.SetAppearance(editor.AppearanceLight) })
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	})
	app.Settle()
	app.Screenshot("dt-review-light-open")

	// 4. Empty new file state ("No records — ask the agent to add some").
	app.Post(func() { _ = ed.SetAppearance(editor.AppearanceDark) })
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	app.Press("id:dt-review-file-1")
	app.Settle()
	app.Screenshot("dt-review-dark-empty")
}
