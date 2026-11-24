// Command agenteval runs fixed agent evaluation tasks against NSGo game docs,
// YAML formats, project checks, builds, and live MCP tools, reporting a score
// (0..100) per task in Markdown and optional JSON.
//
//	go run ./tools/agenteval
//	go run ./tools/agenteval -out report.json -summary "$GITHUB_STEP_SUMMARY"
//	go run ./tools/agenteval -mode=command -agent-cmd="my-agent --prompt \$AGENTEVAL_PROMPT_FILE"
package main

import (
	"context"
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
		mode        = fs.String("mode", ModeReference, "evaluation mode: reference or command")
		agentCmd    = fs.String("agent-cmd", "", "shell command executed per task when -mode=command")
		tasksDir    = fs.String("tasks-dir", "", "directory containing versioned task YAML files (defaults to tools/agenteval/tasks)")
		taskFilter  = fs.String("task", "", "comma-separated list of task IDs to run (default: all)")
		minScore    = fs.Int("min-score", -1, "override minimum passing score per task (0..100; -1 uses each task's threshold)")
		outPath     = fs.String("out", "", "optional path to write machine-readable JSON report")
		summaryPath = fs.String("summary", "", "optional path to append Markdown summary (defaults to $GITHUB_STEP_SUMMARY when set)")
		timeout     = fs.Duration("timeout", 5*time.Minute, "per-task execution and grading timeout")
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
	if *timeout < 0 {
		return fmt.Errorf("agenteval: -timeout must be non-negative (got %s)", *timeout)
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
		RepoRoot:         root,
		TasksDir:         resolvedTasks,
		Mode:             *mode,
		AgentCmd:         *agentCmd,
		TaskFilter:       filter,
		MinScoreOverride: *minScore,
		Timeout:          *timeout,
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
			if _, err := os.Stat(filepath.Join(dir, "packages", "game", "go.mod")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("agenteval: could not locate repository root containing go.work and packages/game/go.mod")
		}
		dir = parent
	}
}
