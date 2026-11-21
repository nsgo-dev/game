package sprite

import (
	"math"
	"time"
)

// Animation plays frames one after another, each for its own duration, moved on by Update with a game.Loop's fixed steps.
//
// An Animation is a value that keeps where it is in its frames, so each sprite plays its own copy of one: copy it from an Atlas's Animations, or write one by hand.
type Animation struct {
	// Frames are the frames it plays, in order.
	Frames []AnimationFrame
	// Loop starts it over after its last frame; without it, it stays on the last frame and is done.
	Loop bool
	// Speed scales playback rate; 0 plays at 1x, positive values play forward, and negative values play in reverse.
	Speed float32
	// OnEvent, if set, is called with the frame's Event and index whenever playback enters a frame that names an Event.
	OnEvent func(event string, frameIndex int)

	index   int
	elapsed time.Duration
	done    bool
	started bool
}

// AnimationFrame is one frame of an animation and how long it shows.
type AnimationFrame struct {
	Frame    Frame
	Duration time.Duration
	// Event is an optional cue name fired when playback enters this frame.
	Event string
}

// Update moves the animation on by dt, past as many frames as dt covers, calling OnEvent for each frame entered that names an Event. A looping animation whose frames add up to no time stays on its first frame.
func (a *Animation) Update(dt time.Duration) {
	a.update(dt, nil)
}

// UpdateEvents moves the animation on by dt as Update does and returns the Event names of the frames entered during the step, in order.
func (a *Animation) UpdateEvents(dt time.Duration) []string {
	var events []string
	a.update(dt, &events)
	return events
}

func (a *Animation) update(dt time.Duration, events *[]string) {
	if a.done || len(a.Frames) == 0 || dt <= 0 {
		return
	}
	speed := float64(a.Speed)
	if math.IsNaN(speed) || math.IsInf(speed, 0) {
		return
	}
	if speed == 0 {
		speed = 1
	}
	reverse := speed < 0
	if reverse {
		speed = -speed
	}
	scaled := dt
	if speed != 1 {
		scaled = time.Duration(math.Round(float64(dt) * speed))
	}

	emit := func(idx int) {
		ev := a.Frames[idx].Event
		if ev == "" {
			return
		}
		if events != nil {
			*events = append(*events, ev)
		}
		if a.OnEvent != nil {
			a.OnEvent(ev, idx)
		}
	}

	if !a.started {
		a.started = true
		if reverse {
			a.index = len(a.Frames) - 1
			a.elapsed = a.Frames[a.index].Duration
		} else {
			a.index = 0
			a.elapsed = 0
		}
		emit(a.index)
	}

	if scaled <= 0 {
		return
	}

	if reverse {
		a.updateReverse(scaled, emit)
		return
	}
	a.updateForward(scaled, emit)
}

func (a *Animation) updateForward(dt time.Duration, emit func(int)) {
	if a.Loop && a.Frames[a.index].Duration <= 0 && a.roundDuration() <= 0 {
		a.elapsed = 0
		return
	}
	a.elapsed += dt
	for a.elapsed >= a.Frames[a.index].Duration {
		if a.index == len(a.Frames)-1 && !a.Loop {
			a.elapsed = a.Frames[a.index].Duration
			a.done = true
			return
		}
		a.elapsed -= a.Frames[a.index].Duration
		a.index = (a.index + 1) % len(a.Frames)
		if a.index == 0 {
			// A whole round passed: skip the rounds dt still covers rather than step through them.
			round := a.roundDuration()
			if round <= 0 {
				a.elapsed = 0
				return
			}
			a.elapsed %= round
		}
		emit(a.index)
	}
}

func (a *Animation) updateReverse(dt time.Duration, emit func(int)) {
	if a.Loop && a.Frames[a.index].Duration <= 0 && a.roundDuration() <= 0 {
		a.elapsed = 0
		return
	}
	a.elapsed -= dt
	for a.elapsed <= 0 {
		if a.index == 0 && !a.Loop {
			a.elapsed = 0
			a.done = true
			return
		}
		if a.index == 0 {
			// A whole round passed: skip the rounds dt still covers rather than step through them.
			round := a.roundDuration()
			if round <= 0 {
				a.elapsed = 0
				return
			}
			rem := (-a.elapsed) % round
			a.index = len(a.Frames) - 1
			a.elapsed = a.Frames[a.index].Duration - rem
		} else {
			a.index--
			a.elapsed += a.Frames[a.index].Duration
		}
		emit(a.index)
	}
}

func (a Animation) roundDuration() time.Duration {
	var round time.Duration
	for _, f := range a.Frames {
		round += f.Duration
	}
	return round
}

func (a Animation) currentIndex() int {
	if len(a.Frames) == 0 {
		return 0
	}
	if !a.started && a.Speed < 0 {
		return len(a.Frames) - 1
	}
	return a.index
}

// Frame returns the frame showing now, the zero Frame for an animation of none.
func (a Animation) Frame() Frame {
	if len(a.Frames) == 0 {
		return Frame{}
	}
	return a.Frames[a.currentIndex()].Frame
}

// Index returns the index in Frames of the frame showing now.
func (a Animation) Index() int { return a.currentIndex() }

// Done reports whether an animation that does not loop has shown its last frame for that frame's duration.
func (a Animation) Done() bool { return a.done }

// Reset starts the animation over from its first frame (or its last frame when Speed is negative).
func (a *Animation) Reset() {
	a.index, a.elapsed, a.done, a.started = 0, 0, false, false
}
