package editor_test

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
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

func makeFakeDimensionPNG(width, height uint32) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	writeChunk := func(typ string, data []byte) {
		_ = binary.Write(&buf, binary.BigEndian, uint32(len(data)))
		buf.WriteString(typ)
		buf.Write(data)
		crc := crc32.NewIEEE()
		_, _ = crc.Write([]byte(typ))
		_, _ = crc.Write(data)
		_ = binary.Write(&buf, binary.BigEndian, crc.Sum32())
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 6 // RGBA
	writeChunk("IHDR", ihdr)
	writeChunk("IEND", nil)
	return buf.Bytes()
}

func TestComposerSecurityPathTraversalSymlinkSizeAndImageBomb(t *testing.T) {
	dir, ed := setupComposerProject(t)

	outside := t.TempDir()
	secretPath := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("TOP_SECRET_OUTSIDE_DATA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outsideSub := filepath.Join(outside, "sub")
	if err := os.MkdirAll(outsideSub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideSub, "leak.md"), []byte("SUBDIR_SECRET_LEAK\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Path traversal, absolute path outside project, and .git paths are refused.
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("[core]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"../secret.txt",
		"rules/../../secret.txt",
		secretPath,
		".git/config",
		"C:\\Windows\\win.ini",
	} {
		if _, err := ed.AttachFileChip(bad); err == nil {
			t.Fatalf("AttachFileChip(%q) succeeded, want error", bad)
		}
		payload := ed.ResolveChipPayload(editor.AgentContextChip{
			ID:     "file:" + bad,
			Kind:   editor.ChipKindFile,
			Target: bad,
		})
		if strings.Contains(payload.Text, "TOP_SECRET_OUTSIDE_DATA") || strings.Contains(payload.Text, "[core]") {
			t.Fatalf("ResolveChipPayload(%q) leaked forbidden file content: %q", bad, payload.Text)
		}
	}

	// 2. Symlink file and symlinked parent directory are refused everywhere (AttachFileChip, AlwaysChips, SearchMentions, ResolveChipPayload).
	if err := os.Symlink(secretPath, filepath.Join(dir, "rules", "escape.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outsideSub, filepath.Join(dir, "rules", "escapedir")); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.AttachFileChip("rules/escape.md"); err == nil {
		t.Fatal("AttachFileChip must refuse symlink file")
	}
	if _, err := ed.AttachFileChip("rules/escapedir/leak.md"); err == nil {
		t.Fatal("AttachFileChip must refuse file under symlinked parent escaping project root")
	}
	if p := ed.ResolveChipPayload(editor.AgentContextChip{
		ID:     "rule:rules/escape.md",
		Kind:   editor.ChipKindRule,
		Target: "rules/escape.md",
	}); strings.Contains(p.Text, "TOP_SECRET_OUTSIDE_DATA") {
		t.Fatalf("ResolveChipPayload leaked symlinked rule content: %q", p.Text)
	}
	if p := ed.ResolveChipPayload(editor.AgentContextChip{
		ID:     "rule:rules/escapedir/leak.md",
		Kind:   editor.ChipKindRule,
		Target: "rules/escapedir/leak.md",
	}); strings.Contains(p.Text, "SUBDIR_SECRET_LEAK") {
		t.Fatalf("ResolveChipPayload leaked symlinked parent dir content: %q", p.Text)
	}

	// Replace brief.yaml and AGENTS.md with symlinks and reopen: AlwaysChips must refuse them.
	_ = os.Remove(filepath.Join(dir, "brief.yaml"))
	_ = os.Remove(filepath.Join(dir, "AGENTS.md"))
	if err := os.Symlink(secretPath, filepath.Join(dir, "brief.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secretPath, filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if always := ed.AlwaysChips(); len(always) != 0 {
		t.Fatalf("AlwaysChips() included symlinked brief.yaml/AGENTS.md: %+v", always)
	}
	for _, m := range ed.SearchMentions("", editor.MentionGroupRules) {
		if strings.Contains(m.Meta, "TOP_SECRET_OUTSIDE_DATA") || strings.Contains(m.Meta, "SUBDIR_SECRET_LEAK") {
			t.Fatalf("SearchMentions leaked symlinked rule summary: %+v", m)
		}
	}

	// 3. Oversized file (> 1 MiB) is refused cleanly without reading unbounded memory.
	bigData := bytes.Repeat([]byte("a"), (1<<20)+128)
	if err := os.WriteFile(filepath.Join(dir, "rules", "huge.md"), bigData, 0o644); err != nil {
		t.Fatal(err)
	}
	bigPayload := ed.ResolveChipPayload(editor.AgentContextChip{
		ID:     "file:rules/huge.md",
		Kind:   editor.ChipKindFile,
		Target: "rules/huge.md",
	})
	if bigPayload.Text != "" {
		t.Fatalf("oversized file should not be loaded into payload text, got len=%d", len(bigPayload.Text))
	}

	// 4. Image decompression bomb (5000x5000 PNG header) does not decode RGBA pixels.
	if err := os.WriteFile(filepath.Join(dir, "art", "enemies", "bomb.png"), makeFakeDimensionPNG(5000, 5000), 0o644); err != nil {
		t.Fatal(err)
	}
	bombPayload := ed.ResolveChipPayload(editor.AgentContextChip{
		ID:     "asset:art/enemies/bomb.png",
		Kind:   editor.ChipKindAsset,
		Target: "art/enemies/bomb.png",
	})
	if bombPayload.ImagePNG != nil || bombPayload.ImageSrc.Pixels != nil {
		t.Fatal("oversized dimension PNG must not be decoded into pixel buffers")
	}
	if !strings.Contains(bombPayload.Header, "5000 × 5000") {
		t.Fatalf("expected dimension header for oversized PNG, got %q", bombPayload.Header)
	}

	// 5. UTF-8 rune-safe truncation for multi-byte CJK rules and thread summaries.
	cjkRule := strings.Repeat("这是中文规则测试内容", 20) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rules", "cjk.md"), []byte(cjkRule), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	for _, m := range ed.SearchMentions("cjk", editor.MentionGroupRules) {
		if !utf8.ValidString(m.Meta) {
			t.Fatalf("CJK rule meta truncated mid-rune (invalid UTF-8): %q", m.Meta)
		}
	}
	ed.AddAgentThreadTurn(editor.AgentThreadTurn{
		Prompt: strings.Repeat("调整关卡障碍物与敌人速度", 20),
		Reply:  strings.Repeat("已完成史莱姆巡逻路径优化", 20),
	})
	ed.AddAgentThreadTurn(editor.AgentThreadTurn{Prompt: "最后一步微调"})
	summary, err := ed.CompactAgentThread()
	if err != nil {
		t.Fatalf("CompactAgentThread: %v", err)
	}
	if !utf8.ValidString(summary) {
		t.Fatalf("compacted CJK summary contains invalid UTF-8: %q", summary)
	}
}

func TestComposerAttachedChipCapAndContextBudget(t *testing.T) {
	dir, ed := setupComposerProject(t)

	// Create 35 small files and verify the 32-chip cap.
	for i := 0; i < 35; i++ {
		rel := filepath.Join("rules", "f"+string(rune('a'+i/26))+string(rune('a'+i%26))+".txt")
		if err := os.WriteFile(filepath.Join(dir, rel), []byte("ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := ed.AttachFileChip(filepath.ToSlash(rel))
		if i < 32 && err != nil {
			t.Fatalf("AttachFileChip #%d failed unexpectedly: %v", i, err)
		}
		if i >= 32 && err == nil {
			t.Fatalf("AttachFileChip #%d should fail once chip cap (32) is reached", i)
		}
	}
	if got := len(ed.ComposerChips()); got != 32 {
		t.Fatalf("len(ComposerChips()) = %d, want 32", got)
	}

	// Verify 2 MiB assembled context budget truncates cleanly with valid UTF-8.
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	chunk := strings.Repeat("关卡配置行内容\n", 45000) // ~945 KiB each, under 1 MiB per file
	for _, name := range []string{"big1.txt", "big2.txt", "big3.txt"} {
		if err := os.WriteFile(filepath.Join(dir, "rules", name), []byte(chunk), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ed.AttachFileChip("rules/" + name); err != nil {
			t.Fatalf("AttachFileChip(%s): %v", name, err)
		}
	}
	req := ed.AssembleAgentRequest("Review big files")
	if len(req.AssembledContext) > (2 << 20) {
		t.Fatalf("AssembledContext len = %d exceeds 2 MiB budget", len(req.AssembledContext))
	}
	if !utf8.ValidString(req.AssembledContext) {
		t.Fatal("truncated AssembledContext is not valid UTF-8")
	}
	if !strings.Contains(req.AssembledContext, "truncated to context budget") {
		t.Fatal("expected truncation marker when exceeding 2 MiB budget")
	}
}

func TestComposerFocusStabilityThemeSwitchAndUnmountTypingSafety(t *testing.T) {
	_, ed := setupComposerProject(t)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	app.Press("id:right-tab-agent")
	app.Settle()

	app.Focus("id:agent-composer-input")
	app.Settle()
	if !ed.IsTyping() {
		t.Fatal("expected IsTyping() == true while agent composer input is focused")
	}

	// Type @sli and toggle Dark -> Light -> Dark appearance mid-typing: input must stay mounted and focused.
	app.Type("id:agent-composer-input", "Move @sli")
	app.Settle()
	app.Post(func() {
		_ = ed.SetAppearance(editor.AppearanceLight)
	})
	app.Settle()
	if !app.Exists("id:agent-composer-input") {
		t.Fatal("composer input missing after switching to Spec Light")
	}
	if got := ed.ComposerDraft(); got != "Move @sli" {
		t.Fatalf("draft after theme switch = %q, want %q", got, "Move @sli")
	}
	if !ed.IsTyping() {
		t.Fatal("expected composer input to keep focus across Spec Dark -> Light switch")
	}

	// Switching right rail back to Inspector while focused unmounts AgentComposerView:
	// IsTyping() MUST return false (no leaked textInputFocusCount).
	app.Post(func() {
		ed.SetRightRailTab(editor.RightTabInspector)
	})
	app.Settle()
	if ed.IsTyping() {
		t.Fatal("IsTyping() remained stuck true after unmounting AgentComposerView")
	}
}

func TestComposerShortcutIsolationAndNoDoubleFire(t *testing.T) {
	_, ed := setupComposerProject(t)

	sendCount := 0
	ed.OnSendAgentComposerRequest = func(req editor.AgentRequest) (editor.AgentResponse, error) {
		sendCount++
		return editor.AgentResponse{Reply: "ok"}, nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	app.Press("id:right-tab-agent")
	app.Settle()

	// 1. Backspace in empty composer must remove EXACTLY 1 chip per press (no OnKeyPress + OnKeyWith double-fire).
	if _, err := ed.AttachObjectChip("door"); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.AttachObjectChip("lever_01"); err != nil {
		t.Fatal(err)
	}
	app.Settle()
	app.Focus("id:agent-composer-input")
	app.Key("backspace")
	app.Settle()
	if chips := ed.ComposerChips(); len(chips) != 1 || chips[0].Target != "door" {
		t.Fatalf("after 1 Backspace, chips = %+v, want [door] (1 removed, not 2)", chips)
	}

	// 2. Pressing Enter while @-mention picker is open adds the chip and NEVER prematurely sends.
	app.Type("id:agent-composer-input", "Adjust @slime_02")
	app.Settle()
	if !ed.MentionPickerOpen() {
		t.Fatal("expected mention picker open")
	}
	app.Key("enter")
	app.Settle()
	if sendCount != 0 {
		t.Fatalf("Enter on mention picker prematurely sent %d requests", sendCount)
	}
	if ed.MentionPickerOpen() {
		t.Fatal("expected mention picker closed after Enter")
	}

	// 3. When Settings modal opens while mention picker is open, Tab/Down/Esc must NOT drive the mention picker behind the modal.
	app.Post(func() {
		ed.OpenMentionPicker("sli")
		ed.SetSettingsOpen(true)
	})
	app.Settle()
	groupBefore := ed.MentionPickerGroup()
	idxBefore := ed.MentionPickerActiveIndex()
	app.Key("tab")
	app.Key("down")
	app.Settle()
	if ed.MentionPickerGroup() != groupBefore || ed.MentionPickerActiveIndex() != idxBefore {
		t.Fatalf("composer picker keys leaked behind Settings modal: group %q->%q idx %d->%d",
			groupBefore, ed.MentionPickerGroup(), idxBefore, ed.MentionPickerActiveIndex())
	}
	app.Post(func() {
		ed.SetSettingsOpen(false)
	})
	app.Settle()

	// 4. Opening Ask-in-place closes composer popovers cleanly.
	app.Post(func() {
		ed.OpenMentionPicker("sli")
		_ = ed.OpenAskInPlace()
	})
	app.Settle()
	if ed.MentionPickerOpen() {
		t.Fatal("opening Ask-in-place must close mention picker popover")
	}
}

func TestComposerLifecycleResetsAndLiveBriefRulesSync(t *testing.T) {
	dir, ed := setupComposerProject(t)

	// 1. Live edits to brief.yaml and AGENTS.md via #1026 APIs sync immediately into AlwaysChips & ResolveChipPayload.
	if err := ed.EditBrief("Update pitch", func(b *project.Brief) {
		b.Pitch = "Fast subterranean platformer"
	}); err != nil {
		t.Fatalf("EditBrief: %v", err)
	}
	if err := ed.SetProjectRules("Update project rules", "# Rules\n- Keep slime speed <= 60.\n"); err != nil {
		t.Fatalf("SetProjectRules: %v", err)
	}
	req := ed.AssembleAgentRequest("Check rules")
	if !strings.Contains(req.AssembledContext, "Fast subterranean platformer") {
		t.Fatalf("AssembledContext missing live brief edit:\n%s", req.AssembledContext)
	}
	if !strings.Contains(req.AssembledContext, "Keep slime speed <= 60.") {
		t.Fatalf("AssembledContext missing live AGENTS.md edit:\n%s", req.AssembledContext)
	}

	// 2. NewScene & SelectTab close transient popovers.
	ed.OpenMentionPicker("sli")
	ed.SetContextBreakdownOpen(true)
	if _, err := ed.NewScene("scenes/level_02.scene.yaml"); err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	if ed.MentionPickerOpen() || ed.ContextBreakdownOpen() {
		t.Fatal("NewScene must close transient composer popovers")
	}
	ed.OpenMentionPicker("sli")
	if err := ed.SelectTab(0); err != nil {
		t.Fatalf("SelectTab(0): %v", err)
	}
	if ed.MentionPickerOpen() {
		t.Fatal("SelectTab must close transient composer popovers")
	}

	// 3. Play-in-editor closes popovers and blocks TriggerAskAgentShortcut.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 800))
	app.Post(func() {
		ed.OpenMentionPicker("sli")
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	if ed.MentionPickerOpen() {
		t.Fatal("Play must close composer popovers")
	}
	if err := ed.TriggerAskAgentShortcut(); err != editor.ErrPlaying {
		t.Fatalf("TriggerAskAgentShortcut during Play = %v, want ErrPlaying", err)
	}
	app.Post(func() {
		_ = ed.Stop()
	})
	app.Settle()

	// 4. OpenDemo resets composer chips, draft, turns, and popovers.
	_, _ = ed.AttachObjectChip("door")
	ed.SetComposerDraft("Leftover draft")
	ed.AddAgentThreadTurn(editor.AgentThreadTurn{Prompt: "Old project turn"})
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if len(ed.ComposerChips()) != 0 || ed.ComposerDraft() != "" || len(ed.AgentThreadTurns()) != 0 {
		t.Fatalf("OpenDemo did not reset composer state: chips=%d draft=%q turns=%d",
			len(ed.ComposerChips()), ed.ComposerDraft(), len(ed.AgentThreadTurns()))
	}

	// 5. Reopening project resets cleanly too.
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if len(ed.ComposerChips()) != 0 || ed.MentionPickerOpen() || ed.ContextBreakdownOpen() {
		t.Fatal("OpenProject did not reset composer state")
	}
}
