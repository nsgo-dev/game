package idle

import (
	"math"
	"time"
)

// Offline credits the time a player spent away from the game, measured between two wall-clock timestamps: the last-seen time a game stores in its save and the time it starts again or returns from the background.
//
// The zero Offline credits every absence in full. A game usually sets Cap, so returning after a month pays no more than, say, eight hours, and Min, so a quick app switch is not an "offline" event.
type Offline struct {
	// Cap is the longest absence credited; 0 means no cap.
	Cap time.Duration
	// Min is the shortest absence credited; a shorter one credits nothing.
	Min time.Duration
	// Efficiency is the fraction of the online rate earned while away, such as 0.5 for half; 0 means 1, the full rate.
	Efficiency float64
}

// Away describes one absence as Offline credits it.
type Away struct {
	// Elapsed is the wall-clock time between the last-seen time and now; 0 when there was no last-seen time or the clock moved backwards.
	Elapsed time.Duration
	// Credited is the part of Elapsed that earns progress, after Min and Cap.
	Credited time.Duration
	// Capped reports that Cap cut the absence short.
	Capped bool
	// Rewound reports that now is before the last-seen time: the device clock was set back, so nothing is credited.
	Rewound bool
	// Seen is the later of the last-seen time and now, without a monotonic clock reading. A game stores it as the next last-seen time, so setting the clock back and forward again cannot credit the same hours twice.
	Seen time.Time
}

// Absence measures the time between last and now and applies Min and Cap. A zero last, as on a first launch, credits nothing. Monotonic clock readings are dropped so the difference is wall-clock time, which keeps running while the device sleeps and is what a saved timestamp holds. It panics if Cap or Min is negative or Efficiency is negative or not finite.
func (o Offline) Absence(last, now time.Time) Away {
	o.check()
	last, now = last.Round(0), now.Round(0)
	if last.IsZero() {
		return Away{Seen: now}
	}
	if now.Before(last) {
		return Away{Rewound: true, Seen: last}
	}
	a := Away{Elapsed: now.Sub(last), Seen: now}
	if a.Elapsed < o.Min {
		return a
	}
	a.Credited = a.Elapsed
	if o.Cap > 0 && a.Credited > o.Cap {
		a.Credited = o.Cap
		a.Capped = true
	}
	return a
}

// Earn returns what rate, an amount per second, yields over the absence between last and now: rate × the credited seconds × Efficiency. It panics if Cap or Min is negative or Efficiency is negative or not finite.
func (o Offline) Earn(rate Number, last, now time.Time) (Number, Away) {
	a := o.Absence(last, now)
	if a.Credited == 0 {
		return Number{}, a
	}
	earned := rate.Mul(Seconds(a.Credited))
	if o.Efficiency != 0 && o.Efficiency != 1 {
		earned = earned.Scale(o.Efficiency)
	}
	return earned, a
}

func (o Offline) check() {
	switch {
	case o.Cap < 0:
		panic("idle: negative Offline.Cap")
	case o.Min < 0:
		panic("idle: negative Offline.Min")
	case o.Efficiency < 0 || math.IsNaN(o.Efficiency) || math.IsInf(o.Efficiency, 0):
		panic("idle: Offline.Efficiency must be a finite value of at least 0")
	}
}

// Steps splits the credited time into whole fixed steps and the remainder, for a game that replays its simulation step by step while away instead of multiplying a rate. It panics if step is not positive.
func (a Away) Steps(step time.Duration) (n int64, rest time.Duration) {
	if step <= 0 {
		panic("idle: Away.Steps with a non-positive step")
	}
	return int64(a.Credited / step), a.Credited % step
}

// Seconds returns d in seconds as a Number, to float64 precision.
func Seconds(d time.Duration) Number {
	return New(float64(d)).Div(nanosPerSecond)
}

var nanosPerSecond = New(1e9)
