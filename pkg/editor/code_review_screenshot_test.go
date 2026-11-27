package editor_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const macosCodeReviewPrefabOrig = `version: 1
name: slime
components:
  patrol:
    speed: 32
    turn_pause: 0.4
  hurtbox: { w: 26, h: 16 }
`

const macosCodeReviewPrefabNext = `version: 1
name: slime
components:
  patrol:
    speed: 32
    turn_pause: 0.4
    turn_at_ledges: true
  hurtbox: { w: 26, h: 16 }
`

const macosCodeReviewPatrolOrig = `package gameplay

// Update moves the slime along its patrol.
func (s *Slime) Update(dt float64, lvl *Level) {
	s.X += s.Dir * s.Speed * dt
	if s.X < s.From || s.X > s.To {
		s.Dir = -s.Dir
	}
}

// turn reverses direction and pauses for turn_pause seconds.
func (s *Slime) turn() {
	s.Dir = -s.Dir
}
`

const macosCodeReviewPatrolRound1 = `package gameplay

// Update moves the slime along its patrol.
func (s *Slime) Update(dt float64, lvl *Level) {
	s.X += s.Dir * s.Speed * dt
	if s.X < s.From || s.X > s.To || !s.floorAhead(lvl) {
		s.turn()
	}
}

// floorAhead reports whether there is ground one pixel below the leading edge.
func (s *Slime) floorAhead(lvl *Level) bool {
	x := s.X + s.Dir*(s.W/2+1)
	return lvl.SolidAt(x, s.Y+s.H)
}

// turn reverses direction and pauses for turn_pause seconds.
func (s *Slime) turn() {
	s.Dir = -s.Dir
}
`

const macosCodeReviewPatrolRound2 = `package gameplay

// Update moves the slime along its patrol.
func (s *Slime) Update(dt float64, lvl *Level) {
	s.X += s.Dir * s.Speed * dt
	if s.X < s.From || s.X > s.To || !s.floorAhead(lvl) {
		s.turn()
	}
}

// floorAhead reports whether there is ground one pixel below the leading edge.
func (s *Slime) floorAhead(lvl *Level) bool {
	x := s.X + s.Dir*(s.W/2+1)
	return lvl.SolidAt(x, s.Y+s.H+1)
}

// turn reverses direction and pauses for turn_pause seconds.
func (s *Slime) turn() {
	s.Dir = -s.Dir
}
`

const macosCodeReviewTestFile = `package gameplay

import "testing"

// TestSlimeTurnsAtLedge plays slime_06 on level_02's upper platform for 10 s and fails if it ever leaves it.
func TestSlimeTurnsAtLedge(t *testing.T) {
	lvl := loadLevel(t, "scenes/level_02.scene.yaml")
	s := lvl.Slime("slime_06")
	edge := lvl.PlatformUnder(s).Right - s.W/2
	for range 600 {
		s.Update(1.0/60, lvl)
		if s.X > edge {
			t.Fatalf("%s walked off the ledge at x %.0f, want a turn at x %.0f", s.ID, s.X, edge)
		}
	}
}

func TestSlimeKeepsPatrolBounds(t *testing.T) {
	lvl := loadLevel(t, "scenes/level_01.scene.yaml")
	s := lvl.Slime("slime_01")
	for range 600 {
		s.Update(1.0/60, lvl)
		if s.X < s.From || s.X > s.To {
			t.Fatalf("%s left its patrol: x %.0f not in %.0f–%.0f", s.ID, s.X, s.From, s.To)
		}
	}
}
`

// TestCodeReviewMacOSScreenshots renders the Go code review states (failed unified, fixing/running,
// passed round 2 split, light theme parity, and Agent panel waiting card after Keep with unconfirmed Go)
// on the real native macOS host for visual review beside design/editor/ReviewCode.dc.html.
func TestCodeReviewMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"scenes", "prefabs", "gameplay"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scenePath := filepath.Join(dir, "scenes", "level_02.scene.yaml")
	if err := os.WriteFile(scenePath, []byte(macosPlayModeScene), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte("version: 1\nname: dungeon-run\nstartScene: scenes/level_02.scene.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prefabs", "slime.prefab.yaml"), []byte(macosCodeReviewPrefabOrig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gameplay", "patrol.go"), []byte(macosCodeReviewPatrolOrig), 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := sceneformat.Parse([]byte(macosPlayModeScene))
	if err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectPath = filepath.Join(dir, "game.yaml")
	ed.FilePath = scenePath
	ed.ScenePath = "scenes/level_02.scene.yaml"
	ed.ExternalEditorName = "GoLand"
	ed.SetLanguage(editor.LanguageEnglish)
	ed.SetAppearance(editor.AppearanceDark)
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{ID: "c1", Name: "claude-code", Transport: "stdio"})

	phase := editor.CodeReviewChecksFailed
	ed.CodeReviewCheckRunner = func(_ context.Context, _ editor.CodeReviewCheckRequest, _ func(editor.CodeReviewCheckProgress)) (editor.CodeReviewCheckResult, error) {
		if phase == editor.CodeReviewChecksFailed {
			return editor.CodeReviewCheckResult{
				Phase:       editor.CodeReviewChecksFailed,
				CheckPassed: true,
				TestPassed:  false,
				PassedCount: 41,
				FailedCount: 1,
				TotalCount:  42,
				Duration:    3200 * time.Millisecond,
				Packages:    []string{"./gameplay"},
				Failures: []editor.CodeReviewTestFailure{{
					Package:     "./gameplay",
					TestName:    "TestSlimeTurnsAtLedge",
					File:        "patrol_test.go",
					Line:        13,
					Message:     "patrol_test.go:13: slime_06 walked off the ledge at x 486, want a turn at x 485",
					Explanation: "The new floor check on line 14 probes the row the slime stands on, so it finds no ground only after the slime has stepped off.",
					SuspectFile: "gameplay/patrol.go",
					SuspectLine: 14,
				}},
			}, nil
		}
		return editor.CodeReviewCheckResult{
			Phase:       editor.CodeReviewChecksPassed,
			CheckPassed: true,
			TestPassed:  true,
			PassedCount: 42,
			TotalCount:  42,
			Duration:    3100 * time.Millisecond,
			Packages:    []string{"./gameplay"},
		}, nil
	}

	if err := ed.ProposeCodeReviewFiles(editor.CodeReviewProposeRequest{
		Prompt:     "Slimes walk off the platform edges on level_02. Make them turn around at ledges.",
		PlayImpact: "Until you confirm it, slimes still walk off ledges in play.",
		Open:       true,
		Files: []editor.CodeReviewProposedFile{
			{Path: "prefabs/slime.prefab.yaml", Content: macosCodeReviewPrefabNext, Note: "Part of Keep. Every slime prefab variant inherits it."},
			{Path: "gameplay/patrol.go", Content: macosCodeReviewPatrolRound1},
			{Path: "gameplay/patrol_test.go", Content: macosCodeReviewTestFile},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ed.RunCodeReviewChecks(context.Background()); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	// 1. Failed state (Unified, Dark).
	app.Screenshot("code-review-failed-dark")

	// 2. Fixing / Running state (18 of 42 + Agent edited patrol.go:14 pill).
	app.Post(func() {
		_ = ed.AskAgentToFixCodeReview()
		_ = ed.ProposeCodeReviewFiles(editor.CodeReviewProposeRequest{
			RoundSummary: "Round 2 changed one line: the probe now looks one pixel below the slime's feet.",
			PreserveTurn: true,
			Open:         true,
			Files: []editor.CodeReviewProposedFile{
				{Path: "gameplay/patrol.go", Content: macosCodeReviewPatrolRound2},
			},
		})
		sess := ed.CodeReviewSession()
		sess.Checks.Phase = editor.CodeReviewChecksFixing
		sess.Checks.Completed = 18
		sess.Checks.ExpectedTotal = 42
		ed.SetCodeReviewLayout(editor.CodeReviewDiffUnified)
	})
	app.Settle()
	app.Screenshot("code-review-fixing-dark")

	// 3. Passed state (Round 2, Split, patrol.go confirmed).
	phase = editor.CodeReviewChecksPassed
	app.Post(func() {
		_ = ed.RunCodeReviewChecks(context.Background())
		_ = ed.ConfirmCodeReviewFile("gameplay/patrol.go", true)
		ed.SetCodeReviewLayout(editor.CodeReviewDiffSplit)
	})
	app.Settle()
	app.Screenshot("code-review-passed-split-dark")

	// 4. Light appearance parity (Unified, Passed).
	app.Post(func() {
		ed.SetAppearance(editor.AppearanceLight)
		ed.SetCodeReviewLayout(editor.CodeReviewDiffUnified)
	})
	app.Settle()
	app.Screenshot("code-review-passed-light")

	// 5. Agent panel waiting card after Keep with patrol_test.go still unconfirmed.
	app.Post(func() {
		ed.SetAppearance(editor.AppearanceDark)
		_ = ed.KeepCodeReview()
	})
	app.Settle()
	app.Screenshot("code-review-agent-waiting-dark")
}
