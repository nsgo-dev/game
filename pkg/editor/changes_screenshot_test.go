package editor_test

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func runMacOSTestGit(t *testing.T, dir string, args ...string) {
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
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeMacOSFixtureFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestChangesPanelMacOSScreenshots renders the Changes & Commit screen on the real native macOS host
// (linking github.com/nsgo-dev/nsgo through editor_test.go) and captures scene comparison, code diff + checks,
// audio audition, and post-commit screenshots for visual review beside VersionControl.dc.html screen 1.
func TestChangesPanelMacOSScreenshots(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := filepath.Join(t.TempDir(), "dungeon-run")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runMacOSTestGit(t, repoDir, "init", "-b", "main")
	runMacOSTestGit(t, repoDir, "config", "user.name", "you")
	runMacOSTestGit(t, repoDir, "config", "user.email", "you@example.com")

	writeMacOSFixtureFile(t, filepath.Join(repoDir, "game.yaml"), "version: 1\nname: dungeon-run\nid: dev.example.dungeonrun\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n")
	writeMacOSFixtureFile(t, filepath.Join(repoDir, "scenes", "level_01.scene.yaml"), macosMergeBaseScene)
	writeMacOSFixtureFile(t, filepath.Join(repoDir, "data", "pickups.data.yaml"), "version: 1\ntable: pickups\nrows:\n  - id: coin\n    sprite: items/coin_spin\n    score: 10\n    sound: audio/coin.wav\n  - id: heart\n    heals: 1\n")
	writeMacOSFixtureFile(t, filepath.Join(repoDir, "scripts", "slime.go"), "package scripts\n\nfunc PatrolUpdate(x, dir, speed, dt float64) float64 {\n\treturn x + dir*speed*dt\n}\n")

	runMacOSTestGit(t, repoDir, "add", ".")
	runMacOSTestGit(t, repoDir, "commit", "-m", "Initial dungeon-run")
	runMacOSTestGit(t, repoDir, "checkout", "-b", "feat/slime-patrol")

	writeMacOSFixtureFile(t, filepath.Join(repoDir, "scenes", "level_01.scene.yaml"), strings.ReplaceAll(macosMergeMineScene, "audio/door_creak.ogg", "audio/door_creak.wav"))
	writeMacOSFixtureFile(t, filepath.Join(repoDir, "data", "pickups.data.yaml"), "version: 1\ntable: pickups\nrows:\n  - id: coin\n    sprite: items/coin_spin\n    score: 25\n    sound: audio/coin.wav\n  - id: heart\n    heals: 1\n")
	writeMacOSFixtureFile(t, filepath.Join(repoDir, "scripts", "slime.go"), "package scripts\n\nfunc PatrolUpdate(x, dir, speed, dt float64) float64 {\n\t// Turn at ledges.\n\tnext := x + dir*speed*dt\n\treturn next\n}\n")

	const sr = 22050
	samples := make([]float64, sr/4)
	for i := range samples {
		samples[i] = 0.45 * (1.0 - float64(i)/float64(len(samples))) * math.Sin(2*math.Pi*440*float64(i)/sr)
	}
	wav, err := sound.EncodeWAV(samples, sr, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, "audio"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "audio", "door_creak.wav"), wav, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatal(err)
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
	ed.CommitMessageDrafter = func(ctx context.Context, req editor.CommitDraftRequest) (editor.CommitDraft, error) {
		return editor.CommitDraft{
			Summary:     "Add a plank guard and a coin trail to level_01",
			Description: "- slime_03 guards the plank; slime_02 runs at 30\n- Three coins lead to the door; coins score 25\n- Slimes turn at ledges (scripts/slime.go)\n- Door moves to x 560 and creaks on open\n\nPlaytest: 10 s, 0 falls, 3 of 3 coins reachable\nMade from 3 agent requests, reviewed and kept",
			Provenance:  "Drafted from 3 reviews and a playtest",
			Attribution: "Made from 3 agent requests, reviewed and kept",
		}, nil
	}
	ed.GHRunner = func(dir string, args ...string) (string, error) {
		return "https://github.com/nsgo-dev/dungeon-run/pull/1", nil
	}

	if err := ed.OpenProject(filepath.Join(repoDir, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.OpenChangesPanel(); err != nil {
		t.Fatalf("OpenChangesPanel: %v", err)
	}
	ed.SetChangesContext(3, "10 s, 0 falls, 3 of 3 coins reachable", 3)
	_ = ed.RunChangesChecks()
	_ = ed.RedraftCommitMessage()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Screenshot("changes-scene")

	app.Press("id:changes-pick-2")
	app.Screenshot("changes-code")

	app.Press("id:changes-pick-3")
	app.Screenshot("changes-audio")

	app.Press("id:changes-commit")
	app.Screenshot("changes-committed")
}
