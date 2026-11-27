package loot

import (
	"errors"
	"fmt"
)

// ErrUnknownTable is returned by Roller.Roll for a table name the Library does not hold.
var ErrUnknownTable = errors.New("loot: unknown table")

type entryKind uint8

const (
	kindNothing entryKind = iota
	kindItem
	kindTable
)

type compiledEntry struct {
	kind  entryKind
	item  string
	table int
	min   int
	max   int
}

type compiledPity struct {
	name        string
	target      int
	counter     int
	guaranteeAt int
	softFrom    int
	softStep    float64
}

type compiledTable struct {
	name       string
	rollsMin   int
	rollsMax   int
	guaranteed []compiledEntry
	entries    []compiledEntry
	weights    []float64
	pity       []compiledPity
	hasSoft    bool
	steps      int
}

// Library is a compiled, immutable set of tables that any number of Rollers can roll concurrently.
type Library struct {
	tables     []compiledTable
	byName     map[string]int
	counters   int
	maxEntries int
}

// New validates doc and compiles it into a Library. The Library does not keep doc, so later changes to doc do not affect it.
func New(doc *Document) (*Library, error) {
	return compile(doc)
}

// Has reports whether the Library holds a table named name.
func (l *Library) Has(name string) bool {
	_, ok := l.byName[name]
	return ok
}

// Tables returns the Library's table names in document order.
func (l *Library) Tables() []string {
	out := make([]string, len(l.tables))
	for i := range l.tables {
		out[i] = l.tables[i].name
	}
	return out
}

// Chances returns the probability of each of the named table's Entries on one pick with no pity ramp applied, in document order. It returns nil for an unknown table or one without entries.
func (l *Library) Chances(table string) []float64 {
	ti, ok := l.byName[table]
	if !ok || len(l.tables[ti].weights) == 0 {
		return nil
	}
	ws := l.tables[ti].weights
	var total float64
	for _, w := range ws {
		total += w
	}
	out := make([]float64, len(ws))
	for i, w := range ws {
		out[i] = w / total
	}
	return out
}

func rangeOf(r *Range) (int, int) {
	if r == nil {
		return 1, 1
	}
	return r.Min, r.Max
}

// satMul multiplies two non-negative step counts, saturating just above MaxRollSteps.
func satMul(a, b int) int {
	const cap = MaxRollSteps + 1
	if a == 0 || b == 0 {
		return 0
	}
	if a > cap/b {
		return cap
	}
	return min(a*b, cap)
}

func satAdd(a, b int) int {
	return min(a+b, MaxRollSteps+1)
}

func compile(doc *Document) (*Library, error) {
	if doc == nil {
		return nil, errors.New("loot: nil document")
	}
	if doc.Version != CurrentVersion {
		return nil, fmt.Errorf("loot: unsupported version %d (want %d)", doc.Version, CurrentVersion)
	}
	if len(doc.Tables) == 0 {
		return nil, errors.New("loot: a document needs at least one table")
	}
	lib := &Library{
		tables: make([]compiledTable, len(doc.Tables)),
		byName: make(map[string]int, len(doc.Tables)),
	}
	for i := range doc.Tables {
		name := doc.Tables[i].Name
		if !validName(name) {
			return nil, fmt.Errorf("loot: tables[%d]: name %q must be non-empty without surrounding spaces", i, name)
		}
		if _, dup := lib.byName[name]; dup {
			return nil, fmt.Errorf("loot: tables[%d]: duplicate table name %q", i, name)
		}
		lib.byName[name] = i
	}

	for i := range doc.Tables {
		t := &doc.Tables[i]
		where := fmt.Sprintf("table %q", t.Name)
		ct := &lib.tables[i]
		ct.name = t.Name
		if len(t.Entries) == 0 && len(t.Guaranteed) == 0 {
			return nil, fmt.Errorf("loot: %s: a table needs entries or guaranteed entries", where)
		}
		if len(t.Entries) == 0 && t.Rolls != nil {
			return nil, fmt.Errorf("loot: %s: rolls needs entries", where)
		}
		if err := validateRange(where+": rolls", t.Rolls, MaxRolls); err != nil {
			return nil, err
		}
		ct.rollsMin, ct.rollsMax = rangeOf(t.Rolls)
		if len(t.Entries) == 0 {
			ct.rollsMin, ct.rollsMax = 0, 0
		}

		var err error
		if ct.guaranteed, err = lib.compileEntries(where+": guaranteed", t.Guaranteed, true); err != nil {
			return nil, err
		}
		if ct.entries, err = lib.compileEntries(where+": entries", t.Entries, false); err != nil {
			return nil, err
		}
		ct.weights = make([]float64, len(t.Entries))
		items := make(map[string]int, len(t.Entries))
		tables := make(map[string]int, len(t.Entries))
		for j := range t.Entries {
			ct.weights[j] = t.Entries[j].Weight
			e := &t.Entries[j]
			switch {
			case e.Item != "":
				if _, dup := items[e.Item]; dup {
					return nil, fmt.Errorf("loot: %s: entries[%d]: item %q is already an entry", where, j, e.Item)
				}
				items[e.Item] = j
			case e.Table != "":
				if _, dup := tables[e.Table]; dup {
					return nil, fmt.Errorf("loot: %s: entries[%d]: table %q is already an entry", where, j, e.Table)
				}
				tables[e.Table] = j
			}
		}
		lib.maxEntries = max(lib.maxEntries, len(t.Entries))

		names := make(map[string]struct{}, len(t.Pity))
		for j := range t.Pity {
			p := &t.Pity[j]
			pwhere := fmt.Sprintf("%s: pity[%d]", where, j)
			if err := validatePity(pwhere, p); err != nil {
				return nil, err
			}
			if _, dup := names[p.Name]; dup {
				return nil, fmt.Errorf("loot: %s: duplicate pity name %q", pwhere, p.Name)
			}
			names[p.Name] = struct{}{}
			var target int
			var ok bool
			if p.Item != "" {
				target, ok = items[p.Item]
			} else {
				target, ok = tables[p.Table]
			}
			if !ok {
				return nil, fmt.Errorf("loot: %s: target is not one of the table's entries", pwhere)
			}
			ct.pity = append(ct.pity, compiledPity{
				name:        p.Name,
				target:      target,
				counter:     lib.counters,
				guaranteeAt: p.GuaranteeAt,
				softFrom:    p.SoftFrom,
				softStep:    p.SoftStep,
			})
			if p.SoftFrom > 0 {
				ct.hasSoft = true
			}
			lib.counters++
		}
	}

	if err := lib.computeSteps(); err != nil {
		return nil, err
	}
	return lib, nil
}

func (l *Library) compileEntries(where string, entries []Entry, guaranteed bool) ([]compiledEntry, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	out := make([]compiledEntry, len(entries))
	for i := range entries {
		e := &entries[i]
		ewhere := fmt.Sprintf("%s[%d]", where, i)
		if err := validateEntry(ewhere, e, guaranteed); err != nil {
			return nil, err
		}
		ce := compiledEntry{}
		ce.min, ce.max = rangeOf(e.Count)
		switch {
		case e.Nothing:
			ce.kind = kindNothing
			ce.min, ce.max = 0, 0
		case e.Item != "":
			ce.kind, ce.item = kindItem, e.Item
		default:
			ti, ok := l.byName[e.Table]
			if !ok {
				return nil, fmt.Errorf("loot: %s: unknown table %q", ewhere, e.Table)
			}
			ce.kind, ce.table = kindTable, ti
		}
		out[i] = ce
	}
	return out, nil
}

// computeSteps refuses reference cycles and computes each table's worst-case work, refusing tables above MaxRollSteps. Each weighted pick counts as the table's number of entries plus its number of pity rules, so wide tables are bounded as well as deep or repeated ones.
func (l *Library) computeSteps() error {
	const (
		unvisited = iota
		visiting
		done
	)
	state := make([]uint8, len(l.tables))
	var visit func(i int, path []string) error
	visit = func(i int, path []string) error {
		switch state[i] {
		case done:
			return nil
		case visiting:
			return fmt.Errorf("loot: table reference cycle: %v -> %s", path, l.tables[i].name)
		}
		state[i] = visiting
		path = append(path, l.tables[i].name)
		t := &l.tables[i]
		entrySteps := func(e *compiledEntry) (int, error) {
			switch e.kind {
			case kindItem:
				return 1, nil
			case kindTable:
				if err := visit(e.table, path); err != nil {
					return 0, err
				}
				return satMul(e.max, satAdd(1, l.tables[e.table].steps)), nil
			}
			return 0, nil
		}
		steps := 0
		for j := range t.guaranteed {
			s, err := entrySteps(&t.guaranteed[j])
			if err != nil {
				return err
			}
			steps = satAdd(steps, s)
		}
		worst := 0
		for j := range t.entries {
			s, err := entrySteps(&t.entries[j])
			if err != nil {
				return err
			}
			worst = max(worst, s)
		}
		// A weighted pick scans every entry (WeightedIndex and the soft pity ramp) and every pity rule, so it costs len(entries)+len(pity) steps on top of the work of the entry it picks.
		pickCost := len(t.entries) + len(t.pity)
		steps = satAdd(steps, satMul(t.rollsMax, satAdd(pickCost, worst)))
		if steps > MaxRollSteps {
			return fmt.Errorf("loot: table %q: a roll can take more than %d steps; lower its rolls or counts", t.name, MaxRollSteps)
		}
		t.steps = steps
		state[i] = done
		return nil
	}
	for i := range l.tables {
		if err := visit(i, nil); err != nil {
			return err
		}
	}
	return nil
}
