package rhythm

import (
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

// Defaults of a Calibrator.
const (
	// DefaultInterval is the time between cues: 80 beats a minute, so a tap up to 375 milliseconds off its seed still finds its cue.
	DefaultInterval = 750 * time.Millisecond
	// DefaultTaps is how many taps a calibration counts.
	DefaultTaps = 16
	// DefaultWarmup is how many first taps it ignores while the player finds the beat.
	DefaultWarmup = 4
	// DefaultLead is the time from Calibration.Start to the first cue.
	DefaultLead = time.Second
	// SteadySpread is the most a result's spread may be for Steady to hold.
	SteadySpread = 30 * time.Millisecond
)

// Calibrator measures an offset from taps to cues: cues come every Interval from a start time, the player taps along, and the median of how late each tap is, after the first Warmup, is the offset. It keeps no clock of its own, so the times it is given decide everything and a test can drive it exactly.
type Calibrator struct {
	// Interval is the time between cues; DefaultInterval when 0.
	Interval time.Duration
	// Taps is how many taps it counts; DefaultTaps when 0.
	Taps int
	// Warmup is how many first taps it ignores; DefaultWarmup when 0, none when negative.
	Warmup int
	// Seed is the offset expected before measuring, such as the output latency audio.OutputLatency reads: a tap is matched with the cue nearest to it less Seed.
	Seed    time.Duration
	start   time.Time
	started bool
	// seen counts the taps matched, warmup included; errs are the counted ones' offsets.
	seen int
	errs []time.Duration
	// shown are when cues were really presented, by cue, where that differs from their nominal time.
	shown map[int]time.Time
}

// CalibrationResult is what a calibration measured.
type CalibrationResult struct {
	// Offset is the median of the counted taps' offsets: how late, on this device, a player taps after a cue.
	Offset time.Duration
	// Spread is the median of how far each tap is from Offset; a few milliseconds for a steady player.
	Spread time.Duration
	// Taps is how many taps it counted.
	Taps int
}

// Steady reports whether the taps agree well enough to keep the result: a spread of at most SteadySpread.
func (r CalibrationResult) Steady() bool { return r.Spread <= SteadySpread }

func (c *Calibrator) interval() time.Duration {
	if c.Interval <= 0 {
		return DefaultInterval
	}
	return c.Interval
}

func (c *Calibrator) taps() int {
	if c.Taps <= 0 {
		return DefaultTaps
	}
	return c.Taps
}

func (c *Calibrator) warmup() int {
	switch {
	case c.Warmup < 0:
		return 0
	case c.Warmup == 0:
		return DefaultWarmup
	}
	return c.Warmup
}

// Start begins a calibration with cue 0 at at, forgetting any taps before.
func (c *Calibrator) Start(at time.Time) {
	c.start, c.started, c.seen = at, true, 0
	c.errs = c.errs[:0]
	clear(c.shown)
}

// Started reports whether Start was called.
func (c *Calibrator) Started() bool { return c.started }

// Cue returns the nominal time of cue k.
func (c *Calibrator) Cue(k int) time.Time { return c.start.Add(time.Duration(k) * c.interval()) }

// CueAt returns the last cue at or before now, -1 before cue 0.
func (c *Calibrator) CueAt(now time.Time) int {
	d := now.Sub(c.start)
	if !c.started || d < 0 {
		return -1
	}
	return int(d / c.interval())
}

// Pulse returns the last cue at or before now and how brightly to show it: 1 at the cue, fading to 0 a quarter of the interval later. It returns -1 and 0 before cue 0 and once the calibration is done.
func (c *Calibrator) Pulse(now time.Time) (cue int, level float64) {
	k := c.CueAt(now)
	if k < 0 || c.Done() {
		return -1, 0
	}
	fade := c.interval() / 4
	return k, max(0, 1-float64(now.Sub(c.Cue(k)))/float64(fade))
}

// Present records that cue k was really presented at at, as the first frame to show a flash or the moment a click without a scheduled start played, so taps are measured from it. Only the first time for a cue counts.
func (c *Calibrator) Present(k int, at time.Time) {
	if k < 0 {
		return
	}
	if c.shown == nil {
		c.shown = map[int]time.Time{}
	}
	if _, ok := c.shown[k]; ok {
		return
	}
	c.shown[k] = at
	for old := range c.shown {
		if old < k-8 {
			delete(c.shown, old)
		}
	}
}

// presented is when cue k was presented: as recorded, or its nominal time.
func (c *Calibrator) presented(k int) time.Time {
	if at, ok := c.shown[k]; ok {
		return at
	}
	return c.Cue(k)
}

// Tap records a tap at at and returns how late it is for its cue, the cue nearest to at less Seed, and whether it counted: not before Start, a cue before 0, during the warmup or once done.
func (c *Calibrator) Tap(at time.Time) (offset time.Duration, counted bool) {
	if !c.started || c.Done() {
		return 0, false
	}
	iv := c.interval()
	rel := at.Sub(c.start) - c.Seed + iv/2
	if rel < 0 {
		return 0, false
	}
	offset = at.Sub(c.presented(int(rel / iv)))
	c.seen++
	if c.seen <= c.warmup() {
		return offset, false
	}
	c.errs = append(c.errs, offset)
	return offset, true
}

// Count returns how many taps counted so far.
func (c *Calibrator) Count() int { return len(c.errs) }

// Done reports whether it counted Taps taps.
func (c *Calibrator) Done() bool { return len(c.errs) >= c.taps() }

// Result returns what the counted taps measured, and whether it is Done.
func (c *Calibrator) Result() (CalibrationResult, bool) {
	if len(c.errs) == 0 {
		return CalibrationResult{}, false
	}
	errs := slices.Clone(c.errs)
	med := median(errs)
	for i, e := range errs {
		errs[i] = max(e-med, med-e)
	}
	return CalibrationResult{Offset: med, Spread: median(errs), Taps: len(c.errs)}, c.Done()
}

// median sorts v and returns its median, the mean of the middle two for an even count.
func median(v []time.Duration) time.Duration {
	slices.Sort(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return v[n/2-1] + (v[n/2]-v[n/2-1])/2
}

// CalibrationKind is which offset a calibration measures.
type CalibrationKind uint8

const (
	// CalibrateAudio measures Offsets.Audio: the player taps to clicks they hear and sees nothing move.
	CalibrateAudio CalibrationKind = iota
	// CalibrateVideo measures Offsets.Video: the player taps to flashes they see and hears nothing.
	CalibrateVideo
)

// Calibration runs a Calibrator on the engine's clock: for CalibrateAudio it plays a click at each cue, scheduled ahead with audio.Voice.StartAt where audio.CanSchedule says the platform can and otherwise on the first Update at or after the cue; for CalibrateVideo it plays nothing and the screen flashes by Pulse. It seeds an audio calibration with the output latency audio.OutputLatency reads.
//
// Call Update every frame, as CalibrationScreen.Draw does, and Tap from the input that taps. Its methods run on the engine's goroutine.
type Calibration struct {
	Calibrator
	// Kind is which offset it measures.
	Kind CalibrationKind
	// Lead is the time from Start to the first cue; DefaultLead when 0.
	Lead time.Duration
	cx   *core.Scope
	// click is the sound played at each cue of an audio calibration.
	click *audio.Sound
	// next is the first cue not yet played or scheduled, and pending the clicks scheduled and not yet ended, stopped when the calibration starts over.
	next    int
	pending []*audio.Playback
	// latency is the output latency read at Start, when known.
	latency      time.Duration
	latencyKnown bool
	// run counts Starts, so a latency reading for an earlier one is dropped.
	run int
}

// NewCalibration returns a calibration of kind on cx's engine, clicking click at each cue of an audio calibration; click may be nil for a video calibration. It starts at Start.
func NewCalibration(cx *core.Scope, kind CalibrationKind, click *audio.Sound) *Calibration {
	return &Calibration{Kind: kind, cx: cx, click: click}
}

// Start begins the calibration, its first cue Lead from now, forgetting the taps of any run before and stopping the clicks it scheduled for it. An audio calibration asks for the output latency and, when the platform knows it, seeds with it.
func (c *Calibration) Start() {
	c.run++
	run := c.run
	c.stopClicks()
	if c.Kind == CalibrateAudio {
		audio.OutputLatency(c.cx, func(latency time.Duration, known bool) {
			if run != c.run || !known {
				return
			}
			c.latency, c.latencyKnown = latency, true
			if c.Count() == 0 {
				c.Seed = latency
			}
		})
	}
	lead := c.Lead
	if lead <= 0 {
		lead = DefaultLead
	}
	c.Calibrator.Start(audio.Now(c.cx).Add(lead))
	c.next = 0
	c.Update()
}

// OutputLatency returns the output latency the platform reported at Start, and whether it reported one.
func (c *Calibration) OutputLatency() (time.Duration, bool) { return c.latency, c.latencyKnown }

// Stop ends the run: it stops the clicks scheduled ahead and takes no more taps until Start, keeping the result so far. Call it when the calibration's screen goes away mid-run.
func (c *Calibration) Stop() {
	c.stopClicks()
	c.started = false
}

// stopClicks stops the clicks scheduled and not yet ended.
func (c *Calibration) stopClicks() {
	for _, p := range c.pending {
		p.Stop()
	}
	c.pending = c.pending[:0]
}

// Update plays the clicks of an audio calibration that are due: those within two intervals ahead, scheduled at their cue, where the platform schedules sounds, and otherwise the latest cue that has come, at once, recording when it played. Once the calibration is done it stops the clicks still scheduled.
func (c *Calibration) Update() {
	if c.Done() {
		c.stopClicks()
		return
	}
	if c.Kind != CalibrateAudio || c.click == nil || !c.Started() {
		return
	}
	now := audio.Now(c.cx)
	if audio.CanSchedule(c.cx) {
		c.pending = slices.DeleteFunc(c.pending, (*audio.Playback).Ended)
		horizon := now.Add(2 * c.interval())
		for ; !c.Cue(c.next).After(horizon); c.next++ {
			c.pending = append(c.pending, c.click.Play(audio.Voice{StartAt: c.Cue(c.next)}))
		}
		return
	}
	k := c.CueAt(now)
	if k < c.next {
		return
	}
	c.click.Play(audio.Voice{})
	c.Present(k, now)
	c.next = k + 1
}

// Tap records a tap now, by the engine's clock, as Calibrator.Tap does. The tap that finishes the calibration stops the clicks still scheduled.
func (c *Calibration) Tap() (offset time.Duration, counted bool) {
	offset, counted = c.Calibrator.Tap(audio.Now(c.cx))
	if counted && c.Done() {
		c.stopClicks()
	}
	return offset, counted
}

// Apply returns o with the offset this calibration measures set to its result, and whether it had a steady result to set.
func (c *Calibration) Apply(o Offsets) (Offsets, bool) {
	r, done := c.Result()
	if !done || !r.Steady() {
		return o, false
	}
	if c.Kind == CalibrateVideo {
		o.Video = r.Offset
	} else {
		o.Audio = r.Offset
	}
	return o, true
}
