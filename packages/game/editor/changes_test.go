package editor_test

import (
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const changesFixtureGameYAML = `version: 1
name: dungeon-run
id: dev.example.dungeonrun
appVersion: 0.1.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`

const changesFixturePickupsHead = `version: 1
table: pickups
rows:
  - id: coin
    sprite: items/coin_spin
    score: 10
    sound: audio/coin.wav
  - id: heart
    heals: 1
`

const changesFixturePickupsNow = `version: 1
table: pickups
rows:
  - id: coin
    sprite: items/coin_spin
    score: 25
    sound: audio/coin.wav
  - id: heart
    heals: 1
`

const changesFixtureSlimeHead = `package scripts

type Patrol struct {
	X, Y, Dir, Speed, From, To, HalfW float64
}

func (p *Patrol) Update(dt float64) {
	p.X += p.Dir * p.Speed * dt
	if p.X < p.From || p.X > p.To {
		p.Dir = -p.Dir
	}
}
`

const changesFixtureSlimeNow = `package scripts

type Patrol struct {
	X, Y, Dir, Speed, From, To, HalfW float64
}

func (p *Patrol) Update(dt float64) {
	next := p.X + p.Dir*p.Speed*dt
	// Turn at the patrol bounds or at a ledge, whichever comes first.
	if next < p.From || next > p.To {
		p.Dir = -p.Dir
		return
	}
	p.X = next
}
`

const changesFixtureSceneHead = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: floor
        x: 0
        y: 300
      - id: plank
        x: 344
        y: 256
      - id: knight
        name: Knight
        x: 400
        y: 222
        z: 10
      - id: door
        name: Door
        prefab: prefabs/door.prefab.yaml
        x: 584
        y: 260
        components:
          trigger:
            kind: exit
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 170
        y: 280
      - id: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 240
        y: 280
        components:
          patrol:
            from: 220
            to: 340
            speed: 24
`

const changesFixtureSceneNow = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: floor
        x: 0
        y: 300
      - id: plank
        x: 344
        y: 256
      - id: knight
        name: Knight
        x: 400
        y: 222
        z: 12
      - id: door
        name: Door
        prefab: prefabs/door.prefab.yaml
        x: 560
        y: 260
        components:
          trigger:
            kind: exit
        properties:
          open_sfx: audio/door_creak.wav
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 170
        y: 280
      - id: slime_02
        prefab: prefabs/slime.prefab.yaml
        x: 240
        y: 280
        components:
          patrol:
            from: 220
            to: 340
            speed: 30
      - id: slime_03
        prefab: prefabs/slime.prefab.yaml
        x: 452
        y: 236
      - id: coin_01
        x: 496
        y: 282
      - id: coin_02
        x: 516
        y: 282
      - id: coin_03
        x: 536
        y: 282
`

func writeChangesFixtureWAV(t *testing.T, dst string) {
	t.Helper()
	const sr = 22050
	n := sr / 4 // 0.25 s tone
	samples := make([]float64, n)
	for i := range n {
		env := 1.0 - float64(i)/float64(n)
		samples[i] = 0.5 * env * math.Sin(2*math.Pi*440*float64(i)/sr)
	}
	wav, err := sound.EncodeWAV(samples, sr, 1)
	if err != nil {
		t.Fatalf("EncodeWAV: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, wav, 0o644); err != nil {
		t.Fatal(err)
	}
}

func initChangesFixtureRepo(t *testing.T) (repoDir, bareRemote string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	bareRemote = filepath.Join(t.TempDir(), "origin.git")
	runTestGit(t, filepath.Dir(bareRemote), "init", "--bare", bareRemote)

	repoDir = filepath.Join(t.TempDir(), "dungeon-run")
	if err := os.MkdirAll(filepath.Join(repoDir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}

	runTestGit(t, repoDir, "init", "-b", "main")
	runTestGit(t, repoDir, "config", "user.name", "you")
	runTestGit(t, repoDir, "config", "user.email", "you@example.com")
	runTestGit(t, repoDir, "remote", "add", "origin", bareRemote)

	mustWriteFile(t, filepath.Join(repoDir, "game.yaml"), changesFixtureGameYAML)
	mustWriteFile(t, filepath.Join(repoDir, "scenes", "level_01.scene.yaml"), changesFixtureSceneHead)
	mustWriteFile(t, filepath.Join(repoDir, "data", "pickups.data.yaml"), changesFixturePickupsHead)
	mustWriteFile(t, filepath.Join(repoDir, "scripts", "slime.go"), changesFixtureSlimeHead)

	runTestGit(t, repoDir, "add", ".")
	runTestGit(t, repoDir, "commit", "-m", "Initial dungeon-run project")
	runTestGit(t, repoDir, "push", "-u", "origin", "main")

	// Create feature branch with upstream tracking.
	runTestGit(t, repoDir, "checkout", "-b", "feat/slime-patrol")
	runTestGit(t, repoDir, "push", "-u", "origin", "feat/slime-patrol")

	// Apply working-tree changes matching VersionControl.dc.html screen 1:
	// 1. Modified scene (added slime_03 + 3 coins; changed Door, Knight, slime_02)
	// 2. Modified data file (coin score 10 -> 25)
	// 3. Modified Go code (scripts/slime.go)
	// 4. Added audio asset (audio/door_creak.wav)
	mustWriteFile(t, filepath.Join(repoDir, "scenes", "level_01.scene.yaml"), changesFixtureSceneNow)
	mustWriteFile(t, filepath.Join(repoDir, "data", "pickups.data.yaml"), changesFixturePickupsNow)
	mustWriteFile(t, filepath.Join(repoDir, "scripts", "slime.go"), changesFixtureSlimeNow)
	writeChangesFixtureWAV(t, filepath.Join(repoDir, "audio", "door_creak.wav"))

	return repoDir, bareRemote
}

func mustWriteFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// #1057 Acceptance 1 & 2:
// - In a fixture git repository, unchecking 1 of 3+ modified/added files and committing leaves that file uncommitted.
// - A changed scene shows its added and changed objects against HEAD.
func TestChangesPanelPartialCommitAndSceneDiffAgainstHEAD(t *testing.T) {
	repoDir, _ := initChangesFixtureRepo(t)

	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(filepath.Join(repoDir, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if got := ed.ChangesCount(); got != 4 {
		t.Fatalf("ChangesCount after OpenProject = %d, want 4", got)
	}

	if err := ed.OpenChangesPanel(); err != nil {
		t.Fatalf("OpenChangesPanel: %v", err)
	}
	if !ed.ChangesOpen() {
		t.Fatal("ChangesOpen() = false after OpenChangesPanel")
	}

	sess := ed.ChangesSession()
	if sess.Branch != "feat/slime-patrol" {
		t.Fatalf("Branch = %q, want feat/slime-patrol", sess.Branch)
	}
	if len(sess.Files) != 4 {
		t.Fatalf("len(Files) = %d, want 4", len(sess.Files))
	}

	// Verify category grouping order: SCENES, DATA, CODE, ASSETS.
	wantCats := []editor.ChangesCategory{
		editor.ChangesCategoryScenes,
		editor.ChangesCategoryData,
		editor.ChangesCategoryCode,
		editor.ChangesCategoryAssets,
	}
	for i, want := range wantCats {
		if sess.Files[i].Category != want {
			t.Fatalf("Files[%d] (%s) Category = %q, want %q", i, sess.Files[i].Path, sess.Files[i].Category, want)
		}
	}

	// Acceptance 2: changed scene shows added and changed objects against HEAD.
	sceneFile := sess.Files[0]
	if sceneFile.Scene == nil {
		t.Fatal("scene file missing SceneComparison")
	}
	if len(sceneFile.Scene.Added) != 4 {
		t.Fatalf("scene added objects = %d, want 4 (slime_03, coin_01..03)", len(sceneFile.Scene.Added))
	}
	if len(sceneFile.Scene.Changed) != 3 {
		t.Fatalf("scene changed objects = %d, want 3 (Knight, Door, slime_02)", len(sceneFile.Scene.Changed))
	}
	if len(sceneFile.Scene.Removed) != 0 {
		t.Fatalf("scene removed objects = %d, want 0", len(sceneFile.Scene.Removed))
	}
	foundDoorSummary := false
	for _, c := range sceneFile.Scene.Changed {
		if c.ID == "door" && strings.Contains(c.Summary, "x 584 → 560") {
			foundDoorSummary = true
		}
	}
	if !foundDoorSummary {
		t.Fatalf("expected Door change summary with 'x 584 → 560', got %+v", sceneFile.Scene.Changed)
	}

	// Verify unified diff on data & code files.
	if sess.Files[1].PlusLines != 1 || sess.Files[1].MinusLines != 1 {
		t.Fatalf("data diff counts = +%d/-%d, want +1/-1", sess.Files[1].PlusLines, sess.Files[1].MinusLines)
	}
	if sess.Files[2].PlusLines == 0 || sess.Files[2].MinusLines == 0 {
		t.Fatalf("code diff counts = +%d/-%d, want non-zero + and -", sess.Files[2].PlusLines, sess.Files[2].MinusLines)
	}

	// Verify audio asset metadata & incoming reference from Door in level_01.
	audioFile := sess.Files[3]
	if audioFile.Audio == nil {
		t.Fatal("audio file missing AudioChangeInfo")
	}
	if audioFile.Audio.Format != "WAV" || audioFile.Audio.SampleRate != 22050 || audioFile.Audio.Duration <= 0 {
		t.Fatalf("unexpected audio metadata: %+v", audioFile.Audio)
	}
	if len(audioFile.Audio.Waveform) != 60 {
		t.Fatalf("waveform bars = %d, want 60", len(audioFile.Audio.Waveform))
	}
	if !strings.Contains(strings.Join(audioFile.Audio.UsedBy, "; "), "Used by Door in level_01 as open_sfx") {
		t.Fatalf("audio UsedBy = %v, want reference from Door in level_01 as open_sfx", audioFile.Audio.UsedBy)
	}

	// Even if an unchecked file was already staged in Git's index before opening Changes,
	// unchecking it must leave it completely uncommitted when committing the checked files!
	runTestGit(t, repoDir, "add", "scripts/slime.go")
	if err := ed.SetChangeIncluded("scripts/slime.go", false); err != nil {
		t.Fatalf("SetChangeIncluded: %v", err)
	}
	inc, total, _, _ := ed.ChangesSession().IncludedSummary()
	if inc != 3 || total != 4 {
		t.Fatalf("IncludedSummary = %d of %d, want 3 of 4", inc, total)
	}

	// Commit only the 3 checked files.
	ed.SetCommitSummary("Add plank guard and coin trail to level_01")
	milestone, err := ed.CommitSelectedChanges()
	if err != nil {
		t.Fatalf("CommitSelectedChanges: %v", err)
	}
	if milestone.SHA == "" {
		t.Fatal("milestone SHA is empty")
	}
	if len(milestone.Files) != 3 {
		t.Fatalf("milestone files = %v, want 3 included files", milestone.Files)
	}

	// Verify HEAD contains the 3 included files and NOT scripts/slime.go.
	committedFiles := runTestGit(t, repoDir, "show", "--name-only", "--format=", "HEAD")
	for _, want := range []string{"scenes/level_01.scene.yaml", "data/pickups.data.yaml", "audio/door_creak.wav"} {
		if !strings.Contains(committedFiles, want) {
			t.Fatalf("HEAD commit missing %q:\n%s", want, committedFiles)
		}
	}
	if strings.Contains(committedFiles, "scripts/slime.go") {
		t.Fatalf("unchecked file scripts/slime.go was committed in HEAD:\n%s", committedFiles)
	}

	// Verify git status --porcelain still shows scripts/slime.go as uncommitted.
	porcelain := runTestGit(t, repoDir, "status", "--porcelain")
	if !strings.Contains(porcelain, "scripts/slime.go") {
		t.Fatalf("git status --porcelain missing unchecked scripts/slime.go:\n%s", porcelain)
	}
	if strings.Contains(porcelain, "level_01.scene.yaml") || strings.Contains(porcelain, "pickups.data.yaml") || strings.Contains(porcelain, "door_creak.wav") {
		t.Fatalf("committed files still dirty in git status:\n%s", porcelain)
	}

	// Verify commit became a milestone in History and ahead count is 1.
	if ms := ed.History.Milestones(); len(ms) != 1 || ms[0].SHA != milestone.SHA {
		t.Fatalf("History.Milestones = %+v, want milestone %s", ms, milestone.SHA)
	}
	if ed.ChangesSession().Ahead != 1 {
		t.Fatalf("Ahead = %d, want 1", ed.ChangesSession().Ahead)
	}

	// Push commits to bare remote and verify ahead resets to 0.
	if err := ed.PushCommits(); err != nil {
		t.Fatalf("PushCommits: %v", err)
	}
	if ed.ChangesSession().Ahead != 0 {
		t.Fatalf("Ahead after PushCommits = %d, want 0", ed.ChangesSession().Ahead)
	}
	if !ed.ChangesSession().LastCommit.Pushed {
		t.Fatal("LastCommit.Pushed = false after PushCommits")
	}
}

func TestChangesPanelChecksRedraftShareBranchAndPull(t *testing.T) {
	repoDir, bareRemote := initChangesFixtureRepo(t)

	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	fixedNow := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return fixedNow }

	var copiedPR string
	ed.ChangesClipboard = func(text string) { copiedPR = text }

	var ghArgs []string
	ed.GHRunner = func(dir string, args ...string) (string, error) {
		ghArgs = append([]string(nil), args...)
		return "https://github.com/nsgo-dev/dungeon-run/pull/12", nil
	}

	ed.ChangesCheckRunner = func(dir string) (editor.ChangesCheckStatus, error) {
		return editor.ChangesCheckStatus{
			Ran:          true,
			RanAt:        fixedNow.Add(-3 * time.Minute),
			CheckPassed:  true,
			CheckSummary: "nsgo check",
			TestPassed:   true,
			TestCount:    42,
			TestSummary:  "nsgo test · 42 passed",
		}, nil
	}

	if err := ed.OpenProject(filepath.Join(repoDir, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.OpenChangesPanel(); err != nil {
		t.Fatalf("OpenChangesPanel: %v", err)
	}

	// Run checks and attach review/playtest/agent context.
	if err := ed.RunChangesChecks(); err != nil {
		t.Fatalf("RunChangesChecks: %v", err)
	}
	ed.SetChangesContext(3, "10 s, 0 falls, 3 of 3 coins reachable", 3)
	if err := ed.RedraftCommitMessage(); err != nil {
		t.Fatalf("RedraftCommitMessage: %v", err)
	}
	sess := ed.ChangesSession()
	if sess.DraftProvenance != "Drafted from 3 reviews and a playtest" {
		t.Fatalf("DraftProvenance = %q", sess.DraftProvenance)
	}
	if sess.DraftAttribution != "Made from 3 agent requests, reviewed and kept" {
		t.Fatalf("DraftAttribution = %q", sess.DraftAttribution)
	}
	if !strings.Contains(sess.DraftDescription, "Playtest: 10 s, 0 falls, 3 of 3 coins reachable") {
		t.Fatalf("DraftDescription missing playtest summary:\n%s", sess.DraftDescription)
	}

	// Custom drafter hook.
	ed.CommitMessageDrafter = func(ctx context.Context, req editor.CommitDraftRequest) (editor.CommitDraft, error) {
		return editor.CommitDraft{
			Summary:     "Add a plank guard and a coin trail to level_01",
			Description: "- slime_03 guards the plank\n- Three coins lead to the door",
			Provenance:  "Drafted from 3 reviews and a playtest",
			Attribution: "Made from 3 agent requests, reviewed and kept",
		}, nil
	}
	if err := ed.RedraftCommitMessage(); err != nil {
		t.Fatalf("RedraftCommitMessage custom: %v", err)
	}
	if ed.ChangesSession().DraftSummary != "Add a plank guard and a coin trail to level_01" {
		t.Fatalf("custom DraftSummary = %q", ed.ChangesSession().DraftSummary)
	}

	// Share: Copy as PR description + Open pull request via GHRunner.
	prDesc, err := ed.CopyPRDescription()
	if err != nil {
		t.Fatalf("CopyPRDescription: %v", err)
	}
	if copiedPR != prDesc || !strings.Contains(prDesc, "nsgo test`: 42 passed") || !strings.Contains(prDesc, "Playtest: 10 s") {
		t.Fatalf("unexpected PR description:\n%s", prDesc)
	}
	if !ed.ChangesGHAvailable() {
		t.Fatal("ChangesGHAvailable() = false with GHRunner injected")
	}
	prURL, err := ed.OpenPullRequest()
	if err != nil {
		t.Fatalf("OpenPullRequest: %v", err)
	}
	if prURL != "https://github.com/nsgo-dev/dungeon-run/pull/12" {
		t.Fatalf("prURL = %q", prURL)
	}
	if len(ghArgs) < 4 || ghArgs[0] != "pr" || ghArgs[1] != "create" {
		t.Fatalf("unexpected gh args: %v", ghArgs)
	}

	// Audio audition starts and stops cleanly.
	if err := ed.AuditionChangedAudio("audio/door_creak.wav"); err != nil {
		t.Fatalf("AuditionChangedAudio: %v", err)
	}
	if ed.ChangesAuditioningPath() != "audio/door_creak.wav" {
		t.Fatalf("ChangesAuditioningPath = %q", ed.ChangesAuditioningPath())
	}
	ed.StopChangedAudio()
	if ed.ChangesAuditioningPath() != "" {
		t.Fatalf("ChangesAuditioningPath after Stop = %q", ed.ChangesAuditioningPath())
	}

	// Commit everything, switch to main, push a remote commit from another clone, then test GitPull.
	if _, err := ed.CommitAndPushSelectedChanges(); err != nil {
		t.Fatalf("CommitAndPushSelectedChanges: %v", err)
	}
	if err := ed.GitSwitchBranch("main"); err != nil {
		t.Fatalf("GitSwitchBranch(main): %v", err)
	}
	if ed.ChangesSession().Branch != "main" {
		t.Fatalf("Branch after switch = %q, want main", ed.ChangesSession().Branch)
	}

	otherClone := filepath.Join(t.TempDir(), "other")
	runTestGit(t, filepath.Dir(otherClone), "clone", bareRemote, otherClone)
	runTestGit(t, otherClone, "config", "user.name", "mia")
	runTestGit(t, otherClone, "config", "user.email", "mia@example.com")
	mustWriteFile(t, filepath.Join(otherClone, "README.md"), "# dungeon-run\n")
	runTestGit(t, otherClone, "add", "README.md")
	runTestGit(t, otherClone, "commit", "-m", "Add README")
	runTestGit(t, otherClone, "push", "origin", "main")

	if err := ed.GitPull(); err != nil {
		t.Fatalf("GitPull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "README.md")); err != nil {
		t.Fatalf("README.md not present after GitPull: %v", err)
	}

	// Verify Play-mode guard refuses OpenChangesPanel.
	ed.CloseChangesPanel()
	var rootScope *core.Scope
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		rootScope = cx
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))
	if err := ed.PlayIn(rootScope); err != nil {
		t.Fatalf("PlayIn: %v", err)
	}
	if err := ed.OpenChangesPanel(); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("OpenChangesPanel during Play = %v, want ErrPlaying", err)
	}
	if err := ed.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// #1057 Acceptance 3: real nsgotest screenshots on macOS beside VersionControl.dc.html screen 1,
// testing status bar trigger, scene side-by-side preview, code diff + checks, audio card, partial commit, and SHARE.
func TestChangesPanelUIAndScreenshot(t *testing.T) {
	repoDir, _ := initChangesFixtureRepo(t)

	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	fixedNow := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	ed.Now = func() time.Time { return fixedNow }
	ed.ChangesCheckRunner = func(dir string) (editor.ChangesCheckStatus, error) {
		return editor.ChangesCheckStatus{
			Ran:          true,
			RanAt:        fixedNow.Add(-3 * time.Minute),
			CheckPassed:  true,
			CheckSummary: "nsgo check",
			TestPassed:   true,
			TestCount:    42,
			TestSummary:  "nsgo test · 42 passed",
		}, nil
	}
	var copied string
	ed.ChangesClipboard = func(text string) { copied = text }
	ed.GHRunner = func(dir string, args ...string) (string, error) {
		return "https://github.com/nsgo-dev/dungeon-run/pull/1", nil
	}

	if err := ed.OpenProject(filepath.Join(repoDir, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// Status bar shows Changes: 4 without touching the 2D toolbar.
	if !app.Exists("id:status-changes") {
		t.Fatal("status-changes entry missing from editor status bar")
	}
	if node := app.Find("id:status-changes"); !strings.Contains(node.Props.Label, "4") {
		t.Fatalf("status-changes label=%q, want count 4", node.Props.Label)
	}

	// Press status bar Changes button -> opens full Changes & Commit screen.
	app.Press("id:status-changes")
	if !app.Exists("id:changes-panel") {
		t.Fatal("ChangesPanelView did not render after pressing status-changes")
	}

	app.Post(func() {
		ed.SetChangesContext(3, "10 s, 0 falls, 3 of 3 coins reachable", 3)
		_ = ed.RunChangesChecks()
		_ = ed.RedraftCommitMessage()
	})
	app.Settle()

	// Verify scene comparison renders HEAD and Now side-by-side canvases + added/changed legend.
	if !app.Exists("id:changes-canvas-head") || !app.Exists("id:changes-canvas-now") {
		t.Fatal("missing side-by-side HEAD and Now scene preview canvases")
	}
	if got := app.Find("id:changes-legend-added").Text; !strings.Contains(got, "slime_03") {
		t.Fatalf("added legend = %q, want slime_03", got)
	}
	if got := app.Find("id:changes-legend-changed").Text; !strings.Contains(got, "Door") {
		t.Fatalf("changed legend = %q, want Door", got)
	}
	app.Screenshot("changes-panel-scene")

	// Select Go code file (index 2: scripts/slime.go) -> shows checks bar + unified diff.
	app.Press("id:changes-pick-2")
	if !app.Exists("id:changes-text-diff") || !app.Exists("id:changes-checks-bar") {
		t.Fatal("code diff or checks bar did not render for scripts/slime.go")
	}
	if got := app.Find("id:changes-test-status").Text; !strings.Contains(got, "42 passed") {
		t.Fatalf("test status = %q, want 42 passed", got)
	}
	app.Screenshot("changes-panel-code")

	// Select audio file (index 3: audio/door_creak.wav) -> shows audio audition card + refs.
	app.Press("id:changes-pick-3")
	if !app.Exists("id:changes-audio-card") || !app.Exists("id:changes-audio-waveform") {
		t.Fatal("audio audition card did not render for audio/door_creak.wav")
	}
	if got := app.Find("id:changes-audio-refs").Text; !strings.Contains(got, "Used by Door in level_01") {
		t.Fatalf("audio refs = %q, want Used by Door in level_01", got)
	}
	app.Press("id:changes-audio-play")
	if ed.ChangesAuditioningPath() != "audio/door_creak.wav" {
		t.Fatalf("audio audition did not start via UI button")
	}
	app.Screenshot("changes-panel-audio")
	app.Press("id:changes-audio-play")

	// Uncheck scripts/slime.go (index 2) and press Commit.
	app.Press("id:changes-check-2")
	app.Press("id:changes-commit")
	if !app.Exists("id:changes-committed-card") {
		t.Fatal("committed confirmation card did not appear after Commit")
	}
	if !app.Exists("id:changes-push") || !app.Exists("id:changes-share-section") || !app.Exists("id:changes-history-list") {
		t.Fatal("expected Push, SHARE, and HISTORY sections after commit")
	}
	app.Press("id:changes-copy-pr")
	if copied == "" {
		t.Fatal("Copy as PR description button did not populate clipboard")
	}
	app.Screenshot("changes-panel-committed")

	// Verify unchecked file stayed uncommitted on disk.
	porcelain := runTestGit(t, repoDir, "status", "--porcelain")
	if !strings.Contains(porcelain, "scripts/slime.go") {
		t.Fatalf("scripts/slime.go was unexpectedly committed:\n%s", porcelain)
	}
}
