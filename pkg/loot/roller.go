package loot

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/random"
)

// MaxMisses is the largest value a pity miss counter holds; counting saturates there, and SetState refuses larger values.
const MaxMisses = 1 << 30

// Drop is one item a roll produced, with its quantity (always at least 1).
type Drop struct {
	Item  string `json:"item" yaml:"item"`
	Count int    `json:"count" yaml:"count"`
}

// State is a Roller's pity counters as plain data for save slots, keyed by table name and then by pity rule name. Only non-zero counters are listed. Its map keys marshal in sorted order with encoding/json and YAML, so its text is deterministic.
type State struct {
	Pity map[string]map[string]int `json:"pity,omitempty" yaml:"pity,omitempty"`
}

// Roller rolls a Library's tables on one random.Stream and keeps the pity miss counters of one player or save. A Roller is not safe for concurrent use; give each goroutine its own Roller and stream.
type Roller struct {
	lib     *Library
	stream  *random.Stream
	misses  []int
	scratch []float64
}

// NewRoller returns a Roller with zero pity counters that rolls lib's tables on stream, typically a stream forked for loot such as streams.Stream("loot"). It panics if lib or stream is nil.
func NewRoller(lib *Library, stream *random.Stream) *Roller {
	if lib == nil || stream == nil {
		panic("loot: NewRoller needs a Library and a Stream")
	}
	return &Roller{
		lib:     lib,
		stream:  stream,
		misses:  make([]int, lib.counters),
		scratch: make([]float64, lib.maxEntries),
	}
}

// Library returns the Library r rolls.
func (r *Roller) Library() *Library { return r.lib }

// Stream returns the stream r draws from; save its State beside r.State to resume the same drops.
func (r *Roller) Stream() *random.Stream { return r.stream }

// Roll rolls the named table once and returns its drops in the order they were produced: guaranteed entries first, then each weighted pick, with nested tables expanded in place.
func (r *Roller) Roll(table string) ([]Drop, error) {
	return r.AppendRoll(nil, table)
}

// AppendRoll is like Roll but appends the drops to dst, so a caller that reuses dst rolls without allocating. On an unknown table it returns dst unchanged and an error wrapping ErrUnknownTable, without drawing from the stream or touching the counters.
func (r *Roller) AppendRoll(dst []Drop, table string) ([]Drop, error) {
	ti, ok := r.lib.byName[table]
	if !ok {
		return dst, fmt.Errorf("%w %q", ErrUnknownTable, table)
	}
	return r.roll(dst, ti), nil
}

func (r *Roller) roll(dst []Drop, ti int) []Drop {
	t := &r.lib.tables[ti]
	for i := range t.guaranteed {
		dst = r.emit(dst, &t.guaranteed[i])
	}
	if len(t.entries) == 0 {
		return dst
	}
	n := t.rollsMin
	if t.rollsMax > n {
		n = r.stream.IntRange(t.rollsMin, t.rollsMax)
	}
	for range n {
		dst = r.emit(dst, &t.entries[r.pick(t)])
	}
	return dst
}

// pick chooses one of t's entries, applying hard and soft pity, and updates t's miss counters.
func (r *Roller) pick(t *compiledTable) int {
	idx := -1
	for i := range t.pity {
		p := &t.pity[i]
		if p.guaranteeAt > 0 && r.misses[p.counter]+1 >= p.guaranteeAt {
			idx = p.target
			break
		}
	}
	if idx < 0 {
		ws := t.weights
		if t.hasSoft {
			ws = r.scratch[:len(t.weights)]
			copy(ws, t.weights)
			for i := range t.pity {
				p := &t.pity[i]
				if p.softFrom == 0 {
					continue
				}
				if n := r.misses[p.counter] + 1; n >= p.softFrom {
					ws[p.target] += float64(n-p.softFrom+1) * p.softStep
				}
			}
		}
		// Every weight is positive, so WeightedIndex never returns -1 here.
		idx = r.stream.WeightedIndex(ws)
	}
	for i := range t.pity {
		p := &t.pity[i]
		if idx == p.target {
			r.misses[p.counter] = 0
		} else if r.misses[p.counter] < MaxMisses {
			r.misses[p.counter]++
		}
	}
	return idx
}

func (r *Roller) emit(dst []Drop, e *compiledEntry) []Drop {
	if e.kind == kindNothing {
		return dst
	}
	n := e.min
	if e.max > n {
		n = r.stream.IntRange(e.min, e.max)
	}
	switch e.kind {
	case kindItem:
		if n > 0 {
			dst = append(dst, Drop{Item: e.item, Count: n})
		}
	case kindTable:
		for range n {
			dst = r.roll(dst, e.table)
		}
	}
	return dst
}

// Misses returns how many picks of table have passed since the target of its pity rule was last picked, and false when the table or rule does not exist. The next pick is pick number Misses+1, which is what GuaranteeAt and SoftFrom count.
func (r *Roller) Misses(table, rule string) (int, bool) {
	ti, ok := r.lib.byName[table]
	if !ok {
		return 0, false
	}
	for _, p := range r.lib.tables[ti].pity {
		if p.name == rule {
			return r.misses[p.counter], true
		}
	}
	return 0, false
}

// State returns a copy of r's pity counters.
func (r *Roller) State() State {
	var st State
	for ti := range r.lib.tables {
		t := &r.lib.tables[ti]
		for _, p := range t.pity {
			n := r.misses[p.counter]
			if n == 0 {
				continue
			}
			if st.Pity == nil {
				st.Pity = make(map[string]map[string]int)
			}
			m := st.Pity[t.name]
			if m == nil {
				m = make(map[string]int)
				st.Pity[t.name] = m
			}
			m[p.name] = n
		}
	}
	return st
}

// SetState replaces r's pity counters with st's. Rules missing from st start at zero, and counters for tables or rules the Library no longer has are ignored, so a save outlives edits to the loot document. It refuses a counter below 0 or above MaxMisses and then leaves r unchanged.
func (r *Roller) SetState(st State) error {
	for table, rules := range st.Pity {
		for rule, n := range rules {
			if n < 0 || n > MaxMisses {
				return fmt.Errorf("loot: pity counter %s/%s = %d must be in [0, %d]", table, rule, n, MaxMisses)
			}
		}
	}
	for i := range r.misses {
		r.misses[i] = 0
	}
	for ti := range r.lib.tables {
		t := &r.lib.tables[ti]
		rules := st.Pity[t.name]
		for _, p := range t.pity {
			r.misses[p.counter] = rules[p.name]
		}
	}
	return nil
}

// ResetPity sets every pity counter of r back to zero.
func (r *Roller) ResetPity() {
	for i := range r.misses {
		r.misses[i] = 0
	}
}
