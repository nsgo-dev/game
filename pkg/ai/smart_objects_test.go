package ai

import "testing"

func TestSmartObjectsClaimReleaseAndSnapshot(t *testing.T) {
	s := NewSmartObjects()
	s.Register(SmartObject{ID: "bench", X: 1, Y: 1, Slots: []SmartSlot{{ID: "a", Tags: []string{"sit"}}}})
	got := s.FindInRadius(0, 0, 2, SmartQuery{Tags: []string{"sit"}})
	if len(got) != 1 {
		t.Fatal(got)
	}
	c, ok := s.Claim("agent", got[0].ID)
	if !ok || !s.Use(c) {
		t.Fatal("claim failed")
	}
	if _, ok = s.Claim("other", got[0].ID); ok {
		t.Fatal("double claim")
	}
	snap := s.Snapshot()
	s.Release(c)
	s.Restore(snap)
	if !s.Use(c) {
		t.Fatal("restore failed")
	}
}
func TestSmartObjectsDisableReleases(t *testing.T) {
	s := NewSmartObjects()
	released := 0
	s.OnReleased = func(SmartClaim) { released++ }
	s.Register(SmartObject{ID: "bed", Slots: []SmartSlot{{ID: "slot"}}})
	c, ok := s.Claim("a", "bed/slot")
	if !ok {
		t.Fatal()
	}
	s.SetEnabled("bed", false)
	if released != 1 || s.Use(c) {
		t.Fatal("disable did not release")
	}
}
