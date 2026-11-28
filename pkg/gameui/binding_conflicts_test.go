package gameui

import "testing"

func TestResolveBindingConflictPolicies(t *testing.T) {
	for _, p := range []ConflictPolicy{ConflictSwap, ConflictClear, ConflictRefuse} {
		b := map[string][]string{"jump": {"pad:south"}, "left": {"pad:south"}}
		g := map[string]string{"jump": "game", "left": "game"}
		_, ok := ResolveBindingConflict(b, g, "jump", "pad:south", p)
		if p == ConflictRefuse && ok {
			t.Fatal("refused conflict")
		}
		if p != ConflictRefuse && !ok {
			t.Fatal("accepted conflict")
		}
	}
}
