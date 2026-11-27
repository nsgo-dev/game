package netplay

import (
	"testing"
)

func TestClientIgnoresSnapshotsFromOtherPeers(t *testing.T) {
	hub := NewLoopbackHub(LoopbackConfig{})
	server := hub.AddPeer(1)
	client := hub.AddPeer(2)
	attacker := hub.AddPeer(3)

	c := NewClientWorld(client, 1)
	_ = server

	full := encodeFullSnapshot(1, 50, map[EntityID]EntityState{1: {ID: 1, ComponentState: ComponentState{X: 999}}})
	if err := attacker.Send(2, ChannelUnreliable, full); err != nil {
		t.Fatalf("attacker send: %v", err)
	}
	hub.Advance(0)
	if applied := c.Poll(); applied != 0 {
		t.Fatalf("Poll applied %d snapshots from a forged sender, want 0", applied)
	}
	if st, ok := c.Entity(1); ok || st.X == 999 {
		t.Fatalf("client accepted a foreign snapshot: state=%+v ok=%v", st, ok)
	}

	// The real server still updates the client.
	if err := server.Send(2, ChannelUnreliable, encodeFullSnapshot(1, 1, map[EntityID]EntityState{1: {ID: 1, ComponentState: ComponentState{X: 42}}})); err != nil {
		t.Fatalf("server send: %v", err)
	}
	hub.Advance(0)
	if applied := c.Poll(); applied != 1 {
		t.Fatalf("Poll applied %d server snapshots, want 1", applied)
	}
	if st, ok := c.Entity(1); !ok || st.X != 42 {
		t.Fatalf("server snapshot not applied: %+v %v", st, ok)
	}
}
