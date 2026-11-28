package liveops

import (
	"errors"
	"sync"
	"time"
)

// AdKind identifies a host ad placement.
type AdKind string

const (
	Rewarded     AdKind = "rewarded"
	Interstitial AdKind = "interstitial"
	Banner       AdKind = "banner"
)

type AdResult string

const (
	AdRewarded AdResult = "rewarded"
	AdSkipped  AdResult = "skipped"
	AdNoFill   AdResult = "no_fill"
	AdFailed   AdResult = "failed"
	AdCapped   AdResult = "capped"
)

// AdHost is the host platform bridge. Show must invoke done exactly once.
type AdHost interface {
	Show(kind AdKind, placement string, done func(ended bool, err error)) error
}

// Ads enforces reward correctness and interstitial frequency limits around a host ad API.
type Ads struct {
	Host                 AdHost
	InterstitialInterval time.Duration
	InterstitialCap      int
	Disabled             func() bool
	Now                  func() time.Time
	Pause                func(bool)
	ResetInput           func()
	mu                   sync.Mutex
	shown                int
	last                 time.Time
	active               bool
}

func (a *Ads) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// ShowRewarded shows a rewarded placement and reports reward only when the host says playback ended.
func (a *Ads) ShowRewarded(placement string, done func(AdResult)) error {
	if a == nil || a.Host == nil {
		return errors.New("liveops: nil ad host")
	}
	if done == nil {
		done = func(AdResult) {}
	}
	var once sync.Once
	finish := func(r AdResult) {
		once.Do(func() {
			if a.Pause != nil {
				a.Pause(false)
			}
			done(r)
		})
	}
	if a.Pause != nil {
		a.Pause(true)
	}
	if a.ResetInput != nil {
		a.ResetInput()
	}
	err := a.Host.Show(Rewarded, placement, func(ended bool, err error) {
		if err != nil {
			finish(AdFailed)
		} else if ended {
			finish(AdRewarded)
		} else {
			finish(AdSkipped)
		}
	})
	if err != nil {
		finish(AdFailed)
	}
	return err
}

// ShowInterstitial enforces Disabled, per-session caps and the minimum interval.
func (a *Ads) ShowInterstitial(placement string, done func(AdResult)) error {
	if a == nil || a.Host == nil {
		return errors.New("liveops: nil ad host")
	}
	if done == nil {
		done = func(AdResult) {}
	}
	if a.Disabled != nil && a.Disabled() {
		done(AdCapped)
		return nil
	}
	now := a.now()
	a.mu.Lock()
	capped := (a.InterstitialCap > 0 && a.shown >= a.InterstitialCap) || (a.InterstitialInterval > 0 && !a.last.IsZero() && now.Sub(a.last) < a.InterstitialInterval)
	if !capped {
		a.shown++
		a.last = now
	}
	a.mu.Unlock()
	if capped {
		done(AdCapped)
		return nil
	}
	if a.Pause != nil {
		a.Pause(true)
	}
	if a.ResetInput != nil {
		a.ResetInput()
	}
	var once sync.Once
	finish := func(r AdResult) {
		once.Do(func() {
			if a.Pause != nil {
				a.Pause(false)
			}
			done(r)
		})
	}
	err := a.Host.Show(Interstitial, placement, func(ended bool, err error) {
		if err != nil {
			finish(AdFailed)
		} else {
			finish(AdSkipped)
		}
	})
	if err != nil {
		finish(AdFailed)
	}
	return err
}
