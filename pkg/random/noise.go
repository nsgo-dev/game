package random

import (
	"math"
	"math/bits"
)

const (
	// 2D simplex skew/unskew constants:
	//   f2  = 0.5 * (sqrt(3) - 1)
	//   g2  = (3 - sqrt(3)) / 6
	//   g22 = 1 - 2*g2 = 1 / sqrt(3)
	simplexF2  = 0.36602540378443864676372317075294
	simplexG2  = 0.21132486540518711774542560974902
	simplexG22 = 0.57735026918962576450914878050196

	// 3D simplex skew/unskew constants.
	simplexF3  = 1.0 / 3.0
	simplexG3  = 1.0 / 6.0
	simplexG32 = 2.0 / 6.0
	simplexG33 = 0.5

	// Normalization scales derived from the exact worst-case extrema over the
	// simplex with radial falloff t = 0.5 - r^2:
	//   2D (unit gradients): max <= 2 / (81 * sqrt(6)) ≈ 0.01008027 -> 1/max ≈ 99.20
	//   3D (12 cube-edge gradients): max <= 0.0130072 -> 1/max ≈ 76.88
	simplex2DScale = 99.0
	simplex3DScale = 76.0

	// Hardcoded unit-circle direction components at multiples of pi/8 (no runtime trig).
	unitCosPi8 = 0.92387953251128675613
	unitSinPi8 = 0.38268343236508977173
	unitCosPi4 = 0.70710678118654752440

	// Fixed decorrelation offsets in noise space for DomainWarp2D's Y channel.
	domainWarpOffsetX = 31.41592653589793
	domainWarpOffsetY = 47.85398163397448
)

// grad2 holds 16 unit-length 2D gradient vectors at 22.5-degree intervals,
// encoded as compile-time IEEE-754 float64 constants to avoid runtime trig
// functions and guarantee bit-identical output across architectures.
var grad2 = [16][2]float64{
	{1.0, 0.0},
	{unitCosPi8, unitSinPi8},
	{unitCosPi4, unitCosPi4},
	{unitSinPi8, unitCosPi8},
	{0.0, 1.0},
	{-unitSinPi8, unitCosPi8},
	{-unitCosPi4, unitCosPi4},
	{-unitCosPi8, unitSinPi8},
	{-1.0, 0.0},
	{-unitCosPi8, -unitSinPi8},
	{-unitCosPi4, -unitCosPi4},
	{-unitSinPi8, -unitCosPi8},
	{0.0, -1.0},
	{unitSinPi8, -unitCosPi8},
	{unitCosPi4, -unitCosPi4},
	{unitCosPi8, -unitSinPi8},
}

// FractalConfig configures multi-octave fractal noise evaluation.
// Zero or negative values fall back to sensible defaults:
// Octaves=4, Frequency=1, Lacunarity=2, Persistence=0.5 (or Gain when set for
// FBM/Billow), and Gain=2 for Ridged multifractal weighting.
type FractalConfig struct {
	// Octaves is the number of noise layers to combine (default 4).
	Octaves int
	// Frequency is the starting coordinate scale (default 1).
	Frequency float64
	// Lacunarity multiplies frequency on each subsequent octave (default 2).
	Lacunarity float64
	// Persistence multiplies amplitude on each subsequent octave (default 0.5).
	Persistence float64
	// Gain controls ridged multifractal weight feedback (default 2) and acts as
	// an amplitude-decay fallback for FBM/Billow when Persistence <= 0.
	Gain float64
}

// DefaultFractalConfig returns a FractalConfig populated with standard defaults
// suitable for terrain, clouds, and procedural textures.
func DefaultFractalConfig() FractalConfig {
	return FractalConfig{
		Octaves:     4,
		Frequency:   1.0,
		Lacunarity:  2.0,
		Persistence: 0.5,
		Gain:        2.0,
	}
}

// Noise is a deterministic, cross-platform procedural noise generator seeded
// through SplitMix64.
//
// All lookups use pure integer hashing and explicit IEEE-754 float64 arithmetic
// (without runtime transcendental functions or fused multiply-add contraction),
// producing bit-identical outputs across arm64, x86_64 and WebAssembly.
//
// Construct a Noise instance with NewNoise. Once constructed, Noise is
// immutable and safe for concurrent use by multiple goroutines.
type Noise struct {
	seed   uint64
	perm   [512]uint8
	perm12 [512]uint8
	values [256]float64
}

// NewNoise returns a deterministic Noise generator seeded with seed.
func NewNoise(seed uint64) *Noise {
	n := &Noise{seed: seed}

	var p [256]uint8
	for i := range 256 {
		p[i] = uint8(i)
	}

	x := seed
	for i := 255; i > 0; i-- {
		j := int(splitMix64Range(&x, uint64(i+1)))
		p[i], p[j] = p[j], p[i]
	}

	for i := range 256 {
		v := p[i]
		n.perm[i] = v
		n.perm[i+256] = v
		m12 := v % 12
		n.perm12[i] = m12
		n.perm12[i+256] = m12

		// Uniform 53-bit float64 lattice value in [-1, 1).
		u := splitMix64(&x) >> 11
		n.values[i] = float64(float64(u)*0x1p-52) - 1.0
	}

	return n
}

// Seed returns the seed that initialized n.
func (n *Noise) Seed() uint64 {
	return n.seed
}

// Fork derives an independent, deterministic child Noise generator for purpose
// (such as "elevation", "moisture" or "caves") from n's seed.
func (n *Noise) Fork(purpose string) *Noise {
	return NewNoise(deriveSeed(n.seed, purpose))
}

// Value2D evaluates smooth 2D lattice value noise at (x, y), returning a value in [-1, 1].
func (n *Noise) Value2D(x, y float64) float64 {
	fx := math.Floor(x)
	fy := math.Floor(y)

	ix := int(int64(fx)) & 255
	iy := int(int64(fy)) & 255

	u := fade(x - fx)
	v := fade(y - fy)

	a := int(n.perm[ix]) + iy
	b := int(n.perm[ix+1]) + iy

	x0 := lerp(u, n.values[n.perm[a]], n.values[n.perm[b]])
	x1 := lerp(u, n.values[n.perm[a+1]], n.values[n.perm[b+1]])
	return lerp(v, x0, x1)
}

// Value3D evaluates smooth 3D lattice value noise at (x, y, z), returning a value in [-1, 1].
func (n *Noise) Value3D(x, y, z float64) float64 {
	fx := math.Floor(x)
	fy := math.Floor(y)
	fz := math.Floor(z)

	ix := int(int64(fx)) & 255
	iy := int(int64(fy)) & 255
	iz := int(int64(fz)) & 255

	u := fade(x - fx)
	v := fade(y - fy)
	w := fade(z - fz)

	a := int(n.perm[ix]) + iy
	aa := int(n.perm[a]) + iz
	ab := int(n.perm[a+1]) + iz
	b := int(n.perm[ix+1]) + iy
	ba := int(n.perm[b]) + iz
	bb := int(n.perm[b+1]) + iz

	x00 := lerp(u, n.values[n.perm[aa]], n.values[n.perm[ba]])
	x10 := lerp(u, n.values[n.perm[ab]], n.values[n.perm[bb]])
	x01 := lerp(u, n.values[n.perm[aa+1]], n.values[n.perm[ba+1]])
	x11 := lerp(u, n.values[n.perm[ab+1]], n.values[n.perm[bb+1]])

	y0 := lerp(v, x00, x10)
	y1 := lerp(v, x01, x11)
	return lerp(w, y0, y1)
}

// Simplex2D evaluates 2D simplex gradient noise at (x, y), returning a value in [-1, 1].
func (n *Noise) Simplex2D(x, y float64) float64 {
	s := float64((x + y) * simplexF2)
	fi := math.Floor(x + s)
	fj := math.Floor(y + s)

	t := float64((fi + fj) * simplexG2)
	x0 := x - (fi - t)
	y0 := y - (fj - t)

	var i1, j1 int
	if x0 > y0 {
		i1, j1 = 1, 0
	} else {
		i1, j1 = 0, 1
	}

	x1 := x0 - float64(i1) + simplexG2
	y1 := y0 - float64(j1) + simplexG2
	x2 := x0 - simplexG22
	y2 := y0 - simplexG22

	ii := int(int64(fi)) & 255
	jj := int(int64(fj)) & 255

	var n0, n1, n2 float64

	t0 := 0.5 - float64(x0*x0) - float64(y0*y0)
	if t0 > 0 {
		t0 *= t0
		g := grad2[n.perm[ii+int(n.perm[jj])]&15]
		n0 = t0 * t0 * (float64(g[0]*x0) + float64(g[1]*y0))
	}

	t1 := 0.5 - float64(x1*x1) - float64(y1*y1)
	if t1 > 0 {
		t1 *= t1
		g := grad2[n.perm[ii+i1+int(n.perm[jj+j1])]&15]
		n1 = t1 * t1 * (float64(g[0]*x1) + float64(g[1]*y1))
	}

	t2 := 0.5 - float64(x2*x2) - float64(y2*y2)
	if t2 > 0 {
		t2 *= t2
		g := grad2[n.perm[ii+1+int(n.perm[jj+1])]&15]
		n2 = t2 * t2 * (float64(g[0]*x2) + float64(g[1]*y2))
	}

	return simplex2DScale * (n0 + n1 + n2)
}

// Simplex3D evaluates 3D simplex gradient noise at (x, y, z), returning a value in [-1, 1].
func (n *Noise) Simplex3D(x, y, z float64) float64 {
	s := float64((x + y + z) * simplexF3)
	fi := math.Floor(x + s)
	fj := math.Floor(y + s)
	fk := math.Floor(z + s)

	t := float64((fi + fj + fk) * simplexG3)
	x0 := x - (fi - t)
	y0 := y - (fj - t)
	z0 := z - (fk - t)

	var i1, j1, k1 int
	var i2, j2, k2 int
	if x0 >= y0 {
		if y0 >= z0 {
			i1, j1, k1 = 1, 0, 0
			i2, j2, k2 = 1, 1, 0
		} else if x0 >= z0 {
			i1, j1, k1 = 1, 0, 0
			i2, j2, k2 = 1, 0, 1
		} else {
			i1, j1, k1 = 0, 0, 1
			i2, j2, k2 = 1, 0, 1
		}
	} else {
		if y0 < z0 {
			i1, j1, k1 = 0, 0, 1
			i2, j2, k2 = 0, 1, 1
		} else if x0 < z0 {
			i1, j1, k1 = 0, 1, 0
			i2, j2, k2 = 0, 1, 1
		} else {
			i1, j1, k1 = 0, 1, 0
			i2, j2, k2 = 1, 1, 0
		}
	}

	x1 := x0 - float64(i1) + simplexG3
	y1 := y0 - float64(j1) + simplexG3
	z1 := z0 - float64(k1) + simplexG3

	x2 := x0 - float64(i2) + simplexG32
	y2 := y0 - float64(j2) + simplexG32
	z2 := z0 - float64(k2) + simplexG32

	x3 := x0 - simplexG33
	y3 := y0 - simplexG33
	z3 := z0 - simplexG33

	ii := int(int64(fi)) & 255
	jj := int(int64(fj)) & 255
	kk := int(int64(fk)) & 255

	var n0, n1, n2, n3 float64

	t0 := 0.5 - float64(x0*x0) - float64(y0*y0) - float64(z0*z0)
	if t0 > 0 {
		t0 *= t0
		gi0 := n.perm12[ii+int(n.perm[jj+int(n.perm[kk])])]
		n0 = t0 * t0 * grad3Dot(gi0, x0, y0, z0)
	}

	t1 := 0.5 - float64(x1*x1) - float64(y1*y1) - float64(z1*z1)
	if t1 > 0 {
		t1 *= t1
		gi1 := n.perm12[ii+i1+int(n.perm[jj+j1+int(n.perm[kk+k1])])]
		n1 = t1 * t1 * grad3Dot(gi1, x1, y1, z1)
	}

	t2 := 0.5 - float64(x2*x2) - float64(y2*y2) - float64(z2*z2)
	if t2 > 0 {
		t2 *= t2
		gi2 := n.perm12[ii+i2+int(n.perm[jj+j2+int(n.perm[kk+k2])])]
		n2 = t2 * t2 * grad3Dot(gi2, x2, y2, z2)
	}

	t3 := 0.5 - float64(x3*x3) - float64(y3*y3) - float64(z3*z3)
	if t3 > 0 {
		t3 *= t3
		gi3 := n.perm12[ii+1+int(n.perm[jj+1+int(n.perm[kk+1])])]
		n3 = t3 * t3 * grad3Dot(gi3, x3, y3, z3)
	}

	return simplex3DScale * (n0 + n1 + n2 + n3)
}

// FBM2D evaluates normalized 2D fractal Brownian motion using Simplex2D,
// returning a value in [-1, 1].
func (n *Noise) FBM2D(x, y float64, cfg FractalConfig) float64 {
	octaves, freq, lacunarity, persistence := resolveFractalConfig(cfg)
	var sum, maxAmp float64
	amp := 1.0
	for range octaves {
		sum += float64(n.Simplex2D(x*freq, y*freq) * amp)
		maxAmp += amp
		amp *= persistence
		freq *= lacunarity
	}
	return sum / maxAmp
}

// FBM3D evaluates normalized 3D fractal Brownian motion using Simplex3D,
// returning a value in [-1, 1].
func (n *Noise) FBM3D(x, y, z float64, cfg FractalConfig) float64 {
	octaves, freq, lacunarity, persistence := resolveFractalConfig(cfg)
	var sum, maxAmp float64
	amp := 1.0
	for range octaves {
		sum += float64(n.Simplex3D(x*freq, y*freq, z*freq) * amp)
		maxAmp += amp
		amp *= persistence
		freq *= lacunarity
	}
	return sum / maxAmp
}

// Ridged2D evaluates normalized 2D ridged multifractal noise using Simplex2D,
// returning a value in [0, 1] with sharp ridge crests near 1 and valleys near 0.
func (n *Noise) Ridged2D(x, y float64, cfg FractalConfig) float64 {
	octaves, freq, lacunarity, persistence, gain := resolveRidgedConfig(cfg)
	var sum, maxAmp float64
	amp := 1.0
	weight := 1.0
	for range octaves {
		s := 1.0 - math.Abs(n.Simplex2D(x*freq, y*freq))
		s = s * s
		s *= weight
		weight = s * gain
		if weight > 1.0 {
			weight = 1.0
		} else if weight < 0.0 {
			weight = 0.0
		}
		sum += float64(s * amp)
		maxAmp += amp
		amp *= persistence
		freq *= lacunarity
	}
	return sum / maxAmp
}

// Ridged3D evaluates normalized 3D ridged multifractal noise using Simplex3D,
// returning a value in [0, 1] with sharp ridge crests near 1 and valleys near 0.
func (n *Noise) Ridged3D(x, y, z float64, cfg FractalConfig) float64 {
	octaves, freq, lacunarity, persistence, gain := resolveRidgedConfig(cfg)
	var sum, maxAmp float64
	amp := 1.0
	weight := 1.0
	for range octaves {
		s := 1.0 - math.Abs(n.Simplex3D(x*freq, y*freq, z*freq))
		s = s * s
		s *= weight
		weight = s * gain
		if weight > 1.0 {
			weight = 1.0
		} else if weight < 0.0 {
			weight = 0.0
		}
		sum += float64(s * amp)
		maxAmp += amp
		amp *= persistence
		freq *= lacunarity
	}
	return sum / maxAmp
}

// Billow2D evaluates normalized 2D billow fractal noise using Simplex2D,
// producing soft cloud-like lobes in [-1, 1].
func (n *Noise) Billow2D(x, y float64, cfg FractalConfig) float64 {
	octaves, freq, lacunarity, persistence := resolveFractalConfig(cfg)
	var sum, maxAmp float64
	amp := 1.0
	for range octaves {
		s := float64(2.0*math.Abs(n.Simplex2D(x*freq, y*freq))) - 1.0
		sum += float64(s * amp)
		maxAmp += amp
		amp *= persistence
		freq *= lacunarity
	}
	return sum / maxAmp
}

// DomainWarp2D displaces (x, y) by two decorrelated 2D FBM fields scaled by
// strength, returning the warped coordinates (wx, wy). Passing (wx, wy) to
// another noise method (or chaining multiple DomainWarp2D calls) creates
// organic swirls, marble and distorted terrain features.
func (n *Noise) DomainWarp2D(x, y, strength float64, cfg FractalConfig) (wx, wy float64) {
	if strength == 0 {
		return x, y
	}
	dx := n.FBM2D(x, y, cfg)
	dy := n.fbm2DOffset(x, y, domainWarpOffsetX, domainWarpOffsetY, cfg)
	return x + float64(dx*strength), y + float64(dy*strength)
}

func (n *Noise) fbm2DOffset(x, y, ox, oy float64, cfg FractalConfig) float64 {
	octaves, freq, lacunarity, persistence := resolveFractalConfig(cfg)
	var sum, maxAmp float64
	amp := 1.0
	for range octaves {
		sx := float64(x*freq) + ox
		sy := float64(y*freq) + oy
		sum += float64(n.Simplex2D(sx, sy) * amp)
		maxAmp += amp
		amp *= persistence
		freq *= lacunarity
	}
	return sum / maxAmp
}

// FillGrid2D populates dst in row-major order (dst[gy*width+gx]) with samples
// produced by sample at coordinates (originX+gx*stepX, originY+gy*stepY).
//
// It performs zero heap allocations. If width <= 0 or height <= 0, FillGrid2D
// is a no-op. It panics if sample is nil or len(dst) < width*height.
func (n *Noise) FillGrid2D(dst []float64, width, height int, originX, originY, stepX, stepY float64, sample func(x, y float64) float64) {
	if sample == nil {
		panic("random: FillGrid2D sample function must not be nil")
	}
	if width <= 0 || height <= 0 {
		return
	}
	checkGridDst(dst, width, height)
	dst = dst[:width*height]
	for gy := range height {
		y := originY + float64(float64(gy)*stepY)
		row := dst[gy*width : (gy+1)*width]
		for gx := range width {
			x := originX + float64(float64(gx)*stepX)
			row[gx] = sample(x, y)
		}
	}
}

// FillValue2D populates dst in row-major order with 2D value noise samples at
// (originX+gx*stepX, originY+gy*stepY) without heap allocations.
func (n *Noise) FillValue2D(dst []float64, width, height int, originX, originY, stepX, stepY float64) {
	if width <= 0 || height <= 0 {
		return
	}
	checkGridDst(dst, width, height)
	dst = dst[:width*height]
	for gy := range height {
		y := originY + float64(float64(gy)*stepY)
		row := dst[gy*width : (gy+1)*width]
		for gx := range width {
			x := originX + float64(float64(gx)*stepX)
			row[gx] = n.Value2D(x, y)
		}
	}
}

// FillSimplex2D populates dst in row-major order with 2D simplex noise samples
// at (originX+gx*stepX, originY+gy*stepY) without heap allocations.
func (n *Noise) FillSimplex2D(dst []float64, width, height int, originX, originY, stepX, stepY float64) {
	if width <= 0 || height <= 0 {
		return
	}
	checkGridDst(dst, width, height)
	dst = dst[:width*height]
	for gy := range height {
		y := originY + float64(float64(gy)*stepY)
		row := dst[gy*width : (gy+1)*width]
		for gx := range width {
			x := originX + float64(float64(gx)*stepX)
			row[gx] = n.Simplex2D(x, y)
		}
	}
}

// FillFBM2D populates dst in row-major order with 2D FBM simplex noise samples
// at (originX+gx*stepX, originY+gy*stepY) without heap allocations.
func (n *Noise) FillFBM2D(dst []float64, width, height int, originX, originY, stepX, stepY float64, cfg FractalConfig) {
	if width <= 0 || height <= 0 {
		return
	}
	checkGridDst(dst, width, height)
	octaves, baseFreq, lacunarity, persistence := resolveFractalConfig(cfg)
	var maxAmp float64
	amp := 1.0
	for range octaves {
		maxAmp += amp
		amp *= persistence
	}
	dst = dst[:width*height]
	for gy := range height {
		y := originY + float64(float64(gy)*stepY)
		row := dst[gy*width : (gy+1)*width]
		for gx := range width {
			x := originX + float64(float64(gx)*stepX)
			var sum float64
			a := 1.0
			f := baseFreq
			for range octaves {
				sum += float64(n.Simplex2D(x*f, y*f) * a)
				a *= persistence
				f *= lacunarity
			}
			row[gx] = sum / maxAmp
		}
	}
}

func checkGridDst(dst []float64, width, height int) {
	if width > len(dst)/height {
		panic("random: destination slice too small for grid")
	}
}

func resolveFractalConfig(cfg FractalConfig) (octaves int, freq, lacunarity, persistence float64) {
	octaves = cfg.Octaves
	if octaves <= 0 {
		octaves = 4
	}
	freq = cfg.Frequency
	if freq <= 0 {
		freq = 1.0
	}
	lacunarity = cfg.Lacunarity
	if lacunarity <= 0 {
		lacunarity = 2.0
	}
	persistence = cfg.Persistence
	if persistence <= 0 {
		if cfg.Gain > 0 {
			persistence = cfg.Gain
		} else {
			persistence = 0.5
		}
	}
	return octaves, freq, lacunarity, persistence
}

func resolveRidgedConfig(cfg FractalConfig) (octaves int, freq, lacunarity, persistence, gain float64) {
	octaves = cfg.Octaves
	if octaves <= 0 {
		octaves = 4
	}
	freq = cfg.Frequency
	if freq <= 0 {
		freq = 1.0
	}
	lacunarity = cfg.Lacunarity
	if lacunarity <= 0 {
		lacunarity = 2.0
	}
	persistence = cfg.Persistence
	if persistence <= 0 {
		persistence = 0.5
	}
	gain = cfg.Gain
	if gain <= 0 {
		gain = 2.0
	}
	return octaves, freq, lacunarity, persistence, gain
}

func splitMix64Range(state *uint64, n uint64) uint64 {
	hi, lo := bits.Mul64(splitMix64(state), n)
	if lo < n {
		thresh := -n % n
		for lo < thresh {
			hi, lo = bits.Mul64(splitMix64(state), n)
		}
	}
	return hi
}

// fade computes the Perlin quintic Hermite curve 6t^5 - 15t^4 + 10t^3 with
// explicit float64 conversions to prevent FMA contraction.
func fade(t float64) float64 {
	t2 := t * t
	t3 := t2 * t
	return t3 * (float64(t*(float64(t*6.0)-15.0)) + 10.0)
}

// lerp linearly interpolates between a and b by t without FMA contraction.
func lerp(t, a, b float64) float64 {
	return a + float64(t*(b-a))
}

// grad3Dot computes the dot product of the h-th cube-edge gradient vector in
// {(±1,±1,0), (±1,0,±1), (0,±1,±1)} with (x, y, z) using pure additions and
// subtractions (no multiplications or FMA).
func grad3Dot(h uint8, x, y, z float64) float64 {
	switch h {
	case 0:
		return x + y
	case 1:
		return -x + y
	case 2:
		return x - y
	case 3:
		return -x - y
	case 4:
		return x + z
	case 5:
		return -x + z
	case 6:
		return x - z
	case 7:
		return -x - z
	case 8:
		return y + z
	case 9:
		return -y + z
	case 10:
		return y - z
	default:
		return -y - z
	}
}
