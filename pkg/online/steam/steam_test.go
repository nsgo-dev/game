package steam_test

import (
	"context"
	"github.com/nsgo-dev/game/pkg/online/steam"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"testing"
)

type fake struct {
	achievements []string
	stored       int
}

func (f *fake) Init(context.Context) (steam.Player, error) {
	return steam.Player{SteamID: "u1", PersonaName: "Test"}, nil
}
func (f *fake) SetAchievement(_ context.Context, id string) error {
	f.achievements = append(f.achievements, id)
	return nil
}
func (*fake) SetStatInt(context.Context, string, int64) error        { return nil }
func (*fake) SetStatFloat(context.Context, string, float64) error    { return nil }
func (f *fake) StoreStats(context.Context) error                     { f.stored++; return nil }
func (*fake) UploadScore(context.Context, string, int64, bool) error { return nil }
func (*fake) QueryScores(context.Context, string, bool, int) ([]steam.Entry, error) {
	return []steam.Entry{{Rank: 1, SteamID: "u1", Score: 10}}, nil
}
func (*fake) SetRichPresence(context.Context, string, string) error { return nil }
func (*fake) RunCallbacks()                                         {}
func (*fake) Shutdown() error                                       { return nil }
func TestPlatformCallbacksAndStats(t *testing.T) {
	var p *steam.Platform
	f := &fake{}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var err error
		p, err = steam.New(cx, f)
		if err != nil {
			t.Fatal(err)
		}
		p.Init(nil)
		return core.View{}
	})
	app.Advance(0)
	if !p.SignedIn() {
		t.Fatal("not signed in")
	}
	done := make(chan error, 1)
	p.Unlock("ACH_FIRST", func(err error) { done <- err })
	app.Advance(0)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(f.achievements) != 1 {
		t.Fatal(f.achievements)
	}
}
