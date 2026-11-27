package editor_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const mergeFixtureBaseScene = `version: 3
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
      - id: torch_02
        x: 120
        y: 264
        properties:
          light: 48
`

const mergeFixtureMineScene = `version: 3
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
          open_sfx: audio/door_creak.ogg
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
      - id: torch_02
        x: 120
        y: 264
        properties:
          light: 48
`

const mergeFixtureTheirsScene = `version: 3
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
        z: 11
      - id: door
        name: Door
        prefab: prefabs/door.prefab.yaml
        x: 600
        y: 260
        components:
          trigger:
            kind: exit
        properties:
          locked_by: lever_01
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 170
        y: 280
      - id: lever_01
        x: 360
        y: 238
      - id: torch_02
        x: 120
        y: 264
        properties:
          light: 64
`

func runTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"LC_ALL=C",
		"GIT_AUTHOR_NAME=you",
		"GIT_AUTHOR_EMAIL=you@example.com",
		"GIT_COMMITTER_NAME=you",
		"GIT_COMMITTER_EMAIL=you@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func runTestGitAs(t *testing.T, dir, author string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"LC_ALL=C",
		"GIT_AUTHOR_NAME="+author,
		"GIT_AUTHOR_EMAIL="+author+"@example.com",
		"GIT_COMMITTER_NAME="+author,
		"GIT_COMMITTER_EMAIL="+author+"@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// setupConflictedGitProject creates a real Git repository containing a valid NSGo project
// (game.yaml + scenes/level_01.scene.yaml) left in an active mid-merge conflict state between
// feat/slime-patrol (ours) and main (theirs) with 3 conflicted objects (door, knight, slime_02)
// and 6 auto-merged objects (slime_03, coin_01..03, lever_01, torch_02).
func setupConflictedGitProject(t *testing.T) (repoDir, mineHeadSHA, theirsHeadSHA string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not installed")
	}

	dir := t.TempDir()
	runTestGit(t, dir, "init", "-b", "main")
	// Configure repo-local identity and disable GPG signing so both `git merge` and
	// `FinishMerge` (`git commit --no-edit`) work on CI runners without global git config.
	runTestGit(t, dir, "config", "user.name", "you")
	runTestGit(t, dir, "config", "user.email", "you@example.com")
	runTestGit(t, dir, "config", "commit.gpgsign", "false")

	gameYAML := "version: 1\nname: dungeon-run\nid: dev.example.dungeonrun\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	sceneFile := filepath.Join(dir, "scenes", "level_01.scene.yaml")
	if err := os.WriteFile(sceneFile, []byte(mergeFixtureBaseScene), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "commit", "-m", "Initial level_01")

	// Create feature branch with our changes.
	runTestGit(t, dir, "checkout", "-b", "feat/slime-patrol")
	if err := os.WriteFile(sceneFile, []byte(mergeFixtureMineScene), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", "scenes/level_01.scene.yaml")
	runTestGitAs(t, dir, "you", "commit", "-m", "Add a plank guard and a coin trail to level_01")
	mineHeadSHA = runTestGit(t, dir, "rev-parse", "HEAD")

	// Add Mia's divergent commit on main.
	runTestGit(t, dir, "checkout", "main")
	if err := os.WriteFile(sceneFile, []byte(mergeFixtureTheirsScene), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", "scenes/level_01.scene.yaml")
	runTestGitAs(t, dir, "mia", "commit", "-m", "Thin out the lower floor for the lever")
	theirsHeadSHA = runTestGit(t, dir, "rev-parse", "HEAD")

	// Switch back to feat/slime-patrol and start merging main (which conflicts).
	runTestGit(t, dir, "checkout", "feat/slime-patrol")
	mergeCmd := exec.Command("git", "merge", "main")
	mergeCmd.Dir = dir
	mergeCmd.Env = append(os.Environ(),
		"LC_ALL=C",
		"GIT_AUTHOR_NAME=you",
		"GIT_AUTHOR_EMAIL=you@example.com",
		"GIT_COMMITTER_NAME=you",
		"GIT_COMMITTER_EMAIL=you@example.com",
	)
	mergeOut, mergeErr := mergeCmd.CombinedOutput()
	if mergeErr == nil {
		t.Fatalf("expected git merge main to conflict, but it succeeded:\n%s", mergeOut)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); err != nil {
		t.Fatalf("git merge main failed without starting a merge (missing MERGE_HEAD): %v\n%s", mergeErr, mergeOut)
	}

	// Write a ConflictDoc YAML into the working tree file, simulating the #1058 merge driver output.
	baseDoc, err := sceneformat.Parse([]byte(mergeFixtureBaseScene))
	if err != nil {
		t.Fatal(err)
	}
	mineDoc, err := sceneformat.Parse([]byte(mergeFixtureMineScene))
	if err != nil {
		t.Fatal(err)
	}
	theirsDoc, err := sceneformat.Parse([]byte(mergeFixtureTheirsScene))
	if err != nil {
		t.Fatal(err)
	}
	res, err := sceneformat.Merge(baseDoc, mineDoc, theirsDoc)
	if err != nil {
		t.Fatal(err)
	}
	conflictBytes, err := sceneformat.EncodeConflict(res.ConflictDoc("scenes/level_01.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sceneFile, conflictBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	return dir, mineHeadSHA, theirsHeadSHA
}

// #1059 acceptance: resolving three conflicts in a real Git fixture merge and finishing it
// leaves a valid scene, runs game check and playtest, and creates a real 2-parent merge commit.
func TestSceneMergeResolveThreeConflictsAndFinishCreatesMergeCommit(t *testing.T) {
	repoDir, mineHeadSHA, theirsHeadSHA := setupConflictedGitProject(t)

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// OpenProject detects the ConflictDoc main scene and opens the Scene Merge Conflict screen automatically.
	if err := ed.OpenProject(repoDir); err != nil {
		t.Fatalf("OpenProject on conflicted repo: %v", err)
	}
	if !ed.MergeConflictOpen() {
		t.Fatalf("expected MergeConflictOpen() == true after opening project with conflicted main scene (status=%q)", ed.Status)
	}

	sess := ed.MergeSession()
	if sess == nil {
		t.Fatal("MergeSession() == nil")
	}
	if sess.Mine.Branch != "feat/slime-patrol" {
		t.Fatalf("Mine.Branch = %q, want feat/slime-patrol", sess.Mine.Branch)
	}
	if sess.Theirs.Branch != "main" {
		t.Fatalf("Theirs.Branch = %q, want main", sess.Theirs.Branch)
	}
	if sess.Theirs.Author != "mia" {
		t.Fatalf("Theirs.Author = %q, want mia", sess.Theirs.Author)
	}
	if len(sess.Conflicts) != 3 {
		t.Fatalf("len(Conflicts) = %d, want 3: %+v", len(sess.Conflicts), sess.Conflicts)
	}
	if len(sess.AutoMerged) < 5 {
		t.Fatalf("len(AutoMerged) = %d, want >= 5: %+v", len(sess.AutoMerged), sess.AutoMerged)
	}

	// Verify FinishMerge refuses while conflicts remain unresolved.
	if err := ed.FinishMerge(); err == nil {
		t.Fatal("expected FinishMerge to fail while conflicts remain unresolved")
	}

	// Verify agent suggestions: door combines fields from both, slime_02 stays deleted, knight z=12.
	byID := make(map[string]editor.MergeConflictItem)
	for _, c := range sess.Conflicts {
		byID[c.ID] = c
	}
	if door := byID["door"]; door.Agent.Meta != "fields from both" || door.Agent.Object == nil || door.Agent.Object.X != 600 {
		t.Fatalf("unexpected door agent suggestion: meta=%q obj=%+v", door.Agent.Meta, door.Agent.Object)
	}
	if slime := byID["slime_02"]; !slime.Agent.Deleted || slime.Agent.Meta != "same as theirs" {
		t.Fatalf("unexpected slime_02 agent suggestion: deleted=%v meta=%q", slime.Agent.Deleted, slime.Agent.Meta)
	}
	if knight := byID["knight"]; knight.Agent.Object == nil || knight.Agent.Object.Z != 12 {
		t.Fatalf("unexpected knight agent suggestion: obj=%+v", knight.Agent.Object)
	}

	// Resolve all three conflicts (door -> Agent, slime_02 -> Theirs, knight -> Mine).
	if err := ed.ResolveMergeConflict("door", editor.MergeChoiceAgent); err != nil {
		t.Fatalf("ResolveMergeConflict(door): %v", err)
	}
	if err := ed.ResolveMergeConflict("slime_02", editor.MergeChoiceTheirs); err != nil {
		t.Fatalf("ResolveMergeConflict(slime_02): %v", err)
	}
	if err := ed.ResolveMergeConflict("knight", editor.MergeChoiceMine); err != nil {
		t.Fatalf("ResolveMergeConflict(knight): %v", err)
	}
	if !sess.AllResolved() {
		t.Fatalf("AllResolved() = false after resolving all 3 (remaining=%d)", sess.RemainingCount())
	}

	var checkRan, playtestRan bool
	ed.MergeCheckRunner = func(dir, scenePath string, doc *sceneformat.SceneDoc) ([]assetcheck.Problem, error) {
		checkRan = true
		if err := sceneformat.Validate(doc); err != nil {
			return nil, err
		}
		return nil, nil
	}
	ed.MergePlaytestRunner = func(dir, scenePath string, doc *sceneformat.SceneDoc) (editor.MergePlaytestResult, error) {
		playtestRan = true
		return editor.MergePlaytestResult{
			Duration: 10 * time.Second,
			Steps:    600,
			Summary:  "Ran game check and a 10 s playtest on the result: no problems, 3 of 3 coins reachable.",
		}, nil
	}

	if err := ed.FinishMerge(); err != nil {
		t.Fatalf("FinishMerge: %v", err)
	}
	if !checkRan || !playtestRan {
		t.Fatalf("expected both game check (%v) and playtest (%v) to run", checkRan, playtestRan)
	}
	if !sess.Finished || sess.CommitSHA == "" {
		t.Fatalf("expected Finished=true and CommitSHA set, got finished=%v sha=%q", sess.Finished, sess.CommitSHA)
	}

	// Read the written scene file from disk and confirm it parses, validates, and contains every merged object.
	raw, err := os.ReadFile(filepath.Join(repoDir, "scenes", "level_01.scene.yaml"))
	if err != nil {
		t.Fatalf("read resolved scene: %v", err)
	}
	resolved, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("resolved scene failed Parse: %v\n%s", err, raw)
	}
	objs := make(map[string]sceneformat.ObjectDoc)
	for _, l := range resolved.Layers {
		for _, o := range l.Objects {
			objs[o.ID] = o
		}
	}
	door, ok := objs["door"]
	if !ok || door.X != 600 || door.Properties["locked_by"] != "lever_01" || door.Properties["open_sfx"] != "audio/door_creak.ogg" {
		t.Fatalf("resolved door = %+v, want x=600, locked_by=lever_01, open_sfx=audio/door_creak.ogg", door)
	}
	if _, exists := objs["slime_02"]; exists {
		t.Fatal("slime_02 should be deleted in resolved scene")
	}
	if k := objs["knight"]; k.Z != 12 {
		t.Fatalf("resolved knight z = %d, want 12", k.Z)
	}
	for _, autoID := range []string{"slime_03", "coin_01", "coin_02", "coin_03", "lever_01", "torch_02"} {
		if _, exists := objs[autoID]; !exists {
			t.Fatalf("auto-merged object %q missing from resolved scene", autoID)
		}
	}

	// Verify Git HEAD is a real 2-parent merge commit whose parents are mineHeadSHA and theirsHeadSHA.
	parentsLine := runTestGit(t, repoDir, "rev-list", "--parents", "-n", "1", "HEAD")
	parents := strings.Fields(parentsLine)
	if len(parents) != 3 {
		t.Fatalf("expected 2-parent merge commit (3 SHAs), got %v", parents)
	}
	if parents[1] != mineHeadSHA || parents[2] != theirsHeadSHA {
		t.Fatalf("merge parents = %v, want [%s %s]", parents[1:], mineHeadSHA, theirsHeadSHA)
	}
	if !strings.HasPrefix(parents[0], sess.CommitSHA) {
		t.Fatalf("HEAD %s does not match reported CommitSHA %q", parents[0], sess.CommitSHA)
	}
	status := runTestGit(t, repoDir, "status", "--porcelain")
	if status != "" {
		t.Fatalf("expected clean working tree after FinishMerge, got:\n%s", status)
	}
}

// #1059 acceptance: Abort merge restores the branch and working tree exactly as they were.
func TestSceneMergeAbortRestoresBranch(t *testing.T) {
	repoDir, mineHeadSHA, _ := setupConflictedGitProject(t)

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(repoDir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if !ed.MergeConflictOpen() {
		t.Fatal("expected MergeConflictOpen() == true")
	}

	if err := ed.AbortMerge(); err != nil {
		t.Fatalf("AbortMerge: %v", err)
	}
	if ed.MergeConflictOpen() {
		t.Fatal("MergeConflictOpen() still true after AbortMerge")
	}

	// Verify HEAD is back at mineHeadSHA, MERGE_HEAD is gone, and level_01 matches Mine's original YAML.
	if head := runTestGit(t, repoDir, "rev-parse", "HEAD"); head != mineHeadSHA {
		t.Fatalf("HEAD after abort = %s, want %s", head, mineHeadSHA)
	}
	if branch := runTestGit(t, repoDir, "rev-parse", "--abbrev-ref", "HEAD"); branch != "feat/slime-patrol" {
		t.Fatalf("branch after abort = %q, want feat/slime-patrol", branch)
	}
	if status := runTestGit(t, repoDir, "status", "--porcelain"); status != "" {
		t.Fatalf("dirty working tree after abort:\n%s", status)
	}
	restored, err := os.ReadFile(filepath.Join(repoDir, "scenes", "level_01.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != mergeFixtureMineScene {
		t.Fatalf("scene after abort does not match original branch content")
	}
}

func TestSceneMergeCustomYAMLAndVerificationGuards(t *testing.T) {
	repoDir, mineHeadSHA, _ := setupConflictedGitProject(t)

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(repoDir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	// Invalid YAML (unknown field) is rejected cleanly without mutating the choice.
	if err := ed.ApplyMergeCustomYAML("door", "id: door\nbogusField: 123\n"); err == nil {
		t.Fatal("expected unknown field in custom YAML to fail")
	}
	// Mismatched object ID is rejected.
	if err := ed.ApplyMergeCustomYAML("door", "id: other_door\nx: 580\n"); err == nil {
		t.Fatal("expected mismatched id in custom YAML to fail")
	}

	// Valid hand-edited YAML sets custom resolution and updates ResultDoc.
	customDoor := "- id: door\n  name: Door\n  prefab: prefabs/door.prefab.yaml\n  x: 590\n  y: 260\n  components:\n    trigger:\n      kind: exit\n  properties:\n    locked_by: lever_01\n    open_sfx: audio/door_creak.ogg\n"
	if err := ed.ApplyMergeCustomYAML("door", customDoor); err != nil {
		t.Fatalf("ApplyMergeCustomYAML valid door: %v", err)
	}
	doc, err := ed.MergeSession().ResultDoc()
	if err != nil {
		t.Fatalf("ResultDoc: %v", err)
	}
	var foundDoor *sceneformat.ObjectDoc
	for i := range doc.Layers[0].Objects {
		if doc.Layers[0].Objects[i].ID == "door" {
			foundDoor = &doc.Layers[0].Objects[i]
		}
	}
	if foundDoor == nil || foundDoor.X != 590 {
		t.Fatalf("custom door X = %+v, want 590", foundDoor)
	}

	// Resolve remaining two conflicts.
	if err := ed.ResolveMergeConflict("slime_02", editor.MergeChoiceAgent); err != nil {
		t.Fatal(err)
	}
	if err := ed.ResolveMergeConflict("knight", editor.MergeChoiceAgent); err != nil {
		t.Fatal(err)
	}

	// Failing game check prevents commit and records VerifyErr.
	ed.MergeCheckRunner = func(dir, scenePath string, doc *sceneformat.SceneDoc) ([]assetcheck.Problem, error) {
		return []assetcheck.Problem{{File: scenePath, Line: 12, Message: "missing prefab"}}, nil
	}
	if err := ed.FinishMerge(); err == nil {
		t.Fatal("expected FinishMerge to fail when game check reports problems")
	}
	if ed.MergeSession().Finished || ed.MergeSession().VerifyErr == "" {
		t.Fatalf("expected Finished=false and VerifyErr set after check failure")
	}
	if head := runTestGit(t, repoDir, "rev-parse", "HEAD"); head != mineHeadSHA {
		t.Fatalf("HEAD moved despite failed game check: %s", head)
	}

	// Passing check + failing playtest prevents commit too.
	ed.MergeCheckRunner = func(dir, scenePath string, doc *sceneformat.SceneDoc) ([]assetcheck.Problem, error) {
		return nil, nil
	}
	ed.MergePlaytestRunner = func(dir, scenePath string, doc *sceneformat.SceneDoc) (editor.MergePlaytestResult, error) {
		return editor.MergePlaytestResult{}, errors.New("coin_03 unreachable")
	}
	if err := ed.FinishMerge(); err == nil {
		t.Fatal("expected FinishMerge to fail when playtest fails")
	}
	if ed.MergeSession().Finished || !strings.Contains(ed.MergeSession().VerifyErr, "coin_03 unreachable") {
		t.Fatalf("unexpected VerifyErr after playtest failure: %q", ed.MergeSession().VerifyErr)
	}

	// Default built-in playtest runs cleanly when MergePlaytestRunner is nil.
	ed.MergePlaytestRunner = nil
	if err := ed.FinishMerge(); err != nil {
		t.Fatalf("FinishMerge with default built-in playtest: %v", err)
	}
	if !ed.MergeSession().Finished || ed.MergeSession().CommitSHA == "" {
		t.Fatalf("expected Finished=true and CommitSHA after default playtest")
	}
}

// #1059 acceptance: interactive UI flow + real macOS nsgotest screenshots beside VersionControl.dc.html.
func TestSceneMergeConflictUIAndScreenshot(t *testing.T) {
	baseDoc, err := sceneformat.Parse([]byte(mergeFixtureBaseScene))
	if err != nil {
		t.Fatal(err)
	}
	mineDoc, err := sceneformat.Parse([]byte(mergeFixtureMineScene))
	if err != nil {
		t.Fatal(err)
	}
	theirsDoc, err := sceneformat.Parse([]byte(mergeFixtureTheirsScene))
	if err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(mineDoc.Clone(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.MergeCheckRunner = func(dir, scenePath string, doc *sceneformat.SceneDoc) ([]assetcheck.Problem, error) {
		return nil, nil
	}
	ed.MergePlaytestRunner = func(dir, scenePath string, doc *sceneformat.SceneDoc) (editor.MergePlaytestResult, error) {
		return editor.MergePlaytestResult{
			Duration: 10 * time.Second,
			Steps:    600,
			Summary:  "The agent ran game check and a 10 s playtest on the result: no problems, 3 of 3 coins reachable.",
		}, nil
	}

	if err := ed.OpenMergeConflictSession(editor.MergeConflictSessionConfig{
		ScenePath:   "scenes/level_01.scene.yaml",
		ProjectName: "dungeon-run",
		SceneName:   "level_01",
		Mine:        editor.MergeSideMeta{Branch: "feat/slime-patrol", Author: "you", Commit: "9f3c2d1"},
		Theirs:      editor.MergeSideMeta{Branch: "origin/main", Author: "mia", Commit: "7c21f0e", Message: "Thin out the lower floor for the lever"},
		BaseDoc:     baseDoc,
		MineDoc:     mineDoc,
		TheirsDoc:   theirsDoc,
	}); err != nil {
		t.Fatalf("OpenMergeConflictSession: %v", err)
	}

	// Pre-resolve slime_02 as Theirs to match the exact initial state of VersionControl.dc.html screen 2!
	if err := ed.ResolveMergeConflict("slime_02", editor.MergeChoiceTheirs); err != nil {
		t.Fatal(err)
	}
	if err := ed.FocusMergeConflict("door"); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	if !app.Exists("id:scene-merge-conflict") {
		t.Fatal("Scene merge conflict view did not render")
	}
	if !app.Exists("id:merge-canvas-mine") || !app.Exists("id:merge-canvas-theirs") || !app.Exists("id:merge-canvas-result") {
		t.Fatal("missing MINE / THEIRS / RESULT preview canvases")
	}
	if got := app.Find("id:merge-progress-text").Text; !strings.Contains(got, "2 of 3 objects need you") {
		t.Fatalf("header progress text = %q, want '2 of 3 objects need you'", got)
	}
	if got := app.Find("id:merge-focus-name").Text; got != "Door" {
		t.Fatalf("focused object name = %q, want Door", got)
	}
	if app.Enabled("id:merge-finish") {
		t.Fatal("Finish merge button should be disabled while 2 objects remain unresolved")
	}

	// Capture macOS screenshot of the unresolved state (matching VersionControl.dc.html screen 2).
	app.Screenshot("merge-conflict-unresolved")

	// Click MINE card -> Door picks Mine.
	app.Press("id:merge-card-mine")
	if got := app.Find("id:merge-chip-door").Text; got != "Mine" {
		t.Fatalf("door chip after pressing MINE card = %q, want Mine", got)
	}

	// Click "Edit the result YAML by hand" -> inline editor opens and cancels cleanly.
	app.Press("id:merge-edit-yaml")
	if !app.Exists("id:merge-custom-yaml-input") {
		t.Fatal("custom YAML editor did not open")
	}
	app.Press("id:merge-custom-yaml-cancel")
	if app.Exists("id:merge-custom-yaml-input") {
		t.Fatal("custom YAML editor did not close on cancel")
	}

	// Click "Take the agent's suggestion for all 3" -> all 3 resolved, Finish merge enabled.
	app.Press("id:merge-take-all-agent")
	if !app.Enabled("id:merge-finish") {
		t.Fatal("Finish merge button should be enabled after resolving all 3")
	}
	if got := app.Find("id:merge-progress-text").Text; !strings.Contains(got, "ready to finish") {
		t.Fatalf("header progress text = %q, want 'ready to finish'", got)
	}
	app.Screenshot("merge-conflict-resolved")

	// Press Finish merge -> verification runs and merged result card appears.
	app.Press("id:merge-finish")
	if !app.Exists("id:merge-result-card") {
		t.Fatal("merge result card did not appear after Finish merge")
	}
	app.Screenshot("merge-conflict-finished")
}

func TestMergeConflictSafetyGuardsAndNonObjectResolutions(t *testing.T) {
	// 1. Non-object resolutions: scene camera.*, ambientColor, layer restore, tilemap, layerOverride, override, connection.
	baseDoc, err := sceneformat.Parse([]byte(`version: 3
inherits: scenes/base.scene.yaml
name: base_scene
virtualWidth: 640
virtualHeight: 360
ambientColor: "#222222"
camera:
  x: 10
  y: 20
  zoom: 1
  bounds:
    x: 0
    y: 0
    width: 640
    height: 360
tilemaps:
  ground:
    source: maps/base.tmj
layerOverrides:
  bg:
    order: 0
overrides:
  hero:
    x: 100
layers:
  - name: actors
    objects:
      - id: door
        x: 100
        y: 200
        originX: 4
        sprite:
          atlas: main
          frame: door_base
        particles:
          file: fx/base.particles.yaml
          atlas: fx
  - name: fx
    order: 5
    objects:
      - id: spark
        x: 10
        y: 10
connections:
  - from: door
    event: open
    to: spark
    handler: play
    once: false
    args:
      delay: 1
`))
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	mineDoc, err := sceneformat.Parse([]byte(`version: 3
inherits: scenes/base.scene.yaml
name: base_scene
virtualWidth: 640
virtualHeight: 360
ambientColor: "#111111"
camera:
  x: 50
  y: 20
  zoom: 1.5
  bounds:
    x: 0
    y: 0
    width: 800
    height: 360
tilemaps:
  ground:
    source: maps/mine.tmj
layerOverrides:
  bg:
    order: 1
overrides:
  hero:
    x: 150
layers:
  - name: actors
    objects:
      - id: door
        x: 100
        y: 200
        originX: 8
        sprite:
          atlas: main
          frame: door_mine
        particles:
          file: fx/mine.particles.yaml
          atlas: fx
connections:
  - from: door
    event: open
    to: spark
    handler: play
    once: true
    args:
      delay: 2
`))
	if err != nil {
		t.Fatalf("parse mine: %v", err)
	}
	theirsDoc, err := sceneformat.Parse([]byte(`version: 3
inherits: scenes/base.scene.yaml
name: base_scene
virtualWidth: 640
virtualHeight: 360
ambientColor: "#333333"
camera:
  x: 90
  y: 20
  zoom: 2
  bounds:
    x: 0
    y: 0
    width: 1024
    height: 360
tilemaps:
  ground:
    source: maps/theirs.tmj
layerOverrides:
  bg:
    order: 2
overrides:
  hero:
    x: 250
layers:
  - name: actors
    objects:
      - id: door
        x: 100
        y: 200
        originX: 16
        sprite:
          atlas: main
          frame: door_theirs
        particles:
          file: fx/theirs.particles.yaml
          atlas: fx
        properties:
          extraTheirs: true
  - name: fx
    order: 9
    objects:
      - id: spark
        x: 10
        y: 10
connections:
  - from: door
    event: open
    to: spark
    handler: play
    once: false
    args:
      delay: 5
`))
	if err != nil {
		t.Fatalf("parse theirs: %v", err)
	}

	ed, err := editor.New(baseDoc.Clone(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenMergeConflictSession(editor.MergeConflictSessionConfig{
		ScenePath: "scenes/test.scene.yaml",
		BaseDoc:   baseDoc,
		MineDoc:   mineDoc,
		TheirsDoc: theirsDoc,
	}); err != nil {
		t.Fatalf("OpenMergeConflictSession: %v", err)
	}

	// Verify SetMergeAgentSuggestion metadata-only update preserves item.Agent.Object (never deletes it).
	if err := ed.SetMergeAgentSuggestion("door", editor.MergeSuggestion{
		Meta:   "fields from both",
		Reason: "Keep theirs sprite/particles/originX with unique fields.",
	}); err != nil {
		t.Fatalf("SetMergeAgentSuggestion: %v", err)
	}
	if err := ed.ResolveMergeConflict("door", editor.MergeChoiceAgent); err != nil {
		t.Fatalf("ResolveMergeConflict(door, agent): %v", err)
	}
	// Resolve every non-object conflict with Theirs.
	for _, item := range ed.MergeSession().Conflicts {
		if item.ID == "door" {
			continue
		}
		if err := ed.ResolveMergeConflict(item.ID, editor.MergeChoiceTheirs); err != nil {
			t.Fatalf("ResolveMergeConflict(%s, theirs): %v", item.ID, err)
		}
	}

	resDoc, err := ed.MergeSession().BuildResultDoc()
	if err != nil {
		t.Fatalf("BuildResultDoc: %v", err)
	}
	if resDoc.AmbientColor != "#333333" {
		t.Fatalf("AmbientColor = %q, want #333333", resDoc.AmbientColor)
	}
	if resDoc.Camera == nil || resDoc.Camera.X != 90 || resDoc.Camera.Zoom == nil || *resDoc.Camera.Zoom != 2 || resDoc.Camera.Bounds == nil || resDoc.Camera.Bounds.Width != 1024 {
		t.Fatalf("Camera = %+v, want x=90 zoom=2 bounds.width=1024", resDoc.Camera)
	}
	if resDoc.Tilemaps["ground"] == nil || resDoc.Tilemaps["ground"].Source != "maps/theirs.tmj" {
		t.Fatalf("Tilemaps[ground] = %+v, want maps/theirs.tmj", resDoc.Tilemaps["ground"])
	}
	if lo, ok := resDoc.LayerOverrides["bg"]; !ok || lo.Order == nil || *lo.Order != 2 {
		t.Fatalf("LayerOverrides[bg] = %+v, want order=2", lo)
	}
	if oo, ok := resDoc.Overrides["hero"]; !ok || oo.X == nil || *oo.X != 250 {
		t.Fatalf("Overrides[hero] = %+v, want x=250", oo)
	}
	hasFXLayer := false
	var doorObj *sceneformat.ObjectDoc
	for i := range resDoc.Layers {
		if resDoc.Layers[i].Name == "fx" && resDoc.Layers[i].Order == 9 {
			hasFXLayer = true
		}
		for j := range resDoc.Layers[i].Objects {
			if resDoc.Layers[i].Objects[j].ID == "door" {
				doorObj = &resDoc.Layers[i].Objects[j]
			}
		}
	}
	if !hasFXLayer {
		t.Fatal("deleted layer fx was not restored when choosing Theirs")
	}
	if doorObj == nil {
		t.Fatal("door was unexpectedly deleted after metadata-only SetMergeAgentSuggestion")
	}
	// Because theirs had 1 unique field (extraTheirs) vs mine's 0, Agent picked theirs for conflicting fields.
	if doorObj.OriginX != 16 || doorObj.Sprite == nil || doorObj.Sprite.Frame != "door_theirs" || doorObj.Particles == nil || doorObj.Particles.File != "fx/theirs.particles.yaml" {
		t.Fatalf("door Agent merged fields wrong: originX=%v sprite=%+v particles=%+v", doorObj.OriginX, doorObj.Sprite, doorObj.Particles)
	}
	if len(resDoc.Connections) != 1 || resDoc.Connections[0].Args["delay"] != 5 {
		t.Fatalf("Connections = %+v, want delay=5 from theirs", resDoc.Connections)
	}

	// 2. OpenDemo clears merge conflict state completely.
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if ed.MergeConflictOpen() || ed.MergeSession() != nil || len(ed.DetectedMergeConflicts()) != 0 {
		t.Fatalf("OpenDemo left stale merge state: open=%v sess=%v detected=%v", ed.MergeConflictOpen(), ed.MergeSession() != nil, ed.DetectedMergeConflicts())
	}

	// 3. Play-mode guards on OpenMergeConflictSession, OpenMergeConflict, FinishMerge, AbortMerge.
	var rootScope *core.Scope
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		rootScope = cx
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(800, 600))
	if err := ed.PlayIn(rootScope); err != nil {
		t.Fatalf("PlayIn: %v", err)
	}
	if err := ed.OpenMergeConflictSession(editor.MergeConflictSessionConfig{
		BaseDoc:   baseDoc,
		MineDoc:   mineDoc,
		TheirsDoc: theirsDoc,
	}); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("OpenMergeConflictSession during Play = %v, want ErrPlaying", err)
	}
	if err := ed.OpenMergeConflict(""); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("OpenMergeConflict during Play = %v, want ErrPlaying", err)
	}
	if err := ed.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestMergeConflictPathTraversalSymlinkAndMonorepoCheckDir(t *testing.T) {
	baseDoc, err := sceneformat.Parse([]byte(mergeFixtureBaseScene))
	if err != nil {
		t.Fatal(err)
	}
	mineDoc, err := sceneformat.Parse([]byte(mergeFixtureMineScene))
	if err != nil {
		t.Fatal(err)
	}
	theirsDoc, err := sceneformat.Parse([]byte(mergeFixtureTheirsScene))
	if err != nil {
		t.Fatal(err)
	}

	repo := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.scene.yaml")
	if err := os.WriteFile(outside, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "subgame", "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "subgame", "game.yaml"), []byte("version: 1\nname: sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. FinishMerge refuses "../" path traversal outside RepoDir.
	ed, err := editor.New(baseDoc.Clone(), nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.MergePlaytestRunner = func(_, _ string, _ *sceneformat.SceneDoc) (editor.MergePlaytestResult, error) {
		return editor.MergePlaytestResult{Duration: time.Second}, nil
	}
	if err := ed.OpenMergeConflictSession(editor.MergeConflictSessionConfig{
		RepoDir:   repo,
		ScenePath: "../escape.scene.yaml",
		BaseDoc:   baseDoc,
		MineDoc:   mineDoc,
		TheirsDoc: theirsDoc,
	}); err != nil {
		t.Fatal(err)
	}
	_ = ed.TakeAllAgentMergeSuggestions()
	if err := ed.FinishMerge(); err == nil {
		t.Fatal("FinishMerge succeeded on ../escape.scene.yaml, want error")
	}

	// 2. FinishMerge refuses symlink escaping RepoDir without modifying target.
	linkPath := filepath.Join(repo, "subgame", "scenes", "link.scene.yaml")
	if err := os.Symlink(outside, linkPath); err == nil {
		if err := ed.OpenMergeConflictSession(editor.MergeConflictSessionConfig{
			RepoDir:   repo,
			ScenePath: "subgame/scenes/link.scene.yaml",
			BaseDoc:   baseDoc,
			MineDoc:   mineDoc,
			TheirsDoc: theirsDoc,
		}); err != nil {
			t.Fatal(err)
		}
		_ = ed.TakeAllAgentMergeSuggestions()
		if err := ed.FinishMerge(); err == nil {
			t.Fatal("FinishMerge succeeded on escaping symlink, want error")
		}
		got, _ := os.ReadFile(outside)
		if string(got) != "untouched" {
			t.Fatalf("outside symlink target was modified: %q", string(got))
		}
	}

	// 3. Monorepo nested game.yaml: FinishMerge passes repo/subgame to MergeCheckRunner,
	// and removes newly created scene file when check fails (!hadBackup).
	var gotCheckDir string
	ed.MergeCheckRunner = func(dir, _ string, _ *sceneformat.SceneDoc) ([]assetcheck.Problem, error) {
		gotCheckDir = dir
		return []assetcheck.Problem{{File: "scenes/new.scene.yaml", Message: "boom"}}, nil
	}
	newSceneRel := "subgame/scenes/new.scene.yaml"
	if err := ed.OpenMergeConflictSession(editor.MergeConflictSessionConfig{
		RepoDir:   repo,
		ScenePath: newSceneRel,
		BaseDoc:   baseDoc,
		MineDoc:   mineDoc,
		TheirsDoc: theirsDoc,
	}); err != nil {
		t.Fatal(err)
	}
	_ = ed.TakeAllAgentMergeSuggestions()
	if err := ed.FinishMerge(); err == nil {
		t.Fatal("FinishMerge succeeded despite MergeCheckRunner problem")
	}
	wantSubgame, _ := filepath.EvalSymlinks(filepath.Join(repo, "subgame"))
	gotSubgame, _ := filepath.EvalSymlinks(gotCheckDir)
	if gotSubgame != wantSubgame {
		t.Fatalf("MergeCheckRunner dir = %q, want nested project dir %q", gotSubgame, wantSubgame)
	}
	if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(newSceneRel))); !os.IsNotExist(err) {
		t.Fatalf("newly written scene file was not cleaned up on check failure: err=%v", err)
	}
}
