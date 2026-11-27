package achievements_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/achievements"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gameservices"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestServiceFor(t *testing.T) {
	for os, want := range map[core.OS]achievements.Service{
		core.OSMacOS:   achievements.GameCenter,
		core.OSIOS:     achievements.GameCenter,
		core.OSAndroid: achievements.PlayGames,
		core.OSWindows: "",
		core.OSLinux:   "",
		core.OSWeb:     "",
	} {
		if got := achievements.ServiceFor(os); got != want {
			t.Errorf("ServiceFor(%q) = %q, want %q", os, got, want)
		}
	}
}

func startScope(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *core.Scope) {
	t.Helper()
	var scope *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		scope = cx
		return core.View{}
	}, opts...)
	return app, scope
}

func gameServicesOn(os core.OS) nsgotest.Option {
	return nsgotest.Environment(core.Environment{OS: os, CanUseGameServices: true})
}

type signInResult struct {
	player gameservices.Player
	err    error
	calls  int
}

func signIn(app *nsgotest.App, cx *core.Scope, tr *achievements.Tracker) *signInResult {
	res := &signInResult{}
	achievements.SignIn(cx, tr, func(p gameservices.Player, err error) {
		res.player, res.err = p, err
		res.calls++
	})
	app.Settle()
	return res
}

func TestSignInUnlocksOnEachService(t *testing.T) {
	cases := []struct {
		os      core.OS
		service achievements.Service
		want    []gameservices.Achievement
	}{
		{core.OSMacOS, achievements.GameCenter, []gameservices.Achievement{
			{ID: "gc.apple", PercentComplete: 100, Unlocked: true},
			{ID: "gc.first", PercentComplete: 100, Unlocked: true},
			{ID: "gc.gems", PercentComplete: 50},
		}},
		{core.OSIOS, achievements.GameCenter, []gameservices.Achievement{
			{ID: "gc.apple", PercentComplete: 100, Unlocked: true},
			{ID: "gc.first", PercentComplete: 100, Unlocked: true},
			{ID: "gc.gems", PercentComplete: 50},
		}},
		{core.OSAndroid, achievements.PlayGames, []gameservices.Achievement{
			{ID: "pg.first", PercentComplete: 100, Unlocked: true},
			{ID: "pg.gems", PercentComplete: 50},
		}},
	}
	for _, tc := range cases {
		t.Run(string(tc.os), func(t *testing.T) {
			app, cx := startScope(t, gameServicesOn(tc.os))
			app.SetGameServicesPlayer(gameservices.Player{ID: "p1", DisplayName: "Ada"})
			tr := newTracker(t)
			// Progress made before sign-in is reported once the player signs in.
			mustOK(t, tr.Unlock("first-match"))
			mustOK(t, tr.Unlock("apple-only"))
			mustOK(t, tr.Add("gem-collector", 1))

			res := signIn(app, cx, tr)
			if res.err != nil || res.calls != 1 || res.player.DisplayName != "Ada" {
				t.Fatalf("SignIn = %+v", res)
			}
			if tr.Service() != tc.service {
				t.Fatalf("service = %q, want %q", tr.Service(), tc.service)
			}
			mustOK(t, tr.Add("gem-collector", 1))
			app.Settle()
			if got := app.GameServicesAchievements(); !slices.Equal(got, tc.want) {
				t.Fatalf("service achievements:\ngot  %+v\nwant %+v", got, tc.want)
			}
			if got := tr.Pending(tc.service); len(got) != 0 {
				t.Fatalf("pending after acknowledgement: %v", got)
			}
		})
	}
}

func TestSignInWithoutGameServices(t *testing.T) {
	for name, opt := range map[string]nsgotest.Option{
		"capability off": nsgotest.Environment(core.Environment{OS: core.OSIOS}),
		"desktop":        gameServicesOn(core.OSWindows),
	} {
		t.Run(name, func(t *testing.T) {
			app, cx := startScope(t, opt)
			tr := newTracker(t)
			mustOK(t, tr.Unlock("first-match"))
			res := signIn(app, cx, tr)
			if !errors.Is(res.err, gameservices.ErrUnsupported) || res.calls != 1 {
				t.Fatalf("SignIn = %+v", res)
			}
			if tr.Service() != "" || !tr.Unlocked("first-match") {
				t.Fatal("tracker changed without a game service")
			}
			if got := app.GameServicesAchievements(); len(got) != 0 {
				t.Fatalf("reported without a game service: %+v", got)
			}
		})
	}
}

func TestSignInFailureKeepsProgressForALaterSignIn(t *testing.T) {
	app, cx := startScope(t, gameServicesOn(core.OSMacOS))
	tr := newTracker(t)
	mustOK(t, tr.Unlock("first-match"))

	if res := signIn(app, cx, tr); !errors.Is(res.err, gameservices.ErrUnauthenticated) {
		t.Fatalf("SignIn without a player = %+v", res)
	}
	app.SetGameServicesAuthError(gameservices.ErrCancelled)
	app.SetGameServicesPlayer(gameservices.Player{ID: "p1", DisplayName: "Ada"})
	if res := signIn(app, cx, tr); !errors.Is(res.err, gameservices.ErrCancelled) {
		t.Fatalf("cancelled SignIn = %+v", res)
	}
	if tr.Service() != "" || len(app.GameServicesAchievements()) != 0 {
		t.Fatal("connected after a failed sign-in")
	}

	app.SetGameServicesAuthError(nil)
	if res := signIn(app, cx, tr); res.err != nil {
		t.Fatalf("SignIn = %+v", res)
	}
	want := []gameservices.Achievement{{ID: "gc.first", PercentComplete: 100, Unlocked: true}}
	if got := app.GameServicesAchievements(); !slices.Equal(got, want) {
		t.Fatalf("service achievements = %+v", got)
	}
	// SignIn with a nil callback is allowed.
	achievements.SignIn(cx, tr, nil)
	app.Settle()
}

func TestServiceOutageIsRetriedOnFlush(t *testing.T) {
	app, cx := startScope(t, gameServicesOn(core.OSAndroid))
	app.SetGameServicesPlayer(gameservices.Player{ID: "p1", Authenticated: true})
	tr := newTracker(t)
	var errs []error
	tr.OnError(func(_ achievements.Definition, err error) { errs = append(errs, err) })
	if res := signIn(app, cx, tr); res.err != nil {
		t.Fatal(res.err)
	}

	app.SetGameServicesError(gameservices.ErrUnavailable)
	mustOK(t, tr.Unlock("first-match"))
	app.Settle()
	if len(errs) != 1 || !errors.Is(errs[0], gameservices.ErrUnavailable) {
		t.Fatalf("errors = %v", errs)
	}
	if got := tr.Pending(achievements.PlayGames); !slices.Equal(got, []string{"first-match"}) {
		t.Fatalf("pending = %v", got)
	}

	app.SetGameServicesError(nil)
	tr.Flush()
	app.Settle()
	want := []gameservices.Achievement{{ID: "pg.first", PercentComplete: 100, Unlocked: true}}
	if got := app.GameServicesAchievements(); !slices.Equal(got, want) {
		t.Fatalf("service achievements = %+v", got)
	}
}

func TestDisposingTheSignInScopeDisconnects(t *testing.T) {
	app, cx := startScope(t, gameServicesOn(core.OSMacOS))
	app.SetGameServicesPlayer(gameservices.Player{ID: "p1", Authenticated: true})
	tr := newTracker(t)
	var screen *core.Scope
	app.Post(func() { screen = cx.Child() })
	app.Settle()
	if res := signIn(app, screen, tr); res.err != nil {
		t.Fatal(res.err)
	}

	// The answer to this report is posted to screen, which is disposed before it can run.
	app.Post(func() {
		errorAsync(t, tr.Add("gem-collector", 1))
		screen.Dispose()
	})
	app.Settle()
	if tr.Service() != "" {
		t.Fatalf("service = %q after its scope was disposed", tr.Service())
	}
	if got := tr.Pending(achievements.GameCenter); !slices.Equal(got, []string{"gem-collector"}) {
		t.Fatalf("pending = %v", got)
	}

	// A later SignIn on a living scope reports what the disposed one never confirmed, and later progress keeps flowing.
	if res := signIn(app, cx, tr); res.err != nil {
		t.Fatal(res.err)
	}
	mustOK(t, tr.Add("gem-collector", 1))
	app.Settle()
	if got := tr.Pending(achievements.GameCenter); len(got) != 0 {
		t.Fatalf("pending = %v", got)
	}
	want := []gameservices.Achievement{{ID: "gc.gems", PercentComplete: 50}}
	if got := app.GameServicesAchievements(); !slices.Equal(got, want) {
		t.Fatalf("service achievements = %+v", got)
	}
}

func TestPersistInCoreStorage(t *testing.T) {
	app, cx := startScope(t)
	tr := newTracker(t)
	mustOK(t, tr.Persist(cx.Storage(), "game.achievements"))
	mustOK(t, tr.Add("gem-collector", 2))
	raw, ok := app.Storage().GetItem("game.achievements")
	if !ok || raw != "version: 1\nachievements:\n  - id: gem-collector\n    steps: 2\n" {
		t.Fatalf("stored %q", raw)
	}
}
