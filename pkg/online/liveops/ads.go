package liveops

import (
	"errors"
	"sync"
	"time"
)

// AdKind identifies a host ad placement.
type AdKind string

// Ad kinds an AdHost can be asked to show.
const (
	Rewarded     AdKind = "rewarded"
	Interstitial AdKind = "interstitial"
	Banner       AdKind = "banner"
)

// AdResult is what Ads reports to the game once a show finishes.
type AdResult string

// Ad results. AdRewarded is reported only for a rewarded ad watched to the end; AdShown for an interstitial that closed normally.
const (
	AdRewarded AdResult = "rewarded"
	AdShown    AdResult = "shown"
	AdSkipped  AdResult = "skipped"
	AdNoFill   AdResult = "no_fill"
	AdFailed   AdResult = "failed"
	AdCapped   AdResult = "capped"
)

var (
	// ErrAdNoFill is the error an AdHost returns or passes to done when the platform has no ad to show; Ads reports it as AdNoFill.
	ErrAdNoFill = errors.New("liveops: no ad fill")
	// ErrAdBusy is returned when a show is requested while another ad is still showing.
	ErrAdBusy    = errors.New("liveops: an ad is already showing")
	errNilAdHost = errors.New("liveops: nil ad host")
)

// AdHost is the host platform bridge. Show must invoke done at most once, with ended true only when a rewarded video played to the end; Ads ignores any further calls. An error wrapping ErrAdNoFill means the platform had no ad to show.
type AdHost interface {
	Show(kind AdKind, placement string, done func(ended bool, err error)) error
}

// Ads enforces reward correctness and interstitial frequency limits around a host ad API. Only one ad shows at a time: a show requested while another is active reports AdFailed and returns ErrAdBusy.
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

// ShowRewarded shows a rewarded placement and reports AdRewarded only when the host says playback ended, at most once per show.
func (a *Ads) ShowRewarded(placement string, done func(AdResult)) error {
	if a == nil || a.Host == nil {
		return errNilAdHost
	}
	if done == nil {
		done = func(AdResult) {}
	}
	a.mu.Lock()
	if a.active {
		a.mu.Unlock()
		done(AdFailed)
		return ErrAdBusy
	}
	a.active = true
	a.mu.Unlock()
	return a.show(Rewarded, placement, done)
}

// ShowInterstitial enforces Disabled, the per-session cap and the minimum interval, reporting AdCapped when any of them blocks the show.
func (a *Ads) ShowInterstitial(placement string, done func(AdResult)) error {
	if a == nil || a.Host == nil {
		return errNilAdHost
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
	if a.active {
		a.mu.Unlock()
		done(AdFailed)
		return ErrAdBusy
	}
	capped := (a.InterstitialCap > 0 && a.shown >= a.InterstitialCap) || (a.InterstitialInterval > 0 && !a.last.IsZero() && now.Sub(a.last) < a.InterstitialInterval)
	if !capped {
		a.shown++
		a.last = now
		a.active = true
	}
	a.mu.Unlock()
	if capped {
		done(AdCapped)
		return nil
	}
	return a.show(Interstitial, placement, done)
}

// show runs one host show with a.active already set, pausing the game and resetting input around it and reporting exactly one result.
func (a *Ads) show(kind AdKind, placement string, done func(AdResult)) error {
	var once sync.Once
	finish := func(r AdResult) {
		once.Do(func() {
			a.mu.Lock()
			a.active = false
			a.mu.Unlock()
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
	err := a.Host.Show(kind, placement, func(ended bool, err error) {
		switch {
		case err != nil:
			finish(failure(err))
		case kind != Rewarded:
			finish(AdShown)
		case ended:
			finish(AdRewarded)
		default:
			finish(AdSkipped)
		}
	})
	if err != nil {
		finish(failure(err))
	}
	return err
}

func failure(err error) AdResult {
	if errors.Is(err, ErrAdNoFill) {
		return AdNoFill
	}
	return AdFailed
}
