package ai_test

import (
	"github.com/nsgo-dev/game/pkg/ai"
	"testing"
)

func TestSmartObjectsClaimDisableAndSnapshot(t *testing.T) {
	s := ai.NewSmartObjects()
	var released ai.SmartClaim
	s.SetOnReleased(func(c ai.SmartClaim) { released = c })
	s.Register(ai.SmartObject{ID: "bench", X: 1, Y: 1, Slots: []ai.SmartSlot{{ID: "b", Tags: []string{"sit"}}, {ID: "a", Tags: []string{"sit"}}}})
	found := s.FindInRadius(0, 0, 3, ai.SmartQuery{Tags: []string{"sit"}})
	if len(found) != 2 || found[0].SlotID != "a" {
		t.Fatalf("found=%v", found)
	}
	_, ok := s.Claim("one", "bench", "a")
	if !ok {
		t.Fatal("claim failed")
	}
	if _, ok := s.Claim("two", "bench", "a"); ok {
		t.Fatal("double claim")
	}
	snap := s.Snapshot()
	s.Disable("bench")
	if released.Agent != "one" {
		t.Fatalf("released=%+v", released)
	}
	s.Restore(snap)
	if _, ok := s.Claim("two", "bench", "a"); ok {
		t.Fatal("restored claim unexpectedly free")
	}
}
