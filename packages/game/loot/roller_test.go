package loot_test

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/packages/game/loot"
	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func mustLibrary(t testing.TB, src string) *loot.Library {
	t.Helper()
	doc, err := loot.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	lib, err := loot.New(doc)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return lib
}

func mustRoll(t testing.TB, r *loot.Roller, table string) []loot.Drop {
	t.Helper()
	drops, err := r.Roll(table)
	if err != nil {
		t.Fatalf("Roll(%q): %v", table, err)
	}
	return drops
}

// chiSquare returns Pearson's statistic for observed counts against expected probabilities over n trials.
func chiSquare(observed []int, probs []float64, n int) float64 {
	var x float64
	for i, p := range probs {
		e := p * float64(n)
		d := float64(observed[i]) - e
		x += d * d / e
	}
	return x
}

// chiCritical holds the chi-square values with a 0.1% upper tail for 1..6 degrees of freedom.
var chiCritical = []float64{0, 10.828, 13.816, 16.266, 18.467, 20.515, 22.458}

const weightedDoc = `
version: 1
tables:
  - name: chest
    entries:
      - item: sword
        weight: 50
      - item: shield
        weight: 30
      - item: ring
        weight: 15
      - item: crown
        weight: 4
      - nothing: true
        weight: 1
`

func TestDropsFollowWeights(t *testing.T) {
	lib := mustLibrary(t, weightedDoc)
	probs := lib.Chances("chest")
	want := []float64{0.5, 0.3, 0.15, 0.04, 0.01}
	for i := range want {
		if math.Abs(probs[i]-want[i]) > 1e-12 {
			t.Fatalf("Chances = %v, want %v", probs, want)
		}
	}
	index := map[string]int{"sword": 0, "shield": 1, "ring": 2, "crown": 3}
	const n = 200_000
	for _, seed := range []uint64{1, 7, 42, 519, 0xdeadbeef} {
		r := loot.NewRoller(lib, random.New(seed).Fork("loot"))
		observed := make([]int, len(want))
		var buf []loot.Drop
		for range n {
			var err error
			buf, err = r.AppendRoll(buf[:0], "chest")
			if err != nil {
				t.Fatal(err)
			}
			switch len(buf) {
			case 0:
				observed[4]++
			case 1:
				if buf[0].Count != 1 {
					t.Fatalf("count = %d, want 1", buf[0].Count)
				}
				observed[index[buf[0].Item]]++
			default:
				t.Fatalf("one pick produced %d drops", len(buf))
			}
		}
		if x := chiSquare(observed, want, n); x > chiCritical[len(want)-1] {
			t.Errorf("seed %d: chi-square %.2f over %v exceeds %.2f", seed, x, observed, chiCritical[len(want)-1])
		}
	}
}

const nestedDoc = `
version: 1
tables:
  - name: boss
    rolls: 2
    guaranteed:
      - item: gold
        count: {min: 10, max: 20}
      - table: gems
    entries:
      - table: common
        weight: 3
      - table: rare
        weight: 1
  - name: common
    entries:
      - item: potion
        weight: 1
      - item: arrow
        weight: 1
        count: 5
  - name: rare
    entries:
      - item: amulet
        weight: 1
  - name: gems
    rolls: {min: 0, max: 2}
    entries:
      - item: ruby
        weight: 1
      - item: opal
        weight: 1
`

func TestNestedTablesFollowCombinedWeights(t *testing.T) {
	lib := mustLibrary(t, nestedDoc)
	r := loot.NewRoller(lib, random.New(3))
	// boss picks common (3/4) or rare (1/4); common splits evenly: potion 3/8, arrow 3/8, amulet 1/4.
	want := []float64{3.0 / 8, 3.0 / 8, 1.0 / 4}
	index := map[string]int{"potion": 0, "arrow": 1, "amulet": 2}
	observed := make([]int, 3)
	gemRolls := make([]int, 3)
	const n = 100_000
	for range n {
		drops := mustRoll(t, r, "boss")
		if drops[0].Item != "gold" || drops[0].Count < 10 || drops[0].Count > 20 {
			t.Fatalf("first drop %+v, want 10..20 gold", drops[0])
		}
		gems := 0
		picks := 0
		for _, d := range drops[1:] {
			switch d.Item {
			case "ruby", "opal":
				if picks > 0 {
					t.Fatalf("guaranteed gems after weighted picks: %+v", drops)
				}
				gems++
			case "arrow":
				if d.Count != 5 {
					t.Fatalf("arrow count %d, want 5", d.Count)
				}
				fallthrough
			default:
				observed[index[d.Item]]++
				picks++
			}
		}
		if picks != 2 {
			t.Fatalf("boss made %d picks, want 2: %+v", picks, drops)
		}
		gemRolls[gems]++
	}
	if x := chiSquare(observed, want, 2*n); x > chiCritical[2] {
		t.Errorf("chi-square %.2f over %v exceeds %.2f", x, observed, chiCritical[2])
	}
	if x := chiSquare(gemRolls, []float64{1.0 / 3, 1.0 / 3, 1.0 / 3}, n); x > chiCritical[2] {
		t.Errorf("gem rolls %v are not uniform over 0..2 (chi-square %.2f)", gemRolls, x)
	}
}

const hardPityDoc = `
version: 1
tables:
  - name: banner
    entries:
      - table: five-star
        weight: 0.000001
      - table: four-star
        weight: 0.000001
      - item: three-star
        weight: 1000
    pity:
      - name: five
        table: five-star
        guaranteeAt: 90
      - name: four
        table: four-star
        guaranteeAt: 10
  - name: five-star
    entries:
      - item: hero
        weight: 1
  - name: four-star
    entries:
      - item: sidekick
        weight: 1
`

func TestHardPityForcesTheTargetOnSchedule(t *testing.T) {
	lib := mustLibrary(t, hardPityDoc)
	r := loot.NewRoller(lib, random.New(11))
	fourAt := 10
	for pull := 1; pull <= 900; pull++ {
		drops := mustRoll(t, r, "banner")
		if len(drops) != 1 {
			t.Fatalf("pull %d: %d drops", pull, len(drops))
		}
		var want string
		switch {
		case pull%90 == 0:
			want = "hero"
		case pull == fourAt:
			want = "sidekick"
		default:
			want = "three-star"
		}
		if drops[0].Item != want {
			t.Fatalf("pull %d: got %q, want %q", pull, drops[0].Item, want)
		}
		if want == "sidekick" {
			fourAt += 10
		} else if pull == fourAt {
			// The five-star rule came first in order; the four-star rule keeps counting and forces the next pull.
			fourAt++
		}
		if pull%90 == 0 {
			if n, _ := r.Misses("banner", "five"); n != 0 {
				t.Fatalf("pull %d: five misses %d after the guarantee", pull, n)
			}
		}
	}
	if n, ok := r.Misses("banner", "four"); !ok || n >= 10 {
		t.Fatalf("Misses(four) = %d, %v", n, ok)
	}
}

func TestHardPityBoundsTheLongestDrought(t *testing.T) {
	lib := mustLibrary(t, `
version: 1
tables:
  - name: t
    entries:
      - item: rare
        weight: 1
      - item: common
        weight: 9
    pity:
      - name: rare
        item: rare
        guaranteeAt: 5
`)
	r := loot.NewRoller(lib, random.New(5))
	gaps := make([]int, 6)
	gap := 0
	const n = 100_000
	for range n {
		gap++
		if mustRoll(t, r, "t")[0].Item == "rare" {
			gaps[gap]++
			gap = 0
		}
	}
	if gaps[0] != 0 {
		t.Fatal("impossible zero gap")
	}
	// The first four pulls hit with probability 0.1 each; the fifth is forced.
	want := []float64{0.1, 0.09, 0.081, 0.0729, 0.6561}
	total := 0
	for _, g := range gaps[1:] {
		total += g
	}
	if x := chiSquare(gaps[1:], want, total); x > chiCritical[4] {
		t.Errorf("gap distribution %v (chi-square %.2f) does not match %v", gaps[1:], x, want)
	}
}

func TestSoftPityRampsTheTargetWeight(t *testing.T) {
	lib := mustLibrary(t, `
version: 1
tables:
  - name: t
    entries:
      - item: rare
        weight: 1
      - item: common
        weight: 99
    pity:
      - name: rare
        item: rare
        softFrom: 6
        softStep: 20
`)
	r := loot.NewRoller(lib, random.New(8))
	const maxPick = 10
	reached := make([]int, maxPick+1)
	hits := make([]int, maxPick+1)
	pick := 0
	for range 400_000 {
		pick++
		hit := mustRoll(t, r, "t")[0].Item == "rare"
		if pick <= maxPick {
			reached[pick]++
			if hit {
				hits[pick]++
			}
		}
		if hit {
			pick = 0
		}
	}
	for n := 1; n <= maxPick; n++ {
		ramp := 0.0
		if n >= 6 {
			ramp = float64(n-6+1) * 20
		}
		p := (1 + ramp) / (100 + ramp)
		got := float64(hits[n]) / float64(reached[n])
		se := math.Sqrt(p * (1 - p) / float64(reached[n]))
		if z := math.Abs(got-p) / se; z > 4.5 {
			t.Errorf("pick %d: hit rate %.4f over %d, want %.4f (z=%.2f)", n, got, reached[n], p, z)
		}
	}
}

func TestSoftAndHardPityTogether(t *testing.T) {
	lib := mustLibrary(t, `
version: 1
tables:
  - name: t
    entries:
      - item: rare
        weight: 0.000001
      - item: common
        weight: 1
    pity:
      - name: rare
        item: rare
        softFrom: 3
        softStep: 0.000001
        guaranteeAt: 4
`)
	r := loot.NewRoller(lib, random.New(1))
	for pull := 1; pull <= 400; pull++ {
		got := mustRoll(t, r, "t")[0].Item
		if want := pull%4 == 0; (got == "rare") != want {
			t.Fatalf("pull %d: got %q", pull, got)
		}
	}
}

func TestStateResumesTheSameDrops(t *testing.T) {
	lib := mustLibrary(t, hardPityDoc+`
  - name: boss
    rolls: {min: 1, max: 3}
    entries:
      - table: banner
        weight: 1
        count: {min: 1, max: 2}
      - nothing: true
        weight: 1
`)
	run := func(split int) []loot.Drop {
		r := loot.NewRoller(lib, random.New(77))
		var out []loot.Drop
		for i := range 300 {
			if i == split {
				data, err := json.Marshal(struct {
					Loot   loot.State
					Stream random.State
				}{r.State(), r.Stream().State()})
				if err != nil {
					t.Fatal(err)
				}
				var saved struct {
					Loot   loot.State
					Stream random.State
				}
				if err := json.Unmarshal(data, &saved); err != nil {
					t.Fatal(err)
				}
				stream := random.New(0)
				stream.SetState(saved.Stream)
				r = loot.NewRoller(lib, stream)
				if err := r.SetState(saved.Loot); err != nil {
					t.Fatal(err)
				}
			}
			out = append(out, mustRoll(t, r, "boss")...)
		}
		return out
	}
	whole := run(-1)
	for _, split := range []int{0, 1, 89, 150, 299} {
		if got := run(split); !reflect.DeepEqual(got, whole) {
			t.Fatalf("saving and restoring at roll %d changed the drops", split)
		}
	}
	random.AssertDeterministic(t, 3, func() []loot.Drop { return run(-1) })
}

func TestStateContents(t *testing.T) {
	lib := mustLibrary(t, hardPityDoc)
	r := loot.NewRoller(lib, random.New(2))
	if st := r.State(); st.Pity != nil {
		t.Fatalf("fresh State = %+v, want empty", st)
	}
	for range 13 {
		mustRoll(t, r, "banner")
	}
	want := loot.State{Pity: map[string]map[string]int{"banner": {"five": 13, "four": 3}}}
	if got := r.State(); !reflect.DeepEqual(got, want) {
		t.Fatalf("State = %+v, want %+v", got, want)
	}
	data, err := json.Marshal(r.State())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"pity":{"banner":{"five":13,"four":3}}}` {
		t.Fatalf("State JSON = %s", data)
	}

	r2 := loot.NewRoller(lib, random.New(2))
	extra := loot.State{Pity: map[string]map[string]int{"banner": {"five": 40, "removed": 3}, "gone": {"x": 1}}}
	if err := r2.SetState(extra); err != nil {
		t.Fatalf("SetState with stale keys: %v", err)
	}
	if got := r2.State(); !reflect.DeepEqual(got, loot.State{Pity: map[string]map[string]int{"banner": {"five": 40}}}) {
		t.Fatalf("State after SetState = %+v", got)
	}
	if err := r2.SetState(loot.State{Pity: map[string]map[string]int{"banner": {"five": -1}}}); err == nil {
		t.Fatal("SetState accepted a negative counter")
	}
	if n, _ := r2.Misses("banner", "five"); n != 40 {
		t.Fatalf("a refused SetState changed the counters: %d", n)
	}
	if err := r2.SetState(loot.State{Pity: map[string]map[string]int{"banner": {"five": loot.MaxMisses + 1}}}); err == nil {
		t.Fatal("SetState accepted a counter above MaxMisses")
	}
	r2.ResetPity()
	if st := r2.State(); st.Pity != nil {
		t.Fatalf("State after ResetPity = %+v", st)
	}
	if _, ok := r2.Misses("banner", "nope"); ok {
		t.Fatal("Misses found an unknown rule")
	}
	if _, ok := r2.Misses("nope", "five"); ok {
		t.Fatal("Misses found an unknown table")
	}
}

func TestUnknownTable(t *testing.T) {
	lib := mustLibrary(t, weightedDoc)
	stream := random.New(9)
	r := loot.NewRoller(lib, stream)
	before := stream.State()
	dst := []loot.Drop{{Item: "kept", Count: 1}}
	got, err := r.AppendRoll(dst, "missing")
	if !errors.Is(err, loot.ErrUnknownTable) {
		t.Fatalf("err = %v, want ErrUnknownTable", err)
	}
	if len(got) != 1 || stream.State() != before {
		t.Fatal("a failed roll changed dst or drew from the stream")
	}
	if !lib.Has("chest") || lib.Has("missing") {
		t.Fatal("Has is wrong")
	}
	if names := lib.Tables(); !reflect.DeepEqual(names, []string{"chest"}) {
		t.Fatalf("Tables = %v", names)
	}
	if lib.Chances("missing") != nil {
		t.Fatal("Chances of an unknown table")
	}
}

func TestNewRollerPanicsOnNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewRoller(nil, nil) did not panic")
		}
	}()
	loot.NewRoller(nil, nil)
}

func TestAppendRollDoesNotAllocate(t *testing.T) {
	lib := mustLibrary(t, hardPityDoc)
	r := loot.NewRoller(lib, random.New(4))
	buf := make([]loot.Drop, 0, 16)
	allocs := testing.AllocsPerRun(1000, func() {
		buf, _ = r.AppendRoll(buf[:0], "banner")
	})
	if allocs != 0 {
		t.Fatalf("AppendRoll allocated %.1f times per roll", allocs)
	}
}

func BenchmarkRollNestedWithPity(b *testing.B) {
	lib := mustLibrary(b, hardPityDoc+`
  - name: ten-pull
    entries:
      - table: banner
        weight: 1
        count: 10
`)
	r := loot.NewRoller(lib, random.New(1))
	buf := make([]loot.Drop, 0, 16)
	b.ReportAllocs()
	for b.Loop() {
		buf, _ = r.AppendRoll(buf[:0], "ten-pull")
	}
}
