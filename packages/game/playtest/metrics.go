package playtest

import (
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game/debug"
)

// Run outcome values reported by Collector and RunReport.Outcome.
const (
	OutcomeRunning  = "running"
	OutcomeFinished = "finished"
	OutcomeFailed   = "failed"
	OutcomeTimeout  = "timeout"
)

// Event is one positioned, step-stamped occurrence reported by the game during a playtest run (such as a death or pickup).
type Event struct {
	// Kind names the event, such as "death" or "pickup".
	Kind string `yaml:"kind"`
	// Step is the zero-based fixed simulation step on which the event happened.
	Step int `yaml:"step"`
	// TimeSeconds is the simulated elapsed time in seconds (Step / Rate).
	TimeSeconds float64 `yaml:"timeSeconds"`
	// X and Y are the world coordinates where the event occurred.
	X float64 `yaml:"x"`
	Y float64 `yaml:"y"`
	// Tag is an optional label identifying the cause or entity, such as "slime_pit" or "coin_03".
	Tag string `yaml:"tag,omitempty"`
}

// PerfSummary holds frame timing and allocation telemetry captured from the perf readout (debug.FrameStats and debug.GCMonitor).
type PerfSummary struct {
	// SampleCount is the number of frames sampled.
	SampleCount int `yaml:"sampleCount,omitempty"`
	// FrameMs is the mean total frame duration in milliseconds.
	FrameMs float64 `yaml:"frameMs,omitempty"`
	// UpdateMs is the mean fixed-update duration in milliseconds.
	UpdateMs float64 `yaml:"updateMs,omitempty"`
	// DrawMs is the mean draw duration in milliseconds.
	DrawMs float64 `yaml:"drawMs,omitempty"`
	// P50FrameMs is the 50th percentile frame duration in milliseconds.
	P50FrameMs float64 `yaml:"p50FrameMs,omitempty"`
	// P95FrameMs is the 95th percentile frame duration in milliseconds.
	P95FrameMs float64 `yaml:"p95FrameMs,omitempty"`
	// P99FrameMs is the 99th percentile frame duration in milliseconds.
	P99FrameMs float64 `yaml:"p99FrameMs,omitempty"`
	// MaxFrameMs is the peak frame duration in milliseconds.
	MaxFrameMs float64 `yaml:"maxFrameMs,omitempty"`
	// AllocsPerFrame is the mean heap allocations per frame.
	AllocsPerFrame float64 `yaml:"allocsPerFrame,omitempty"`
}

// Collector is the small Go API a game calls during a playtest run to report positioned events, counters, timers, outcome and perf readouts.
//
// Every method on Collector is safe to call on a nil receiver, so game code can call it unconditionally in both playtest runs and live play without guarding every call site.
type Collector struct {
	rate         int
	step         int
	outcome      string
	reason       string
	events       []Event
	counters     map[string]float64
	activeTimers map[string]int
	timers       map[string]float64
	perf         *PerfSummary
}

// NewCollector returns a fresh Collector for a run stepping at rate steps per second (60 when rate <= 0).
func NewCollector(rate int) *Collector {
	if rate <= 0 {
		rate = defaultStepRate
	}
	return &Collector{
		rate:    rate,
		outcome: OutcomeRunning,
	}
}

// SetStep updates the collector's current zero-based simulation step. Run calls this before each step.
func (c *Collector) SetStep(step int) {
	if c == nil || step < 0 {
		return
	}
	c.step = step
}

// Step returns the collector's current zero-based simulation step.
func (c *Collector) Step() int {
	if c == nil {
		return 0
	}
	return c.step
}

// Rate returns the fixed step rate in steps per second.
func (c *Collector) Rate() int {
	if c == nil || c.rate <= 0 {
		return defaultStepRate
	}
	return c.rate
}

// Elapsed returns the deterministic simulated time corresponding to the current step.
func (c *Collector) Elapsed() time.Duration {
	if c == nil {
		return 0
	}
	return time.Duration(c.step) * (time.Second / time.Duration(c.Rate()))
}

// Event records a positioned event (such as "death" or "pickup") at world coordinates (x, y) on the current simulation step, with an optional tag.
func (c *Collector) Event(kind string, x, y float64, tag ...string) {
	if c == nil || kind == "" || !isFinite(x) || !isFinite(y) {
		return
	}
	var label string
	if len(tag) > 0 {
		label = tag[0]
	}
	c.events = append(c.events, Event{
		Kind:        kind,
		Step:        c.step,
		TimeSeconds: cleanZero(float64(c.step) / float64(c.Rate())),
		X:           cleanZero(x),
		Y:           cleanZero(y),
		Tag:         label,
	})
}

// Count adds delta to the named counter. Non-finite deltas are ignored.
func (c *Collector) Count(name string, delta float64) {
	if c == nil || name == "" || !isFinite(delta) {
		return
	}
	if c.counters == nil {
		c.counters = make(map[string]float64)
	}
	c.counters[name] = cleanZero(c.counters[name] + delta)
}

// Inc increments the named counter by 1.
func (c *Collector) Inc(name string) {
	c.Count(name, 1)
}

// SetCounter sets the named counter to value. Non-finite values are ignored.
func (c *Collector) SetCounter(name string, value float64) {
	if c == nil || name == "" || !isFinite(value) {
		return
	}
	if c.counters == nil {
		c.counters = make(map[string]float64)
	}
	c.counters[name] = cleanZero(value)
}

// Counter returns the current value of the named counter, or 0 if unset.
func (c *Collector) Counter(name string) float64 {
	if c == nil || c.counters == nil {
		return 0
	}
	return c.counters[name]
}

// StartTimer starts a deterministic simulated-time timer under name at the current step.
func (c *Collector) StartTimer(name string) {
	if c == nil || name == "" {
		return
	}
	if c.activeTimers == nil {
		c.activeTimers = make(map[string]int)
	}
	c.activeTimers[name] = c.step
}

// StopTimer stops the named timer, records its elapsed simulated seconds, and returns the elapsed duration.
// Calling StopTimer on a timer that was not started returns 0 without recording a sample.
func (c *Collector) StopTimer(name string) time.Duration {
	if c == nil || name == "" || c.activeTimers == nil {
		return 0
	}
	startStep, ok := c.activeTimers[name]
	if !ok {
		return 0
	}
	delete(c.activeTimers, name)
	steps := c.step - startStep
	if steps < 0 {
		steps = 0
	}
	secs := cleanZero(float64(steps) / float64(c.Rate()))
	if c.timers == nil {
		c.timers = make(map[string]float64)
	}
	c.timers[name] = secs
	return time.Duration(steps) * (time.Second / time.Duration(c.Rate()))
}

// RecordTimer directly records a simulated duration sample for timer name. Negative durations are clamped to 0.
func (c *Collector) RecordTimer(name string, d time.Duration) {
	if c == nil || name == "" {
		return
	}
	if d < 0 {
		d = 0
	}
	if c.timers == nil {
		c.timers = make(map[string]float64)
	}
	c.timers[name] = cleanZero(d.Seconds())
}

// Timer returns the recorded duration in seconds for timer name and whether it was recorded.
func (c *Collector) Timer(name string) (float64, bool) {
	if c == nil || c.timers == nil {
		return 0, false
	}
	v, ok := c.timers[name]
	return v, ok
}

// Finish marks the run as finished (cleared / won). Calling Finish or Fail stops further stepping in Run.
func (c *Collector) Finish() {
	if c == nil || c.outcome != OutcomeRunning {
		return
	}
	c.outcome = OutcomeFinished
}

// Fail marks the run as failed with an optional human-readable reason (such as "out of lives").
func (c *Collector) Fail(reason string) {
	if c == nil || c.outcome != OutcomeRunning {
		return
	}
	c.outcome = OutcomeFailed
	c.reason = reason
}

// Done reports whether the run has ended via Finish, Fail or timeout.
func (c *Collector) Done() bool {
	if c == nil {
		return false
	}
	return c.outcome != OutcomeRunning
}

// Outcome returns the run's current outcome ("running", "finished", "failed" or "timeout").
func (c *Collector) Outcome() string {
	if c == nil || c.outcome == "" {
		return OutcomeRunning
	}
	return c.outcome
}

// Reason returns the optional failure reason passed to Fail.
func (c *Collector) Reason() string {
	if c == nil {
		return ""
	}
	return c.reason
}

// Events returns a copy of all positioned events recorded so far.
func (c *Collector) Events() []Event {
	if c == nil || len(c.events) == 0 {
		return nil
	}
	return slices.Clone(c.events)
}

// RecordPerf captures frame timing and allocation telemetry from debug.FrameStats and debug.GCMonitor, matching the calculations of game_perf_readout.
func (c *Collector) RecordPerf(stats *debug.FrameStats, gc *debug.GCMonitor) {
	if c == nil {
		return
	}
	summary, ok := PerfFromStats(stats, gc)
	if !ok {
		return
	}
	c.SetPerf(summary)
}

// SetPerf stores an explicit PerfSummary on the collector.
func (c *Collector) SetPerf(summary PerfSummary) {
	if c == nil {
		return
	}
	cp := sanitizePerf(summary)
	c.perf = &cp
}

// Perf returns a copy of the recorded PerfSummary, or nil when none was recorded.
func (c *Collector) Perf() *PerfSummary {
	if c == nil || c.perf == nil {
		return nil
	}
	cp := *c.perf
	return &cp
}

// PerfFromStats builds a PerfSummary from debug.FrameStats and debug.GCMonitor using the same averaging and percentile definitions as game/mcp's game_perf_readout.
// It returns false when neither stats nor gc holds any samples.
func PerfFromStats(stats *debug.FrameStats, gc *debug.GCMonitor) (PerfSummary, bool) {
	var out PerfSummary
	hasAny := false
	if stats != nil && stats.Len() > 0 {
		hasAny = true
		n := stats.Len()
		out.SampleCount = n
		avg := stats.Average()
		out.FrameMs = durationMs(avg.Total)
		out.UpdateMs = durationMs(avg.Update)
		out.DrawMs = durationMs(avg.Draw)

		samples := stats.Samples(make([]debug.FrameSample, 0, n))
		totals := make([]time.Duration, len(samples))
		for i, s := range samples {
			totals[i] = s.Total
		}
		slices.Sort(totals)
		out.P50FrameMs = durationMs(percentileDuration(totals, 50))
		out.P95FrameMs = durationMs(percentileDuration(totals, 95))
		out.P99FrameMs = durationMs(percentileDuration(totals, 99))
		out.MaxFrameMs = durationMs(totals[len(totals)-1])
	}
	if gc != nil && gc.Len() > 0 {
		hasAny = true
		out.AllocsPerFrame = cleanZero(gc.AllocsPerFrame())
	}
	if !hasAny {
		return PerfSummary{}, false
	}
	return sanitizePerf(out), true
}

func sanitizePerf(p PerfSummary) PerfSummary {
	clean := func(v float64) float64 {
		if !isFinite(v) || v <= 0 {
			return 0
		}
		return cleanZero(v)
	}
	if p.SampleCount < 0 {
		p.SampleCount = 0
	}
	p.FrameMs = clean(p.FrameMs)
	p.UpdateMs = clean(p.UpdateMs)
	p.DrawMs = clean(p.DrawMs)
	p.P50FrameMs = clean(p.P50FrameMs)
	p.P95FrameMs = clean(p.P95FrameMs)
	p.P99FrameMs = clean(p.P99FrameMs)
	p.MaxFrameMs = clean(p.MaxFrameMs)
	p.AllocsPerFrame = clean(p.AllocsPerFrame)
	return p
}

func (p PerfSummary) field(key string) (float64, bool) {
	switch key {
	case "frame_ms":
		return p.FrameMs, true
	case "update_ms":
		return p.UpdateMs, true
	case "draw_ms":
		return p.DrawMs, true
	case "p50_frame_ms":
		return p.P50FrameMs, true
	case "p95_frame_ms":
		return p.P95FrameMs, true
	case "p99_frame_ms":
		return p.P99FrameMs, true
	case "max_frame_ms":
		return p.MaxFrameMs, true
	case "allocs_per_frame":
		return p.AllocsPerFrame, true
	default:
		return 0, false
	}
}

func durationMs(d time.Duration) float64 {
	return cleanZero(float64(d) / float64(time.Millisecond))
}

func percentileDuration(sorted []time.Duration, pct int) time.Duration {
	n := len(sorted)
	if n == 0 {
		return 0
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
