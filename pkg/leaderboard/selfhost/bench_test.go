package selfhost_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/leaderboard/selfhost"
)

// queryBudget is the most a top-N or around-me query may take on a board of a million entries (#558).
const queryBudget = 50 * time.Millisecond

// millionEntries is the size of the benchmark board.
const millionEntries = 1_000_000

// nopStore keeps nothing, so the benchmarks measure the service alone.
type nopStore struct{}

func (nopStore) Load(context.Context, func(selfhost.Record) error) error { return nil }
func (nopStore) Save(context.Context, selfhost.Record) error             { return nil }
func (nopStore) Drop(context.Context, string, string) error              { return nil }

var million struct {
	once     sync.Once
	svc      *selfhost.Service
	accounts []string
}

// millionBoard returns a service whose high-score board holds a million accounts with random scores, built once for every benchmark.
func millionBoard(b *testing.B) (*selfhost.Service, []string) {
	b.Helper()
	million.once.Do(func() {
		c := &clock{t0}
		svc := newService(b, nopStore{}, c)
		rng := rand.New(rand.NewPCG(558, 1))
		accounts := make([]string, millionEntries)
		ctx := context.Background()
		for i := range accounts {
			accounts[i] = fmt.Sprintf("acct-%07d", i)
			if _, err := svc.Submit(ctx, selfhost.Submission{Board: "high-score", Account: accounts[i], Score: rng.Int64N(10_000_000)}); err != nil {
				b.Fatal(err)
			}
		}
		million.svc, million.accounts = svc, accounts
	})
	if million.svc == nil {
		b.Fatal("building the board failed")
	}
	return million.svc, million.accounts
}

// checkBudget fails the benchmark when an operation took longer than queryBudget on average.
func checkBudget(b *testing.B) {
	b.Helper()
	per := b.Elapsed() / time.Duration(b.N)
	b.ReportMetric(float64(per)/float64(time.Millisecond), "ms/op")
	if per > queryBudget {
		b.Errorf("%v per query, over the %v budget", per, queryBudget)
	}
}

func BenchmarkTop100OfAMillion(b *testing.B) {
	svc, _ := millionBoard(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p, err := svc.Top(ctx, "high-score", "", 0, 100)
		if err != nil || len(p.Entries) != 100 || p.Total != millionEntries {
			b.Fatalf("Top = %d entries of %d, %v", len(p.Entries), p.Total, err)
		}
	}
	checkBudget(b)
}

func BenchmarkTop100DeepInAMillion(b *testing.B) {
	svc, _ := millionBoard(b)
	ctx := context.Background()
	rng := rand.New(rand.NewPCG(1, 1))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p, err := svc.Top(ctx, "high-score", "", rng.IntN(millionEntries-100), 100)
		if err != nil || len(p.Entries) != 100 {
			b.Fatalf("Top = %d entries, %v", len(p.Entries), err)
		}
	}
	checkBudget(b)
}

func BenchmarkAroundMeInAMillion(b *testing.B) {
	svc, accounts := millionBoard(b)
	ctx := context.Background()
	rng := rand.New(rand.NewPCG(2, 2))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p, err := svc.Around(ctx, "high-score", "", accounts[rng.IntN(len(accounts))], 10)
		if err != nil || len(p.Entries) != 21 {
			b.Fatalf("Around = %d entries, %v", len(p.Entries), err)
		}
	}
	checkBudget(b)
}

func BenchmarkFriends100InAMillion(b *testing.B) {
	svc, accounts := millionBoard(b)
	ctx := context.Background()
	rng := rand.New(rand.NewPCG(3, 3))
	friends := make([]string, 100)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for i := range friends {
			friends[i] = accounts[rng.IntN(len(accounts))]
		}
		if _, err := svc.Friends(ctx, "high-score", "", friends); err != nil {
			b.Fatal(err)
		}
	}
	checkBudget(b)
}

func BenchmarkSubmitIntoAMillion(b *testing.B) {
	svc, accounts := millionBoard(b)
	ctx := context.Background()
	rng := rand.New(rand.NewPCG(4, 4))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		// Scores above the generated range always improve, so every submission moves an entry.
		if _, err := svc.Submit(ctx, selfhost.Submission{Board: "high-score", Account: accounts[rng.IntN(len(accounts))], Score: 10_000_000 + rng.Int64N(1_000_000)}); err != nil {
			b.Fatal(err)
		}
	}
	checkBudget(b)
}
