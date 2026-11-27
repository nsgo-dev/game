package random_test

import (
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/nsgo-dev/game/pkg/random"
)

func TestNoiseGoldenVectors(t *testing.T) {
	cfg := random.FractalConfig{
		Octaves:     4,
		Frequency:   0.5,
		Lacunarity:  2.0,
		Persistence: 0.5,
		Gain:        2.0,
	}

	pts2 := [][2]float64{
		{0.0, 0.0},
		{1.25, -3.75},
		{-12.345, 67.89},
	}
	pts3 := [][3]float64{
		{0.0, 0.0, 0.0},
		{1.25, -3.75, 2.5},
		{-12.345, 67.89, -4.125},
	}

	type seedGoldens struct {
		seed         uint64
		value2D      [3]uint64
		simplex2D    [3]uint64
		fbm2D        [3]uint64
		ridged2D     [3]uint64
		billow2D     [3]uint64
		domainWarp2X [3]uint64
		domainWarp2Y [3]uint64
		value3D      [3]uint64
		simplex3D    [3]uint64
		fbm3D        [3]uint64
		ridged3D     [3]uint64
	}

	goldens := []seedGoldens{
		{
			seed: 1337,
			value2D: [3]uint64{
				0xbfee04db91d771b2,
				0xbfe6b1b3de529915,
				0x3fdbdaa49e608aa4,
			},
			simplex2D: [3]uint64{
				0x0000000000000000,
				0x3feb54e44334ba2d,
				0x3fe50c89e53a88fa,
			},
			fbm2D: [3]uint64{
				0x0000000000000000,
				0x3fc7e29376c2e219,
				0x3fd8f219ec65c17f,
			},
			ridged2D: [3]uint64{
				0x3ff0000000000000,
				0x3fcbc6eedd19bc96,
				0x3fc3194441224cc9,
			},
			billow2D: [3]uint64{
				0xbff0000000000000,
				0x3fc48c6dac771dfa,
				0x3fa644551dbd889e,
			},
			domainWarp2X: [3]uint64{
				0x0000000000000000,
				0x3ff87a7ba6448a65,
				0xc027854a9ff5785f,
			},
			domainWarp2Y: [3]uint64{
				0x3fe31f7e9a7296a4,
				0xc00e407f91176fa7,
				0x4050d702a63f94ce,
			},
			value3D: [3]uint64{
				0x3fb273c9c0da7450,
				0x3fba2b172adcca14,
				0xbfdfb19796ecadb5,
			},
			simplex3D: [3]uint64{
				0x0000000000000000,
				0xbfd8bd5555555554,
				0xbfe8a36efa80288b,
			},
			fbm3D: [3]uint64{
				0x0000000000000000,
				0xbfb0ec511111110b,
				0xbfd57e0ded04046a,
			},
			ridged3D: [3]uint64{
				0x3ff0000000000000,
				0x3fe498610176ff6d,
				0x3fd91a233b311e16,
			},
		},
		{
			seed: 42,
			value2D: [3]uint64{
				0xbfa1b37ab83d2a60,
				0xbfe22f7d747707a8,
				0xbfb4747ec4c7f7c0,
			},
			simplex2D: [3]uint64{
				0x0000000000000000,
				0xbfecfade5ea7310a,
				0xbfeb881647e9e818,
			},
			fbm2D: [3]uint64{
				0x0000000000000000,
				0xbfe397506f48f623,
				0xbfd3b7b6dc4cf72f,
			},
			ridged2D: [3]uint64{
				0x3ff0000000000000,
				0x3fa58d93ea00c490,
				0x3fd83ae155814d49,
			},
			billow2D: [3]uint64{
				0xbff0000000000000,
				0x3fd4c779e903a930,
				0xbfcb93afccc8f7a2,
			},
			domainWarp2X: [3]uint64{
				0x0000000000000000,
				0x3fd53a0eb2251d98,
				0xc0299d40695dd907,
			},
			domainWarp2Y: [3]uint64{
				0x3fd4bd4599127640,
				0xc00a553dca807e24,
				0x4050faca1a2ef351,
			},
			value3D: [3]uint64{
				0x3fee10eb623bf542,
				0xbfc213d3403285a6,
				0xbfd0c1b3973574bd,
			},
			simplex3D: [3]uint64{
				0x0000000000000000,
				0xbfe3bdfffffffffa,
				0x3fdb3ff232feef16,
			},
			fbm3D: [3]uint64{
				0x0000000000000000,
				0xbfcd2c5f49f49f42,
				0x3fce6e78edd1d280,
			},
			ridged3D: [3]uint64{
				0x3ff0000000000000,
				0x3fe101b5bef8b7f5,
				0x3fdebca506554d49,
			},
		},
	}

	checkBits := func(t *testing.T, label string, got float64, wantBits uint64) {
		t.Helper()
		gotBits := math.Float64bits(got)
		if gotBits != wantBits {
			t.Fatalf("%s = %.17g (0x%016x), want %.17g (0x%016x)",
				label, got, gotBits, math.Float64frombits(wantBits), wantBits)
		}
	}

	for _, g := range goldens {
		t.Run(fmt.Sprintf("seed_%d", g.seed), func(t *testing.T) {
			n := random.NewNoise(g.seed)
			for i, p := range pts2 {
				checkBits(t, fmt.Sprintf("Value2D[%d]", i), n.Value2D(p[0], p[1]), g.value2D[i])
				checkBits(t, fmt.Sprintf("Simplex2D[%d]", i), n.Simplex2D(p[0], p[1]), g.simplex2D[i])
				checkBits(t, fmt.Sprintf("FBM2D[%d]", i), n.FBM2D(p[0], p[1], cfg), g.fbm2D[i])
				checkBits(t, fmt.Sprintf("Ridged2D[%d]", i), n.Ridged2D(p[0], p[1], cfg), g.ridged2D[i])
				checkBits(t, fmt.Sprintf("Billow2D[%d]", i), n.Billow2D(p[0], p[1], cfg), g.billow2D[i])
				wx, wy := n.DomainWarp2D(p[0], p[1], 1.5, cfg)
				checkBits(t, fmt.Sprintf("DomainWarp2D.x[%d]", i), wx, g.domainWarp2X[i])
				checkBits(t, fmt.Sprintf("DomainWarp2D.y[%d]", i), wy, g.domainWarp2Y[i])
			}
			for i, p := range pts3 {
				checkBits(t, fmt.Sprintf("Value3D[%d]", i), n.Value3D(p[0], p[1], p[2]), g.value3D[i])
				checkBits(t, fmt.Sprintf("Simplex3D[%d]", i), n.Simplex3D(p[0], p[1], p[2]), g.simplex3D[i])
				checkBits(t, fmt.Sprintf("FBM3D[%d]", i), n.FBM3D(p[0], p[1], p[2], cfg), g.fbm3D[i])
				checkBits(t, fmt.Sprintf("Ridged3D[%d]", i), n.Ridged3D(p[0], p[1], p[2], cfg), g.ridged3D[i])
			}
		})
	}
}

func TestNoiseBoundsAndDistribution(t *testing.T) {
	n := random.NewNoise(1337)
	cfg := random.DefaultFractalConfig()

	type stats struct {
		min, max, sum float64
		count         int
	}
	update := func(s *stats, v float64, lo, hi float64, name string) {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < lo || v > hi {
			t.Fatalf("%s out of [%v, %v]: %v", name, lo, hi, v)
		}
		if s.count == 0 || v < s.min {
			s.min = v
		}
		if s.count == 0 || v > s.max {
			s.max = v
		}
		s.sum += v
		s.count++
	}

	var val2, simp2, fbm2, ridge2, billow2 stats
	for gy := -64; gy < 64; gy++ {
		y := float64(gy) * 0.137
		for gx := -64; gx < 64; gx++ {
			x := float64(gx) * 0.137
			update(&val2, n.Value2D(x, y), -1, 1, "Value2D")
			update(&simp2, n.Simplex2D(x, y), -1, 1, "Simplex2D")
			update(&fbm2, n.FBM2D(x, y, cfg), -1, 1, "FBM2D")
			update(&ridge2, n.Ridged2D(x, y, cfg), 0, 1, "Ridged2D")
			update(&billow2, n.Billow2D(x, y, cfg), -1, 1, "Billow2D")
		}
	}

	var val3, simp3, fbm3, ridge3 stats
	for gz := -10; gz < 10; gz++ {
		z := float64(gz) * 0.231
		for gy := -10; gy < 10; gy++ {
			y := float64(gy) * 0.231
			for gx := -10; gx < 10; gx++ {
				x := float64(gx) * 0.231
				update(&val3, n.Value3D(x, y, z), -1, 1, "Value3D")
				update(&simp3, n.Simplex3D(x, y, z), -1, 1, "Simplex3D")
				update(&fbm3, n.FBM3D(x, y, z, cfg), -1, 1, "FBM3D")
				update(&ridge3, n.Ridged3D(x, y, z, cfg), 0, 1, "Ridged3D")
			}
		}
	}

	checkSymmetric := func(name string, s stats, minSpan float64) {
		t.Helper()
		if s.min >= 0 || s.max <= 0 || (s.max-s.min) < minSpan {
			t.Fatalf("%s poor dynamic range: min=%v max=%v (want span >= %v)", name, s.min, s.max, minSpan)
		}
		mean := s.sum / float64(s.count)
		if math.Abs(mean) > 0.15 {
			t.Fatalf("%s mean = %v, want near 0", name, mean)
		}
	}
	checkSymmetric("Value2D", val2, 1.4)
	checkSymmetric("Simplex2D", simp2, 1.4)
	checkSymmetric("FBM2D", fbm2, 1.0)
	checkSymmetric("Value3D", val3, 1.2)
	checkSymmetric("Simplex3D", simp3, 1.2)
	checkSymmetric("FBM3D", fbm3, 0.8)

	if billow2.min >= 0 || billow2.max <= 0 || (billow2.max-billow2.min) < 0.8 {
		t.Fatalf("Billow2D poor dynamic range: min=%v max=%v", billow2.min, billow2.max)
	}
	if ridge2.min > 0.15 || ridge2.max < 0.85 {
		t.Fatalf("Ridged2D poor dynamic range: min=%v max=%v", ridge2.min, ridge2.max)
	}
	if ridge3.min > 0.2 || ridge3.max < 0.8 {
		t.Fatalf("Ridged3D poor dynamic range: min=%v max=%v", ridge3.min, ridge3.max)
	}
}

func TestNoiseContinuityAndSmoothness(t *testing.T) {
	n := random.NewNoise(42)
	cfg := random.DefaultFractalConfig()
	const eps = 1e-4

	// Sample across many positions including integer grid and simplex cell boundaries.
	for i := -25; i <= 25; i++ {
		x := float64(i) * 0.2
		y := float64(i)*-0.15 + 0.5
		z := float64(i)*0.11 - 0.3

		checkStep := func(name string, a, b, maxDelta float64) {
			t.Helper()
			if d := math.Abs(a - b); d > maxDelta {
				t.Fatalf("%s discontinuity at (%.4f, %.4f, %.4f): |%v - %v| = %v > %v",
					name, x, y, z, a, b, d, maxDelta)
			}
		}

		checkStep("Value2D_x", n.Value2D(x, y), n.Value2D(x+eps, y), 0.01)
		checkStep("Value2D_y", n.Value2D(x, y), n.Value2D(x, y+eps), 0.01)
		checkStep("Value3D_x", n.Value3D(x, y, z), n.Value3D(x+eps, y, z), 0.01)
		checkStep("Value3D_z", n.Value3D(x, y, z), n.Value3D(x, y, z+eps), 0.01)

		checkStep("Simplex2D_x", n.Simplex2D(x, y), n.Simplex2D(x+eps, y), 0.01)
		checkStep("Simplex2D_y", n.Simplex2D(x, y), n.Simplex2D(x, y+eps), 0.01)
		checkStep("Simplex3D_x", n.Simplex3D(x, y, z), n.Simplex3D(x+eps, y, z), 0.01)
		checkStep("Simplex3D_z", n.Simplex3D(x, y, z), n.Simplex3D(x, y, z+eps), 0.01)

		checkStep("FBM2D", n.FBM2D(x, y, cfg), n.FBM2D(x+eps, y+eps, cfg), 0.05)
		checkStep("FBM3D", n.FBM3D(x, y, z, cfg), n.FBM3D(x+eps, y+eps, z+eps, cfg), 0.05)
		checkStep("Ridged2D", n.Ridged2D(x, y, cfg), n.Ridged2D(x+eps, y+eps, cfg), 0.05)
		checkStep("Ridged3D", n.Ridged3D(x, y, z, cfg), n.Ridged3D(x+eps, y+eps, z+eps, cfg), 0.05)
		checkStep("Billow2D", n.Billow2D(x, y, cfg), n.Billow2D(x+eps, y+eps, cfg), 0.05)

		wx1, wy1 := n.DomainWarp2D(x, y, 2.0, cfg)
		wx2, wy2 := n.DomainWarp2D(x+eps, y+eps, 2.0, cfg)
		checkStep("DomainWarp2D_x", wx1, wx2, 0.1)
		checkStep("DomainWarp2D_y", wy1, wy2, 0.1)
	}
}

func TestNoiseSeedReproducibilityIndependenceAndConcurrency(t *testing.T) {
	a := random.NewNoise(1337)
	b := random.NewNoise(1337)
	other := random.NewNoise(1338)

	if a.Seed() != 1337 {
		t.Fatalf("Seed() = %d, want 1337", a.Seed())
	}

	cfg := random.DefaultFractalConfig()
	diffs := 0
	for i := range 64 {
		x := float64(i)*0.37 - 5.1
		y := float64(i)*-0.29 + 3.4
		z := float64(i)*0.19 + 1.2

		if math.Float64bits(a.Value2D(x, y)) != math.Float64bits(b.Value2D(x, y)) {
			t.Fatalf("Value2D not reproducible at %d", i)
		}
		if math.Float64bits(a.Value3D(x, y, z)) != math.Float64bits(b.Value3D(x, y, z)) {
			t.Fatalf("Value3D not reproducible at %d", i)
		}
		if math.Float64bits(a.Simplex2D(x, y)) != math.Float64bits(b.Simplex2D(x, y)) {
			t.Fatalf("Simplex2D not reproducible at %d", i)
		}
		if math.Float64bits(a.Simplex3D(x, y, z)) != math.Float64bits(b.Simplex3D(x, y, z)) {
			t.Fatalf("Simplex3D not reproducible at %d", i)
		}
		if math.Float64bits(a.FBM2D(x, y, cfg)) != math.Float64bits(b.FBM2D(x, y, cfg)) {
			t.Fatalf("FBM2D not reproducible at %d", i)
		}
		if a.Simplex2D(x, y) != other.Simplex2D(x, y) {
			diffs++
		}
	}
	if diffs < 60 {
		t.Fatalf("different seeds produced too few distinct samples: %d/64", diffs)
	}

	// Fork produces deterministic, independent noise generators.
	elev1 := a.Fork("elevation")
	elev2 := b.Fork("elevation")
	moist := a.Fork("moisture")
	if elev1.Seed() != elev2.Seed() {
		t.Fatalf("Fork(\"elevation\") seeds differ: %d vs %d", elev1.Seed(), elev2.Seed())
	}
	if elev1.Seed() == moist.Seed() || elev1.Seed() == a.Seed() {
		t.Fatalf("Fork seeds collided: elev=%d moist=%d root=%d", elev1.Seed(), moist.Seed(), a.Seed())
	}
	if math.Float64bits(elev1.Simplex2D(1.5, 2.5)) != math.Float64bits(elev2.Simplex2D(1.5, 2.5)) {
		t.Fatal("Forked Noise generators diverged")
	}

	// Verify AssertDeterministic compatibility.
	random.AssertDeterministic(t, 4, func() [4]uint64 {
		n := random.NewNoise(42)
		return [4]uint64{
			math.Float64bits(n.Value2D(1.2, 3.4)),
			math.Float64bits(n.Simplex2D(1.2, 3.4)),
			math.Float64bits(n.Simplex3D(1.2, 3.4, 5.6)),
			math.Float64bits(n.FBM2D(1.2, 3.4, cfg)),
		}
	})

	// Noise is immutable and safe for concurrent goroutine reads.
	const workers = 8
	wantSimp := math.Float64bits(a.Simplex2D(4.2, -7.8))
	wantFBM := math.Float64bits(a.FBM3D(1.1, 2.2, 3.3, cfg))
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 256 {
				if got := math.Float64bits(a.Simplex2D(4.2, -7.8)); got != wantSimp {
					errs <- fmt.Errorf("concurrent Simplex2D mismatch: 0x%016x != 0x%016x", got, wantSimp)
					return
				}
				if got := math.Float64bits(a.FBM3D(1.1, 2.2, 3.3, cfg)); got != wantFBM {
					errs <- fmt.Errorf("concurrent FBM3D mismatch: 0x%016x != 0x%016x", got, wantFBM)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestNoiseFractalConfigDefaultsAndDomainWarp(t *testing.T) {
	n := random.NewNoise(42)
	def := random.DefaultFractalConfig()
	zero := random.FractalConfig{}

	if math.Float64bits(n.FBM2D(1.3, 2.7, zero)) != math.Float64bits(n.FBM2D(1.3, 2.7, def)) {
		t.Fatal("zero FractalConfig did not match DefaultFractalConfig for FBM2D")
	}
	if math.Float64bits(n.FBM3D(1.3, 2.7, -0.9, zero)) != math.Float64bits(n.FBM3D(1.3, 2.7, -0.9, def)) {
		t.Fatal("zero FractalConfig did not match DefaultFractalConfig for FBM3D")
	}
	if math.Float64bits(n.Ridged2D(1.3, 2.7, zero)) != math.Float64bits(n.Ridged2D(1.3, 2.7, def)) {
		t.Fatal("zero FractalConfig did not match DefaultFractalConfig for Ridged2D")
	}
	if math.Float64bits(n.Ridged3D(1.3, 2.7, -0.9, zero)) != math.Float64bits(n.Ridged3D(1.3, 2.7, -0.9, def)) {
		t.Fatal("zero FractalConfig did not match DefaultFractalConfig for Ridged3D")
	}
	if math.Float64bits(n.Billow2D(1.3, 2.7, zero)) != math.Float64bits(n.Billow2D(1.3, 2.7, def)) {
		t.Fatal("zero FractalConfig did not match DefaultFractalConfig for Billow2D")
	}

	// Gain acts as persistence fallback when Persistence <= 0 in FBM/Billow.
	withGain := random.FractalConfig{Octaves: 3, Frequency: 0.25, Lacunarity: 2.1, Gain: 0.4}
	withPers := random.FractalConfig{Octaves: 3, Frequency: 0.25, Lacunarity: 2.1, Persistence: 0.4}
	if math.Float64bits(n.FBM2D(2.1, 3.4, withGain)) != math.Float64bits(n.FBM2D(2.1, 3.4, withPers)) {
		t.Fatal("Gain fallback did not match Persistence in FBM2D")
	}
	if math.Float64bits(n.Billow2D(2.1, 3.4, withGain)) != math.Float64bits(n.Billow2D(2.1, 3.4, withPers)) {
		t.Fatal("Gain fallback did not match Persistence in Billow2D")
	}

	// DomainWarp2D with strength 0 returns original coordinates unchanged.
	wx, wy := n.DomainWarp2D(3.5, -2.25, 0, def)
	if wx != 3.5 || wy != -2.25 {
		t.Fatalf("DomainWarp2D(strength=0) = (%v, %v), want (3.5, -2.25)", wx, wy)
	}
	// Non-zero strength warps both axes independently even at low frequencies.
	lowFreq := random.FractalConfig{Octaves: 3, Frequency: 0.005, Lacunarity: 2.0, Persistence: 0.5}
	wx, wy = n.DomainWarp2D(10.0, 20.0, 4.0, lowFreq)
	dx, dy := wx-10.0, wy-20.0
	if dx == 0 || dy == 0 || math.Abs(dx-dy) < 1e-3 {
		t.Fatalf("DomainWarp2D did not decorrelate X and Y warps: dx=%v dy=%v", dx, dy)
	}
}

func TestNoiseGridFillHelpers(t *testing.T) {
	n := random.NewNoise(1337)
	const w, h = 32, 24
	const ox, oy, sx, sy = -4.5, 2.25, 0.125, 0.15
	cfg := random.FractalConfig{
		Octaves:     4,
		Frequency:   0.5,
		Lacunarity:  2.0,
		Persistence: 0.5,
	}

	dst := make([]float64, w*h)

	// FillSimplex2D bit-exact match.
	n.FillSimplex2D(dst, w, h, ox, oy, sx, sy)
	for gy := range h {
		y := oy + float64(float64(gy)*sy)
		for gx := range w {
			x := ox + float64(float64(gx)*sx)
			got := dst[gy*w+gx]
			want := n.Simplex2D(x, y)
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf("FillSimplex2D[%d,%d] = 0x%016x, want 0x%016x", gx, gy, math.Float64bits(got), math.Float64bits(want))
			}
		}
	}

	// FillValue2D bit-exact match.
	n.FillValue2D(dst, w, h, ox, oy, sx, sy)
	for gy := range h {
		y := oy + float64(float64(gy)*sy)
		for gx := range w {
			x := ox + float64(float64(gx)*sx)
			got := dst[gy*w+gx]
			want := n.Value2D(x, y)
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf("FillValue2D[%d,%d] = 0x%016x, want 0x%016x", gx, gy, math.Float64bits(got), math.Float64bits(want))
			}
		}
	}

	// FillFBM2D bit-exact match.
	n.FillFBM2D(dst, w, h, ox, oy, sx, sy, cfg)
	for gy := range h {
		y := oy + float64(float64(gy)*sy)
		for gx := range w {
			x := ox + float64(float64(gx)*sx)
			got := dst[gy*w+gx]
			want := n.FBM2D(x, y, cfg)
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf("FillFBM2D[%d,%d] = 0x%016x, want 0x%016x", gx, gy, math.Float64bits(got), math.Float64bits(want))
			}
		}
	}

	// FillGrid2D bit-exact match with custom sampler.
	n.FillGrid2D(dst, w, h, ox, oy, sx, sy, func(x, y float64) float64 {
		return n.Ridged2D(x, y, cfg)
	})
	for gy := range h {
		y := oy + float64(float64(gy)*sy)
		for gx := range w {
			x := ox + float64(float64(gx)*sx)
			got := dst[gy*w+gx]
			want := n.Ridged2D(x, y, cfg)
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf("FillGrid2D[%d,%d] = 0x%016x, want 0x%016x", gx, gy, math.Float64bits(got), math.Float64bits(want))
			}
		}
	}

	// Zero-allocation verification.
	if allocs := testing.AllocsPerRun(50, func() {
		n.FillSimplex2D(dst, w, h, ox, oy, sx, sy)
	}); allocs != 0 {
		t.Fatalf("FillSimplex2D allocs = %v, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(50, func() {
		n.FillValue2D(dst, w, h, ox, oy, sx, sy)
	}); allocs != 0 {
		t.Fatalf("FillValue2D allocs = %v, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(50, func() {
		n.FillFBM2D(dst, w, h, ox, oy, sx, sy, cfg)
	}); allocs != 0 {
		t.Fatalf("FillFBM2D allocs = %v, want 0", allocs)
	}
	sampler := func(x, y float64) float64 { return n.Billow2D(x, y, cfg) }
	if allocs := testing.AllocsPerRun(50, func() {
		n.FillGrid2D(dst, w, h, ox, oy, sx, sy, sampler)
	}); allocs != 0 {
		t.Fatalf("FillGrid2D allocs = %v, want 0", allocs)
	}

	// Non-positive dimensions are safe no-ops even with nil dst.
	n.FillSimplex2D(nil, 0, 10, 0, 0, 1, 1)
	n.FillValue2D(nil, 10, 0, 0, 0, 1, 1)
	n.FillFBM2D(nil, -1, 5, 0, 0, 1, 1, cfg)
	n.FillGrid2D(nil, 5, -1, 0, 0, 1, 1, sampler)

	// Invalid arguments panic.
	panicCases := []struct {
		name string
		fn   func()
	}{
		{"FillSimplex2D_small", func() { n.FillSimplex2D(make([]float64, 5), 3, 2, 0, 0, 1, 1) }},
		{"FillValue2D_small", func() { n.FillValue2D(make([]float64, 5), 3, 2, 0, 0, 1, 1) }},
		{"FillFBM2D_small", func() { n.FillFBM2D(make([]float64, 5), 3, 2, 0, 0, 1, 1, cfg) }},
		{"FillGrid2D_small", func() { n.FillGrid2D(make([]float64, 5), 3, 2, 0, 0, 1, 1, sampler) }},
		{"FillGrid2D_nil_sample", func() { n.FillGrid2D(make([]float64, 6), 3, 2, 0, 0, 1, 1, nil) }},
	}
	for _, tc := range panicCases {
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

func BenchmarkNoiseFill256x256Simplex(b *testing.B) {
	n := random.NewNoise(1337)
	dst := make([]float64, 256*256)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		n.FillSimplex2D(dst, 256, 256, 0, 0, 0.05, 0.05)
	}
}

func BenchmarkNoiseFill256x256Value(b *testing.B) {
	n := random.NewNoise(1337)
	dst := make([]float64, 256*256)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		n.FillValue2D(dst, 256, 256, 0, 0, 0.05, 0.05)
	}
}

func BenchmarkNoiseFill256x256FBM4(b *testing.B) {
	n := random.NewNoise(1337)
	dst := make([]float64, 256*256)
	cfg := random.FractalConfig{
		Octaves:     4,
		Frequency:   1.0,
		Lacunarity:  2.0,
		Persistence: 0.5,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		n.FillFBM2D(dst, 256, 256, 0, 0, 0.05, 0.05, cfg)
	}
}
