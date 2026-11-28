// Package steam defines the engine-facing boundary for Steamworks user stats,
// achievements, leaderboards and rich presence. Native adapters implement
// Driver, while tests can use a fake without loading the Steam SDK.
package steam

import (
	"context"
	"errors"

	"github.com/nsgo-dev/nsgo/core"
)

var (
	ErrUnavailable     = errors.New("steam: unavailable")
	ErrClosed          = errors.New("steam: closed")
	ErrUnauthenticated = errors.New("steam: unauthenticated")
)

type Player struct{ SteamID, PersonaName string }
type Entry struct {
	Rank                 int
	SteamID, PersonaName string
	Score                int64
}

// Driver is implemented by a Steamworks adapter. Calls are synchronous at the
// boundary and Platform posts completion callbacks to its core scope.
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

type Platform struct {
	cx     *core.Scope
	driver Driver
	player *core.Signal[Player]
	closed bool
}

// New creates a Steam platform around driver. A nil driver returns ErrUnavailable.
func New(cx *core.Scope, driver Driver) (*Platform, error) {
	if cx == nil {
		return nil, ErrUnavailable
	}
	if driver == nil {
		return nil, ErrUnavailable
	}
	p := &Platform{cx: cx, driver: driver, player: core.NewSignal(cx, Player{})}
	cx.Cleanup(func() { _ = p.Close() })
	return p, nil
}

func (p *Platform) Player() Player {
	if p == nil || p.player == nil {
		return Player{}
	}
	return p.player.Get()
}
func (p *Platform) SignedIn() bool { return p.Player().SteamID != "" }
func (p *Platform) Tick() {
	if p != nil && !p.closed {
		p.driver.RunCallbacks()
	}
}

func (p *Platform) Init(done func(Player, error)) {
	if p == nil || p.closed {
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
func (p *Platform) Unlock(id string, done func(error)) {
	p.call(func() error { return p.driver.SetAchievement(context.Background(), id) }, done)
}
func (p *Platform) SetStatInt(id string, value int64, done func(error)) {
	p.call(func() error { return p.driver.SetStatInt(context.Background(), id, value) }, done)
}
func (p *Platform) SetStatFloat(id string, value float64, done func(error)) {
	p.call(func() error { return p.driver.SetStatFloat(context.Background(), id, value) }, done)
}
func (p *Platform) StoreStats(done func(error)) {
	p.call(func() error { return p.driver.StoreStats(context.Background()) }, done)
}
func (p *Platform) UploadScore(board string, score int64, keepBest bool, done func(error)) {
	p.call(func() error { return p.driver.UploadScore(context.Background(), board, score, keepBest) }, done)
}
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
func (p *Platform) SetPresence(key, value string, done func(error)) {
	p.call(func() error { return p.driver.SetRichPresence(context.Background(), key, value) }, done)
}
func (p *Platform) Close() error {
	if p == nil || p.closed {
		return nil
	}
	p.closed = true
	p.player.Set(Player{})
	return p.driver.Shutdown()
}
