package entities_test

import (
	"github.com/nsgo-dev/game/pkg/entities"
	"testing"
	"time"
)

func TestSchedulerEveryRunsDeterministically(t *testing.T) {
	w := entities.NewWorld()
	s := entities.NewScheduler()
	n := 0
	if err := s.AddSystem("slow", func(*entities.World, time.Duration) { n++ }, entities.Every(3)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		if err := s.Step(w, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if n != 3 {
		t.Fatalf("runs=%d want 3", n)
	}
}
