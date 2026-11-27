package editor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func writeAutomationsProjectFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	scenesDir := filepath.Join(dir, "scenes")
	if err := os.MkdirAll(scenesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	levelYAML := "version: 1\nname: Level 01\nlayers:\n  - name: world\n    space: world\n    order: 0\n"
	if err := os.WriteFile(filepath.Join(scenesDir, "level_01.scene.yaml"), []byte(levelYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	pf := &project.File{
		Version:    project.CurrentVersion,
		Name:       "Dungeon Run",
		ID:         "dev.nsgo.dungeonrun",
		AppVersion: "0.4.2",
		Window: project.Window{
			Width:  640,
			Height: 360,
		},
		Autoload: &project.Autoload{
			Scenes: []project.Entry{
				{Name: "level_01", Path: "scenes/level_01.scene.yaml"},
			},
			MainScene: "level_01",
		},
	}
	data, err := project.Encode(pf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.FileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAutomationsYAMLStrictValidationAndRoundTrip(t *testing.T) {
	def := editor.DefaultAutomations()
	enc, err := editor.EncodeAutomations(def)
	if err != nil {
		t.Fatalf("EncodeAutomations: %v", err)
	}
	if !strings.HasPrefix(string(enc), editor.AutomationsSchemaComment+"\n") {
		t.Fatalf("encoded automations missing schema comment: %s", string(enc))
	}
	parsed, err := editor.ParseAutomations(enc)
	if err != nil {
		t.Fatalf("ParseAutomations: %v", err)
	}
	reenc, err := editor.EncodeAutomations(parsed)
	if err != nil {
		t.Fatalf("EncodeAutomations(reencode): %v", err)
	}
	if string(reenc) != string(enc) {
		t.Fatalf("round-trip mismatch:\ngot:\n%s\nwant:\n%s", reenc, enc)
	}

	invalidCases := map[string]string{
		"empty":           "",
		"missing_version": "automations: []\n",
		"wrong_version":   "version: 2\nautomations: []\n",
		"unknown_field":   "version: 1\nextra: true\nautomations: []\n",
		"trailing_doc":    "version: 1\nautomations: []\n---\nversion: 1\n",
		"duplicate_key":   "version: 1\nversion: 1\n",
		"yaml_anchor":     "version: 1\nautomations:\n  - &a\n    id: x\n    enabled: true\n    trigger:\n      event: before_export\n    instruction: check\n",
		"duplicate_id": "version: 1\nautomations:\n" +
			"  - id: dup\n    enabled: true\n    trigger:\n      event: before_export\n    instruction: a\n" +
			"  - id: dup\n    enabled: false\n    trigger:\n      event: review_kept\n    instruction: b\n",
		"file_changed_no_glob":   "version: 1\nautomations:\n  - id: x\n    enabled: true\n    trigger:\n      event: file_changed\n    instruction: check\n",
		"file_changed_bad_glob":  "version: 1\nautomations:\n  - id: x\n    enabled: true\n    trigger:\n      event: file_changed\n      glob: ../outside/*.yaml\n    instruction: check\n",
		"file_changed_with_time": "version: 1\nautomations:\n  - id: x\n    enabled: true\n    trigger:\n      event: file_changed\n      glob: scenes/**\n      time: \"02:00\"\n    instruction: check\n",
		"file_added_no_kind":     "version: 1\nautomations:\n  - id: x\n    enabled: true\n    trigger:\n      event: file_added\n    instruction: check\n",
		"daily_bad_time":         "version: 1\nautomations:\n  - id: x\n    enabled: true\n    trigger:\n      event: daily\n      time: \"25:99\"\n    instruction: check\n",
		"bad_platform":           "version: 1\nautomations:\n  - id: x\n    enabled: true\n    trigger:\n      event: before_export\n      platform: gameboy\n    instruction: check\n",
		"empty_instruction":      "version: 1\nautomations:\n  - id: x\n    enabled: true\n    trigger:\n      event: before_export\n    instruction: \"   \"\n",
	}
	for name, raw := range invalidCases {
		_, err := editor.ParseAutomations([]byte(raw))
		if err == nil {
			t.Errorf("ParseAutomations(%s) succeeded, want error", name)
			continue
		}
		var le *loaderr.Error
		if !errors.As(err, &le) {
			t.Errorf("ParseAutomations(%s) error %T is not *loaderr.Error", name, err)
		}
	}

	// Verify glob matcher and file kind classifier.
	if !editor.MatchAutomationGlob("scenes/level_*.scene.yaml", "scenes/level_01.scene.yaml") {
		t.Fatal("expected level_01 to match scenes/level_*.scene.yaml")
	}
	if !editor.MatchAutomationGlob("scenes/**/*.scene.yaml", "scenes/level_01.scene.yaml") {
		t.Fatal("expected ** to match zero subdirectories")
	}
	if !editor.MatchAutomationGlob("scenes/**/*.scene.yaml", "scenes/world1/floor2/boss.scene.yaml") {
		t.Fatal("expected ** to match nested subdirectories")
	}
	if editor.MatchAutomationGlob("scenes/level_*.scene.yaml", "prefabs/slime.prefab.yaml") {
		t.Fatal("prefab unexpectedly matched level glob")
	}
	if got := editor.ClassifyAutomationFileKind("prefabs/goblin.prefab.yaml"); got != editor.AutomationFileKindPrefab {
		t.Fatalf("ClassifyAutomationFileKind(prefab) = %q, want prefab", got)
	}
	if got := editor.ClassifyAutomationFileKind("scenes/level_02.scene.yaml"); got != editor.AutomationFileKindScene {
		t.Fatalf("ClassifyAutomationFileKind(scene) = %q, want scene", got)
	}
	if got := editor.ClassifyAutomationFileKind("scripts/patrol.go"); got != editor.AutomationFileKindScript {
		t.Fatalf("ClassifyAutomationFileKind(script) = %q, want script", got)
	}
}

// #1029 Acceptance 1: saving a level file starts an automation task whose result lands in Ready to review,
// with a checkpoint taken first, under the same permission rules, and never able to apply You-only drafts.
func TestAutomationsSaveLevelStartsTaskWithCheckpointFirstAndReadyToReview(t *testing.T) {
	dir := writeAutomationsProjectFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	var (
		mu                  sync.Mutex
		gotReq              editor.AgentRequest
		checkpointExisted   bool
		manifestOnDisk      bool
		actorActiveInRunner bool
		youOnlyKeepErr      error
		youOnlyDraftErr     error
		youOnlyToggleErr    error
		youOnlyPauseErr     error
	)

	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		cp, ok := ed.AutomationCheckpointByID(req.CheckpointID)
		manifestOk := false
		if ok && cp.Dir != "" {
			if _, serr := os.Stat(filepath.Join(cp.Dir, "manifest.yaml")); serr == nil {
				manifestOk = true
			}
		}
		actorActive := ed.AgentActorActive()
		keepErr := ed.KeepAgentTurnReview("auto-turn-1")
		draftErr := ed.KeepAutomationDraft()
		toggleErr := ed.SetAutomationEnabled("level-check", false)
		pauseErr := ed.PauseAutomationsForToday()

		mu.Lock()
		gotReq = req
		checkpointExisted = ok
		manifestOnDisk = manifestOk
		actorActiveInRunner = actorActive
		youOnlyKeepErr = keepErr
		youOnlyDraftErr = draftErr
		youOnlyToggleErr = toggleErr
		youOnlyPauseErr = pauseErr
		mu.Unlock()

		emit(editor.AgentEvent{
			Kind:       editor.AgentEventStep,
			StepID:     "s1",
			StepTitle:  "Ran game check and 5 playtests",
			StepStatus: editor.AgentStepDone,
		})
		emit(editor.AgentEvent{
			Kind: editor.AgentEventMessage,
			Text: "Found 1 gap jump problem on level_01.",
		})
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})

	// Edit and save the level file.
	if err := ed.SetSceneName("Level 01 Updated"); err != nil {
		t.Fatal(err)
	}
	if err := ed.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !ed.WaitAutomations(3 * time.Second) {
		t.Fatal("automation did not finish within timeout")
	}

	mu.Lock()
	reqCopy := gotReq
	cpExisted := checkpointExisted
	cpManifest := manifestOnDisk
	actorActive := actorActiveInRunner
	keepErrCopy := youOnlyKeepErr
	draftErrCopy := youOnlyDraftErr
	toggleErrCopy := youOnlyToggleErr
	pauseErrCopy := youOnlyPauseErr
	mu.Unlock()

	if reqCopy.AutomationID != "level-check" {
		t.Fatalf("AutomationID = %q, want level-check", reqCopy.AutomationID)
	}
	if reqCopy.CheckpointID == "" || !cpExisted || !cpManifest {
		t.Fatalf("checkpoint must exist on disk before runner starts: id=%q existed=%v manifest=%v", reqCopy.CheckpointID, cpExisted, cpManifest)
	}
	if !actorActive {
		t.Fatal("expected AgentActorActive() == true inside automation runner")
	}
	if reqCopy.Permissions != ed.AgentPermissions() {
		t.Fatalf("automation runner received permissions %+v, want %+v", reqCopy.Permissions, ed.AgentPermissions())
	}
	for name, gotErr := range map[string]error{
		"KeepAgentTurnReview":    keepErrCopy,
		"KeepAutomationDraft":    draftErrCopy,
		"SetAutomationEnabled":   toggleErrCopy,
		"PauseAutomationsForDay": pauseErrCopy,
	} {
		if !errors.Is(gotErr, editor.ErrAutomationYouOnly) {
			t.Errorf("%s inside automation = %v, want ErrAutomationYouOnly", name, gotErr)
		}
	}

	ready := ed.ReadyToReviewTurns()
	if len(ready) != 1 {
		t.Fatalf("ReadyToReviewTurns len = %d, want 1", len(ready))
	}
	turn := ready[0]
	if !turn.Automation || turn.ReviewState != editor.AgentReviewReady || turn.CheckpointID != reqCopy.CheckpointID {
		t.Fatalf("unexpected automation turn: %+v", turn)
	}
	if !strings.Contains(turn.Summary, "level_01") {
		t.Fatalf("turn summary = %q, want level_01 report", turn.Summary)
	}
	if runs := ed.AutomationWeeklyRuns("level-check"); runs != 1 {
		t.Fatalf("AutomationWeeklyRuns(level-check) = %d, want 1", runs)
	}

	// Verify checkpoint actually restores pre-run file contents.
	levelPath := filepath.Join(dir, "scenes", "level_01.scene.yaml")
	savedBeforeMutate, err := os.ReadFile(levelPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(levelPath, []byte("version: 1\nname: Corrupted\nlayers:\n  - name: world\n    space: world\n    order: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.RestoreAutomationCheckpoint(reqCopy.CheckpointID); err != nil {
		t.Fatalf("RestoreAutomationCheckpoint: %v", err)
	}
	restored, err := os.ReadFile(levelPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(savedBeforeMutate) {
		t.Fatalf("checkpoint restore did not recover original level file")
	}

	// Human can keep the Ready-to-review turn cleanly.
	if err := ed.KeepAgentTurnReview(turn.ID); err != nil {
		t.Fatalf("KeepAgentTurnReview: %v", err)
	}
	if !ed.WaitAutomations(3 * time.Second) {
		t.Fatal("follow-up review_kept automation did not finish")
	}
	// Keeping level-check's review triggered review-brief; keeping review-brief's own review must NOT recurse.
	if runs := ed.AutomationWeeklyRuns("review-brief"); runs != 1 {
		t.Fatalf("AutomationWeeklyRuns(review-brief) = %d, want 1", runs)
	}
	for _, rt := range ed.ReadyToReviewTurns() {
		if rt.AutomationID == "review-brief" {
			if err := ed.KeepAgentTurnReview(rt.ID); err != nil {
				t.Fatalf("KeepAgentTurnReview(review-brief): %v", err)
			}
		}
	}
	if !ed.WaitAutomations(2 * time.Second) {
		t.Fatal("wait after keeping review-brief failed")
	}
	if runs := ed.AutomationWeeklyRuns("review-brief"); runs != 1 {
		t.Fatalf("keeping review-brief recursively re-triggered it: runs = %d, want 1", runs)
	}
}

// #1029: all five trigger kinds (file_changed, review_kept, file_added, before_export, daily) work end to end.
func TestAutomationsAllFiveTriggers(t *testing.T) {
	dir := writeAutomationsProjectFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	var timeMu sync.Mutex
	curTime := time.Date(2026, 11, 26, 1, 55, 0, 0, time.UTC)
	ed.Now = func() time.Time {
		timeMu.Lock()
		defer timeMu.Unlock()
		return curTime
	}
	setTime := func(next time.Time) {
		timeMu.Lock()
		curTime = next
		timeMu.Unlock()
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Enable before-export-mobile and nightly-balance too.
	if err := ed.SetAutomationEnabled("before-export-mobile", true); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetAutomationEnabled("nightly-balance", true); err != nil {
		t.Fatal(err)
	}

	var (
		mu      sync.Mutex
		firedID []string
	)
	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		mu.Lock()
		firedID = append(firedID, req.AutomationID)
		mu.Unlock()
		emit(editor.AgentEvent{Kind: editor.AgentEventDone, Text: "ok"})
		return nil
	})

	// 1. file_added (new prefab detected by PollDiskChanges).
	prefabDir := filepath.Join(dir, "prefabs")
	if err := os.MkdirAll(prefabDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefabDir, "goblin.prefab.yaml"), []byte("version: 1\nname: Goblin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed.PollDiskChanges()

	// 2. review_kept.
	ed.BeginReview()
	ed.KeepReview()

	// 3. before_export.
	ed.TriggerBeforeExportAutomations("ios")

	// 4. daily: 01:55 is before 02:00 -> must NOT fire yet.
	if n := ed.StepAutomations(); n != 0 {
		t.Fatalf("StepAutomations at 01:55 = %d, want 0", n)
	}
	// Advance to 02:00 -> fires once.
	setTime(time.Date(2026, 11, 26, 2, 0, 0, 0, time.UTC))
	if n := ed.StepAutomations(); n != 1 {
		t.Fatalf("StepAutomations at 02:00 = %d, want 1", n)
	}
	// Calling again same day at 02:10 -> must NOT fire a second time.
	setTime(time.Date(2026, 11, 26, 2, 10, 0, 0, time.UTC))
	if n := ed.StepAutomations(); n != 0 {
		t.Fatalf("StepAutomations repeat same day = %d, want 0", n)
	}
	// Advance to next day 02:00 -> fires again.
	setTime(time.Date(2026, 11, 27, 2, 0, 0, 0, time.UTC))
	if n := ed.StepAutomations(); n != 1 {
		t.Fatalf("StepAutomations next day 02:00 = %d, want 1", n)
	}

	if !ed.WaitAutomations(3 * time.Second) {
		t.Fatal("automations did not finish")
	}

	mu.Lock()
	defer mu.Unlock()
	counts := map[string]int{}
	for _, id := range firedID {
		counts[id]++
	}
	if counts["new-prefab-example"] != 1 {
		t.Errorf("new-prefab-example fired %d times, want 1", counts["new-prefab-example"])
	}
	if counts["review-brief"] != 1 {
		t.Errorf("review-brief fired %d times, want 1", counts["review-brief"])
	}
	if counts["before-export-mobile"] != 1 {
		t.Errorf("before-export-mobile fired %d times, want 1", counts["before-export-mobile"])
	}
	if counts["nightly-balance"] != 2 {
		t.Errorf("nightly-balance fired %d times, want 2 (day 1 + day 2)", counts["nightly-balance"])
	}
}

// #1029 Acceptance 2: "Pause all automations for today" stops every trigger until the next day.
func TestAutomationsPauseAllForTodayUntilNextDay(t *testing.T) {
	dir := writeAutomationsProjectFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	var timeMu sync.Mutex
	curTime := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time {
		timeMu.Lock()
		defer timeMu.Unlock()
		return curTime
	}
	setTime := func(next time.Time) {
		timeMu.Lock()
		curTime = next
		timeMu.Unlock()
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetAutomationEnabled("before-export-mobile", true); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetAutomationEnabled("nightly-balance", true); err != nil {
		t.Fatal(err)
	}

	var (
		mu    sync.Mutex
		fired int
	)
	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		mu.Lock()
		fired++
		mu.Unlock()
		emit(editor.AgentEvent{Kind: editor.AgentEventDone, Text: "ok"})
		return nil
	})

	if err := ed.PauseAutomationsForToday(); err != nil {
		t.Fatalf("PauseAutomationsForToday: %v", err)
	}
	if !ed.AutomationsPausedToday() {
		t.Fatal("expected AutomationsPausedToday() == true")
	}

	// Attempt every trigger on the same day -> none may start.
	if err := ed.SetSceneName("Paused Edit"); err != nil {
		t.Fatal(err)
	}
	if err := ed.Save(); err != nil {
		t.Fatal(err)
	}
	ed.BeginReview()
	ed.KeepReview()
	ed.NotifyAutomationFileAdded("prefabs/paused.prefab.yaml")
	ed.TriggerBeforeExportAutomations("ios")
	setTime(time.Date(2026, 11, 26, 23, 59, 0, 0, time.UTC))
	ed.StepAutomations()
	ed.WaitAutomations(2 * time.Second)

	mu.Lock()
	if fired != 0 {
		mu.Unlock()
		t.Fatalf("fired = %d while paused for today, want 0", fired)
	}
	mu.Unlock()

	// Next calendar day: pause automatically lifts and triggers resume.
	setTime(time.Date(2026, 11, 27, 2, 0, 0, 0, time.UTC))
	if ed.AutomationsPausedToday() {
		t.Fatal("expected AutomationsPausedToday() == false on next calendar day")
	}
	if n := ed.StepAutomations(); n != 1 {
		t.Fatalf("StepAutomations next day = %d, want 1", n)
	}
	if err := ed.SetSceneName("Next Day Edit"); err != nil {
		t.Fatal(err)
	}
	if err := ed.Save(); err != nil {
		t.Fatal(err)
	}
	if !ed.WaitAutomations(3 * time.Second) {
		t.Fatal("next-day automations did not finish")
	}

	mu.Lock()
	defer mu.Unlock()
	if fired != 2 {
		t.Fatalf("fired on next day = %d, want 2 (daily + level save)", fired)
	}
}

func TestAutomationsSettingsUIAndDraftInWordsAndAgentReview(t *testing.T) {
	dir := writeAutomationsProjectFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 26, 14, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "All 5 playtests passed."})
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	// Open Settings › Automations from sidebar navigation.
	app.Post(func() { ed.OpenSettingsPage(editor.SettingsPageProject) })
	app.Settle()
	app.Press("id:settings-nav-automations")
	app.Settle()

	if ed.SettingsPage() != editor.SettingsPageAutomations {
		t.Fatalf("SettingsPage = %q, want automations", ed.SettingsPage())
	}
	if !app.Exists("id:automations-settings-panel") || !app.Exists("id:settings-agent-card") {
		t.Fatal("missing automations settings panel or Agent on this page card")
	}
	if got := app.Find("id:settings-page-title").Text; got != "Automations" {
		t.Fatalf("page title = %q, want Automations", got)
	}

	// Each automation row exposes a role=switch toggle and weekly run count.
	if !app.Exists("role:switch Before export") {
		t.Fatal("expected automation switch with role=switch and label Before export")
	}
	app.Press("id:automation-switch-before-export-mobile")
	app.Settle()
	if ed.EnabledAutomationsCount() != 4 {
		t.Fatalf("EnabledAutomationsCount = %d, want 4 after toggling switch", ed.EnabledAutomationsCount())
	}
	// Toggling persisted canonical automations.yaml to disk.
	raw, err := os.ReadFile(filepath.Join(dir, editor.AutomationsFileName))
	if err != nil {
		t.Fatalf("expected automations.yaml written to disk: %v", err)
	}
	if _, err := editor.ParseAutomations(raw); err != nil {
		t.Fatalf("written automations.yaml failed strict parse: %v", err)
	}

	// "+ New automation, in words": agent drafts entry, human keeps it.
	app.Press("id:automations-add-btn")
	app.Settle()
	app.Type("id:automations-draft-input", "Before iOS export, check frame time on a phone profile")
	app.Press("id:automations-draft-submit")
	app.Settle()
	if !app.Exists("id:automations-draft-card") {
		t.Fatal("expected drafted automation review card")
	}
	draft := ed.PendingAutomationDraft()
	if draft == nil || draft.Rule.Trigger.Event != editor.AutomationTriggerBeforeExport || draft.Rule.Trigger.Platform != "ios" {
		t.Fatalf("unexpected drafted rule: %+v", draft)
	}
	if n := len(ed.Automations().Automations); n != 5 {
		t.Fatalf("automation added before Keep: len = %d, want 5", n)
	}
	app.Press("id:automations-draft-keep")
	app.Settle()
	if n := len(ed.Automations().Automations); n != 6 {
		t.Fatalf("automation not added after Keep: len = %d, want 6", n)
	}

	// Pause all automations for today & resume via UI.
	app.Press("id:automations-pause-btn")
	app.Settle()
	if !ed.AutomationsPausedToday() || !app.Exists("id:automations-paused-banner") {
		t.Fatal("Pause button did not pause automations and show banner")
	}
	app.Press("id:automations-banner-resume")
	app.Settle()
	if ed.AutomationsPausedToday() || app.Exists("id:automations-paused-banner") {
		t.Fatal("Resume button did not clear paused state")
	}

	// Spec Light live repaint check.
	light := editor.LightSpecPalette()
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	})
	if got := app.Find("id:automation-row-level-check").Style.Background; got != light.Raised {
		t.Fatalf("light automation row bg = %#x, want %#x", uint32(got), uint32(light.Raised))
	}

	// Close Settings, save level, and verify Ready to review card in Agent panel.
	app.Post(func() {
		ed.CloseSettings()
		_ = ed.SetSceneName("Level 01 Reviewed")
		_ = ed.Save()
	})
	app.Settle()
	if !ed.WaitAutomations(3 * time.Second) {
		t.Fatal("level save automation did not finish")
	}
	app.Post(func() { ed.SetRightRailTab(editor.RightTabAgent) })
	app.Settle()
	if !app.Exists("id:agent-ready-to-review-banner") {
		t.Fatal("expected agent-ready-to-review-banner in Agent panel")
	}
	if !app.Exists("id:agent-turn-keep-0") || !app.Exists("id:agent-turn-dismiss-0") {
		t.Fatal("expected Keep and Dismiss buttons on Ready-to-review automation turn")
	}
	app.Press("id:agent-turn-dismiss-0")
	app.Settle()
	if ready := ed.ReadyToReviewTurns(); len(ready) != 0 {
		t.Fatalf("ReadyToReviewTurns after Dismiss = %d (%+v), want 0", len(ready), ready)
	}
}

func TestAutomationsHardeningYouOnlyPermissionsTasksAndHistoryPreview(t *testing.T) {
	dir := writeAutomationsProjectFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 26, 11, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// 1. automations.yaml and .nsgo/automations-state.yaml are protected as You-only targets.
	for _, p := range []string{"automations.yaml", "./automations.yaml", ".nsgo/automations-state.yaml"} {
		if dom, ok := editor.MatchYouOnlyPath(p); !ok || dom != editor.YouOnlyAutomations {
			t.Fatalf("MatchYouOnlyPath(%q) = (%q, %v), want (%q, true)", p, dom, ok, editor.YouOnlyAutomations)
		}
		if dom, ok := ed.CheckYouOnlyTarget(p); !ok || dom != editor.YouOnlyAutomations {
			t.Fatalf("CheckYouOnlyTarget(%q) = (%q, %v), want (%q, true)", p, dom, ok, editor.YouOnlyAutomations)
		}
	}

	// 2. CheckAutomationActionPermission respects permissions.yaml profiles AND always denies You-only paths.
	if err := ed.SelectPermissionProfile(editor.PermissionProfileCautious); err != nil {
		t.Fatal(err)
	}
	if got := ed.CheckAutomationActionPermission("Edit", "scenes/level_01.scene.yaml"); got != editor.AgentPermissionAsk {
		t.Fatalf("Cautious edit scenes = %q, want ask", got)
	}
	if err := ed.SelectPermissionProfile(editor.PermissionProfileAutonomous); err != nil {
		t.Fatal(err)
	}
	if got := ed.CheckAutomationActionPermission("Edit", "scenes/level_01.scene.yaml"); got != editor.AgentPermissionAllow {
		t.Fatalf("Autonomous edit scenes = %q, want allow", got)
	}
	if got := ed.CheckAutomationActionPermission("Edit", "automations.yaml"); got != editor.AgentPermissionDeny {
		t.Fatalf("Autonomous edit automations.yaml = %q, want deny (You-only)", got)
	}
	if got := ed.CheckAutomationActionPermission("Edit", "permissions.yaml"); got != editor.AgentPermissionDeny {
		t.Fatalf("Autonomous edit permissions.yaml = %q, want deny (You-only)", got)
	}
	if got := ed.CheckAutomationActionPermission("Run", "shell"); got != editor.AgentPermissionDeny {
		t.Fatalf("Autonomous run shell = %q, want deny", got)
	}

	// 3. Automations running under WithAgentActor can NEVER tick or apply an open You-only PermissionsDraft.
	if _, err := ed.SubmitPermissionsDraft(editor.PermissionsDraftRequest{
		Request: "allow shell",
		Rules: []editor.PermissionsDraftRuleSpec{
			{Action: editor.PermissionActionRun, Patterns: []string{"shell"}, Decision: editor.PermissionAllow},
		},
	}); err != nil {
		t.Fatal(err)
	}

	var (
		mu          sync.Mutex
		applyErr    error
		tickedAfter int
	)
	ed.AgentRunner = editor.AgentRunnerFunc(func(ctx context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		ed.SetPermissionsDraftRuleTicked(0, true)
		_, aErr := ed.ApplyPermissionsDraft()
		mu.Lock()
		applyErr = aErr
		tickedAfter = ed.PermissionsDraftTickedCount()
		mu.Unlock()
		emit(editor.AgentEvent{Kind: editor.AgentEventDone, Text: "checked level"})
		return nil
	})

	if err := ed.SetSceneName("Level 01 Check"); err != nil {
		t.Fatal(err)
	}
	if err := ed.Save(); err != nil {
		t.Fatal(err)
	}
	if !ed.WaitAutomations(3 * time.Second) {
		t.Fatal("automation did not finish")
	}

	mu.Lock()
	gotApplyErr := applyErr
	gotTicked := tickedAfter
	mu.Unlock()
	if !errors.Is(gotApplyErr, editor.ErrAutomationYouOnly) {
		t.Fatalf("ApplyPermissionsDraft inside automation = %v, want ErrAutomationYouOnly", gotApplyErr)
	}
	if gotTicked != 0 || !ed.PermissionsDraftOpen() {
		t.Fatalf("automation mutated permissions draft: ticked=%d open=%v", gotTicked, ed.PermissionsDraftOpen())
	}
	ed.DiscardPermissionsDraft()

	// 4. Automation task is mirrored in shared Tasks list (TaskSourceAutomation -> TaskReadyToReview).
	readyTurns := ed.ReadyToReviewTurns()
	if len(readyTurns) != 1 {
		t.Fatalf("ReadyToReviewTurns = %d, want 1", len(readyTurns))
	}
	task, ok := ed.Task(readyTurns[0].ID)
	if !ok || task.Source != editor.TaskSourceAutomation || task.State != editor.TaskReadyToReview {
		t.Fatalf("mirrored task = (%+v, %v), want source=automation state=ready_to_review", task, ok)
	}
	// Agent actor cannot self-keep the automation task via KeepTask either.
	if err := ed.WithAgentActor(func() error { return ed.KeepTask(task.ID) }); !errors.Is(err, editor.ErrAutomationYouOnly) {
		t.Fatalf("KeepTask(automation) as agent = %v, want ErrAutomationYouOnly", err)
	}

	// 5. Checkpoints exclude You-only files (permissions.yaml, automations.yaml) and refuse HistoryPreviewActive.
	cp, ok := ed.AutomationCheckpointByID(readyTurns[0].CheckpointID)
	if !ok {
		t.Fatal("missing automation checkpoint")
	}
	if _, hasPerm := cp.Files["permissions.yaml"]; hasPerm {
		t.Fatal("checkpoint should not snapshot You-only permissions.yaml")
	}
	if _, hasAuto := cp.Files["automations.yaml"]; hasAuto {
		t.Fatal("checkpoint should not snapshot You-only automations.yaml")
	}
	entries := ed.HistoryEntries()
	if len(entries) > 0 {
		if err := ed.PreviewHistoryEntry(entries[0].ID); err != nil {
			t.Fatalf("PreviewHistoryEntry: %v", err)
		}
		if started := ed.NotifyAutomationFileChanged("scenes/level_01.scene.yaml"); started != 0 {
			t.Fatalf("NotifyAutomationFileChanged during History preview = %d, want 0", started)
		}
		if err := ed.RestoreAutomationCheckpoint(cp.ID); !errors.Is(err, editor.ErrHistoryPreview) {
			t.Fatalf("RestoreAutomationCheckpoint during History preview = %v, want ErrHistoryPreview", err)
		}
		ed.ExitHistoryPreview()
	}

	// 6. Symlink destination escape is refused by RestoreAutomationCheckpoint.
	outside := filepath.Join(t.TempDir(), "escape.yaml")
	if err := os.WriteFile(outside, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	levelPath := filepath.Join(dir, "scenes", "level_01.scene.yaml")
	if err := os.Remove(levelPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, levelPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := ed.RestoreAutomationCheckpoint(cp.ID); err == nil {
		t.Fatal("RestoreAutomationCheckpoint succeeded across symlink escape, want error")
	}
	rawOutside, err := os.ReadFile(outside)
	if err != nil || string(rawOutside) != "untouched" {
		t.Fatalf("outside symlink target was modified: %q, err=%v", string(rawOutside), err)
	}
}

func TestAutomationsDraftInputRemountFocusStability(t *testing.T) {
	dir := writeAutomationsProjectFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	app.Post(func() { ed.OpenSettingsPage(editor.SettingsPageAutomations) })
	app.Settle()
	app.Press("id:automations-add-btn")
	app.Settle()

	// Submit empty input to trigger draft error banner.
	app.Press("id:automations-draft-submit")
	app.Settle()
	if !app.Exists("id:automations-draft-error") {
		t.Fatal("expected automations-draft-error on empty draft submit")
	}

	// Focus input and type multiple characters: clearing the error on the first keystroke
	// must NOT remount automations-draft-input or lose focus.
	app.Type("id:automations-draft-input", "Every night at 03:30, run balance checks")
	app.Settle()
	if app.Exists("id:automations-draft-error") {
		t.Fatal("expected automations-draft-error to clear once user typed")
	}
	if got := ed.AutomationDraftText(); got != "Every night at 03:30, run balance checks" {
		t.Fatalf("AutomationDraftText = %q, want full typed string", got)
	}
}
