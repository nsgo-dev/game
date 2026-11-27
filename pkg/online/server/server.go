// Package server runs one or more matches of a game at fixed tick rates in a
// plain Go process, without an NSGo app, window or core.Scope.
//
// The Update function a game gives game.Loop runs unchanged here: each match
// steps it with the same fixed dt, so dedicated-server code and client-side
// prediction share one simulation.
package server

import (
	"context"
	"errors"
	"runtime/debug"
	"strconv"
	"time"
)

// Match is one game simulation stepped at Rate ticks per second.
type Match struct {
	// Name identifies the match in overrun reports and per-match metrics; a
	// Runner replaces an empty name with match_<index> when it starts.
	Name string
	// Rate is how many updates run a second, 60 when 0 or negative.
	Rate int
	// Update moves the match on by dt, always 1/Rate seconds. It runs on the
	// runner's goroutine and must return promptly.
	Update func(dt time.Duration)
	// OnStop runs once after the match's final tick when the context ends,
	// for notifying peers or flushing state. Optional.
	OnStop func()
}

// Runner steps the matches in Matches concurrently, each at its own rate,
// until its context is canceled.
type Runner struct {
	// Matches are the matches to run. A nil Update is an error.
	Matches []*Match
	// Health, when set, records every match tick's duration in the health's
	// nsgo_server_match_tick_seconds histogram. Wrap OnOverrun with the
	// health's OnOverrun to count overruns and missed ticks per match too.
	Health *Health
	// OnOverrun, when set, reports a tick that finished past the next tick's
	// deadline. lag is the time past the deadline and missed is how many whole
	// intervals that covers (at least 1). The runner does not catch up the
	// missed ticks: the deadline resets to one interval from when the slow
	// update returns, so a slow update slows the match instead of running a
	// burst.
	OnOverrun func(m *Match, lag time.Duration, missed int)
	// OnPanic, when set, reports a panic recovered from a match's Update or
	// OnStop and chooses the outcome. With StopMatch (the zero value, also
	// the default when OnPanic is nil) only the panicking match ends: it
	// never ticks again, its OnStop runs once, and the runner keeps going.
	// With Crash every match stops, every OnStop runs, and Run re-panics
	// with the recovered value.
	OnPanic func(p Panic) PanicAction
}

// interval returns the match's fixed step, defaulting to 60 Hz.
func (m *Match) interval() time.Duration {
	rate := m.Rate
	if rate <= 0 {
		rate = 60
	}
	step := time.Duration(int64(time.Second) / int64(rate))
	if step <= 0 {
		return time.Nanosecond
	}
	return step
}

// Run starts every match and blocks until ctx is canceled and each match has
// finished its current tick and run OnStop. It returns an error without
// starting anything when a match has no Update.
func (r *Runner) Run(ctx context.Context) error {
	if len(r.Matches) == 0 {
		return errors.New("server: no matches to run")
	}
	for _, m := range r.Matches {
		if m == nil || m.Update == nil {
			return errors.New("server: a match has no Update function")
		}
	}
	for i, m := range r.Matches {
		if m.Name == "" {
			m.Name = "match_" + strconv.Itoa(i)
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ps := &panicState{}
	done := make(chan struct{}, len(r.Matches))
	for _, m := range r.Matches {
		go func(m *Match) {
			defer func() {
				r.runOnStop(m, cancel, ps)
				done <- struct{}{}
			}()
			r.runMatch(ctx, m, cancel, ps)
		}(m)
	}
	for range r.Matches {
		<-done
	}
	if v := ps.crashValue(); v != nil {
		panic(v)
	}
	return nil
}

func (r *Runner) runMatch(ctx context.Context, m *Match, cancel context.CancelFunc, ps *panicState) {
	step := m.interval()
	next := time.Now().Add(step)
	timer := time.NewTimer(step)
	defer timer.Stop()
	var tick uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return
		}
		tick++
		start := time.Now()
		if !r.guardUpdate(m, step, tick, cancel, ps) {
			return
		}
		if r.Health != nil {
			r.Health.ObserveMatchTick(m.Name, time.Since(start))
		}
		now := time.Now()
		next = next.Add(step)
		if now.After(next) {
			if r.OnOverrun != nil {
				lag := now.Sub(next)
				missed := int(lag / step)
				if missed < 1 {
					missed = 1
				}
				r.OnOverrun(m, lag, missed)
				now = time.Now()
			}
			if ctx.Err() != nil {
				return
			}
			next = now.Add(step)
			timer.Reset(step)
			continue
		}
		if ctx.Err() != nil {
			return
		}
		timer.Reset(next.Sub(now))
	}
}

// guardUpdate runs one tick of m.Update, recovering a panic into handlePanic.
// It reports whether the match may keep ticking.
func (r *Runner) guardUpdate(m *Match, dt time.Duration, tick uint64, cancel context.CancelFunc, ps *panicState) (ok bool) {
	defer func() {
		if v := recover(); v != nil {
			r.handlePanic(m, v, tick, cancel, ps)
			ok = false
		}
	}()
	m.Update(dt)
	return true
}

// runOnStop runs m.OnStop once, recovering and reporting a panic the same way
// a panicking Update is, so a failing OnStop never kills the process.
func (r *Runner) runOnStop(m *Match, cancel context.CancelFunc, ps *panicState) {
	if m.OnStop == nil {
		return
	}
	defer func() {
		if v := recover(); v != nil {
			r.handlePanic(m, v, 0, cancel, ps)
		}
	}()
	m.OnStop()
}

// handlePanic reports a recovered panic through OnPanic and applies the chosen
// action: StopMatch ends only this match, Crash stops every match and makes
// Run re-panic once all OnStop callbacks have run.
func (r *Runner) handlePanic(m *Match, v any, tick uint64, cancel context.CancelFunc, ps *panicState) {
	action := StopMatch
	if r.OnPanic != nil {
		action = r.OnPanic(Panic{Match: m, Value: v, Stack: debug.Stack(), Tick: tick, Time: time.Now()})
	}
	if action == Crash {
		ps.set(v)
		cancel()
	}
}
