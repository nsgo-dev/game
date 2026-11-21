package game

import (
	"math"
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
	// Paused stops Update from advancing with display time while Draw keeps running. Unpausing resumes from the current frame without a catch-up backlog.
	Paused bool
	// TimeScale scales simulated time, 1 when 0 or less (or not finite). Update still receives the fixed 1/Rate step, so 0.5 runs half as many updates a second and 2 runs twice as many.
	TimeScale float64
	// DrawRate caps Draw to at most DrawRate calls a second, uncapped when 0 or less. Update still runs at Rate.
	DrawRate int
	// OnDemand skips Draw on frames where no Update ran, unless MarkDirty has been called since the last Draw.
	OnDemand bool
	// Update moves the game on by dt, which is always 1/Rate seconds.
	Update func(dt time.Duration)
	// Draw draws the frame; alpha, from 0 up to but not including 1, is how far the frame falls between the last update and the next.
	Draw func(alpha float64)

	pendingSteps int
	dirty        bool
}

// SetPaused pauses or resumes the loop's updates without accumulating a catch-up backlog.
func (l *Loop) SetPaused(paused bool) {
	l.Paused = paused
	if !paused {
		l.pendingSteps = 0
	}
}

// SetTimeScale sets the simulation time scale; values at or below 0 run at 1.
func (l *Loop) SetTimeScale(scale float64) {
	l.TimeScale = scale
}

// Step queues n fixed updates to run on the next display frame while Paused is true, redrawing afterward. Values at or below 0 do nothing.
func (l *Loop) Step(n int) {
	if n > 0 {
		l.pendingSteps += n
	}
}

// StepOnce queues one fixed update to run on the next display frame while Paused is true.
func (l *Loop) StepOnce() {
	l.Step(1)
}

// MarkDirty requests a Draw on the next frame when OnDemand is true.
func (l *Loop) MarkDirty() {
	l.dirty = true
}

// Run starts the loop on cx's display frames until cx is disposed or stop is called.
//
// The loop pauses while cx's window is in the background (core.Env.Lifecycle), that is hidden, minimized or covered on a desktop and the app in the background on a phone, and resumes from the time it shows again, so the time away runs no updates. While running in the foreground it keeps the display awake (core.Scope.KeepAwake), releasing the hold when paused, stopped or disposed.
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
	// releaseAwake releases the display keep-awake hold, nil while it is paused.
	releaseAwake func()
	// last is the time of the last frame, or of the start or resumption.
	last time.Time
	// period is the average frame length, 0 before the first frame; owed is the frame time not yet added to acc, below 0 when a frame was taken to last longer than it did.
	period, owed time.Duration
	// acc is the time added but not yet run as updates.
	acc time.Duration
	// drawAcc is the frame time since the last capped Draw; drawn says Draw has run since the run started, resumed or went idle.
	drawAcc time.Duration
	drawn   bool
}

// resume starts the run's frames from now, unless they run.
func (r *loopRun) resume() {
	if r.cancel != nil {
		return
	}
	r.last = r.cx.Now()
	r.owed = 0
	// cx owns the frame and keep-awake hold rather than the effect that resumes, so what Update and Draw make lives as long as cx, as it would in a frame cx asked for itself.
	r.cx.Run(func() {
		r.releaseAwake = r.cx.KeepAwake("game loop", core.KeepAwakeOptions{Display: true})
		r.cancel = r.cx.RequestAnimationFrame(r.frame)
	})
}

// pause stops the run's frames until resume.
func (r *loopRun) pause() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.releaseAwake != nil {
		r.releaseAwake()
		r.releaseAwake = nil
	}
}

func (r *loopRun) frame(at time.Time) {
	r.cancel = r.cx.RequestAnimationFrame(r.frame)
	// A display's frame time (nsgo-dev/nsgo#1048) may fall before the time the run resumed at, read from cx's clock, so a frame never counts as going back in time: that would take time off acc and skip the frames' updates until it was paid back.
	rawDt := max(0, at.Sub(r.last))
	r.last = at
	step := r.loop.step()
	steps := 0
	var drawDt time.Duration
	if r.loop.Paused {
		r.period = 0
		r.owed = 0
		drawDt = rawDt
		if n := r.loop.pendingSteps; n > 0 {
			r.loop.pendingSteps = 0
			r.acc = 0
			for range n {
				steps++
				if r.loop.Update != nil {
					r.loop.Update(step)
				}
				// Update may have stopped the loop.
				if r.cancel == nil {
					return
				}
			}
		} else if r.acc >= step {
			r.acc %= step
		}
	} else {
		r.loop.pendingSteps = 0
		dt := r.smooth(rawDt)
		drawDt = dt
		add := scaleDuration(dt, r.loop.timeScale())
		if add > math.MaxInt64-r.acc {
			r.acc = math.MaxInt64
		} else {
			r.acc += add
		}
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
			// Update may have stopped or paused the loop.
			if r.cancel == nil {
				return
			}
			if r.loop.Paused {
				r.acc = 0
				r.period = 0
				r.owed = 0
				break
			}
		}
	}
	if steps > 0 {
		r.loop.dirty = true
	}
	shouldDraw := !r.loop.OnDemand || r.loop.dirty
	if !r.canDraw(drawDt, shouldDraw) {
		return
	}
	r.loop.dirty = false
	if r.loop.Draw != nil {
		r.loop.Draw(float64(r.acc) / float64(step))
	}
}

// canDraw advances the draw-rate pace by dt and reports whether Draw runs on this frame.
func (r *loopRun) canDraw(dt time.Duration, shouldDraw bool) bool {
	drawStep := r.loop.drawStep()
	if drawStep <= 0 {
		r.drawAcc = 0
		r.drawn = shouldDraw
		return shouldDraw
	}
	if !r.drawn {
		if !shouldDraw {
			return false
		}
		r.drawn = true
		r.drawAcc = 0
		return true
	}
	if dt > math.MaxInt64-r.drawAcc {
		r.drawAcc = math.MaxInt64
	} else {
		r.drawAcc += dt
	}
	if r.drawAcc < drawStep {
		return false
	}
	if !shouldDraw {
		r.drawn = false
		r.drawAcc = 0
		return false
	}
	r.drawAcc %= drawStep
	return true
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

func (l *Loop) timeScale() float64 {
	if l.TimeScale <= 0 || math.IsNaN(l.TimeScale) || math.IsInf(l.TimeScale, 0) {
		return 1
	}
	return l.TimeScale
}

func (l *Loop) drawStep() time.Duration {
	if l.DrawRate <= 0 {
		return 0
	}
	return time.Second / time.Duration(l.DrawRate)
}

func scaleDuration(d time.Duration, scale float64) time.Duration {
	if scale == 1 || d <= 0 {
		return d
	}
	scaled := float64(d) * scale
	if scaled >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return time.Duration(math.Round(scaled))
}
