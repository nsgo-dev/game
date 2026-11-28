package session

import (
	"testing"
	"time"
)

func TestSummaryPercentilesAndJank(t *testing.T) {
	r := New(time.Millisecond)
	for i := 1; i <= 100; i++ {
		r.Observe(time.Duration(i) * time.Millisecond)
	}
	s := r.Summary()
	if s.Frames != 100 || s.P95 < 90*time.Millisecond || s.Jank2x != 99 {
		t.Fatalf("summary=%+v", s)
	}
}
