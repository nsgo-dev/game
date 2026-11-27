package editormcp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
)

const (
	mcpPatrolOrig = `package gameplay

type Level struct {
	PlatformRight float64
	FloorY        float64
}

func (l *Level) SolidAt(x, y float64) bool {
	if y == l.FloorY {
		return true
	}
	return x <= l.PlatformRight && y > l.FloorY
}

type Slime struct {
	ID    string
	X, Y  float64
	W, H  float64
	Dir   float64
	Speed float64
	From  float64
	To    float64
}

func (s *Slime) Update(dt float64, lvl *Level) {
	s.X += s.Dir * s.Speed * dt
	if s.X < s.From || s.X > s.To {
		s.Dir = -s.Dir
	}
}
`
	mcpPatrolBuggy = `package gameplay

type Level struct {
	PlatformRight float64
	FloorY        float64
}

func (l *Level) SolidAt(x, y float64) bool {
	if y == l.FloorY {
		return true
	}
	return x <= l.PlatformRight && y > l.FloorY
}

type Slime struct {
	ID    string
	X, Y  float64
	W, H  float64
	Dir   float64
	Speed float64
	From  float64
	To    float64
}

func (s *Slime) Update(dt float64, lvl *Level) {
	s.X += s.Dir * s.Speed * dt
	if s.X < s.From || s.X > s.To || !s.floorAhead(lvl) {
		s.Dir = -s.Dir
	}
}

func (s *Slime) floorAhead(lvl *Level) bool {
	return lvl.SolidAt(s.X+s.Dir*(s.W/2+1), s.Y+s.H)
}
`
	mcpPatrolFixed = `package gameplay

type Level struct {
	PlatformRight float64
	FloorY        float64
}

func (l *Level) SolidAt(x, y float64) bool {
	if y == l.FloorY {
		return true
	}
	return x <= l.PlatformRight && y > l.FloorY
}

type Slime struct {
	ID    string
	X, Y  float64
	W, H  float64
	Dir   float64
	Speed float64
	From  float64
	To    float64
}

func (s *Slime) Update(dt float64, lvl *Level) {
	s.X += s.Dir * s.Speed * dt
	if s.X < s.From || s.X > s.To || !s.floorAhead(lvl) {
		s.Dir = -s.Dir
	}
}

func (s *Slime) floorAhead(lvl *Level) bool {
	return lvl.SolidAt(s.X+s.Dir*(s.W/2+1), s.Y+s.H+1)
}
`
	mcpPatrolTest = `package gameplay

import "testing"

func TestSlimeTurnsAtLedge(t *testing.T) {
	lvl := &Level{PlatformRight: 500, FloorY: 200}
	s := &Slime{ID: "slime_06", X: 460, Y: 184, W: 30, H: 16, Dir: 1, Speed: 60, From: 380, To: 560}
	edge := lvl.PlatformRight - s.W/2
	for range 600 {
		s.Update(1.0/60, lvl)
		if s.X > edge+2 {
			t.Fatalf("%s walked off the ledge at x %.0f, want a turn at x %.0f", s.ID, s.X, edge)
		}
	}
}
`
	mcpPrefabOrig = "version: 1\nname: slime\n"
	mcpPrefabNext = "version: 1\nname: slime\nturn_at_ledges: true\n"
)

func mcpOverlayCheckRunner(ctx context.Context, req editor.CodeReviewCheckRequest, _ func(editor.CodeReviewCheckProgress)) (editor.CodeReviewCheckResult, error) {
	started := time.Now()
	res := editor.CodeReviewCheckResult{Packages: req.Packages}
	env := append(os.Environ(), "GOWORK=off", "GOFLAGS=-overlay="+req.OverlayPath)

	vetCmd := exec.CommandContext(ctx, "go", append([]string{"vet"}, req.Packages...)...)
	vetCmd.Dir = req.ProjectDir
	vetCmd.Env = env
	if out, err := vetCmd.CombinedOutput(); err != nil {
		res.CheckPassed = false
		res.CheckError = strings.TrimSpace(string(out))
		res.Phase = editor.CodeReviewChecksFailed
		res.Duration = time.Since(started)
		return res, nil
	}
	res.CheckPassed = true

	testCmd := exec.CommandContext(ctx, "go", append([]string{"test", "-json", "-count=1"}, req.Packages...)...)
	testCmd.Dir = req.ProjectDir
	testCmd.Env = env
	out, _ := testCmd.Output()
	res.TestPassed = true
	linesByTest := map[string][]string{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		var ev struct {
			Action string `json:"Action"`
			Test   string `json:"Test"`
			Output string `json:"Output"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Test != "" && ev.Output != "" {
			linesByTest[ev.Test] = append(linesByTest[ev.Test], ev.Output)
		}
		if ev.Test == "" || strings.Contains(ev.Test, "/") {
			continue
		}
		switch ev.Action {
		case "pass":
			res.PassedCount++
		case "fail":
			res.FailedCount++
			res.TestPassed = false
			msg := strings.TrimSpace(strings.Join(linesByTest[ev.Test], ""))
			line := 12
			for _, l := range linesByTest[ev.Test] {
				tr := strings.TrimSpace(l)
				if strings.HasPrefix(tr, "patrol_test.go:") {
					msg = tr
					_, _ = fmt.Sscanf(tr, "patrol_test.go:%d:", &line)
					break
				}
			}
			res.Failures = append(res.Failures, editor.CodeReviewTestFailure{
				Package:  req.Packages[0],
				TestName: ev.Test,
				File:     "patrol_test.go",
				Line:     line,
				Message:  msg,
			})
		}
	}
	res.TotalCount = res.PassedCount + res.FailedCount
	res.Duration = time.Since(started)
	if res.CheckPassed && res.TestPassed && res.FailedCount == 0 {
		res.Phase = editor.CodeReviewChecksPassed
	} else {
		res.Phase = editor.CodeReviewChecksFailed
	}
	return res, nil
}

// #1034 Acceptance:
// A turn edits a Go file with a failing test through MCP, checks that Confirm is refused and disk is unchanged,
// has the agent fix it through MCP (round 2 in the same turn), reruns checks, confirms, keeps, finds the new
// files on disk, and one Undo restores the old files.
func TestCodeReviewMCPFailFixConfirmKeepAndUndo(t *testing.T) {
	if runtime.GOOS == "ios" || runtime.GOOS == "android" || runtime.GOOS == "js" {
		t.Skip("requires desktop Go toolchain")
	}
	f := openFixture(t)
	if err := os.WriteFile(filepath.Join(f.dir, "go.mod"), []byte("module example.com/testgame\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.dir, "gameplay"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.dir, "prefabs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "gameplay/patrol.go"), []byte(mcpPatrolOrig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "prefabs/slime.prefab.yaml"), []byte(mcpPrefabOrig), 0o644); err != nil {
		t.Fatal(err)
	}
	f.ed.CodeReviewCheckRunner = mcpOverlayCheckRunner
	if err := f.ed.SetAgentPrivacy(editor.PrivacyGoSource, true); err != nil {
		t.Fatalf("SetAgentPrivacy(GoSource): %v", err)
	}

	s := editormcp.New(f.ed, editormcp.Options{OnPermissionAsk: allowAsk})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// Path traversal is refused.
	badRes := call(t, cs, editormcp.ToolCodeReviewPropose, map[string]any{
		"files": []map[string]any{{"path": "../escape.go", "content": "package evil\n"}},
	})
	if !badRes.IsError {
		t.Fatal("expected path traversal in code_review_propose to fail")
	}

	// 1) Turn 1: agent proposes buggy patrol.go, new patrol_test.go, and slime.prefab.yaml.
	res := call(t, cs, editormcp.ToolCodeReviewPropose, map[string]any{
		"prompt":              "Slimes walk off platform ledges. Make them turn around.",
		"failure_explanation": "Line 30 probes s.Y+s.H instead of one pixel below the slime's feet.",
		"suspect_file":        "gameplay/patrol.go",
		"suspect_line":        30,
		"play_impact":         "Until you confirm it, slimes still walk off ledges in play.",
		"files": []map[string]any{
			{"path": "prefabs/slime.prefab.yaml", "content": mcpPrefabNext},
			{"path": "gameplay/patrol.go", "content": mcpPatrolBuggy},
			{"path": "gameplay/patrol_test.go", "content": mcpPatrolTest},
		},
	})
	_ = mustOK(t, res)
	var st editormcp.CodeReviewStatusResult
	structured(t, res, &st)

	if !st.Active || st.Round != 1 || st.Checks.Phase != "failed" || st.Checks.FailedCount != 1 {
		t.Fatalf("unexpected round 1 status: %+v", st)
	}
	// Disk must still hold original patrol.go and no patrol_test.go.
	gotPatrol, _ := os.ReadFile(filepath.Join(f.dir, "gameplay/patrol.go"))
	if string(gotPatrol) != mcpPatrolOrig {
		t.Fatal("patrol.go on disk changed before Confirm/Keep")
	}
	if _, err := os.Stat(filepath.Join(f.dir, "gameplay/patrol_test.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("patrol_test.go exists on disk before Confirm/Keep: %v", err)
	}

	// Confirm is refused while failing.
	if err := f.ed.ConfirmCodeReviewFile("gameplay/patrol.go", true); !errors.Is(err, editor.ErrCodeReviewChecksNotPassed) {
		t.Fatalf("ConfirmCodeReviewFile while failing = %v, want ErrCodeReviewChecksNotPassed", err)
	}

	// 2) Author asks agent to fix -> status reflects fixRequested.
	if err := f.ed.AskAgentToFixCodeReview(); err != nil {
		t.Fatalf("AskAgentToFixCodeReview: %v", err)
	}
	stRes := call(t, cs, editormcp.ToolCodeReviewStatus, map[string]any{})
	_ = mustOK(t, stRes)
	var stFix editormcp.CodeReviewStatusResult
	structured(t, stRes, &stFix)
	if !stFix.FixRequested || stFix.TurnID != st.TurnID {
		t.Fatalf("expected fixRequested=true on same turn, got %+v", stFix)
	}

	// 3) Agent submits Round 2 fix through MCP and runs checks via code_review_run_checks.
	fixProp := call(t, cs, editormcp.ToolCodeReviewPropose, map[string]any{
		"summary":    "Round 2 probes s.Y+s.H+1.",
		"run_checks": false,
		"files": []map[string]any{
			{"path": "gameplay/patrol.go", "content": mcpPatrolFixed},
		},
	})
	_ = mustOK(t, fixProp)

	checkRes := call(t, cs, editormcp.ToolCodeReviewRunChecks, map[string]any{})
	_ = mustOK(t, checkRes)
	var stPassed editormcp.CodeReviewStatusResult
	structured(t, checkRes, &stPassed)
	if stPassed.Round != 2 || stPassed.TurnID != st.TurnID || stPassed.Checks.Phase != "passed" || stPassed.Checks.PassedCount != 1 {
		t.Fatalf("unexpected round 2 check result: %+v", stPassed)
	}

	// 4) Confirm both Go files, Keep, verify new files on disk, then Undo restores old ones.
	if err := f.ed.ConfirmCodeReviewFile("gameplay/patrol.go", true); err != nil {
		t.Fatalf("confirm patrol.go: %v", err)
	}
	if err := f.ed.ConfirmCodeReviewFile("gameplay/patrol_test.go", true); err != nil {
		t.Fatalf("confirm patrol_test.go: %v", err)
	}
	if err := f.ed.KeepCodeReview(); err != nil {
		t.Fatalf("KeepCodeReview: %v", err)
	}

	gotPatrol, _ = os.ReadFile(filepath.Join(f.dir, "gameplay/patrol.go"))
	gotTest, _ := os.ReadFile(filepath.Join(f.dir, "gameplay/patrol_test.go"))
	gotPrefab, _ := os.ReadFile(filepath.Join(f.dir, "prefabs/slime.prefab.yaml"))
	if string(gotPatrol) != mcpPatrolFixed || string(gotTest) != mcpPatrolTest || string(gotPrefab) != mcpPrefabNext {
		t.Fatal("KeepCodeReview did not land fixed Go + test + prefab on disk")
	}

	if !f.ed.Undo() {
		t.Fatal("Undo returned false")
	}
	gotPatrol, _ = os.ReadFile(filepath.Join(f.dir, "gameplay/patrol.go"))
	gotPrefab, _ = os.ReadFile(filepath.Join(f.dir, "prefabs/slime.prefab.yaml"))
	if string(gotPatrol) != mcpPatrolOrig || string(gotPrefab) != mcpPrefabOrig {
		t.Fatal("one Undo did not restore original patrol.go and slime.prefab.yaml")
	}
	if _, err := os.Stat(filepath.Join(f.dir, "gameplay/patrol_test.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("one Undo did not remove patrol_test.go: %v", err)
	}
}

func TestCodeReviewMCPPermissionsAndYouOnlyBoundary(t *testing.T) {
	f := openFixture(t)
	if err := os.MkdirAll(filepath.Join(f.dir, "gameplay"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "gameplay/patrol.go"), []byte(mcpPatrolOrig), 0o644); err != nil {
		t.Fatal(err)
	}
	f.ed.CodeReviewCheckRunner = func(_ context.Context, req editor.CodeReviewCheckRequest, _ func(editor.CodeReviewCheckProgress)) (editor.CodeReviewCheckResult, error) {
		return editor.CodeReviewCheckResult{
			Phase:       editor.CodeReviewChecksPassed,
			CheckPassed: true,
			TestPassed:  true,
			PassedCount: 1,
			TotalCount:  1,
			Packages:    req.Packages,
		}, nil
	}

	// Default Balanced profile without OnPermissionAsk:
	// 1. Proposing Go code must hit Code **/*.go -> Ask (not silently fall back to scenes/**).
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	askRes := call(t, cs, editormcp.ToolCodeReviewPropose, map[string]any{
		"files": []map[string]any{
			{"path": "gameplay/patrol.go", "content": mcpPatrolFixed},
		},
	})
	if !askRes.IsError || !strings.Contains(text(askRes), "permission required") {
		t.Fatalf("expected Balanced profile to ask on Go code_review_propose, got isErr=%v text=%q", askRes.IsError, text(askRes))
	}

	// 2. Proposing a deletion must hit Delete ** -> Deny.
	delRes := call(t, cs, editormcp.ToolCodeReviewPropose, map[string]any{
		"files": []map[string]any{
			{"path": "gameplay/patrol.go", "delete": true},
		},
	})
	if !delRes.IsError || !strings.Contains(text(delRes), "permission denied") {
		t.Fatalf("expected Balanced profile to deny delete in code_review_propose, got isErr=%v text=%q", delRes.IsError, text(delRes))
	}

	// 3. Under Autonomous, You-only files and .git metadata must STILL be refused, and delete remains denied.
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileAutonomous); err != nil {
		t.Fatalf("SelectPermissionProfile(Autonomous): %v", err)
	}
	for _, target := range []string{"permissions.yaml", "tools.yaml", ".nsgo/permissions.yaml"} {
		res := call(t, cs, editormcp.ToolCodeReviewPropose, map[string]any{
			"files": []map[string]any{
				{"path": target, "content": "version: 1\n"},
			},
		})
		if !res.IsError || !strings.Contains(text(res), "You only") {
			t.Fatalf("expected You-only refusal for %q under Autonomous, got isErr=%v text=%q", target, res.IsError, text(res))
		}
	}
	gitRes := call(t, cs, editormcp.ToolCodeReviewPropose, map[string]any{
		"files": []map[string]any{
			{"path": ".git/hooks/pre-commit", "content": "#!/bin/sh\necho pwn\n"},
		},
	})
	if !gitRes.IsError {
		t.Fatalf("expected .git path to be refused in code_review_propose, got %q", text(gitRes))
	}

	// 4. Default PrivacyGoSource=false refuses code_review_* even under Autonomous until the person enables Go source.
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{editormcp.ToolCodeReviewPropose, map[string]any{"files": []map[string]any{{"path": "gameplay/patrol.go", "content": mcpPatrolFixed}}}},
		{editormcp.ToolCodeReviewStatus, map[string]any{}},
		{editormcp.ToolCodeReviewRunChecks, map[string]any{}},
	} {
		privRes := call(t, cs, tc.tool, tc.args)
		if !privRes.IsError || !strings.Contains(text(privRes), "Go source") {
			t.Fatalf("expected PrivacyGoSource=false to refuse %s, got isErr=%v text=%q", tc.tool, privRes.IsError, text(privRes))
		}
	}

	// 5. After enabling Go source privacy under Autonomous, proposing Go code succeeds; denying Run game check blocks code_review_run_checks.
	if err := f.ed.SetAgentPrivacy(editor.PrivacyGoSource, true); err != nil {
		t.Fatalf("SetAgentPrivacy(GoSource): %v", err)
	}
	okRes := call(t, cs, editormcp.ToolCodeReviewPropose, map[string]any{
		"files": []map[string]any{
			{"path": "gameplay/patrol.go", "content": mcpPatrolFixed},
		},
	})
	_ = mustOK(t, okRes)

	perms := f.ed.Permissions()
	perms.Rules = append([]editor.PermissionRule{{
		Action:   editor.PermissionActionRun,
		Patterns: []string{editor.PermissionRunGameCheck},
		Decision: editor.PermissionDeny,
	}}, perms.Rules...)
	if err := f.ed.SetPermissions(perms); err != nil {
		t.Fatalf("SetPermissions: %v", err)
	}
	denyRun := call(t, cs, editormcp.ToolCodeReviewRunChecks, map[string]any{})
	if !denyRun.IsError || !strings.Contains(text(denyRun), "permission denied") {
		t.Fatalf("expected Run game check deny rule to block code_review_run_checks, got isErr=%v text=%q", denyRun.IsError, text(denyRun))
	}
}
