package game

import (
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// Loop runs a game's updates at a fixed rate on a scope's display frames, and draws once a frame in between.
//
// Each frame adds the time since the last one and runs Update once for each whole step of 1/Rate seconds it holds, zero or more times, then Draw once with how far the time left over reaches into the next step. A game moves its state in Update by the fixed dt, so it plays alike at any display rate, and Draw places what moves between the last two states by alpha, so it moves smoothly at any display rate.
//
// Run reads the fields on each frame, so a game may change them while the loop runs.
type Loop struct {
	// Rate is how many updates run a second, 60 when 0.
	Rate int
	// MaxSteps is the most updates one frame runs, Rate/10 (a tenth of a second's updates) and at least 1 when 0. A frame that would run more drops the whole steps past it, so an Update slower than its step, or a long hitch, slows the game rather than making each frame run more updates than the last.
	MaxSteps int
	// Update moves the game on by dt, which is always 1/Rate seconds.
	Update func(dt time.Duration)
	// Draw draws the frame; alpha, from 0 up to but not including 1, is how far the frame falls between the last update and the next.
	Draw func(alpha float64)
}

// Run starts the loop on cx's display frames until cx is disposed or stop is called.
//
// The loop pauses while cx's window is in the background (core.Env.Lifecycle), that is hidden, minimized or covered on a desktop and the app in the background on a phone, and resumes from the time it shows again, so the time away runs no updates.
//
// A frame's time is the engine's clock as it runs the frame, not the display's (nsgo-dev/nsgo#1048), so it varies with when the engine gets to the frame, and the loop smooths it: a frame counts as the average frame length while the time it owes the clock stays within half a frame, and pays all it owes when a frame is unlike those before, so the updates keep to the clock while the loop runs.
func (l *Loop) Run(cx *core.Scope) (stop func()) {
	r := &loopRun{loop: l, cx: cx}
	stopEffect := cx.Effect(func() {
		if cx.Env().Lifecycle() == core.LifecycleBackground {
			r.pause()
		} else {
			r.resume()
		}
	})
	return func() {
		r.pause()
		stopEffect()
	}
}

// loopRun is one Run of a Loop.
type loopRun struct {
	loop *Loop
	cx   *core.Scope
	// cancel cancels the frame the run waits for, nil while it is paused.
	cancel func()
	// last is the time of the last frame, or of the start or resumption.
	last time.Time
	// period is the average frame length, 0 before the first frame; owed is the frame time not yet added to acc, below 0 when a frame was taken to last longer than it did.
	period, owed time.Duration
	// acc is the time added but not yet run as updates.
	acc time.Duration
}

// resume starts the run's frames from now, unless they run.
func (r *loopRun) resume() {
	if r.cancel != nil {
		return
	}
	r.last = r.cx.Now()
	r.owed = 0
	// cx owns the frame rather than the effect that resumes, so what Update and Draw make lives as long as cx, as it would in a frame cx asked for itself.
	r.cx.Run(func() { r.cancel = r.cx.RequestAnimationFrame(r.frame) })
}

// pause stops the run's frames until resume.
func (r *loopRun) pause() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

func (r *loopRun) frame(at time.Time) {
	r.cancel = r.cx.RequestAnimationFrame(r.frame)
	r.acc += r.smooth(at.Sub(r.last))
	r.last = at
	step := r.loop.step()
	steps := 0
	for r.acc >= step {
		if steps == r.loop.maxSteps() {
			r.acc %= step
			break
		}
		r.acc -= step
		steps++
		if r.loop.Update != nil {
			r.loop.Update(step)
		}
		// Update may have stopped the loop.
		if r.cancel == nil {
			return
		}
	}
	if r.loop.Draw != nil {
		r.loop.Draw(float64(r.acc) / float64(step))
	}
}

// smooth adds the frame length dt to owed and returns the time the frame adds, taking it off owed: the average frame length while owed stays within half of it, else everything owed, starting the average over from dt.
func (r *loopRun) smooth(dt time.Duration) time.Duration {
	r.owed += dt
	if r.period <= 0 || (r.owed-r.period).Abs() > r.period/2 {
		add := r.owed
		r.period = dt
		r.owed = 0
		return add
	}
	r.period += (dt - r.period) / 8
	r.owed -= r.period
	return r.period
}

// step is the time one update covers.
func (l *Loop) step() time.Duration {
	return time.Second / time.Duration(l.rate())
}

func (l *Loop) rate() int {
	if l.Rate <= 0 {
		return 60
	}
	return l.Rate
}

func (l *Loop) maxSteps() int {
	if l.MaxSteps > 0 {
		return l.MaxSteps
	}
	return max(1, l.rate()/10)
}
