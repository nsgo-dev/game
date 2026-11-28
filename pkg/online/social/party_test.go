package social

import "testing"

func TestPartyLifecycle(t *testing.T) {
	p := NewParties()
	x := p.Create("a", 2, true)
	if _, e := p.Join(x.ID, "b"); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Join(x.ID, "c"); e != ErrPartyFull {
		t.Fatal(e)
	}
	y := p.Leave(x.ID, "a")
	if y.Leader != "b" {
		t.Fatalf("leader=%s", y.Leader)
	}
}
