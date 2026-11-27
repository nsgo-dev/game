package editor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func writeTwoSceneThreadProject(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	scenesDir := filepath.Join(dir, "scenes")
	if err := os.MkdirAll(scenesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	level1Doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "level_01",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#1a1f2a",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 10,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:   "Door",
						Name: "Door",
						X:    632,
						Y:    356,
						Properties: map[string]any{
							"locked":            true,
							"opens_after_pulls": 2,
						},
					},
					{
						ID:   "lever_01",
						Name: "lever_01",
						X:    598,
						Y:    372,
					},
					{
						ID:   "Knight",
						Name: "Knight",
						X:    444,
						Y:    286,
					},
				},
			},
		},
	}
	bossDoc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "boss_room",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#1a1f2a",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 10,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:   "boss_spawn",
						Name: "boss_spawn",
						X:    320,
						Y:    220,
					},
				},
			},
		},
	}
	l1Bytes, err := sceneformat.Encode(level1Doc)
	if err != nil {
		t.Fatal(err)
	}
	bossBytes, err := sceneformat.Encode(bossDoc)
	if err != nil {
		t.Fatal(err)
	}
	l1Path := filepath.Join(scenesDir, "level_01.scene.yaml")
	bossPath := filepath.Join(scenesDir, "boss_room.scene.yaml")
	if err := os.WriteFile(l1Path, l1Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bossPath, bossBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	proj := &project.File{
		Version:    project.CurrentVersion,
		Name:       "dungeon-run",
		ID:         "dev.nsgo.dungeonrun",
		AppVersion: "0.1.0",
		Window:     project.Window{Width: 640, Height: 360},
		Autoload: &project.Autoload{
			MainScene: "level_01",
			Scenes: []project.Entry{
				{Name: "level_01", Path: "scenes/level_01.scene.yaml"},
				{Name: "boss_room", Path: "scenes/boss_room.scene.yaml"},
			},
		},
	}
	projBytes, err := project.Encode(proj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.FileName), projBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	briefYAML := `suggestions:
  - "Block out level_01 with three platforms and an exit door"
  - "Add a slime that patrols the floor and costs the Knight one heart"
  - "Set up the HUD: three hearts and a coin counter"
`
	if err := os.WriteFile(filepath.Join(dir, "brief.yaml"), []byte(briefYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, "scenes/level_01.scene.yaml", "scenes/boss_room.scene.yaml"
}

// #1019 Acceptance 1: A test sends requests in two scenes, switches scenes and sees each scene's own thread;
// after reopening the project the threads and their states are back.
func TestAgentThreadsPerSceneSwitchingPersistenceAndGroups(t *testing.T) {
	dir, level1Rel, bossRel := writeTwoSceneThreadProject(t)

	now := time.Date(2026, 11, 26, 14, 20, 0, 0, time.UTC)
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.Now = func() time.Time { return now }
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if ed.ScenePath != level1Rel {
		t.Fatalf("initial ScenePath = %q, want %q", ed.ScenePath, level1Rel)
	}

	// Initially level_01 has an empty draft thread.
	initial := ed.ActiveAgentThread()
	if initial == nil || len(initial.Requests) != 0 || initial.ScenePath != level1Rel {
		t.Fatalf("initial active thread = %+v, want empty draft for %q", initial, level1Rel)
	}

	// Thread 1 in level_01: reverted yesterday.
	now = time.Date(2026, 11, 25, 13, 0, 0, 0, time.UTC)
	reqRev, err := ed.SendAgentRequestWith(editor.AgentSendRequest{
		Prompt:       "Spikes under the plank",
		SessionID:    "sess-rev",
		Outcome:      editor.AgentThreadOutcomeReverted,
		ChangesTotal: 2,
		ChangesKept:  0,
	})
	if err != nil {
		t.Fatalf("send reverted thread: %v", err)
	}
	revThreadID := ed.ActiveAgentThread().ID
	if reqRev.Outcome != editor.AgentThreadOutcomeReverted {
		t.Fatalf("reqRev.Outcome = %q", reqRev.Outcome)
	}

	// Thread 2 in level_01: 1 h ago, kept 5 of 6.
	ed.NewAgentThread()
	now = time.Date(2026, 11, 26, 13, 20, 0, 0, time.UTC)
	if _, err := ed.SendAgentRequestWith(editor.AgentSendRequest{
		Prompt:       "Slimes and a coin trail",
		SessionID:    "sess-slimes",
		Outcome:      editor.AgentThreadOutcomeKept,
		ChangesTotal: 6,
		ChangesKept:  5,
		Objects:      []string{"Knight"},
	}); err != nil {
		t.Fatalf("send slimes thread: %v", err)
	}

	// Thread 3 (most recent) in level_01: now, 3 changes to review, continuing one session ID.
	ed.NewAgentThread()
	now = time.Date(2026, 11, 26, 14, 18, 0, 0, time.UTC)
	if _, err := ed.SendAgentRequestWith(editor.AgentSendRequest{
		Prompt:       "Door opens after two lever pulls",
		SessionID:    "sess-door-42",
		Outcome:      editor.AgentThreadOutcomeKept,
		ChangesTotal: 3,
		ChangesKept:  3,
		Objects:      []string{"Door", "lever_01"},
		Detail:       "Door.locked, Door.opens_after_pulls, lever_01.pulls",
	}); err != nil {
		t.Fatalf("send door req 1: %v", err)
	}
	now = time.Date(2026, 11, 26, 14, 20, 0, 0, time.UTC)
	if _, err := ed.SendAgentRequestWith(editor.AgentSendRequest{
		Prompt:       "Open the door with a short rumble and a small camera shake.",
		Outcome:      editor.AgentThreadOutcomePending,
		ChangesTotal: 3,
		Files:        []string{"prefabs/door.prefab.yaml", "scenes/level_01.scene.yaml"},
		Objects:      []string{"Door"},
	}); err != nil {
		t.Fatalf("send door req 2: %v", err)
	}
	doorThread := ed.ActiveAgentThread()
	if doorThread.Title != "Door opens after two lever pulls" {
		t.Fatalf("doorThread.Title = %q, want named after first request", doorThread.Title)
	}
	if doorThread.SessionID != "sess-door-42" {
		t.Fatalf("doorThread.SessionID = %q, want sess-door-42 preserved across requests", doorThread.SessionID)
	}

	// Project-wide thread (Project · no scene).
	ed.NewProjectAgentThread()
	now = time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	if _, err := ed.SendAgentRequestWith(editor.AgentSendRequest{
		Prompt:  "Double jump for the Knight",
		Outcome: editor.AgentThreadOutcomeKept,
		Files:   []string{"game.yaml", "prefabs/knight.prefab.yaml"},
	}); err != nil {
		t.Fatalf("send project thread: %v", err)
	}
	projectThreadID := ed.ActiveAgentThread().ID

	// Switch to boss_room: switching scenes MUST NOT carry level_01's or project's thread along.
	if err := ed.OpenScene(bossRel); err != nil {
		t.Fatalf("OpenScene(boss_room): %v", err)
	}
	bossDraft := ed.ActiveAgentThread()
	if bossDraft == nil || bossDraft.ScenePath != bossRel || len(bossDraft.Requests) != 0 {
		t.Fatalf("switching to boss_room carried thread along: %+v", bossDraft)
	}

	// Send a request in boss_room.
	now = time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC)
	if _, err := ed.SendAgentRequestWith(editor.AgentSendRequest{
		Prompt:       "Torches around the boss arena",
		SessionID:    "sess-boss-99",
		Outcome:      editor.AgentThreadOutcomeKept,
		ChangesTotal: 4,
		ChangesKept:  4,
		Objects:      []string{"boss_spawn"},
	}); err != nil {
		t.Fatalf("send boss thread: %v", err)
	}
	bossThreadID := ed.ActiveAgentThread().ID

	// Switch back to level_01: enters level_01's most recent thread automatically!
	now = time.Date(2026, 11, 26, 14, 20, 0, 0, time.UTC)
	if err := ed.OpenScene(level1Rel); err != nil {
		t.Fatalf("OpenScene(level_01): %v", err)
	}
	if got := ed.ActiveAgentThread(); got == nil || got.ID != doorThread.ID {
		t.Fatalf("returning to level_01 active thread = %+v, want %q", got, doorThread.ID)
	}

	// Switch back to boss_room: enters boss_room's own thread!
	if err := ed.OpenScene(bossRel); err != nil {
		t.Fatalf("OpenScene(boss_room) again: %v", err)
	}
	if got := ed.ActiveAgentThread(); got == nil || got.ID != bossThreadID {
		t.Fatalf("returning to boss_room active thread = %+v, want %q", got, bossThreadID)
	}

	// Switch back to level_01 and verify switcher groups & states match Threads.dc.html!
	if err := ed.OpenScene(level1Rel); err != nil {
		t.Fatal(err)
	}
	groups := ed.AgentThreadGroups("")
	if len(groups) != 3 {
		t.Fatalf("AgentThreadGroups count = %d, want 3 (open scene, boss_room, project): %+v", len(groups), groups)
	}
	if groups[0].Heading != "LEVEL_01 · OPEN SCENE" || !groups[0].OpenScene || len(groups[0].Threads) != 3 {
		t.Fatalf("group[0] = %+v", groups[0])
	}
	if got := groups[0].Threads[0].Meta; got != "now · 3 changes to review" {
		t.Fatalf("door thread meta = %q, want %q", got, "now · 3 changes to review")
	}
	if got := groups[0].Threads[1].Meta; got != "1 h ago · kept 5 of 6" {
		t.Fatalf("slimes thread meta = %q, want %q", got, "1 h ago · kept 5 of 6")
	}
	if got := groups[0].Threads[2].Meta; got != "yesterday · reverted" {
		t.Fatalf("reverted thread meta = %q, want %q", got, "yesterday · reverted")
	}
	if groups[1].Heading != "BOSS_ROOM" || len(groups[1].Threads) != 1 || groups[1].Threads[0].Meta != "yesterday · kept 4 of 4" {
		t.Fatalf("group[1] = %+v", groups[1])
	}
	if groups[2].Heading != "PROJECT · NO SCENE" || !groups[2].Project || len(groups[2].Threads) != 1 || groups[2].Threads[0].ID != projectThreadID {
		t.Fatalf("group[2] = %+v", groups[2])
	}
	if !strings.Contains(groups[2].Threads[0].Meta, "game.yaml, knight.prefab.yaml") {
		t.Fatalf("project thread meta = %q, want files list", groups[2].Threads[0].Meta)
	}

	// Search filters groups cleanly.
	filtered := ed.AgentThreadGroups("torches")
	if len(filtered) != 1 || filtered[0].Heading != "BOSS_ROOM" || len(filtered[0].Threads) != 1 {
		t.Fatalf("AgentThreadGroups(torches) = %+v", filtered)
	}

	// Verify .nsgo/threads/<id>.yaml files and .nsgo/.gitignore exist on disk.
	gitignoreBytes, err := os.ReadFile(filepath.Join(dir, ".nsgo", ".gitignore"))
	if err != nil || strings.TrimSpace(string(gitignoreBytes)) != "*" {
		t.Fatalf(".nsgo/.gitignore = %q err=%v, want *", string(gitignoreBytes), err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".nsgo", "threads", doorThread.ID+".yaml")); err != nil {
		t.Fatalf("missing persisted door thread file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".nsgo", "threads", revThreadID+".yaml")); err != nil {
		t.Fatalf("missing persisted reverted thread file: %v", err)
	}

	// Reopen project in a brand-new Editor instance: all threads, session IDs, and states must be back!
	ed2, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed2.Now = func() time.Time { return now }
	if err := ed2.OpenProject(dir); err != nil {
		t.Fatalf("reopen OpenProject: %v", err)
	}
	gotDoor := ed2.ActiveAgentThread()
	if gotDoor == nil || gotDoor.ID != doorThread.ID || gotDoor.SessionID != "sess-door-42" || len(gotDoor.Requests) != 2 {
		t.Fatalf("after reopen active thread = %+v, want %q with session sess-door-42 and 2 requests", gotDoor, doorThread.ID)
	}
	reopenedGroups := ed2.AgentThreadGroups("")
	if len(reopenedGroups) != 3 {
		t.Fatalf("reopened groups = %d, want 3", len(reopenedGroups))
	}
	if reopenedGroups[0].Threads[0].Meta != "now · 3 changes to review" ||
		reopenedGroups[0].Threads[1].Meta != "1 h ago · kept 5 of 6" ||
		reopenedGroups[0].Threads[2].Meta != "yesterday · reverted" {
		t.Fatalf("reopened level_01 metas = %+v", reopenedGroups[0].Threads)
	}
	if reopenedGroups[1].Threads[0].SessionID != "sess-boss-99" {
		t.Fatalf("reopened boss thread sessionId = %q, want sess-boss-99", reopenedGroups[1].Threads[0].SessionID)
	}

	// Selecting another scene's thread from the switcher opens that scene and activates that thread.
	if err := ed2.SelectAgentThread(bossThreadID); err != nil {
		t.Fatalf("SelectAgentThread(boss): %v", err)
	}
	if ed2.ScenePath != bossRel || ed2.ActiveAgentThread().ID != bossThreadID {
		t.Fatalf("after SelectAgentThread(boss): ScenePath=%q active=%+v", ed2.ScenePath, ed2.ActiveAgentThread())
	}
}

// #1019 Acceptance 2 + Folding + History link + Jump to latest + Empty thread + ⌘N UI flow.
func TestAgentThreadsInspectorFooterFoldingHistoryEmptyAndShortcut(t *testing.T) {
	dir, _, _ := writeTwoSceneThreadProject(t)

	now := time.Date(2026, 11, 26, 14, 20, 0, 0, time.UTC)
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.Now = func() time.Time { return now }
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Record 2 requests on level_01: req1 touched Door 2 min ago, req2 touched lever_01 now, req3 reverted on Door.
	now = time.Date(2026, 11, 26, 14, 18, 0, 0, time.UTC)
	req1, err := ed.SendAgentRequestWith(editor.AgentSendRequest{
		Title:        "Door opens after two lever pulls",
		Prompt:       "Keep the door shut until the lever is pulled twice, then let it open.",
		Outcome:      editor.AgentThreadOutcomeKept,
		ChangesTotal: 3,
		ChangesKept:  3,
		Objects:      []string{"Door", "lever_01"},
		Detail:       "Door.locked, Door.opens_after_pulls, lever_01.pulls",
		Steps: []editor.AgentThreadStep{
			{ID: "s1", Label: "Set Door.opens_after_pulls = 2", Status: editor.AgentThreadStepDone, Objects: []string{"Door"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	now = time.Date(2026, 11, 26, 14, 19, 0, 0, time.UTC)
	if _, err := ed.SendAgentRequestWith(editor.AgentSendRequest{
		Title:        "Door opens over 0.6 s",
		Prompt:       "Slow the door opening down to 0.6 s.",
		Outcome:      editor.AgentThreadOutcomeReverted,
		ChangesTotal: 1,
		Objects:      []string{"Door"},
	}); err != nil {
		t.Fatal(err)
	}
	now = time.Date(2026, 11, 26, 14, 20, 0, 0, time.UTC)
	if _, err := ed.SendAgentRequestWith(editor.AgentSendRequest{
		Title:        "Click sound on the lever",
		Prompt:       "Add a click sound when the lever moves.",
		Outcome:      editor.AgentThreadOutcomePending,
		ChangesTotal: 2,
		Files:        []string{"scenes/level_01.scene.yaml"},
		Objects:      []string{"lever_01"},
	}); err != nil {
		t.Fatal(err)
	}
	threadID := ed.ActiveAgentThread().ID

	// Select Door in Inspector: reverted req2 MUST NOT override req1 attribution!
	ed.SelectObject("Door")
	attr, ok := ed.LastAgentChangeForObject("Door")
	if !ok || attr.RequestID != req1.ID || attr.RequestTitle != "Door opens after two lever pulls" || attr.AgeLabel != "2 min ago" {
		t.Fatalf("LastAgentChangeForObject(Door) = (%+v, %v)", attr, ok)
	}

	var openedHistory editor.AgentHistorySelection
	ed.OnOpenHistoryStep = func(sel editor.AgentHistorySelection) {
		openedHistory = sel
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"}))

	// Inspector footer names the request that last changed Door and opens it when pressed.
	footerText := app.Find("id:inspector-thread-footer-text").Text
	wantFooter := "Changed in “Door opens after two lever pulls” · 2 min ago"
	if footerText != wantFooter {
		t.Fatalf("inspector-thread-footer-text = %q, want %q", footerText, wantFooter)
	}
	app.Press("id:inspector-thread-footer")

	if ed.RightRailTab() != editor.RightRailAgent {
		t.Fatalf("after clicking inspector footer, RightRailTab = %v, want RightRailAgent", ed.RightRailTab())
	}
	if ed.ActiveAgentThread().ID != threadID || ed.ExpandedAgentRequestID() != req1.ID {
		t.Fatalf("after clicking inspector footer: thread=%q expanded=%q, want %q / %q", ed.ActiveAgentThread().ID, ed.ExpandedAgentRequestID(), threadID, req1.ID)
	}
	if !app.Exists("id:agent-request-detail-"+req1.ID) || !app.Exists("id:agent-jump-latest") {
		t.Fatal("expanded earlier request detail and Jump to latest pill should both be visible")
	}

	// Click "Open this step in History" from the expanded request.
	app.Press("id:agent-open-history-" + req1.ID)
	if ed.RightRailTab() != editor.RightRailHistory {
		t.Fatalf("after Open in History, RightRailTab = %v, want RightRailHistory", ed.RightRailTab())
	}
	if openedHistory.ThreadID != threadID || openedHistory.RequestID != req1.ID || openedHistory.StepID != "s1" {
		t.Fatalf("openedHistory = %+v, want thread=%q req=%q step=s1", openedHistory, threadID, req1.ID)
	}
	if !app.Exists("id:agent-history-panel") {
		t.Fatal("History panel did not render")
	}

	// Return to thread from History and press "Jump to latest".
	app.Press("id:history-open-thread-" + req1.ID)
	if ed.RightRailTab() != editor.RightRailAgent || ed.ExpandedAgentRequestID() != req1.ID {
		t.Fatalf("back to thread failed: tab=%v expanded=%q", ed.RightRailTab(), ed.ExpandedAgentRequestID())
	}
	app.Press("id:agent-jump-latest")
	if ed.ExpandedAgentRequestID() != "" || app.Exists("id:agent-jump-latest") {
		t.Fatalf("Jump to latest did not collapse expanded request: expanded=%q", ed.ExpandedAgentRequestID())
	}

	// Press ⌘N in Agent scope to start a new empty thread (without opening Start Screen!).
	app.Key("meta+n")
	if ed.StartScreenOpen() {
		t.Fatal("meta+n inside Agent panel unexpectedly opened Start Screen instead of creating a new thread")
	}
	if active := ed.ActiveAgentThread(); active == nil || len(active.Requests) != 0 {
		t.Fatalf("after meta+n, active thread = %+v, want new empty thread", active)
	}
	if !app.Exists("id:agent-empty-thread") || !app.Exists("id:agent-empty-context-scene") || !app.Exists("id:agent-empty-context-brief") || !app.Exists("id:agent-empty-context-rules") || !app.Exists("id:agent-empty-context-viewport") {
		t.Fatal("empty thread view missing expected always-read context chips")
	}
	if got := app.Find("id:agent-empty-context-scene").Children[0].Text; got != "level_01 · whole scene" {
		t.Fatalf("scene context chip = %q, want level_01 · whole scene", got)
	}
	// Brief suggestions loaded from brief.yaml.
	if !app.Exists("id:agent-brief-suggestion-0") {
		t.Fatal("brief suggestion 0 missing")
	}
	suggText := app.Find("id:agent-brief-suggestion-0").Children[0].Text
	if suggText != "Block out level_01 with three platforms and an exit door" {
		t.Fatalf("brief suggestion 0 = %q", suggText)
	}

	// Clicking a suggestion sends it and names the thread after it!
	app.Press("id:agent-brief-suggestion-0")
	if active := ed.ActiveAgentThread(); active == nil || active.Title != "Block out level_01 with three platforms and an exit door" || len(active.Requests) != 1 {
		t.Fatalf("after clicking suggestion: active thread = %+v", active)
	}
}

func TestAgentThreadBriefSymlinkOutsideProjectRejected(t *testing.T) {
	dir, _, _ := writeTwoSceneThreadProject(t)
	outsideDir := t.TempDir()
	outsideBrief := filepath.Join(outsideDir, "secret.yaml")
	if err := os.WriteFile(outsideBrief, []byte("suggestions:\n  - \"LEAKED_OUTSIDE_SUGGESTION\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "brief.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideBrief, filepath.Join(dir, "brief.yaml")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	for _, s := range ed.AgentBriefSuggestions() {
		if strings.Contains(s, "LEAKED_OUTSIDE_SUGGESTION") {
			t.Fatalf("AgentBriefSuggestions read symlinked brief.yaml outside project root: %q", s)
		}
	}
}

func TestAgentThreadYAMLStrictValidationAndRoundTrip(t *testing.T) {
	valid := &editor.AgentThread{
		Version:   editor.AgentThreadFileVersion,
		ID:        "door-lever-pulls",
		Title:     "Door opens after two lever pulls",
		ScenePath: "scenes/level_01.scene.yaml",
		SessionID: "sess_01",
		CreatedAt: "2026-11-26T13:40:00Z",
		UpdatedAt: "2026-11-26T14:18:00Z",
		Requests: []editor.AgentThreadRequest{
			{
				ID:           "req-001",
				Title:        "Door opens after two lever pulls",
				Prompt:       "Keep the door shut until the lever is pulled twice.",
				CreatedAt:    "2026-11-26T13:40:00Z",
				UpdatedAt:    "2026-11-26T13:41:00Z",
				Outcome:      editor.AgentThreadOutcomeKept,
				ChangesTotal: 3,
				ChangesKept:  3,
				Files:        []string{"scenes/level_01.scene.yaml"},
				Objects:      []string{"Door"},
			},
		},
	}
	enc1, err := editor.EncodeAgentThreadYAML(valid)
	if err != nil {
		t.Fatalf("EncodeAgentThreadYAML: %v", err)
	}
	parsed, err := editor.ParseAgentThreadYAML(enc1)
	if err != nil {
		t.Fatalf("ParseAgentThreadYAML: %v", err)
	}
	enc2, err := editor.EncodeAgentThreadYAML(parsed)
	if err != nil {
		t.Fatalf("EncodeAgentThreadYAML round-trip: %v", err)
	}
	if !bytes.Equal(enc1, enc2) {
		t.Fatalf("round-trip mismatch:\n--- enc1 ---\n%s\n--- enc2 ---\n%s", enc1, enc2)
	}

	badCases := map[string]string{
		"empty":        "",
		"missingVer":   "id: t1\ncreatedAt: \"2026-11-26T13:40:00Z\"\nupdatedAt: \"2026-11-26T13:40:00Z\"\n",
		"wrongVer":     "version: 99\nid: t1\ncreatedAt: \"2026-11-26T13:40:00Z\"\nupdatedAt: \"2026-11-26T13:40:00Z\"\n",
		"unknownField": "version: 1\nid: t1\nextra: true\ncreatedAt: \"2026-11-26T13:40:00Z\"\nupdatedAt: \"2026-11-26T13:40:00Z\"\n",
		"badID":        "version: 1\nid: ../escape\ncreatedAt: \"2026-11-26T13:40:00Z\"\nupdatedAt: \"2026-11-26T13:40:00Z\"\n",
		"badScenePath": "version: 1\nid: t1\nscenePath: ../outside.yaml\ncreatedAt: \"2026-11-26T13:40:00Z\"\nupdatedAt: \"2026-11-26T13:40:00Z\"\n",
		"dupKey":       "version: 1\nid: t1\nid: t2\ncreatedAt: \"2026-11-26T13:40:00Z\"\nupdatedAt: \"2026-11-26T13:40:00Z\"\n",
		"anchor":       "version: 1\nid: &a t1\ncreatedAt: \"2026-11-26T13:40:00Z\"\nupdatedAt: \"2026-11-26T13:40:00Z\"\n",
		"keptGtTotal":  "version: 1\nid: t1\ncreatedAt: \"2026-11-26T13:40:00Z\"\nupdatedAt: \"2026-11-26T13:40:00Z\"\nrequests:\n  - id: r1\n    prompt: hi\n    createdAt: \"2026-11-26T13:40:00Z\"\n    outcome: kept\n    changesTotal: 2\n    changesKept: 3\n",
		"badOutcome":   "version: 1\nid: t1\ncreatedAt: \"2026-11-26T13:40:00Z\"\nupdatedAt: \"2026-11-26T13:40:00Z\"\nrequests:\n  - id: r1\n    prompt: hi\n    createdAt: \"2026-11-26T13:40:00Z\"\n    outcome: mystery\n",
		"trailingDoc":  "version: 1\nid: t1\ncreatedAt: \"2026-11-26T13:40:00Z\"\nupdatedAt: \"2026-11-26T13:40:00Z\"\n---\nversion: 1\n",
	}
	for name, src := range badCases {
		if _, err := editor.ParseAgentThreadYAML([]byte(src)); err == nil {
			t.Errorf("ParseAgentThreadYAML(%s) succeeded, want error", name)
		}
	}
}
