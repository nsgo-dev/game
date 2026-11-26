package editor_test

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func setupComposerProject(t *testing.T) (string, *editor.Editor) {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"scenes", "prefabs", "art/enemies", "audio/sfx", "rules"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(`version: 1
name: dungeon-run
id: com.example.dungeonrun
appVersion: 0.1.0
window:
  title: Dungeon Run
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "brief.yaml"), []byte(`title: Dungeon Run
goals:
  - Keep combat crisp and readable
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(`# Project rules
Keep edits focused and deterministic.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "rules/enemies.md"), []byte(`No enemy within 64 px of the start.
Slimes patrol horizontally.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "prefabs/slime.prefab.yaml"), []byte(`version: 1
name: slime
hp: 2
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a tiny valid PNG asset.
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 0x57, G: 0xa8, B: 0x7a, A: 0xff})
		}
	}
	f, err := os.Create(filepath.Join(dir, "art/enemies/slime_green.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()

	// Write a placeholder wav asset.
	if err := os.WriteFile(filepath.Join(dir, "audio/sfx/slime_squish.wav"), []byte("RIFF\x00\x00\x00\x00WAVE"), 0o644); err != nil {
		t.Fatal(err)
	}

	sceneYAML := []byte(`version: 4
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#141824"
layers:
  - name: actors
    space: world
    objects:
      - id: knight
        name: Knight
        x: 132
        y: 350
      - id: door
        name: Door
        x: 632
        y: 356
        properties:
          locked: true
          opens_after_pulls: 2
      - id: lever_01
        name: lever_01
        x: 598
        y: 372
      - id: slime_01
        name: slime_01
        x: 228
        y: 376
      - id: slime_02
        name: slime_02
        x: 292
        y: 376
        properties:
          speed: 45
      - id: slime_03
        name: slime_03
        x: 512
        y: 300
`)
	if err := os.WriteFile(filepath.Join(dir, "scenes/level_01.scene.yaml"), sceneYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	sheet := image.NewRGBA(image.Rect(0, 0, 24, 24))
	actors := &sprite.Atlas{
		Image: core.ImageSource{Pixels: sheet},
		Frames: map[string]sprite.Frame{
			"hero": {Name: "hero", X: 0, Y: 0, W: 24, H: 24, Width: 24, Height: 24},
		},
	}
	ed, err := editor.New(nil, sceneformat.AtlasMap{"actors": actors})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	return dir, ed
}

// Acceptance 1 (#1020): A test types @sli, adds slime_02 with ↵, sends, and the request
// the scripted agent receives contains exactly slime_02's YAML lines shown on hover.
func TestComposerMentionAtSliAddSlime02AndSendMatchesHoverYAML(t *testing.T) {
	_, ed := setupComposerProject(t)

	var received *editor.AgentRequest
	ed.OnSendAgentComposerRequest = func(req editor.AgentRequest) (editor.AgentResponse, error) {
		r := req
		received = &r
		return editor.AgentResponse{
			Reply: "Moved slime_02 in front of the door.",
			Usage: &editor.AgentTokenUsage{
				Limit:         200_000,
				Conversation:  24_000,
				OpenScene:     12_000,
				Attached:      4_000,
				Always:        6_000,
				ToolsAndInstr: 10_000,
				ViewportImage: 6_000,
			},
		}, nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// Switch to Agent tab (⌘1 / button).
	app.Press("id:right-tab-agent")
	app.Settle()

	if !app.Exists("id:agent-composer-input") {
		t.Fatal("agent composer input not mounted after switching to Agent tab")
	}

	// Type prompt ending with @sli to open the @-mention picker filtered to "sli".
	app.Focus("id:agent-composer-input")
	app.Type("id:agent-composer-input", "Guard the door with @sli")
	app.Settle()

	if !ed.MentionPickerOpen() {
		t.Fatal("expected @-mention picker to open after typing @sli")
	}
	if got := ed.MentionPickerQuery(); got != "sli" {
		t.Fatalf("MentionPickerQuery() = %q, want %q", got, "sli")
	}
	if !app.Exists("id:agent-mention-picker") {
		t.Fatal("mention picker UI not rendered")
	}

	// Find slime_02 in the picker items and navigate to it with ↓, then press ↵ (Enter).
	items := ed.MentionPickerItems()
	slimeIdx := -1
	for i, it := range items {
		if it.Target == "slime_02" && it.Group == editor.MentionGroupObjects {
			slimeIdx = i
			break
		}
	}
	if slimeIdx < 0 {
		t.Fatalf("slime_02 not found in @sli picker items: %+v", items)
	}
	for step := 0; step < slimeIdx; step++ {
		app.Key("down")
		app.Settle()
	}
	if ed.MentionPickerActiveIndex() != slimeIdx {
		t.Fatalf("picker active index = %d, want %d", ed.MentionPickerActiveIndex(), slimeIdx)
	}

	app.Key("enter")
	app.Settle()

	if ed.MentionPickerOpen() {
		t.Fatal("expected @-mention picker to close after pressing Enter")
	}
	chips := ed.ComposerChips()
	if len(chips) != 1 || chips[0].Target != "slime_02" {
		t.Fatalf("ComposerChips() = %+v, want [slime_02]", chips)
	}

	// Inspect what hovering the slime_02 chip displays.
	app.Post(func() {
		ed.SetHoveredChip(chips[0].ID)
	})
	app.Settle()

	hoverPayload, ok := ed.HoveredChipPayload()
	if !ok {
		t.Fatal("expected HoveredChipPayload to resolve for slime_02 chip")
	}
	if !strings.HasPrefix(hoverPayload.Header, "# level_01.scene.yaml · lines ") {
		t.Fatalf("hover header = %q, want prefix %q", hoverPayload.Header, "# level_01.scene.yaml · lines ")
	}
	if !strings.Contains(hoverPayload.Text, "- id: slime_02") || !strings.Contains(hoverPayload.Text, "speed: 45") {
		t.Fatalf("hover YAML text missing slime_02 fields:\n%s", hoverPayload.Text)
	}
	if !app.Exists("id:agent-chip-hover-preview") {
		t.Fatal("hover preview card not rendered")
	}
	if got := app.Find("id:agent-chip-hover-header").Text; got != hoverPayload.Header {
		t.Fatalf("rendered hover header = %q, want %q", got, hoverPayload.Header)
	}
	if got := app.Find("id:agent-chip-hover-body").Text; got != hoverPayload.Text {
		t.Fatalf("rendered hover body = %q, want %q", got, hoverPayload.Text)
	}

	// Send the prompt and verify the scripted agent receives the EXACT same lines shown on hover.
	app.Press("id:agent-composer-send")
	app.Settle()

	if received == nil {
		t.Fatal("scripted agent OnSendAgentComposerRequest was not called")
	}
	if received.Prompt != "Guard the door with" {
		t.Fatalf("received prompt = %q, want %q", received.Prompt, "Guard the door with")
	}
	var slimeAttachment *editor.AgentChipPayload
	for i := range received.Attachments {
		if received.Attachments[i].Chip.Target == "slime_02" {
			slimeAttachment = &received.Attachments[i]
			break
		}
	}
	if slimeAttachment == nil {
		t.Fatalf("received attachments missing slime_02: %+v", received.Attachments)
	}
	if slimeAttachment.Header != hoverPayload.Header || slimeAttachment.Text != hoverPayload.Text {
		t.Fatalf("sent slime_02 payload differs from hover:\nsent:\n%s\n%s\nhover:\n%s\n%s",
			slimeAttachment.Header, slimeAttachment.Text, hoverPayload.Header, hoverPayload.Text)
	}
	if !strings.Contains(received.AssembledContext, hoverPayload.Formatted()) {
		t.Fatalf("AssembledContext does not contain exact hover YAML block:\n%s", received.AssembledContext)
	}
	// Context ring updated from agent's reported usage: (24+12+4+6+10+6)k / 200k = 31%.
	meter := ed.ContextMeter()
	if meter.Percent != 31 || meter.Level != editor.ContextMeterNormal {
		t.Fatalf("ContextMeter = %d%% (level %d), want 31%% normal", meter.Percent, meter.Level)
	}
}

// Acceptance 2 (#1020): Selecting an object without adding it sends nothing extra;
// ⌘L with nothing selected attaches the whole scene.
func TestComposerSelectionOnlySuggestsAndCmdLAttachesScene(t *testing.T) {
	_, ed := setupComposerProject(t)

	// Baseline request with nothing selected and no attached chips.
	ed.SelectObject("")
	baseReq := ed.AssembleAgentRequest("Inspect level")

	// Select Knight: should appear ONLY as a suggested chip, never in the assembled request!
	if !ed.SelectObject("knight") {
		t.Fatal("failed to select knight")
	}
	sug, ok := ed.SuggestedChip()
	if !ok || sug.Target != "knight" || !sug.Suggested {
		t.Fatalf("SuggestedChip() = %+v, %v; want knight suggested", sug, ok)
	}
	withSelectionReq := ed.AssembleAgentRequest("Inspect level")
	if withSelectionReq.AssembledContext != baseReq.AssembledContext {
		t.Fatalf("selecting an object without adding it changed sent context:\nbase:\n%s\nwith selection:\n%s",
			baseReq.AssembledContext, withSelectionReq.AssembledContext)
	}
	for _, a := range withSelectionReq.Attachments {
		if a.Chip.Target == "knight" {
			t.Fatalf("unattached selected object knight was sent to agent: %+v", a)
		}
	}

	// Verify Always chips (brief.yaml, AGENTS.md) and Viewport image toggle work as expected.
	alwaysNames := make([]string, 0, len(ed.AlwaysChips()))
	for _, c := range ed.AlwaysChips() {
		alwaysNames = append(alwaysNames, c.Name)
	}
	if len(alwaysNames) != 2 || alwaysNames[0] != "brief.yaml" || alwaysNames[1] != "AGENTS.md" {
		t.Fatalf("AlwaysChips() = %v, want [brief.yaml AGENTS.md]", alwaysNames)
	}
	if !withSelectionReq.HasViewportImg || len(withSelectionReq.ViewportImage) == 0 {
		t.Fatal("expected Viewport image PNG in request while toggle is on")
	}
	ed.ToggleViewportImage()
	noVPReq := ed.AssembleAgentRequest("Inspect level")
	if noVPReq.HasViewportImg || len(noVPReq.ViewportImage) != 0 {
		t.Fatal("expected no Viewport image PNG after toggling Viewport image off")
	}

	// Clear selection and trigger ⌘L (TriggerAskAgentShortcut): must attach the whole scene.
	ed.SelectObject("")
	if err := ed.TriggerAskAgentShortcut(); err != nil {
		t.Fatalf("TriggerAskAgentShortcut with nothing selected: %v", err)
	}
	if ed.RightRailMode() != editor.RightRailAgent {
		t.Fatal("expected ⌘L to switch right rail to Agent tab")
	}
	chips := ed.ComposerChips()
	if len(chips) != 1 || chips[0].Kind != editor.ChipKindWholeScene {
		t.Fatalf("ComposerChips() after ⌘L with nothing selected = %+v, want whole scene chip", chips)
	}
	sceneHover := ed.ResolveChipPayload(chips[0])
	sceneReq := ed.AssembleAgentRequest("Audit level")
	if !strings.Contains(sceneReq.AssembledContext, sceneHover.Formatted()) {
		t.Fatalf("sent request missing exact whole-scene YAML shown on hover:\n%s", sceneReq.AssembledContext)
	}
	if !strings.Contains(sceneReq.AssembledContext, "id: door") || !strings.Contains(sceneReq.AssembledContext, "id: slime_02") {
		t.Fatalf("whole-scene attachment missing expected scene contents:\n%s", sceneReq.AssembledContext)
	}
}

// Acceptance 3 (#1020): Compact folds earlier requests into a summary, keeps the last request whole,
// and drops the context ring; Start fresh opens a new thread seeded with that summary.
func TestComposerContextRingBreakdownCompactAndStartFresh(t *testing.T) {
	_, ed := setupComposerProject(t)

	// Populate 5 turns and report 74% usage (148k of 200k), matching Threads.dc.html.
	for _, prompt := range []string{
		"Door opens after two lever pulls",
		"Click sound on the lever",
		"Lever glows while in reach",
		"Door opens over 0.6 s",
		"Open the door with a short rumble and a small camera shake.",
	} {
		ed.AddAgentThreadTurn(editor.AgentThreadTurn{
			Prompt: prompt,
			Reply:  "Applied change for: " + prompt,
			Tokens: 16_400,
		})
	}
	ed.SetAgentTokenUsage(editor.AgentTokenUsage{
		Limit:         200_000,
		Conversation:  82_000, // 41%
		OpenScene:     24_000, // 12%
		Attached:      14_000, // 7%
		Always:        12_000, // 6%
		ToolsAndInstr: 10_000, // 5%
		ViewportImage: 6_000,  // 3%
	})

	before := ed.ContextMeter()
	if before.Percent != 74 || before.Level != editor.ContextMeterWarning {
		t.Fatalf("before compact: ContextMeter = %d%% (level %d), want 74%% warning", before.Percent, before.Level)
	}
	if len(before.Rows) != 6 {
		t.Fatalf("breakdown rows = %d, want 6", len(before.Rows))
	}
	if !before.Rows[0].Highlight || before.Rows[0].Percent != 41 {
		t.Fatalf("conversation row = %+v, want 41%% highlighted", before.Rows[0])
	}

	// Verify 93% critical "compact now" level too.
	ed.SetAgentTokenUsage(editor.AgentTokenUsage{
		Limit:         200_000,
		Conversation:  120_000,
		OpenScene:     24_000,
		Attached:      14_000,
		Always:        12_000,
		ToolsAndInstr: 10_000,
		ViewportImage: 6_000,
	})
	crit := ed.ContextMeter()
	if crit.Percent != 93 || crit.Level != editor.ContextMeterCritical {
		t.Fatalf("critical ContextMeter = %d%% (level %d), want 93%% critical", crit.Percent, crit.Level)
	}

	// Compact thread via UI button.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	app.Press("id:right-tab-agent")
	app.Settle()

	app.Press("id:agent-context-ring-btn")
	app.Settle()
	if !ed.ContextBreakdownOpen() || !app.Exists("id:agent-context-breakdown") {
		t.Fatal("expected context breakdown popover to open when clicking context ring")
	}

	app.Press("id:agent-context-compact")
	app.Settle()

	afterCompact := ed.ContextMeter()
	if afterCompact.Percent >= crit.Percent {
		t.Fatalf("context ring did not drop after Compact: before=%d%% after=%d%%", crit.Percent, afterCompact.Percent)
	}
	turns := ed.AgentThreadTurns()
	if len(turns) != 1 {
		t.Fatalf("turns after Compact = %d, want 1 (last request kept whole)", len(turns))
	}
	if turns[0].Prompt != "Open the door with a short rumble and a small camera shake." {
		t.Fatalf("kept last turn prompt = %q, want latest request", turns[0].Prompt)
	}
	if summary := ed.AgentThreadSummary(); !strings.Contains(summary, "Door opens after two lever pulls") {
		t.Fatalf("compacted summary missing earlier requests:\n%s", summary)
	}

	// Start fresh with summary opens a new thread seeded with that summary.
	oldThreadID := ed.AgentThreadID()
	app.Press("id:agent-context-start-fresh")
	app.Settle()

	if ed.AgentThreadID() == oldThreadID {
		t.Fatalf("Start fresh did not create a new thread ID (still %q)", oldThreadID)
	}
	if len(ed.AgentThreadTurns()) != 0 {
		t.Fatalf("fresh thread turns = %d, want 0", len(ed.AgentThreadTurns()))
	}
	if seed := ed.AgentThreadSummary(); !strings.Contains(seed, "Open the door with a short rumble") {
		t.Fatalf("fresh thread summary seed missing prior work:\n%s", seed)
	}
	freshReq := ed.AssembleAgentRequest("Next step")
	if freshReq.SummarySeed == "" || !strings.Contains(freshReq.AssembledContext, "Thread summary") {
		t.Fatalf("fresh thread request missing summary seed:\n%+v", freshReq)
	}
}

// Covers all 8 @-mention picker groups, Tab cycling, Esc closing, click removal, and Backspace chip removal.
func TestComposerPickerGroupsTabsKeysAndChipRemoval(t *testing.T) {
	_, ed := setupComposerProject(t)

	// Add a history step and a playtest run so all groups have live items.
	if err := ed.MoveObject("slime_01", 240, 376); err != nil {
		t.Fatal(err)
	}
	ed.AddComposerPlaytestRun(editor.ComposerPlaytestRun{
		ID:      "run-1405",
		Name:    "Run 14:05 · 10 s",
		Summary: "slime_02 fell off the plank at 6.2 s",
		Detail:  "profile: cautious\nruns: 10\nslime_02 fell off the plank at 6.2 s",
	})

	// Check each group populates items over search.go and editor state.
	for _, group := range []editor.MentionGroup{
		editor.MentionGroupObjects,
		editor.MentionGroupFiles,
		editor.MentionGroupPrefabs,
		editor.MentionGroupAssets,
		editor.MentionGroupPlaytests,
		editor.MentionGroupHistory,
		editor.MentionGroupRules,
	} {
		items := ed.SearchMentions("", group)
		if len(items) == 0 {
			t.Errorf("SearchMentions(%q) returned 0 items, want >= 1", group)
		}
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	app.Press("id:right-tab-agent")
	app.Settle()

	// Open picker via @ button, cycle tabs with Tab, close with Esc.
	app.Press("id:agent-composer-at-btn")
	app.Settle()
	if !ed.MentionPickerOpen() || ed.MentionPickerGroup() != editor.MentionGroupAll {
		t.Fatalf("picker state = open:%v group:%q, want true/all", ed.MentionPickerOpen(), ed.MentionPickerGroup())
	}
	app.Key("tab")
	app.Settle()
	if ed.MentionPickerGroup() != editor.MentionGroupObjects {
		t.Fatalf("after Tab group = %q, want objects", ed.MentionPickerGroup())
	}
	app.Key("escape")
	app.Settle()
	if ed.MentionPickerOpen() {
		t.Fatal("expected Esc to close picker")
	}

	// Attach two chips, remove one by click and the other with Backspace in an empty composer.
	if _, err := ed.AttachObjectChip("door"); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.AttachObjectChip("lever_01"); err != nil {
		t.Fatal(err)
	}
	app.Settle()

	app.Press("id:agent-chip-remove-0")
	app.Settle()
	if chips := ed.ComposerChips(); len(chips) != 1 || chips[0].Target != "lever_01" {
		t.Fatalf("after click remove, chips = %+v, want [lever_01]", chips)
	}

	app.Focus("id:agent-composer-input")
	app.Key("backspace")
	app.Settle()
	if chips := ed.ComposerChips(); len(chips) != 0 {
		t.Fatalf("after Backspace in empty composer, chips = %+v, want empty", chips)
	}
}
