package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// CriterionResult records the outcome of one rubric criterion on a task.
type CriterionResult struct {
	Name   string `json:"name"`
	Weight int    `json:"weight"`
	Earned int    `json:"earned"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

// TaskReport records the evaluation result and per-criterion score breakdown for one task.
type TaskReport struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Score       int               `json:"score"`
	MaxScore    int               `json:"max_score"`
	MinScore    int               `json:"min_score"`
	Passed      bool              `json:"passed"`
	DurationMS  int64             `json:"duration_ms"`
	Criteria    []CriterionResult `json:"criteria"`
	AgentOutput string            `json:"agent_output,omitempty"`
	// AgentTimedOut reports that the agent hit its own timeout; grading still ran on what it left.
	AgentTimedOut         bool    `json:"agent_timed_out,omitempty"`
	AgentError            string  `json:"agent_error,omitempty"`
	AgentTurns            int     `json:"agent_turns,omitempty"`
	AgentToolCalls        int     `json:"agent_tool_calls,omitempty"`
	AgentPromptTokens     int     `json:"agent_prompt_tokens,omitempty"`
	AgentCompletionTokens int     `json:"agent_completion_tokens,omitempty"`
	Trials                int     `json:"trials"`
	PassRate              float64 `json:"pass_rate"`
	MeanScore             float64 `json:"mean_score"`
	StdDev                float64 `json:"stddev"`
}

// SuiteReport aggregates scores across every evaluated task.
type SuiteReport struct {
	Version      int          `json:"version"`
	Mode         string       `json:"mode"`
	Provider     string       `json:"provider,omitempty"`
	Model        string       `json:"model,omitempty"`
	Skipped      bool         `json:"skipped,omitempty"`
	SkipReason   string       `json:"skip_reason,omitempty"`
	PassedTasks  int          `json:"passed_tasks"`
	TotalTasks   int          `json:"total_tasks"`
	AverageScore float64      `json:"average_score"`
	Passed       bool         `json:"passed"`
	Tasks        []TaskReport `json:"tasks"`
}

// AggregateTaskReports combines repeated independent trials of one task into one report. The criteria, agent output and agent error come from the first failing trial (or the first trial when all pass), so a failed task shows the evidence of a failure; duration, turns, tool calls and tokens are summed over every trial. The task passes only when every trial passed.
func AggregateTaskReports(trials []TaskReport) TaskReport {
	if len(trials) == 0 {
		return TaskReport{}
	}
	out := trials[0]
	for _, t := range trials {
		if !t.Passed {
			out = t
			break
		}
	}
	out.Trials = len(trials)
	out.DurationMS = 0
	out.AgentTurns = 0
	out.AgentToolCalls = 0
	out.AgentPromptTokens = 0
	out.AgentCompletionTokens = 0
	var sum, pass float64
	for _, t := range trials {
		sum += float64(t.Score)
		if t.Passed {
			pass++
		}
		if t.AgentTimedOut {
			out.AgentTimedOut = true
		}
		out.DurationMS += t.DurationMS
		out.AgentTurns += t.AgentTurns
		out.AgentToolCalls += t.AgentToolCalls
		out.AgentPromptTokens += t.AgentPromptTokens
		out.AgentCompletionTokens += t.AgentCompletionTokens
	}
	n := float64(len(trials))
	out.MeanScore = sum / n
	out.PassRate = pass / n
	var variance float64
	for _, t := range trials {
		d := float64(t.Score) - out.MeanScore
		variance += d * d
	}
	out.StdDev = math.Sqrt(variance / n)
	out.Score = int(math.Round(out.MeanScore))
	out.Passed = pass == n
	return out
}

// BuildSuiteReport computes aggregate totals and pass/fail status from task reports.
func BuildSuiteReport(mode string, tasks []TaskReport) SuiteReport {
	report := SuiteReport{
		Version:    CurrentTaskVersion,
		Mode:       mode,
		TotalTasks: len(tasks),
		Passed:     len(tasks) > 0,
		Tasks:      tasks,
	}
	sum := 0
	for _, t := range tasks {
		sum += t.Score
		if t.Passed {
			report.PassedTasks++
		} else {
			report.Passed = false
		}
	}
	if len(tasks) > 0 {
		report.AverageScore = float64(sum) / float64(len(tasks))
	}
	return report
}

// BuildSkippedSuiteReport constructs a passing skipped report used when live agent evaluation is not configured.
func BuildSkippedSuiteReport(mode, reason string) SuiteReport {
	return SuiteReport{
		Version:    CurrentTaskVersion,
		Mode:       mode,
		Skipped:    true,
		SkipReason: strings.TrimSpace(reason),
		Passed:     true,
		Tasks:      []TaskReport{},
	}
}

// RenderMarkdown formats r as a GitHub-Flavored Markdown summary table suitable for stdout, files, or $GITHUB_STEP_SUMMARY.
func (r SuiteReport) RenderMarkdown() string {
	var b strings.Builder
	if r.Mode != "" {
		fmt.Fprintf(&b, "## Agent Evaluation Suite (%s)\n\n", r.Mode)
	} else {
		b.WriteString("## Agent Evaluation Suite\n\n")
	}
	fmt.Fprintf(&b, "- **Mode**: `%s`\n", r.Mode)
	if r.Provider != "" {
		fmt.Fprintf(&b, "- **Provider**: `%s`\n", r.Provider)
	}
	if r.Model != "" {
		fmt.Fprintf(&b, "- **Model**: `%s`\n", r.Model)
	}
	if r.Skipped {
		b.WriteString("- **Status**: SKIPPED\n")
		if r.SkipReason != "" {
			fmt.Fprintf(&b, "- **Reason**: %s\n", strings.TrimSpace(r.SkipReason))
		}
		return b.String()
	}

	fmt.Fprintf(&b, "- **Tasks passed**: %d / %d\n", r.PassedTasks, r.TotalTasks)
	fmt.Fprintf(&b, "- **Average score**: %.1f / 100\n", r.AverageScore)

	showAgentStats := r.Mode == ModeAgent
	var totalPrompt, totalCompletion int
	for _, t := range r.Tasks {
		if t.AgentTurns > 0 || t.AgentToolCalls > 0 {
			showAgentStats = true
		}
		totalPrompt += t.AgentPromptTokens
		totalCompletion += t.AgentCompletionTokens
	}
	if totalPrompt > 0 || totalCompletion > 0 {
		fmt.Fprintf(&b, "- **Tokens**: %d prompt / %d completion\n", totalPrompt, totalCompletion)
	}
	b.WriteString("\n")

	if showAgentStats {
		b.WriteString("| Task | Score | Threshold | Criteria | Agent | Status | Duration |\n")
		b.WriteString("| --- | ---: | ---: | ---: | --- | --- | ---: |\n")
	} else {
		b.WriteString("| Task | Score | Threshold | Criteria | Status | Duration |\n")
		b.WriteString("| --- | ---: | ---: | ---: | --- | ---: |\n")
	}

	for _, t := range r.Tasks {
		passedCriteria := 0
		for _, c := range t.Criteria {
			if c.Passed {
				passedCriteria++
			}
		}
		status := "PASS"
		if !t.Passed {
			status = "FAIL"
		}
		if t.AgentTimedOut {
			status += " (agent timed out)"
		} else if t.AgentError != "" {
			status += " (agent error)"
		}
		if showAgentStats {
			agentStats := fmt.Sprintf("%d turns, %d tools", t.AgentTurns, t.AgentToolCalls)
			fmt.Fprintf(&b, "| `%s` (%s) | %d/%d | %d | %d/%d | %s | %s | %dms |\n",
				t.ID, mdCell(t.Title), t.Score, t.MaxScore, t.MinScore, passedCriteria, len(t.Criteria), agentStats, status, t.DurationMS)
		} else {
			fmt.Fprintf(&b, "| `%s` (%s) | %d/%d | %d | %d/%d | %s | %dms |\n",
				t.ID, mdCell(t.Title), t.Score, t.MaxScore, t.MinScore, passedCriteria, len(t.Criteria), status, t.DurationMS)
		}
	}

	b.WriteString("\n### Criterion Breakdown\n")
	for _, t := range r.Tasks {
		fmt.Fprintf(&b, "\n#### `%s` — %d/%d\n\n", t.ID, t.Score, t.MaxScore)
		if t.Trials > 1 {
			fmt.Fprintf(&b, "%d trials: pass rate %.0f%%, mean score %.1f, standard deviation %.1f. The criteria below are from the first failing trial, or the first trial when all passed.\n\n", t.Trials, t.PassRate*100, t.MeanScore, t.StdDev)
		}
		if t.AgentError != "" {
			fmt.Fprintf(&b, "> Agent error: %s\n\n", mdCell(t.AgentError))
		}
		b.WriteString("| Criterion | Points | Status | Detail |\n")
		b.WriteString("| --- | ---: | --- | --- |\n")
		for _, c := range t.Criteria {
			status := "PASS"
			if !c.Passed {
				status = "FAIL"
			}
			fmt.Fprintf(&b, "| `%s` | %d/%d | %s | %s |\n", c.Name, c.Earned, c.Weight, status, mdCell(c.Detail))
		}
	}
	return b.String()
}

func mdCell(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	return strings.ReplaceAll(s, "|", "\\|")
}

// WriteJSON writes r as formatted deterministic JSON to path.
func (r SuiteReport) WriteJSON(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON report: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write JSON report %s: %w", path, err)
	}
	return nil
}

// WriteMarkdown writes r.RenderMarkdown() to path, replacing any existing file.
func (r SuiteReport) WriteMarkdown(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create markdown report directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(r.RenderMarkdown()), 0o644); err != nil {
		return fmt.Errorf("write markdown report %s: %w", path, err)
	}
	return nil
}
