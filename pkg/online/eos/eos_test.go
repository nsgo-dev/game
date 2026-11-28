package eos_test

import (
	"errors"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/eos"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// fake records requests and, like the EOS SDK, completes them only from Tick.
type fake struct {
	code    string
	unlocks []string
	percent float64
	scores  map[string]int64
	query   struct {
		board   string
		friends bool
		limit   int
	}
	presence [2]string
	pending  []func()
	closed   int
}

func (f *fake) later(fn func()) { f.pending = append(f.pending, fn) }

func (f *fake) Login(code string, done func(eos.Player, error)) {
	f.code = code
	f.later(func() { done(eos.Player{ProductUserID: "p1", DisplayName: "Player"}, nil) })
}

func (f *fake) UnlockAchievement(id string, done func(error)) {
	f.unlocks = append(f.unlocks, id)
	f.later(func() { done(nil) })
}

func (f *fake) ProgressAchievement(_ string, percent float64, done func(error)) {
	f.percent = percent
	f.later(func() { done(nil) })
}

func (f *fake) SubmitScore(board string, score int64, done func(error)) {
	if f.scores == nil {
		f.scores = map[string]int64{}
	}
	f.scores[board] = score
	f.later(func() { done(nil) })
}

func (f *fake) QueryLeaderboard(board string, friends bool, limit int, done func([]eos.LeaderboardEntry, error)) {
	f.query.board, f.query.friends, f.query.limit = board, friends, limit
	f.later(func() { done([]eos.LeaderboardEntry{{Rank: 1, UserID: "p1", Score: 42}}, nil) })
}

func (f *fake) SetPresence(details, status string, done func(error)) {
	f.presence = [2]string{details, status}
	f.later(func() { done(nil) })
}

func (f *fake) Tick() {
	pending := f.pending
	f.pending = nil
	for _, fn := range pending {
		fn()
	}
}

func (f *fake) Close() error { f.closed++; return nil }

func start(t *testing.T, f *fake, cfg eos.Config) (*nsgotest.App, *eos.Platform) {
	t.Helper()
	var platform *eos.Platform
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var err error
		platform, err = eos.New(cx, cfg, f)
		if err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})
	return app, platform
}

var cfg = eos.Config{ProductID: "p", SandboxID: "s", DeploymentID: "d", ExchangeCode: "x"}

// step pumps the driver and then runs what the platform posted.
func step(app *nsgotest.App, p *eos.Platform) {
	p.Tick()
	app.Advance(0)
}

func TestPlatformLogsInAndReportsOnTheEngineGoroutine(t *testing.T) {
	f := &fake{}
	app, p := start(t, f, cfg)

	var loginErr error
	logged, inCall := false, true
	p.Login(func(player eos.Player, err error) {
		if inCall {
			t.Error("login done ran inside Login")
		}
		logged, loginErr = true, err
		if player.ProductUserID != "p1" {
			t.Errorf("player = %+v", player)
		}
	})
	inCall = false
	if f.code != "x" {
		t.Fatalf("exchange code = %q", f.code)
	}
	app.Advance(0)
	if logged || p.SignedIn() {
		t.Fatal("login completed before the driver ticked")
	}
	step(app, p)
	if !logged || loginErr != nil || !p.SignedIn() || p.Player().DisplayName != "Player" {
		t.Fatalf("logged=%v err=%v player=%+v", logged, loginErr, p.Player())
	}

	var errs []error
	collect := func(err error) { errs = append(errs, err) }
	p.UnlockAchievement("first", collect)
	p.ProgressAchievement("half", 50, collect)
	p.SubmitScore("time", 1234, collect)
	p.SetPresence("Level 2", "Playing", collect)
	var rows []eos.LeaderboardEntry
	p.QueryLeaderboard("time", true, 10, func(r []eos.LeaderboardEntry, err error) {
		rows = r
		collect(err)
	})
	if len(errs) != 0 {
		t.Fatalf("results ran inside the calls: %v", errs)
	}
	step(app, p)
	if len(errs) != 5 {
		t.Fatalf("results = %v, want 5", errs)
	}
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(f.unlocks) != 1 || f.unlocks[0] != "first" || f.percent != 50 || f.scores["time"] != 1234 {
		t.Fatalf("unlocks=%v percent=%v scores=%v", f.unlocks, f.percent, f.scores)
	}
	if f.query.board != "time" || !f.query.friends || f.query.limit != 10 || len(rows) != 1 || rows[0].Score != 42 {
		t.Fatalf("query=%+v rows=%v", f.query, rows)
	}
	if f.presence != [2]string{"Level 2", "Playing"} {
		t.Fatalf("presence = %v", f.presence)
	}
}

func TestPlatformRejectsRequestsBeforeLoginAndBadArguments(t *testing.T) {
	f := &fake{}
	app, p := start(t, f, cfg)

	var errs []error
	collect := func(err error) { errs = append(errs, err) }
	p.UnlockAchievement("first", collect)
	app.Advance(0)
	if len(errs) != 1 || !errors.Is(errs[0], eos.ErrUnauthenticated) || len(f.unlocks) != 0 {
		t.Fatalf("before login: errs=%v unlocks=%v", errs, f.unlocks)
	}

	p.Login(nil)
	step(app, p)
	errs = nil
	p.ProgressAchievement("half", 101, collect)
	p.ProgressAchievement("half", -1, collect)
	p.UnlockAchievement(" ", collect)
	p.QueryLeaderboard("time", false, 0, func(_ []eos.LeaderboardEntry, err error) { collect(err) })
	app.Advance(0)
	if len(errs) != 4 {
		t.Fatalf("errs = %v, want 4", errs)
	}
	for _, err := range errs {
		if !errors.Is(err, eos.ErrInvalidArgument) {
			t.Fatalf("err = %v, want ErrInvalidArgument", err)
		}
	}
	if len(f.pending) != 0 {
		t.Fatalf("invalid requests reached the driver: %d pending", len(f.pending))
	}
}

func TestPlatformCloseDropsLateResults(t *testing.T) {
	f := &fake{}
	app, p := start(t, f, cfg)
	p.Login(nil)
	step(app, p)

	called := false
	p.UnlockAchievement("first", func(error) { called = true })
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil || f.closed != 1 {
		t.Fatalf("second Close: err=%v closed=%d", err, f.closed)
	}
	f.Tick()
	app.Advance(0)
	if called || p.SignedIn() {
		t.Fatalf("called=%v signedIn=%v after Close", called, p.SignedIn())
	}

	var loginErr error
	p.Login(func(_ eos.Player, err error) { loginErr = err })
	app.Advance(0)
	if !errors.Is(loginErr, eos.ErrClosed) {
		t.Fatalf("login after Close = %v", loginErr)
	}
}

func TestPlatformLoginWithoutExchangeCode(t *testing.T) {
	f := &fake{}
	c := cfg
	c.ExchangeCode = ""
	app, p := start(t, f, c)
	var loginErr error
	p.Login(func(_ eos.Player, err error) { loginErr = err })
	app.Advance(0)
	if !errors.Is(loginErr, eos.ErrUnauthenticated) || len(f.pending) != 0 {
		t.Fatalf("err=%v pending=%d", loginErr, len(f.pending))
	}
}

func TestNewRejectsMissingDriverAndConfig(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		if _, err := eos.New(cx, cfg, nil); !errors.Is(err, eos.ErrUnavailable) {
			t.Errorf("nil driver: err=%v", err)
		}
		if _, err := eos.New(cx, eos.Config{ProductID: "p", SandboxID: " "}, &fake{}); !errors.Is(err, eos.ErrInvalidConfig) {
			t.Errorf("missing IDs: err=%v", err)
		}
		return core.View{}
	})
	app.Advance(0)
}

func TestExchangeCodeFromArgs(t *testing.T) {
	launcher := []string{"-AUTH_LOGIN=unused", "-AUTH_PASSWORD=abc123", "-AUTH_TYPE=exchangecode", "-epicapp=Game", "-EpicPortal"}
	if got := eos.ExchangeCodeFromArgs(launcher); got != "abc123" {
		t.Fatalf("launcher args = %q", got)
	}
	if got := eos.ExchangeCodeFromArgs([]string{"-AUTH_PASSWORD=abc123", "-AUTH_TYPE=developer"}); got != "" {
		t.Fatalf("developer auth = %q, want empty", got)
	}
	if got := eos.ExchangeCodeFromArgs(nil); got != "" {
		t.Fatalf("no args = %q", got)
	}
}
