package stats_test

import (
	"errors"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/stats"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func newSheet(t *testing.T, defs ...stats.Def) *stats.Sheet {
	t.Helper()
	s, err := stats.NewSheet(defs...)
	if err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	return s
}

func mustAdd(t *testing.T, s *stats.Sheet, source string, m stats.Modifier) stats.ID {
	t.Helper()
	id, err := s.Add(source, m)
	if err != nil {
		t.Fatalf("Add(%q, %+v): %v", source, m, err)
	}
	return id
}

func mustAddTimed(t *testing.T, s *stats.Sheet, source string, m stats.Modifier, steps int) stats.ID {
	t.Helper()
	id, err := s.AddTimed(source, m, steps)
	if err != nil {
		t.Fatalf("AddTimed(%q, %+v, %d): %v", source, m, steps, err)
	}
	return id
}

func TestStackingFollowsDocumentedRule(t *testing.T) {
	add := func(v float64) stats.Modifier { return stats.Modifier{Stat: "x", Op: stats.OpAdd, Value: v} }
	mul := func(v float64) stats.Modifier { return stats.Modifier{Stat: "x", Op: stats.OpMul, Value: v} }
	override := func(v float64, p int) stats.Modifier {
		return stats.Modifier{Stat: "x", Op: stats.OpOverride, Value: v, Priority: p}
	}
	tests := []struct {
		name string
		def  stats.Def
		mods []stats.Modifier
		want float64
	}{
		{"base only", stats.Def{Base: 10}, nil, 10},
		{"adds sum", stats.Def{Base: 10}, []stats.Modifier{add(5), add(3), add(-2)}, 16},
		{"muls multiply", stats.Def{Base: 10}, []stats.Modifier{mul(2), mul(1.5)}, 30},
		{"adds before muls whatever the order", stats.Def{Base: 10}, []stats.Modifier{mul(2), add(5), mul(1.5), add(3)}, 54},
		{"mul by zero", stats.Def{Base: 10}, []stats.Modifier{add(5), mul(0)}, 0},
		{"override ignores adds and muls", stats.Def{Base: 10}, []stats.Modifier{add(5), override(7, 0), mul(3)}, 7},
		{"highest priority override wins", stats.Def{Base: 10}, []stats.Modifier{override(1, 5), override(2, 9), override(3, 1)}, 2},
		{"latest override wins a tie", stats.Def{Base: 10}, []stats.Modifier{override(1, 4), override(2, 4), override(3, 0)}, 2},
		{"max clamps", stats.Def{Base: 10, Max: stats.Float(25)}, []stats.Modifier{mul(3)}, 25},
		{"min clamps", stats.Def{Base: 10, Min: stats.Float(0)}, []stats.Modifier{add(-50)}, 0},
		{"bounds clamp an override", stats.Def{Base: 10, Min: stats.Float(1), Max: stats.Float(5)}, []stats.Modifier{override(99, 0)}, 5},
		{"bounds clamp the base", stats.Def{Base: -3, Min: stats.Float(0)}, nil, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.def.Name = "x"
			s := newSheet(t, tc.def)
			for _, m := range tc.mods {
				mustAdd(t, s, "src", m)
			}
			if got := s.Value("x"); got != tc.want {
				t.Fatalf("Value = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTimedModifierExpiresOnExactStep(t *testing.T) {
	s := newSheet(t, stats.Def{Name: "speed", Base: 5})
	var changes []stats.Change
	var changedAt []uint64
	s.OnChange = func(c stats.Change) {
		changes = append(changes, c)
		changedAt = append(changedAt, s.Steps())
	}

	haste := mustAddTimed(t, s, "buff:haste", stats.Modifier{Stat: "speed", Op: stats.OpMul, Value: 2}, 3)
	slow := mustAddTimed(t, s, "debuff:slow", stats.Modifier{Stat: "speed", Op: stats.OpAdd, Value: -1}, 1)
	gear := mustAdd(t, s, "equip:boots", stats.Modifier{Stat: "speed", Op: stats.OpAdd, Value: 1})
	if got := s.Value("speed"); got != 10 {
		t.Fatalf("speed = %v, want (5-1+1)*2 = 10", got)
	}

	s.Step() // slow's only step.
	if _, ok := s.Remaining(slow); ok {
		t.Fatal("slow still on the sheet after its 1 step")
	}
	if got := s.Value("speed"); got != 12 {
		t.Fatalf("after step 1 speed = %v, want 12", got)
	}
	s.Step()
	if r, ok := s.Remaining(haste); !ok || r != 1 {
		t.Fatalf("haste Remaining = %d, %v; want 1, true", r, ok)
	}
	if got := s.Value("speed"); got != 12 {
		t.Fatalf("after step 2 speed = %v, want 12", got)
	}
	s.Step() // haste's third and last step.
	if _, ok := s.Remaining(haste); ok {
		t.Fatal("haste still on the sheet after its 3 steps")
	}
	if got := s.Value("speed"); got != 6 {
		t.Fatalf("after step 3 speed = %v, want 6", got)
	}
	for range 100 {
		s.Step()
	}
	if r, ok := s.Remaining(gear); !ok || r != 0 {
		t.Fatalf("untimed gear Remaining = %d, %v; want 0, true", r, ok)
	}

	wantChanges := []stats.Change{
		{Stat: "speed", Old: 5, New: 10},
		{Stat: "speed", Old: 10, New: 8},
		{Stat: "speed", Old: 8, New: 10},
		{Stat: "speed", Old: 10, New: 12},
		{Stat: "speed", Old: 12, New: 6},
	}
	if !slices.Equal(changes, wantChanges) {
		t.Fatalf("changes = %+v, want %+v", changes, wantChanges)
	}
	if want := []uint64{0, 0, 0, 1, 3}; !slices.Equal(changedAt, want) {
		t.Fatalf("changes reported at steps %v, want %v", changedAt, want)
	}
}

func TestTimedModifierExpiresOnLoopStep(t *testing.T) {
	s := newSheet(t, stats.Def{Name: "armor", Base: 10})
	step := 0
	expiredAt := 0
	s.OnChange = func(c stats.Change) {
		if c.New == 10 {
			expiredAt = step
		}
	}
	mustAddTimed(t, s, "buff:shield", stats.Modifier{Stat: "armor", Op: stats.OpAdd, Value: 5}, 4)

	l := &game.Loop{Rate: 50, Update: func(time.Duration) {
		step++
		s.Step()
	}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})

	// 50 Hz is 20 ms a step: 60 ms runs 3 steps and the shield holds.
	app.Advance(60 * time.Millisecond)
	if step != 3 || s.Value("armor") != 15 || expiredAt != 0 {
		t.Fatalf("after 3 steps: step=%d armor=%v expiredAt=%d", step, s.Value("armor"), expiredAt)
	}
	// While the loop is paused no step runs, so the shield does not run down.
	l.SetPaused(true)
	app.Advance(200 * time.Millisecond)
	if step != 3 || s.Value("armor") != 15 {
		t.Fatalf("while paused: step=%d armor=%v", step, s.Value("armor"))
	}
	l.SetPaused(false)
	app.Advance(40 * time.Millisecond)
	if expiredAt != 4 || s.Value("armor") != 10 {
		t.Fatalf("shield expired at step %d with armor %v, want step 4 and 10", expiredAt, s.Value("armor"))
	}
}

func TestSourcesAndRemoval(t *testing.T) {
	s := newSheet(t, stats.Def{Name: "attack", Base: 10}, stats.Def{Name: "health", Base: 100})
	sword, err := s.Apply("equip:weapon", stats.Effect{Name: "sword", Modifiers: []stats.Modifier{
		{Stat: "attack", Op: stats.OpAdd, Value: 8},
		{Stat: "health", Op: stats.OpAdd, Value: 20},
	}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	ring := mustAdd(t, s, "equip:ring", stats.Modifier{Stat: "attack", Op: stats.OpMul, Value: 1.5})
	if s.Value("attack") != 27 || s.Value("health") != 120 {
		t.Fatalf("attack=%v health=%v, want 27 and 120", s.Value("attack"), s.Value("health"))
	}

	mods := s.Modifiers()
	if len(mods) != 3 || mods[0].ID != sword[0] || mods[1].ID != sword[1] || mods[2].ID != ring || mods[2].Source != "equip:ring" {
		t.Fatalf("Modifiers = %+v", mods)
	}

	if n := s.RemoveSource("equip:weapon"); n != 2 {
		t.Fatalf("RemoveSource = %d, want 2", n)
	}
	if s.Value("attack") != 15 || s.Value("health") != 100 {
		t.Fatalf("after unequip attack=%v health=%v, want 15 and 100", s.Value("attack"), s.Value("health"))
	}
	if n := s.RemoveSource("equip:weapon"); n != 0 {
		t.Fatalf("second RemoveSource = %d, want 0", n)
	}
	if !s.Remove(ring) || s.Remove(ring) || s.Remove(0) {
		t.Fatal("Remove should succeed once and then report false")
	}
	if s.Value("attack") != 10 {
		t.Fatalf("attack = %v, want base 10", s.Value("attack"))
	}

	mustAdd(t, s, "a", stats.Modifier{Stat: "attack", Op: stats.OpAdd, Value: 1})
	next := mustAdd(t, s, "b", stats.Modifier{Stat: "attack", Op: stats.OpAdd, Value: 1})
	if next <= ring {
		t.Fatalf("IDs were reused: %d after %d", next, ring)
	}
	s.Clear()
	if len(s.Modifiers()) != 0 || s.Value("attack") != 10 {
		t.Fatalf("after Clear: %d modifiers, attack %v", len(s.Modifiers()), s.Value("attack"))
	}
}

func TestApplyStacksAndRefresh(t *testing.T) {
	s := newSheet(t, stats.Def{Name: "attack", Base: 10})
	rage := stats.Effect{Name: "rage", Steps: 2, Modifiers: []stats.Modifier{{Stat: "attack", Op: stats.OpAdd, Value: 5}}}
	for range 2 {
		if _, err := s.Apply("buff:rage", rage); err != nil {
			t.Fatal(err)
		}
	}
	if s.Value("attack") != 20 {
		t.Fatalf("two stacks: attack = %v, want 20", s.Value("attack"))
	}
	s.Step()
	// Refresh: drop the old stacks and apply once more for a full duration.
	s.RemoveSource("buff:rage")
	if _, err := s.Apply("buff:rage", rage); err != nil {
		t.Fatal(err)
	}
	s.Step()
	if s.Value("attack") != 15 {
		t.Fatalf("refreshed after 1 step: attack = %v, want 15", s.Value("attack"))
	}
	s.Step()
	if s.Value("attack") != 10 {
		t.Fatalf("refreshed after 2 steps: attack = %v, want 10", s.Value("attack"))
	}
}

func TestChangesReportedOncePerStatInDefinitionOrder(t *testing.T) {
	s := newSheet(t, stats.Def{Name: "a", Base: 1}, stats.Def{Name: "b", Base: 1}, stats.Def{Name: "c", Base: 1})
	var got []stats.Change
	s.OnChange = func(c stats.Change) { got = append(got, c) }
	_, err := s.Apply("x", stats.Effect{Name: "e", Modifiers: []stats.Modifier{
		{Stat: "c", Op: stats.OpAdd, Value: 1},
		{Stat: "a", Op: stats.OpAdd, Value: 1},
		{Stat: "a", Op: stats.OpMul, Value: 3},
		{Stat: "b", Op: stats.OpMul, Value: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []stats.Change{{Stat: "a", Old: 1, New: 6}, {Stat: "c", Old: 1, New: 2}}
	if !slices.Equal(got, want) {
		t.Fatalf("changes = %+v, want %+v (b did not change)", got, want)
	}

	got = nil
	if err := s.SetBase("b", 4); err != nil {
		t.Fatal(err)
	}
	if s.Base("b") != 4 || s.Value("b") != 4 || !slices.Equal(got, []stats.Change{{Stat: "b", Old: 1, New: 4}}) {
		t.Fatalf("SetBase: base=%v value=%v changes=%+v", s.Base("b"), s.Value("b"), got)
	}
}

func TestOnChangeMayChangeTheSheet(t *testing.T) {
	s := newSheet(t, stats.Def{Name: "health", Base: 100}, stats.Def{Name: "maxHealth", Base: 100})
	var order []string
	s.OnChange = func(c stats.Change) {
		order = append(order, c.Stat)
		// Keep health equal to maxHealth's bonus: an effect that cascades.
		if c.Stat == "maxHealth" {
			s.RemoveSource("sync")
			if _, err := s.Add("sync", stats.Modifier{Stat: "health", Op: stats.OpAdd, Value: c.New - 100}); err != nil {
				t.Error(err)
			}
		}
	}
	mustAdd(t, s, "gear", stats.Modifier{Stat: "maxHealth", Op: stats.OpAdd, Value: 25})
	if s.Value("health") != 125 {
		t.Fatalf("health = %v, want 125", s.Value("health"))
	}
	if !slices.Equal(order, []string{"maxHealth", "health"}) {
		t.Fatalf("order = %v", order)
	}
}

func TestSheetErrors(t *testing.T) {
	if _, err := stats.NewSheet(stats.Def{Name: "a"}, stats.Def{Name: "a"}); !errors.Is(err, stats.ErrDuplicateStat) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := stats.NewSheet(stats.Def{Name: "a", Min: stats.Float(2), Max: stats.Float(1)}); err == nil {
		t.Fatal("min above max accepted")
	}
	if _, err := stats.NewSheet(stats.Def{}); err == nil {
		t.Fatal("empty name accepted")
	}
	s := newSheet(t, stats.Def{Name: "a", Base: 1})
	cases := []struct {
		m     stats.Modifier
		steps int
		want  error
	}{
		{stats.Modifier{Stat: "b", Op: stats.OpAdd, Value: 1}, 0, stats.ErrUnknownStat},
		{stats.Modifier{Stat: "a", Op: "pow", Value: 1}, 0, stats.ErrInvalidOp},
		{stats.Modifier{Stat: "a", Op: stats.OpAdd, Value: inf()}, 0, stats.ErrInvalidValue},
		{stats.Modifier{Stat: "a", Op: stats.OpAdd, Value: 1}, -1, stats.ErrInvalidSteps},
	}
	for _, tc := range cases {
		if _, err := s.AddTimed("x", tc.m, tc.steps); !errors.Is(err, tc.want) {
			t.Fatalf("AddTimed(%+v, %d) = %v, want %v", tc.m, tc.steps, err, tc.want)
		}
	}
	// Apply is all or nothing.
	_, err := s.Apply("x", stats.Effect{Name: "bad", Modifiers: []stats.Modifier{
		{Stat: "a", Op: stats.OpAdd, Value: 1},
		{Stat: "missing", Op: stats.OpAdd, Value: 1},
	}})
	if !errors.Is(err, stats.ErrUnknownStat) || len(s.Modifiers()) != 0 || s.Value("a") != 1 {
		t.Fatalf("partial Apply: err=%v modifiers=%d a=%v", err, len(s.Modifiers()), s.Value("a"))
	}
	if err := s.SetBase("missing", 1); !errors.Is(err, stats.ErrUnknownStat) {
		t.Fatalf("SetBase unknown: %v", err)
	}
	if err := s.SetBase("a", inf()); !errors.Is(err, stats.ErrInvalidValue) {
		t.Fatalf("SetBase inf: %v", err)
	}
	if v, ok := s.Lookup("missing"); ok || v != 0 || s.Value("missing") != 0 || s.Has("missing") {
		t.Fatal("unknown stat should read as missing")
	}
	if !slices.Equal(s.Stats(), []string{"a"}) {
		t.Fatalf("Stats = %v", s.Stats())
	}
}

func TestZeroSheetIsUsable(t *testing.T) {
	var s stats.Sheet
	if err := s.Define(stats.Def{Name: "luck", Base: 3}); err != nil {
		t.Fatal(err)
	}
	mustAdd(t, &s, "charm", stats.Modifier{Stat: "luck", Op: stats.OpAdd, Value: 2})
	if s.Value("luck") != 5 {
		t.Fatalf("luck = %v", s.Value("luck"))
	}
}

func TestDefineCopiesBounds(t *testing.T) {
	maxV := 10.0
	s := newSheet(t, stats.Def{Name: "a", Base: 50, Max: &maxV})
	maxV = 100
	if err := s.SetBase("a", 60); err != nil {
		t.Fatal(err)
	}
	if s.Value("a") != 10 {
		t.Fatalf("a = %v, want 10: the sheet must keep its own copy of Max", s.Value("a"))
	}
}

func TestParseFileAndBuildSheet(t *testing.T) {
	data, err := os.ReadFile("testdata/hero.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f, err := stats.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := stats.Encode(f)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(out) != string(data) {
		t.Fatalf("round trip changed the file:\n%s\nwant:\n%s", out, data)
	}

	s, err := f.NewSheet()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Stats(), []string{"health", "attack", "speed"}) {
		t.Fatalf("Stats = %v", s.Stats())
	}
	apply := func(source, name string) {
		t.Helper()
		e, ok := f.Effect(name)
		if !ok {
			t.Fatalf("effect %q missing", name)
		}
		if _, err := s.Apply(source, e); err != nil {
			t.Fatal(err)
		}
	}
	apply("equip:weapon", "iron-sword")
	apply("buff:berserk", "berserk")
	if s.Value("attack") != 30 || s.Value("speed") != 7 {
		t.Fatalf("attack=%v speed=%v, want 30 and 7", s.Value("attack"), s.Value("speed"))
	}
	apply("debuff:frozen", "frozen")
	if s.Value("speed") != 0 {
		t.Fatalf("frozen speed = %v, want 0", s.Value("speed"))
	}
	for range 90 {
		s.Step()
	}
	if s.Value("speed") != 7 {
		t.Fatalf("speed after frozen = %v, want 7", s.Value("speed"))
	}
	for range 210 {
		s.Step()
	}
	if s.Value("attack") != 20 || s.Value("speed") != 5 {
		t.Fatalf("after berserk: attack=%v speed=%v, want 20 and 5", s.Value("attack"), s.Value("speed"))
	}
	if _, ok := f.Effect("missing"); ok {
		t.Fatal("Effect found a missing name")
	}
}

func TestEncodeIsDeterministic(t *testing.T) {
	f := &stats.File{
		Version: stats.CurrentVersion,
		Stats:   []stats.Def{{Name: "b", Base: 0.1}, {Name: "a", Base: -2, Min: stats.Float(-5)}},
		Effects: []stats.Effect{{Name: "z", Steps: 3, Modifiers: []stats.Modifier{{Stat: "a", Op: stats.OpOverride, Value: 1e21, Priority: -1}}}},
	}
	first, err := stats.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		again, err := stats.Encode(f)
		if err != nil || string(again) != string(first) {
			t.Fatalf("Encode differs: %v\n%s\n%s", err, again, first)
		}
	}
	back, err := stats.Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	if back.Stats[0].Base != 0.1 || *back.Stats[1].Min != -5 || back.Effects[0].Modifiers[0].Value != 1e21 || back.Effects[0].Modifiers[0].Priority != -1 {
		t.Fatalf("round trip lost values: %+v", back)
	}
}

func TestParseRefusesInvalidFiles(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"no version":      "stats: []\n",
		"future version":  "version: 2\nstats: []\n",
		"unknown field":   "version: 1\nstats:\n  - name: a\n    base: 1\n    color: red\n",
		"unknown root":    "version: 1\nstats: []\nextra: 1\n",
		"duplicate stat":  "version: 1\nstats:\n  - name: a\n    base: 1\n  - name: a\n    base: 2\n",
		"empty stat name": "version: 1\nstats:\n  - base: 1\n",
		"nan base":        "version: 1\nstats:\n  - name: a\n    base: .nan\n",
		"min above max":   "version: 1\nstats:\n  - name: a\n    base: 1\n    min: 3\n    max: 2\n",
		"unknown op":      "version: 1\nstats:\n  - name: a\n    base: 1\neffects:\n  - name: e\n    modifiers:\n      - stat: a\n        op: pow\n        value: 2\n",
		"unknown stat":    "version: 1\nstats:\n  - name: a\n    base: 1\neffects:\n  - name: e\n    modifiers:\n      - stat: b\n        op: add\n        value: 2\n",
		"inf value":       "version: 1\nstats:\n  - name: a\n    base: 1\neffects:\n  - name: e\n    modifiers:\n      - stat: a\n        op: add\n        value: .inf\n",
		"negative steps":  "version: 1\nstats:\n  - name: a\n    base: 1\neffects:\n  - name: e\n    steps: -1\n    modifiers:\n      - stat: a\n        op: add\n        value: 2\n",
		"no modifiers":    "version: 1\nstats:\n  - name: a\n    base: 1\neffects:\n  - name: e\n    modifiers: []\n",
		"duplicate effect": "version: 1\nstats:\n  - name: a\n    base: 1\neffects:\n  - name: e\n    modifiers:\n      - stat: a\n        op: add\n        value: 2\n" +
			"  - name: e\n    modifiers:\n      - stat: a\n        op: add\n        value: 2\n",
		"trailing document": "version: 1\nstats: []\n---\nversion: 1\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if f, err := stats.Parse([]byte(src)); err == nil {
				t.Fatalf("Parse accepted %q as %+v", src, f)
			} else if !strings.HasPrefix(err.Error(), "stats: ") {
				t.Fatalf("error %q lacks the package prefix", err)
			}
		})
	}
	if _, err := stats.Encode(&stats.File{Version: 7}); err == nil {
		t.Fatal("Encode accepted an invalid file")
	}
}

func inf() float64 { return math.Inf(1) }

func BenchmarkStep(b *testing.B) {
	s, err := stats.NewSheet(stats.Def{Name: "attack", Base: 10}, stats.Def{Name: "speed", Base: 5})
	if err != nil {
		b.Fatal(err)
	}
	for i := range 32 {
		if _, err := s.Add("gear", stats.Modifier{Stat: "attack", Op: stats.OpAdd, Value: float64(i)}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.AddTimed("buff", stats.Modifier{Stat: "speed", Op: stats.OpMul, Value: 1.1}, 1); err != nil {
			b.Fatal(err)
		}
		s.Step()
	}
}
