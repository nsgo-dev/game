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
    yaml_valid: 15
    project_check: 15
    domain_check: 25
    build_check: 15
    mcp_verify: 30
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
	badWeights = bytes.Replace(badWeights, []byte("mcp_verify: 30"), []byte("mcp_verify: 15"), 1)
	if _, err := ParseTask(badWeights); err == nil {
		t.Fatal("ParseTask accepted weights summing to 85, want error")
	}

	// Path traversal, absolute paths, and unsafe task IDs must be refused.
	validBase := bytes.Replace(badUnknown, []byte("extra_unknown_field: true\n"), nil, 1)
	if _, err := ParseTask(validBase); err != nil {
		t.Fatalf("ParseTask(valid base): %v", err)
	}
	for name, mutated := range map[string][]byte{
		"traversal_starter":   bytes.Replace(validBase, []byte("starter_dir: a"), []byte("starter_dir: ../outside"), 1),
		"abs_context_doc":     bytes.Replace(validBase, []byte("docs/en/mcp.md"), []byte("/etc/passwd"), 1),
		"traversal_scene":     bytes.Replace(validBase, []byte("scenes/menu.scene.yaml"), []byte("../../escape.scene.yaml"), 1),
		"unsafe_task_id":      bytes.Replace(validBase, []byte("id: bad"), []byte("id: ../bad"), 1),
		"moved_without_step":  bytes.Replace(validBase, []byte("min_scenes: 1"), []byte("min_scenes: 1\n    expect_moved_entity: player"), 1),
		"comp_without_moved":  bytes.Replace(validBase, []byte("min_scenes: 1"), []byte("min_scenes: 1\n    expect_component: enemy"), 1),
		"min_score_negative":  bytes.Replace(validBase, []byte("prompt: Bad"), []byte("prompt: Bad\nmin_score: -1"), 1),
		"press_without_step":  bytes.Replace(validBase, []byte("min_scenes: 1"), []byte("min_scenes: 1\n    press_actions: [right]"), 1),
		"click_without_scene": bytes.Replace(validBase, []byte("min_scenes: 1"), []byte("min_scenes: 1\n    click: {x: 1, y: 2}"), 1),
		"click_nan":           bytes.Replace(validBase, []byte("min_scenes: 1"), []byte("min_scenes: 1\n    click: {x: .nan, y: 2, expect_scene: a}"), 1),
	} {
		if _, err := ParseTask(mutated); err == nil {
			t.Errorf("ParseTask(%s) accepted invalid spec, want error", name)
		}
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
	if rep.Score != 75 {
		t.Fatalf("broken platform gap score = %d, want 75 (lost 25-point domain_check); criteria=%+v", rep.Score, rep.Criteria)
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

	// The mock agent gets no path into the repository: it checks the prompt, the staged docs, the staged Agent Skill, and the MCP config, copies in the reference solution (whose path only the test knows), and builds it itself, which needs the go.work the runner sets up before the agent runs.
	ref := filepath.Join(root, "tools", "agenteval", "testdata", "fixtures", "add-menu", "reference")
	mockCmd := `test -z "$AGENTEVAL_REPO_ROOT" && test -s "$AGENTEVAL_PROMPT_FILE" && test -s "$AGENTEVAL_CONTEXT_DIR/docs/en/mcp.md" && test -s "$AGENTEVAL_SKILL_FILE" && test -s "$AGENTEVAL_SKILL_DIR/references/mcp-tools.md" && test -s "$AGENTEVAL_MCP_CONFIG" && test -n "$GOWORK" && cp -R '` + ref + `/.' . && go tool nsgo check`
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

func TestPlatformerReachabilityEdgeCases(t *testing.T) {
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

	// Extra decorative platforms far left of player or far right of goal must not cause false failures.
	wsExtra := filepath.Join(t.TempDir(), "extra-platforms")
	if err := copyDir(filepath.Join(root, filepath.FromSlash(plat.ReferenceDir)), wsExtra); err != nil {
		t.Fatal(err)
	}
	scenePath := filepath.Join(wsExtra, "scenes", "level1.scene.yaml")
	data, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	extraPlatforms := strings.Replace(string(data), "      - id: platform_start", `      - id: platform_left_bg
        prefab: platform
        x: -400
        y: 144
        properties:
          height: 16
          width: 64
      - id: platform_right_bg
        prefab: platform
        x: 700
        y: 96
        properties:
          height: 16
          width: 64
      - id: platform_start`, 1)
	if err := os.WriteFile(scenePath, []byte(extraPlatforms), 0o644); err != nil {
		t.Fatal(err)
	}
	res := gradePlatformerLevel(wsExtra, plat.Checks.PlatformerLevel, 25)
	if !res.Passed {
		t.Fatalf("decorative platforms outside [player, goal] falsely failed reachability: %+v", res)
	}

	// Removing platform_goal leaves goal unsupported (x=344 past reach=288) and must fail.
	wsUnsupported := filepath.Join(t.TempDir(), "unsupported-goal")
	if err := copyDir(filepath.Join(root, filepath.FromSlash(plat.ReferenceDir)), wsUnsupported); err != nil {
		t.Fatal(err)
	}
	unsupported := strings.Split(string(data), "      - id: platform_goal")[0] + "  - name: actors" + strings.Split(string(data), "  - name: actors")[1]
	if err := os.WriteFile(filepath.Join(wsUnsupported, "scenes", "level1.scene.yaml"), []byte(unsupported), 0o644); err != nil {
		t.Fatal(err)
	}
	res2 := gradePlatformerLevel(wsUnsupported, plat.Checks.PlatformerLevel, 25)
	if res2.Passed {
		t.Fatalf("unsupported goal unexpectedly passed reachability: %+v", res2)
	}
}

func TestTaskMinScoreZeroIsKept(t *testing.T) {
	root := testRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "tools", "agenteval", "tasks", "add-menu.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	zero := bytes.Replace(data, []byte("min_score: 100"), []byte("min_score: 0"), 1)
	task, err := ParseTask(zero)
	if err != nil {
		t.Fatalf("ParseTask(min_score: 0): %v", err)
	}
	if got := task.EffectiveMinScore(100); got != 0 {
		t.Fatalf("EffectiveMinScore with min_score: 0 = %d, want 0", got)
	}
	enc, err := EncodeTask(task)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(enc, []byte("min_score: 0")) {
		t.Fatalf("EncodeTask dropped min_score: 0:\n%s", enc)
	}
	unset := bytes.Replace(data, []byte("min_score: 100\n"), nil, 1)
	task, err = ParseTask(unset)
	if err != nil {
		t.Fatal(err)
	}
	if got := task.EffectiveMinScore(100); got != 100 {
		t.Fatalf("EffectiveMinScore with no min_score = %d, want the fallback 100", got)
	}
}

func TestPlatformerSpecRefusesNonFiniteBounds(t *testing.T) {
	root := testRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "tools", "agenteval", "tasks", "platformer-level.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"max_gap: .nan", "max_gap: .inf", "max_gap: 0", "max_rise: .nan", "max_rise: -.inf", "max_rise: 0"} {
		key := strings.SplitN(bad, ":", 2)[0]
		mutated := regexpReplaceLine(data, key, bad)
		if _, err := ParseTask(mutated); err == nil {
			t.Errorf("ParseTask accepted %q, want error", bad)
		}
	}
}

func regexpReplaceLine(data []byte, key, line string) []byte {
	var out []string
	for l := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(l) != "" && strings.HasPrefix(strings.TrimSpace(l), key+":") {
			indent := l[:len(l)-len(strings.TrimLeft(l, " "))]
			l = indent + line
		}
		out = append(out, l)
	}
	return []byte(strings.Join(out, "\n"))
}

func TestPlatformerReachabilityBoundsVerticalSteps(t *testing.T) {
	root := testRepoRoot(t)
	plat := loadTask(t, root, "platformer-level")
	ws := filepath.Join(t.TempDir(), "tall-step")
	if err := copyDir(filepath.Join(root, filepath.FromSlash(plat.ReferenceDir)), ws); err != nil {
		t.Fatal(err)
	}
	scenePath := filepath.Join(ws, "scenes", "level1.scene.yaml")
	data, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	// platform_mid2 at y 112 lifted to y -88 is a 216-point climb from platform_mid1.
	tall := strings.Replace(string(data), "y: 112", "y: -88", 1)
	if err := os.WriteFile(scenePath, []byte(tall), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := gradePlatformerLevel(ws, plat.Checks.PlatformerLevel, 25); res.Passed {
		t.Fatalf("a 216-point vertical step passed reachability: %+v", res)
	}
}

func TestMCPVerifyRequiresTheTestToPass(t *testing.T) {
	root := testRepoRoot(t)
	task := loadTask(t, root, "add-menu")
	ws := filepath.Join(t.TempDir(), "bypass")
	if err := copyDir(filepath.Join(root, filepath.FromSlash(task.ReferenceDir)), ws); err != nil {
		t.Fatal(err)
	}
	// A TestMain that exits 0 without m.Run must not earn the MCP points.
	bypass := "package main\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestMain(m *testing.M) { os.Exit(0) }\n"
	if err := os.WriteFile(filepath.Join(ws, "bypass_test.go"), []byte(bypass), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	rep := GradeWorkspace(ctx, root, ws, task, 100)
	for _, c := range rep.Criteria {
		if c.Name == "mcp_verify_passes" && c.Passed {
			t.Fatalf("mcp_verify_passes passed with a TestMain that skips the tests: %+v", c)
		}
	}
}

func TestCommandModeAgentTimeoutIsReported(t *testing.T) {
	root := testRepoRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	report, err := RunSuite(ctx, RunOptions{
		RepoRoot:         root,
		TasksDir:         filepath.Join(root, "tools", "agenteval", "tasks"),
		Mode:             ModeCommand,
		AgentCmd:         "sleep 30",
		TaskFilter:       []string{"add-menu"},
		MinScoreOverride: -1,
		Timeout:          2 * time.Minute,
		AgentTimeout:     time.Second,
	})
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	tr := report.Tasks[0]
	if !tr.AgentTimedOut || !strings.Contains(tr.AgentOutput, "timed out") {
		t.Fatalf("agent timeout not recorded: timed_out=%v output=%q", tr.AgentTimedOut, tr.AgentOutput)
	}
	// The starter still builds under the grader's own budget, so the timeout does not turn into a build failure.
	for _, c := range tr.Criteria {
		if c.Name == "builds_cleanly" && !c.Passed {
			t.Fatalf("builds_cleanly failed after an agent timeout: %+v", c)
		}
	}
}

func loadTask(t *testing.T, root, id string) *Task {
	t.Helper()
	tasks, err := LoadTasksDir(filepath.Join(root, "tools", "agenteval", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("task %s not found", id)
	return nil
}
