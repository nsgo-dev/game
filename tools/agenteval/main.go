// Command agenteval runs fixed evaluation tasks for the game layer: in reference mode it grades the hand-written reference solutions as an API regression suite, in command mode it runs an external agent command on each task's starter workspace, and in agent mode it runs a built-in tool-calling coding agent against the staged docs, skill, and MCP config, reporting a score (0..100) per task in Markdown and optional JSON.
//
//	go run ./tools/agenteval
//	go run ./tools/agenteval -out report.json -markdown-out summary.md -summary "$GITHUB_STEP_SUMMARY"
//	go run ./tools/agenteval -mode=command -agent-cmd='my-agent --prompt "$AGENTEVAL_PROMPT_FILE"'
//	go run ./tools/agenteval -mode=agent -agent-provider=auto -skip-if-unconfigured
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if err := runCLI(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCLI(args []string) error {
	fs := flag.NewFlagSet("agenteval", flag.ContinueOnError)
	var (
		mode               = fs.String("mode", ModeReference, "evaluation mode: reference, command, or agent")
		agentCmd           = fs.String("agent-cmd", "", "shell command executed per task when -mode=command (defaults to $AGENT_EVAL_COMMAND)")
		agentProvider      = fs.String("agent-provider", "", "LLM provider for -mode=agent: auto, anthropic, or openai (defaults to $AGENT_EVAL_PROVIDER or auto)")
		agentModel         = fs.String("agent-model", "", "model name for -mode=agent (defaults to $AGENT_EVAL_MODEL or provider default)")
		agentBaseURL       = fs.String("agent-base-url", "", "API base URL override for -mode=agent (defaults to $AGENT_EVAL_BASE_URL)")
		agentMaxTurns      = fs.Int("agent-max-turns", 0, "maximum model turns per task in -mode=agent (0 uses $AGENT_EVAL_MAX_TURNS or 25)")
		skipIfUnconfigured = fs.Bool("skip-if-unconfigured", false, "write a skipped report and exit 0 when -mode=agent or -mode=command lacks credentials or a command")
		tasksDir           = fs.String("tasks-dir", "", "directory containing versioned task YAML files (defaults to tools/agenteval/tasks)")
		taskFilter         = fs.String("task", "", "comma-separated list of task IDs to run (default: all)")
		minScore           = fs.Int("min-score", -1, "override minimum passing score per task (0..100; -1 uses each task's threshold)")
		outPath            = fs.String("out", "", "optional path to write machine-readable JSON report")
		markdownOut        = fs.String("markdown-out", "", "optional path to write standalone Markdown report")
		summaryPath        = fs.String("summary", "", "optional path to append Markdown summary (defaults to $GITHUB_STEP_SUMMARY when set)")
		timeout            = fs.Duration("timeout", 5*time.Minute, "per-task grading timeout (nsgo check and the MCP verification test)")
		agentTimeout       = fs.Duration("agent-timeout", 15*time.Minute, "per-task agent budget in -mode=command or -mode=agent, separate from -timeout")
		trials             = fs.Int("trials", 1, "independent trials per task")
		comparePath        = fs.String("compare", "", "optional previous JSON report to compare criterion pass states")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("agenteval: unexpected positional argument(s): %s", strings.Join(fs.Args(), " "))
	}
	if *minScore < -1 || *minScore > 100 {
		return fmt.Errorf("agenteval: -min-score must be between -1 and 100 (got %d)", *minScore)
	}
	if *agentMaxTurns < 0 {
		return fmt.Errorf("agenteval: -agent-max-turns must be non-negative (got %d)", *agentMaxTurns)
	}
	if *timeout < 0 {
		return fmt.Errorf("agenteval: -timeout must be non-negative (got %s)", *timeout)
	}
	if *agentTimeout < 0 {
		return fmt.Errorf("agenteval: -agent-timeout must be non-negative (got %s)", *agentTimeout)
	}
	if *trials < 1 {
		return fmt.Errorf("agenteval: -trials must be at least 1 (got %d)", *trials)
	}

	root, err := findRepoRoot()
	if err != nil {
		return err
	}
	resolvedTasks := *tasksDir
	if resolvedTasks == "" {
		resolvedTasks = filepath.Join(root, "tools", "agenteval", "tasks")
	}

	var filter []string
	if strings.TrimSpace(*taskFilter) != "" {
		filter = strings.Split(*taskFilter, ",")
	}

	report, err := RunSuite(context.Background(), RunOptions{
		RepoRoot: root,
		TasksDir: resolvedTasks,
		Mode:     *mode,
		AgentCmd: *agentCmd,
		Agent: AgentConfig{
			Provider: *agentProvider,
			Model:    *agentModel,
			BaseURL:  *agentBaseURL,
			MaxTurns: *agentMaxTurns,
		},
		SkipIfUnconfigured: *skipIfUnconfigured,
		TaskFilter:         filter,
		MinScoreOverride:   *minScore,
		Timeout:            *timeout,
		AgentTimeout:       *agentTimeout,
		Trials:             *trials,
	})
	if err != nil {
		return err
	}

	md := report.RenderMarkdown()
	fmt.Print(md)

	if *outPath != "" {
		if err := report.WriteJSON(*outPath); err != nil {
			return err
		}
	}
	if *markdownOut != "" {
		if err := report.WriteMarkdown(*markdownOut); err != nil {
			return err
		}
	}
	if *comparePath != "" {
		data, err := os.ReadFile(*comparePath)
		if err != nil {
			return fmt.Errorf("read comparison report: %w", err)
		}
		var previous SuiteReport
		if err := json.Unmarshal(data, &previous); err != nil {
			return fmt.Errorf("decode comparison report: %w", err)
		}
		for _, change := range CompareSuiteReports(report, previous) {
			fmt.Printf("criterion changed: %s/%s %t -> %t\n", change.TaskID, change.Criterion, change.PreviousPassed, change.CurrentPassed)
		}
	}

	summary := *summaryPath
	if summary == "" {
		summary = os.Getenv("GITHUB_STEP_SUMMARY")
	}
	if summary != "" {
		if err := appendSummary(summary, md); err != nil {
			return err
		}
	}

	if !report.Passed {
		return fmt.Errorf("agenteval: %d of %d tasks passed (average %.1f/100)", report.PassedTasks, report.TotalTasks, report.AverageScore)
	}
	return nil
}

func appendSummary(path, content string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open summary %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(content + "\n"); err != nil {
		return fmt.Errorf("write summary %s: %w", path, err)
	}
	return nil
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("agenteval: could not locate repository root containing go.work and go.mod")
		}
		dir = parent
	}
}
