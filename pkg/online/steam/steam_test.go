package steam_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/steam"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type fake struct {
	player       steam.Player
	achievements []string
	stored       int
	shutdowns    int
}

func newFake() *fake { return &fake{player: steam.Player{SteamID: "u1", PersonaName: "Test"}} }

func (f *fake) Init(context.Context) (steam.Player, error) { return f.player, nil }
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
func (f *fake) Shutdown() error                                     { f.shutdowns++; return nil }

func start(t *testing.T, f *fake) (*nsgotest.App, *steam.Platform) {
	t.Helper()
	var p *steam.Platform
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var err error
		p, err = steam.New(cx, f)
		if err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})
	return app, p
}

func TestNewWithoutDriverIsUnavailable(t *testing.T) {
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		if _, err := steam.New(cx, nil); !errors.Is(err, steam.ErrUnavailable) {
			t.Fatalf("New(nil driver) = %v, want ErrUnavailable", err)
		}
		return core.View{}
	})
}

func TestPlatformCallbacksAndStats(t *testing.T) {
	f := newFake()
	app, p := start(t, f)
	var initErr error
	p.Init(func(_ steam.Player, err error) { initErr = err })
	if p.SignedIn() {
		t.Fatal("signed in before the Init callback ran")
	}
	app.Advance(0)
	if initErr != nil || !p.SignedIn() || p.Player().PersonaName != "Test" {
		t.Fatalf("after Init: err %v, player %+v", initErr, p.Player())
	}

	var unlockErr, storeErr error = errors.New("not called"), errors.New("not called")
	p.Unlock("ACH_FIRST", func(err error) { unlockErr = err })
	p.StoreStats(func(err error) { storeErr = err })
	app.Advance(0)
	if unlockErr != nil || storeErr != nil {
		t.Fatalf("Unlock %v, StoreStats %v", unlockErr, storeErr)
	}
	if len(f.achievements) != 1 || f.achievements[0] != "ACH_FIRST" || f.stored != 1 {
		t.Fatalf("driver saw achievements %v, %d stores", f.achievements, f.stored)
	}

	var entries []steam.Entry
	p.Scores("HighScores", false, 10, func(e []steam.Entry, err error) {
		if err != nil {
			t.Error(err)
		}
		entries = e
	})
	app.Advance(0)
	if len(entries) != 1 || entries[0].Score != 10 {
		t.Fatalf("Scores = %+v", entries)
	}
}

func TestInitWithoutSteamIDIsUnauthenticated(t *testing.T) {
	f := newFake()
	f.player = steam.Player{}
	app, p := start(t, f)
	var initErr error
	p.Init(func(_ steam.Player, err error) { initErr = err })
	app.Advance(0)
	if !errors.Is(initErr, steam.ErrUnauthenticated) || p.SignedIn() {
		t.Fatalf("Init err %v, signed in %v", initErr, p.SignedIn())
	}
}

func TestCloseSignsOutAndRefusesCalls(t *testing.T) {
	f := newFake()
	app, p := start(t, f)
	p.Init(nil)
	app.Advance(0)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil || f.shutdowns != 1 {
		t.Fatalf("second Close: err %v, %d shutdowns", err, f.shutdowns)
	}
	if p.SignedIn() {
		t.Fatal("signed in after Close")
	}
	var initErr, unlockErr error
	p.Init(func(_ steam.Player, err error) { initErr = err })
	p.Unlock("ACH_FIRST", func(err error) { unlockErr = err })
	if !errors.Is(initErr, steam.ErrClosed) || !errors.Is(unlockErr, steam.ErrClosed) {
		t.Fatalf("after Close: Init %v, Unlock %v", initErr, unlockErr)
	}
	if len(f.achievements) != 0 {
		t.Fatalf("driver saw achievements after Close: %v", f.achievements)
	}
}
