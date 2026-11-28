package playtest

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/project"
	"go.yaml.in/yaml/v3"
)

// ReportVersion is the schema version EncodeReport writes and ParseReport accepts.
const ReportVersion = 1

// ReportFileName is the default playtest report file name written by go tool game playtest.
const ReportFileName = "playtest.report.yaml"

var (
	validOutcomes    = []string{OutcomeFinished, OutcomeFailed, OutcomeTimeout, OutcomeRunning}
	validStatuses    = []string{StatusMet, StatusMissed, StatusInfo}
	validSeriesFlags = []string{"", FlagProblem, FlagWarning}
)

// Report is the deterministic YAML report written after running a playtest batch.
type Report struct {
	// Version is the report schema version; it must equal ReportVersion (1).
	Version int `yaml:"version"`
	// Scope identifies the project and scene that were playtested.
	Scope Scope `yaml:"scope"`
	// Bot records the bot profile and parameters used for the runs.
	Bot BotConfig `yaml:"bot"`
	// Rate is the fixed simulation step rate in steps per second.
	Rate int `yaml:"rate"`
	// Speed is the configured speed multiplier.
	Speed float64 `yaml:"speed,omitempty"`
	// SeedCount is the number of seeded runs included in Runs.
	SeedCount int `yaml:"seedCount"`
	// Passed reports whether every metric with a Goal met its goal.
	Passed bool `yaml:"passed"`
	// Metrics holds the aggregated value and goal check for every metric in the playtest config.
	Metrics []MetricReport `yaml:"metrics,omitempty"`
	// BriefTargets lists every brief target covered by this report, including targets that were not measured.
	BriefTargets []BriefTargetReport `yaml:"briefTargets,omitempty"`
	// HeatMaps holds world-space position data collected from positioned events for heat maps.
	HeatMaps []HeatMap `yaml:"heatMaps,omitempty"`
	// Series holds labeled bar or per-level numeric series (such as hits per wave or shortest solution per level).
	Series []SeriesChart `yaml:"series,omitempty"`
	// Matrices holds labeled row-vs-column numeric matrices (such as deck-pair win rates in percent).
	Matrices []MatrixChart `yaml:"matrices,omitempty"`
	// Runs holds per-seed outcomes, durations, counters, timers, positioned events, perf readouts and recording paths.
	Runs []RunReport `yaml:"runs"`
	// Baselines holds the result of each baseline policy declared in the playtest config, at most one per policy.
	Baselines []BaselineReport `yaml:"baselines,omitempty"`
}

// Baseline policy names reported in BaselineReport.Name.
const (
	BaselineIdle      = "idle"
	BaselineReference = "reference"
	BaselineDegraded  = "degraded"
)

// BaselineReport records whether one declared baseline policy held across the seeded runs.
type BaselineReport struct {
	// Name is the policy: "idle", "reference" or "degraded".
	Name string `yaml:"name"`
	// Passed reports whether the policy held.
	Passed bool `yaml:"passed"`
	// Outcome is the outcome the idle runs reached, such as "lose" or "stall".
	Outcome string `yaml:"outcome,omitempty"`
	// WinRate is the measured win rate of the reference or degraded runs, from 0 to 1; nil when not measured.
	WinRate *float64 `yaml:"winRate,omitempty"`
	// Note is an optional one-line explanation, such as why the policy failed.
	Note string `yaml:"note,omitempty"`
}

// BriefTargetReport records whether one feel target from the project's brief.yaml was measured and met.
type BriefTargetReport struct {
	// Metric is the brief target's metric name.
	Metric string `yaml:"metric"`
	// Scene is the brief target's scene, empty for a target that applies to every scene.
	Scene string `yaml:"scene,omitempty"`
	// Basis is the target's basis in brief.yaml: locked, target or assumption.
	Basis string `yaml:"basis,omitempty"`
	// Status is BriefTargetPending when this report has no metric of that name, and otherwise BriefTargetMet or BriefTargetMissed from the metric's value checked against the brief target's own bounds.
	Status string `yaml:"status"`
}

// Brief target statuses in a report's briefTargets.
const (
	BriefTargetPending = "pending"
	BriefTargetMet     = "met"
	BriefTargetMissed  = "missed"
)

// ApplyBriefTargetStatuses lists in r every target of b that applies to r's scene, sorted by scene and metric. A target whose metric r did not measure stays pending, so a report never implies evidence it did not collect; a measured one is met or missed by its own bounds in b, even when playtest.yaml gives the metric a different goal. Targets r already lists are kept as they are.
func ApplyBriefTargetStatuses(r *Report, b *project.Brief) {
	if r == nil || b == nil {
		return
	}
	existing := make(map[string]bool, len(r.BriefTargets))
	for _, t := range r.BriefTargets {
		existing[t.Scene+"\x00"+t.Metric] = true
	}
	for _, t := range b.TargetsForScene(r.Scope.Scene) {
		if existing[t.Scene+"\x00"+t.Metric] {
			continue
		}
		status := BriefTargetPending
		if m := metricByName(r.Metrics, t.Metric); m != nil {
			status = BriefTargetMissed
			if GoalFromBriefTarget(t).Evaluate(m.Value) == StatusMet {
				status = BriefTargetMet
			}
		}
		r.BriefTargets = append(r.BriefTargets, BriefTargetReport{Metric: t.Metric, Scene: t.Scene, Basis: t.Basis, Status: status})
	}
	slices.SortFunc(r.BriefTargets, func(a, b BriefTargetReport) int {
		if a.Scene != b.Scene {
			return strings.Compare(a.Scene, b.Scene)
		}
		return strings.Compare(a.Metric, b.Metric)
	})
}

func metricByName(metrics []MetricReport, name string) *MetricReport {
	for i := range metrics {
		if metrics[i].Name == name {
			return &metrics[i]
		}
	}
	return nil
}

// SeriesChart holds labeled numeric bars or per-bucket aggregates across runs (such as hits per wave or shortest solution moves per level).
type SeriesChart struct {
	// Name uniquely identifies the series (such as "hits_per_wave" or "shortest_solution").
	Name string `yaml:"name"`
	// Label is an optional display title (such as "Hits taken per wave").
	Label string `yaml:"label,omitempty"`
	// Unit is an optional value unit (such as "hits" or "moves").
	Unit string `yaml:"unit,omitempty"`
	// Aggregate is how per-run bucket values were combined (such as "median", "mean", "sum", "min" or "max").
	Aggregate string `yaml:"aggregate,omitempty"`
	// TargetMax, when non-nil, is an upper target line for bars; entries above it are flagged.
	TargetMax *float64 `yaml:"targetMax,omitempty"`
	// Entries lists each labeled bar in deterministic display order.
	Entries []SeriesEntry `yaml:"entries,omitempty"`
}

// SeriesEntry is one labeled value in a SeriesChart.
type SeriesEntry struct {
	// Label identifies the bucket (such as "wave 1" or "level_07").
	Label string `yaml:"label"`
	// Value is the aggregated value for this bucket.
	Value float64 `yaml:"value"`
	// Flag optionally marks a standout bar: "problem" or "warning".
	Flag string `yaml:"flag,omitempty"`
	// Note is an optional short annotation for this entry.
	Note string `yaml:"note,omitempty"`
}

// MatrixChart holds a labeled row-vs-column numeric matrix (such as deck-pair win rates in percent).
type MatrixChart struct {
	// Name uniquely identifies the matrix (such as "deck_win_rates").
	Name string `yaml:"name"`
	// Label is an optional display title (such as "Win rate by deck pair").
	Label string `yaml:"label,omitempty"`
	// Unit is an optional display unit for cells (such as "%").
	Unit string `yaml:"unit,omitempty"`
	// FlagAbove, when non-nil, flags cells whose value exceeds this threshold (such as 60 for >60% win rate).
	FlagAbove *float64 `yaml:"flagAbove,omitempty"`
	// Labels lists the row and column names in order (such as deck names).
	Labels []string `yaml:"labels"`
	// Cells holds the len(Labels) x len(Labels) grid; nil entries represent diagonal or unplayed pairs.
	Cells [][]*float64 `yaml:"cells"`
}

// MetricReport holds the evaluated aggregate value and goal status for one Metric.
type MetricReport struct {
	// Name uniquely identifies the metric.
	Name string `yaml:"name"`
	// Label is the human-readable display label, if set.
	Label string `yaml:"label,omitempty"`
	// Unit is the display unit, if set.
	Unit string `yaml:"unit,omitempty"`
	// Aggregate is the aggregation applied across runs (such as "count", "sum", "median", "mean").
	Aggregate string `yaml:"aggregate"`
	// Value is the evaluated aggregate value.
	Value float64 `yaml:"value"`
	// Min and Max are the minimum and maximum per-run values contributing to Value.
	Min float64 `yaml:"min,omitempty"`
	Max float64 `yaml:"max,omitempty"`
	// Goal is the target range or threshold checked against Value, if any.
	Goal *Goal `yaml:"goal,omitempty"`
	// Status is "met", "missed", or "info" (when Goal is nil).
	Status string `yaml:"status"`
}

// HeatMap holds world-space positioned samples for one event metric across all runs.
type HeatMap struct {
	// Metric is the metric name this heat map corresponds to.
	Metric string `yaml:"metric"`
	// Event is the event kind recorded in Points (such as "death").
	Event string `yaml:"event"`
	// Points lists every recorded position in deterministic run-then-step order.
	Points []HeatPoint `yaml:"points,omitempty"`
}

// HeatPoint is one world-space event sample in a HeatMap.
type HeatPoint struct {
	// Seed is the run seed that produced this event.
	Seed uint64 `yaml:"seed"`
	// Step is the zero-based simulation step when the event happened.
	Step int `yaml:"step"`
	// X and Y are the world coordinates of the event.
	X float64 `yaml:"x"`
	Y float64 `yaml:"y"`
	// Tag is the optional event tag.
	Tag string `yaml:"tag,omitempty"`
}

// RunReport holds the deterministic results of one seeded playtest run.
type RunReport struct {
	// Index is the 1-based run number within the batch.
	Index int `yaml:"index"`
	// Seed is the deterministic random seed for this run.
	Seed uint64 `yaml:"seed"`
	// Outcome is "finished", "failed", or "timeout".
	Outcome string `yaml:"outcome"`
	// Reason is the optional failure reason when Outcome is "failed".
	Reason string `yaml:"reason,omitempty"`
	// Steps is the number of fixed simulation steps executed.
	Steps int `yaml:"steps"`
	// DurationSeconds is the simulated run duration in seconds (Steps / Rate).
	DurationSeconds float64 `yaml:"durationSeconds"`
	// Recording is the slash-separated path to this run's saved .replay.yaml recording, when saved.
	Recording string `yaml:"recording,omitempty"`
	// Counters holds the final values of all counters reported during this run.
	Counters map[string]float64 `yaml:"counters,omitempty"`
	// Timers holds the recorded simulated durations in seconds of all timers reported during this run.
	Timers map[string]float64 `yaml:"timers,omitempty"`
	// Events holds all positioned events reported during this run.
	Events []Event `yaml:"events,omitempty"`
	// Perf holds the perf readout captured for this run, when available.
	Perf *PerfSummary `yaml:"perf,omitempty"`
}

// MissedMetrics returns every MetricReport whose Status is StatusMissed.
func (r *Report) MissedMetrics() []MetricReport {
	if r == nil {
		return nil
	}
	var missed []MetricReport
	for _, m := range r.Metrics {
		if m.Status == StatusMissed {
			missed = append(missed, m)
		}
	}
	return missed
}

// Summary formats a concise, deterministic human-readable report summary listing every metric's value, goal, source file and MET/MISSED status.
func (r *Report) Summary() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	scope := r.Scope.Scene
	if r.Scope.Project != "" {
		scope = r.Scope.Project + " · " + scope
	}
	fmt.Fprintf(&b, "Playtest %s (%d runs, bot %s)\n", scope, r.SeedCount, r.Bot.Profile)
	for _, m := range r.Metrics {
		name := m.Name
		if m.Label != "" {
			name = m.Label
		}
		val := strconv.FormatFloat(cleanZero(m.Value), 'f', -1, 64)
		if m.Unit != "" {
			if m.Unit == "%" {
				val += "%"
			} else {
				val += " " + m.Unit
			}
		}
		status := strings.ToUpper(m.Status)
		if m.Goal != nil {
			fmt.Fprintf(&b, "  [%s] %s = %s (goal: %s, from %s)\n", status, name, val, m.Goal.Describe(m.Unit), m.Goal.Source)
		} else {
			fmt.Fprintf(&b, "  [%s] %s = %s\n", status, name, val)
		}
	}
	if missed := r.MissedMetrics(); len(missed) > 0 {
		fmt.Fprintf(&b, "Result: FAILED (%d of %d goals missed)\n", len(missed), countGoalMetrics(r.Metrics))
	} else {
		fmt.Fprintf(&b, "Result: PASSED (%d runs)\n", r.SeedCount)
	}
	return b.String()
}

func countGoalMetrics(metrics []MetricReport) int {
	n := 0
	for _, m := range metrics {
		if m.Goal != nil {
			n++
		}
	}
	return n
}

// ValidateReport checks that r uses ReportVersion and has consistent, finite fields.
func ValidateReport(r *Report) error {
	if r == nil {
		return loaderr.Errorf("", "provide a playtest report", "playtest report is nil")
	}
	if r.Version != ReportVersion {
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("unsupported version %d", r.Version),
			Expected: fmt.Sprint(ReportVersion),
			Fix:      fmt.Sprintf("set version to %d", ReportVersion),
		}
	}
	if strings.TrimSpace(r.Scope.Scene) == "" {
		return loaderr.Errorf("scope.scene", "set scope.scene to the tested scene name", "scope.scene is required")
	}
	if err := validateBot("bot", r.Bot); err != nil {
		return err
	}
	if r.Rate <= 0 {
		return loaderr.Errorf("rate", "set rate to a positive fixed step rate", "rate %d must be positive", r.Rate)
	}
	if !isFinite(r.Speed) || r.Speed < 0 {
		return loaderr.Errorf("speed", "set speed to a finite non-negative number", "speed %v is invalid", r.Speed)
	}
	if r.SeedCount < 0 || r.SeedCount != len(r.Runs) {
		return loaderr.Errorf("seedCount", "set seedCount to the number of entries in runs", "seedCount %d does not match len(runs) %d", r.SeedCount, len(r.Runs))
	}
	seenMetrics := make(map[string]struct{}, len(r.Metrics))
	for i, m := range r.Metrics {
		at := loaderr.Path("metrics", i)
		if strings.TrimSpace(m.Name) == "" {
			return loaderr.Errorf(loaderr.Join(at, "name"), "give each metric a non-empty name", "metric name is required")
		}
		if _, dup := seenMetrics[m.Name]; dup {
			return loaderr.Errorf(loaderr.Join(at, "name"), "list each metric once", "duplicate metric %q", m.Name)
		}
		seenMetrics[m.Name] = struct{}{}
		if !slices.Contains(validAggregates, m.Aggregate) {
			return loaderr.Errorf(loaderr.Join(at, "aggregate"), "use one of: "+strings.Join(validAggregates, ", "), "unknown aggregate %q", m.Aggregate)
		}
		if !isFinite(m.Value) || !isFinite(m.Min) || !isFinite(m.Max) {
			return loaderr.Errorf(at, "use finite metric values", "metric %q has non-finite values", m.Name)
		}
		if !slices.Contains(validStatuses, m.Status) {
			return loaderr.Errorf(loaderr.Join(at, "status"), "use met, missed or info", "unknown metric status %q", m.Status)
		}
		if m.Goal != nil {
			if err := validateGoal(loaderr.Join(at, "goal"), m.Goal); err != nil {
				return err
			}
		}
	}
	seenBrief := make(map[string]struct{}, len(r.BriefTargets))
	for i, t := range r.BriefTargets {
		at := loaderr.Path("briefTargets", i)
		if strings.TrimSpace(t.Metric) == "" {
			return loaderr.Errorf(loaderr.Join(at, "metric"), "set a brief target metric", "brief target metric is required")
		}
		if !slices.Contains([]string{BriefTargetPending, BriefTargetMet, BriefTargetMissed}, t.Status) {
			return loaderr.Errorf(loaderr.Join(at, "status"), "use pending, met, or missed", "unknown brief target status %q", t.Status)
		}
		key := t.Scene + "\x00" + t.Metric
		if _, dup := seenBrief[key]; dup {
			return loaderr.Errorf(loaderr.Join(at, "metric"), "list each brief target once", "duplicate brief target %q", t.Metric)
		}
		seenBrief[key] = struct{}{}
	}
	for i, hm := range r.HeatMaps {
		at := loaderr.Path("heatMaps", i)
		if strings.TrimSpace(hm.Metric) == "" {
			return loaderr.Errorf(loaderr.Join(at, "metric"), "name the metric the heat map belongs to", "heatMap metric is required")
		}
		if strings.TrimSpace(hm.Event) == "" {
			return loaderr.Errorf(loaderr.Join(at, "event"), "name the event kind the heat map records", "heatMap event is required")
		}
		for j, pt := range hm.Points {
			if pt.Step < 0 || !isFinite(pt.X) || !isFinite(pt.Y) {
				return loaderr.Errorf(loaderr.Join(at, "points", j), "use non-negative step and finite x, y", "invalid heat map point")
			}
		}
	}
	seenSeries := make(map[string]struct{}, len(r.Series))
	for i, s := range r.Series {
		at := loaderr.Path("series", i)
		if strings.TrimSpace(s.Name) == "" {
			return loaderr.Errorf(loaderr.Join(at, "name"), "give each series a non-empty name", "series name is required")
		}
		if _, dup := seenSeries[s.Name]; dup {
			return loaderr.Errorf(loaderr.Join(at, "name"), "list each series once", "duplicate series %q", s.Name)
		}
		seenSeries[s.Name] = struct{}{}
		if s.Aggregate != "" && !slices.Contains(validAggregates, s.Aggregate) {
			return loaderr.Errorf(loaderr.Join(at, "aggregate"), "use one of: "+strings.Join(validAggregates, ", "), "unknown series aggregate %q", s.Aggregate)
		}
		if s.TargetMax != nil && !isFinite(*s.TargetMax) {
			return loaderr.Errorf(loaderr.Join(at, "targetMax"), "use a finite targetMax", "series %q targetMax is not finite", s.Name)
		}
		for j, e := range s.Entries {
			eat := loaderr.Join(at, "entries", j)
			if strings.TrimSpace(e.Label) == "" {
				return loaderr.Errorf(loaderr.Join(eat, "label"), "give each series entry a non-empty label", "series entry label is required")
			}
			if !isFinite(e.Value) {
				return loaderr.Errorf(loaderr.Join(eat, "value"), "use a finite value", "series entry %q has non-finite value", e.Label)
			}
			if !slices.Contains(validSeriesFlags, e.Flag) {
				return loaderr.Errorf(loaderr.Join(eat, "flag"), "use problem, warning or empty", "unknown series flag %q", e.Flag)
			}
		}
	}
	seenMatrices := make(map[string]struct{}, len(r.Matrices))
	for i, m := range r.Matrices {
		at := loaderr.Path("matrices", i)
		if strings.TrimSpace(m.Name) == "" {
			return loaderr.Errorf(loaderr.Join(at, "name"), "give each matrix a non-empty name", "matrix name is required")
		}
		if _, dup := seenMatrices[m.Name]; dup {
			return loaderr.Errorf(loaderr.Join(at, "name"), "list each matrix once", "duplicate matrix %q", m.Name)
		}
		seenMatrices[m.Name] = struct{}{}
		if m.FlagAbove != nil && !isFinite(*m.FlagAbove) {
			return loaderr.Errorf(loaderr.Join(at, "flagAbove"), "use a finite flagAbove threshold", "matrix %q flagAbove is not finite", m.Name)
		}
		if len(m.Labels) == 0 {
			return loaderr.Errorf(loaderr.Join(at, "labels"), "provide at least one matrix label", "matrix %q has no labels", m.Name)
		}
		seenLabels := make(map[string]struct{}, len(m.Labels))
		for j, lbl := range m.Labels {
			if strings.TrimSpace(lbl) == "" {
				return loaderr.Errorf(loaderr.Join(at, "labels", j), "use non-empty matrix labels", "matrix label is empty")
			}
			if _, dup := seenLabels[lbl]; dup {
				return loaderr.Errorf(loaderr.Join(at, "labels", j), "use unique matrix labels", "duplicate matrix label %q", lbl)
			}
			seenLabels[lbl] = struct{}{}
		}
		if len(m.Cells) != len(m.Labels) {
			return loaderr.Errorf(loaderr.Join(at, "cells"), "provide one row per label", "matrix %q has %d rows, want %d", m.Name, len(m.Cells), len(m.Labels))
		}
		for rIdx, row := range m.Cells {
			if len(row) != len(m.Labels) {
				return loaderr.Errorf(loaderr.Join(at, "cells", rIdx), "provide one cell per label", "matrix %q row %d has %d cells, want %d", m.Name, rIdx, len(row), len(m.Labels))
			}
			for cIdx, cell := range row {
				if cell != nil && !isFinite(*cell) {
					return loaderr.Errorf(loaderr.Join(at, "cells", rIdx, cIdx), "use finite matrix cell values", "matrix %q cell [%d][%d] is not finite", m.Name, rIdx, cIdx)
				}
			}
		}
	}
	for i, run := range r.Runs {
		at := loaderr.Path("runs", i)
		if run.Index <= 0 {
			return loaderr.Errorf(loaderr.Join(at, "index"), "set run index to a 1-based positive integer", "invalid run index %d", run.Index)
		}
		if !slices.Contains(validOutcomes, run.Outcome) {
			return loaderr.Errorf(loaderr.Join(at, "outcome"), "use finished, failed, timeout or running", "unknown run outcome %q", run.Outcome)
		}
		if run.Steps < 0 {
			return loaderr.Errorf(loaderr.Join(at, "steps"), "set steps to 0 or more", "negative steps %d", run.Steps)
		}
		if !isFinite(run.DurationSeconds) || run.DurationSeconds < 0 {
			return loaderr.Errorf(loaderr.Join(at, "durationSeconds"), "set durationSeconds to a finite non-negative number", "invalid durationSeconds %v", run.DurationSeconds)
		}
		for k, v := range run.Counters {
			if k == "" || !isFinite(v) {
				return loaderr.Errorf(loaderr.Join(at, "counters", k), "use non-empty counter names and finite numbers", "invalid counter %q=%v", k, v)
			}
		}
		for k, v := range run.Timers {
			if k == "" || !isFinite(v) || v < 0 {
				return loaderr.Errorf(loaderr.Join(at, "timers", k), "use non-empty timer names and finite non-negative seconds", "invalid timer %q=%v", k, v)
			}
		}
		for j, ev := range run.Events {
			if ev.Kind == "" || ev.Step < 0 || !isFinite(ev.TimeSeconds) || ev.TimeSeconds < 0 || !isFinite(ev.X) || !isFinite(ev.Y) {
				return loaderr.Errorf(loaderr.Join(at, "events", j), "use non-empty kind, non-negative step/time and finite x, y", "invalid event")
			}
		}
		if run.Perf != nil {
			if run.Perf.SampleCount < 0 || !isFinite(run.Perf.FrameMs) || !isFinite(run.Perf.UpdateMs) || !isFinite(run.Perf.DrawMs) ||
				!isFinite(run.Perf.P50FrameMs) || !isFinite(run.Perf.P95FrameMs) || !isFinite(run.Perf.P99FrameMs) ||
				!isFinite(run.Perf.MaxFrameMs) || !isFinite(run.Perf.AllocsPerFrame) {
				return loaderr.Errorf(loaderr.Join(at, "perf"), "use non-negative finite perf metrics", "invalid perf summary")
			}
		}
	}
	seenBaselines := make(map[string]struct{}, len(r.Baselines))
	for i, b := range r.Baselines {
		at := loaderr.Path("baselines", i)
		if b.Name != BaselineIdle && b.Name != BaselineReference && b.Name != BaselineDegraded {
			return loaderr.Errorf(loaderr.Join(at, "name"), "use idle, reference or degraded", "unknown baseline %q", b.Name)
		}
		if _, dup := seenBaselines[b.Name]; dup {
			return loaderr.Errorf(loaderr.Join(at, "name"), "report each baseline once", "duplicate baseline %q", b.Name)
		}
		seenBaselines[b.Name] = struct{}{}
		if b.WinRate != nil && !isUnitRate(*b.WinRate) {
			return loaderr.Errorf(loaderr.Join(at, "winRate"), "set winRate between 0 and 1", "invalid baseline winRate %v", *b.WinRate)
		}
	}
	return nil
}

// ParseReport decodes and validates a YAML playtest report, refusing unknown fields, missing or unsupported versions and trailing YAML documents.
func ParseReport(data []byte) (*Report, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", "provide a playtest report YAML document", "playtest report is empty"), "", data, "")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var rep Report
	if err := dec.Decode(&rep); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("playtest: decode report: %w", err), "", data, "check the YAML syntax and field names in the playtest report")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep one YAML document per report file", "unexpected trailing YAML document"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("playtest: decode report: %w", err), "", data, "remove trailing YAML content after the playtest report")
	}
	normalizeReport(&rep)
	if err := ValidateReport(&rep); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	return &rep, nil
}

func normalizeReport(r *Report) {
	r.Speed = cleanZero(r.Speed)
	for k, v := range r.Bot.Params {
		if f, ok := v.(float64); ok {
			r.Bot.Params[k] = cleanZero(f)
		}
	}
	for i := range r.Metrics {
		r.Metrics[i].Value = cleanZero(r.Metrics[i].Value)
		r.Metrics[i].Min = cleanZero(r.Metrics[i].Min)
		r.Metrics[i].Max = cleanZero(r.Metrics[i].Max)
		normalizeGoal(r.Metrics[i].Goal)
	}
	for i := range r.HeatMaps {
		for j := range r.HeatMaps[i].Points {
			r.HeatMaps[i].Points[j].X = cleanZero(r.HeatMaps[i].Points[j].X)
			r.HeatMaps[i].Points[j].Y = cleanZero(r.HeatMaps[i].Points[j].Y)
		}
	}
	for i := range r.Series {
		if r.Series[i].TargetMax != nil {
			v := cleanZero(*r.Series[i].TargetMax)
			r.Series[i].TargetMax = &v
		}
		for j := range r.Series[i].Entries {
			r.Series[i].Entries[j].Value = cleanZero(r.Series[i].Entries[j].Value)
		}
	}
	for i := range r.Matrices {
		if r.Matrices[i].FlagAbove != nil {
			v := cleanZero(*r.Matrices[i].FlagAbove)
			r.Matrices[i].FlagAbove = &v
		}
		for rIdx := range r.Matrices[i].Cells {
			for cIdx := range r.Matrices[i].Cells[rIdx] {
				if r.Matrices[i].Cells[rIdx][cIdx] != nil {
					v := cleanZero(*r.Matrices[i].Cells[rIdx][cIdx])
					r.Matrices[i].Cells[rIdx][cIdx] = &v
				}
			}
		}
	}
	for i := range r.Runs {
		r.Runs[i].DurationSeconds = cleanZero(r.Runs[i].DurationSeconds)
		for k, v := range r.Runs[i].Counters {
			r.Runs[i].Counters[k] = cleanZero(v)
		}
		for k, v := range r.Runs[i].Timers {
			r.Runs[i].Timers[k] = cleanZero(v)
		}
		for j := range r.Runs[i].Events {
			r.Runs[i].Events[j].TimeSeconds = cleanZero(r.Runs[i].Events[j].TimeSeconds)
			r.Runs[i].Events[j].X = cleanZero(r.Runs[i].Events[j].X)
			r.Runs[i].Events[j].Y = cleanZero(r.Runs[i].Events[j].Y)
		}
		if r.Runs[i].Perf != nil {
			p := sanitizePerf(*r.Runs[i].Perf)
			r.Runs[i].Perf = &p
		}
	}
	for i := range r.Baselines {
		r.Baselines[i].WinRate = cleanZeroPtr(r.Baselines[i].WinRate)
	}
}

// EncodeReport validates r and writes it as canonical, deterministic 2-space YAML.
func EncodeReport(r *Report) ([]byte, error) {
	if err := ValidateReport(r); err != nil {
		return nil, err
	}
	cp := cloneReport(r)
	normalizeReport(cp)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cp); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("playtest: encode report: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("playtest: encode report: %w", err)
	}
	return buf.Bytes(), nil
}

// LoadReportFile reads, decodes and validates a YAML playtest report from disk.
func LoadReportFile(path string) (*Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, loaderr.Locate(fmt.Errorf("playtest: read report %q: %w", path, err), path, nil, "check that the playtest report file exists and is readable")
	}
	rep, err := ParseReport(data)
	if err != nil {
		return nil, loaderr.WithFile(err, path)
	}
	return rep, nil
}

// SaveReportFile validates, encodes and writes r to path.
func SaveReportFile(path string, r *Report) error {
	data, err := EncodeReport(r)
	if err != nil {
		return fmt.Errorf("playtest: write report %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("playtest: write report %q: %w", path, err)
	}
	return nil
}

func cloneReport(r *Report) *Report {
	if r == nil {
		return nil
	}
	out := &Report{
		Version:   r.Version,
		Scope:     r.Scope,
		Bot:       cloneBot(r.Bot),
		Rate:      r.Rate,
		Speed:     r.Speed,
		SeedCount: r.SeedCount,
		Passed:    r.Passed,
	}
	if len(r.Metrics) > 0 {
		out.Metrics = make([]MetricReport, len(r.Metrics))
		for i, m := range r.Metrics {
			cp := m
			cp.Goal = cloneGoal(m.Goal)
			out.Metrics[i] = cp
		}
	}
	if len(r.BriefTargets) > 0 {
		out.BriefTargets = slices.Clone(r.BriefTargets)
	}
	if len(r.HeatMaps) > 0 {
		out.HeatMaps = make([]HeatMap, len(r.HeatMaps))
		for i, hm := range r.HeatMaps {
			out.HeatMaps[i] = HeatMap{
				Metric: hm.Metric,
				Event:  hm.Event,
				Points: slices.Clone(hm.Points),
			}
		}
	}
	if len(r.Series) > 0 {
		out.Series = make([]SeriesChart, len(r.Series))
		for i, s := range r.Series {
			cp := SeriesChart{
				Name:      s.Name,
				Label:     s.Label,
				Unit:      s.Unit,
				Aggregate: s.Aggregate,
				Entries:   slices.Clone(s.Entries),
			}
			if s.TargetMax != nil {
				v := *s.TargetMax
				cp.TargetMax = &v
			}
			out.Series[i] = cp
		}
	}
	if len(r.Matrices) > 0 {
		out.Matrices = make([]MatrixChart, len(r.Matrices))
		for i, m := range r.Matrices {
			cp := MatrixChart{
				Name:   m.Name,
				Label:  m.Label,
				Unit:   m.Unit,
				Labels: slices.Clone(m.Labels),
				Cells:  make([][]*float64, len(m.Cells)),
			}
			if m.FlagAbove != nil {
				v := *m.FlagAbove
				cp.FlagAbove = &v
			}
			for rIdx, row := range m.Cells {
				cp.Cells[rIdx] = make([]*float64, len(row))
				for cIdx, cell := range row {
					if cell != nil {
						v := *cell
						cp.Cells[rIdx][cIdx] = &v
					}
				}
			}
			out.Matrices[i] = cp
		}
	}
	if len(r.Runs) > 0 {
		out.Runs = make([]RunReport, len(r.Runs))
		for i, run := range r.Runs {
			cp := run
			if len(run.Counters) > 0 {
				cp.Counters = make(map[string]float64, len(run.Counters))
				for k, v := range run.Counters {
					cp.Counters[k] = v
				}
			} else {
				cp.Counters = nil
			}
			if len(run.Timers) > 0 {
				cp.Timers = make(map[string]float64, len(run.Timers))
				for k, v := range run.Timers {
					cp.Timers[k] = v
				}
			} else {
				cp.Timers = nil
			}
			cp.Events = slices.Clone(run.Events)
			if run.Perf != nil {
				p := *run.Perf
				cp.Perf = &p
			}
			out.Runs[i] = cp
		}
	}
	if len(r.Baselines) > 0 {
		out.Baselines = make([]BaselineReport, len(r.Baselines))
		for i, b := range r.Baselines {
			cp := b
			cp.WinRate = cloneFloat(b.WinRate)
			out.Baselines[i] = cp
		}
	}
	return out
}
