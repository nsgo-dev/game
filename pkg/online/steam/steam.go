// Package steam defines the engine-facing boundary for Steamworks user stats, achievements, leaderboards and rich presence. Native adapters implement Driver, while tests can use a fake without loading the Steam SDK.
package steam

import (
	"context"
	"errors"

	"github.com/nsgo-dev/nsgo/core"
)

var (
	// ErrUnavailable reports that no Steamworks driver is available: New was given a nil scope or driver.
	ErrUnavailable = errors.New("steam: unavailable")
	// ErrClosed reports a call on a Platform that is nil or has been closed.
	ErrClosed = errors.New("steam: closed")
	// ErrUnauthenticated reports that the driver initialized without a signed-in Steam user.
	ErrUnauthenticated = errors.New("steam: unauthenticated")
)

// Player is the signed-in Steam user.
type Player struct{ SteamID, PersonaName string }

// Entry is one leaderboard row.
type Entry struct {
	Rank                 int
	SteamID, PersonaName string
	Score                int64
}

// Driver is implemented by a Steamworks adapter. Calls are synchronous at the boundary and Platform posts completion callbacks to its core scope.
type Driver interface {
	Init(context.Context) (Player, error)
	SetAchievement(context.Context, string) error
	SetStatInt(context.Context, string, int64) error
	SetStatFloat(context.Context, string, float64) error
	StoreStats(context.Context) error
	UploadScore(context.Context, string, int64, bool) error
	QueryScores(context.Context, string, bool, int) ([]Entry, error)
	SetRichPresence(context.Context, string, string) error
	RunCallbacks()
	Shutdown() error
}

// Platform wraps a Driver for one core scope. Use it from the scope's goroutine; every completion callback runs there through Scope.Post, and none runs once the scope is disposed.
type Platform struct {
	cx     *core.Scope
	driver Driver
	player *core.Signal[Player]
	closed bool
}

// New creates a Steam platform around driver and closes it when cx is disposed. A nil scope or driver returns ErrUnavailable.
func New(cx *core.Scope, driver Driver) (*Platform, error) {
	if cx == nil || driver == nil {
		return nil, ErrUnavailable
	}
	p := &Platform{cx: cx, driver: driver, player: core.NewSignal(cx, Player{})}
	cx.Cleanup(func() { _ = p.Close() })
	return p, nil
}

// Player returns the signed-in user, or the zero Player before Init succeeds and after Close.
func (p *Platform) Player() Player {
	if p == nil || p.player == nil {
		return Player{}
	}
	return p.player.Get()
}

// SignedIn reports whether Init has signed in a Steam user.
func (p *Platform) SignedIn() bool { return p.Player().SteamID != "" }

// Tick pumps the driver's Steam callbacks; call it once per fixed step.
func (p *Platform) Tick() {
	if p != nil && !p.closed {
		p.driver.RunCallbacks()
	}
}

// Init initializes the driver and signs the user in. done receives the player, or ErrUnauthenticated when the driver returns no Steam ID, or ErrClosed on a closed Platform.
func (p *Platform) Init(done func(Player, error)) {
	if p == nil || p.closed {
		if done != nil {
			done(Player{}, ErrClosed)
		}
		return
	}
	player, err := p.driver.Init(context.Background())
	if err == nil && player.SteamID == "" {
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

// Unlock sets the achievement id. Call StoreStats to commit it to Steam.
func (p *Platform) Unlock(id string, done func(error)) {
	p.call(func() error { return p.driver.SetAchievement(context.Background(), id) }, done)
}

// SetStatInt sets the INT stat id. Call StoreStats to commit it to Steam.
func (p *Platform) SetStatInt(id string, value int64, done func(error)) {
	p.call(func() error { return p.driver.SetStatInt(context.Background(), id, value) }, done)
}

// SetStatFloat sets the FLOAT stat id. Call StoreStats to commit it to Steam.
func (p *Platform) SetStatFloat(id string, value float64, done func(error)) {
	p.call(func() error { return p.driver.SetStatFloat(context.Background(), id, value) }, done)
}

// StoreStats commits the stats and achievements set since the last store.
func (p *Platform) StoreStats(done func(error)) {
	p.call(func() error { return p.driver.StoreStats(context.Background()) }, done)
}

// UploadScore submits score to board. With keepBest, Steam keeps the player's better score; otherwise it overwrites it.
func (p *Platform) UploadScore(board string, score int64, keepBest bool, done func(error)) {
	p.call(func() error { return p.driver.UploadScore(context.Background(), board, score, keepBest) }, done)
}

// Scores queries up to limit entries of board, from the global range or, with friends, the player's friends.
func (p *Platform) Scores(board string, friends bool, limit int, done func([]Entry, error)) {
	if p == nil || p.closed {
		if done != nil {
			done(nil, ErrClosed)
		}
		return
	}
	entries, err := p.driver.QueryScores(context.Background(), board, friends, limit)
	p.cx.Post(func() {
		if done != nil {
			done(entries, err)
		}
	})
}

// SetPresence sets the rich presence key to value.
func (p *Platform) SetPresence(key, value string, done func(error)) {
	p.call(func() error { return p.driver.SetRichPresence(context.Background(), key, value) }, done)
}

// Close signs the user out and shuts the driver down. Later calls report ErrClosed; closing again does nothing.
func (p *Platform) Close() error {
	if p == nil || p.closed {
		return nil
	}
	p.closed = true
	p.player.Set(Player{})
	return p.driver.Shutdown()
}
