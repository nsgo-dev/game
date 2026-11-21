package random_test

import (
	"encoding/json"
	"fmt"
	"math"
	mathrand "math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestGoldenSequences(t *testing.T) {
	// Verify SplitMix64(0) canonical seeding vector.
	wantInit0 := [4]uint64{
		0xe220a8397b1dcdaf,
		0x6e789e6aa1b965f4,
		0x06c45d188009454f,
		0xf88bb8a8724c81ec,
	}
	if got := random.New(0).State().S; got != wantInit0 {
		t.Fatalf("New(0).State().S = %#x, want %#x", got, wantInit0)
	}

	u64Cases := []struct {
		seed uint64
		want []uint64
	}{
		{
			seed: 0,
			want: []uint64{
				0x99ec5f36cb75f2b4,
				0xbf6e1f784956452a,
				0x1a5f849d4933e6e0,
				0x6aa594f1262d2d2c,
				0xbba5ad4a1f842e59,
				0xffef8375d9ebcaca,
			},
		},
		{
			seed: 42,
			want: []uint64{
				0x15780b2e0c2ec716,
				0x6104d9866d113a7e,
				0xae17533239e499a1,
				0xecb8ad4703b360a1,
				0xfde6dc7fe2ec5e64,
				0xc50da53101795238,
			},
		},
		{
			seed: 123456789,
			want: []uint64{
				0xd1eea10c836f0cc2,
				0xe1bb9dfa08f02548,
				0x1503f3b726a1b888,
				0x88bf5a022cf9d5c2,
				0xde0f231c26906fe1,
				0x7bf14df7468f6bd5,
			},
		},
	}

	for _, tc := range u64Cases {
		t.Run(fmt.Sprintf("seed_%d", tc.seed), func(t *testing.T) {
			s := random.New(tc.seed)
			for i, w := range tc.want {
				if got := s.Uint64(); got != w {
					t.Fatalf("draw %d: got 0x%016x, want 0x%016x", i, got, w)
				}
			}
		})
	}

	// Verify Uint32 uses upper 32 bits of Uint64.
	s32 := random.New(42)
	wantU32 := []uint32{0x15780b2e, 0x6104d986, 0xae175332, 0xecb8ad47}
	for i, w := range wantU32 {
		if got := s32.Uint32(); got != w {
			t.Fatalf("Uint32[%d] = 0x%08x, want 0x%08x", i, got, w)
		}
	}

	// Verify Intn golden sequence.
	sInt := random.New(42)
	wantInts := []int{8, 37, 68, 92, 99, 76, 71, 85}
	for i, w := range wantInts {
		if got := sInt.Intn(100); got != w {
			t.Fatalf("Intn(100)[%d] = %d, want %d", i, got, w)
		}
	}

	// Verify Float64 exact IEEE-754 bit patterns.
	sF64 := random.New(42)
	wantF64Bits := []uint64{
		0x3fb5780b2e0c2ec0,
		0x3fd84136619b444e,
		0x3fe5c2ea66473c93,
		0x3fed9715a8e0766c,
	}
	for i, w := range wantF64Bits {
		if got := math.Float64bits(sF64.Float64()); got != w {
			t.Fatalf("Float64[%d] bits = 0x%016x, want 0x%016x", i, got, w)
		}
	}

	// Verify Float32 exact IEEE-754 bit patterns.
	sF32 := random.New(42)
	wantF32Bits := []uint32{
		0x3dabc058,
		0x3ec209b2,
		0x3f2e1753,
		0x3f6cb8ad,
	}
	for i, w := range wantF32Bits {
		if got := math.Float32bits(sF32.Float32()); got != w {
			t.Fatalf("Float32[%d] bits = 0x%08x, want 0x%08x", i, got, w)
		}
	}
}

func TestGoldenForksAndIndependence(t *testing.T) {
	forkCases := []struct {
		purpose  string
		wantSeed uint64
		want     []uint64
	}{
		{
			purpose:  "loot",
			wantSeed: 0x38618f15f456066b,
			want: []uint64{
				0x710723ce029c4f72,
				0x5db3df7ddb8acc21,
				0x4937b01aff6ce4ac,
				0x68ca75eab557bbc5,
			},
		},
		{
			purpose:  "ai",
			wantSeed: 0x6aa532823c4f7617,
			want: []uint64{
				0x791326500b9aaaa1,
				0x5b1e591f6d7a15e8,
				0xe3002afd360dae19,
				0xa1c9ebd10267e5ea,
			},
		},
		{
			purpose:  "particles",
			wantSeed: 0xeebb88441523cf06,
			want: []uint64{
				0x02b2eef3bb65ccf7,
				0x1c5bed444902f59f,
				0x6f8aa55a15dae61d,
				0x7799f341711492a9,
			},
		},
		{
			purpose:  "level-1",
			wantSeed: 0x64c4e23475a62443,
			want: []uint64{
				0x61161ce702c85bdd,
				0xa0ed1e880d817581,
				0xadcdbcef190625ff,
				0xce4731b755ae8591,
			},
		},
	}

	root := random.New(42)
	before := root.State()
	for _, tc := range forkCases {
		t.Run(tc.purpose, func(t *testing.T) {
			child := root.Fork(tc.purpose)
			if child.Seed() != tc.wantSeed {
				t.Fatalf("Fork(%q).Seed() = 0x%016x, want 0x%016x", tc.purpose, child.Seed(), tc.wantSeed)
			}
			for i, w := range tc.want {
				if got := child.Uint64(); got != w {
					t.Fatalf("Fork(%q)[%d] = 0x%016x, want 0x%016x", tc.purpose, i, got, w)
				}
			}
		})
	}
	// Forking must never advance or perturb the parent stream.
	if after := root.State(); after != before {
		t.Fatalf("parent state changed after Fork: got %+v, want %+v", after, before)
	}

	// Nested fork golden vector.
	nested := root.Fork("level-1").Fork("loot")
	if nested.Seed() != 0xff11613ce7a7e19b {
		t.Fatalf("nested seed = 0x%016x, want 0xff11613ce7a7e19b", nested.Seed())
	}
	wantNested := []uint64{
		0x324c16728c25401a,
		0x2546614ca352bbcf,
		0xd49f655c49f8f59e,
		0xa7af097d1935cf56,
	}
	for i, w := range wantNested {
		if got := nested.Uint64(); got != w {
			t.Fatalf("nested[%d] = 0x%016x, want 0x%016x", i, got, w)
		}
	}

	// Sibling call order and parent consumption do not affect child streams.
	r1 := random.New(99)
	loot1 := r1.Fork("loot")
	ai1 := r1.Fork("ai")
	// Consume loot1 heavily before reading ai1.
	for range 100 {
		loot1.Uint64()
	}

	r2 := random.New(99)
	for range 50 {
		r2.Uint64()
	}
	ai2 := r2.Fork("ai")
	loot2 := r2.Fork("loot")

	for i := range 16 {
		if a, b := ai1.Uint64(), ai2.Uint64(); a != b {
			t.Fatalf("ai streams diverged at %d: 0x%016x vs 0x%016x", i, a, b)
		}
	}
	// Fresh fork reproduces loot2 from the start.
	loot3 := random.New(99).Fork("loot")
	for i := range 16 {
		if a, b := loot2.Uint64(), loot3.Uint64(); a != b {
			t.Fatalf("loot streams diverged at %d: 0x%016x vs 0x%016x", i, a, b)
		}
	}

	// Hierarchical boundary separation: Fork("a").Fork("b") != Fork("ab"), and Fork("") != root.
	if root.Fork("a").Fork("b").Seed() == root.Fork("ab").Seed() {
		t.Fatal(`Fork("a").Fork("b") collided with Fork("ab")`)
	}
	if root.Fork("").Seed() == root.Seed() {
		t.Fatal(`Fork("") collided with parent seed`)
	}
}

func TestZeroValueStreamMatchesNewZero(t *testing.T) {
	var zero random.Stream
	explicit := random.New(0)
	for i := range 16 {
		if a, b := zero.Uint64(), explicit.Uint64(); a != b {
			t.Fatalf("step %d: zero stream 0x%016x != New(0) 0x%016x", i, a, b)
		}
	}
}

func TestStreamsManagerAndReset(t *testing.T) {
	m := random.NewStreams(42)
	if m.Seed() != 42 {
		t.Fatalf("Seed() = %d, want 42", m.Seed())
	}

	loot := m.Stream("loot")
	ai := m.Stream("ai")
	if m.Stream("loot") != loot {
		t.Fatal("Stream returned a different pointer for the same name")
	}

	var firstLoot, firstAI [8]uint64
	for i := range firstLoot {
		firstLoot[i] = loot.Uint64()
		firstAI[i] = ai.Uint64()
	}

	// Reset rewinds cached pointers in place.
	m.Reset()
	for i := range firstLoot {
		if got := loot.Uint64(); got != firstLoot[i] {
			t.Fatalf("after Reset, loot[%d] = 0x%016x, want 0x%016x", i, got, firstLoot[i])
		}
		if got := ai.Uint64(); got != firstAI[i] {
			t.Fatalf("after Reset, ai[%d] = 0x%016x, want 0x%016x", i, got, firstAI[i])
		}
	}

	// Snapshot and restore via JSON.
	raw, err := json.Marshal(m.States())
	if err != nil {
		t.Fatalf("json.Marshal(States): %v", err)
	}
	var wantLoot, wantAI [8]uint64
	for i := range wantLoot {
		wantLoot[i] = loot.Uint64()
		wantAI[i] = ai.Uint64()
	}

	// Create a brand new manager and restore from JSON.
	m2 := random.NewStreams(42)
	// Also request an extra stream not present in the snapshot to verify it resets.
	extra := m2.Stream("particles")
	firstParticle := extra.Uint64()

	var decoded map[string]random.State
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(States): %v", err)
	}
	m2.Restore(decoded)

	for i := range wantLoot {
		if got := m2.Stream("loot").Uint64(); got != wantLoot[i] {
			t.Fatalf("restored loot[%d] = 0x%016x, want 0x%016x", i, got, wantLoot[i])
		}
		if got := m2.Stream("ai").Uint64(); got != wantAI[i] {
			t.Fatalf("restored ai[%d] = 0x%016x, want 0x%016x", i, got, wantAI[i])
		}
	}
	if got := extra.Uint64(); got != firstParticle {
		t.Fatalf("unlisted stream was not reset: got 0x%016x, want 0x%016x", got, firstParticle)
	}
}

func TestStateJSONRoundTrip(t *testing.T) {
	s := random.New(777)
	for range 25 {
		s.Uint64()
	}

	snap := s.State()
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var restored random.State
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	var clone random.Stream
	clone.SetState(restored)

	if clone.Seed() != s.Seed() {
		t.Fatalf("restored Seed() = %d, want %d", clone.Seed(), s.Seed())
	}
	for i := range 32 {
		if a, b := s.Uint64(), clone.Uint64(); a != b {
			t.Fatalf("step %d after restore: 0x%016x != 0x%016x", i, a, b)
		}
	}
	if s.Fork("loot").Uint64() != clone.Fork("loot").Uint64() {
		t.Fatal("Fork after SetState did not match original")
	}

	// SetState with zero S re-derives state from Seed.
	var fromSeedOnly random.Stream
	fromSeedOnly.SetState(random.State{Seed: 42})
	if a, b := fromSeedOnly.Uint64(), random.New(42).Uint64(); a != b {
		t.Fatalf("SetState with zero S: got 0x%016x, want 0x%016x", a, b)
	}
}

func TestDistributionsAndBounds(t *testing.T) {
	const samples = 20000
	s := random.New(2026)

	// Intn bounds and uniformity.
	var buckets [10]int
	for range samples {
		v := s.Intn(len(buckets))
		if v < 0 || v >= len(buckets) {
			t.Fatalf("Intn out of range: %d", v)
		}
		buckets[v]++
	}
	for i, c := range buckets {
		if c < 1700 || c > 2300 {
			t.Fatalf("Intn bucket %d count %d outside [1700, 2300]", i, c)
		}
	}
	if v := s.Intn(1); v != 0 {
		t.Fatalf("Intn(1) = %d, want 0", v)
	}

	// IntRange inclusive bounds, negative range, equal bounds, full int range.
	seen := make(map[int]bool)
	for range 2000 {
		v := s.IntRange(-3, 3)
		if v < -3 || v > 3 {
			t.Fatalf("IntRange(-3, 3) out of bounds: %d", v)
		}
		seen[v] = true
	}
	for v := -3; v <= 3; v++ {
		if !seen[v] {
			t.Fatalf("IntRange(-3, 3) never produced %d", v)
		}
	}
	if v := s.IntRange(7, 7); v != 7 {
		t.Fatalf("IntRange(7, 7) = %d, want 7", v)
	}
	// Smoke-test extreme full-width integer range without overflow.
	_ = s.IntRange(math.MinInt, math.MaxInt)

	// Float64 in [0, 1).
	var sum64 float64
	for range samples {
		v := s.Float64()
		if v < 0 || v >= 1 || math.IsNaN(v) {
			t.Fatalf("Float64 out of [0, 1): %v", v)
		}
		sum64 += v
	}
	if mean := sum64 / samples; math.Abs(mean-0.5) > 0.02 {
		t.Fatalf("Float64 mean = %v, want ~0.5", mean)
	}

	// Float32 in [0, 1).
	var sum32 float64
	for range samples {
		v := s.Float32()
		if v < 0 || v >= 1 || math.IsNaN(float64(v)) {
			t.Fatalf("Float32 out of [0, 1): %v", v)
		}
		sum32 += float64(v)
	}
	if mean := sum32 / samples; math.Abs(mean-0.5) > 0.02 {
		t.Fatalf("Float32 mean = %v, want ~0.5", mean)
	}

	// Range and Range32.
	for range 5000 {
		v64 := s.Range(-10.5, 2.5)
		if v64 < -10.5 || v64 >= 2.5 {
			t.Fatalf("Range(-10.5, 2.5) out of bounds: %v", v64)
		}
		v32 := s.Range32(-5.0, 5.0)
		if v32 < -5.0 || v32 >= 5.0 {
			t.Fatalf("Range32(-5, 5) out of bounds: %v", v32)
		}
	}
	if v := s.Range(3.25, 3.25); v != 3.25 {
		t.Fatalf("Range(3.25, 3.25) = %v, want 3.25", v)
	}
	if v := s.Range32(3.25, 3.25); v != 3.25 {
		t.Fatalf("Range32(3.25, 3.25) = %v, want 3.25", v)
	}

	// Bool balance.
	trues := 0
	for range samples {
		if s.Bool() {
			trues++
		}
	}
	if trues < 9500 || trues > 10500 {
		t.Fatalf("Bool true count %d outside [9500, 10500]", trues)
	}

	// Chance edge cases and probability.
	for range 100 {
		if s.Chance(0) || s.Chance(-0.5) || s.Chance(math.NaN()) {
			t.Fatal("Chance returned true for p <= 0 or NaN")
		}
		if !s.Chance(1) || !s.Chance(1.5) {
			t.Fatal("Chance returned false for p >= 1")
		}
	}
	hits := 0
	for range samples {
		if s.Chance(0.25) {
			hits++
		}
	}
	if hits < 4600 || hits > 5400 {
		t.Fatalf("Chance(0.25) hits %d outside [4600, 5400]", hits)
	}

	// Pick.
	items := []string{"sword", "shield", "potion", "bow"}
	pickCounts := make(map[string]int, len(items))
	for range samples {
		pickCounts[random.Pick(s, items)]++
	}
	for _, item := range items {
		if c := pickCounts[item]; c < 4600 || c > 5400 {
			t.Fatalf("Pick(%q) count %d outside [4600, 5400]", item, c)
		}
	}

	// WeightedIndex edge cases and distribution.
	if idx := s.WeightedIndex(nil); idx != -1 {
		t.Fatalf("WeightedIndex(nil) = %d, want -1", idx)
	}
	if idx := s.WeightedIndex([]float64{0, 0, 0}); idx != -1 {
		t.Fatalf("WeightedIndex(all zeros) = %d, want -1", idx)
	}
	weights := []float64{0, 1, 0, 2, 7, 0}
	var wCounts [6]int
	for range samples {
		idx := s.WeightedIndex(weights)
		if idx < 0 || idx >= len(weights) {
			t.Fatalf("WeightedIndex out of range: %d", idx)
		}
		wCounts[idx]++
	}
	if wCounts[0] != 0 || wCounts[2] != 0 || wCounts[5] != 0 {
		t.Fatalf("WeightedIndex selected zero-weight entry: %v", wCounts)
	}
	if wCounts[1] < 1700 || wCounts[1] > 2300 || wCounts[3] < 3600 || wCounts[3] > 4400 || wCounts[4] < 13500 || wCounts[4] > 14500 {
		t.Fatalf("WeightedIndex counts %v outside expected [0, ~2000, 0, ~4000, ~14000, 0]", wCounts)
	}

	// Shuffle integrity, determinism, and uniformity.
	random.Shuffle[int](s, nil)
	single := []int{9}
	random.Shuffle(s, single)
	if single[0] != 9 {
		t.Fatalf("Shuffle mutated single-element slice: %v", single)
	}

	a := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	b := slices.Clone(a)
	random.Shuffle(random.New(55), a)
	random.Shuffle(random.New(55), b)
	if !slices.Equal(a, b) {
		t.Fatalf("Shuffle not deterministic: %v vs %v", a, b)
	}
	sorted := slices.Clone(a)
	slices.Sort(sorted)
	if !slices.Equal(sorted, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}) {
		t.Fatalf("Shuffle lost or duplicated elements: %v", a)
	}

	permCounts := make(map[[3]int]int, 6)
	for range 12000 {
		arr := [3]int{1, 2, 3}
		random.Shuffle(s, arr[:])
		permCounts[arr]++
	}
	if len(permCounts) != 6 {
		t.Fatalf("Shuffle produced %d permutations of 3 items, want 6", len(permCounts))
	}
	for perm, c := range permCounts {
		if c < 1700 || c > 2300 {
			t.Fatalf("Shuffle perm %v count %d outside [1700, 2300]", perm, c)
		}
	}
}

func TestInvalidArgumentsPanic(t *testing.T) {
	s := random.New(1)
	cases := []struct {
		name string
		fn   func()
	}{
		{"Intn(0)", func() { s.Intn(0) }},
		{"Intn(-1)", func() { s.Intn(-1) }},
		{"IntRange(5,4)", func() { s.IntRange(5, 4) }},
		{"Range(2,1)", func() { s.Range(2, 1) }},
		{"Range(NaN,1)", func() { s.Range(math.NaN(), 1) }},
		{"Range(1,NaN)", func() { s.Range(1, math.NaN()) }},
		{"Range32(2,1)", func() { s.Range32(2, 1) }},
		{"Range32(NaN,1)", func() { s.Range32(float32(math.NaN()), 1) }},
		{"Pick(empty)", func() { random.Pick(s, []int(nil)) }},
		{"WeightedIndex(negative)", func() { s.WeightedIndex([]float64{1, -0.1}) }},
		{"WeightedIndex(NaN)", func() { s.WeightedIndex([]float64{1, math.NaN()}) }},
		{"WeightedIndex(Inf)", func() { s.WeightedIndex([]float64{1, math.Inf(1)}) }},
		{"WeightedIndex(overflow)", func() { s.WeightedIndex([]float64{math.MaxFloat64, math.MaxFloat64}) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s did not panic", tc.name)
				}
			}()
			tc.fn()
		})
	}
}

func TestDeterministicGameLoopSimulationPasses(t *testing.T) {
	type simState struct {
		Step   int
		X, Y   float64
		Score  int
		LootID int
	}

	// Run a real fixed-step game.Loop under nsgotest multiple times and assert identical final state.
	runLoop := func() simState {
		streams := random.NewStreams(1337)
		ai := streams.Stream("ai")
		loot := streams.Stream("loot")

		var st simState
		loop := &game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				st.Step++
				sec := dt.Seconds()
				st.X += ai.Range(-10, 10) * sec
				st.Y += ai.Range(-10, 10) * sec
				if loot.Chance(0.2) {
					st.Score += loot.IntRange(1, 5)
					st.LootID = loot.WeightedIndex([]float64{5, 3, 2})
				}
			},
		}

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			loop.Run(cx)
			return core.View{}
		})
		app.Advance(500 * time.Millisecond)
		return st
	}

	random.AssertDeterministic(t, 4, runLoop)

	// Step-by-step deterministic simulation check.
	makeSim := func() func(step int) simState {
		streams := random.NewStreams(1337)
		ai := streams.Stream("ai")
		loot := streams.Stream("loot")
		dt := (time.Second / 60).Seconds()
		var st simState
		return func(step int) simState {
			st.Step = step
			st.X += ai.Range(-10, 10) * dt
			st.Y += ai.Range(-10, 10) * dt
			if loot.Chance(0.2) {
				st.Score += loot.IntRange(1, 5)
				st.LootID = loot.WeightedIndex([]float64{5, 3, 2})
			}
			return st
		}
	}

	random.AssertDeterministicSteps(t, 120, makeSim, nil)
	if step, _, _, ok := random.CheckSteps(120, makeSim, nil); !ok || step != -1 {
		t.Fatalf("CheckSteps on deterministic sim = (%d, %v), want (-1, true)", step, ok)
	}
}

func TestCheckStepsReportsExactFirstDivergentStep(t *testing.T) {
	runCount := 0
	step, a, b, ok := random.CheckSteps(20, func() func(step int) int {
		id := runCount
		runCount++
		return func(s int) int {
			if s < 7 {
				return s * 10
			}
			return s*10 + id
		}
	}, nil)
	if ok {
		t.Fatal("CheckSteps passed on a simulation that diverges at step 7")
	}
	if step != 7 {
		t.Fatalf("firstDivergentStep = %d, want 7", step)
	}
	if a != 70 || b != 71 {
		t.Fatalf("diverging states = (%d, %d), want (70, 71)", a, b)
	}
}

func TestDeterminismChecksCatchNonDeterminism(t *testing.T) {
	t.Run("map iteration order", func(t *testing.T) {
		// Map iteration order in Go is randomized; order-sensitive hashing diverges.
		step, a, b, ok := random.CheckStepsRuns(8, 10, func() func(step int) uint64 {
			entities := make(map[int]uint64, 16)
			for i := range 16 {
				entities[i] = uint64(i + 1)
			}
			var acc uint64 = 1
			return func(step int) uint64 {
				for k, v := range entities {
					acc = acc*31 + uint64(k) + v
				}
				return acc
			}
		}, nil)
		if ok {
			t.Fatal("CheckSteps failed to catch map iteration nondeterminism")
		}
		if step < 0 || a == b {
			t.Fatalf("unexpected mismatch report: step=%d a=%d b=%d", step, a, b)
		}
	})

	t.Run("wall clock time.Now", func(t *testing.T) {
		m, ok := random.CompareRuns(4, func() int64 {
			// Ensure wall clock advances between runs.
			start := time.Now().UnixNano()
			for time.Now().UnixNano() == start {
			}
			return time.Now().UnixNano()
		}, nil)
		if ok {
			t.Fatal("CompareRuns failed to catch time.Now() wall-clock nondeterminism")
		}
		if m.Run < 1 || m.First == m.Other {
			t.Fatalf("unexpected mismatch: %+v", m)
		}
	})

	t.Run("unseeded math/rand", func(t *testing.T) {
		step, a, b, ok := random.CheckSteps(5, func() func(step int) uint64 {
			return func(step int) uint64 {
				return mathrand.Uint64()
			}
		}, nil)
		if ok {
			t.Fatal("CheckSteps failed to catch unseeded math/rand/v2")
		}
		if step != 0 || a == b {
			t.Fatalf("unexpected mismatch report: step=%d a=%d b=%d", step, a, b)
		}
	})

	t.Run("goroutine race", func(t *testing.T) {
		// Unordered concurrent goroutine results read via select produce nondeterministic order.
		_, ok := random.CompareRuns(32, func() uint64 {
			chA := make(chan uint64, 1)
			chB := make(chan uint64, 1)
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				chA <- 1
			}()
			go func() {
				defer wg.Done()
				chB <- 2
			}()
			wg.Wait()
			var acc uint64
			for range 2 {
				select {
				case v := <-chA:
					acc = acc*10 + v
				case v := <-chB:
					acc = acc*10 + v
				}
			}
			return acc
		}, nil)
		if ok {
			t.Fatal("CompareRuns failed to catch goroutine scheduling nondeterminism")
		}
	})
}

// spyTB captures Fatalf messages without failing the enclosing test.
type spyTB struct {
	testing.TB
	failed string
}

func (s *spyTB) Helper() {}

func (s *spyTB) Fatalf(format string, args ...any) {
	s.failed = fmt.Sprintf(format, args...)
}

func TestAssertHelpersFormatDivergingStates(t *testing.T) {
	spy := &spyTB{TB: t}
	n := 0
	random.AssertDeterministic(spy, 3, func() int {
		n++
		return n
	})
	if !strings.Contains(spy.failed, "run 1") || !strings.Contains(spy.failed, "1") || !strings.Contains(spy.failed, "2") {
		t.Fatalf("AssertDeterministic failure message = %q, want run index and both states", spy.failed)
	}

	spy2 := &spyTB{TB: t}
	runIdx := 0
	random.AssertDeterministicSteps(spy2, 10, func() func(step int) string {
		id := runIdx
		runIdx++
		return func(step int) string {
			if step == 3 {
				return fmt.Sprintf("diverged-%d", id)
			}
			return "same"
		}
	}, nil)
	if !strings.Contains(spy2.failed, "step 3") || !strings.Contains(spy2.failed, "diverged-0") || !strings.Contains(spy2.failed, "diverged-1") {
		t.Fatalf("AssertDeterministicSteps failure message = %q, want step 3 and both states", spy2.failed)
	}
}
