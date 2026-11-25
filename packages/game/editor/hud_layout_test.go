package editor_test

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/gameui"
	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func boolPtr(v bool) *bool { return &v }

// makeHUDFixtureProject writes a complete dungeon-run project matching docs/design/editor/HUDLayout.dc.html
// with four screens (title, hud, pause, game_over) and English + Chinese (zh-Hans) locale tables.
func makeHUDFixtureProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	gameYAML := `version: 1
id: dev.example.dungeonrun
name: dungeon-run
appVersion: 0.1.0
window:
  width: 640
  height: 360
display:
  width: 640
  height: 360
`
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(gameYAML), 0o644); err != nil {
		t.Fatalf("write game.yaml: %v", err)
	}

	localesDir := filepath.Join(dir, "locales")
	if err := os.MkdirAll(localesDir, 0o755); err != nil {
		t.Fatalf("mkdir locales: %v", err)
	}
	enTbl := &l10n.StringTable{
		Version: l10n.StringTableVersion,
		Locale:  "en",
		Strings: map[string]string{
			"hud.coins":      "× 12",
			"hud.floor.1":    "Floor 1 · The Cellar",
			"hud.hint.lever": "Jump at levers to pull",
			"pause.title":    "Paused",
			"pause.resume":   "Resume",
			"pause.restart":  "Restart checkpoint",
			"pause.settings": "Settings",
			"pause.quit":     "Quit to title",
			"title.heading":  "Dungeon Run",
			"gameover.title": "Game Over",
		},
	}
	zhTbl := &l10n.StringTable{
		Version: l10n.StringTableVersion,
		Locale:  "zh-Hans",
		Strings: map[string]string{
			"hud.coins":      "× 12",
			"hud.floor.1":    "第一层 · 地窖",
			"hud.hint.lever": "靠近拉杆时按下跳跃键即可拉动",
			"pause.title":    "已暂停",
			"pause.resume":   "继续",
			"pause.restart":  "从上一个检查点重新开始",
			"pause.settings": "设置",
			"pause.quit":     "返回标题画面",
			"title.heading":  "地牢奔跑",
			"gameover.title": "游戏结束",
		},
	}
	for name, tbl := range map[string]*l10n.StringTable{"en.yaml": enTbl, "zh-Hans.yaml": zhTbl} {
		data, err := l10n.EncodeYAML(tbl)
		if err != nil {
			t.Fatalf("encode %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(localesDir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	screensDir := filepath.Join(dir, "screens")
	if err := os.MkdirAll(screensDir, 0o755); err != nil {
		t.Fatalf("mkdir screens: %v", err)
	}

	hudDoc := &gameui.ScreenDoc{
		Version:      gameui.ScreenFormatVersion,
		Name:         "hud",
		DesignWidth:  640,
		DesignHeight: 360,
		Nodes: []gameui.ScreenNodeDoc{
			{
				ID:      "status",
				Type:    gameui.NodeVBox,
				Anchor:  gameui.AnchorTopLeft,
				Offset:  &gameui.ScreenOffsetDoc{X: 12, Y: 12},
				Spacing: 4,
				Children: []gameui.ScreenNodeDoc{
					{
						ID:          "hearts",
						Type:        gameui.NodeSpriteRow,
						Width:       52,
						Height:      14,
						SpriteCount: 3,
						Spacing:     2,
					},
					{
						ID:      "coins",
						Type:    gameui.NodeLabel,
						Width:   52,
						Height:  16,
						TextKey: "hud.coins",
					},
				},
			},
			{
				ID:         "floor_label",
				Type:       gameui.NodeLabel,
				Anchor:     gameui.AnchorTopCenter,
				Offset:     &gameui.ScreenOffsetDoc{X: 0, Y: 12},
				SizeMode:   gameui.SizeFitText,
				Height:     18,
				TextKey:    "hud.floor.1",
				TextColor:  "#6b624f",
				Background: "#1c2230",
			},
			{
				ID:           "pause_button",
				Type:         gameui.NodeButton,
				Anchor:       gameui.AnchorTopRight,
				Offset:       &gameui.ScreenOffsetDoc{X: 12, Y: 12},
				Width:        24,
				Height:       24,
				Background:   "rgba(16,18,22,0.6)",
				CornerRadius: 4,
			},
			{
				ID:           "hint",
				Type:         gameui.NodeLabel,
				Anchor:       gameui.AnchorBottomCenter,
				Offset:       &gameui.ScreenOffsetDoc{X: 0, Y: 70},
				SizeMode:     gameui.SizeFixed,
				Width:        160,
				Height:       22,
				Padding:      &gameui.ScreenPaddingDoc{Left: 6, Right: 6},
				TextKey:      "hud.hint.lever",
				TextColor:    "#ffffff",
				Background:   "rgba(16,18,22,0.78)",
				CornerRadius: 4,
			},
			{
				ID:            "touch",
				Type:          gameui.NodeContainer,
				DeviceClasses: []gameui.DeviceClass{gameui.DevicePhone, gameui.DeviceTablet},
				Children: []gameui.ScreenNodeDoc{
					{
						ID:           "stick",
						Type:         gameui.NodeTouchStick,
						Anchor:       gameui.AnchorBottomLeft,
						Offset:       &gameui.ScreenOffsetDoc{X: 24, Y: 20},
						Width:        72,
						Height:       72,
						TouchHitSize: 96,
					},
					{
						ID:           "jump",
						Type:         gameui.NodeTouchButton,
						Anchor:       gameui.AnchorBottomRight,
						Offset:       &gameui.ScreenOffsetDoc{X: 24, Y: 28},
						Width:        52,
						Height:       52,
						TouchHitSize: 64,
					},
					{
						ID:           "attack",
						Type:         gameui.NodeTouchButton,
						Anchor:       gameui.AnchorBottomRight,
						Offset:       &gameui.ScreenOffsetDoc{X: 92, Y: 20},
						Width:        36,
						Height:       36,
						TouchHitSize: 36,
					},
				},
			},
		},
	}

	pauseDoc := &gameui.ScreenDoc{
		Version:      gameui.ScreenFormatVersion,
		Name:         "pause",
		DesignWidth:  640,
		DesignHeight: 360,
		Nodes: []gameui.ScreenNodeDoc{
			{
				ID:         "dim",
				Type:       gameui.NodeColorRect,
				Anchor:     gameui.AnchorFill,
				SafeArea:   boolPtr(false),
				Background: "rgba(10,12,16,0.62)",
			},
			{
				ID:           "panel",
				Type:         gameui.NodeVBox,
				Anchor:       gameui.AnchorCenter,
				Offset:       &gameui.ScreenOffsetDoc{X: 0, Y: 4},
				Width:        200,
				Height:       196,
				Padding:      &gameui.ScreenPaddingDoc{Top: 14, Right: 30, Bottom: 14, Left: 30},
				Spacing:      8,
				Background:   "#171a21",
				CornerRadius: 8,
				Children: []gameui.ScreenNodeDoc{
					{
						ID:       "title",
						Type:     gameui.NodeLabel,
						SizeMode: gameui.SizeFitText,
						Height:   20,
						TextKey:  "pause.title",
					},
					{
						ID:         "resume",
						Type:       gameui.NodeButton,
						Width:      140,
						Height:     26,
						Padding:    &gameui.ScreenPaddingDoc{Left: 8, Right: 8},
						TextKey:    "pause.resume",
						FocusOrder: 1,
					},
					{
						ID:         "restart",
						Type:       gameui.NodeButton,
						Width:      140,
						Height:     26,
						Padding:    &gameui.ScreenPaddingDoc{Left: 8, Right: 8},
						TextKey:    "pause.restart",
						FocusOrder: 2,
					},
					{
						ID:         "settings",
						Type:       gameui.NodeButton,
						Width:      140,
						Height:     26,
						Padding:    &gameui.ScreenPaddingDoc{Left: 8, Right: 8},
						TextKey:    "pause.settings",
						FocusOrder: 3,
					},
					{
						ID:         "quit",
						Type:       gameui.NodeButton,
						Width:      140,
						Height:     26,
						Padding:    &gameui.ScreenPaddingDoc{Left: 8, Right: 8},
						TextKey:    "pause.quit",
						FocusOrder: 4,
					},
				},
			},
		},
	}

	titleDoc := &gameui.ScreenDoc{
		Version:      gameui.ScreenFormatVersion,
		Name:         "title",
		DesignWidth:  640,
		DesignHeight: 360,
		Nodes: []gameui.ScreenNodeDoc{
			{ID: "title_bg", Type: gameui.NodeColorRect, Anchor: gameui.AnchorFill, Background: "#101216"},
			{ID: "title_heading", Type: gameui.NodeLabel, Anchor: gameui.AnchorCenter, SizeMode: gameui.SizeFitText, Height: 24, TextKey: "title.heading"},
		},
	}
	gameOverDoc := &gameui.ScreenDoc{
		Version:      gameui.ScreenFormatVersion,
		Name:         "game_over",
		DesignWidth:  640,
		DesignHeight: 360,
		Nodes: []gameui.ScreenNodeDoc{
			{ID: "over_bg", Type: gameui.NodeColorRect, Anchor: gameui.AnchorFill, Background: "#101216"},
			{ID: "over_heading", Type: gameui.NodeLabel, Anchor: gameui.AnchorCenter, SizeMode: gameui.SizeFitText, Height: 24, TextKey: "gameover.title"},
		},
	}

	for name, doc := range map[string]*gameui.ScreenDoc{
		"hud.screen.yaml":       hudDoc,
		"pause.screen.yaml":     pauseDoc,
		"title.screen.yaml":     titleDoc,
		"game_over.screen.yaml": gameOverDoc,
	} {
		data, err := gameui.EncodeScreen(doc)
		if err != nil {
			t.Fatalf("encode %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(screensDir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// #1047 Acceptance 1: switching to 中文 redraws all four sizes, and an overflowing label shows
// in the TEXT WIDTH, PX table and in GAME CHECK with Show selecting it.
func TestHUDLayoutLanguageSwitchRedrawsOverflowAndShowSelects(t *testing.T) {
	dir := makeHUDFixtureProject(t)
	ed := newTestEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.OpenHUDScreen("screens/hud.screen.yaml"); err != nil {
		t.Fatalf("OpenHUDScreen: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	if !app.Exists("id:hud-layout-screen") {
		t.Fatal("hud-layout-screen did not mount")
	}
	if got := app.Find("id:hud-breadcrumb-project").Text; got != "dungeon-run" {
		t.Fatalf("breadcrumb project = %q, want dungeon-run", got)
	}
	if got := app.Find("id:hud-breadcrumb-file").Text; got != "hud.screen.yaml" {
		t.Fatalf("breadcrumb file = %q, want hud.screen.yaml", got)
	}
	if frames := ed.HUDLayoutFrames(848, 812); len(frames) != 4 {
		t.Fatalf("HUDLayoutFrames count = %d, want 4", len(frames))
	}

	// Start in English: hint fits inside its 160 px box, so GAME CHECK should NOT flag hint overflow.
	if ed.HUDLocale() != "en" {
		t.Fatalf("initial locale = %q, want en", ed.HUDLocale())
	}
	for _, p := range ed.HUDProblems() {
		if p.NodeID == "hint" && p.Code == gameui.IssueTextOverflow {
			t.Fatalf("unexpected English overflow problem on hint: %+v", p)
		}
	}
	// Select another node before switching language so we can verify Show selects hint.
	ed.SelectHUDNode("status")
	app.Settle()

	// Switch to 中文 via the Language segmented control.
	app.Press("id:hud-lang-zh-Hans")
	app.Settle()

	if got := ed.HUDLocale(); got != "zh-Hans" {
		t.Fatalf("locale after pressing 中文 = %q, want zh-Hans", got)
	}

	// All four device frames must now hold the localized Chinese text.
	frames := ed.HUDLayoutFrames(848, 812)
	for _, fr := range frames {
		lbl, ok := fr.Built.Widget("hint").(*gameui.Label)
		if !ok || lbl == nil {
			t.Fatalf("device %s missing hint label widget", fr.Device.ID)
		}
		if lbl.Text != "靠近拉杆时按下跳跃键即可拉动" {
			t.Errorf("device %s hint text = %q, want Chinese string", fr.Device.ID, lbl.Text)
		}
	}

	// TEXT WIDTH, PX table must report overflow for hint in zh-Hans and fit in en.
	var hintRow *editor.HUDTextWidthRow
	for _, r := range ed.HUDTextWidths() {
		if r.NodeID == "hint" {
			rc := r
			hintRow = &rc
			break
		}
	}
	if hintRow == nil {
		t.Fatal("hint missing from HUDTextWidths")
	}
	if hintRow.Overflow["en"] != 0 {
		t.Errorf("hint English overflow = %v, want 0", hintRow.Overflow["en"])
	}
	if hintRow.Overflow["zh-Hans"] <= 0 {
		t.Errorf("hint zh-Hans overflow = %v, want > 0", hintRow.Overflow["zh-Hans"])
	}
	if !app.Exists("id:hud-text-width-table") || !app.Exists("id:hud-text-width-hint-zh-Hans") {
		t.Fatal("TEXT WIDTH, PX table did not render hint zh-Hans cell")
	}

	// GAME CHECK must list hint text overflow, and pressing Show selects hint.
	if !app.Exists("id:hud-problem-show-hint") {
		t.Fatal("GAME CHECK missing Show button for overflowing hint node")
	}
	app.Press("id:hud-problem-show-hint")
	app.Settle()
	if got := ed.HUDSelectedNodeID(); got != "hint" {
		t.Fatalf("selected node after pressing Show = %q, want hint", got)
	}
	if !app.Exists("id:hud-zoom-detail") {
		t.Fatal("zoomed overflow detail card did not render for overflowing hint")
	}
	if got := app.Find("id:hud-zoom-title").Text; got != "Phone · 中文 · 1.5×" {
		t.Fatalf("zoom title = %q, want %q", got, "Phone · 中文 · 1.5×")
	}

	// Switching hint to Fit text resolves the overflow in one undo step and hides zoom detail.
	histBefore := ed.History.Index()
	app.Press("id:hud-size-fit")
	app.Settle()
	if ed.History.Index() != histBefore+1 {
		t.Fatalf("Fit text history index = %d, want %d", ed.History.Index(), histBefore+1)
	}
	if app.Exists("id:hud-zoom-detail") {
		t.Fatal("zoom detail should disappear once hint fits text")
	}
	if app.Exists("id:hud-problem-show-hint") {
		t.Fatal("hint overflow should disappear from GAME CHECK after Fit text")
	}
	// Undo restores fixed sizing and the overflow problem.
	ed.Undo()
	app.Settle()
	if !app.Exists("id:hud-problem-show-hint") {
		t.Fatal("Undo did not restore hint overflow problem")
	}

	// Switch to pause screen in English: verify zero-problems message.
	app.Press("id:hud-screen-pause")
	app.Settle()
	app.Press("id:hud-lang-en")
	app.Settle()
	if !app.Exists("id:hud-check-empty") {
		t.Fatalf("expected 'No problems at any size in this language' on pause screen in English; problems=%+v", ed.HUDProblems())
	}
}

// #1047 Acceptance 2: dragging a touch button writes its offset to the screen file in one undo step.
func TestHUDLayoutDragTouchButtonWritesFileInOneUndoStep(t *testing.T) {
	dir := makeHUDFixtureProject(t)
	hudPath := filepath.Join(dir, "screens", "hud.screen.yaml")
	origBytes, err := os.ReadFile(hudPath)
	if err != nil {
		t.Fatalf("read original hud.screen.yaml: %v", err)
	}

	ed := newTestEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.OpenHUDScreen("screens/hud.screen.yaml"); err != nil {
		t.Fatalf("OpenHUDScreen: %v", err)
	}

	frames := ed.HUDLayoutFrames(848, 812)
	phone := frames[0]
	jumpWidget := phone.Built.Widget("jump")
	if jumpWidget == nil {
		t.Fatal("jump widget missing on Phone frame")
	}
	jb := jumpWidget.Bounds()
	startX := phone.X + (float32(jb.Origin.X)+float32(jb.Size.Width)/2)*phone.Scale
	startY := phone.Y + (float32(jb.Origin.Y)+float32(jb.Size.Height)/2)*phone.Scale

	histBefore := ed.History.Index()

	if !ed.BeginHUDTouchDrag(startX, startY, 848, 812) {
		t.Fatalf("BeginHUDTouchDrag at (%v,%v) returned false", startX, startY)
	}
	if !ed.HUDTouchDragActive() {
		t.Fatal("HUDTouchDragActive = false during drag")
	}
	// Drag left by 20 virtual pt and up by 10 virtual pt (bottom_right inset increases X and Y).
	midX := startX - 10*phone.Scale
	midY := startY - 5*phone.Scale
	endX := startX - 20*phone.Scale
	endY := startY - 10*phone.Scale
	ed.UpdateHUDTouchDrag(midX, midY)
	ed.UpdateHUDTouchDrag(endX, endY)
	if err := ed.EndHUDTouchDrag(); err != nil {
		t.Fatalf("EndHUDTouchDrag: %v", err)
	}

	if ed.HUDTouchDragActive() {
		t.Fatal("HUDTouchDragActive still true after EndHUDTouchDrag")
	}
	if got := ed.History.Index(); got != histBefore+1 {
		t.Fatalf("drag recorded %d undo steps, want 1", got-histBefore)
	}

	// Verify file on disk has the updated offset and round-trips deterministically.
	afterBytes, err := os.ReadFile(hudPath)
	if err != nil {
		t.Fatalf("read updated hud.screen.yaml: %v", err)
	}
	if bytes.Equal(origBytes, afterBytes) {
		t.Fatal("hud.screen.yaml was not updated on disk after touch drag")
	}
	parsed, err := gameui.ParseScreen(afterBytes)
	if err != nil {
		t.Fatalf("ParseScreen after drag: %v", err)
	}
	reencoded, err := gameui.EncodeScreen(parsed)
	if err != nil {
		t.Fatalf("EncodeScreen after drag: %v", err)
	}
	if !bytes.Equal(afterBytes, reencoded) {
		t.Fatalf("written screen file is not canonical YAML")
	}
	var jumpNode *gameui.ScreenNodeDoc
	for _, top := range parsed.Nodes {
		if top.ID == "touch" {
			for i := range top.Children {
				if top.Children[i].ID == "jump" {
					jumpNode = &top.Children[i]
				}
			}
		}
	}
	if jumpNode == nil || jumpNode.Offset == nil {
		t.Fatal("jump offset missing after drag")
	}
	if jumpNode.Offset.X != 44 || jumpNode.Offset.Y != 38 {
		t.Fatalf("jump offset on disk = (%v, %v), want (44, 38)", jumpNode.Offset.X, jumpNode.Offset.Y)
	}

	// One Undo restores both memory and the exact original file bytes on disk.
	if !ed.Undo() {
		t.Fatal("Undo returned false")
	}
	undoneBytes, err := os.ReadFile(hudPath)
	if err != nil {
		t.Fatalf("read hud.screen.yaml after Undo: %v", err)
	}
	if !bytes.Equal(undoneBytes, origBytes) {
		t.Fatalf("Undo did not restore original hud.screen.yaml bytes")
	}

	// Redo writes the dragged offset back to disk.
	if !ed.Redo() {
		t.Fatal("Redo returned false")
	}
	redoneBytes, err := os.ReadFile(hudPath)
	if err != nil {
		t.Fatalf("read hud.screen.yaml after Redo: %v", err)
	}
	if !bytes.Equal(redoneBytes, afterBytes) {
		t.Fatalf("Redo did not re-apply dragged offset to hud.screen.yaml")
	}

	// CancelHUDTouchDrag aborts without adding undo history or writing disk.
	if !ed.BeginHUDTouchDrag(endX, endY, 848, 812) {
		t.Fatal("second BeginHUDTouchDrag failed")
	}
	ed.UpdateHUDTouchDrag(endX-30*phone.Scale, endY-30*phone.Scale)
	ed.CancelHUDTouchDrag()
	if ed.History.Index() != histBefore+1 {
		t.Fatalf("cancelled drag changed history index to %d", ed.History.Index())
	}
	afterCancelBytes, _ := os.ReadFile(hudPath)
	if !bytes.Equal(afterCancelBytes, afterBytes) {
		t.Fatal("cancelled drag modified disk file")
	}
}

func TestHUDLayoutAnchorSafeAreaSpacingReorderAndComposer(t *testing.T) {
	dir := makeHUDFixtureProject(t)
	ed := newTestEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.OpenHUDScreen("screens/hud.screen.yaml"); err != nil {
		t.Fatalf("OpenHUDScreen: %v", err)
	}

	var prompts []editor.HUDPromptRequest
	ed.OnHUDPrompt = func(req editor.HUDPromptRequest) {
		prompts = append(prompts, req)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	// Select status VBox: test spacing + anchor grid + safe area toggle.
	app.Press("id:hud-tree-status")
	app.Settle()
	if got := app.Find("id:hud-spacing-value").Text; got != "4 px" {
		t.Fatalf("initial status spacing = %q, want 4 px", got)
	}
	app.Press("id:hud-spacing-inc")
	app.Settle()
	if got := app.Find("id:hud-spacing-value").Text; got != "6 px" {
		t.Fatalf("status spacing after + = %q, want 6 px", got)
	}
	ed.Undo()
	app.Settle()
	if got := app.Find("id:hud-spacing-value").Text; got != "4 px" {
		t.Fatalf("status spacing after Undo = %q, want 4 px", got)
	}

	// Reorder children inside status VBox (move hearts down after coins).
	app.Press("id:hud-tree-hearts")
	app.Settle()
	app.Press("id:hud-order-down")
	app.Settle()
	doc := ed.HUDActiveScreen()
	if doc.Nodes[0].Children[0].ID != "coins" || doc.Nodes[0].Children[1].ID != "hearts" {
		t.Fatalf("status children after reorder = [%s, %s], want [coins, hearts]",
			doc.Nodes[0].Children[0].ID, doc.Nodes[0].Children[1].ID)
	}
	ed.Undo()
	app.Settle()
	doc = ed.HUDActiveScreen()
	if doc.Nodes[0].Children[0].ID != "hearts" {
		t.Fatalf("status children after Undo = %s first, want hearts", doc.Nodes[0].Children[0].ID)
	}

	// Change anchor via 3×3 grid and Fill button, and toggle safe area.
	app.Press("id:hud-tree-hint")
	app.Settle()
	app.Press("id:hud-anchor-top_right")
	app.Settle()
	if got := app.Find("id:hud-anchor-words").Text; got != "Top right of the safe area" {
		t.Fatalf("anchor words = %q, want Top right of the safe area", got)
	}
	app.Press("id:hud-safe-area-toggle")
	app.Settle()
	if got := app.Find("id:hud-anchor-words").Text; got != "Top right of the screen" {
		t.Fatalf("anchor words after unsafe toggle = %q, want Top right of the screen", got)
	}
	app.Press("id:hud-anchor-fill")
	app.Settle()
	if got := app.Find("id:hud-anchor-words").Text; got != "Fills the whole screen" {
		t.Fatalf("anchor words after fill = %q, want Fills the whole screen", got)
	}

	// Toggle all four overlay switches.
	for _, key := range []string{"safe", "anchors", "touch", "focus"} {
		app.Press("id:hud-overlay-" + key)
	}
	app.Settle()
	if ov := ed.HUDOverlays(); ov.SafeAreas || ov.Anchors || ov.TouchAreas || ov.FocusOrder {
		t.Fatalf("expected all overlays off after toggling, got %+v", ov)
	}

	// Test "Ask the agent to fix N" button and composer submission.
	app.Press("id:hud-ask-agent-fix")
	app.Settle()
	if len(prompts) != 1 || !strings.Contains(prompts[0].Prompt, "hud.screen.yaml") {
		t.Fatalf("Ask agent fix prompt = %+v", prompts)
	}
	app.Type("id:hud-composer-input", "add a boss health bar that only shows on floor 3")
	app.Press("id:hud-composer-send")
	app.Settle()
	if len(prompts) != 2 || prompts[1].Prompt != "add a boss health bar that only shows on floor 3" {
		t.Fatalf("composer prompt = %+v", prompts)
	}
	if ed.LastHUDPrompt().Prompt != "add a boss health bar that only shows on floor 3" {
		t.Fatalf("LastHUDPrompt = %+v", ed.LastHUDPrompt())
	}
}

func TestHUDLayoutEmptyErrorAndEditorIntegration(t *testing.T) {
	// 1. Empty state when no screens exist.
	ed := newTestEditor(t)
	ed.SetHUDLayoutOpen(true)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))
	app.Settle()
	if !app.Exists("id:hud-empty-state") {
		t.Fatal("expected hud-empty-state when no screens exist")
	}

	// Returning to scene editor via breadcrumb button.
	app.Press("id:hud-back-scene")
	app.Settle()
	if ed.HUDLayoutOpen() {
		t.Fatal("expected HUDLayoutOpen = false after pressing back button")
	}

	// 2. Command registry toggles HUD layout.
	if err := ed.ExecuteCommand(editor.CmdHUDLayout); err != nil {
		t.Fatalf("ExecuteCommand(CmdHUDLayout): %v", err)
	}
	if !ed.HUDLayoutOpen() {
		t.Fatal("CmdHUDLayout did not open HUD layout screen")
	}

	// 3. Error state for a malformed *.screen.yaml file.
	dir := makeHUDFixtureProject(t)
	badPath := filepath.Join(dir, "screens", "broken.screen.yaml")
	if err := os.WriteFile(badPath, []byte("version: 99\nunknownField: true\n"), 0o644); err != nil {
		t.Fatalf("write broken screen: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.OpenHUDScreen("screens/broken.screen.yaml"); err == nil {
		t.Fatal("expected parse error for broken.screen.yaml")
	}
	app.Settle()
	if !app.Exists("id:hud-error-state") {
		t.Fatal("expected hud-error-state for broken screen file")
	}

	// 4. OpenProjectFile and OpenProblem route *.screen.yaml directly into HUD layout.
	ed.SetHUDLayoutOpen(false)
	if err := ed.OpenProjectFile("screens/pause.screen.yaml"); err != nil {
		t.Fatalf("OpenProjectFile(pause): %v", err)
	}
	if !ed.HUDLayoutOpen() || ed.HUDActiveScreenPath() != "screens/pause.screen.yaml" {
		t.Fatalf("OpenProjectFile did not open pause screen: open=%v path=%q", ed.HUDLayoutOpen(), ed.HUDActiveScreenPath())
	}
	ed.SetHUDLayoutOpen(false)
	if err := ed.OpenProblem(assetcheck.Problem{File: "screens/hud.screen.yaml", Line: 5, Message: "overflow"}); err != nil {
		t.Fatalf("OpenProblem(hud): %v", err)
	}
	if !ed.HUDLayoutOpen() || ed.HUDActiveScreenPath() != "screens/hud.screen.yaml" {
		t.Fatalf("OpenProblem did not open hud screen: open=%v path=%q", ed.HUDLayoutOpen(), ed.HUDActiveScreenPath())
	}
}

func TestHUDLayoutRefusesPathTraversalAndSymlinkEscape(t *testing.T) {
	projDir := makeHUDFixtureProject(t)
	outsideDir := t.TempDir()
	outsideScreen := filepath.Join(outsideDir, "secret.screen.yaml")
	outsideBytes, err := gameui.EncodeScreen(&gameui.ScreenDoc{
		Version:      gameui.ScreenFormatVersion,
		Name:         "secret",
		DesignWidth:  640,
		DesignHeight: 360,
		Nodes:        []gameui.ScreenNodeDoc{{ID: "secret_lbl", Type: gameui.NodeLabel, Width: 100, Height: 20, TextKey: "hud.coins"}},
	})
	if err != nil {
		t.Fatalf("EncodeScreen: %v", err)
	}
	if err := os.WriteFile(outsideScreen, outsideBytes, 0o644); err != nil {
		t.Fatalf("WriteFile outsideScreen: %v", err)
	}
	outsideLocale := filepath.Join(outsideDir, "xx.yaml")
	if err := os.WriteFile(outsideLocale, []byte("version: 1\nlocale: xx\nstrings:\n  secret: leaked\n"), 0o644); err != nil {
		t.Fatalf("WriteFile outsideLocale: %v", err)
	}

	linkScreen := filepath.Join(projDir, "screens", "escape.screen.yaml")
	if err := os.Symlink(outsideScreen, linkScreen); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	linkLocale := filepath.Join(projDir, "locales", "xx.yaml")
	if err := os.Symlink(outsideLocale, linkLocale); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	ed := newTestEditor(t)
	if err := ed.OpenProject(projDir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	// Scan must skip symlinked screens and locales escaping the project root.
	for _, sc := range ed.HUDScreens() {
		if strings.Contains(sc.Path, "escape") {
			t.Fatalf("RefreshHUDScreens included escaping symlink screen %q", sc.Path)
		}
	}
	for _, loc := range ed.HUDLocales() {
		if loc == "xx" {
			t.Fatal("RefreshHUDScreens loaded escaping symlink locale xx")
		}
	}

	// Direct OpenHUDScreen must refuse path traversal and symlink escapes.
	relEscape, err := filepath.Rel(projDir, outsideScreen)
	if err == nil {
		if err := ed.OpenHUDScreen(filepath.ToSlash(relEscape)); err == nil {
			t.Fatalf("expected OpenHUDScreen(%q) to refuse path traversal", relEscape)
		}
	}
	if err := ed.OpenHUDScreen(outsideScreen); err == nil {
		t.Fatal("expected OpenHUDScreen(abs) to refuse absolute path")
	}
	if err := ed.OpenHUDScreen("screens/escape.screen.yaml"); err == nil {
		t.Fatal("expected OpenHUDScreen to refuse symlink escaping project root")
	}
}

func TestHUDLayoutCrossFileUndoIsolationAndDemoReset(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	fileA := filepath.Join(dirA, "hud.screen.yaml")
	fileB := filepath.Join(dirB, "hud.screen.yaml")

	docA := &gameui.ScreenDoc{
		Version:      gameui.ScreenFormatVersion,
		Name:         "hud",
		DesignWidth:  640,
		DesignHeight: 360,
		Nodes:        []gameui.ScreenNodeDoc{{ID: "node_a", Type: gameui.NodeLabel, Anchor: gameui.AnchorTopLeft, Width: 80, Height: 20, TextKey: "a"}},
	}
	docB := &gameui.ScreenDoc{
		Version:      gameui.ScreenFormatVersion,
		Name:         "hud",
		DesignWidth:  640,
		DesignHeight: 360,
		Nodes:        []gameui.ScreenNodeDoc{{ID: "node_b", Type: gameui.NodeLabel, Anchor: gameui.AnchorBottomRight, Width: 90, Height: 24, TextKey: "b"}},
	}
	rawA, _ := gameui.EncodeScreen(docA)
	rawB, _ := gameui.EncodeScreen(docB)
	if err := os.WriteFile(fileA, rawA, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, rawB, 0o644); err != nil {
		t.Fatal(err)
	}

	ed := newTestEditor(t)
	if err := ed.OpenHUDScreenFile(fileA); err != nil {
		t.Fatalf("OpenHUDScreenFile A: %v", err)
	}
	if err := ed.SetHUDNodeAnchor("node_a", gameui.AnchorTopRight); err != nil {
		t.Fatalf("SetHUDNodeAnchor on A: %v", err)
	}
	editedA, _ := os.ReadFile(fileA)

	// Switch to standalone fileB (which shares the base name "hud.screen.yaml").
	if err := ed.OpenHUDScreenFile(fileB); err != nil {
		t.Fatalf("OpenHUDScreenFile B: %v", err)
	}

	// Undo/Redo of fileA's command must not overwrite fileB in memory or fileA on disk.
	ed.Undo()
	if got := ed.HUDActiveScreen(); got == nil || len(got.Nodes) != 1 || got.Nodes[0].ID != "node_b" {
		t.Fatalf("Undo after switching standalone screen files corrupted screen B: %+v", got)
	}
	if diskA, _ := os.ReadFile(fileA); !bytes.Equal(diskA, editedA) {
		t.Fatal("stale cross-session Undo rewrote previous standalone fileA on disk")
	}
	ed.Redo()
	if got := ed.HUDActiveScreen(); got == nil || len(got.Nodes) != 1 || got.Nodes[0].ID != "node_b" {
		t.Fatalf("Redo after switching standalone screen files corrupted screen B: %+v", got)
	}

	// OpenDemo must reset HUD layout state and close the HUD layout screen.
	projDir := makeHUDFixtureProject(t)
	if err := ed.OpenProject(projDir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.OpenHUDScreen("screens/hud.screen.yaml"); err != nil {
		t.Fatalf("OpenHUDScreen: %v", err)
	}
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if ed.HUDLayoutOpen() {
		t.Fatal("expected OpenDemo to close HUDLayoutOpen")
	}
	if len(ed.HUDScreens()) != 0 {
		t.Fatalf("expected OpenDemo to clear project HUD screens, got %d", len(ed.HUDScreens()))
	}
}

func TestHUDLayoutPlayModeCancelsDragClosesScreenAndGuardsEdits(t *testing.T) {
	dir := makeHUDFixtureProject(t)
	ed := newTestEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.OpenHUDScreen("screens/hud.screen.yaml"); err != nil {
		t.Fatalf("OpenHUDScreen: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	frames := ed.HUDLayoutFrames(848, 812)
	jb := frames[0].Built.Widget("jump").Bounds()
	startX := frames[0].X + (float32(jb.Origin.X)+float32(jb.Size.Width)/2)*frames[0].Scale
	startY := frames[0].Y + (float32(jb.Origin.Y)+float32(jb.Size.Height)/2)*frames[0].Scale
	if !ed.BeginHUDTouchDrag(startX, startY, 848, 812) {
		t.Fatal("BeginHUDTouchDrag failed")
	}

	// Entering Play mode while HUD layout & drag are active must cancel the drag and exit HUD layout.
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()

	if !ed.Playing() {
		t.Fatal("expected Playing() == true")
	}
	if ed.HUDTouchDragActive() {
		t.Fatal("expected Play() to cancel active HUD touch drag")
	}
	if ed.HUDLayoutOpen() || app.Exists("id:hud-layout-screen") {
		t.Fatal("expected Play() to close HUDLayoutView so playing viewport is visible")
	}

	// Opening HUD screens or mutating during Play is refused.
	ed.SetHUDLayoutOpen(true)
	if ed.HUDLayoutOpen() {
		t.Fatal("SetHUDLayoutOpen(true) should be ignored while playing")
	}
	if err := ed.OpenHUDScreen("screens/pause.screen.yaml"); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("OpenHUDScreen during Play = %v, want ErrPlaying", err)
	}
	if err := ed.OpenHUDScreenFile(filepath.Join(dir, "screens", "pause.screen.yaml")); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("OpenHUDScreenFile during Play = %v, want ErrPlaying", err)
	}
	if err := ed.OpenHUDScreenDoc("mem", ed.HUDActiveScreen(), nil); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("OpenHUDScreenDoc during Play = %v, want ErrPlaying", err)
	}

	app.Post(func() { _ = ed.Stop() })
	app.Settle()
}

func TestHUDLayoutNonFiniteInputsAndOverflowSafeFormatting(t *testing.T) {
	dir := makeHUDFixtureProject(t)
	ed := newTestEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.OpenHUDScreen("screens/hud.screen.yaml"); err != nil {
		t.Fatalf("OpenHUDScreen: %v", err)
	}

	frames := ed.HUDLayoutFrames(float32(math.NaN()), float32(math.Inf(1)))
	if len(frames) != 4 {
		t.Fatalf("HUDLayoutFrames(NaN, +Inf) returned %d frames, want 4", len(frames))
	}
	for _, fr := range frames {
		if math.IsNaN(float64(fr.X)) || math.IsInf(float64(fr.X), 0) || math.IsNaN(float64(fr.W)) || math.IsInf(float64(fr.W), 0) {
			t.Fatalf("non-finite frame geometry from HUDLayoutFrames: %+v", fr)
		}
	}

	jb := frames[0].Built.Widget("jump").Bounds()
	startX := frames[0].X + (float32(jb.Origin.X)+float32(jb.Size.Width)/2)*frames[0].Scale
	startY := frames[0].Y + (float32(jb.Origin.Y)+float32(jb.Size.Height)/2)*frames[0].Scale
	if !ed.BeginHUDTouchDrag(startX, startY, 848, 812) {
		t.Fatal("BeginHUDTouchDrag failed")
	}
	if ed.UpdateHUDTouchDrag(float32(math.NaN()), float32(math.Inf(-1))) {
		t.Fatal("UpdateHUDTouchDrag accepted non-finite coordinates")
	}
	ed.CancelHUDTouchDrag()

	if err := ed.SetHUDNodeOffset("hint", float32(math.NaN()), 0); err == nil {
		t.Fatal("SetHUDNodeOffset accepted NaN")
	}
	if err := ed.SetHUDNodeSpacing("status", float32(math.Inf(1))); err == nil {
		t.Fatal("SetHUDNodeSpacing accepted +Inf")
	}
}
