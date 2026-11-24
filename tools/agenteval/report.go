package main

import (
	"encoding/json"
	"fmt"
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
	// AgentTimedOut reports that the agent command hit its own timeout; grading still ran on what it left.
	AgentTimedOut bool `json:"agent_timed_out,omitempty"`
}

// SuiteReport aggregates scores across every evaluated task.
type SuiteReport struct {
	Version      int          `json:"version"`
	Mode         string       `json:"mode"`
	PassedTasks  int          `json:"passed_tasks"`
	TotalTasks   int          `json:"total_tasks"`
	AverageScore float64      `json:"average_score"`
	Passed       bool         `json:"passed"`
	Tasks        []TaskReport `json:"tasks"`
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

// RenderMarkdown formats r as a GitHub-Flavored Markdown summary table suitable for stdout or $GITHUB_STEP_SUMMARY.
func (r SuiteReport) RenderMarkdown() string {
	var b strings.Builder
	b.WriteString("## Agent Evaluation Suite\n\n")
	fmt.Fprintf(&b, "- **Mode**: `%s`\n", r.Mode)
	fmt.Fprintf(&b, "- **Tasks passed**: %d / %d\n", r.PassedTasks, r.TotalTasks)
	fmt.Fprintf(&b, "- **Average score**: %.1f / 100\n\n", r.AverageScore)

	b.WriteString("| Task | Score | Threshold | Criteria | Status | Duration |\n")
	b.WriteString("| --- | ---: | ---: | ---: | --- | ---: |\n")
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
		}
		fmt.Fprintf(&b, "| `%s` (%s) | %d/%d | %d | %d/%d | %s | %dms |\n",
			t.ID, mdCell(t.Title), t.Score, t.MaxScore, t.MinScore, passedCriteria, len(t.Criteria), status, t.DurationMS)
	}

	b.WriteString("\n### Criterion Breakdown\n")
	for _, t := range r.Tasks {
		fmt.Fprintf(&b, "\n#### `%s` — %d/%d\n\n", t.ID, t.Score, t.MaxScore)
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
