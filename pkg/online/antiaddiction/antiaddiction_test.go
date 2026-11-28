package antiaddiction

import (
	"testing"
	"time"
)

func TestWindowBoundaries(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	fri := time.Date(2026, 11, 27, 20, 0, 0, 0, loc)
	if ok, until := Window(15, fri, nil); !ok || until.Hour() != 21 {
		t.Fatalf("start=%v until=%v", ok, until)
	}
	if ok, _ := Window(15, fri.Add(time.Hour), nil); ok {
		t.Fatal("window remained open")
	}
	if ok, _ := Window(17, time.Date(2026, 11, 26, 20, 30, 0, 0, loc), map[string]bool{"2026-11-26": true}); !ok {
		t.Fatal("holiday denied")
	}
}
func TestSpendCaps(t *testing.T) {
	if SpendAllowed(7, 1, 0) || !SpendAllowed(10, 50, 150) || SpendAllowed(10, 51, 0) || !SpendAllowed(17, 100, 300) || SpendAllowed(17, 1, 400) {
		t.Fatal("spend caps")
	}
}
