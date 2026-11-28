package social

import "testing"

func TestFriendInviteAcceptBlock(t *testing.T) {
	g := NewGraph()
	if err := g.AddFriend("a", "b"); err != nil {
		t.Fatal(err)
	}
	if len(g.Friends("b", StateInviteReceived)) != 1 {
		t.Fatal()
	}
	if err := g.AddFriend("b", "a"); err != nil {
		t.Fatal(err)
	}
	if len(g.Friends("a", StateFriend)) != 1 {
		t.Fatal()
	}
	g.Block("a", "b")
	if !g.Blocked("b", "a") {
		t.Fatal()
	}
	if err := g.AddFriend("a", "b"); err != ErrBlocked {
		t.Fatal(err)
	}
}
