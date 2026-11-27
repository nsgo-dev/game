package selfhost_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/leaderboard/selfhost"
)

// Wednesday of the week starting Monday 2026-11-23.
var t0 = time.Date(2026, 11, 25, 15, 30, 0, 0, time.UTC)

// clock is a settable test clock.
type clock struct{ t time.Time }

func (c *clock) Now() time.Time { return c.t }

func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func seedKey(b byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{b}, ed25519.SeedSize))
}

func mustSigner(seed byte) *selfhost.ProofSigner {
	s, err := selfhost.NewProofSigner(seedKey(seed))
	if err != nil {
		panic(err)
	}
	return s
}

var testConfig = `version: 1
proofTTL: 5m
boards:
  - name: high-score
    order: descending
    reset: allTime
  - name: weekly
    order: descending
    reset: weekly
    keep: 1
  - name: speedrun
    order: ascending
    reset: allTime
    min: 1
    max: 3600000
  - name: ranked
    order: descending
    reset: daily
    validation: signed
` + serverKeyYAML

func newService(t testing.TB, store selfhost.Store, c *clock) *selfhost.Service {
	t.Helper()
	cfg, err := selfhost.ParseConfig([]byte(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := selfhost.New(context.Background(), cfg, selfhost.Options{Store: store, Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func submit(t testing.TB, svc *selfhost.Service, board, account string, score int64) selfhost.Result {
	t.Helper()
	res, err := svc.Submit(context.Background(), selfhost.Submission{Board: board, Account: account, Score: score})
	if err != nil {
		t.Fatalf("submit %s %s %d: %v", board, account, score, err)
	}
	return res
}

// ranking renders a page as "account:score" in rank order.
func ranking(t testing.TB, p selfhost.Page) []string {
	t.Helper()
	out := []string{}
	for i, e := range p.Entries {
		if i > 0 && e.Rank <= p.Entries[i-1].Rank {
			t.Fatalf("ranks out of order: %+v", p.Entries)
		}
		out = append(out, fmt.Sprintf("%s:%d", e.Account, e.Score))
	}
	return out
}

func top(t testing.TB, svc *selfhost.Service, board, period string) []string {
	t.Helper()
	p, err := svc.Top(context.Background(), board, period, 0, selfhost.MaxPage)
	if err != nil {
		t.Fatalf("top %s %s: %v", board, period, err)
	}
	return ranking(t, p)
}

func TestBestScoreCountsAndTiesGoToTheFirst(t *testing.T) {
	c := &clock{t0}
	svc := newService(t, selfhost.NewMemoryStore(), c)
	if res := submit(t, svc, "high-score", "ada", 500); !res.Improved || res.Entry.Rank != 1 || res.Total != 1 || res.Period.ID != selfhost.AllTimePeriod {
		t.Fatalf("first = %+v", res)
	}
	c.advance(time.Second)
	submit(t, svc, "high-score", "bob", 700)
	c.advance(time.Second)
	submit(t, svc, "high-score", "cyd", 500) // ties ada, later
	c.advance(time.Second)
	if res := submit(t, svc, "high-score", "ada", 400); res.Improved || res.Entry.Score != 500 || res.Entry.Rank != 2 || !res.Entry.Achieved.Equal(t0) {
		t.Fatalf("worse = %+v", res)
	}
	if res := submit(t, svc, "high-score", "ada", 500); res.Improved {
		t.Fatalf("equal score improved: %+v", res)
	}
	if got, want := top(t, svc, "high-score", ""), []string{"bob:700", "ada:500", "cyd:500"}; !slices.Equal(got, want) {
		t.Fatalf("top = %v, want %v", got, want)
	}

	submit(t, svc, "speedrun", "ada", 92_000)
	submit(t, svc, "speedrun", "bob", 88_500)
	if res := submit(t, svc, "speedrun", "ada", 95_000); res.Improved {
		t.Fatal("a slower time improved an ascending board")
	}
	submit(t, svc, "speedrun", "ada", 87_000)
	if got, want := top(t, svc, "speedrun", "current"), []string{"ada:87000", "bob:88500"}; !slices.Equal(got, want) {
		t.Fatalf("speedrun = %v, want %v", got, want)
	}
	for _, score := range []int64{0, 3_600_001} {
		if _, err := svc.Submit(context.Background(), selfhost.Submission{Board: "speedrun", Account: "ada", Score: score}); !errors.Is(err, selfhost.ErrScoreOutOfRange) {
			t.Errorf("score %d: %v, want ErrScoreOutOfRange", score, err)
		}
	}
	if _, err := svc.Submit(context.Background(), selfhost.Submission{Board: "high-score", Account: "ada", Score: selfhost.MaxSafeScore + 1}); !errors.Is(err, selfhost.ErrScoreOutOfRange) {
		t.Errorf("unsafe score: %v", err)
	}
	if _, err := svc.Submit(context.Background(), selfhost.Submission{Board: "nope", Account: "ada", Score: 1}); !errors.Is(err, selfhost.ErrUnknownBoard) {
		t.Errorf("unknown board: %v", err)
	}
	for _, acct := range []string{"", "a\x00b", "tab\there", string(bytes.Repeat([]byte{'x'}, selfhost.MaxAccountLength+1)), "\xff"} {
		if _, err := svc.Submit(context.Background(), selfhost.Submission{Board: "high-score", Account: acct, Score: 1}); !errors.Is(err, selfhost.ErrInvalidAccount) {
			t.Errorf("account %q: %v", acct, err)
		}
	}
}

func TestWeeklyBoardResetsOnScheduleAndKeepsThePreviousPeriod(t *testing.T) {
	c := &clock{t0}
	store := selfhost.NewMemoryStore()
	svc := newService(t, store, c)
	submit(t, svc, "weekly", "ada", 300)
	submit(t, svc, "weekly", "bob", 900)
	submit(t, svc, "high-score", "ada", 300)

	// The last instant of the week still counts in it.
	c.t = time.Date(2026, 11, 29, 23, 59, 59, 999_999_999, time.UTC)
	if res := submit(t, svc, "weekly", "cyd", 500); res.Period.ID != "2026-11-23" || !res.Period.End.Equal(time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("Sunday night counted in %+v", res.Period)
	}
	lastWeek := []string{"bob:900", "cyd:500", "ada:300"}

	// Midnight UTC on Monday starts a new week.
	c.t = time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	p, err := svc.Top(context.Background(), "weekly", "", 0, 10)
	if err != nil || p.Period.ID != "2026-11-30" || p.Total != 0 || len(p.Entries) != 0 {
		t.Fatalf("new week = %+v, %v", p, err)
	}
	for _, q := range []string{"previous", "2026-11-23"} {
		if got := top(t, svc, "weekly", q); !slices.Equal(got, lastWeek) {
			t.Fatalf("%s = %v, want %v", q, got, lastWeek)
		}
	}
	if res := submit(t, svc, "weekly", "ada", 100); res.Entry.Rank != 1 || res.Period.ID != "2026-11-30" {
		t.Fatalf("new week's first score = %+v", res)
	}
	if got := top(t, svc, "weekly", "previous"); !slices.Equal(got, lastWeek) {
		t.Fatalf("a new score changed last week: %v", got)
	}
	// An all-time board does not reset.
	if got := top(t, svc, "high-score", ""); !slices.Equal(got, []string{"ada:300"}) {
		t.Fatalf("high-score = %v", got)
	}
	info := svc.Boards(context.Background())
	if len(info) != 4 || info[1].Current.ID != "2026-11-30" || len(info[1].Previous) != 1 || info[1].Previous[0].ID != "2026-11-23" || len(info[0].Previous) != 0 {
		t.Fatalf("Boards = %+v", info)
	}

	// A restart in the same week finds both periods as they were.
	again := newService(t, store, c)
	if got := top(t, again, "weekly", "previous"); !slices.Equal(got, lastWeek) {
		t.Fatalf("after restart previous = %v", got)
	}
	if got := top(t, again, "weekly", ""); !slices.Equal(got, []string{"ada:100"}) {
		t.Fatalf("after restart current = %v", got)
	}

	// Two resets later the board keeps one earlier week, so 2026-11-23 is dropped, from the store too.
	c.t = time.Date(2026, 12, 7, 0, 0, 0, 0, time.UTC)
	for _, q := range []string{"2026-11-23", "2026-11-16", "2026-12-14", "2026-12-01", "garbage"} {
		if _, err := svc.Top(context.Background(), "weekly", q, 0, 10); !errors.Is(err, selfhost.ErrUnknownPeriod) {
			t.Errorf("period %q: %v, want ErrUnknownPeriod", q, err)
		}
	}
	if got := top(t, svc, "weekly", "previous"); !slices.Equal(got, []string{"ada:100"}) {
		t.Fatalf("previous after two resets = %v", got)
	}
	if got, want := store.Periods(), []string{"high-score/all", "weekly/2026-11-30"}; !slices.Equal(got, want) {
		t.Fatalf("store periods = %v, want %v", got, want)
	}
	if _, err := svc.Top(context.Background(), "high-score", "previous", 0, 10); !errors.Is(err, selfhost.ErrUnknownPeriod) {
		t.Fatalf("all-time previous: %v", err)
	}

	// A service started after a reset it never saw drops what the boards no longer keep.
	store2 := selfhost.NewMemoryStore()
	c.t = t0
	old := newService(t, store2, c)
	submit(t, old, "weekly", "ada", 1)
	c.t = time.Date(2026, 12, 14, 0, 0, 0, 0, time.UTC)
	newService(t, store2, c)
	if got := store2.Periods(); len(got) != 0 {
		t.Fatalf("stale periods kept: %v", got)
	}
}

func TestWeekStartAndDailyPeriods(t *testing.T) {
	cfg, err := selfhost.ParseConfig([]byte("version: 1\nboards:\n  - name: sun\n    order: descending\n    reset: weekly\n    weekStart: sunday\n    keep: 2\n  - name: day\n    order: descending\n    reset: daily\n"))
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{t0}
	svc, err := selfhost.New(context.Background(), cfg, selfhost.Options{Store: selfhost.NewMemoryStore(), Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	info := svc.Boards(context.Background())
	if info[0].Current.ID != "2026-11-22" || info[0].Previous[0].ID != "2026-11-15" || info[0].Previous[1].ID != "2026-11-08" {
		t.Fatalf("sunday weeks = %+v", info[0])
	}
	if info[1].Current.ID != "2026-11-25" || info[1].Previous[0].ID != "2026-11-24" || !info[1].Current.End.Equal(time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("days = %+v", info[1])
	}
	// A period ID must be a period's start.
	if _, err := svc.Top(context.Background(), "sun", "2026-11-16", 0, 1); !errors.Is(err, selfhost.ErrUnknownPeriod) {
		t.Fatalf("misaligned ID: %v", err)
	}
	if _, err := svc.Top(context.Background(), "sun", "2026-11-08", 0, 1); err != nil {
		t.Fatalf("second kept week: %v", err)
	}
}

func TestSignedBoardRejectsScoresTheGameServerDidNotSign(t *testing.T) {
	c := &clock{t0}
	svc := newService(t, selfhost.NewMemoryStore(), c)
	server := mustSigner(9)
	server.Now = c.Now
	stranger := mustSigner(10)
	stranger.Now = c.Now
	ctx := context.Background()
	sign := func(s *selfhost.ProofSigner, account string, score int64) string {
		p, err := s.Sign("ranked", account, score)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := []struct {
		name  string
		proof string
		score int64
		want  error
	}{
		{"no proof", "", 100, selfhost.ErrProofRequired},
		{"unknown key", sign(stranger, "ada", 100), 100, selfhost.ErrInvalidProof},
		{"other score", sign(server, "ada", 100), 1000, selfhost.ErrInvalidProof},
		{"other account", sign(server, "bob", 100), 100, selfhost.ErrInvalidProof},
		{"garbage", "v1.abc.1.xyz", 100, selfhost.ErrInvalidProof},
	}
	for _, tc := range cases {
		if _, err := svc.Submit(ctx, selfhost.Submission{Board: "ranked", Account: "ada", Score: tc.score, Proof: tc.proof}); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	if got := top(t, svc, "ranked", ""); len(got) != 0 {
		t.Fatalf("rejected scores ranked: %v", got)
	}

	proof := sign(server, "ada", 100)
	c.advance(5*time.Minute - time.Second)
	if res, err := svc.Submit(ctx, selfhost.Submission{Board: "ranked", Account: "ada", Score: 100, Proof: proof}); err != nil || !res.Improved {
		t.Fatalf("signed score = %+v, %v", res, err)
	}
	c.advance(time.Second)
	if _, err := svc.Submit(ctx, selfhost.Submission{Board: "ranked", Account: "ada", Score: 100, Proof: proof}); !errors.Is(err, selfhost.ErrProofExpired) {
		t.Fatalf("after proofTTL: %v", err)
	}
	server.Now = func() time.Time { return c.t.Add(time.Minute) }
	if _, err := svc.Submit(ctx, selfhost.Submission{Board: "ranked", Account: "bob", Score: 5, Proof: sign(server, "bob", 5)}); !errors.Is(err, selfhost.ErrInvalidProof) {
		t.Fatalf("proof from the future: %v", err)
	}
	// Unsigned boards ignore proofs entirely.
	if _, err := svc.Submit(ctx, selfhost.Submission{Board: "high-score", Account: "bob", Score: 5, Proof: "junk"}); err != nil {
		t.Fatalf("unsigned board with a proof: %v", err)
	}

	cfg, _ := selfhost.ParseConfig([]byte(testConfig))
	lenient, err := selfhost.New(ctx, cfg, selfhost.Options{Store: selfhost.NewMemoryStore(), Now: c.Now, Leeway: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lenient.Submit(ctx, selfhost.Submission{Board: "ranked", Account: "bob", Score: 5, Proof: sign(server, "bob", 5)}); err != nil {
		t.Fatalf("within leeway: %v", err)
	}
}

func TestAroundAndFriends(t *testing.T) {
	c := &clock{t0}
	svc := newService(t, selfhost.NewMemoryStore(), c)
	ctx := context.Background()
	for i := 1; i <= 20; i++ {
		submit(t, svc, "high-score", fmt.Sprintf("p%02d", i), int64(i*10)) // p20 ranks first
	}
	around := func(account string, radius int) []string {
		t.Helper()
		p, err := svc.Around(ctx, "high-score", "", account, radius)
		if err != nil {
			t.Fatalf("around %s: %v", account, err)
		}
		if p.Total != 20 {
			t.Fatalf("total = %d", p.Total)
		}
		var out []string
		for _, e := range p.Entries {
			out = append(out, fmt.Sprintf("%d:%s", e.Rank, e.Account))
		}
		return out
	}
	if got, want := around("p10", 2), []string{"9:p12", "10:p11", "11:p10", "12:p09", "13:p08"}; !slices.Equal(got, want) {
		t.Fatalf("middle = %v, want %v", got, want)
	}
	if got, want := around("p20", 2), []string{"1:p20", "2:p19", "3:p18", "4:p17", "5:p16"}; !slices.Equal(got, want) {
		t.Fatalf("top = %v, want %v", got, want)
	}
	if got, want := around("p02", 1), []string{"18:p03", "19:p02", "20:p01"}; !slices.Equal(got, want) {
		t.Fatalf("bottom = %v, want %v", got, want)
	}
	if got := around("p05", 30-20); len(got) != 20 {
		t.Fatalf("radius past both ends = %v", got)
	}
	if e, err := svc.Rank(ctx, "high-score", "", "p15"); err != nil || e.Rank != 6 || e.Score != 150 {
		t.Fatalf("Rank = %+v, %v", e, err)
	}
	if _, err := svc.Around(ctx, "high-score", "", "nobody", 2); !errors.Is(err, selfhost.ErrNotRanked) {
		t.Fatalf("unranked: %v", err)
	}
	if _, err := svc.Around(ctx, "weekly", "", "p01", 2); !errors.Is(err, selfhost.ErrNotRanked) {
		t.Fatalf("empty board: %v", err)
	}
	for _, r := range []int{-1, selfhost.MaxRadius + 1} {
		if _, err := svc.Around(ctx, "high-score", "", "p01", r); !errors.Is(err, selfhost.ErrInvalidQuery) {
			t.Fatalf("radius %d: %v", r, err)
		}
	}

	p, err := svc.Friends(ctx, "high-score", "", []string{"p03", "nobody", "p17", "p03", "p11"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ranking(t, p), []string{"p17:170", "p11:110", "p03:30"}; !slices.Equal(got, want) || p.Entries[0].Rank != 4 || p.Total != 20 {
		t.Fatalf("friends = %v %+v, want %v", got, p.Entries, want)
	}
	if _, err := svc.Friends(ctx, "high-score", "", make([]string, selfhost.MaxFriends+1)); !errors.Is(err, selfhost.ErrInvalidQuery) {
		t.Fatalf("too many friends: %v", err)
	}

	page, err := svc.Top(ctx, "high-score", "", 18, 5)
	if err != nil || page.Total != 20 || len(page.Entries) != 2 || page.Entries[0].Rank != 19 {
		t.Fatalf("last page = %+v, %v", page, err)
	}
	if page, err := svc.Top(ctx, "high-score", "", 40, 5); err != nil || len(page.Entries) != 0 || page.Total != 20 {
		t.Fatalf("past the end = %+v, %v", page, err)
	}
	for _, q := range [][2]int{{-1, 5}, {0, 0}, {0, selfhost.MaxPage + 1}} {
		if _, err := svc.Top(ctx, "high-score", "", q[0], q[1]); !errors.Is(err, selfhost.ErrInvalidQuery) {
			t.Errorf("top %v: %v", q, err)
		}
	}
}

// failingStore fails every Save.
type failingStore struct{ selfhost.MemoryStore }

func (*failingStore) Save(context.Context, selfhost.Record) error { return errors.New("disk full") }

func TestAFailedSaveLeavesTheBoardUnchanged(t *testing.T) {
	c := &clock{t0}
	svc := newService(t, &failingStore{}, c)
	if _, err := svc.Submit(context.Background(), selfhost.Submission{Board: "high-score", Account: "ada", Score: 1}); err == nil {
		t.Fatal("Submit succeeded without saving")
	}
	if got := top(t, svc, "high-score", ""); len(got) != 0 {
		t.Fatalf("unsaved score ranked: %v", got)
	}
}

func TestNewRefusesInvalidStoreRecords(t *testing.T) {
	store := selfhost.NewMemoryStore()
	ctx := context.Background()
	_ = store.Save(ctx, selfhost.Record{Board: "high-score", Period: selfhost.AllTimePeriod, Account: "", Score: 1, Achieved: t0})
	cfg, _ := selfhost.ParseConfig([]byte(testConfig))
	if _, err := selfhost.New(ctx, cfg, selfhost.Options{Store: store, Now: (&clock{t0}).Now}); err == nil {
		t.Fatal("New loaded a record without an account")
	}
	// Records of a board the configuration dropped are kept in the store but not served.
	other := selfhost.NewMemoryStore()
	_ = other.Save(ctx, selfhost.Record{Board: "retired", Period: selfhost.AllTimePeriod, Account: "ada", Score: 1, Achieved: t0})
	if _, err := selfhost.New(ctx, cfg, selfhost.Options{Store: other, Now: (&clock{t0}).Now}); err != nil {
		t.Fatal(err)
	}
	if got := other.Periods(); !slices.Equal(got, []string{"retired/all"}) {
		t.Fatalf("store = %v", got)
	}
	if _, err := selfhost.New(ctx, cfg, selfhost.Options{}); err == nil {
		t.Fatal("New without a store")
	}
}

func TestConcurrentSubmitsAndQueries(t *testing.T) {
	c := &clock{t0}
	svc := newService(t, selfhost.NewMemoryStore(), c)
	ctx := context.Background()
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				acct := fmt.Sprintf("w%d-%d", w, i%50)
				if _, err := svc.Submit(ctx, selfhost.Submission{Board: "high-score", Account: acct, Score: int64(i)}); err != nil {
					t.Error(err)
					return
				}
				if _, err := svc.Top(ctx, "high-score", "", 0, 10); err != nil {
					t.Error(err)
					return
				}
				if _, err := svc.Around(ctx, "high-score", "", acct, 3); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	p, err := svc.Top(ctx, "high-score", "", 0, selfhost.MaxPage)
	if err != nil || p.Total != 400 {
		t.Fatalf("total = %d, %v", p.Total, err)
	}
	for _, e := range p.Entries {
		if e.Score < 150 {
			t.Fatalf("%s kept %d, not its best", e.Account, e.Score)
		}
	}
}

func TestNewLeavesPeriodsItCannotPlaceInTheStore(t *testing.T) {
	ctx := context.Background()
	store := selfhost.NewMemoryStore()
	c := &clock{t0}
	svc := newService(t, store, c)
	submit(t, svc, "weekly", "ada", 300)
	submit(t, svc, "high-score", "bob", 50)
	all := []string{"high-score/all", "weekly/2026-11-23"}

	// A clock two weeks behind sees the week as the future: it is kept and not served until the clock reaches it.
	c.t = t0.Add(-14 * 24 * time.Hour)
	behind := newService(t, store, c)
	if got := store.Periods(); !slices.Equal(got, all) {
		t.Fatalf("a clock behind dropped periods: %v", got)
	}
	if got := top(t, behind, "weekly", ""); len(got) != 0 {
		t.Fatalf("current week behind the clock = %v", got)
	}
	if _, err := behind.Top(ctx, "weekly", "2026-11-23", 0, 10); !errors.Is(err, selfhost.ErrUnknownPeriod) {
		t.Fatalf("a future period served: %v", err)
	}
	c.t = t0
	if got := top(t, behind, "weekly", ""); !slices.Equal(got, []string{"ada:300"}) {
		t.Fatalf("once the clock caught up = %v", got)
	}
	if got := store.Periods(); !slices.Equal(got, all) {
		t.Fatalf("catching up dropped periods: %v", got)
	}

	// Changing a board's reset or weekStart leaves the periods stored under the old one, even long after they would have expired.
	for name, boards := range map[string]string{
		"weekStart":         "  - name: high-score\n    order: descending\n    reset: allTime\n  - name: weekly\n    order: descending\n    reset: weekly\n    weekStart: sunday\n    keep: 1\n",
		"allTime to weekly": "  - name: high-score\n    order: descending\n    reset: weekly\n    keep: 1\n  - name: weekly\n    order: descending\n    reset: weekly\n    weekStart: sunday\n    keep: 1\n",
		"weekly to allTime": "  - name: high-score\n    order: descending\n    reset: allTime\n  - name: weekly\n    order: descending\n    reset: allTime\n",
	} {
		cfg, err := selfhost.ParseConfig([]byte("version: 1\nboards:\n" + boards))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		store := selfhost.NewMemoryStore()
		_ = store.Save(ctx, selfhost.Record{Board: "high-score", Period: selfhost.AllTimePeriod, Account: "bob", Score: 50, Achieved: t0})
		_ = store.Save(ctx, selfhost.Record{Board: "weekly", Period: "2026-11-23", Account: "ada", Score: 300, Achieved: t0})
		late := &clock{t0.Add(10 * 7 * 24 * time.Hour)}
		changed, err := selfhost.New(ctx, cfg, selfhost.Options{Store: store, Now: late.Now})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := store.Periods(); !slices.Equal(got, all) {
			t.Fatalf("%s dropped periods: %v", name, got)
		}
		if got := top(t, changed, "weekly", ""); len(got) != 0 {
			t.Fatalf("%s served a period it cannot place: %v", name, got)
		}
	}
}

func TestASubmissionAtAResetCountsInThePeriodOfItsTime(t *testing.T) {
	ctx := context.Background()
	sunday := time.Date(2026, 11, 29, 23, 59, 59, 999_999_000, time.UTC)
	monday := time.Date(2026, 11, 30, 0, 0, 0, 500, time.UTC)
	// New reads the clock once; the clock then moves past the reset between any two readings.
	readings := []time.Time{t0, sunday, monday}
	now := func() time.Time {
		r := readings[0]
		if len(readings) > 1 {
			readings = readings[1:]
		}
		return r
	}
	cfg, _ := selfhost.ParseConfig([]byte(testConfig))
	svc, err := selfhost.New(ctx, cfg, selfhost.Options{Store: selfhost.NewMemoryStore(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.Submit(ctx, selfhost.Submission{Board: "weekly", Account: "ada", Score: 1})
	if err != nil {
		t.Fatal(err)
	}
	if a := res.Entry.Achieved; res.Period.ID != "2026-11-23" || !a.Equal(sunday) {
		t.Fatalf("filed in %s at %v, want 2026-11-23 at %v", res.Period.ID, a, sunday)
	}

	// A clock reading from before a reset the board already made counts at the reset's instant.
	c := &clock{time.Date(2026, 11, 30, 0, 0, 1, 0, time.UTC)}
	svc3, err := selfhost.New(ctx, cfg, selfhost.Options{Store: selfhost.NewMemoryStore(), Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	c.t = sunday
	res, err = svc3.Submit(ctx, selfhost.Submission{Board: "weekly", Account: "bob", Score: 1})
	if err != nil {
		t.Fatal(err)
	}
	if start := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC); res.Period.ID != "2026-11-30" || !res.Entry.Achieved.Equal(start) {
		t.Fatalf("a reading behind the board filed in %s at %v", res.Period.ID, res.Entry.Achieved)
	}
}

func TestAProofSignedBeforeAResetDoesNotCountAfterIt(t *testing.T) {
	ctx := context.Background()
	c := &clock{time.Date(2026, 11, 25, 23, 59, 59, 0, time.UTC)}
	svc := newService(t, selfhost.NewMemoryStore(), c)
	server := mustSigner(9)
	server.Now = c.Now
	proof, err := server.Sign("ranked", "ada", 100)
	if err != nil {
		t.Fatal(err)
	}
	// Two seconds later the daily board has reset, well inside proofTTL.
	c.advance(2 * time.Second)
	if _, err := svc.Submit(ctx, selfhost.Submission{Board: "ranked", Account: "ada", Score: 100, Proof: proof}); !errors.Is(err, selfhost.ErrProofExpired) {
		t.Fatalf("a proof from the previous day: %v", err)
	}
	if got := top(t, svc, "ranked", "previous"); len(got) != 0 {
		t.Fatalf("previous day changed: %v", got)
	}
	fresh, err := server.Sign("ranked", "ada", 100)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := svc.Submit(ctx, selfhost.Submission{Board: "ranked", Account: "ada", Score: 100, Proof: fresh}); err != nil || res.Period.ID != "2026-11-26" {
		t.Fatalf("a proof from the new day = %+v, %v", res, err)
	}
}

func TestTopPastTheEndIsEmpty(t *testing.T) {
	svc := newService(t, selfhost.NewMemoryStore(), &clock{t0})
	submit(t, svc, "high-score", "ada", 1)
	submit(t, svc, "high-score", "bob", 2)
	for _, offset := range []int{2, 3, math.MaxInt} {
		p, err := svc.Top(context.Background(), "high-score", "", offset, 10)
		if err != nil || p.Total != 2 || len(p.Entries) != 0 {
			t.Fatalf("offset %d = %+v, %v", offset, p, err)
		}
	}
}
