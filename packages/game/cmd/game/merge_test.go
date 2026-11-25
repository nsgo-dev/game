package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func writeSceneFile(t *testing.T, path string, doc *sceneformat.SceneDoc) {
	t.Helper()
	raw, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode(%s): %v", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func sampleMergeBase() *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version:       2,
		Name:          "Level 1",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 10,
				Objects: []sceneformat.ObjectDoc{
					{ID: "knight", X: 400, Y: 222, Z: 10},
					{ID: "door", X: 584, Y: 260},
				},
			},
		},
	}
}

func TestMergeCleanInPlaceAndOut(t *testing.T) {
	dir := t.TempDir()
	base := sampleMergeBase()
	ours := sampleMergeBase()
	ours.Layers[0].Objects[0].X = 420
	theirs := sampleMergeBase()
	theirs.Layers[0].Objects[1].X = 600

	baseFile := filepath.Join(dir, "base.scene.yaml")
	oursFile := filepath.Join(dir, "ours.scene.yaml")
	theirsFile := filepath.Join(dir, "theirs.scene.yaml")
	outFile := filepath.Join(dir, "merged.scene.yaml")
	writeSceneFile(t, baseFile, base)
	writeSceneFile(t, oursFile, ours)
	writeSceneFile(t, theirsFile, theirs)

	// With -out: ours stays untouched, merged scene is written to outFile.
	a, _, stderr := newTestApp(t)
	if code := run([]string{"merge", "-out", outFile, baseFile, oursFile, theirsFile, "scenes/level_01.scene.yaml"}, a); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("Parse merged: %v", err)
	}
	if merged.Layers[0].Objects[0].X != 420 || merged.Layers[0].Objects[1].X != 600 {
		t.Errorf("merged positions = (%v, %v), want (420, 600)", merged.Layers[0].Objects[0].X, merged.Layers[0].Objects[1].X)
	}

	// Without -out: overwrites ours in place (as Git expects).
	a, _, stderr = newTestApp(t)
	if code := run([]string{"merge", baseFile, oursFile, theirsFile, "scenes/level_01.scene.yaml"}, a); code != 0 {
		t.Fatalf("in-place exit %d: %s", code, stderr)
	}
	inPlace, err := os.ReadFile(oursFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(inPlace, raw) {
		t.Errorf("in-place output differs from -out:\n%s\nvs\n%s", inPlace, raw)
	}
}

func TestMergeConflictWritesValidConflictYAMLAndExits1(t *testing.T) {
	dir := t.TempDir()
	base := sampleMergeBase()
	ours := sampleMergeBase()
	ours.Layers[0].Objects[1].X = 560
	theirs := sampleMergeBase()
	theirs.Layers[0].Objects[1].X = 600

	baseFile := filepath.Join(dir, "base.scene.yaml")
	oursFile := filepath.Join(dir, "ours.scene.yaml")
	theirsFile := filepath.Join(dir, "theirs.scene.yaml")
	writeSceneFile(t, baseFile, base)
	writeSceneFile(t, oursFile, ours)
	writeSceneFile(t, theirsFile, theirs)

	a, _, stderr := newTestApp(t)
	if code := run([]string{"merge", baseFile, oursFile, theirsFile, "scenes/level_01.scene.yaml"}, a); code != 1 {
		t.Fatalf("exit %d, want 1 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr.String(), "1 merge conflict(s)") {
		t.Errorf("stderr = %q, want conflict summary", stderr)
	}

	raw, err := os.ReadFile(oursFile)
	if err != nil {
		t.Fatal(err)
	}
	cdoc, err := sceneformat.ParseConflict(raw)
	if err != nil {
		t.Fatalf("ParseConflict: %v\n%s", err, raw)
	}
	if cdoc.Path != "scenes/level_01.scene.yaml" || len(cdoc.Conflicts) != 1 {
		t.Fatalf("conflict doc = %+v", cdoc)
	}
	c := cdoc.Conflicts[0]
	if c.Object != "door" || c.Field != "x" || c.Base != 584 || c.Ours != 560 || c.Theirs != 600 {
		t.Errorf("conflict = %+v", c)
	}
	// Conflict YAML must not accidentally parse as a valid scene.
	if _, err := sceneformat.Parse(raw); err == nil {
		t.Error("conflict YAML unexpectedly parsed as a valid SceneDoc")
	}
}

func TestMergeConflictOutPreservesOurs(t *testing.T) {
	dir := t.TempDir()
	base := sampleMergeBase()
	ours := sampleMergeBase()
	ours.Layers[0].Objects[1].X = 560
	theirs := sampleMergeBase()
	theirs.Layers[0].Objects[1].X = 600

	baseFile := filepath.Join(dir, "base.scene.yaml")
	oursFile := filepath.Join(dir, "ours.scene.yaml")
	theirsFile := filepath.Join(dir, "theirs.scene.yaml")
	conflictFile := filepath.Join(dir, "conflict.yaml")
	writeSceneFile(t, baseFile, base)
	writeSceneFile(t, oursFile, ours)
	writeSceneFile(t, theirsFile, theirs)
	oursBefore, _ := os.ReadFile(oursFile)

	a, _, _ := newTestApp(t)
	if code := run([]string{"merge", "-conflict-out", conflictFile, baseFile, oursFile, theirsFile}, a); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	oursAfter, _ := os.ReadFile(oursFile)
	if !bytes.Equal(oursBefore, oursAfter) {
		t.Error("-conflict-out modified ours file")
	}
	raw, err := os.ReadFile(conflictFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sceneformat.ParseConflict(raw); err != nil {
		t.Fatalf("ParseConflict(%s): %v", conflictFile, err)
	}
}

func TestMergeEmptyBaseAddAdd(t *testing.T) {
	dir := t.TempDir()
	baseFile := filepath.Join(dir, "base.scene.yaml")
	if err := os.WriteFile(baseFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ours := &sceneformat.SceneDoc{
		Version: 2, Name: "New",
		Layers: []sceneformat.LayerDoc{{Name: "main", Objects: []sceneformat.ObjectDoc{{ID: "a"}}}},
	}
	theirs := &sceneformat.SceneDoc{
		Version: 2, Name: "New",
		Layers: []sceneformat.LayerDoc{{Name: "main", Objects: []sceneformat.ObjectDoc{{ID: "b"}}}},
	}
	oursFile := filepath.Join(dir, "ours.scene.yaml")
	theirsFile := filepath.Join(dir, "theirs.scene.yaml")
	writeSceneFile(t, oursFile, ours)
	writeSceneFile(t, theirsFile, theirs)

	a, _, stderr := newTestApp(t)
	if code := run([]string{"merge", baseFile, oursFile, theirsFile}, a); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	raw, err := os.ReadFile(oursFile)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Layers[0].Objects) != 2 {
		t.Errorf("got %d objects, want 2", len(doc.Layers[0].Objects))
	}
}

func TestMergeUsageErrors(t *testing.T) {
	cases := [][]string{
		{"merge"},
		{"merge", "a", "b"},
		{"merge", "a", "b", "c", "d", "e"},
		{"merge", "-setup", "-out", "x"},
		{"merge", "-setup", "-conflict-out", "x"},
		{"merge", "-setup", "a", "b"},
	}
	for _, args := range cases {
		a, stdout, stderr := newTestApp(t)
		if code := run(args, a); code != 2 {
			t.Errorf("%q exit %d, want 2 (stderr: %s)", args, code, stderr)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "usage: game merge") {
			t.Errorf("%q stdout=%q stderr=%q", args, stdout, stderr)
		}
	}
}

func TestMergeSetupAndNewInGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	gitCmd := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	t.Run("setup_idempotent", func(t *testing.T) {
		dir := t.TempDir()
		gitCmd(dir, "init")

		a, stdout, stderr := newTestApp(t)
		if code := run([]string{"merge", "-setup", dir}, a); code != 0 {
			t.Fatalf("merge -setup exit %d: %s", code, stderr)
		}
		if !strings.Contains(stdout.String(), "configured git merge.nsgo-scene.driver") {
			t.Errorf("stdout = %q", stdout)
		}
		if got := gitCmd(dir, "config", "--local", "merge.nsgo-scene.driver"); got != sceneMergeDriverCmd {
			t.Errorf("driver = %q, want %q", got, sceneMergeDriverCmd)
		}
		attrs, err := os.ReadFile(filepath.Join(dir, ".gitattributes"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(attrs), "*.scene.yaml merge=nsgo-scene") {
			t.Errorf(".gitattributes:\n%s", attrs)
		}

		// Second run does not duplicate lines.
		a, stdout, stderr = newTestApp(t)
		if code := run([]string{"merge", "-setup", dir}, a); code != 0 {
			t.Fatalf("second merge -setup exit %d: %s", code, stderr)
		}
		attrs2, _ := os.ReadFile(filepath.Join(dir, ".gitattributes"))
		if !bytes.Equal(attrs, attrs2) {
			t.Errorf("second -setup changed .gitattributes:\n%s", attrs2)
		}
	})

	t.Run("new_in_git_repo_configures_driver", func(t *testing.T) {
		stubGo(t, nil)
		dir := filepath.Join(t.TempDir(), "mygame")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		gitCmd(dir, "init")

		a, _, stderr := newTestApp(t)
		if code := run([]string{"new", dir}, a); code != 0 {
			t.Fatalf("game new exit %d: %s", code, stderr)
		}
		if got := gitCmd(dir, "config", "--local", "merge.nsgo-scene.driver"); got != sceneMergeDriverCmd {
			t.Errorf("game new did not configure merge driver: got %q, want %q", got, sceneMergeDriverCmd)
		}
		attrs, err := os.ReadFile(filepath.Join(dir, ".gitattributes"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(attrs), "*.scene.yaml merge=nsgo-scene") {
			t.Errorf(".gitattributes missing merge rule:\n%s", attrs)
		}
	})
}

// TestMergeEndToEndWithGit builds the game binary, configures it as a real Git
// merge driver in a temporary repository, and runs real `git merge` commands
// for both a clean two-branch object merge and a conflicting single-field merge.
func TestMergeEndToEndWithGit(t *testing.T) {
	if testing.Short() {
		t.Skip("builds cmd/game binary and runs git merge")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	bin := filepath.Join(t.TempDir(), "game")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	buildCmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build cmd/game: %v\n%s", err, out)
	}

	repo := t.TempDir()
	gitRun := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	mustGit := func(args ...string) string {
		t.Helper()
		out, err := gitRun(args...)
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return out
	}

	mustGit("init", "-b", "main")
	mustGit("config", "merge.nsgo-scene.name", sceneMergeDriverTitle)
	driverCmd := shellWord(filepath.ToSlash(bin), "linux") + " merge %O %A %B %P"
	mustGit("config", "merge.nsgo-scene.driver", driverCmd)

	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte(sceneGitAttributesContent), 0o644); err != nil {
		t.Fatal(err)
	}
	sceneRel := filepath.Join("scenes", "level_01.scene.yaml")
	sceneAbs := filepath.Join(repo, sceneRel)
	writeSceneFile(t, sceneAbs, sampleMergeBase())
	mustGit("add", ".gitattributes", sceneRel)
	mustGit("commit", "-m", "base scene")

	// Scenario 1: Two branches move different objects -> clean merge with both moves kept.
	mustGit("checkout", "-b", "branch-a")
	docA := sampleMergeBase()
	docA.Layers[0].Objects[0].X = 420
	writeSceneFile(t, sceneAbs, docA)
	mustGit("commit", "-am", "move knight")

	mustGit("checkout", "main")
	mustGit("checkout", "-b", "branch-b")
	docB := sampleMergeBase()
	docB.Layers[0].Objects[1].X = 600
	writeSceneFile(t, sceneAbs, docB)
	mustGit("commit", "-am", "move door")

	mustGit("checkout", "branch-a")
	mustGit("merge", "--no-edit", "branch-b")

	rawMerged, err := os.ReadFile(sceneAbs)
	if err != nil {
		t.Fatal(err)
	}
	mergedDoc, err := sceneformat.Parse(rawMerged)
	if err != nil {
		t.Fatalf("Parse git-merged scene: %v\n%s", err, rawMerged)
	}
	if mergedDoc.Layers[0].Objects[0].X != 420 || mergedDoc.Layers[0].Objects[1].X != 600 {
		t.Errorf("merged positions = (%v, %v), want (420, 600)", mergedDoc.Layers[0].Objects[0].X, mergedDoc.Layers[0].Objects[1].X)
	}

	// Scenario 2: Both branches change door.x differently -> git marks unmerged and leaves ConflictDoc YAML.
	mustGit("checkout", "main")
	mustGit("checkout", "-b", "conflict-a")
	docCA := sampleMergeBase()
	docCA.Layers[0].Objects[1].X = 560
	writeSceneFile(t, sceneAbs, docCA)
	mustGit("commit", "-am", "door to 560")

	mustGit("checkout", "main")
	mustGit("checkout", "-b", "conflict-b")
	docCB := sampleMergeBase()
	docCB.Layers[0].Objects[1].X = 600
	writeSceneFile(t, sceneAbs, docCB)
	mustGit("commit", "-am", "door to 600")

	mustGit("checkout", "conflict-a")
	if out, err := gitRun("merge", "--no-edit", "conflict-b"); err == nil {
		t.Fatalf("expected git merge conflict, succeeded:\n%s", out)
	}
	status := mustGit("status", "--porcelain")
	if !strings.Contains(status, "UU scenes/level_01.scene.yaml") {
		t.Errorf("git status = %q, want UU scenes/level_01.scene.yaml", status)
	}
	conflictRaw, err := os.ReadFile(sceneAbs)
	if err != nil {
		t.Fatal(err)
	}
	cdoc, err := sceneformat.ParseConflict(conflictRaw)
	if err != nil {
		t.Fatalf("ParseConflict after git merge: %v\n%s", err, conflictRaw)
	}
	if cdoc.Path != "scenes/level_01.scene.yaml" || len(cdoc.Conflicts) != 1 {
		t.Fatalf("conflict doc = %+v", cdoc)
	}
	c := cdoc.Conflicts[0]
	if c.Object != "door" || c.Field != "x" || c.Base != 584 || c.Ours != 560 || c.Theirs != 600 {
		t.Errorf("conflict = %+v, want door.x (584, 560, 600)", c)
	}
}
