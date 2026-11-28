package eos_test

import (
	"context"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/eos"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type fake struct {
	logged  bool
	unlocks []string
}

func (f *fake) Login(context.Context, string) (eos.Player, error) {
	f.logged = true
	return eos.Player{ProductUserID: "p1", DisplayName: "Player"}, nil
}
func (f *fake) UnlockAchievement(_ context.Context, id string) error {
	f.unlocks = append(f.unlocks, id)
	return nil
}
func (*fake) ProgressAchievement(context.Context, string, float64) error { return nil }
func (*fake) SubmitScore(context.Context, string, int64) error           { return nil }
func (*fake) QueryLeaderboard(context.Context, string, bool, int) ([]eos.LeaderboardEntry, error) {
	return []eos.LeaderboardEntry{{Rank: 1, UserID: "p1", Score: 42}}, nil
}
func (*fake) SetPresence(context.Context, string, string) error { return nil }
func (*fake) Tick()                                             {}
func (*fake) Close() error                                      { return nil }

func TestPlatformPostsLoginAndReportsAchievement(t *testing.T) {
	var platform *eos.Platform
	f := &fake{}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var err error
		platform, err = eos.New(cx, eos.Config{ProductID: "p", SandboxID: "s", DeploymentID: "d", ExchangeCode: "x"}, f)
		if err != nil {
			t.Fatal(err)
		}
		platform.Login(func(player eos.Player, err error) {
			if err != nil || player.ProductUserID != "p1" {
				t.Errorf("login = %+v, %v", player, err)
			}
		})
		return core.View{}
	})
	app.Advance(0)
	if !platform.SignedIn() || !f.logged {
		t.Fatalf("player=%+v logged=%v", platform.Player(), f.logged)
	}
	done := make(chan error, 1)
	platform.UnlockAchievement("first", func(err error) { done <- err })
	app.Advance(0)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(f.unlocks) != 1 || f.unlocks[0] != "first" {
		t.Fatalf("unlocks=%v", f.unlocks)
	}
}

func TestPlatformRejectsMissingNativeDriver(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		_, err := eos.New(cx, eos.Config{ProductID: "p", SandboxID: "s", DeploymentID: "d"}, nil)
		if err != eos.ErrUnavailable {
			t.Errorf("err=%v", err)
		}
		return core.View{}
	})
	app.Advance(0)
}
