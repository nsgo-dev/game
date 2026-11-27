package tween

import (
	"cmp"
	"math"
	"slices"
	"time"
)

type track struct {
	start time.Duration
	tw    *Tween
	order int
}

type cue struct {
	at    time.Duration
	fn    func()
	order int
}

// Timeline coordinates multiple tweens and timed callback cues along a single deterministic clock, supporting seeking, forward and reverse playback, and looping. Its zero value is ready to use.
type Timeline struct {
	tracks []track
	cues   []cue

	pos       time.Duration
	playing   bool
	reverse   bool
	loops     int // 0 = plays once, -1 wraps forever, N>0 = runs remaining counting the current pass
	speed     float64
	speedSet  bool
	done      bool
	edgeFired bool
	seekGen   uint64
}

// NewTimeline returns an empty Timeline ready to add tracks and cues to.
func NewTimeline() *Timeline { return &Timeline{} }

// Add places tw on the timeline starting at offset at. Nil tweens are ignored; negative offsets are clamped to 0.
func (tl *Timeline) Add(at time.Duration, tw *Tween) *Timeline {
	if tw == nil {
		return tl
	}
	if at < 0 {
		at = 0
	}
	tl.tracks = append(tl.tracks, track{
		start: at,
		tw:    tw,
		order: len(tl.tracks),
	})
	return tl
}

// Append places tw on the timeline starting at its current Duration. Nil tweens are ignored.
func (tl *Timeline) Append(tw *Tween) *Timeline {
	return tl.Add(tl.Duration(), tw)
}

// Call schedules fn to run when playback crosses timestamp at. Nil callbacks are ignored; negative offsets are clamped to 0.
func (tl *Timeline) Call(at time.Duration, fn func()) *Timeline {
	if fn == nil {
		return tl
	}
	if at < 0 {
		at = 0
	}
	tl.cues = append(tl.cues, cue{
		at:    at,
		fn:    fn,
		order: len(tl.cues),
	})
	return tl
}

// Duration returns the total span of the timeline across all tracks and cues.
func (tl *Timeline) Duration() time.Duration {
	var dur time.Duration
	for _, tr := range tl.tracks {
		if end := addDuration(tr.start, tr.tw.Duration()); end > dur {
			dur = end
		}
	}
	for _, c := range tl.cues {
		if c.at > dur {
			dur = c.at
		}
	}
	return dur
}

// Time returns the timeline's current playback timestamp.
func (tl *Timeline) Time() time.Duration { return tl.pos }

// Playing reports whether the timeline is currently playing.
func (tl *Timeline) Playing() bool { return tl.playing }

// Done reports whether a non-looping timeline has reached the end of its playback direction.
func (tl *Timeline) Done() bool { return tl.done }

// Play starts or resumes playback in the current direction.
func (tl *Timeline) Play() {
	dur := tl.Duration()
	if tl.loops >= 0 && tl.loops <= 1 && tl.done {
		if !tl.reverse && tl.pos >= dur {
			return
		}
		if tl.reverse && tl.pos <= 0 {
			return
		}
	}
	tl.done = false
	tl.playing = true
}

// Pause suspends playback at the current timestamp.
func (tl *Timeline) Pause() {
	tl.playing = false
}

// Reverse sets whether playback runs backward (from Duration down to 0).
func (tl *Timeline) Reverse(reverse bool) {
	tl.reverse = reverse
	if tl.done {
		dur := tl.Duration()
		if (!reverse && tl.pos < dur) || (reverse && tl.pos > 0) {
			tl.done = false
		}
	}
}

// SetLoop sets whether playback wraps around on reaching the end.
func (tl *Timeline) SetLoop(loop bool) {
	if loop {
		tl.SetLoops(0)
	} else {
		tl.SetLoops(1)
	}
}

// SetLoops sets how many runs the timeline plays: n <= 0 wraps around forever (the same
// as SetLoop(true)), n == 1 plays once (the same as SetLoop(false)), and larger n wraps
// n-1 more times. Calling it while playing counts the remaining runs, including the
// current pass. Unless n is 1, setting loops clears a finished timeline's done state.
func (tl *Timeline) SetLoops(n int) {
	if n <= 0 {
		tl.loops = -1
		tl.done = false
		return
	}
	tl.loops = n
	if n > 1 {
		tl.done = false
	}
}

// SetSpeed scales every step by s, so the timeline runs in slow motion (0.25) or fast
// forward. A scale of 0 stops advancement; negative values and NaN are treated as 1.
func (tl *Timeline) SetSpeed(s float64) {
	if math.IsNaN(s) || s < 0 {
		s = 1
	}
	tl.speed = s
	tl.speedSet = true
}

// Restart jumps to the start of the current playback direction (0 forward, Duration in reverse), applies all tracks, and starts playing.
func (tl *Timeline) Restart() {
	if tl.reverse {
		tl.Seek(tl.Duration())
	} else {
		tl.Seek(0)
	}
	tl.done = false
	tl.playing = true
}

// Seek moves the timeline to timestamp at (clamped to [0, Duration()]) and puts every track — before, during, and after its window — at the exact value it holds at that time without firing cues.
func (tl *Timeline) Seek(at time.Duration) {
	dur := tl.Duration()
	if at < 0 {
		at = 0
	} else if at > dur {
		at = dur
	}
	tl.pos = at
	tl.edgeFired = false
	tl.seekGen++
	tl.applyAt(at)

	if tl.loops < 0 || tl.loops > 1 {
		tl.done = false
	} else if tl.reverse {
		tl.done = dur > 0 && at <= 0
	} else {
		tl.done = dur > 0 && at >= dur
	}
	if tl.done {
		tl.playing = false
	}
}

// Update advances the timeline by dt when playing, updating tracks and firing any cues crossed during the step.
func (tl *Timeline) Update(dt time.Duration) {
	if !tl.playing || dt <= 0 {
		return
	}
	if tl.speedSet {
		dt = time.Duration(float64(dt) * tl.speed)
	}
	if dt <= 0 {
		return
	}
	if tl.done {
		tl.playing = false
		return
	}

	dur := tl.Duration()
	if dur == 0 {
		gen := tl.seekGen
		tl.pos = 0
		tl.applyAt(0)
		if !tl.edgeFired {
			tl.edgeFired = true
			if tl.reverse {
				tl.fireCuesReverse(0, 0, true, gen)
			} else {
				tl.fireForwardEvents(0, 0, true, gen)
			}
		}
		if tl.seekGen != gen || !tl.playing {
			return
		}
		if tl.loops == 0 || tl.loops == 1 {
			tl.loops = 0
			tl.done = true
			tl.playing = false
		}
		return
	}

	rem := dt
	for rem > 0 && tl.playing && !tl.done {
		gen := tl.seekGen
		rev := tl.reverse

		if !rev {
			prev := tl.pos
			toEnd := dur - prev
			step := rem
			if step > toEnd {
				step = toEnd
			}
			next := prev + step
			includeStart := prev == 0 && !tl.edgeFired
			tl.pos = next
			tl.edgeFired = true
			tl.applyAt(next)
			tl.fireForwardEvents(prev, next, includeStart, gen)
			if tl.seekGen != gen || !tl.playing {
				return
			}
			rem -= step
			if tl.reverse != rev {
				continue
			}
			if next >= dur {
				if tl.loops < 0 || tl.loops > 1 {
					if tl.loops > 1 {
						tl.loops--
					}
					tl.pos = 0
					tl.edgeFired = false
					if rem == 0 {
						tl.applyAt(0)
					}
				} else {
					tl.loops = 0
					tl.done = true
					tl.playing = false
				}
			}
		} else {
			prev := tl.pos
			step := rem
			if step > prev {
				step = prev
			}
			next := prev - step
			includeStart := prev == dur && !tl.edgeFired
			tl.pos = next
			tl.edgeFired = true
			tl.applyAt(next)
			tl.fireCuesReverse(next, prev, includeStart, gen)
			if tl.seekGen != gen || !tl.playing {
				return
			}
			rem -= step
			if tl.reverse != rev {
				continue
			}
			if next <= 0 {
				if tl.loops < 0 || tl.loops > 1 {
					if tl.loops > 1 {
						tl.loops--
					}
					tl.pos = dur
					tl.edgeFired = false
					if rem == 0 {
						tl.applyAt(dur)
					}
				} else {
					tl.loops = 0
					tl.done = true
					tl.playing = false
				}
			}
		}
	}
}

func (tl *Timeline) applyAt(at time.Duration) {
	if len(tl.tracks) == 0 {
		return
	}
	idx := make([]int, len(tl.tracks))
	for i := range idx {
		idx[i] = i
	}
	slices.SortFunc(idx, func(a, b int) int {
		if c := cmp.Compare(tl.tracks[a].start, tl.tracks[b].start); c != 0 {
			return c
		}
		return cmp.Compare(tl.tracks[a].order, tl.tracks[b].order)
	})

	// 1. Future tracks (at < start) in reverse start order so earlier tracks take precedence when sharing a target.
	for i := len(idx) - 1; i >= 0; i-- {
		tr := tl.tracks[idx[i]]
		if at < tr.start {
			tr.tw.seekBefore()
		}
	}
	// 2. Past tracks (finite and at >= end) in forward order so chained tracks resolve sequentially.
	for _, i := range idx {
		tr := tl.tracks[i]
		if !tr.tw.infinite() {
			end := addDuration(tr.start, tr.tw.Duration())
			if at >= end {
				tr.tw.Seek(tr.tw.Duration())
			}
		}
	}
	// 3. Active tracks (start <= at < end, or infinite with start <= at) in forward order.
	for _, i := range idx {
		tr := tl.tracks[i]
		if at < tr.start {
			continue
		}
		if !tr.tw.infinite() && at >= addDuration(tr.start, tr.tw.Duration()) {
			continue
		}
		tr.tw.Seek(at - tr.start)
	}
}

type timedEvent struct {
	at       time.Duration
	priority int // 0 = track OnComplete, 1 = Call cue
	order    int
	fn       func()
}

func (tl *Timeline) fireForwardEvents(prev, next time.Duration, includeStart bool, gen uint64) {
	inRange := func(at time.Duration) bool {
		if includeStart {
			return at >= prev && at <= next
		}
		return at > prev && at <= next
	}

	var events []timedEvent
	for _, tr := range tl.tracks {
		if tr.tw.infinite() || len(tr.tw.onComplete) == 0 {
			continue
		}
		end := addDuration(tr.start, tr.tw.Duration())
		if inRange(end) {
			tw := tr.tw
			events = append(events, timedEvent{
				at:       end,
				priority: 0,
				order:    tr.order,
				fn:       tw.fireComplete,
			})
		}
	}
	for _, c := range tl.cues {
		if inRange(c.at) {
			events = append(events, timedEvent{
				at:       c.at,
				priority: 1,
				order:    c.order,
				fn:       c.fn,
			})
		}
	}
	if len(events) == 0 {
		return
	}
	slices.SortFunc(events, func(a, b timedEvent) int {
		if c := cmp.Compare(a.at, b.at); c != 0 {
			return c
		}
		if c := cmp.Compare(a.priority, b.priority); c != 0 {
			return c
		}
		return cmp.Compare(a.order, b.order)
	})
	for _, ev := range events {
		if tl.seekGen != gen || !tl.playing {
			return
		}
		ev.fn()
	}
}

func (tl *Timeline) fireCuesReverse(next, prev time.Duration, includeStart bool, gen uint64) {
	inRange := func(at time.Duration) bool {
		if includeStart {
			return at >= next && at <= prev
		}
		return at >= next && at < prev
	}

	var matched []cue
	for _, c := range tl.cues {
		if inRange(c.at) {
			matched = append(matched, c)
		}
	}
	if len(matched) == 0 {
		return
	}
	slices.SortFunc(matched, func(a, b cue) int {
		if c := cmp.Compare(b.at, a.at); c != 0 {
			return c
		}
		return cmp.Compare(b.order, a.order)
	})
	for _, c := range matched {
		if tl.seekGen != gen || !tl.playing {
			return
		}
		c.fn()
	}
}
