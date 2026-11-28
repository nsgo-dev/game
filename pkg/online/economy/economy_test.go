package economy

import (
	"sync"
	"testing"
)

func TestIdempotentGrantAndConcurrentSpend(t *testing.T) {
	w := New()
	w.Grant("a", "coin", "g", 100)
	var wg sync.WaitGroup
	wins := 0
	var mu sync.Mutex
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := w.Spend("a", "coin", "s", 10); e == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 20 || w.Balance("a", "coin") != 90 {
		t.Fatalf("wins=%d balance=%d", wins, w.Balance("a", "coin"))
	}
}
