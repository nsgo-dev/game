package eos_test

import (
	"context"
	"github.com/nsgo-dev/game/pkg/online/eos"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"sync"
	"testing"
)

type fake struct {
	mu                                sync.Mutex
	player                            eos.Player
	unlock, progress, score, presence int
	rows                              []eos.Score
}

func (f *fake) Login(_ context.Context, code string, cb func(eos.Player, error)) {
	f.mu.Lock()
	f.player = eos.Player{ID: code, Authenticated: true}
	f.mu.Unlock()
	cb(f.player, nil)
}
func (f *fake) Unlock(context.Context, string, func(error))             { f.unlock++ }
func (f *fake) Progress(context.Context, string, float64, func(error))  { f.progress++ }
func (f *fake) SubmitScore(context.Context, string, int64, func(error)) { f.score++ }
func (f *fake) Scores(context.Context, string, int, bool, func([]eos.Score, error)) {
	cb := func([]eos.Score, error) {}
	_ = cb
}
func (f *fake) SetPresence(context.Context, eos.Presence, func(error)) { f.presence++ }
func (f *fake) Tick()                                                  {}
func (f *fake) Close()                                                 {}
func TestServicePostsCallbacksAndUsesExchangeCode(t *testing.T) {
	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node { cx = scope; return nil })
	d := &fake{}
	s, _ := eos.New(cx, eos.Config{ExchangeCode: "launcher", Driver: d})
	var got eos.Player
	done := make(chan struct{})
	s.Login(context.Background(), "", func(p eos.Player, err error) { got = p; close(done) })
	app.Settle()
	select {
	case <-done:
	default:
		t.Fatal("callback not posted")
	}
	if got.ID != "launcher" || !got.Authenticated {
		t.Fatalf("player=%+v", got)
	}
}
func TestExchangeCode(t *testing.T) {
	if eos.ExchangeCode([]string{"-AUTH_TYPE=exchangecode", "-AUTH_PASSWORD", "abc"}) != "abc" {
		t.Fatal("exchange code parse")
	}
}
