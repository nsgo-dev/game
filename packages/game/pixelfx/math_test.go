package pixelfx_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// The expected values in these tests are what pixelfx's own src/core at the pinned commit returns for the same calls under Node.

func TestRngIsPixelfxsMulberry32(t *testing.T) {
	r := pixelfx.NewRng(42)
	got := []float64{r.Next(), r.Next(), r.Next(), r.Range(-3, 3), r.Int(1, 6)}
	want := []float64{0.6011037519201636, 0.44829055899754167, 0.8524657934904099, 1.018404248636216, 2}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("draw %d is %v, want %v", i, got[i], want[i])
		}
	}
	if got := pixelfx.NewRng(0).Next(); got != 0.6270739405881613 {
		t.Errorf("seed 0 (read as 1) gives %v, want 0.6270739405881613", got)
	}
	if got := pixelfx.NewRng(-7.9).Next(); got != 0.43306733411736786 {
		t.Errorf("seed -7.9 (converted as >>> 0) gives %v, want 0.43306733411736786", got)
	}
}

func TestHashAndRandConvertAsJavaScriptDoes(t *testing.T) {
	for _, c := range []struct {
		in   []float64
		want uint32
	}{{[]float64{1, 2, 3}, 2901138804}, {[]float64{-5, 2.7}, 3719718745}, {[]float64{4294967301}, 2007225770}, {nil, 2654435769}} {
		if got := pixelfx.Hash(c.in...); got != c.want {
			t.Errorf("Hash(%v) = %d, want %d", c.in, got, c.want)
		}
	}
	if got := pixelfx.Rand(3, 9); got != 0.11862509953789413 {
		t.Errorf("Rand(3, 9) = %v", got)
	}
}

func TestNoiseMatchesPixelfx(t *testing.T) {
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"Noise", pixelfx.Noise(1.37, 5), 0.26955744680030636},
		{"Noise2", pixelfx.Noise2(2.5, -1.25, 3), 0.5333712513529463},
		{"Fbm", pixelfx.Fbm(0.3, 0.7, 2, 3), -0.14567158377864228},
		{"TileNoise", pixelfx.TileNoise(3.3, 7.1, 4, 8, 1), 0.7714577751037779},
		{"LoopNoise", pixelfx.LoopNoise(0.3, 1, 2), 0.5303838996587982},
	} {
		if math.Abs(c.got-c.want) > 1e-12 {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if a, b := pixelfx.LoopNoise(0, 1.5, 7), pixelfx.LoopNoise(1, 1.5, 7); math.Abs(a-b) > 1e-12 {
		t.Errorf("LoopNoise is %v at phase 0 and %v at 1, want the same", a, b)
	}
}

func TestHypotRoundAndInt32FollowJavaScript(t *testing.T) {
	if got := pixelfx.Hypot(3e-310, 4e-310); got != 5e-310 {
		t.Errorf("Hypot of subnormals = %v, want 5e-310", got)
	}
	if got := pixelfx.Hypot3(1.1, 2.2, 3.3); got != 4.115823125451335 {
		t.Errorf("Hypot3 = %v, want 4.115823125451335", got)
	}
	if got := pixelfx.Hypot(0.1, 0.2); got != 0.223606797749979 {
		t.Errorf("Hypot(0.1, 0.2) = %v", got)
	}
	for in, want := range map[float64]float64{-2.5: -2, 2.5: 3, -0.5000000000000001: -1, 0.49999999999999994: 0} {
		if got := pixelfx.Round(in); got != want {
			t.Errorf("Round(%v) = %v, want %v", in, got, want)
		}
	}
	// Regression: on arm64, inlined Round(d*30) must not fuse d*30-floor(d*30) into FMSUBD.
	d145 := 1.45
	if got := pixelfx.Round(d145 * float64(30)); got != 44 {
		t.Errorf("Round(1.45 * 30) = %v, want 44", got)
	}
	for in, want := range map[float64]int32{-1.9: -1, 2147483648: -2147483648, 4294967301: 5, math.NaN(): 0, math.Inf(1): 0} {
		if got := pixelfx.ToInt32(in); got != want {
			t.Errorf("ToInt32(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestEasing(t *testing.T) {
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"OutElastic", pixelfx.Ease.OutElastic(0.3), 0.875},
		{"OutBounce", pixelfx.Ease.OutBounce(0.8), 0.94},
		{"InOutCubic", pixelfx.Ease.InOutCubic(0.7), 0.8919999999999999},
		{"OutBack", pixelfx.Ease.OutBack(0.5), 1.0876975},
	} {
		if math.Abs(c.got-c.want) > 1e-12 {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestCycleRepeatsEveryPeriod(t *testing.T) {
	sample := func(t0 float64) []float64 {
		var ages []float64
		pixelfx.Cycle(t0, 2, 5, 3, pixelfx.Fixed(1.5), func(age, _ float64, r *pixelfx.Rng, i int) {
			ages = append(ages, age, r.Next(), float64(i))
		})
		return ages
	}
	a, b := sample(0.7), sample(2.7)
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("%d and %d particles a period apart", len(a), len(b))
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-12 {
			t.Fatalf("particle values differ a period apart: %v and %v", a, b)
		}
	}
}
