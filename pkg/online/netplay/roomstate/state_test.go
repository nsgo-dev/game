package roomstate

import "testing"

func TestDeltaDirtyOrderingAndViews(t *testing.T) {
	s := New()
	s.Set("hand", []int{1}, ViewOwner, "alice", "")
	s.Set("score", 3, ViewAll, "", "")
	b, v, err := s.Delta(0, "bob", "")
	if err != nil {
		t.Fatal(err)
	}
	if v != 2 || string(b) != `{"version":2,"fields":{"score":3}}` {
		t.Fatalf("%d %s", v, b)
	}
	b, _, err = s.Delta(0, "alice", "")
	if err != nil || string(b) != `{"version":2,"fields":{"hand":[1],"score":3}}` {
		t.Fatalf("%s %v", b, err)
	}
}
