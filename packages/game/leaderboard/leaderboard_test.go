package leaderboard_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/leaderboard"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const (
	highScoreGC = "dev.nsgo.game.example.high_score"
	highScorePG = "CgkI6v7z8vQREAIQAQ"
	fastestGC   = "dev.nsgo.game.example.fastest_run"
)

func testCatalog(t *testing.T) *leaderboard.Catalog {
	t.Helper()
	c, err := leaderboard.Parse([]byte(canonical))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return c
}

// start runs an empty app with the given environment and returns it with a Service over the canonical catalog.
func start(t *testing.T, env core.Environment) (*nsgotest.App, *leaderboard.Service) {
	t.Helper()
	var svc *leaderboard.Service
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s, err := leaderboard.New(cx, testCatalog(t))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		svc = s
		return core.View{}
	}, nsgotest.Environment(env))
	return app, svc
}

var (
	iosEnv     = core.Environment{CanUseGameServices: true, OS: core.OSIOS}
	androidEnv = core.Environment{CanUseGameServices: true, OS: core.OSAndroid}
	linuxEnv   = core.Environment{OS: core.OSLinux}
)

func TestProviderFor(t *testing.T) {
	cases := map[core.OS]leaderboard.Provider{
		core.OSIOS:     leaderboard.ProviderGameCenter,
		core.OSMacOS:   leaderboard.ProviderGameCenter,
		core.OSAndroid: leaderboard.ProviderPlayGames,
		core.OSWindows: leaderboard.ProviderNone,
		core.OSLinux:   leaderboard.ProviderNone,
		core.OSWeb:     leaderboard.ProviderNone,
	}
	for os, want := range cases {
		if got := leaderboard.ProviderFor(os); got != want {
			t.Errorf("ProviderFor(%v) = %v, want %v", os, got, want)
		}
	}
	if leaderboard.ProviderGameCenter.String() != "gameCenter" || leaderboard.ProviderPlayGames.String() != "playGames" || leaderboard.ProviderNone.String() != "none" {
		t.Fatal("unexpected provider names")
	}
}

func TestNewValidatesAndCopiesCatalog(t *testing.T) {
	var newErr, nilScopeErr error
	var svc *leaderboard.Service
	c := testCatalog(t)
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		_, newErr = leaderboard.New(cx, &leaderboard.Catalog{Version: 1})
		_, nilScopeErr = leaderboard.New(nil, c)
		svc, _ = leaderboard.New(cx, c)
		return core.View{}
	})
	if newErr == nil {
		t.Fatal("New accepted an empty catalog")
	}
	if nilScopeErr == nil {
		t.Fatal("New accepted a nil scope")
	}
	c.Leaderboards[0].GameCenter = "changed"
	if got := svc.Catalog().Leaderboards[0].GameCenter; got != highScoreGC {
		t.Fatalf("service catalog followed the caller's edit: %q", got)
	}
	svc.Catalog().Leaderboards[0].Name = "changed"
	if _, ok := svc.Catalog().Board("high-score"); !ok {
		t.Fatal("Catalog returned the service's own slice")
	}
}

func TestSubmitAndShowOnGameCenter(t *testing.T) {
	app, svc := start(t, iosEnv)
	app.SetGameServicesPlayer(leaderboard.Player{ID: "p-ada", DisplayName: "Ada"})

	if svc.Provider() != leaderboard.ProviderGameCenter || !svc.Available() || !svc.Supports("high-score") {
		t.Fatalf("provider=%v available=%v supports=%v", svc.Provider(), svc.Available(), svc.Supports("high-score"))
	}
	if id, err := svc.ID("high-score"); err != nil || id != highScoreGC {
		t.Fatalf("ID(high-score) = %q, %v", id, err)
	}

	var signedIn leaderboard.Player
	var signErr error
	svc.SignIn(func(p leaderboard.Player, err error) { signedIn, signErr = p, err })
	app.Settle()
	if signErr != nil || signedIn.ID != "p-ada" || !svc.SignedIn() || svc.Player().DisplayName != "Ada" {
		t.Fatalf("SignIn: player=%+v err=%v signedIn=%v", signedIn, signErr, svc.SignedIn())
	}

	submitErr := errors.New("not called")
	svc.Submit("high-score", 4200, func(err error) { submitErr = err })
	if submitErr == nil {
		t.Fatal("Submit called done before returning")
	}
	app.Settle()
	if submitErr != nil {
		t.Fatalf("Submit: %v", submitErr)
	}
	svc.Submit("fastest-run", 61, nil)
	app.Settle()

	got := app.GameServicesSubmittedScores()
	want := []core.GameScoreEntry{
		{LeaderboardID: highScoreGC, PlayerID: "p-ada", DisplayName: "Ada", Score: 4200},
		{LeaderboardID: fastestGC, PlayerID: "p-ada", DisplayName: "Ada", Score: 61},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("submitted = %+v, want %+v", got, want)
	}

	if err := svc.Show("high-score"); err != nil {
		t.Fatalf("Show: %v", err)
	}
	if shown := app.GameServicesShownLeaderboards(); !slices.Equal(shown, []string{highScoreGC}) {
		t.Fatalf("shown = %v", shown)
	}
}

func TestScoresUsesPlatformIDAndQuery(t *testing.T) {
	app, svc := start(t, androidEnv)
	app.SetGameServicesPlayer(leaderboard.Player{ID: "p1", DisplayName: "One", Authenticated: true})
	app.SetGameServicesFriends("p2")
	app.SeedLeaderboardScores(highScorePG,
		core.GameScoreEntry{PlayerID: "p2", DisplayName: "Two", Score: 900},
		core.GameScoreEntry{PlayerID: "p3", DisplayName: "Three", Score: 700},
		core.GameScoreEntry{PlayerID: "p4", DisplayName: "Four", Score: 300},
	)
	svc.Submit("high-score", 800, nil)

	var rows []leaderboard.Entry
	var err error
	svc.Scores("high-score", leaderboard.Query{Limit: 2}, func(e []leaderboard.Entry, e2 error) { rows, err = e, e2 })
	app.Settle()
	if err != nil {
		t.Fatalf("Scores: %v", err)
	}
	if len(rows) != 2 || rows[0].PlayerID != "p2" || rows[1].PlayerID != "p1" || rows[1].Rank != 2 || rows[1].LeaderboardID != highScorePG {
		t.Fatalf("global top 2 = %+v", rows)
	}

	// nsgotest's fake game service ignores the time scope (nsgo-dev/nsgo#2250), so this checks only that a Week query reaches the platform and answers.
	svc.Scores("high-score", leaderboard.Query{Players: leaderboard.Friends, Time: leaderboard.Week}, func(e []leaderboard.Entry, e2 error) { rows, err = e, e2 })
	app.Settle()
	if err != nil || len(rows) != 2 || rows[0].PlayerID != "p2" || rows[1].PlayerID != "p1" {
		t.Fatalf("friends = %+v, %v", rows, err)
	}
}

func TestScoresClampsLimit(t *testing.T) {
	app, svc := start(t, androidEnv)
	app.SetGameServicesPlayer(leaderboard.Player{ID: "p1", Authenticated: true})
	seed := make([]core.GameScoreEntry, 150)
	for i := range seed {
		seed[i] = core.GameScoreEntry{PlayerID: fmt.Sprintf("p%03d", i), Score: int64(1000 + i)}
	}
	app.SeedLeaderboardScores(highScorePG, seed...)

	for _, tc := range []struct{ limit, want int }{{500, 100}, {0, 25}, {-3, 25}, {40, 40}} {
		var rows []leaderboard.Entry
		var err error
		svc.Scores("high-score", leaderboard.Query{Limit: tc.limit}, func(e []leaderboard.Entry, e2 error) { rows, err = e, e2 })
		app.Settle()
		if err != nil || len(rows) != tc.want {
			t.Fatalf("Limit %d: %d rows, %v; want %d", tc.limit, len(rows), err, tc.want)
		}
		if rows[0].Score != 1149 || rows[0].Rank != 1 {
			t.Fatalf("Limit %d: first row %+v", tc.limit, rows[0])
		}
	}
}

func TestErrorsBeforeReachingThePlatform(t *testing.T) {
	app, svc := start(t, androidEnv)
	app.SetGameServicesPlayer(leaderboard.Player{ID: "p1", Authenticated: true})

	// "fastest-run" is only on Game Center.
	if svc.Supports("fastest-run") {
		t.Fatal("Supports(fastest-run) on Android")
	}
	var submitErr error
	svc.Submit("fastest-run", 10, func(err error) { submitErr = err })
	app.Settle()
	if !errors.Is(submitErr, leaderboard.ErrNoPlatformID) {
		t.Fatalf("Submit(fastest-run) = %v, want ErrNoPlatformID", submitErr)
	}
	if err := svc.Show("fastest-run"); !errors.Is(err, leaderboard.ErrNoPlatformID) {
		t.Fatalf("Show(fastest-run) = %v", err)
	}

	svc.Submit("hgih-score", 10, func(err error) { submitErr = err })
	app.Settle()
	if !errors.Is(submitErr, leaderboard.ErrUnknownLeaderboard) {
		t.Fatalf("Submit(typo) = %v, want ErrUnknownLeaderboard", submitErr)
	}
	var scoresErr error
	svc.Scores("hgih-score", leaderboard.Query{}, func(_ []leaderboard.Entry, err error) { scoresErr = err })
	app.Settle()
	if !errors.Is(scoresErr, leaderboard.ErrUnknownLeaderboard) {
		t.Fatalf("Scores(typo) = %v", scoresErr)
	}
	if len(app.GameServicesSubmittedScores()) != 0 || len(app.GameServicesShownLeaderboards()) != 0 {
		t.Fatal("a refused call reached the platform")
	}
}

func TestUnsupportedPlatform(t *testing.T) {
	app, svc := start(t, linuxEnv)
	if svc.Available() || svc.Supports("high-score") || svc.Provider() != leaderboard.ProviderNone {
		t.Fatal("Linux reported a game service")
	}
	var signErr, submitErr error
	svc.SignIn(func(_ leaderboard.Player, err error) { signErr = err })
	svc.Submit("high-score", 1, func(err error) { submitErr = err })
	app.Settle()
	if !errors.Is(signErr, leaderboard.ErrUnsupported) || !errors.Is(submitErr, leaderboard.ErrUnsupported) {
		t.Fatalf("SignIn=%v Submit=%v, want ErrUnsupported", signErr, submitErr)
	}
	if err := svc.Show("high-score"); !errors.Is(err, leaderboard.ErrUnsupported) {
		t.Fatalf("Show = %v", err)
	}
	// A typo is still reported as such on a platform without a game service.
	if err := svc.Show("typo"); !errors.Is(err, leaderboard.ErrUnknownLeaderboard) {
		t.Fatalf("Show(typo) = %v", err)
	}
}

func TestSignInFailureAndPlatformSignOut(t *testing.T) {
	app, svc := start(t, iosEnv)

	app.SetGameServicesAuthError(leaderboard.ErrCancelled)
	app.SetGameServicesPlayer(leaderboard.Player{ID: "p1"})
	var signErr error
	svc.SignIn(func(_ leaderboard.Player, err error) { signErr = err })
	app.Settle()
	if !errors.Is(signErr, leaderboard.ErrCancelled) || svc.SignedIn() {
		t.Fatalf("cancelled SignIn: err=%v signedIn=%v", signErr, svc.SignedIn())
	}

	app.SetGameServicesAuthError(nil)
	svc.SignIn(nil)
	app.Settle()
	if !svc.SignedIn() {
		t.Fatal("SignIn did not sign in")
	}

	// The platform signs the player out; the next submit reports it and the service follows.
	app.SetGameServicesPlayer(leaderboard.Player{})
	var submitErr error
	svc.Submit("high-score", 5, func(err error) { submitErr = err })
	app.Settle()
	if !errors.Is(submitErr, leaderboard.ErrUnauthenticated) || svc.SignedIn() {
		t.Fatalf("after platform sign-out: err=%v signedIn=%v", submitErr, svc.SignedIn())
	}

	sentinel := leaderboard.ErrUnavailable
	app.SetGameServicesPlayer(leaderboard.Player{ID: "p1", Authenticated: true})
	svc.SignIn(nil)
	app.Settle()
	app.SetGameServicesError(sentinel)
	svc.Submit("high-score", 5, func(err error) { submitErr = err })
	app.Settle()
	if !errors.Is(submitErr, sentinel) || !svc.SignedIn() {
		t.Fatalf("unavailable: err=%v signedIn=%v (an outage is not a sign-out)", submitErr, svc.SignedIn())
	}
}

func TestSignedInIsReactive(t *testing.T) {
	var svc *leaderboard.Service
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		svc, _ = leaderboard.New(cx, testCatalog(t))
		return core.Show{
			When: svc.SignedIn,
			Then: func(*core.Scope) core.Node {
				return core.View{
					TestID:  "board-button",
					Role:    core.RoleButton,
					Style:   core.Style{Width: core.Pt(120), Height: core.Pt(44)},
					OnPress: func(core.PressEvent) { _ = svc.Show("high-score") },
				}
			},
		}
	}, nsgotest.Environment(iosEnv))
	app.SetGameServicesPlayer(leaderboard.Player{ID: "p1", Authenticated: true})
	if app.Exists("board-button") {
		t.Fatal("button shown before sign-in")
	}
	svc.SignIn(nil)
	app.Settle()
	app.Press("board-button")
	if shown := app.GameServicesShownLeaderboards(); !slices.Equal(shown, []string{highScoreGC}) {
		t.Fatalf("shown = %v", shown)
	}
}
