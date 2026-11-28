package sqlitestore_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost"
	"github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost/sqlitestore"
)

func TestConcurrentBestScore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlitestore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Configure([]selfhost.Board{{Name: "scores", Order: selfhost.Descending}})
	var wg sync.WaitGroup
	for i := int64(1); i <= 32; i++ {
		wg.Add(1)
		go func(score int64) {
			defer wg.Done()
			if err := s.Save(ctx, selfhost.Record{Board: "scores", Period: "all", Account: "ada", Score: score, Achieved: time.Unix(score, 0)}); err != nil {
				t.Errorf("save: %v", err)
			}
		}(i)
	}
	wg.Wait()
	records, err := s.Records(ctx, "scores", "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Score != 32 {
		t.Fatalf("records=%+v", records)
	}
}
