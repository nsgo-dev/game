package editormcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/playtest"
)

const (
	// ToolGamePlaytest runs headless bot playtests configured by playtest.yaml with replay recordings and returns the evaluated YAML report.
	ToolGamePlaytest = "game_playtest"
	// ToolPlaytestDefine proposes adding or updating a metric in playtest.yaml, returning a diff that arrives as a review rather than overwriting the file directly.
	ToolPlaytestDefine = "playtest_define"
	// ToolPlaytestReport opens, inspects or updates the editor's Playtest Report screen (loading a report file, updating the agent's read and suggestions, or watching a run's recording).
	ToolPlaytestReport = "playtest_report"
)

// PlaytestRunRequest is the validated request passed to Options.RunPlaytest (or the default `go tool nsgo test` runner).
type PlaytestRunRequest struct {
	// ProjectRoot is the absolute directory of the open project.
	ProjectRoot string
	// ConfigFile is the slash-separated path to playtest.yaml relative to ProjectRoot.
	ConfigFile string
	// ConfigPath is the absolute path to playtest.yaml.
	ConfigPath string
	// Config is the validated playtest configuration with any count/seeds overrides applied.
	Config *playtest.Config
	// OutFile is the slash-separated path to the YAML report relative to ProjectRoot.
	OutFile string
	// OutPath is the absolute path to the YAML report.
	OutPath string
	// RecordingsDir is the slash-separated directory for per-run .replay.yaml files relative to ProjectRoot, or empty.
	RecordingsDir string
	// RecordingsPath is the absolute directory for per-run .replay.yaml files, or empty.
	RecordingsPath string
	// Pattern is the Go test -run regexp (defaults to "Playtest").
	Pattern string
}

// PlaytestRunResult is what game_playtest returns beside its text summary.
type PlaytestRunResult struct {
	// File is the slash-separated path of the playtest.yaml that ran.
	File string `json:"file"`
	// ReportFile is the slash-separated path where the YAML report was written.
	ReportFile string `json:"reportFile"`
	// RecordingsDir is the slash-separated directory where per-seed .replay.yaml recordings were written, if enabled.
	RecordingsDir string `json:"recordingsDir,omitempty"`
	// Passed reports whether every metric with a goal met its goal.
	Passed bool `json:"passed"`
	// MissedCount is how many metrics missed their goal.
	MissedCount int `json:"missedCount"`
	// Report is the evaluated playtest report.
	Report *playtest.Report `json:"report"`
}

// PlaytestReview is a proposed edit to playtest.yaml created by playtest_define awaiting a person's review.
type PlaytestReview struct {
	// ID uniquely identifies this pending review on the server (e.g. "playtest-review-1").
	ID string `json:"id"`
	// File is the slash-separated path to playtest.yaml from the project root.
	File string `json:"file"`
	// Metric is the proposed playtest metric definition.
	Metric playtest.Metric `json:"metric"`
	// Rationale explains why the agent proposed this metric (for example, which goal in brief.yaml motivated it).
	Rationale string `json:"rationale,omitempty"`
	// Replace reports whether the proposal updates an existing metric with the same name instead of appending a new one.
	Replace bool `json:"replace,omitempty"`
	// Before is the text of playtest.yaml on disk when the proposal was created.
	Before string `json:"before"`
	// After is the canonical YAML text of playtest.yaml if the proposal is accepted.
	After string `json:"after"`
	// Diff is a unified line diff between Before and After.
	Diff string `json:"diff"`
	// Status is "pending", "applied", or "dismissed".
	Status string `json:"status"`
}

// PlaytestDefineResult is what playtest_define returns beside its text summary.
type PlaytestDefineResult struct {
	// Status is "pending_review", confirming playtest.yaml on disk was not modified yet.
	Status string `json:"status"`
	// Review holds the proposed playtest.yaml change and its diff.
	Review PlaytestReview `json:"review"`
}

const gamePlaytestSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "file": {"type": "string", "description": "Slash-separated path to playtest.yaml from the project root; playtest.yaml when absent"},
    "run": {"type": "string", "description": "Go test -run regexp matching the tests that call playtest.RunTest; Playtest when absent"},
    "count": {"type": "integer", "minimum": 1, "description": "Override runs.count in playtest.yaml"},
    "seeds": {"type": "array", "items": {"type": "integer", "minimum": 0}, "description": "Override runs.seeds with explicit seeds"},
    "out": {"type": "string", "description": "Slash-separated output path for the YAML report; playtest.report.yaml when absent"},
    "recordings": {"type": "string", "description": "Slash-separated directory for per-seed .replay.yaml files; playtest-recordings when absent"}
  }
}`

const playtestDefineSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["metric"],
  "properties": {
    "file": {"type": "string", "description": "Slash-separated path to playtest.yaml from the project root; playtest.yaml when absent"},
    "rationale": {"type": "string", "description": "Short explanation of why this metric is proposed (such as the goal in brief.yaml it checks)"},
    "replace": {"type": "boolean", "description": "When true, update an existing metric with the same name instead of refusing duplicates"},
    "metric": {
      "type": "object",
      "additionalProperties": false,
      "required": ["name", "measure"],
      "properties": {
        "name": {"type": "string", "minLength": 1},
        "label": {"type": "string"},
        "unit": {"type": "string"},
        "measure": {
          "type": "object",
          "additionalProperties": false,
          "required": ["kind", "key"],
          "properties": {
            "kind": {"type": "string", "enum": ["event", "counter", "timer", "outcome", "ratio", "perf"]},
            "key": {"type": "string", "minLength": 1},
            "denomKey": {"type": "string"},
            "aggregate": {"type": "string", "enum": ["count", "sum", "mean", "median", "min", "max", "p95", "p99", "rate"]},
            "heatMap": {"type": "boolean"}
          }
        },
        "goal": {
          "type": "object",
          "additionalProperties": false,
          "required": ["source"],
          "properties": {
            "summary": {"type": "string"},
            "source": {"type": "string", "minLength": 1},
            "min": {"type": "number"},
            "max": {"type": "number"},
            "equal": {"type": "number"}
          }
        }
      }
    }
  }
}`

func playtestTools() []Tool {
	return []Tool{
		{
			Name:        ToolGamePlaytest,
			Title:       "Run playtest",
			Description: "Validate playtest.yaml and run the configured headless bot playtests with input recordings, writing a YAML report and returning each metric's value, goal status (met or missed) and heat-map position data.",
			InputSchema: []byte(gamePlaytestSchema),
			Action:      ActionRun,
			Target:      gamePlaytestTarget,
			Handler:     gamePlaytest,
		},
		{
			Name:        ToolPlaytestDefine,
			Title:       "Propose a playtest metric",
			Description: "Propose adding or updating a metric in playtest.yaml. Validates the metric against the playtest schema, builds the canonical YAML diff, and queues it as a pending review of playtest.yaml without modifying the file on disk until the person accepts it.",
			InputSchema: []byte(playtestDefineSchema),
			Action:      ActionEdit,
			Target:      playtestDefineTarget,
			Handler:     playtestDefine,
		},
		{
			Name:        ToolPlaytestReport,
			Title:       "Inspect or update the playtest report screen",
			Description: "Open or inspect the editor's Playtest Report screen, load a playtest.report.yaml file, attach the agent's read and actionable suggestions, or watch a recorded run in Play mode.",
			InputSchema: []byte(playtestReportSchema),
			Action:      ActionEdit,
			Target:      playtestReportTarget,
			Handler:     playtestReportTool,
		},
	}
}

const playtestReportSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "file": {"type": "string", "description": "Optional slash-separated path to a playtest.report.yaml file to load"},
    "open": {"type": "boolean", "description": "When true, open the Playtest Report screen; when false, close it"},
    "read": {"type": "string", "description": "Optional agent's read summary paragraph to show in the Playtest Report screen"},
    "suggestions": {"type": "array", "items": {"type": "string"}, "description": "Optional list of actionable suggestions shown under Agent's read"},
    "watchRun": {"type": "integer", "minimum": 1, "description": "Optional 1-based run index whose saved recording to open in Play mode"}
  }
}`

// PlaytestReportStatusResult is the structured payload returned by playtest_report.
type PlaytestReportStatusResult struct {
	Open             bool     `json:"open"`
	ReportPath       string   `json:"reportPath,omitempty"`
	Scope            string   `json:"scope,omitempty"`
	Project          string   `json:"project,omitempty"`
	ChartKind        string   `json:"chartKind,omitempty"`
	Passed           bool     `json:"passed"`
	MissedCount      int      `json:"missedCount"`
	WatchRunIndex    int      `json:"watchRunIndex,omitempty"`
	PendingProposals int      `json:"pendingProposals"`
	Read             string   `json:"read,omitempty"`
	Suggestions      []string `json:"suggestions,omitempty"`
}

type gamePlaytestArgs struct {
	File       string   `json:"file"`
	Run        string   `json:"run"`
	Count      int      `json:"count"`
	Seeds      []uint64 `json:"seeds"`
	Out        string   `json:"out"`
	Recordings *string  `json:"recordings"`
}

func gamePlaytest(ctx context.Context, c *Call) (*Result, error) {
	var args gamePlaytestArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("game_playtest: %w", err)
	}
	if args.Count < 0 {
		return nil, errors.New("game_playtest: count must be positive")
	}

	var (
		root       string
		sceneTitle string
	)
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		root = projectRoot(ed)
		if root == "" {
			return errors.New("game_playtest: no project is open in the editor; open one with go tool game editor <project-dir>")
		}
		if ed.Doc != nil && ed.Doc.Name != "" {
			sceneTitle = ed.Doc.Name
		} else {
			sceneTitle = sceneName(ed, root)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	cfgFile := args.File
	if cfgFile == "" {
		cfgFile = playtest.FileName
	}
	cleanCfg, err := cleanProjectPath(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("game_playtest: %w", err)
	}
	cfgPath := filepath.Join(root, filepath.FromSlash(cleanCfg))
	cfg, err := playtest.LoadConfigFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("game_playtest: %w", err)
	}
	if len(args.Seeds) > 0 {
		cfg.Runs.Seeds = slices.Clone(args.Seeds)
		cfg.Runs.Count = len(args.Seeds)
	}
	if args.Count > 0 {
		cfg.Runs.Count = args.Count
	}

	outFile := args.Out
	if outFile == "" {
		outFile = playtest.ReportFileName
	}
	cleanOut, err := cleanProjectPath(outFile)
	if err != nil {
		return nil, fmt.Errorf("game_playtest: %w", err)
	}
	outPath := filepath.Join(root, filepath.FromSlash(cleanOut))

	recDir := "playtest-recordings"
	if args.Recordings != nil {
		recDir = *args.Recordings
	}
	var cleanRec, recPath string
	if recDir != "" {
		cleanRec, err = cleanProjectPath(recDir)
		if err != nil {
			return nil, fmt.Errorf("game_playtest: %w", err)
		}
		recPath = filepath.Join(root, filepath.FromSlash(cleanRec))
	}

	pattern := args.Run
	if pattern == "" {
		pattern = "Playtest"
	}

	req := PlaytestRunRequest{
		ProjectRoot:    root,
		ConfigFile:     cleanCfg,
		ConfigPath:     cfgPath,
		Config:         cfg,
		OutFile:        cleanOut,
		OutPath:        outPath,
		RecordingsDir:  cleanRec,
		RecordingsPath: recPath,
		Pattern:        pattern,
	}

	// Always take a pre-start checkpoint before an autonomous playtest runs.
	_ = c.Editor(ctx, func(ed *editor.Editor) error {
		if ed.ActiveTurnID() == "" {
			_, _ = ed.TakeCheckpointBeforeAutomation("Playtest · "+cleanCfg, cleanOut)
		} else {
			_ = ed.RecordCheckpointFile(cleanOut)
		}
		return nil
	})

	runner := c.s.runPlaytest
	if runner == nil {
		runner = defaultRunPlaytest
	}
	playActivity := "playing " + sceneTitle + " headless"
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		c.s.beginBackgroundWork(ed, playActivity)
		return nil
	}); err == nil {
		defer func() {
			_ = c.Editor(context.Background(), func(ed *editor.Editor) error {
				c.s.endBackgroundWork(ed)
				return nil
			})
		}()
	}
	rep, err := runner(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("game_playtest: %w", err)
	}
	if err := playtest.ValidateReport(rep); err != nil {
		return nil, fmt.Errorf("game_playtest: invalid report: %w", err)
	}
	runs := len(rep.Runs)
	if runs <= 0 {
		runs = max(1, cfg.Runs.Count)
	}
	_ = c.Editor(ctx, func(ed *editor.Editor) error {
		ed.RecordPlaytestRun(runs)
		return nil
	})
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, fmt.Errorf("game_playtest: %w", err)
	}
	if err := playtest.SaveReportFile(outPath, rep); err != nil {
		return nil, fmt.Errorf("game_playtest: %w", err)
	}

	_ = c.Editor(ctx, func(ed *editor.Editor) error {
		return ed.LoadPlaytestReport(editor.PlaytestReportOpenConfig{
			Report:     rep,
			ReportPath: cleanOut,
			ConfigPath: cleanCfg,
			BaseDir:    root,
		})
	})

	res := PlaytestRunResult{
		File:          cleanCfg,
		ReportFile:    cleanOut,
		RecordingsDir: cleanRec,
		Passed:        rep.Passed,
		MissedCount:   len(rep.MissedMetrics()),
		Report:        rep,
	}
	return &Result{Text: rep.Summary(), Structured: res}, nil
}

func defaultRunPlaytest(ctx context.Context, req PlaytestRunRequest) (*playtest.Report, error) {
	if err := os.MkdirAll(filepath.Dir(req.OutPath), 0o755); err != nil {
		return nil, err
	}
	tmpDir, err := os.MkdirTemp(filepath.Dir(req.OutPath), ".playtest-report-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	tmpOut := filepath.Join(tmpDir, "report.yaml")

	env := append(os.Environ(),
		playtest.EnvConfig+"="+req.ConfigPath,
		playtest.EnvOut+"="+tmpOut,
		playtest.EnvNoFail+"=1",
	)
	if req.RecordingsPath != "" {
		env = append(env, playtest.EnvRecordings+"="+req.RecordingsPath)
	}
	if len(req.Config.Runs.Seeds) > 0 {
		parts := make([]string, len(req.Config.Runs.Seeds))
		for i, s := range req.Config.Runs.Seeds {
			parts[i] = strconv.FormatUint(s, 10)
		}
		env = append(env, playtest.EnvSeeds+"="+strings.Join(parts, ","))
	}
	if req.Config.Runs.Count > 0 {
		env = append(env, fmt.Sprintf("%s=%d", playtest.EnvCount, req.Config.Runs.Count))
	}

	cmd := exec.CommandContext(ctx, "go", "tool", "nsgo", "test", "-run", req.Pattern, "./...", "-count=1")
	cmd.Dir = req.ProjectRoot
	cmd.Env = env
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go tool nsgo test -run %q failed: %w\n%s", req.Pattern, err, strings.TrimSpace(output.String()))
	}
	rep, err := playtest.LoadReportFile(tmpOut)
	if err != nil {
		return nil, fmt.Errorf("playtest tests wrote no report to %s (make sure a test matching -run %q calls playtest.RunTest): %w", req.OutFile, req.Pattern, err)
	}
	return rep, nil
}

type playtestDefineArgs struct {
	File      string                 `json:"file"`
	Rationale string                 `json:"rationale"`
	Replace   bool                   `json:"replace"`
	Metric    playtestDefineMetricIn `json:"metric"`
}

type playtestDefineMetricIn struct {
	Name    string                  `json:"name"`
	Label   string                  `json:"label"`
	Unit    string                  `json:"unit"`
	Measure playtestDefineMeasureIn `json:"measure"`
	Goal    *playtestDefineGoalIn   `json:"goal"`
}

type playtestDefineMeasureIn struct {
	Kind      string `json:"kind"`
	Key       string `json:"key"`
	DenomKey  string `json:"denomKey"`
	Aggregate string `json:"aggregate"`
	HeatMap   bool   `json:"heatMap"`
}

type playtestDefineGoalIn struct {
	Summary string   `json:"summary"`
	Source  string   `json:"source"`
	Min     *float64 `json:"min"`
	Max     *float64 `json:"max"`
	Equal   *float64 `json:"equal"`
}

func (m playtestDefineMetricIn) toMetric() playtest.Metric {
	out := playtest.Metric{
		Name:  m.Name,
		Label: m.Label,
		Unit:  m.Unit,
		Measure: playtest.Measure{
			Kind:      m.Measure.Kind,
			Key:       m.Measure.Key,
			DenomKey:  m.Measure.DenomKey,
			Aggregate: m.Measure.Aggregate,
			HeatMap:   m.Measure.HeatMap,
		},
	}
	if m.Goal != nil {
		out.Goal = &playtest.Goal{
			Summary: m.Goal.Summary,
			Source:  m.Goal.Source,
			Min:     m.Goal.Min,
			Max:     m.Goal.Max,
			Equal:   m.Goal.Equal,
		}
	}
	return out
}

func playtestDefine(ctx context.Context, c *Call) (*Result, error) {
	var args playtestDefineArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("playtest_define: %w", err)
	}

	metric := args.Metric.toMetric()
	if err := playtest.ValidateMetric(metric); err != nil {
		return nil, fmt.Errorf("playtest_define: %w", err)
	}

	var root string
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		root = projectRoot(ed)
		if root == "" {
			return errors.New("playtest_define: no project is open in the editor; open one with go tool game editor <project-dir>")
		}
		return nil
	}); err != nil {
		return nil, err
	}

	cfgFile := args.File
	if cfgFile == "" {
		cfgFile = playtest.FileName
	}
	cleanCfg, err := cleanProjectPath(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("playtest_define: %w", err)
	}
	beforeBytes, err := readProjectFile(root, cleanCfg)
	if err != nil {
		return nil, fmt.Errorf("playtest_define: %w", err)
	}
	cfg, err := playtest.ParseConfig(beforeBytes)
	if err != nil {
		return nil, fmt.Errorf("playtest_define: %s: %w", cleanCfg, err)
	}

	updated := cfg.Clone()
	existingIdx := -1
	for i, m := range updated.Metrics {
		if m.Name == metric.Name {
			existingIdx = i
			break
		}
	}
	switch {
	case existingIdx >= 0 && !args.Replace:
		return nil, fmt.Errorf("playtest_define: %s already defines metric %q; set replace: true to update it or pick a new metric name", cleanCfg, metric.Name)
	case existingIdx >= 0:
		updated.Metrics[existingIdx] = metric
	default:
		updated.Metrics = append(updated.Metrics, metric)
	}

	afterBytes, err := playtest.EncodeConfig(updated)
	if err != nil {
		return nil, fmt.Errorf("playtest_define: %w", err)
	}

	beforeStr := string(beforeBytes)
	afterStr := string(afterBytes)
	diffText := unifiedLineDiff(cleanCfg, beforeStr, afterStr)

	review := c.s.addPlaytestReview(PlaytestReview{
		File:      cleanCfg,
		Metric:    metric,
		Rationale: args.Rationale,
		Replace:   existingIdx >= 0,
		Before:    beforeStr,
		After:     afterStr,
		Diff:      diffText,
		Status:    "pending",
	}, root)

	reviewID := review.ID
	_ = c.Editor(ctx, func(ed *editor.Editor) error {
		ed.AddPlaytestProposal(editor.PlaytestProposal{
			ID:          reviewID,
			File:        review.File,
			MetricName:  review.Metric.Name,
			MetricLabel: review.Metric.Label,
			Rationale:   review.Rationale,
			Replace:     review.Replace,
			Before:      review.Before,
			After:       review.After,
			Diff:        review.Diff,
			Status:      review.Status,
			ApplyFunc: func() error {
				return c.s.ApplyPlaytestReview(reviewID)
			},
			DismissFunc: func() error {
				return c.s.DismissPlaytestReview(reviewID)
			},
		})
		return nil
	})

	var text strings.Builder
	verb := "add"
	if review.Replace {
		verb = "update"
	}
	fmt.Fprintf(&text, "Proposed to %s metric %q in %s as review %s (pending review; file on disk unchanged).", verb, metric.Name, cleanCfg, review.ID)
	if args.Rationale != "" {
		fmt.Fprintf(&text, "\nRationale: %s", args.Rationale)
	}
	text.WriteString("\n\n")
	text.WriteString(diffText)

	return &Result{
		Text: text.String(),
		Structured: PlaytestDefineResult{
			Status: "pending_review",
			Review: review,
		},
	}, nil
}

type storedPlaytestReview struct {
	review PlaytestReview
	root   string
}

func (s *Server) addPlaytestReview(r PlaytestReview, root string) PlaytestReview {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextReviewID++
	r.ID = fmt.Sprintf("playtest-review-%d", s.nextReviewID)
	s.playtestReviews = append(s.playtestReviews, storedPlaytestReview{review: r, root: root})
	return r
}

// PlaytestReviews returns all playtest.yaml metric reviews proposed via playtest_define on this server, in creation order.
func (s *Server) PlaytestReviews() []PlaytestReview {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PlaytestReview, len(s.playtestReviews))
	for i, item := range s.playtestReviews {
		out[i] = item.review
	}
	return out
}

// ApplyPlaytestReview applies the pending playtest_define review with ID id to disk after verifying the file has not changed since the review was created.
func (s *Server) ApplyPlaytestReview(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.playtestReviews {
		item := &s.playtestReviews[i]
		if item.review.ID != id {
			continue
		}
		if item.review.Status != "pending" {
			return fmt.Errorf("editormcp: review %s is %s, not pending", id, item.review.Status)
		}
		path := filepath.Join(item.root, filepath.FromSlash(item.review.File))
		current, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("editormcp: read %s: %w", item.review.File, err)
		}
		if string(current) != item.review.Before {
			return fmt.Errorf("editormcp: %s changed on disk since review %s was proposed; re-propose the metric", item.review.File, id)
		}
		if err := os.WriteFile(path, []byte(item.review.After), 0o644); err != nil {
			return fmt.Errorf("editormcp: write %s: %w", item.review.File, err)
		}
		item.review.Status = "applied"
		return nil
	}
	return fmt.Errorf("editormcp: unknown playtest review %q", id)
}

// DismissPlaytestReview marks the pending playtest_define review with ID id as dismissed without modifying disk.
func (s *Server) DismissPlaytestReview(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.playtestReviews {
		item := &s.playtestReviews[i]
		if item.review.ID != id {
			continue
		}
		if item.review.Status != "pending" {
			return fmt.Errorf("editormcp: review %s is %s, not pending", id, item.review.Status)
		}
		item.review.Status = "dismissed"
		return nil
	}
	return fmt.Errorf("editormcp: unknown playtest review %q", id)
}

// unifiedLineDiff produces a clean, deterministic unified line diff between before and after for review presentation.
func unifiedLineDiff(file, before, after string) string {
	a := splitLines(before)
	b := splitLines(after)

	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}

	var out strings.Builder
	fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n", file, file)
	if prefix == len(a) && prefix == len(b) {
		return out.String()
	}

	ctxStart := max(0, prefix-2)
	aEnd := len(a) - suffix
	bEnd := len(b) - suffix
	ctxEndA := min(len(a), aEnd+2)
	ctxEndB := min(len(b), bEnd+2)

	fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", ctxStart+1, ctxEndA-ctxStart, ctxStart+1, ctxEndB-ctxStart)
	for i := ctxStart; i < prefix; i++ {
		out.WriteString(" " + a[i] + "\n")
	}
	for i := prefix; i < aEnd; i++ {
		out.WriteString("-" + a[i] + "\n")
	}
	for i := prefix; i < bEnd; i++ {
		out.WriteString("+" + b[i] + "\n")
	}
	for i := aEnd; i < ctxEndA; i++ {
		out.WriteString(" " + a[i] + "\n")
	}
	return out.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

type playtestReportArgs struct {
	File        string   `json:"file"`
	Open        *bool    `json:"open"`
	Read        string   `json:"read"`
	Suggestions []string `json:"suggestions"`
	WatchRun    int      `json:"watchRun"`
}

func playtestReportTool(ctx context.Context, c *Call) (*Result, error) {
	var args playtestReportArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("playtest_report: %w", err)
	}

	var status PlaytestReportStatusResult
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		if args.File != "" {
			cleanFile, err := cleanProjectPath(args.File)
			if err != nil {
				return err
			}
			if err := ed.OpenPlaytestReportFile(cleanFile); err != nil {
				return err
			}
		}
		if args.Read != "" || args.Suggestions != nil {
			if err := ed.SetPlaytestAgentRead(args.Read, args.Suggestions); err != nil {
				return err
			}
		}
		if args.Open != nil {
			ed.SetPlaytestReportOpen(*args.Open)
		}
		if args.WatchRun < 0 {
			return errors.New("watchRun must be >= 1")
		}
		if args.WatchRun > 0 {
			if err := ed.WatchPlaytestRun(args.WatchRun); err != nil {
				return err
			}
		}

		status.Open = ed.PlaytestReportOpen()
		if sess := ed.PlaytestSession(); sess != nil {
			for _, p := range sess.Proposals {
				if p.Status == "pending" {
					status.PendingProposals++
				}
			}
			if ent := sess.ActiveEntry(); ent != nil && ent.Report != nil {
				status.ReportPath = ent.ReportPath
				status.Scope = ent.Report.Scope.Scene
				status.Project = ent.Report.Scope.Project
				status.ChartKind = ent.Chart.Kind.String()
				status.Passed = ent.Report.Passed
				status.MissedCount = len(ent.Report.MissedMetrics())
				status.WatchRunIndex = ent.WatchRunIndex
				status.Read = ent.AgentRead
				for _, sg := range ent.Suggestions {
					status.Suggestions = append(status.Suggestions, sg.Text)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("playtest_report: %w", err)
	}

	var summary string
	if status.Scope != "" {
		summary = fmt.Sprintf("Playtest report %s (open=%v, chart=%s, passed=%v, missed=%d, pendingProposals=%d)",
			status.Scope, status.Open, status.ChartKind, status.Passed, status.MissedCount, status.PendingProposals)
	} else {
		summary = fmt.Sprintf("Playtest report screen (open=%v, no report loaded, pendingProposals=%d)", status.Open, status.PendingProposals)
	}
	return &Result{Text: summary, Structured: status}, nil
}
