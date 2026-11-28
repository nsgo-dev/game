package redeem

import (
	"errors"
	"sync"
	"testing"
)

func TestConcurrentSingleUse(t *testing.T) {
	s := New()
	s.Add(Code{Value: "abc", Reward: "coins", MaxUses: 1})
	var wg sync.WaitGroup
	var wins int
	var mu sync.Mutex
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.Redeem("a", "abc"); e == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			} else if !errors.Is(e, ErrCodeUsed) && !errors.Is(e, ErrCodeLimit) {
				t.Errorf("err=%v", e)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("wins=%d", wins)
	}
}
