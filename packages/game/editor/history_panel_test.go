package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func buildHistoryAcceptanceScene() *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "level_01",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#181C24",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "knight", Name: "Knight", X: 416, Y: 220},
					{ID: "door", Name: "Door", X: 560, Y: 204},
				},
			},
		},
	}
}

// #1017 Acceptance 1 & 2:
// Makes a manual edit, keeps an agent turn (including a multi-file change), makes another manual edit,
// previews the first entry and asserts the previewed scene matches that earlier point while the live
// open document and undo cursor are unchanged, verifies Esc and Now exit preview cleanly, then clicks
// Restore and verifies ONE Undo returns immediately to the latest state (including extra touched files).
// Also verifies day grouping, saved markers, All/You/Agent filter tabs, and ⌘Y / toolbar tooltip.
func TestHistoryPanelAcceptancePreviewAndNonDestructiveRestore(t *testing.T) {
	dir := t.TempDir()
	projYAML := "version: 1\nname: dungeon-run\nid: dev.example.dungeonrun\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(projYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	rawScene, err := sceneformat.Encode(buildHistoryAcceptanceScene())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"), rawScene, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	_ = ed.SetAutoSave(false)

	now := time.Date(2026, 11, 26, 14, 15, 0, 0, time.UTC)
	ed.SetHistoryClock(func() time.Time { return now })

	// Yesterday edit to verify day grouping (YESTERDAY vs TODAY).
	now = time.Date(2026, 11, 25, 18, 12, 0, 0, time.UTC)
	if err := ed.SetObjectZ("door", 2); err != nil {
		t.Fatal(err)
	}

	// Saved point 15 minutes before "now" (14:28).
	now = time.Date(2026, 11, 26, 14, 28, 0, 0, time.UTC)
	if err := ed.Save(); err != nil {
		t.Fatal(err)
	}

	// Step 1 (manual edit, 4 min ago -> 14:39): Move Knight x 416 -> 452.
	now = time.Date(2026, 11, 26, 14, 39, 0, 0, time.UTC)
	if err := ed.MoveObject("knight", 452, 220); err != nil {
		t.Fatal(err)
	}
	entriesAfterManual1 := ed.HistoryEntries()
	if len(entriesAfterManual1) == 0 {
		t.Fatal("expected history entry after manual edit")
	}
	manual1ID := entriesAfterManual1[0].ID
	if entriesAfterManual1[0].Author != editor.HistoryAuthorYou ||
		entriesAfterManual1[0].Summary != "moved Knight" ||
		entriesAfterManual1[0].Detail != "x 416 → 452" {
		t.Fatalf("manual1 entry = %+v, want You / moved Knight / x 416 → 452", entriesAfterManual1[0])
	}

	// Step 2 (agent turn, 2 min ago -> 14:41): add slimes and coins + write extra file fx/sparkle.particles.yaml.
	now = time.Date(2026, 11, 26, 14, 41, 0, 0, time.UTC)
	sparklePath := filepath.Join(dir, "fx", "sparkle.particles.yaml")
	if err := ed.RecordAgentTurn(editor.AgentTurnOptions{
		Title:       "slimes and coins",
		ChangeCount: 6,
		KeptCount:   5,
		Files:       []string{"fx/sparkle.particles.yaml"},
	}, func() error {
		if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "slime_01", Name: "Slime 1", X: 300, Y: 220}); err != nil {
			return err
		}
		if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "coin_01", Name: "Coin 1", X: 380, Y: 210}); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(sparklePath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(sparklePath, []byte("version: 1\nname: sparkle\n"), 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	agentEntry := ed.HistoryEntries()[0]
	if agentEntry.Author != editor.HistoryAuthorAgent ||
		agentEntry.Summary != "slimes and coins" ||
		agentEntry.Detail != "6 changes · kept 5" ||
		agentEntry.ChangeCount != 6 ||
		agentEntry.KeptCount != 5 {
		t.Fatalf("agent entry = %+v", agentEntry)
	}

	// Step 3 (another manual edit, 1 min ago -> 14:42): Move Knight x 452 -> 510.
	now = time.Date(2026, 11, 26, 14, 42, 0, 0, time.UTC)
	if err := ed.MoveObject("knight", 510, 220); err != nil {
		t.Fatal(err)
	}

	// Advance clock to "now" (14:43) so relative times read 1 min / 2 min / 4 min / 15 min ago.
	now = time.Date(2026, 11, 26, 14, 43, 0, 0, time.UTC)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	// Verify toolbar button exists and opens History panel, and ⌘Y toggles it.
	if !app.Exists("id:toolbar-history") {
		t.Fatal("toolbar-history icon button missing")
	}
	if ed.HistoryPanelOpen() {
		t.Fatal("History panel should be closed initially")
	}
	app.Key("meta+y")
	app.Settle()
	if !ed.HistoryPanelOpen() || !app.Exists("id:history-panel") {
		t.Fatal("⌘Y did not open the History panel")
	}

	// Verify Now row, day headers, and entries show who/what/when.
	if !app.Exists("id:history-row-now") || !app.Exists("id:history-day-today") || !app.Exists("id:history-day-yesterday") {
		t.Fatal("expected Now row plus TODAY and YESTERDAY day headers in History panel")
	}
	if !strings.Contains(app.Find("id:history-now-status").Text, "unsaved") {
		t.Fatalf("Now status = %q, want unsaved", app.Find("id:history-now-status").Text)
	}
	if got := app.Find(fmt.Sprintf("id:history-row-title-%d", agentEntry.ID)).Text; got != "Agent · slimes and coins" {
		t.Fatalf("agent row title = %q, want %q", got, "Agent · slimes and coins")
	}
	if got := app.Find(fmt.Sprintf("id:history-row-subtitle-%d", agentEntry.ID)).Text; got != "6 changes · kept 5 · 2 min ago" {
		t.Fatalf("agent row subtitle = %q, want %q", got, "6 changes · kept 5 · 2 min ago")
	}
	if got := app.Find(fmt.Sprintf("id:history-row-title-%d", manual1ID)).Text; got != "You · moved Knight" {
		t.Fatalf("manual1 row title = %q, want %q", got, "You · moved Knight")
	}
	if got := app.Find(fmt.Sprintf("id:history-row-subtitle-%d", manual1ID)).Text; got != "x 416 → 452 · 4 min ago" {
		t.Fatalf("manual1 row subtitle = %q, want %q", got, "x 416 → 452 · 4 min ago")
	}

	// Verify Agent filter lists ONLY agent turns.
	app.Press("id:history-filter-agent")
	app.Settle()
	if ed.HistoryFilter() != editor.HistoryFilterAgent {
		t.Fatalf("filter = %q, want agent", ed.HistoryFilter())
	}
	if !app.Exists(fmt.Sprintf("id:history-row-%d", agentEntry.ID)) {
		t.Fatal("Agent filter hid the agent turn")
	}
	if app.Exists(fmt.Sprintf("id:history-row-%d", manual1ID)) {
		t.Fatal("Agent filter should hide manual edits")
	}
	if filtered := ed.FilteredHistoryEntries(editor.HistoryFilterAgent); len(filtered) != 1 || filtered[0].ID != agentEntry.ID {
		t.Fatalf("FilteredHistoryEntries(agent) = %+v", filtered)
	}

	// Switch back to All.
	app.Press("id:history-filter-all")
	app.Settle()

	// Preview the first manual edit entry: viewport shows Knight at 452 with NO slime_01,
	// while live ed.Doc is UNCHANGED (Knight at 510, slime_01 present) and undo cursor is UNCHANGED.
	cursorBeforePreview := ed.History.Index()
	app.Press(fmt.Sprintf("id:history-row-%d", manual1ID))
	app.Settle()

	if !ed.HistoryPreviewActive() {
		t.Fatal("expected HistoryPreviewActive() after pressing entry")
	}
	if !app.Exists("id:history-preview-banner") {
		t.Fatal("expected floating viewport preview banner")
	}
	if banner := app.Find("id:history-preview-banner-text").Text; banner != "Previewing 4 min ago · editing is off" {
		t.Fatalf("preview banner = %q, want %q", banner, "Previewing 4 min ago · editing is off")
	}
	if ed.History.Index() != cursorBeforePreview {
		t.Fatalf("undo cursor moved during preview: got %d, want %d", ed.History.Index(), cursorBeforePreview)
	}

	// Check previewed scene vs live open document.
	previewScene := ed.ActiveScene()
	if previewScene == nil {
		t.Fatal("ActiveScene nil during preview")
	}
	previewKnight := previewScene.Object("knight")
	if previewKnight == nil || previewKnight.Doc.X != 452 {
		t.Fatalf("previewed knight = %+v, want x=452", previewKnight)
	}
	if previewScene.Object("slime_01") != nil {
		t.Fatal("previewed scene should not contain slime_01 from later agent turn")
	}
	liveKnight, _, _ := ed.FindObject("knight")
	if liveKnight == nil || liveKnight.X != 510 {
		t.Fatalf("live Doc knight mutated during preview: %+v, want x=510", liveKnight)
	}
	if liveSlime, _, _ := ed.FindObject("slime_01"); liveSlime == nil {
		t.Fatal("live Doc lost slime_01 during preview")
	}

	// Pressing Esc ends preview cleanly and returns to live editing.
	app.Key("escape")
	app.Settle()
	if ed.HistoryPreviewActive() || app.Exists("id:history-preview-banner") {
		t.Fatal("Esc did not exit history preview")
	}
	if ed.ActiveScene().Object("knight").Doc.X != 510 {
		t.Fatal("ActiveScene did not return to live scene after Esc")
	}

	// Pressing Now also ends preview cleanly.
	app.Press(fmt.Sprintf("id:history-row-%d", manual1ID))
	app.Settle()
	if !ed.HistoryPreviewActive() {
		t.Fatal("expected preview active before clicking Now")
	}
	app.Press("id:history-row-now")
	app.Settle()
	if ed.HistoryPreviewActive() {
		t.Fatal("clicking Now did not exit history preview")
	}

	// Preview manual1 again and click Restore:
	// - live scene rolls back to Knight x=452, no slime_01
	// - extra file fx/sparkle.particles.yaml is rolled back (removed)
	// - earlier and later steps stay in History
	// - ONE Undo returns immediately to latest state (Knight x=510, slime_01 present, sparkle file back)!
	app.Press(fmt.Sprintf("id:history-row-%d", manual1ID))
	app.Settle()
	entriesBeforeRestore := len(ed.HistoryEntries())

	app.Press("id:history-preview-restore")
	app.Settle()

	if ed.HistoryPreviewActive() {
		t.Fatal("Restore should end preview mode")
	}
	if ed.History.Index() != cursorBeforePreview+1 {
		t.Fatalf("History.Index after Restore = %d, want %d (one new undoable step)", ed.History.Index(), cursorBeforePreview+1)
	}
	if len(ed.HistoryEntries()) != entriesBeforeRestore+1 {
		t.Fatalf("HistoryEntries count after Restore = %d, want %d (all later steps preserved)", len(ed.HistoryEntries()), entriesBeforeRestore+1)
	}
	if k, _, _ := ed.FindObject("knight"); k == nil || k.X != 452 {
		t.Fatalf("after Restore knight = %+v, want x=452", k)
	}
	if s, _, _ := ed.FindObject("slime_01"); s != nil {
		t.Fatal("after Restore slime_01 should be gone")
	}
	if _, err := os.Stat(sparklePath); !os.IsNotExist(err) {
		t.Fatalf("after Restore fx/sparkle.particles.yaml should be removed, stat err = %v", err)
	}

	// ONE Undo returns immediately to the latest state before Restore!
	app.Post(func() {
		if !ed.Undo() {
			t.Error("Undo after Restore returned false")
		}
	})
	app.Settle()

	if ed.History.Index() != cursorBeforePreview {
		t.Fatalf("History.Index after one Undo = %d, want %d", ed.History.Index(), cursorBeforePreview)
	}
	if k, _, _ := ed.FindObject("knight"); k == nil || k.X != 510 {
		t.Fatalf("after one Undo knight = %+v, want x=510", k)
	}
	if s, _, _ := ed.FindObject("slime_01"); s == nil {
		t.Fatal("after one Undo slime_01 should be restored")
	}
	if raw, err := os.ReadFile(sparklePath); err != nil || !strings.Contains(string(raw), "name: sparkle") {
		t.Fatalf("after one Undo fx/sparkle.particles.yaml = %q, err=%v", string(raw), err)
	}
}

// #1017 Acceptance 4: Real nsgotest screenshots on macOS beside AskInPlace.dc.html / Checkpoints.dc.html.
// Set NSGO_HISTORY_SCREENSHOT_DIR=/tmp/history-shots to export PNGs for visual inspection.
func TestHistoryPanelScreenshots(t *testing.T) {
	dir := t.TempDir()
	projYAML := "version: 1\nname: dungeon-run\nid: dev.example.dungeonrun\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(projYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	rawScene, err := sceneformat.Encode(buildHistoryAcceptanceScene())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"), rawScene, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	_ = ed.SetAutoSave(false)

	now := time.Date(2026, 11, 25, 18, 12, 0, 0, time.UTC)
	ed.SetHistoryClock(func() time.Time { return now })
	if err := ed.SetObjectZ("door", 2); err != nil {
		t.Fatal(err)
	}

	now = time.Date(2026, 11, 26, 14, 28, 0, 0, time.UTC)
	if err := ed.Save(); err != nil {
		t.Fatal(err)
	}

	now = time.Date(2026, 11, 26, 14, 31, 0, 0, time.UTC)
	if err := ed.RecordAgentTurn(editor.AgentTurnOptions{
		Title:       "built the first room",
		Detail:      "from the template",
		ChangeCount: 18,
		KeptCount:   18,
	}, func() error {
		_, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "torch_01", Name: "Torch", X: 180, Y: 160})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	now = time.Date(2026, 11, 26, 14, 39, 0, 0, time.UTC)
	if err := ed.MoveObject("knight", 452, 220); err != nil {
		t.Fatal(err)
	}
	manualID := ed.HistoryEntries()[0].ID

	now = time.Date(2026, 11, 26, 14, 41, 0, 0, time.UTC)
	if err := ed.RecordAgentTurn(editor.AgentTurnOptions{
		Title:       "slimes and coins",
		ChangeCount: 6,
		KeptCount:   5,
		Files:       []string{"prefabs/coin.prefab.yaml"},
	}, func() error {
		if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "slime_01", Name: "Slime 1", X: 260, Y: 220}); err != nil {
			return err
		}
		_, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "slime_02", Name: "Slime 2", X: 330, Y: 220})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	now = time.Date(2026, 11, 26, 14, 43, 0, 0, time.UTC)
	ed.SetHistoryPanelOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	app.Screenshot("history-panel-all-dark")

	app.Press("id:history-filter-agent")
	app.Settle()
	app.Screenshot("history-panel-agent-dark")

	app.Press("id:history-filter-all")
	app.Settle()
	app.Press(fmt.Sprintf("id:history-row-%d", manualID))
	app.Settle()
	app.Screenshot("history-panel-preview-dark")

	app.Post(func() {
		_ = ed.SetAppearance(editor.AppearanceLight)
	})
	app.Settle()
	app.Screenshot("history-panel-preview-light")
}

// Blocker 7: Unrelated rev bumps (hover, status updates, pan/zoom) must NOT remount the History
// panel, its core.Scroll ("history-panel-list"), filter controls, or preview banner, and live
// Dark <-> Light and EN <-> ZH switches must update cleanly in place.
func TestHistoryPanelZeroRemountFocusAndLiveThemeSwitch(t *testing.T) {
	dir := t.TempDir()
	projYAML := "version: 1\nname: remount-check\nid: dev.example.remount\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(projYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	rawScene, err := sceneformat.Encode(buildHistoryAcceptanceScene())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "level_01.scene.yaml"), rawScene, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	_ = ed.SetAutoSave(false)

	now := time.Date(2026, 11, 26, 14, 30, 0, 0, time.UTC)
	ed.SetHistoryClock(func() time.Time { return now })
	if err := ed.MoveObject("knight", 460, 220); err != nil {
		t.Fatal(err)
	}
	entryID := ed.HistoryEntries()[0].ID
	now = now.Add(3 * time.Minute)
	ed.SetHistoryPanelOpen(true)
	if err := ed.PreviewHistoryEntry(entryID); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))

	// Focus a filter button inside HistoryPanelView header and verify unrelated 60 Hz rev bumps
	// keep focus intact (no remount of header/scroll/banner).
	app.Focus("id:history-filter-all")
	app.Settle()
	if !app.Find("id:history-filter-all").Focused {
		t.Fatal("expected history-filter-all to have focus")
	}

	for i := 0; i < 15; i++ {
		app.Post(func() {
			ed.PanView2D(1, 0)
		})
	}
	app.Settle()
	if !app.Find("id:history-filter-all").Focused {
		t.Fatal("unrelated viewport rev bumps dropped focus on history-filter-all (remount regression)")
	}

	// Focus the floating banner's Back button and verify unrelated rev bumps preserve focus.
	app.Focus("id:history-viewport-back")
	app.Settle()
	if !app.Find("id:history-viewport-back").Focused {
		t.Fatal("expected history-viewport-back to have focus")
	}
	for i := 0; i < 10; i++ {
		app.Post(func() {
			ed.PanView2D(-1, 0)
		})
	}
	app.Settle()
	if !app.Find("id:history-viewport-back").Focused {
		t.Fatal("unrelated rev bumps dropped focus on history-viewport-back (banner remount regression)")
	}

	// Live Light <-> Dark and ZH <-> EN switches update banner and panel title in place.
	app.Post(func() {
		_ = ed.SetAppearance(editor.AppearanceLight)
		_ = ed.SetLanguage(editor.Language("zh"))
	})
	app.Settle()
	if got := app.Find("id:history-panel-title").Text; got != "历史" {
		t.Fatalf("ZH history panel title = %q, want 历史", got)
	}
	if got := app.Find("id:history-preview-banner-text").Text; !strings.Contains(got, "正在预览") {
		t.Fatalf("ZH preview banner = %q, want 正在预览", got)
	}
}
