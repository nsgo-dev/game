package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// ModeReference grades each task's reference workspace deterministically without calling an external LLM.
	ModeReference = "reference"
	// ModeCommand stages each task's starter workspace, invokes an external agent command, and grades its output.
	ModeCommand = "command"
)

// RunOptions configures a suite execution.
type RunOptions struct {
	RepoRoot         string
	TasksDir         string
	Mode             string
	AgentCmd         string
	TaskFilter       []string
	MinScoreOverride int // < 0 uses each task's min_score (or 100 in reference mode).
	Timeout          time.Duration
}

// RunSuite loads tasks, executes each selected task in an isolated temporary workspace, and returns the SuiteReport.
func RunSuite(ctx context.Context, opts RunOptions) (SuiteReport, error) {
	if opts.Mode == "" {
		opts.Mode = ModeReference
	}
	switch opts.Mode {
	case ModeReference:
	case ModeCommand:
		if strings.TrimSpace(opts.AgentCmd) == "" {
			return SuiteReport{}, errors.New("mode=command requires -agent-cmd")
		}
	default:
		return SuiteReport{}, fmt.Errorf("unknown mode %q (want %q or %q)", opts.Mode, ModeReference, ModeCommand)
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Minute
	}

	tasks, err := LoadTasksDir(opts.TasksDir)
	if err != nil {
		return SuiteReport{}, err
	}

	selected, err := filterTasks(tasks, opts.TaskFilter)
	if err != nil {
		return SuiteReport{}, err
	}

	results := make([]TaskReport, 0, len(selected))
	for _, task := range selected {
		rep, err := runSingleTask(ctx, opts, task)
		if err != nil {
			return SuiteReport{}, fmt.Errorf("task %s: %w", task.ID, err)
		}
		results = append(results, rep)
	}
	return BuildSuiteReport(opts.Mode, results), nil
}

func filterTasks(tasks []*Task, filter []string) ([]*Task, error) {
	if len(filter) == 0 {
		return tasks, nil
	}
	wanted := make(map[string]bool, len(filter))
	for _, id := range filter {
		id = strings.TrimSpace(id)
		if id != "" {
			wanted[id] = true
		}
	}
	if len(wanted) == 0 {
		return tasks, nil
	}
	var out []*Task
	found := make(map[string]bool, len(wanted))
	for _, t := range tasks {
		if wanted[t.ID] {
			out = append(out, t)
			found[t.ID] = true
		}
	}
	var missing []string
	for id := range wanted {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return nil, fmt.Errorf("unknown task id(s): %s", strings.Join(missing, ", "))
	}
	return out, nil
}

func runSingleTask(ctx context.Context, opts RunOptions, task *Task) (TaskReport, error) {
	start := time.Now()
	taskCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	stageRoot, err := os.MkdirTemp("", "agenteval-"+task.ID+"-*")
	if err != nil {
		return TaskReport{}, fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(stageRoot)

	workspaceDir := filepath.Join(stageRoot, "workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return TaskReport{}, fmt.Errorf("create workspace dir: %w", err)
	}

	var srcDir string
	if opts.Mode == ModeReference {
		srcDir = filepath.Join(opts.RepoRoot, filepath.FromSlash(task.ReferenceDir))
	} else {
		srcDir = filepath.Join(opts.RepoRoot, filepath.FromSlash(task.StarterDir))
	}
	if err := copyDir(srcDir, workspaceDir); err != nil {
		return TaskReport{}, fmt.Errorf("stage fixture %s: %w", srcDir, err)
	}

	var agentOut string
	if opts.Mode == ModeCommand {
		promptPath := filepath.Join(stageRoot, "prompt.md")
		if err := os.WriteFile(promptPath, []byte(renderAgentPrompt(opts.RepoRoot, task)), 0o644); err != nil {
			return TaskReport{}, fmt.Errorf("write prompt file: %w", err)
		}
		out, runErr := runAgentCommand(taskCtx, opts.RepoRoot, workspaceDir, promptPath, opts.AgentCmd, task)
		agentOut = out
		if runErr != nil {
			agentOut = strings.TrimSpace(agentOut + "\n[agent command error: " + runErr.Error() + "]")
		}
	}

	minScore := task.EffectiveMinScore(100)
	if opts.MinScoreOverride >= 0 {
		minScore = opts.MinScoreOverride
	}

	rep := GradeWorkspace(taskCtx, opts.RepoRoot, workspaceDir, task, minScore)
	rep.DurationMS = time.Since(start).Milliseconds()
	rep.AgentOutput = agentOut
	return rep, nil
}

func renderAgentPrompt(repoRoot string, task *Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Task: %s (`%s`)\n\n", task.Title, task.ID)
	b.WriteString(strings.TrimSpace(task.Prompt))
	b.WriteString("\n\n## Required Documentation Context\n\n")
	for _, doc := range task.ContextDocs {
		fmt.Fprintf(&b, "- `%s` (`%s`)\n", doc, filepath.Join(repoRoot, filepath.FromSlash(doc)))
	}
	return b.String()
}

func runAgentCommand(ctx context.Context, repoRoot, workspaceDir, promptPath, agentCmd string, task *Task) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", agentCmd)
	cmd.Dir = workspaceDir
	env := os.Environ()
	env = append(env,
		"AGENTEVAL_TASK_ID="+task.ID,
		"AGENTEVAL_TASK_TITLE="+task.Title,
		"AGENTEVAL_PROMPT_FILE="+promptPath,
		"AGENTEVAL_WORKSPACE="+workspaceDir,
		"AGENTEVAL_REPO_ROOT="+repoRoot,
		"AGENTEVAL_DOCS="+strings.Join(task.ContextDocs, ","),
	)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}
