package social

import (
	"errors"
	"testing"
)

func TestGroupsRolesAndBans(t *testing.T) {
	s := NewGroups()
	g, _ := s.Create("owner", "Guild", 2, true)
	if err := s.Join(g.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if err := s.Promote(g.ID, "member", "owner"); !errors.Is(err, ErrGroupPermission) {
		t.Fatal("member promoted")
	}
	if err := s.Leave(g.ID, "owner"); !errors.Is(err, ErrGroupPermission) {
		t.Fatal("owner left")
	}
	if len(s.Search("gu")) != 1 {
		t.Fatal("search")
	}
}
