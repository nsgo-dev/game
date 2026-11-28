package entities

import (
	"testing"
	"time"
)

func TestSchedulerRunIfSkipsWithoutChangingOrder(t *testing.T) {
	s := NewScheduler()
	n := 0
	_ = s.AddSystem("a", func(*World, time.Duration) { n++ }, RunIf(func(*World) bool { return false }))
	_ = s.AddSystem("b", func(*World, time.Duration) { n++ })
	if err := s.Step(NewWorld(), 0); err != nil {
		t.Fatal(err)
	}
	if n != 1 || s.String() != "a -> b" {
		t.Fatalf("n=%d order=%s", n, s)
	}
}
