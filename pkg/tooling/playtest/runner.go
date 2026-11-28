package playtest

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/random"
	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
)

// Sim is one seeded headless simulation driven by Run.
type Sim struct {
	// Step advances the simulation by one fixed step of dt.
	// Like game.Loop.Update and statedump.Sim.Step, Step must call the provided replay.ActionSource's Step method once per step, or Run returns an error.
	Step func(dt time.Duration)
	// Observe returns the current state observation for the Bot before each step (such as a PlatformerObservation).
	Observe func() any
	// FrameStats is an optional gamedebug.FrameStats whose perf readout is captured at the end of the run.
	FrameStats *gamedebug.FrameStats
	// GC is an optional gamedebug.GCMonitor whose allocation readout is captured at the end of the run.
	GC *gamedebug.GCMonitor
	// Perf is an optional callback returning a PerfSummary at the end of the run; when non-nil it overrides FrameStats and GC.
	Perf func() *PerfSummary
}

// SimFactory creates a fresh deterministic simulation for one seeded run.
// src is a *replay.Recorder wrapping the run's bot; calling src.Step() once per fixed step records every input transition into that run's replay.Recording.
// metrics is the per-run *Collector where the game reports positioned events, counters, timers and outcome.
type SimFactory func(seed uint64, src replay.ActionSource, metrics *Collector) (Sim, error)

// RunOptions configures Run.
type RunOptions struct {
	// Bot overrides the bot factory resolved from cfg.Bot.Profile when non-nil.
	Bot BotFactory
	// Actions overrides the watched action names recorded into each run's replay.Recording.
	// When empty, Run uses BotChannels.Channels if the bot implements it.
	Actions []string
	// Axes overrides the watched axis names recorded into each run's replay.Recording.
	// When empty, Run uses BotChannels.Channels if the bot implements it.
	Axes []string
	// RecordingsDir, when non-empty, writes each run's replay.Recording as <RecordingsDir>/run_<NNN>_seed_<seed>.replay.yaml and records the relative/slash path in RunReport.Recording.
	RecordingsDir string
	// MeasurePerf, when true and the simulation provides no perf readout of its own, measures each Step call with gamedebug.FrameStats.
	// Off by default so repeated headless runs produce byte-identical reports.
	MeasurePerf bool
}

// RunResult holds the evaluated Report and the in-memory replay.Recording of every seeded run.
type RunResult struct {
	// Report is the deterministic YAML-serializable playtest report.
	Report *Report
	// Recordings holds one replay.Recording per entry in Report.Runs, in the same order.
	Recordings []*replay.Recording
}

// Run executes every seeded run configured by cfg against makeSim, records each run as a replay.Recording, evaluates every metric and goal, and returns the Report and recordings.
func Run(cfg *Config, makeSim SimFactory, opts RunOptions) (*RunResult, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if makeSim == nil {
		return nil, errors.New("playtest: nil makeSim")
	}

	seeds := cfg.Runs.ResolveSeeds()
	rate := cfg.Runs.EffectiveRate()
	dt := time.Second / time.Duration(rate)
	maxSteps := cfg.Runs.MaxSteps()

	if opts.RecordingsDir != "" {
		if err := os.MkdirAll(opts.RecordingsDir, 0o755); err != nil {
			return nil, fmt.Errorf("playtest: create recordings dir %q: %w", opts.RecordingsDir, err)
		}
	}

	runs := make([]RunReport, 0, len(seeds))
	collectors := make([]*Collector, 0, len(seeds))
	recordings := make([]*replay.Recording, 0, len(seeds))

	for i, seed := range seeds {
		var (
			bot Bot
			err error
		)
		if opts.Bot != nil {
			bot, err = opts.Bot(cfg.Bot)
		} else {
			bot, err = NewBot(cfg.Bot)
		}
		if err != nil {
			return nil, fmt.Errorf("playtest: seed %d: %w", seed, err)
		}

		actions := opts.Actions
		axes := opts.Axes
		if len(actions) == 0 && len(axes) == 0 {
			if ch, ok := bot.(BotChannels); ok {
				actions, axes = ch.Channels()
			}
		}

		// Derive a dedicated bot stream from seed so bot jitter never perturbs the game's own random stream.
		botRNG := random.NewStreams(seed).Stream("playtest.bot")
		metrics := NewCollector(rate)

		var obsFn func() any
		botSrc := NewBotActionSource(bot, func() any {
			if obsFn != nil {
				return obsFn()
			}
			return nil
		}, botRNG, dt)
		recorder := replay.NewRecorder(botSrc, seed, rate, actions, axes)

		sim, err := makeSim(seed, recorder, metrics)
		if err != nil {
			return nil, fmt.Errorf("playtest: seed %d: make sim: %w", seed, err)
		}
		if sim.Step == nil {
			return nil, fmt.Errorf("playtest: seed %d: sim has no Step", seed)
		}
		obsFn = sim.Observe

		var measuredStats *gamedebug.FrameStats
		if opts.MeasurePerf && sim.Perf == nil && sim.FrameStats == nil {
			measuredStats = &gamedebug.FrameStats{}
		}

		stepsRun := 0
		for step := 0; step < maxSteps && !metrics.Done(); step++ {
			metrics.SetStep(step)
			if measuredStats != nil {
				t0 := time.Now()
				sim.Step(dt)
				elapsed := time.Since(t0)
				measuredStats.Record(elapsed, elapsed, 0)
			} else {
				sim.Step(dt)
			}
			stepsRun++
			if got := recorder.TotalSteps(); got != stepsRun {
				return nil, fmt.Errorf("playtest: seed %d: sim stepped ActionSource %d times after %d steps (must call src.Step once per step)", seed, got, stepsRun)
			}
		}
		metrics.SetStep(stepsRun)

		outcome := metrics.Outcome()
		if outcome == OutcomeRunning {
			outcome = OutcomeTimeout
		}

		if metrics.Perf() == nil {
			switch {
			case sim.Perf != nil:
				if p := sim.Perf(); p != nil {
					metrics.SetPerf(*p)
				}
			case sim.FrameStats != nil || sim.GC != nil:
				metrics.RecordPerf(sim.FrameStats, sim.GC)
			case measuredStats != nil:
				metrics.RecordPerf(measuredStats, nil)
			}
		}

		rec := recorder.Recording()
		recordings = append(recordings, rec)

		var recPath string
		if opts.RecordingsDir != "" {
			name := fmt.Sprintf("run_%03d_seed_%d.replay.yaml", i+1, seed)
			full := filepath.Join(opts.RecordingsDir, name)
			if err := replay.SaveFile(full, rec); err != nil {
				return nil, fmt.Errorf("playtest: seed %d: save recording: %w", seed, err)
			}
			recPath = filepath.ToSlash(filepath.Join(filepath.Base(opts.RecordingsDir), name))
		}

		rr := RunReport{
			Index:           i + 1,
			Seed:            seed,
			Outcome:         outcome,
			Reason:          metrics.Reason(),
			Steps:           stepsRun,
			DurationSeconds: cleanZero(float64(stepsRun) / float64(rate)),
			Recording:       recPath,
			Events:          metrics.Events(),
			Perf:            metrics.Perf(),
		}
		if len(metrics.counters) > 0 {
			rr.Counters = maps.Clone(metrics.counters)
		}
		if len(metrics.timers) > 0 {
			rr.Timers = maps.Clone(metrics.timers)
		}
		runs = append(runs, rr)
		collectors = append(collectors, metrics)
	}

	metricReports, heatMaps, passed := evaluateMetrics(cfg.Metrics, runs)
	seriesCharts := aggregateSeries(collectors)
	matrixCharts := aggregateMatrices(collectors)
	rep := &Report{
		Version:   ReportVersion,
		Scope:     cfg.Scope,
		Bot:       cloneBot(cfg.Bot),
		Rate:      rate,
		Speed:     cfg.Runs.EffectiveSpeed(),
		SeedCount: len(runs),
		Passed:    passed,
		Metrics:   metricReports,
		HeatMaps:  heatMaps,
		Series:    seriesCharts,
		Matrices:  matrixCharts,
		Runs:      runs,
	}
	if cfg.Scope.Project != "" {
		if brief, err := project.LoadBrief(filepath.Join(cfg.Scope.Project, project.BriefFileName)); err == nil {
			ApplyBriefTargetStatuses(rep, brief)
		}
	}
	normalizeReport(rep)
	if err := ValidateReport(rep); err != nil {
		return nil, err
	}
	return &RunResult{
		Report:     rep,
		Recordings: recordings,
	}, nil
}

func evaluateMetrics(defs []Metric, runs []RunReport) ([]MetricReport, []HeatMap, bool) {
	if len(defs) == 0 {
		return nil, nil, true
	}
	reports := make([]MetricReport, 0, len(defs))
	var heatMaps []HeatMap
	passed := true

	for _, m := range defs {
		agg := m.Measure.EffectiveAggregate()
		vals := extractMetricValues(m, runs)
		val, lo, hi := aggregateValues(agg, vals, len(runs))
		status := m.Goal.Evaluate(val)
		if status == StatusMissed {
			passed = false
		}
		reports = append(reports, MetricReport{
			Name:      m.Name,
			Label:     m.Label,
			Unit:      m.Unit,
			Aggregate: agg,
			Value:     cleanZero(val),
			Min:       cleanZero(lo),
			Max:       cleanZero(hi),
			Goal:      cloneGoal(m.Goal),
			Status:    status,
		})
		if m.Measure.HeatMap {
			hm := HeatMap{Metric: m.Name, Event: m.Measure.Key}
			for _, run := range runs {
				for _, ev := range run.Events {
					if ev.Kind == m.Measure.Key {
						hm.Points = append(hm.Points, HeatPoint{
							Seed: run.Seed,
							Step: ev.Step,
							X:    cleanZero(ev.X),
							Y:    cleanZero(ev.Y),
							Tag:  ev.Tag,
						})
					}
				}
			}
			heatMaps = append(heatMaps, hm)
		}
	}
	return reports, heatMaps, passed
}

func extractMetricValues(m Metric, runs []RunReport) []float64 {
	vals := make([]float64, 0, len(runs))
	switch m.Measure.Kind {
	case MeasureOutcome:
		for _, r := range runs {
			if r.Outcome == m.Measure.Key {
				vals = append(vals, 1)
			} else {
				vals = append(vals, 0)
			}
		}
	case MeasureEvent:
		for _, r := range runs {
			var count float64
			for _, ev := range r.Events {
				if ev.Kind == m.Measure.Key {
					count++
				}
			}
			vals = append(vals, count)
		}
	case MeasureCounter:
		for _, r := range runs {
			vals = append(vals, r.Counters[m.Measure.Key])
		}
	case MeasureTimer:
		for _, r := range runs {
			if v, ok := r.Timers[m.Measure.Key]; ok {
				vals = append(vals, v)
			}
		}
	case MeasureRatio:
		scale := 1.0
		if m.Unit == "%" {
			scale = 100.0
		}
		for _, r := range runs {
			denom := r.Counters[m.Measure.DenomKey]
			if denom > 0 {
				vals = append(vals, scale*(r.Counters[m.Measure.Key]/denom))
			} else {
				vals = append(vals, 0)
			}
		}
	case MeasurePerf:
		for _, r := range runs {
			if r.Perf == nil {
				continue
			}
			if v, ok := r.Perf.field(m.Measure.Key); ok {
				vals = append(vals, v)
			}
		}
	}
	return vals
}

func aggregateValues(agg string, vals []float64, totalRuns int) (value, lo, hi float64) {
	if len(vals) == 0 {
		return 0, 0, 0
	}
	sorted := slices.Clone(vals)
	slices.Sort(sorted)
	lo = sorted[0]
	hi = sorted[len(sorted)-1]

	var sum float64
	var nonZero int
	for _, v := range vals {
		sum += v
		if v > 0 {
			nonZero++
		}
	}

	switch agg {
	case AggCount:
		value = float64(nonZero)
	case AggSum:
		value = sum
	case AggMean:
		value = sum / float64(len(vals))
	case AggMedian:
		value = percentileFloat(sorted, 50)
	case AggMin:
		value = lo
	case AggMax:
		value = hi
	case AggP95:
		value = percentileFloat(sorted, 95)
	case AggP99:
		value = percentileFloat(sorted, 99)
	case AggRate:
		if totalRuns > 0 {
			value = 100.0 * float64(nonZero) / float64(totalRuns)
		}
	default:
		value = sum
	}
	return cleanZero(value), cleanZero(lo), cleanZero(hi)
}

func percentileFloat(sorted []float64, pct int) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if pct == 50 {
		mid := n / 2
		if n%2 == 1 {
			return sorted[mid]
		}
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	idx := (pct*(n-1) + 99) / 100
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

func aggregateSeries(collectors []*Collector) []SeriesChart {
	var names []string
	seen := make(map[string]struct{})
	for _, c := range collectors {
		if c == nil {
			continue
		}
		for _, name := range c.seriesOrder {
			if _, ok := seen[name]; !ok {
				seen[name] = struct{}{}
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return nil
	}
	out := make([]SeriesChart, 0, len(names))
	for _, name := range names {
		var (
			label, unit, agg string
			targetMax        *float64
			buckets          []string
			seenBuckets      = make(map[string]struct{})
			flags            = make(map[string]string)
			notes            = make(map[string]string)
		)
		for _, c := range collectors {
			if c == nil || c.series == nil {
				continue
			}
			s, ok := c.series[name]
			if !ok {
				continue
			}
			if label == "" && s.label != "" {
				label = s.label
			}
			if unit == "" && s.unit != "" {
				unit = s.unit
			}
			if agg == "" && s.aggregate != "" {
				agg = s.aggregate
			}
			if targetMax == nil && s.targetMax != nil {
				v := *s.targetMax
				targetMax = &v
			}
			for _, b := range s.order {
				if _, ok := seenBuckets[b]; !ok {
					seenBuckets[b] = struct{}{}
					buckets = append(buckets, b)
				}
				if fl := s.flags[b]; fl != "" {
					if flags[b] != FlagProblem {
						flags[b] = fl
					}
				}
				if nt := s.notes[b]; nt != "" && notes[b] == "" {
					notes[b] = nt
				}
			}
		}
		if agg == "" {
			agg = AggMedian
		}
		entries := make([]SeriesEntry, 0, len(buckets))
		for _, b := range buckets {
			vals := make([]float64, 0, len(collectors))
			for _, c := range collectors {
				if c != nil && c.series != nil {
					if s, ok := c.series[name]; ok {
						vals = append(vals, s.values[b])
						continue
					}
				}
				vals = append(vals, 0)
			}
			val, _, _ := aggregateValues(agg, vals, len(collectors))
			fl := flags[b]
			if targetMax != nil && fl == "" && val > *targetMax+1e-9 {
				fl = FlagProblem
			}
			entries = append(entries, SeriesEntry{
				Label: b,
				Value: cleanZero(val),
				Flag:  fl,
				Note:  notes[b],
			})
		}
		out = append(out, SeriesChart{
			Name:      name,
			Label:     label,
			Unit:      unit,
			Aggregate: agg,
			TargetMax: targetMax,
			Entries:   entries,
		})
	}
	return out
}

func aggregateMatrices(collectors []*Collector) []MatrixChart {
	var names []string
	seen := make(map[string]struct{})
	for _, c := range collectors {
		if c == nil {
			continue
		}
		for _, name := range c.matrixOrder {
			if _, ok := seen[name]; !ok {
				seen[name] = struct{}{}
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return nil
	}
	out := make([]MatrixChart, 0, len(names))
	for _, name := range names {
		var (
			label, unit string
			flagAbove   *float64
			labels      []string
			tallies     = make(map[matchupPair]matchupTally)
		)
		for _, c := range collectors {
			if c == nil || c.matrices == nil {
				continue
			}
			m, ok := c.matrices[name]
			if !ok {
				continue
			}
			if label == "" && m.label != "" {
				label = m.label
			}
			if unit == "" && m.unit != "" {
				unit = m.unit
			}
			if flagAbove == nil && m.flagAbove != nil {
				v := *m.flagAbove
				flagAbove = &v
			}
			for _, l := range m.labels {
				if !slices.Contains(labels, l) {
					labels = append(labels, l)
				}
			}
			for pair, t := range m.tallies {
				acc := tallies[pair]
				acc.wins += t.wins
				acc.total += t.total
				tallies[pair] = acc
			}
		}
		if len(labels) == 0 {
			continue
		}
		if unit == "" {
			unit = "%"
		}
		scale := 1.0
		if unit == "%" {
			scale = 100.0
		}
		cells := make([][]*float64, len(labels))
		for rIdx, rowLbl := range labels {
			cells[rIdx] = make([]*float64, len(labels))
			for cIdx, colLbl := range labels {
				t, ok := tallies[matchupPair{row: rowLbl, col: colLbl}]
				if !ok || t.total <= 0 {
					continue
				}
				v := cleanZero(scale * (t.wins / t.total))
				cells[rIdx][cIdx] = &v
			}
		}
		out = append(out, MatrixChart{
			Name:      name,
			Label:     label,
			Unit:      unit,
			FlagAbove: flagAbove,
			Labels:    labels,
			Cells:     cells,
		})
	}
	return out
}
