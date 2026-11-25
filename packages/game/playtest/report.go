package playtest

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"go.yaml.in/yaml/v3"
)

// ReportVersion is the schema version EncodeReport writes and ParseReport accepts.
const ReportVersion = 1

// ReportFileName is the default playtest report file name written by go tool game playtest.
const ReportFileName = "playtest.report.yaml"

var (
	validOutcomes = []string{OutcomeFinished, OutcomeFailed, OutcomeTimeout, OutcomeRunning}
	validStatuses = []string{StatusMet, StatusMissed, StatusInfo}
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
	// HeatMaps holds world-space position data collected from positioned events for heat maps.
	HeatMaps []HeatMap `yaml:"heatMaps,omitempty"`
	// Runs holds per-seed outcomes, durations, counters, timers, positioned events, perf readouts and recording paths.
	Runs []RunReport `yaml:"runs"`
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
	return nil
}

// ParseReport decodes and validates a YAML playtest report, refusing unknown fields, missing or unsupported versions and trailing YAML documents.
func ParseReport(data []byte) (*Report, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", "provide a playtest report YAML document", "playtest report is empty")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var rep Report
	if err := dec.Decode(&rep); err != nil {
		return nil, fmt.Errorf("playtest: decode report: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Errorf("", "keep one YAML document per report file", "unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("playtest: decode report: %w", err)
	}
	normalizeReport(&rep)
	if err := ValidateReport(&rep); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("playtest: read report %q: %w", path, err)
	}
	rep, err := ParseReport(data)
	if err != nil {
		return nil, fmt.Errorf("playtest: read report %q: %w", path, err)
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
	return out
}
