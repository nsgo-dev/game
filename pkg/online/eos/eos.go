// Package eos provides the Go boundary for Epic Online Services. The package keeps the callback contract independent of the native SDK so games can use a fake driver in tests and return ErrUnavailable when the SDK is absent.
package eos

import (
	"errors"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

var (
	ErrUnavailable     = errors.New("eos: unavailable")
	ErrClosed          = errors.New("eos: closed")
	ErrUnauthenticated = errors.New("eos: unauthenticated")
	ErrInvalidConfig   = errors.New("eos: invalid config")
	ErrInvalidArgument = errors.New("eos: invalid argument")
)

// Config identifies an Epic product. ExchangeCode is the code the Epic launcher passes with -AUTH_PASSWORD; ExchangeCodeFromArgs reads it from the command line.
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

// ExchangeCodeFromArgs returns the exchange code the Epic launcher passes as -AUTH_PASSWORD=<code> together with -AUTH_TYPE=exchangecode, or "" when args carry no exchange-code login. Pass os.Args[1:].
func ExchangeCodeFromArgs(args []string) string {
	var code, typ string
	for _, a := range args {
		name, value, ok := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !ok || !strings.HasPrefix(a, "-") {
			continue
		}
		switch strings.ToUpper(name) {
		case "AUTH_PASSWORD":
			code = value
		case "AUTH_TYPE":
			typ = value
		}
	}
	if !strings.EqualFold(typ, "exchangecode") {
		return ""
	}
	return code
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

// Driver is the native boundary implemented by an EOS SDK adapter. Every operation is asynchronous, as in the EOS SDK: a method starts the request and returns, and the driver calls done exactly once when the request completes, normally from inside Tick, where the EOS SDK dispatches its callbacks, or at once when the request cannot start. Platform calls every method and Tick on the engine goroutine and posts each done result back to its scope, so a driver may also call done from another goroutine.
type Driver interface {
	Login(exchangeCode string, done func(Player, error))
	UnlockAchievement(id string, done func(error))
	ProgressAchievement(id string, percent float64, done func(error))
	SubmitScore(board string, score int64, done func(error))
	QueryLeaderboard(board string, friends bool, limit int, done func([]LeaderboardEntry, error))
	SetPresence(details, status string, done func(error))
	Tick()
	Close() error
}

// Platform owns one EOS connection and its driver. Its methods are safe only on the engine goroutine, matching sound and game services. Every done callback runs on the engine goroutine under the scope given to New, never inside the call that started the request; results that arrive after Close or after the scope is disposed are dropped.
type Platform struct {
	cx     *core.Scope
	cfg    Config
	driver Driver
	player *core.Signal[Player]
	closed bool
}

// New creates a platform around an injected driver and closes it when cx is disposed. Native adapters should return ErrUnavailable when the EOS shared library cannot be loaded; a nil driver yields ErrUnavailable.
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

// Player returns the authenticated player, or zero while signed out. It is a reactive read.
func (p *Platform) Player() Player {
	if p == nil || p.player == nil {
		return Player{}
	}
	return p.player.Get()
}

// SignedIn reports whether Login succeeded.
func (p *Platform) SignedIn() bool { return p.Player().ProductUserID != "" }

// Tick pumps the native EOS callback queue once. Call it every step of the fixed game loop.
func (p *Platform) Tick() {
	if p != nil && !p.closed {
		p.driver.Tick()
	}
}

// post runs fn on the engine goroutine unless the platform has been closed by then.
func (p *Platform) post(fn func()) {
	p.cx.Post(func() {
		if !p.closed {
			fn()
		}
	})
}

// fail reports err to done on the engine goroutine, for requests that never reach the driver.
func (p *Platform) fail(done func(error), err error) {
	if done != nil {
		p.cx.Post(func() { done(err) })
	}
}

// Login authenticates with the configured exchange code. On success Player and SignedIn report the account; on failure they report signed out. done may be nil.
func (p *Platform) Login(done func(Player, error)) {
	if p.closed {
		if done != nil {
			p.cx.Post(func() { done(Player{}, ErrClosed) })
		}
		return
	}
	if p.cfg.ExchangeCode == "" {
		if done != nil {
			p.cx.Post(func() { done(Player{}, ErrUnauthenticated) })
		}
		return
	}
	p.driver.Login(p.cfg.ExchangeCode, func(player Player, err error) {
		p.post(func() {
			if err == nil && player.ProductUserID == "" {
				err = ErrUnauthenticated
			}
			if err != nil {
				player = Player{}
			}
			p.player.Set(player)
			if done != nil {
				done(player, err)
			}
		})
	})
}

// complete wraps done so the driver's result is posted to the engine goroutine.
func (p *Platform) complete(done func(error)) func(error) {
	return func(err error) {
		p.post(func() {
			if done != nil {
				done(err)
			}
		})
	}
}

// start reports ErrClosed, ErrUnauthenticated or ErrInvalidArgument through done and returns false when a reporting request must not reach the driver.
func (p *Platform) start(id string, done func(error)) bool {
	switch {
	case p.closed:
		p.fail(done, ErrClosed)
	case p.player.GetUntracked().ProductUserID == "":
		p.fail(done, ErrUnauthenticated)
	case strings.TrimSpace(id) == "":
		p.fail(done, ErrInvalidArgument)
	default:
		return true
	}
	return false
}

// UnlockAchievement reports an achievement ID to EOS. done may be nil.
func (p *Platform) UnlockAchievement(id string, done func(error)) {
	if p.start(id, done) {
		p.driver.UnlockAchievement(id, p.complete(done))
	}
}

// ProgressAchievement reports achievement progress in [0,100]; other values yield ErrInvalidArgument. done may be nil.
func (p *Platform) ProgressAchievement(id string, percent float64, done func(error)) {
	if !(percent >= 0 && percent <= 100) {
		p.fail(done, ErrInvalidArgument)
		return
	}
	if p.start(id, done) {
		p.driver.ProgressAchievement(id, percent, p.complete(done))
	}
}

// SubmitScore submits a score to a named EOS leaderboard. done may be nil.
func (p *Platform) SubmitScore(board string, score int64, done func(error)) {
	if p.start(board, done) {
		p.driver.SubmitScore(board, score, p.complete(done))
	}
}

// QueryLeaderboard loads up to limit global or friends rows from an EOS leaderboard; limit must be positive. done must not be nil.
func (p *Platform) QueryLeaderboard(board string, friends bool, limit int, done func([]LeaderboardEntry, error)) {
	failed := func(err error) { p.cx.Post(func() { done(nil, err) }) }
	if limit <= 0 {
		failed(ErrInvalidArgument)
		return
	}
	if !p.start(board, func(err error) { done(nil, err) }) {
		return
	}
	p.driver.QueryLeaderboard(board, friends, limit, func(entries []LeaderboardEntry, err error) {
		p.post(func() { done(entries, err) })
	})
}

// SetPresence publishes rich presence details and status. done may be nil.
func (p *Platform) SetPresence(details, status string, done func(error)) {
	switch {
	case p.closed:
		p.fail(done, ErrClosed)
	case p.player.GetUntracked().ProductUserID == "":
		p.fail(done, ErrUnauthenticated)
	default:
		p.driver.SetPresence(details, status, p.complete(done))
	}
}

// Close shuts down the native driver and signs the player out. It is idempotent.
func (p *Platform) Close() error {
	if p == nil || p.closed {
		return nil
	}
	p.closed = true
	p.player.Set(Player{})
	return p.driver.Close()
}
