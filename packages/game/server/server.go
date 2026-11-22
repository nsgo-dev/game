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
	"time"
)

// Match is one game simulation stepped at Rate ticks per second.
type Match struct {
	// Name identifies the match in overrun reports; optional.
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
	// OnOverrun, when set, reports a tick whose Update ran longer than its
	// interval. lag is the time past the interval and missed is how many whole
	// intervals that covers. The runner does not catch up the missed ticks: the
	// next tick is scheduled from now, so a slow update slows the match instead
	// of running a burst.
	OnOverrun func(m *Match, lag time.Duration, missed int)
}

// interval returns the match's fixed step, defaulting to 60 Hz.
func (m *Match) interval() time.Duration {
	rate := m.Rate
	if rate <= 0 {
		rate = 60
	}
	return time.Duration(int64(time.Second) / int64(rate))
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

	done := make(chan struct{}, len(r.Matches))
	for _, m := range r.Matches {
		go func(m *Match) {
			defer func() {
				if m.OnStop != nil {
					m.OnStop()
				}
				done <- struct{}{}
			}()
			r.runMatch(ctx, m)
		}(m)
	}
	for range r.Matches {
		<-done
	}
	return nil
}

func (r *Runner) runMatch(ctx context.Context, m *Match) {
	step := m.interval()
	timer := time.NewTimer(step)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return
		}
		start := time.Now()
		m.Update(step)
		if elapsed := time.Since(start); elapsed > step && r.OnOverrun != nil {
			lag := elapsed - step
			missed := int(lag / step)
			if missed < 1 {
				missed = 1
			}
			r.OnOverrun(m, lag, missed)
		}
		if ctx.Err() != nil {
			return
		}
		timer.Reset(step)
	}
}
