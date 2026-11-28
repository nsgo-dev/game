// Package eos provides a small asynchronous Epic Online Services boundary.
// The production SDK binding is supplied by a platform driver. Tests and
// unsupported targets can use a fake driver without loading a native library.
package eos

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
)

var (
	ErrUnavailable     = errors.New("eos: unavailable")
	ErrUnauthenticated = errors.New("eos: unauthenticated")
	ErrClosed          = errors.New("eos: closed")
)

type Player struct {
	ID            string
	DisplayName   string
	Authenticated bool
}
type Score struct {
	Rank        int
	PlayerID    string
	DisplayName string
	Score       int64
}
type Presence struct{ Details, Status, JoinInfo string }

// Driver is the asynchronous boundary implemented by EOS SDK bindings.
type Driver interface {
	Login(context.Context, string, func(Player, error))
	Unlock(context.Context, string, func(error))
	Progress(context.Context, string, float64, func(error))
	SubmitScore(context.Context, string, int64, func(error))
	Scores(context.Context, string, int, bool, func([]Score, error))
	SetPresence(context.Context, Presence, func(error))
	Tick()
	Close()
}

type Config struct {
	ProductID, SandboxID, DeploymentID, ExchangeCode string
	Driver                                           Driver
}

type Service struct {
	cx     *core.Scope
	cfg    Config
	mu     sync.Mutex
	driver Driver
	player Player
	closed bool
}

func New(cx *core.Scope, cfg Config) (*Service, error) {
	if cx == nil {
		return nil, errors.New("eos: nil scope")
	}
	d := cfg.Driver
	if d == nil {
		return nil, ErrUnavailable
	}
	return &Service{cx: cx, cfg: cfg, driver: d}, nil
}
func ExchangeCode(args []string) string {
	for i, a := range args {
		if a == "-AUTH_PASSWORD" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, "-AUTH_PASSWORD=") {
			return strings.TrimPrefix(a, "-AUTH_PASSWORD=")
		}
	}
	return ""
}
func (s *Service) Player() Player { s.mu.Lock(); defer s.mu.Unlock(); return s.player }
func (s *Service) Login(ctx context.Context, code string, done func(Player, error)) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		s.post(func() { done(Player{}, ErrClosed) })
		return
	}
	d := s.driver
	if code == "" {
		code = s.cfg.ExchangeCode
	}
	s.mu.Unlock()
	d.Login(ctx, code, func(p Player, err error) {
		s.post(func() {
			if err == nil {
				s.mu.Lock()
				s.player = p
				s.mu.Unlock()
			}
			if done != nil {
				done(p, err)
			}
		})
	})
}
func (s *Service) Unlock(ctx context.Context, id string, done func(error)) {
	s.call(func(d Driver) {
		d.Unlock(ctx, id, func(err error) {
			s.post(func() {
				if done != nil {
					done(err)
				}
			})
		})
	}, done)
}
func (s *Service) Progress(ctx context.Context, id string, p float64, done func(error)) {
	s.call(func(d Driver) {
		d.Progress(ctx, id, p, func(err error) {
			s.post(func() {
				if done != nil {
					done(err)
				}
			})
		})
	}, done)
}
func (s *Service) SubmitScore(ctx context.Context, id string, score int64, done func(error)) {
	s.call(func(d Driver) {
		d.SubmitScore(ctx, id, score, func(err error) {
			s.post(func() {
				if done != nil {
					done(err)
				}
			})
		})
	}, done)
}
func (s *Service) Scores(ctx context.Context, id string, limit int, friends bool, done func([]Score, error)) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		s.post(func() { done(nil, ErrClosed) })
		return
	}
	d := s.driver
	s.mu.Unlock()
	d.Scores(ctx, id, limit, friends, func(rows []Score, err error) {
		s.post(func() {
			if done != nil {
				done(rows, err)
			}
		})
	})
}
func (s *Service) Presence(ctx context.Context, p Presence, done func(error)) {
	s.call(func(d Driver) {
		d.SetPresence(ctx, p, func(err error) {
			s.post(func() {
				if done != nil {
					done(err)
				}
			})
		})
	}, done)
}
func (s *Service) Tick() {
	s.mu.Lock()
	d := s.driver
	closed := s.closed
	s.mu.Unlock()
	if !closed {
		d.Tick()
	}
}
func (s *Service) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	d := s.driver
	s.mu.Unlock()
	d.Close()
}
func (s *Service) call(start func(Driver), done func(error)) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		s.post(func() {
			if done != nil {
				done(ErrClosed)
			}
		})
		return
	}
	d := s.driver
	s.mu.Unlock()
	start(d)
}
func (s *Service) post(fn func()) {
	if fn != nil {
		s.cx.Post(fn)
	}
}

// NewUnavailable returns the unsupported-target service error consistently.
func NewUnavailable() error { return ErrUnavailable }

// EnvExchangeCode reads the launcher exchange code from the process arguments.
func EnvExchangeCode() string { return ExchangeCode(os.Args[1:]) }
