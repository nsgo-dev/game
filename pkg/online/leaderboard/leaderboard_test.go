package leaderboard_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/leaderboard"
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
func start(t *testing.T, env core.Environment, opts ...leaderboard.Option) (*nsgotest.App, *leaderboard.Service) {
	t.Helper()
	var svc *leaderboard.Service
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s, err := leaderboard.New(cx, testCatalog(t), opts...)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		svc = s
		return core.View{}
	}, nsgotest.Environment(env))
	return app, svc
}

var (
	iosEnv            = core.Environment{CanUseGameServices: true, OS: core.OSIOS}
	androidEnv        = core.Environment{CanUseGameServices: true, OS: core.OSAndroid}
	linuxEnv          = core.Environment{OS: core.OSLinux}
	simIOSEnv         = core.Environment{OS: core.OSIOS, Simulated: true}
	miniProgramIOSEnv = core.Environment{OS: core.OSIOS, Simulated: true, MiniProgramSignIn: true}
)

type fakeMiniGameHost struct {
	mu        sync.Mutex
	writes    []leaderboard.MiniGameEntry
	writeErr  error
	shown     []leaderboard.MiniGameRanking
	showErr   error
	hideCalls int
	hideErr   error
	async     bool
}

func (h *fakeMiniGameHost) SetUserCloudStorage(entries []leaderboard.MiniGameEntry, done func(error)) {
	run := func() {
		h.mu.Lock()
		err := h.writeErr
		if err == nil {
			h.writes = append(h.writes, entries...)
		}
		h.mu.Unlock()
		if done != nil {
			done(err)
		}
	}
	if h.async {
		go run()
		return
	}
	run()
}

func (h *fakeMiniGameHost) ShowFriendRanking(req leaderboard.MiniGameRanking) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.showErr != nil {
		return h.showErr
	}
	h.shown = append(h.shown, req)
	return nil
}

func (h *fakeMiniGameHost) HideFriendRanking() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hideErr != nil {
		return h.hideErr
	}
	h.hideCalls++
	return nil
}

func (h *fakeMiniGameHost) snapshotWrites() []leaderboard.MiniGameEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.writes)
}

func (h *fakeMiniGameHost) snapshotShown() []leaderboard.MiniGameRanking {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.shown)
}

func (h *fakeMiniGameHost) hides() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hideCalls
}

func TestProviderFor(t *testing.T) {
	if got := leaderboard.ProviderFor(nil); got != leaderboard.ProviderNone {
		t.Fatalf("ProviderFor(nil) = %v, want none", got)
	}
	cases := []struct {
		name string
		env  core.Environment
		want leaderboard.Provider
	}{
		{"ios", core.Environment{OS: core.OSIOS}, leaderboard.ProviderGameCenter},
		{"macos", core.Environment{OS: core.OSMacOS}, leaderboard.ProviderGameCenter},
		{"android", core.Environment{OS: core.OSAndroid}, leaderboard.ProviderPlayGames},
		{"windows", core.Environment{OS: core.OSWindows}, leaderboard.ProviderNone},
		{"linux", core.Environment{OS: core.OSLinux}, leaderboard.ProviderNone},
		{"web", core.Environment{OS: core.OSWeb}, leaderboard.ProviderNone},
		{"simulated ios web (--nsgo-os ios)", core.Environment{OS: core.OSIOS, Simulated: true}, leaderboard.ProviderNone},
		{"simulated macos web", core.Environment{OS: core.OSMacOS, Simulated: true}, leaderboard.ProviderNone},
		{"simulated android web", core.Environment{OS: core.OSAndroid, Simulated: true}, leaderboard.ProviderNone},
		{"simulated ios even with game services bit", core.Environment{OS: core.OSIOS, Simulated: true, CanUseGameServices: true}, leaderboard.ProviderNone},
		{"mini program on ios", core.Environment{OS: core.OSIOS, Simulated: true, MiniProgramSignIn: true}, leaderboard.ProviderMiniGame},
		{"mini program on android", core.Environment{OS: core.OSAndroid, Simulated: true, MiniProgramSignIn: true}, leaderboard.ProviderMiniGame},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got leaderboard.Provider
			nsgotest.Start(t, func(cx *core.Scope) core.Node {
				got = leaderboard.ProviderFor(cx.Env())
				return core.View{}
			}, nsgotest.Environment(tc.env))
			if got != tc.want {
				t.Errorf("ProviderFor(%+v) = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
	if leaderboard.ProviderGameCenter.String() != "gameCenter" ||
		leaderboard.ProviderPlayGames.String() != "playGames" ||
		leaderboard.ProviderMiniGame.String() != "miniGame" ||
		leaderboard.ProviderNone.String() != "none" {
		t.Fatal("unexpected provider names")
	}
}

func TestSimulatedIOSReportsNoGameCenter(t *testing.T) {
	// A web build with --nsgo-os ios or a mini program without a mini-game host must never report Game Center.
	app, svc := start(t, simIOSEnv)
	if svc.Provider() != leaderboard.ProviderNone {
		t.Fatalf("Provider = %v, want none (must not report Game Center)", svc.Provider())
	}
	if svc.Available() || svc.Supports("high-score") {
		t.Fatalf("Simulated iOS reported Available=%v Supports=%v", svc.Available(), svc.Supports("high-score"))
	}
	var submitErr error
	svc.Submit("high-score", 100, func(err error) { submitErr = err })
	app.Settle()
	if !errors.Is(submitErr, leaderboard.ErrUnsupported) {
		t.Fatalf("Submit = %v, want ErrUnsupported", submitErr)
	}
	if err := svc.Show("high-score"); !errors.Is(err, leaderboard.ErrUnsupported) {
		t.Fatalf("Show = %v, want ErrUnsupported", err)
	}
	if err := svc.Hide(); !errors.Is(err, leaderboard.ErrUnsupported) {
		t.Fatalf("Hide = %v, want ErrUnsupported", err)
	}

	// Inside a mini program on iOS, Provider is ProviderMiniGame (never Game Center), and without a mini-game host Available is false.
	_, mpSvc := start(t, miniProgramIOSEnv)
	if mpSvc.Provider() != leaderboard.ProviderMiniGame {
		t.Fatalf("mini program on iOS Provider = %v, want miniGame", mpSvc.Provider())
	}
	if mpSvc.Available() {
		t.Fatal("mini program without a mini-game host reported Available")
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
	c.Leaderboards[0].MiniGame.Key = "changed"
	if got := svc.Catalog().Leaderboards[0].GameCenter; got != highScoreGC {
		t.Fatalf("service catalog followed the caller's edit: %q", got)
	}
	if got := svc.Catalog().Leaderboards[0].MiniGame.Key; got != "best" {
		t.Fatalf("service catalog followed the caller's MiniGame edit: %q", got)
	}
	copyCat := svc.Catalog()
	copyCat.Leaderboards[0].Name = "changed"
	copyCat.Leaderboards[0].MiniGame.Key = "changed"
	if b, ok := svc.Catalog().Board("high-score"); !ok || b.MiniGame.Key != "best" {
		t.Fatalf("Catalog returned the service's own slice or MiniGame pointer: %+v, %v", b, ok)
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
	if err := svc.Hide(); err != nil {
		t.Fatalf("Hide on Game Center = %v, want nil", err)
	}
	if shown := app.GameServicesShownLeaderboards(); !slices.Equal(shown, []string{highScoreGC}) {
		t.Fatalf("shown = %v", shown)
	}
}

func TestMiniGameSubmitKeepsBestScoreAndPersists(t *testing.T) {
	storage := map[string]string{}
	host := &fakeMiniGameHost{}
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)

	var svc *leaderboard.Service
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var err error
		svc, err = leaderboard.New(cx, testCatalog(t), leaderboard.WithMiniGameHost(host))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return core.View{}
	},
		nsgotest.Environment(miniProgramIOSEnv),
		nsgotest.Storage(storage),
		nsgotest.WallTime(now),
	)

	if svc.Provider() != leaderboard.ProviderMiniGame || !svc.Available() {
		t.Fatalf("Provider=%v Available=%v", svc.Provider(), svc.Available())
	}
	if !svc.Supports("high-score") || !svc.Supports("fastest-run") || svc.Supports("daily") {
		t.Fatalf("Supports: high-score=%v fastest-run=%v daily=%v", svc.Supports("high-score"), svc.Supports("fastest-run"), svc.Supports("daily"))
	}
	if id, err := svc.ID("high-score"); err != nil || id != "best" {
		t.Fatalf("ID(high-score) = %q, %v", id, err)
	}

	// Descending board ("high-score", key "best", order desc):
	submit := func(board string, score int64) error {
		called := false
		var gotErr error
		svc.Submit(board, score, func(err error) {
			called = true
			gotErr = err
		})
		if called {
			t.Fatalf("Submit(%q, %d) called done before returning", board, score)
		}
		app.Settle()
		if !called {
			t.Fatalf("Submit(%q, %d) did not invoke done", board, score)
		}
		return gotErr
	}

	if err := submit("high-score", 500); err != nil {
		t.Fatalf("Submit 500: %v", err)
	}
	// Worse and equal scores must not overwrite cloud storage.
	if err := submit("high-score", 300); err != nil {
		t.Fatalf("Submit 300: %v", err)
	}
	if err := submit("high-score", 500); err != nil {
		t.Fatalf("Submit 500 again: %v", err)
	}
	// Better score writes.
	if err := submit("high-score", 900); err != nil {
		t.Fatalf("Submit 900: %v", err)
	}

	// Host error must not update the cached best score.
	host.writeErr = leaderboard.ErrUnavailable
	if err := submit("high-score", 1200); !errors.Is(err, leaderboard.ErrUnavailable) {
		t.Fatalf("Submit with host error = %v, want ErrUnavailable", err)
	}
	host.writeErr = nil
	// Because 1200 failed, 1000 still beats 900 and must write.
	if err := submit("high-score", 1000); err != nil {
		t.Fatalf("Submit 1000 after failed 1200: %v", err)
	}

	// Ascending board ("fastest-run", key "fastest", order asc):
	if err := submit("fastest-run", 120); err != nil {
		t.Fatalf("Submit asc 120: %v", err)
	}
	if err := submit("fastest-run", 150); err != nil {
		t.Fatalf("Submit asc 150: %v", err)
	}
	if err := submit("fastest-run", 120); err != nil {
		t.Fatalf("Submit asc 120 again: %v", err)
	}
	if err := submit("fastest-run", 85); err != nil {
		t.Fatalf("Submit asc 85: %v", err)
	}

	// Board without miniGame entry fails before reaching host.
	if err := submit("daily", 50); !errors.Is(err, leaderboard.ErrNoPlatformID) {
		t.Fatalf("Submit(daily) = %v, want ErrNoPlatformID", err)
	}

	writes := host.snapshotWrites()
	wantWrites := []struct {
		key   string
		score int64
	}{
		{"best", 500},
		{"best", 900},
		{"best", 1000},
		{"fastest", 120},
		{"fastest", 85},
	}
	if len(writes) != len(wantWrites) {
		t.Fatalf("writes (%d) = %+v, want %d", len(writes), writes, len(wantWrites))
	}
	for i, w := range wantWrites {
		if writes[i].Key != w.key {
			t.Fatalf("write[%d].Key = %q, want %q", i, writes[i].Key, w.key)
		}
		gotScore, gotTS, err := leaderboard.DecodeMiniGameScore(writes[i].Value)
		if err != nil {
			t.Fatalf("DecodeMiniGameScore(%q): %v", writes[i].Value, err)
		}
		if gotScore != w.score || gotTS != now.Unix() {
			t.Fatalf("write[%d] = (score=%d, ts=%d), want (score=%d, ts=%d)", i, gotScore, gotTS, w.score, now.Unix())
		}
	}

	// Across an app restart with the persisted core.Storage snapshot, worse scores still do not overwrite cloud storage.
	disk := map[string]string{}
	for _, k := range app.Storage().Keys() {
		if v, ok := app.Storage().GetItem(k); ok {
			disk[k] = v
		}
	}
	host2 := &fakeMiniGameHost{}
	var svc2 *leaderboard.Service
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var err error
		svc2, err = leaderboard.New(cx, testCatalog(t), leaderboard.WithMiniGameHost(host2))
		if err != nil {
			t.Fatalf("New(restart): %v", err)
		}
		return core.View{}
	},
		nsgotest.Environment(miniProgramIOSEnv),
		nsgotest.Storage(disk),
		nsgotest.WallTime(now.Add(time.Hour)),
	)

	var errAfterRestart error
	svc2.Submit("high-score", 950, func(err error) { errAfterRestart = err })
	svc2.Submit("fastest-run", 90, func(err error) {
		if err != nil {
			errAfterRestart = err
		}
	})
	app2.Settle()
	if errAfterRestart != nil || len(host2.snapshotWrites()) != 0 {
		t.Fatalf("after restart worse scores wrote to host: err=%v writes=%+v", errAfterRestart, host2.snapshotWrites())
	}
	svc2.Submit("high-score", 1500, func(err error) { errAfterRestart = err })
	app2.Settle()
	if errAfterRestart != nil || len(host2.snapshotWrites()) != 1 {
		t.Fatalf("after restart better score: err=%v writes=%+v", errAfterRestart, host2.snapshotWrites())
	}
}

func TestMiniGameShowHideAndScoresUnsupported(t *testing.T) {
	host := &fakeMiniGameHost{}
	var svc *leaderboard.Service
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var err error
		svc, err = leaderboard.New(cx, testCatalog(t), leaderboard.WithMiniGameHost(host))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return core.View{}
	},
		nsgotest.Environment(miniProgramIOSEnv),
		nsgotest.Size(390, 844),
	)

	// Show uses the full window rectangle.
	if err := svc.Show("high-score"); err != nil {
		t.Fatalf("Show(high-score): %v", err)
	}
	// ShowIn uses the caller's rectangle.
	rect := core.Bounds{
		Origin: core.Point{X: 20, Y: 100},
		Size:   core.Size{Width: 350, Height: 480},
	}
	if err := svc.ShowIn("fastest-run", rect); err != nil {
		t.Fatalf("ShowIn(fastest-run): %v", err)
	}
	// Negative dimensions are rejected before reaching the host.
	if err := svc.ShowIn("high-score", core.Bounds{Size: core.Size{Width: -10, Height: 100}}); err == nil {
		t.Fatal("ShowIn accepted negative width")
	}
	// Board without miniGame entry returns ErrNoPlatformID.
	if err := svc.Show("daily"); !errors.Is(err, leaderboard.ErrNoPlatformID) {
		t.Fatalf("Show(daily) = %v, want ErrNoPlatformID", err)
	}
	// Unknown board returns ErrUnknownLeaderboard.
	if err := svc.Show("missing"); !errors.Is(err, leaderboard.ErrUnknownLeaderboard) {
		t.Fatalf("Show(missing) = %v, want ErrUnknownLeaderboard", err)
	}

	shown := host.snapshotShown()
	wantShown := []leaderboard.MiniGameRanking{
		{Key: "best", Order: leaderboard.OrderDesc, Bounds: core.Bounds{Size: core.Size{Width: 390, Height: 844}}},
		{Key: "fastest", Order: leaderboard.OrderAsc, Bounds: rect},
	}
	if !slices.Equal(shown, wantShown) {
		t.Fatalf("shown = %+v, want %+v", shown, wantShown)
	}

	if err := svc.Hide(); err != nil {
		t.Fatalf("Hide: %v", err)
	}
	if host.hides() != 1 {
		t.Fatalf("hideCalls = %d, want 1", host.hides())
	}

	// Scores (Friends and Global) stay ErrUnsupported on ProviderMiniGame because friend data never leaves the open data context.
	for _, scope := range []leaderboard.PlayerScope{leaderboard.Friends, leaderboard.Global} {
		var entries []leaderboard.Entry
		var scoresErr error
		svc.Scores("high-score", leaderboard.Query{Players: scope}, func(e []leaderboard.Entry, err error) {
			entries, scoresErr = e, err
		})
		app.Settle()
		if !errors.Is(scoresErr, leaderboard.ErrUnsupported) || len(entries) != 0 {
			t.Fatalf("Scores(scope=%v) = (%v, %v), want ErrUnsupported", scope, entries, scoresErr)
		}
	}
	// Unknown board still reports ErrUnknownLeaderboard on Scores.
	var typoErr error
	svc.Scores("typo", leaderboard.Query{Players: leaderboard.Friends}, func(_ []leaderboard.Entry, err error) { typoErr = err })
	app.Settle()
	if !errors.Is(typoErr, leaderboard.ErrUnknownLeaderboard) {
		t.Fatalf("Scores(typo) = %v, want ErrUnknownLeaderboard", typoErr)
	}
}

func TestMiniGameSignInAndAsyncHostCallback(t *testing.T) {
	host := &fakeMiniGameHost{async: true}
	app, svc := start(t, miniProgramIOSEnv, leaderboard.WithMiniGameHost(host))

	app.MiniProgramSignIn = func(core.MiniProgramSignInOptions) (core.MiniProgramCredential, error) {
		return core.MiniProgramCredential{Platform: core.MiniProgramWeChat, Code: "wx-code-42"}, nil
	}
	var signedIn leaderboard.Player
	var signErr error
	svc.SignIn(func(p leaderboard.Player, err error) { signedIn, signErr = p, err })
	app.Settle()
	if signErr != nil || !signedIn.Authenticated || signedIn.ID != "" || signedIn.DisplayName != "" || !svc.SignedIn() {
		t.Fatalf("SignIn: player=%+v err=%v signedIn=%v", signedIn, signErr, svc.SignedIn())
	}

	// Host callbacks invoked from another goroutine are marshaled cleanly onto the engine goroutine.
	doneCh := make(chan error, 1)
	svc.Submit("high-score", 777, func(err error) { doneCh <- err })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.Settle()
		select {
		case err := <-doneCh:
			if err != nil {
				t.Fatalf("async Submit: %v", err)
			}
			goto done
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	t.Fatal("timed out waiting for async mini-game Submit callback")
done:
	if len(host.snapshotWrites()) != 1 {
		t.Fatalf("writes = %+v, want 1", host.snapshotWrites())
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

	// "fastest-run" is only on Game Center and miniGame.
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
	if err := svc.Hide(); !errors.Is(err, leaderboard.ErrUnsupported) {
		t.Fatalf("Hide = %v", err)
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
