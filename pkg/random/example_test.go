package random_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/random"
)

// A Stream gives the same sequence for the same seed on every platform, so a seeded level or replay comes out the same.
func ExampleStream() {
	a, b := random.New(42), random.New(42)
	fmt.Println(a.Intn(100) == b.Intn(100), a.Float64() == b.Float64())

	loot := random.New(7)
	rarity := []string{"common", "rare", "epic"}
	counts := map[string]int{}
	for range 1000 {
		counts[rarity[loot.WeightedIndex([]float64{80, 15, 5})]]++
	}
	fmt.Println(counts["common"] > counts["rare"], counts["rare"] > counts["epic"])

	st := loot.State()
	next := loot.Uint64()
	loot.SetState(st)
	fmt.Println(loot.Uint64() == next)
	// Output:
	// true true
	// true true
	// true
}

// Streams derives an independent named stream per system from one master seed, so adding a draw in one system does not change another's results.
func ExampleStreams() {
	world := random.NewStreams(1234)
	enemies, weather := world.Stream("enemies"), world.Stream("weather")
	first := weather.Uint32()

	again := random.NewStreams(1234)
	again.Stream("enemies").Uint64() // an extra draw in another system
	fmt.Println(again.Stream("weather").Uint32() == first, enemies.Seed() != weather.Seed())
	// Output:
	// true true
}

// Shuffle and Pick use a Stream, so a shuffled deck is reproducible from its seed.
func ExampleShuffle() {
	deck := []string{"A", "2", "3", "4", "5"}
	random.Shuffle(random.New(9), deck)
	again := []string{"A", "2", "3", "4", "5"}
	random.Shuffle(random.New(9), again)
	fmt.Println(deck, fmt.Sprint(deck) == fmt.Sprint(again))
	// Output:
	// [3 4 5 2 A] true
}

// Noise gives smooth coherent noise for terrain and textures, in the range [-1, 1] and the same for the same seed.
func ExampleNoise() {
	n := random.NewNoise(1)
	cfg := random.DefaultFractalConfig()
	h := n.FBM2D(10.5, 3.25, cfg)
	fmt.Println(h >= -1 && h <= 1, h == random.NewNoise(1).FBM2D(10.5, 3.25, cfg))
	grid := make([]float64, 4*4)
	n.FillSimplex2D(grid, 4, 4, 0, 0, 0.1, 0.1)
	fmt.Println(len(grid))
	// Output:
	// true true
	// 16
}

// CheckSteps runs a simulation several times from scratch and reports the first step where two runs disagree, which finds hidden nondeterminism.
func ExampleCheckSteps() {
	makeSim := func() func(step int) int {
		s := random.New(5)
		pos := 0
		return func(int) int {
			pos += s.IntRange(-1, 1)
			return pos
		}
	}
	step, _, _, ok := random.CheckSteps(100, makeSim, nil)
	fmt.Println(step, ok)
	// Output:
	// -1 true
}
