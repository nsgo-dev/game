// Package eos provides the Go boundary for Epic Online Services. The package
// keeps the callback contract independent of the native SDK so games can use
// a fake driver in tests and return ErrUnavailable when the SDK is absent.
package eos

import (
	"context"
	"errors"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

var (
	ErrUnavailable     = errors.New("eos: unavailable")
	ErrClosed          = errors.New("eos: closed")
	ErrUnauthenticated = errors.New("eos: unauthenticated")
	ErrInvalidConfig   = errors.New("eos: invalid config")
)

// Config identifies an Epic product. ExchangeCode is supplied by the Epic
// launcher with -AUTH_PASSWORD for exchange-code login.
type Config struct {
	ProductID    string
	SandboxID    string
	DeploymentID string
	ExchangeCode string
}

func (c Config) validate() error {
	if strings.TrimSpace(c.ProductID) == "" || strings.TrimSpace(c.SandboxID) == "" || strings.TrimSpace(c.DeploymentID) == "" {
		return ErrInvalidConfig
	}
	return nil
}

// Player is the local Epic account returned by Login.
type Player struct {
	ProductUserID string
	DisplayName   string
}

// LeaderboardEntry is one row returned by QueryLeaderboard.
type LeaderboardEntry struct {
	Rank    int
	UserID  string
	Display string
	Score   int64
}

// Driver is the small native boundary implemented by an EOS SDK adapter.
// Implementations must not invoke callbacks themselves. Platform posts every
// result to its core scope so game state changes stay on the engine goroutine.
type Driver interface {
	Login(context.Context, string) (Player, error)
	UnlockAchievement(context.Context, string) error
	ProgressAchievement(context.Context, string, float64) error
	SubmitScore(context.Context, string, int64) error
	QueryLeaderboard(context.Context, string, bool, int) ([]LeaderboardEntry, error)
	SetPresence(context.Context, string, string) error
	Tick()
	Close() error
}

// Platform owns one EOS connection and its driver. Its methods are safe only
// on the engine goroutine, matching sound and game services.
type Platform struct {
	cx     *core.Scope
	cfg    Config
	driver Driver
	player *core.Signal[Player]
	closed bool
}

// New creates a platform around an injected driver. Native adapters should
// return ErrUnavailable when the EOS shared library cannot be loaded.
func New(cx *core.Scope, cfg Config, driver Driver) (*Platform, error) {
	if cx == nil {
		return nil, ErrInvalidConfig
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if driver == nil {
		return nil, ErrUnavailable
	}
	p := &Platform{cx: cx, cfg: cfg, driver: driver, player: core.NewSignal(cx, Player{})}
	cx.Cleanup(func() { _ = p.Close() })
	return p, nil
}

// Player returns the authenticated player, or zero while signed out.
func (p *Platform) Player() Player {
	if p == nil || p.player == nil {
		return Player{}
	}
	return p.player.Get()
}

// SignedIn reports whether Login succeeded.
func (p *Platform) SignedIn() bool { return p.Player().ProductUserID != "" }

// Tick pumps the native EOS callback queue once. Call from the fixed game loop.
func (p *Platform) Tick() {
	if p != nil && !p.closed {
		p.driver.Tick()
	}
}

// Login authenticates with the configured exchange code and posts done on cx.
func (p *Platform) Login(done func(Player, error)) {
	if p == nil || p.closed {
		return
	}
	player, err := p.driver.Login(context.Background(), p.cfg.ExchangeCode)
	if err == nil && player.ProductUserID == "" {
		err = ErrUnauthenticated
	}
	p.cx.Post(func() {
		if err == nil {
			p.player.Set(player)
		} else {
			p.player.Set(Player{})
		}
		if done != nil {
			done(player, err)
		}
	})
}

func (p *Platform) call(fn func() error, done func(error)) {
	if p == nil || p.closed {
		if done != nil {
			done(ErrClosed)
		}
		return
	}
	err := fn()
	p.cx.Post(func() {
		if done != nil {
			done(err)
		}
	})
}

// UnlockAchievement reports an achievement ID to EOS.
func (p *Platform) UnlockAchievement(id string, done func(error)) {
	p.call(func() error { return p.driver.UnlockAchievement(context.Background(), id) }, done)
}

// ProgressAchievement reports achievement progress in [0,100].
func (p *Platform) ProgressAchievement(id string, percent float64, done func(error)) {
	p.call(func() error { return p.driver.ProgressAchievement(context.Background(), id, percent) }, done)
}

// SubmitScore submits a score to a named EOS leaderboard.
func (p *Platform) SubmitScore(board string, score int64, done func(error)) {
	p.call(func() error { return p.driver.SubmitScore(context.Background(), board, score) }, done)
}

// QueryLeaderboard loads global or friends rows from an EOS leaderboard.
func (p *Platform) QueryLeaderboard(board string, friends bool, limit int, done func([]LeaderboardEntry, error)) {
	if p == nil || p.closed {
		if done != nil {
			done(nil, ErrClosed)
		}
		return
	}
	entries, err := p.driver.QueryLeaderboard(context.Background(), board, friends, limit)
	p.cx.Post(func() {
		if done != nil {
			done(entries, err)
		}
	})
}

// SetPresence publishes rich presence details and status.
func (p *Platform) SetPresence(details, status string, done func(error)) {
	p.call(func() error { return p.driver.SetPresence(context.Background(), details, status) }, done)
}

// Close shuts down the native driver. It is idempotent.
func (p *Platform) Close() error {
	if p == nil || p.closed {
		return nil
	}
	p.closed = true
	p.player.Set(Player{})
	return p.driver.Close()
}
