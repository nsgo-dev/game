package flow

import "time"

// Timers schedules one-shot and repeating callbacks on the game clock rather than the wall clock.
//
// Call Update on each fixed step of a game.Loop; timers pause automatically while the loop is paused or while Paused is true. The zero value is ready to use.
type Timers struct {
	// Paused stops Update from advancing timers when true.
	Paused bool

	timers []*Timer
	nextID uint64
	depth  int
}

// Timer is a handle to a callback scheduled on a Timers scheduler.
type Timer struct {
	id        uint64
	remaining time.Duration
	interval  time.Duration
	repeat    bool
	active    bool
	fn        func()
}

// After schedules fn to run once after d of game time has elapsed. A non-positive d fires on the next Update step.
func (ts *Timers) After(d time.Duration, fn func()) *Timer {
	return ts.add(max(0, d), 0, false, fn)
}

// Every schedules fn to run each time d of game time elapses, carrying any overshoot into the next period so repeating timers do not drift. A non-positive d fires once per Update step.
func (ts *Timers) Every(d time.Duration, fn func()) *Timer {
	if d < 0 {
		d = 0
	}
	return ts.add(d, d, true, fn)
}

func (ts *Timers) add(remaining, interval time.Duration, repeat bool, fn func()) *Timer {
	ts.nextID++
	t := &Timer{
		id:        ts.nextID,
		remaining: remaining,
		interval:  interval,
		repeat:    repeat,
		active:    true,
		fn:        fn,
	}
	ts.timers = append(ts.timers, t)
	return t
}

// SetPaused sets whether Update advances scheduled timers.
func (ts *Timers) SetPaused(paused bool) {
	ts.Paused = paused
}

// Clear cancels all scheduled timers.
func (ts *Timers) Clear() {
	for _, t := range ts.timers {
		t.Cancel()
	}
	if ts.depth == 0 {
		clear(ts.timers)
		ts.timers = ts.timers[:0]
	}
}

// Update advances all active timers by dt and fires expired ones in registration order.
//
// Callbacks may safely cancel existing timers or schedule new ones; newly scheduled timers begin counting on the following Update step.
func (ts *Timers) Update(dt time.Duration) {
	if ts.Paused || dt <= 0 {
		return
	}
	ts.depth++
	defer func() {
		ts.depth--
		if ts.depth == 0 {
			ts.compact()
		}
	}()

	n := len(ts.timers)
	for i := 0; i < n; i++ {
		if ts.Paused {
			break
		}
		t := ts.timers[i]
		if !t.active {
			continue
		}
		t.remaining -= dt
		if t.remaining > 0 {
			continue
		}
		if !t.repeat {
			fn := t.fn
			t.active = false
			t.remaining = 0
			t.fn = nil
			if fn != nil {
				fn()
			}
			continue
		}
		if t.interval <= 0 {
			t.remaining = 0
			if t.fn != nil {
				t.fn()
			}
			continue
		}
		for t.active && !ts.Paused && t.remaining <= 0 {
			t.remaining += t.interval
			if t.fn != nil {
				t.fn()
			}
		}
	}
}

func (ts *Timers) compact() {
	w := 0
	for _, t := range ts.timers {
		if t.active {
			ts.timers[w] = t
			w++
		}
	}
	clear(ts.timers[w:])
	ts.timers = ts.timers[:w]
}

// Cancel stops the timer so it will not fire again. Cancelling a nil or inactive timer is a no-op.
func (t *Timer) Cancel() {
	if t == nil {
		return
	}
	t.active = false
	t.remaining = 0
	t.fn = nil
}

// Active reports whether the timer is still scheduled to fire.
func (t *Timer) Active() bool {
	return t != nil && t.active
}

// Remaining returns the game time left before the timer next fires, or 0 when inactive.
func (t *Timer) Remaining() time.Duration {
	if t == nil || !t.active || t.remaining <= 0 {
		return 0
	}
	return t.remaining
}
