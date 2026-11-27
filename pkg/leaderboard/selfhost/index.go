package selfhost

import (
	"math/rand/v2"
)

// maxLevel bounds the skip list's height; with p = 1/4 it serves 4^16 entries at the expected O(log n) cost.
const maxLevel = 16

// entry is one account's best score in a period.
type entry struct {
	account  string
	score    int64
	achieved int64 // Unix nanoseconds
}

type link struct {
	to   *node
	span int // ranks from this node to to
}

type node struct {
	e    entry
	next []link
}

// index ranks a period's entries: an order-statistic skip list (each link counts the ranks it skips, as in Redis's sorted sets) beside a map from account to node, so finding an account's rank, the entry at a rank, and inserting or moving a score all take O(log n). It is not safe for concurrent use.
type index struct {
	ascending bool
	head      node
	level     int
	n         int
	byAccount map[string]*node
	rng       *rand.Rand
}

func newIndex(order Order) *index {
	x := &index{ascending: order == Ascending, level: 1, byAccount: map[string]*node{}}
	x.head.next = make([]link, maxLevel)
	// A fixed seed keeps the shape, and so the cost, of an index reproducible; the order of entries never depends on it.
	x.rng = rand.New(rand.NewPCG(0x6c656164, 0x626f617264))
	return x
}

// before reports whether a ranks above b: the better score, then the earlier achievement, then the smaller account ID.
func (x *index) before(a, b *entry) bool {
	if a.score != b.score {
		if x.ascending {
			return a.score < b.score
		}
		return a.score > b.score
	}
	if a.achieved != b.achieved {
		return a.achieved < b.achieved
	}
	return a.account < b.account
}

// better reports whether score beats the account's entry e on this index's order.
func (x *index) better(score int64, e entry) bool {
	if x.ascending {
		return score < e.score
	}
	return score > e.score
}

func (x *index) randomLevel() int {
	l := 1
	for l < maxLevel && x.rng.Uint32()&3 == 0 {
		l++
	}
	return l
}

// get returns the account's entry.
func (x *index) get(account string) (entry, bool) {
	n, ok := x.byAccount[account]
	if !ok {
		return entry{}, false
	}
	return n.e, true
}

// set puts e as its account's entry, replacing any earlier one.
func (x *index) set(e entry) {
	if old, ok := x.byAccount[e.account]; ok {
		x.remove(&old.e)
	}
	var update [maxLevel]*node
	var rank [maxLevel]int
	cur := &x.head
	for i := x.level - 1; i >= 0; i-- {
		if i < x.level-1 {
			rank[i] = rank[i+1]
		}
		for cur.next[i].to != nil && x.before(&cur.next[i].to.e, &e) {
			rank[i] += cur.next[i].span
			cur = cur.next[i].to
		}
		update[i] = cur
	}
	lvl := x.randomLevel()
	if lvl > x.level {
		for i := x.level; i < lvl; i++ {
			rank[i] = 0
			update[i] = &x.head
			update[i].next[i].span = x.n
		}
		x.level = lvl
	}
	nd := &node{e: e, next: make([]link, lvl)}
	for i := 0; i < lvl; i++ {
		nd.next[i].to = update[i].next[i].to
		update[i].next[i].to = nd
		nd.next[i].span = update[i].next[i].span - (rank[0] - rank[i])
		update[i].next[i].span = rank[0] - rank[i] + 1
	}
	for i := lvl; i < x.level; i++ {
		update[i].next[i].span++
	}
	x.n++
	x.byAccount[e.account] = nd
}

// remove takes the node holding e out of the list and the map.
func (x *index) remove(e *entry) {
	var update [maxLevel]*node
	cur := &x.head
	for i := x.level - 1; i >= 0; i-- {
		for cur.next[i].to != nil && x.before(&cur.next[i].to.e, e) {
			cur = cur.next[i].to
		}
		update[i] = cur
	}
	target := cur.next[0].to
	for i := 0; i < x.level; i++ {
		if update[i].next[i].to == target {
			update[i].next[i].span += target.next[i].span - 1
			update[i].next[i].to = target.next[i].to
		} else {
			update[i].next[i].span--
		}
	}
	for x.level > 1 && x.head.next[x.level-1].to == nil {
		x.head.next[x.level-1].span = 0
		x.level--
	}
	x.n--
	delete(x.byAccount, e.account)
}

// rank returns the account's 1-based rank, or 0 when it has no entry.
func (x *index) rank(account string) int {
	nd, ok := x.byAccount[account]
	if !ok {
		return 0
	}
	r := 0
	cur := &x.head
	for i := x.level - 1; i >= 0; i-- {
		for cur.next[i].to != nil && (cur.next[i].to == nd || x.before(&cur.next[i].to.e, &nd.e)) {
			r += cur.next[i].span
			cur = cur.next[i].to
		}
		if cur == nd {
			return r
		}
	}
	return r
}

// at returns the node at 1-based rank r, or nil when r is outside 1..n.
func (x *index) at(r int) *node {
	if r < 1 || r > x.n {
		return nil
	}
	traversed := 0
	cur := &x.head
	for i := x.level - 1; i >= 0; i-- {
		for cur.next[i].to != nil && traversed+cur.next[i].span <= r {
			traversed += cur.next[i].span
			cur = cur.next[i].to
		}
		if traversed == r {
			return cur
		}
	}
	return nil
}

// window returns up to limit entries from 1-based rank first on, with their ranks.
func (x *index) window(first, limit int) []Entry {
	if first < 1 {
		first = 1
	}
	if limit <= 0 || first > x.n {
		return nil
	}
	nd := x.at(first)
	const maxWindowPrealloc = 1024
	out := make([]Entry, 0, min(limit, x.n-first+1, maxWindowPrealloc))
	for r := first; nd != nil && len(out) < limit; r++ {
		out = append(out, nd.e.public(r))
		nd = nd.next[0].to
	}
	return out
}
