package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.work")); err != nil {
		t.Fatalf("expected repo root at %s: %v", root, err)
	}
	return root
}

func TestTaskYAMLParsingAndRoundTrip(t *testing.T) {
	root := testRepoRoot(t)
	tasksDir := filepath.Join(root, "tools", "agenteval", "tasks")
	tasks, err := LoadTasksDir(tasksDir)
	if err != nil {
		t.Fatalf("LoadTasksDir: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("loaded %d tasks, want 3", len(tasks))
	}

	for _, task := range tasks {
		enc1, err := EncodeTask(task)
		if err != nil {
			t.Fatalf("EncodeTask(%s): %v", task.ID, err)
		}
		reparsed, err := ParseTask(enc1)
		if err != nil {
			t.Fatalf("re-ParseTask(%s): %v", task.ID, err)
		}
		enc2, err := EncodeTask(reparsed)
		if err != nil {
			t.Fatalf("re-EncodeTask(%s): %v", task.ID, err)
		}
		if !bytes.Equal(enc1, enc2) {
			t.Fatalf("task %s round-trip was not deterministic:\n--- first ---\n%s\n--- second ---\n%s", task.ID, enc1, enc2)
		}
	}

	// Unknown fields must be refused.
	badUnknown := []byte(`
version: 1
id: bad
title: Bad
description: Bad
prompt: Bad
extra_unknown_field: true
context_docs: [docs/en/mcp.md]
starter_dir: a
reference_dir: b
checks:
  yaml_files: [game.yaml]
  menu_spec:
    scene_file: scenes/menu.scene.yaml
    min_menu_items: 1
    require_screen_layer: true
  mcp_verify:
    min_scenes: 1
  weights:
    docs_context: 10
    yaml_valid: 15
    project_check: 15
    domain_check: 20
    build_check: 15
    mcp_verify: 25
`)
	if _, err := ParseTask(badUnknown); err == nil {
		t.Fatal("ParseTask accepted unknown field, want error")
	}

	// Unsupported version must be refused.
	badVersion := bytes.Replace(badUnknown, []byte("extra_unknown_field: true\n"), nil, 1)
	badVersion = bytes.Replace(badVersion, []byte("version: 1"), []byte("version: 99"), 1)
	if _, err := ParseTask(badVersion); err == nil {
		t.Fatal("ParseTask accepted unsupported version 99, want error")
	}

	// Weights not summing to 100 must be refused.
	badWeights := bytes.Replace(badUnknown, []byte("extra_unknown_field: true\n"), nil, 1)
	badWeights = bytes.Replace(badWeights, []byte("mcp_verify: 25"), []byte("mcp_verify: 10"), 1)
	if _, err := ParseTask(badWeights); err == nil {
		t.Fatal("ParseTask accepted weights summing to 85, want error")
	}
}

func TestReferenceSuitePassesAllTasks(t *testing.T) {
	root := testRepoRoot(t)
	tasksDir := filepath.Join(root, "tools", "agenteval", "tasks")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	report, err := RunSuite(ctx, RunOptions{
		RepoRoot:         root,
		TasksDir:         tasksDir,
		Mode:             ModeReference,
		MinScoreOverride: -1,
		Timeout:          2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("RunSuite(reference): %v", err)
	}
	if !report.Passed {
		t.Fatalf("expected reference suite to pass:\n%s", report.RenderMarkdown())
	}
	if report.TotalTasks != 3 || report.PassedTasks != 3 {
		t.Fatalf("passed %d/%d tasks, want 3/3", report.PassedTasks, report.TotalTasks)
	}
	if report.AverageScore != 100 {
		t.Fatalf("average score = %.1f, want 100.0", report.AverageScore)
	}
	for _, tr := range report.Tasks {
		if tr.Score != 100 || !tr.Passed {
			t.Errorf("task %s got %d/%d (passed=%v), want 100/100\n%s", tr.ID, tr.Score, tr.MaxScore, tr.Passed, report.RenderMarkdown())
		}
		for _, c := range tr.Criteria {
			if !c.Passed || c.Earned != c.Weight {
				t.Errorf("task %s criterion %s failed: %+v", tr.ID, c.Name, c)
			}
		}
	}

	md := report.RenderMarkdown()
	for _, id := range []string{"platformer-level", "add-enemy-type", "add-menu"} {
		if !strings.Contains(md, "`"+id+"`") {
			t.Errorf("markdown report missing task %q:\n%s", id, md)
		}
	}

	outPath := filepath.Join(t.TempDir(), "report.json")
	if err := report.WriteJSON(outPath); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SuiteReport
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode JSON report: %v", err)
	}
	if decoded.PassedTasks != 3 || decoded.AverageScore != 100 {
		t.Fatalf("decoded JSON report mismatch: %+v", decoded)
	}
}

func TestStarterFixturesCatchRegressionsWithPartialCredit(t *testing.T) {
	root := testRepoRoot(t)
	tasks, err := LoadTasksDir(filepath.Join(root, "tools", "agenteval", "tasks"))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for _, task := range tasks {
		ws := filepath.Join(t.TempDir(), task.ID)
		if err := copyDir(filepath.Join(root, filepath.FromSlash(task.StarterDir)), ws); err != nil {
			t.Fatal(err)
		}
		rep := GradeWorkspace(ctx, root, ws, task, 100)
		if rep.Passed {
			t.Fatalf("starter workspace for %s unexpectedly passed with %d/100", task.ID, rep.Score)
		}
		if rep.Score == 0 || rep.Score >= 100 {
			t.Fatalf("starter workspace for %s score = %d, want partial credit in (0, 100)", task.ID, rep.Score)
		}
	}
}

func TestBrokenLevelGeometryAndUnknownYAMLFieldDropScore(t *testing.T) {
	root := testRepoRoot(t)
	tasks, err := LoadTasksDir(filepath.Join(root, "tools", "agenteval", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	var plat *Task
	for _, task := range tasks {
		if task.ID == "platformer-level" {
			plat = task
		}
	}
	if plat == nil {
		t.Fatal("platformer-level task not found")
	}

	ws := filepath.Join(t.TempDir(), "broken-gap")
	if err := copyDir(filepath.Join(root, filepath.FromSlash(plat.ReferenceDir)), ws); err != nil {
		t.Fatal(err)
	}
	// Replace scene with an unjumpable horizontal gap (> 64 points) between platforms.
	scenePath := filepath.Join(ws, "scenes", "level1.scene.yaml")
	data, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(data), "x: 304", "x: 420", 1)
	if err := os.WriteFile(scenePath, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	rep := GradeWorkspace(ctx, root, ws, plat, 100)
	if rep.Passed {
		t.Fatalf("broken platform gap unexpectedly passed: %+v", rep)
	}
	if rep.Score != 80 {
		t.Fatalf("broken platform gap score = %d, want 80 (lost 20-point domain_check); criteria=%+v", rep.Score, rep.Criteria)
	}

	// Also break game.yaml with an unknown field -> yaml_valid & project_check_passes must fail.
	gameYAML := filepath.Join(ws, "game.yaml")
	if err := os.WriteFile(gameYAML, []byte("version: 1\nname: Broken\nid: dev.nsgo.bad\nunknownField: 123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep2 := GradeWorkspace(ctx, root, ws, plat, 100)
	if rep2.Score >= rep.Score {
		t.Fatalf("broken game.yaml did not reduce score further: before=%d after=%d", rep.Score, rep2.Score)
	}
}

func TestCommandModeWithMockAgent(t *testing.T) {
	root := testRepoRoot(t)
	tasksDir := filepath.Join(root, "tools", "agenteval", "tasks")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Mock external agent command checks env vars & prompt file, then copies the reference solution into $AGENTEVAL_WORKSPACE.
	mockCmd := `test -s "$AGENTEVAL_PROMPT_FILE" && test -n "$AGENTEVAL_DOCS" && cp -R "$AGENTEVAL_REPO_ROOT/tools/agenteval/testdata/fixtures/add-menu/reference/." "$AGENTEVAL_WORKSPACE/"`
	report, err := RunSuite(ctx, RunOptions{
		RepoRoot:         root,
		TasksDir:         tasksDir,
		Mode:             ModeCommand,
		AgentCmd:         mockCmd,
		TaskFilter:       []string{"add-menu"},
		MinScoreOverride: 100,
		Timeout:          2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("RunSuite(command): %v", err)
	}
	if !report.Passed || report.TotalTasks != 1 || report.Tasks[0].Score != 100 {
		t.Fatalf("mock agent command run failed:\n%s", report.RenderMarkdown())
	}
}
