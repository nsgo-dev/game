package netplay

import "testing"

func TestPeerBudgetConfiguration(t *testing.T) {
	w := NewServerWorld(nil)
	w.SetPeerBudget(64 * 1024)
	w.SetPriority(func(PeerID, EntityState) float32 { return 1 })
	if w.peerBudget != 64*1024 || w.priority == nil {
		t.Fatal("budget configuration missing")
	}
}
