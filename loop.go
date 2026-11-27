package game

import (
	"math"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/mobile"
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
	// PausedAwake is how long the loop keeps the display awake (core.Scope.KeepAwake) after it is Paused or while an OnDemand loop is idle (Idle returns true with no MarkDirty or updates). Zero or negative releases the display hold at once.
	PausedAwake time.Duration
	// TimeScale scales simulated time, 1 when 0 or less (or not finite). Update still receives the fixed 1/Rate step, so 0.5 runs half as many updates a second and 2 runs twice as many.
	TimeScale float64
	// DrawRate caps Draw to at most DrawRate calls a second, uncapped when 0 or less. Update still runs at Rate.
	DrawRate int
	// OnDemand skips Draw on frames where no Update ran, unless MarkDirty has been called since the last Draw. When Idle is also set and returns true, OnDemand skips Update steps too until Idle returns false or MarkDirty wakes the loop.
	OnDemand bool
	// Idle, when set with OnDemand, reports whether the game currently has no ongoing work (such as animations, timers or typewriter reveals) that needs fixed-step updates. While OnDemand is true, Paused is false and Idle returns true, display frames skip both Update and Draw without accumulating time, and the display keep-awake hold is released after PausedAwake. Calling MarkDirty wakes an idle loop to run one fixed Update step and Draw on the next frame; if that Update makes Idle return false, fixed-step updates resume until Idle returns true again.
	Idle func() bool
	// Immersive hides a phone's system bars (mobile.Immersive) while the game plays, that is while the loop runs and is not Paused, and brings them back while it is Paused, as under a pause menu, and once it stops or its scope is disposed. The loop hides them as it starts or resumes in the foreground and applies a change to Immersive or Paused on its next frame, and leaves the bars as they are while the app is in the background. Desktop and web targets ignore it.
	Immersive bool
	// Update moves the game on by dt, which is always 1/Rate seconds.
	Update func(dt time.Duration)
	// Draw draws the frame; alpha, from 0 up to but not including 1, is how far the frame falls between the last update and the next.
	Draw func(alpha float64)
	// FrameTime, when set, receives each frame's length, the time it adds (0 for a frame stamped before the run started or resumed), ahead of the frame's updates. It is what frame-time statistics such as quality.Selector's read; a frame that ends a pause in the background measures from the time the window showed again, not the time away. It fires while Paused is set too, so a caller measuring only frames that simulate skips those itself.
	FrameTime func(dt time.Duration)
	// OnPause, when set, runs whenever Paused changes, so subsystems such as sound.Mixer (sound.Mixer.SetPaused) pause and resume with the loop. SetPaused calls it at once when the state changes; changing Paused directly calls it on the next frame, or in the same frame when Update changes it. If Paused is already true when Run starts, Run calls OnPause(true). Backgrounding does not call OnPause; both Loop and sound.Mixer follow core.Env.Lifecycle directly.
	OnPause func(paused bool)

	pendingSteps int
	dirty        bool
	wakeStep     bool
	lastPaused   bool
	steps        uint64
	segRate      int
	segSteps     uint64
	prevElapsed  float64
	activeRun    *loopRun
}

// SetPaused pauses or resumes the loop's updates without accumulating a catch-up backlog, calling OnPause if the paused state changed.
func (l *Loop) SetPaused(paused bool) {
	l.Paused = paused
	if paused {
		l.wakeStep = false
	} else {
		l.pendingSteps = 0
	}
	l.syncPause()
}

func (l *Loop) syncPause() {
	if l.Paused == l.lastPaused {
		return
	}
	l.lastPaused = l.Paused
	if r := l.activeRun; r != nil {
		r.awakeIdle = 0
	}
	if l.OnPause != nil {
		l.OnPause(l.Paused)
	}
	if r := l.activeRun; r != nil {
		r.syncAwake(false)
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
		if r := l.activeRun; r != nil && l.Paused {
			r.awakeIdle = 0
			r.syncAwake(false)
		}
	}
}

// StepOnce queues one fixed update to run on the next display frame while Paused is true.
func (l *Loop) StepOnce() {
	l.Step(1)
}

// MarkDirty requests a Draw on the next frame when OnDemand is true. When an unpaused OnDemand loop is idle (Idle returns true), MarkDirty also queues one fixed Update step on the next frame so Update can sample newly arrived input and advance state.
func (l *Loop) MarkDirty() {
	l.dirty = true
	if l.OnDemand && !l.Paused {
		l.wakeStep = true
	}
	if r := l.activeRun; r != nil && l.OnDemand {
		r.awakeIdle = 0
		r.syncAwake(false)
	}
}

func (l *Loop) isIdle() bool {
	return l.OnDemand && l.Idle != nil && l.Idle()
}

// Steps returns the total number of fixed Update steps executed by l.
func (l *Loop) Steps() uint64 {
	if l == nil {
		return 0
	}
	return l.steps
}

// ElapsedSeconds returns the simulated game time advanced by fixed Update steps
// in seconds. Each contiguous run at a fixed rate contributes steps/rate seconds
// without integer-nanosecond drift, so 60 steps at 60 Hz is exactly 1.0.
func (l *Loop) ElapsedSeconds() float64 {
	if l == nil {
		return 0
	}
	if l.segRate <= 0 {
		return l.prevElapsed
	}
	return l.prevElapsed + float64(l.segSteps)/float64(l.segRate)
}

// Elapsed returns the simulated game time advanced by fixed Update steps,
// rounded to the nearest nanosecond from ElapsedSeconds (so 60 steps at 60 Hz
// is exactly time.Second).
func (l *Loop) Elapsed() time.Duration {
	sec := l.ElapsedSeconds()
	if sec <= 0 {
		return 0
	}
	ns := sec * float64(time.Second)
	if ns >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return time.Duration(math.Round(ns))
}

// StepRate returns the effective fixed update rate in Hz (Rate when > 0, else 60).
func (l *Loop) StepRate() int {
	if l == nil {
		return 60
	}
	return l.rate()
}

// StepDuration returns the fixed duration passed to Update (time.Second / StepRate()).
func (l *Loop) StepDuration() time.Duration {
	if l == nil {
		return time.Second / 60
	}
	return l.step()
}

// EffectiveTimeScale returns the simulation time scale actually applied by Run
// (TimeScale when finite and > 0, else 1).
func (l *Loop) EffectiveTimeScale() float64 {
	if l == nil {
		return 1
	}
	return l.timeScale()
}

// StepNow synchronously executes n fixed Update steps right away (whether Paused
// is true or false), followed by a Draw when Draw is set, and returns how many
// steps ran. Values at or below 0 do nothing and return 0. If Update stops an
// active Run, StepNow stops immediately without calling Draw.
func (l *Loop) StepNow(n int) int {
	if l == nil || n <= 0 {
		return 0
	}
	r := l.activeRun
	if r != nil && l.Paused {
		l.pendingSteps = 0
		r.acc = 0
	}
	ran := 0
	var lastStep time.Duration
	for range n {
		rate := l.rate()
		step := time.Second / time.Duration(rate)
		lastStep = step
		ran++
		l.recordStep(rate)
		if l.Update != nil {
			l.Update(step)
		}
		if r != nil && r.ended {
			return ran
		}
	}
	l.syncPause()
	if r != nil {
		if r.ended {
			return ran
		}
		r.awakeIdle = 0
		r.syncImmersive()
		r.syncAwake(false)
	}
	l.wakeStep = false
	l.dirty = true
	if l.Draw != nil {
		l.dirty = false
		alpha := 0.0
		if r != nil && !l.Paused && r.acc > 0 && lastStep > 0 {
			alpha = float64(r.acc) / float64(lastStep)
			if alpha >= 1 {
				alpha = 0
			}
		}
		l.Draw(alpha)
	}
	return ran
}

func (l *Loop) recordStep(rate int) {
	l.steps++
	if l.segRate == 0 {
		l.segRate = rate
	} else if l.segRate != rate {
		l.prevElapsed += float64(l.segSteps) / float64(l.segRate)
		l.segRate = rate
		l.segSteps = 0
	}
	l.segSteps++
}

// Run starts the loop on cx's display frames until cx is disposed or stop is called.
//
// The loop pauses while cx's window is in the background (core.Env.Lifecycle), that is hidden, minimized or covered on a desktop and the app in the background on a phone, and resumes from the time it shows again, so the time away runs no updates. While running in the foreground and active (not Paused and not idle under OnDemand and Idle) it keeps the display awake (core.Scope.KeepAwake). It releases the display hold immediately on background, stop or scope disposal, and once the loop has been Paused or OnDemand-idle (with no MarkDirty or updates) for PausedAwake (at once when PausedAwake <= 0), taking it again on unpause, when Idle returns false, and while pending Step updates or an OnDemand MarkDirty wake/draw run.
//
// A frame adds the time between its display time and the last frame's, as core.Scope.RequestAnimationFrame gives it: the display's own frame time where the platform stamps one, so the frame length is the display's rather than when the engine got to the frame, and the graph clock's otherwise. The first frame after the loop starts or resumes adds the time from then to its display time, which may lie a frame ahead where the display stamps a frame with when it will show, so the loop's time is the shown frame's. A frame stamped before the time the loop started or resumed adds nothing, and the next frame adds the time from then.
//
// With Immersive set, the run hides the system bars while the game plays and shows them again when it is paused, stopped or disposed. Runs in the same window share the bars: they stay hidden while any of them plays with Immersive set, and come back once the last of those pauses or ends, so a scene that starts its own loop before the last scene's loop is disposed keeps them hidden.
func (l *Loop) Run(cx *core.Scope) (stop func()) {
	l.syncPause()
	r := &loopRun{loop: l, cx: cx, env: cx.Env()}
	l.activeRun = r
	r.unregisterWeb = l.attachWebBridge(cx)
	stopEffect := cx.Effect(func() {
		if cx.Env().Lifecycle() == core.LifecycleBackground {
			r.pause()
		} else {
			r.resume()
		}
	})
	cx.Cleanup(r.end)
	return func() {
		r.pause()
		r.end()
		stopEffect()
	}
}

// setImmersive asks for the system bars hidden or shown. NSGo's nsgotest records the status bar without its Immersive field (nsgotest.StatusBarConfig, nsgo-dev/nsgo#2255), so the tests replace this to see what the loop asks for.
var setImmersive = mobile.Immersive

// immersiveRuns counts, for each window's environment, the runs that want the system bars hidden. mobile.Immersive patches the one app-wide setting of the window, so a run that pauses or ends shows the bars again only when no other run in the window still wants them hidden.
var (
	immersiveMu   sync.Mutex
	immersiveRuns = map[*core.Env]int{}
)

// loopRun is one Run of a Loop.
type loopRun struct {
	loop *Loop
	cx   *core.Scope
	// env is cx's environment, which keys the run's share of immersiveRuns.
	env *core.Env
	// cancel cancels the frame the run waits for, nil while it is paused.
	cancel func()
	// releaseAwake releases the display keep-awake hold, nil while it is not held.
	releaseAwake func()
	// awakeIdle is how long the run has been continuously paused (without pending steps or OnDemand dirty draws) in the foreground.
	awakeIdle time.Duration
	// last is the time of the last frame, or of the start or resumption.
	last time.Time
	// acc is the time added but not yet run as updates.
	acc time.Duration
	// drawAcc is the frame time since the last capped Draw; drawn says Draw has run since the run started, resumed or went idle.
	drawAcc time.Duration
	drawn   bool
	// immersive says the run counts in immersiveRuns as wanting the system bars hidden; ended says stop ran or cx was disposed.
	immersive, ended bool
	// unregisterWeb detaches the web Play-mode MCP bridge registration (no-op outside js/wasm).
	unregisterWeb func()
}

// syncImmersive counts the run as wanting the system bars hidden while the game plays with Immersive set, hiding them when it is the first such run in its window and showing them again when the last one stops wanting them hidden.
func (r *loopRun) syncImmersive() {
	want := r.loop.Immersive && !r.loop.Paused && !r.ended
	if want == r.immersive {
		return
	}
	r.immersive = want
	immersiveMu.Lock()
	n := immersiveRuns[r.env]
	if want {
		n++
	} else {
		n--
	}
	if n > 0 {
		immersiveRuns[r.env] = n
	} else {
		delete(immersiveRuns, r.env)
	}
	immersiveMu.Unlock()
	if (want && n == 1) || (!want && n == 0) {
		setImmersive(r.cx, want)
	}
}

// syncAwake acquires or releases the display keep-awake hold to match whether the run is active in the foreground (or within PausedAwake). active is true while the current frame is executing pending Step updates or an OnDemand dirty wake/draw.
func (r *loopRun) syncAwake(active bool) {
	want := !r.ended && r.cancel != nil && (active ||
		(!r.loop.Paused && !r.loop.isIdle()) ||
		r.loop.pendingSteps > 0 ||
		(r.loop.OnDemand && r.loop.dirty) ||
		r.awakeIdle < r.loop.PausedAwake)
	if want {
		if r.releaseAwake == nil {
			// cx owns the keep-awake hold rather than the caller's current scope or effect, so disposing cx always releases it.
			r.cx.Run(func() {
				r.releaseAwake = r.cx.KeepAwake("game loop", core.KeepAwakeOptions{Display: true})
			})
		}
		return
	}
	if r.releaseAwake != nil {
		r.releaseAwake()
		r.releaseAwake = nil
	}
}

// end ends the run for good, giving up its share of the hidden system bars. Stopping pauses the run first, while disposing cx cancels the frame and keep-awake hold that cx owns.
func (r *loopRun) end() {
	r.ended = true
	r.cancel = nil
	r.releaseAwake = nil
	if r.unregisterWeb != nil {
		r.unregisterWeb()
		r.unregisterWeb = nil
	}
	if r.loop.activeRun == r {
		r.loop.activeRun = nil
	}
	r.syncImmersive()
}

// resume starts the run's frames from now, unless they run.
func (r *loopRun) resume() {
	if r.cancel != nil {
		return
	}
	r.last = r.cx.Now()
	r.awakeIdle = 0
	r.syncImmersive()
	// cx owns the frame and keep-awake hold rather than the effect that resumes, so what Update and Draw make lives as long as cx, as it would in a frame cx asked for itself.
	r.cx.Run(func() {
		r.cancel = r.cx.RequestAnimationFrame(r.frame)
	})
	r.syncAwake(false)
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
	// A display's frame time may fall before the time the run resumed at, read from cx's clock. Such a frame adds nothing and leaves last at the resumption: going back in time would take time off acc and skip the frames' updates until it was paid back, and measuring the next frame from the earlier stamp would count time from before the resumption.
	var dt time.Duration
	if at.After(r.last) {
		dt = at.Sub(r.last)
		r.last = at
	}
	if r.loop.FrameTime != nil {
		r.loop.FrameTime(dt)
		// FrameTime may have stopped the loop.
		if r.cancel == nil {
			return
		}
	}
	r.loop.syncPause()
	if r.cancel == nil {
		return
	}
	rate := r.loop.rate()
	step := time.Second / time.Duration(rate)
	steps := 0
	stepped := false
	if r.loop.Paused {
		r.loop.wakeStep = false
		if n := r.loop.pendingSteps; n > 0 {
			stepped = true
			r.awakeIdle = 0
			r.syncAwake(true)
			r.loop.pendingSteps = 0
			r.acc = 0
			for range n {
				steps++
				r.loop.recordStep(rate)
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
	} else if r.loop.isIdle() {
		r.loop.pendingSteps = 0
		if r.loop.wakeStep {
			stepped = true
			r.awakeIdle = 0
			r.syncAwake(true)
			r.loop.wakeStep = false
			r.acc = 0
			steps++
			r.loop.recordStep(rate)
			if r.loop.Update != nil {
				r.loop.Update(step)
			}
			// Update may have stopped the loop.
			if r.cancel == nil {
				return
			}
			r.loop.wakeStep = false
			r.acc = 0
		} else {
			r.acc = 0
		}
	} else {
		r.awakeIdle = 0
		r.loop.pendingSteps = 0
		r.loop.wakeStep = false
		add := scaleDuration(dt, r.loop.timeScale())
		if add > math.MaxInt64-r.acc {
			r.acc = math.MaxInt64
		} else {
			r.acc += add
		}
		for r.acc >= step {
			if steps >= r.loop.maxSteps() {
				r.acc %= step
				break
			}
			r.acc -= step
			steps++
			r.loop.recordStep(rate)
			if r.loop.Update != nil {
				r.loop.Update(step)
			}
			// Update may have stopped, paused or idled the loop.
			if r.cancel == nil {
				return
			}
			if r.loop.Paused || r.loop.isIdle() {
				r.acc = 0
				break
			}
		}
		r.loop.wakeStep = false
	}
	r.loop.syncPause()
	if r.cancel == nil {
		return
	}
	r.syncImmersive()
	if steps > 0 {
		r.loop.dirty = true
	}
	wasDirty := r.loop.OnDemand && r.loop.dirty
	stillIdle := r.loop.isIdle()
	if (!r.loop.Paused && !stillIdle) || stepped || steps > 0 || r.loop.pendingSteps > 0 || wasDirty {
		r.awakeIdle = 0
	} else if dt > math.MaxInt64-r.awakeIdle {
		r.awakeIdle = math.MaxInt64
	} else {
		r.awakeIdle += dt
	}
	shouldDraw := !r.loop.OnDemand || r.loop.dirty
	if !r.canDraw(dt, shouldDraw) {
		r.syncAwake(false)
		return
	}
	r.syncAwake(stepped || wasDirty)
	r.loop.dirty = false
	if r.loop.Draw != nil {
		r.loop.Draw(float64(r.acc) / float64(step))
	}
	if r.cancel == nil {
		return
	}
	r.syncAwake(false)
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
