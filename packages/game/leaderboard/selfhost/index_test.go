package selfhost

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// model is the index's reference: every entry in a slice, sorted on demand.
type model struct {
	x       *index // only for its order
	entries map[string]entry
}

func (m *model) sorted() []entry {
	out := make([]entry, 0, len(m.entries))
	for _, e := range m.entries {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b entry) int {
		switch {
		case m.x.before(&a, &b):
			return -1
		case m.x.before(&b, &a):
			return 1
		}
		return 0
	})
	return out
}

func checkIndex(t *testing.T, x *index, m *model) {
	t.Helper()
	want := m.sorted()
	if x.n != len(want) || len(x.byAccount) != len(want) {
		t.Fatalf("size = %d (map %d), want %d", x.n, len(x.byAccount), len(want))
	}
	got := x.window(1, len(want)+1)
	if len(got) != len(want) {
		t.Fatalf("window holds %d entries, want %d", len(got), len(want))
	}
	for i, e := range want {
		r := i + 1
		if got[i].Rank != r || got[i].Account != e.account || got[i].Score != e.score {
			t.Fatalf("rank %d = %+v, want %+v", r, got[i], e)
		}
		if gotRank := x.rank(e.account); gotRank != r {
			t.Fatalf("rank(%s) = %d, want %d", e.account, gotRank, r)
		}
		if nd := x.at(r); nd == nil || nd.e != e {
			t.Fatalf("at(%d) = %v, want %+v", r, nd, e)
		}
	}
	if x.at(0) != nil || x.at(len(want)+1) != nil {
		t.Fatal("at outside 1..n returned a node")
	}
	// Every level's spans add up to the size.
	for l := 0; l < x.level; l++ {
		sum := 0
		for cur := &x.head; cur.next[l].to != nil; cur = cur.next[l].to {
			sum += cur.next[l].span
		}
		if sum > x.n {
			t.Fatalf("level %d spans %d > %d", l, sum, x.n)
		}
	}
}

func TestIndexMatchesASortedModel(t *testing.T) {
	for _, order := range []Order{Descending, Ascending} {
		t.Run(string(order), func(t *testing.T) {
			x := newIndex(order)
			m := &model{x: x, entries: map[string]entry{}}
			rng := rand.New(rand.NewPCG(1, 2))
			for step := 0; step < 4000; step++ {
				acct := fmt.Sprintf("a%03d", rng.IntN(300))
				switch op := rng.IntN(10); {
				case op < 8:
					// Narrow scores and times make ties on both common.
					e := entry{account: acct, score: int64(rng.IntN(40)), achieved: int64(rng.IntN(20))}
					x.set(e)
					m.entries[acct] = e
				default:
					if e, ok := m.entries[acct]; ok {
						x.remove(&e)
						delete(m.entries, acct)
					}
				}
				if step%97 == 0 {
					checkIndex(t, x, m)
				}
			}
			checkIndex(t, x, m)
			for acct, e := range m.entries {
				x.remove(&e)
				delete(m.entries, acct)
			}
			checkIndex(t, x, m)
			if x.level != 1 {
				t.Fatalf("empty index has level %d", x.level)
			}
		})
	}
}

func TestIndexOrdersTiesByTimeThenAccount(t *testing.T) {
	x := newIndex(Descending)
	x.set(entry{account: "b", score: 10, achieved: 5})
	x.set(entry{account: "a", score: 10, achieved: 5})
	x.set(entry{account: "c", score: 10, achieved: 1})
	x.set(entry{account: "d", score: 11, achieved: 9})
	var got []string
	for _, e := range x.window(1, 10) {
		got = append(got, e.Account)
	}
	if want := []string{"d", "c", "a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
