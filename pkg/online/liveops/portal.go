package liveops

import (
	"context"
	"errors"
	"sync"
	"time"
)

// PortalProvider identifies a web-game hosting portal. None is safe on native
// targets and in tests.
type PortalProvider uint8

const (
	PortalNone PortalProvider = iota
	PortalPoki
	PortalCrazyGames
)

// PortalSDK is the small provider seam used by Portal. Web adapters call the
// corresponding JavaScript SDK methods, while tests can use a deterministic fake.
type PortalSDK interface {
	LoadingFinished(context.Context) error
	GameplayStart(context.Context) error
	GameplayStop(context.Context) error
	CommercialBreak(context.Context) error
	RewardedBreak(context.Context) (bool, error)
}

// PortalHooks owns game-side state that must be suspended during an ad break.
type PortalHooks struct {
	Pause      func()
	Resume     func()
	Mute       func()
	Unmute     func()
	ResetInput func()
}

// Portal coordinates portal SDK calls with the game pause, audio and input lifecycle.
type Portal struct {
	Provider PortalProvider
	SDK      PortalSDK
	Hooks    PortalHooks
	Timeout  time.Duration
	mu       sync.Mutex
	playing  bool
}

func (p *Portal) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return 10 * time.Second
}

// LoadingFinished reports that the first playable scene is ready.
func (p *Portal) LoadingFinished(ctx context.Context) error {
	if p == nil || p.Provider == PortalNone || p.SDK == nil {
		return nil
	}
	c, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()
	return p.SDK.LoadingFinished(c)
}

// GameplayStart and GameplayStop mirror the game's active play state.
func (p *Portal) GameplayStart(ctx context.Context) error {
	if p == nil || p.Provider == PortalNone || p.SDK == nil {
		return nil
	}
	c, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()
	p.mu.Lock()
	p.playing = true
	p.mu.Unlock()
	return p.SDK.GameplayStart(c)
}
func (p *Portal) GameplayStop(ctx context.Context) error {
	if p == nil || p.Provider == PortalNone || p.SDK == nil {
		return nil
	}
	c, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()
	p.mu.Lock()
	p.playing = false
	p.mu.Unlock()
	return p.SDK.GameplayStop(c)
}

func (p *Portal) suspend() func() {
	if p.Hooks.Pause != nil {
		p.Hooks.Pause()
	}
	if p.Hooks.Mute != nil {
		p.Hooks.Mute()
	}
	if p.Hooks.ResetInput != nil {
		p.Hooks.ResetInput()
	}
	return func() {
		if p.Hooks.Unmute != nil {
			p.Hooks.Unmute()
		}
		if p.Hooks.Resume != nil {
			p.Hooks.Resume()
		}
	}
}

// Break runs a commercial break and restores game state even when the SDK
// fails or times out.
func (p *Portal) Break(ctx context.Context) error {
	if p == nil || p.Provider == PortalNone || p.SDK == nil {
		return nil
	}
	restore := p.suspend()
	defer restore()
	c, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()
	return p.SDK.CommercialBreak(c)
}

// Rewarded runs an opt-in break and returns true only when the provider grants
// the reward. SDK errors and timeouts never grant it.
func (p *Portal) Rewarded(ctx context.Context) (bool, error) {
	if p == nil || p.Provider == PortalNone || p.SDK == nil {
		return false, nil
	}
	restore := p.suspend()
	defer restore()
	c, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()
	granted, err := p.SDK.RewardedBreak(c)
	if err != nil {
		return false, err
	}
	return granted, nil
}

var ErrPortalTimeout = context.DeadlineExceeded

// Validate rejects an incomplete configured portal.
func (p *Portal) Validate() error {
	if p.Provider != PortalNone && p.SDK == nil {
		return errors.New("liveops: portal SDK is required")
	}
	return nil
}
