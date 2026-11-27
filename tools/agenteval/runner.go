package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	// ModeAgent stages each task's starter workspace, runs the built-in tool-calling coding agent against the docs, skill, and MCP, and grades its output.
	ModeAgent = "agent"

	defaultSkillRelDir = "skills/nsgo-game"
)

// RunOptions configures a suite execution.
type RunOptions struct {
	RepoRoot           string
	TasksDir           string
	Mode               string
	AgentCmd           string
	Agent              AgentConfig
	SkipIfUnconfigured bool
	Getenv             func(string) string
	TaskFilter         []string
	MinScoreOverride   int           // < 0 uses each task's min_score (or 100 in reference mode).
	Timeout            time.Duration // grading budget per task (nsgo check and the MCP verification test)
	AgentTimeout       time.Duration // agent budget per task in command/agent mode, separate from Timeout
}

// RunSuite loads tasks, executes each selected task in an isolated temporary workspace, and returns the SuiteReport.
func RunSuite(ctx context.Context, opts RunOptions) (SuiteReport, error) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if opts.Mode == "" {
		opts.Mode = ModeReference
	}
	switch opts.Mode {
	case ModeReference:
	case ModeCommand:
		if strings.TrimSpace(opts.AgentCmd) == "" {
			opts.AgentCmd = strings.TrimSpace(getenv("AGENT_EVAL_COMMAND"))
		}
		if strings.TrimSpace(opts.AgentCmd) == "" {
			if opts.SkipIfUnconfigured {
				return BuildSkippedSuiteReport(ModeCommand, "no agent command configured (set -agent-cmd or AGENT_EVAL_COMMAND)"), nil
			}
			return SuiteReport{}, errors.New("mode=command requires -agent-cmd or AGENT_EVAL_COMMAND")
		}
	case ModeAgent:
		resolved, err := ResolveAgentConfig(opts.Agent, getenv)
		if err != nil {
			if opts.SkipIfUnconfigured && errors.Is(err, ErrAgentUnconfigured) {
				return BuildSkippedSuiteReport(ModeAgent, err.Error()), nil
			}
			return SuiteReport{}, err
		}
		opts.Agent = resolved
	default:
		return SuiteReport{}, fmt.Errorf("unknown mode %q (want %q, %q, or %q)", opts.Mode, ModeReference, ModeCommand, ModeAgent)
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Minute
	}
	if opts.AgentTimeout <= 0 {
		opts.AgentTimeout = 15 * time.Minute
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
	report := BuildSuiteReport(opts.Mode, results)
	if opts.Mode == ModeAgent {
		report.Provider = opts.Agent.Provider
		report.Model = opts.Agent.Model
	}
	return report, nil
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

	stageRoot, err := os.MkdirTemp("", "agenteval-"+task.ID+"-*")
	if err != nil {
		return TaskReport{}, fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(stageRoot)
	if resolved, err := filepath.EvalSymlinks(stageRoot); err == nil {
		stageRoot = resolved
	}

	workspaceDir := filepath.Join(stageRoot, "workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return TaskReport{}, fmt.Errorf("create workspace dir: %w", err)
	}

	var relSrc string
	if opts.Mode == ModeReference {
		relSrc = task.ReferenceDir
	} else {
		relSrc = task.StarterDir
	}
	srcDir, err := safeJoin(opts.RepoRoot, relSrc)
	if err != nil {
		return TaskReport{}, fmt.Errorf("resolve fixture %s: %w", relSrc, err)
	}
	if err := copyDir(srcDir, workspaceDir); err != nil {
		return TaskReport{}, fmt.Errorf("stage fixture %s: %w", srcDir, err)
	}

	// The context docs and agent skill are copied next to the workspace so the agent reads them without access to the repository, whose fixtures hold the reference answers. A missing or empty doc or skill fails the run instead of costing points.
	contextDir := filepath.Join(stageRoot, "context")
	if err := stageContextDocs(opts.RepoRoot, contextDir, task.ContextDocs); err != nil {
		return TaskReport{}, err
	}
	skillDir, err := stageSkill(opts.RepoRoot, contextDir, defaultSkillRelDir)
	if err != nil {
		return TaskReport{}, err
	}

	// The temporary go.work exists before the agent runs, so `go tool nsgo check` and `go tool nsgo test` work for the agent as they do for the grader.
	workFile, cleanupWork, err := prepareGoWorkspace(ctx, opts.RepoRoot, workspaceDir)
	if err != nil {
		return TaskReport{}, fmt.Errorf("prepare go workspace: %w", err)
	}
	defer cleanupWork()

	var (
		agentOut              string
		agentTimedOut         bool
		agentErrMsg           string
		agentTurns            int
		agentToolCalls        int
		agentPromptTokens     int
		agentCompletionTokens int
	)

	if opts.Mode == ModeCommand || opts.Mode == ModeAgent {
		mcpConfigPath := filepath.Join(stageRoot, "mcp.json")
		if err := writeMCPConfig(mcpConfigPath); err != nil {
			return TaskReport{}, fmt.Errorf("write mcp config: %w", err)
		}
		promptText := renderAgentPrompt(contextDir, skillDir, mcpConfigPath, task)
		promptPath := filepath.Join(stageRoot, "prompt.md")
		if err := os.WriteFile(promptPath, []byte(promptText), 0o644); err != nil {
			return TaskReport{}, fmt.Errorf("write prompt file: %w", err)
		}

		agentCtx, cancelAgent := context.WithTimeout(ctx, opts.AgentTimeout)
		switch opts.Mode {
		case ModeCommand:
			out, runErr := runAgentCommand(agentCtx, workspaceDir, workFile, promptPath, contextDir, skillDir, mcpConfigPath, opts.AgentCmd, task)
			agentTimedOut = errors.Is(agentCtx.Err(), context.DeadlineExceeded)
			agentOut = out
			if agentTimedOut {
				agentOut = strings.TrimSpace(agentOut + "\n[agent command timed out after " + opts.AgentTimeout.String() + "]")
			} else if runErr != nil {
				agentErrMsg = runErr.Error()
				agentOut = strings.TrimSpace(agentOut + "\n[agent command error: " + runErr.Error() + "]")
			}
		case ModeAgent:
			res := runBuiltinAgent(agentCtx, opts.Agent, agentEnv{
				workspaceDir:  workspaceDir,
				contextDir:    contextDir,
				skillDir:      skillDir,
				mcpConfigPath: mcpConfigPath,
				workFile:      workFile,
				promptPath:    promptPath,
				task:          task,
				cmdTimeout:    opts.Agent.CommandTimeout,
			}, promptText)
			agentTimedOut = errors.Is(agentCtx.Err(), context.DeadlineExceeded)
			agentTurns = res.Turns
			agentToolCalls = res.ToolCalls
			agentPromptTokens = res.PromptTokens
			agentCompletionTokens = res.CompletionTokens
			agentOut = res.Output
			if agentTimedOut {
				agentOut = strings.TrimSpace(agentOut + "\n[agent timed out after " + opts.AgentTimeout.String() + "]")
			} else if res.Err != nil {
				agentErrMsg = res.Err.Error()
				agentOut = strings.TrimSpace(agentOut + "\n[agent error: " + res.Err.Error() + "]")
			}
		}
		cancelAgent()
	}

	minScore := task.EffectiveMinScore(100)
	if opts.MinScoreOverride >= 0 {
		minScore = opts.MinScoreOverride
	}

	// The agent may have rewritten go.mod, so pin the root game module to this checkout again before grading.
	if opts.Mode == ModeCommand || opts.Mode == ModeAgent {
		if err := pinGamePackage(ctx, opts.RepoRoot, workspaceDir); err != nil {
			return TaskReport{}, fmt.Errorf("re-pin game module after the agent: %w", err)
		}
	}

	// Grading gets its own budget so an agent that used most of its time does not turn the grader's builds into code failures.
	gradeCtx, cancelGrade := context.WithTimeout(ctx, opts.Timeout)
	defer cancelGrade()
	rep := gradeWorkspace(gradeCtx, workspaceDir, workFile, task, minScore)
	rep.DurationMS = time.Since(start).Milliseconds()
	rep.AgentOutput = agentOut
	rep.AgentTimedOut = agentTimedOut
	rep.AgentError = agentErrMsg
	rep.AgentTurns = agentTurns
	rep.AgentToolCalls = agentToolCalls
	rep.AgentPromptTokens = agentPromptTokens
	rep.AgentCompletionTokens = agentCompletionTokens
	return rep, nil
}

// stageContextDocs copies each context doc from repoRoot into dir at the same relative path, failing when one is missing or empty.
func stageContextDocs(repoRoot, dir string, docs []string) error {
	for _, rel := range docs {
		src, err := safeJoin(repoRoot, rel)
		if err != nil {
			return fmt.Errorf("context doc %s: %w", rel, err)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("context doc %s: %w", rel, err)
		}
		if len(bytes.TrimSpace(data)) == 0 {
			return fmt.Errorf("context doc %s is empty", rel)
		}
		dst, err := safeJoin(dir, rel)
		if err != nil {
			return fmt.Errorf("context doc %s: %w", rel, err)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// stageSkill copies the repository's Agent Skill directory into contextDir, failing if SKILL.md is missing or empty.
func stageSkill(repoRoot, contextDir, relSkillDir string) (string, error) {
	src, err := safeJoin(repoRoot, relSkillDir)
	if err != nil {
		return "", fmt.Errorf("agent skill %s: %w", relSkillDir, err)
	}
	skillMD := filepath.Join(src, "SKILL.md")
	data, err := os.ReadFile(skillMD)
	if err != nil {
		return "", fmt.Errorf("agent skill %s/SKILL.md: %w", relSkillDir, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return "", fmt.Errorf("agent skill %s/SKILL.md is empty", relSkillDir)
	}
	dst, err := safeJoin(contextDir, relSkillDir)
	if err != nil {
		return "", fmt.Errorf("agent skill %s: %w", relSkillDir, err)
	}
	if err := copyDir(src, dst); err != nil {
		return "", fmt.Errorf("stage agent skill %s: %w", relSkillDir, err)
	}
	return dst, nil
}

// writeMCPConfig writes a standard MCP client configuration pointing at `go tool nsgo mcp`.
func writeMCPConfig(path string) error {
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"nsgo": map[string]any{
				"command": "go",
				"args":    []string{"tool", "nsgo", "mcp"},
			},
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func renderAgentPrompt(contextDir, skillDir, mcpConfigPath string, task *Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Task: %s (`%s`)\n\n", task.Title, task.ID)
	b.WriteString(strings.TrimSpace(task.Prompt))
	b.WriteString("\n\n## Agent Skill\n\n")
	fmt.Fprintf(&b, "- `skills/nsgo-game/SKILL.md` (`%s`)\n", filepath.Join(skillDir, "SKILL.md"))
	fmt.Fprintf(&b, "- Reference guides under `skills/nsgo-game/references/` (`%s`)\n", filepath.Join(skillDir, "references"))
	b.WriteString("\n## Required Documentation Context\n\n")
	for _, doc := range task.ContextDocs {
		fmt.Fprintf(&b, "- `%s` (`%s`)\n", doc, filepath.Join(contextDir, filepath.FromSlash(doc)))
	}
	b.WriteString("\n## Verification & MCP\n\n")
	b.WriteString("- Run `go tool nsgo check` and `go tool nsgo test ./...` in the workspace to verify your work.\n")
	b.WriteString("- Verify live `game/mcp` tools headlessly with `nsgotest.Start` and `automation.Call`, or attach an MCP client using ")
	fmt.Fprintf(&b, "`%s` (`go tool nsgo mcp`).\n", mcpConfigPath)
	return b.String()
}

func runAgentCommand(ctx context.Context, workspaceDir, workFile, promptPath, contextDir, skillDir, mcpConfigPath, agentCmd string, task *Task) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", agentCmd)
	killProcessGroupOnCancel(cmd)
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = workspaceDir
	cmd.Env = append(cleanGoEnv(workFile),
		"AGENTEVAL_TASK_ID="+task.ID,
		"AGENTEVAL_TASK_TITLE="+task.Title,
		"AGENTEVAL_PROMPT_FILE="+promptPath,
		"AGENTEVAL_WORKSPACE="+workspaceDir,
		"AGENTEVAL_CONTEXT_DIR="+contextDir,
		"AGENTEVAL_SKILL_DIR="+skillDir,
		"AGENTEVAL_SKILL_FILE="+filepath.Join(skillDir, "SKILL.md"),
		"AGENTEVAL_MCP_CONFIG="+mcpConfigPath,
		"AGENTEVAL_DOCS="+strings.Join(task.ContextDocs, ","),
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
