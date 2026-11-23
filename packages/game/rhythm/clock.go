package rhythm

import "time"

// DefaultTolerance is how far a player's reported position may stray from a Clock before Sync moves the clock to it.
const DefaultTolerance = 30 * time.Millisecond

// Offsets are the delays between the engine starting a sound or drawing a frame and the player acting on it, as calibration measures them on a device.
type Offsets struct {
	// Audio is how long after the engine starts a sound the player taps to it: the output's latency, the input's and the player's habit together. It is usually positive, and tens to hundreds of milliseconds over Bluetooth.
	Audio time.Duration
	// Video is how long after the engine draws a frame the player taps to what it shows: the display's latency, the input's and the player's habit together.
	Video time.Duration
}

// Clock is a track's music clock: the position the audio output plays, in the track's beats, read at any time of the engine's clock (audio.Now, which audio.Voice.StartAt also counts on), and corrected by a device's Offsets for judging input and drawing notes.
//
// Start it when the track starts playing: at the time passed to audio.Voice.StartAt for a sound, or in an audio.Player's Play callback with the player's position. Between starts it extrapolates from the engine's clock, so it does not move in the steps of a player's progress reports; Sync, called from the player's OnProgress, moves it only when the player drifts past Tolerance, as after a seek, a loop or a stall.
//
// A Clock is used from the engine's goroutine, as the audio package is.
type Clock struct {
	// Tolerance is how far a reported position may stray before Sync moves the clock; DefaultTolerance when 0.
	Tolerance time.Duration
	timing    *Timing
	offsets   Offsets
	running   bool
	// anchor is the engine time at which the output played track position base.
	anchor time.Time
	base   time.Duration
}

// NewClock returns a stopped clock at track position 0, counting beats by t.
func NewClock(t *Timing) *Clock { return &Clock{timing: t} }

// Timing returns the beat map's timing the clock counts by.
func (c *Clock) Timing() *Timing { return c.timing }

// Offsets returns the device's offsets the clock corrects by.
func (c *Clock) Offsets() Offsets { return c.offsets }

// SetOffsets sets the device's offsets the clock corrects by, such as those OffsetStore.Get returns.
func (c *Clock) SetOffsets(o Offsets) { c.offsets = o }

// Running reports whether the clock runs, between Start or Resume and Pause.
func (c *Clock) Running() bool { return c.running }

// Start has the clock run from track position from, played by the output at engine time at.
func (c *Clock) Start(at time.Time, from time.Duration) {
	c.anchor, c.base, c.running = at, from, true
}

// Pause stops the clock at the position the output plays at engine time at.
func (c *Clock) Pause(at time.Time) {
	c.base, c.running = c.Track(at), false
}

// Resume has a paused clock run on from where it paused, played by the output at engine time at.
func (c *Clock) Resume(at time.Time) {
	if !c.running {
		c.Start(at, c.base)
	}
}

// Track returns the track position the output plays at engine time now, uncorrected: where the track is, not what is heard.
func (c *Clock) Track(now time.Time) time.Duration {
	if !c.running {
		return c.base
	}
	return c.base + now.Sub(c.anchor)
}

// Position returns the track position a player acting at engine time now acts on: the output's position less the device's audio offset. Judge input by it.
func (c *Clock) Position(now time.Time) time.Duration { return c.Track(now) - c.offsets.Audio }

// Beat returns the beat at Position(now).
func (c *Clock) Beat(now time.Time) float64 { return c.timing.Beat(c.Position(now)) }

// DrawPosition returns the track position a frame drawn at engine time now should show, so that what it shows reaches the player with the beat they hear: the output's position plus the video offset less the audio offset.
func (c *Clock) DrawPosition(now time.Time) time.Duration {
	return c.Track(now) + c.offsets.Video - c.offsets.Audio
}

// DrawBeat returns the beat at DrawPosition(now): where a frame drawn at now puts the hit line.
func (c *Clock) DrawBeat(now time.Time) float64 { return c.timing.Beat(c.DrawPosition(now)) }

// TrackTime returns the engine time at which the output plays track position pos while the clock runs. Pass it to audio.Voice.StartAt to start a hit sound or a metronome click in time with the track: both go through the same output, so no offset applies.
func (c *Clock) TrackTime(pos time.Duration) time.Time { return c.anchor.Add(pos - c.base) }

// BeatTime returns TrackTime of beat.
func (c *Clock) BeatTime(beat float64) time.Time { return c.TrackTime(c.timing.Time(beat)) }

// Judge returns how early (negative) or late (positive) an input at engine time at is for beat, corrected by the device's audio offset.
func (c *Clock) Judge(at time.Time, beat float64) time.Duration {
	return c.Position(at) - c.timing.Time(beat)
}

// Sync compares the position a player reports, reported at engine time at, with the clock, and moves the clock to it when they differ by more than Tolerance, reporting whether it did. A stopped clock does not move. Call it from audio.PlayerOptions.OnProgress with audio.Now.
func (c *Clock) Sync(reported time.Duration, at time.Time) bool {
	if !c.running {
		return false
	}
	tol := c.Tolerance
	if tol <= 0 {
		tol = DefaultTolerance
	}
	diff := reported - c.Track(at)
	if diff <= tol && diff >= -tol {
		return false
	}
	c.Start(at, reported)
	return true
}
