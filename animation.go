package game

import "time"

// Animation plays frames one after another, each for its own duration, moved on by Update with a Loop's fixed steps.
//
// An Animation is a value that keeps where it is in its frames, so each sprite plays its own copy of one: copy it from an Atlas's Animations, or write one by hand.
type Animation struct {
	// Frames are the frames it plays, in order.
	Frames []AnimationFrame
	// Loop starts it over after its last frame; without it, it stays on the last frame and is done.
	Loop bool

	index   int
	elapsed time.Duration
	done    bool
}

// AnimationFrame is one frame of an animation and how long it shows.
type AnimationFrame struct {
	Frame    Frame
	Duration time.Duration
}

// Update moves the animation on by dt, past as many frames as dt covers. A looping animation whose frames add up to no time stays on its first frame.
func (a *Animation) Update(dt time.Duration) {
	if a.done || len(a.Frames) == 0 {
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
			var round time.Duration
			for _, f := range a.Frames {
				round += f.Duration
			}
			if round <= 0 {
				a.elapsed = 0
				return
			}
			a.elapsed %= round
		}
	}
}

// Frame returns the frame showing now, the zero Frame for an animation of none.
func (a Animation) Frame() Frame {
	if len(a.Frames) == 0 {
		return Frame{}
	}
	return a.Frames[a.index].Frame
}

// Index returns the index in Frames of the frame showing now.
func (a Animation) Index() int { return a.index }

// Done reports whether an animation that does not loop has shown its last frame for that frame's duration.
func (a Animation) Done() bool { return a.done }

// Reset starts the animation over from its first frame.
func (a *Animation) Reset() {
	a.index, a.elapsed, a.done = 0, 0, false
}
