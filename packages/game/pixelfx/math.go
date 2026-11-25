// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/core/math.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See LICENSE and NOTICE.md.

package pixelfx

import "math"

// Numbers, easing, randomness and noise. Everything here is pure and deterministic, so an effect drawn at time t always looks the same. Numbers are float64 like JavaScript's, and the integer helpers follow JavaScript's 32-bit conversions, so the port draws what pixelfx draws.

// Tau is a full turn in radians.
const Tau = math.Pi * 2

// Clamp returns x held to [0, 1].
func Clamp(x float64) float64 { return ClampTo(x, 0, 1) }

// ClampTo returns x held to [a, b].
func ClampTo(x, a, b float64) float64 {
	if x < a {
		return a
	}
	if x > b {
		return b
	}
	return x
}

// Lerp returns the point t of the way from a to b.
func Lerp(a, b, t float64) float64 { return a + (b-a)*t }

// InvLerp returns how far x is from a to b, 0 when a and b are equal.
func InvLerp(a, b, x float64) float64 {
	if b == a {
		return 0
	}
	return (x - a) / (b - a)
}

// Remap maps x from [a0, a1] to [b0, b1].
func Remap(x, a0, a1, b0, b1 float64) float64 { return Lerp(b0, b1, InvLerp(a0, a1, x)) }

// Fract returns the fractional part of x, x - floor(x).
func Fract(x float64) float64 { return x - math.Floor(x) }

// Wrap returns x modulo m, always on the sign of m.
func Wrap(x, m float64) float64 { return math.Mod(math.Mod(x, m)+m, m) }

// Seg returns the progress 0..1 of t through the window [t0, t1]: 0 before it, 1 after it.
func Seg(t, t0, t1 float64) float64 { return Clamp((t - t0) / (t1 - t0)) }

// Smooth returns the Hermite smoothstep of x between e0 and e1.
func Smooth(e0, e1, x float64) float64 {
	t := Seg(x, e0, e1)
	return t * t * (3 - 2*t)
}

// Bump returns 0 → 1 → 0 over the window [t0, t1] (a sine bump).
func Bump(t, t0, t1 float64) float64 {
	if t <= t0 || t >= t1 {
		return 0
	}
	return math.Sin(math.Pi * Seg(t, t0, t1))
}

// PingPong returns x bounced between 0 and 1.
func PingPong(x float64) float64 { return 1 - math.Abs(Wrap(x, 2)-1) }

// Dist returns the distance between (x0, y0) and (x1, y1).
func Dist(x0, y0, x1, y1 float64) float64 { return Hypot(x1-x0, y1-y0) }

// AngleTo returns the angle from (x0, y0) to (x1, y1) in radians.
func AngleTo(x0, y0, x1, y1 float64) float64 { return math.Atan2(y1-y0, x1-x0) }

// Hypot returns sqrt(x*x + y*y) the way JavaScript's Math.hypot computes it, scaled by the larger value and summed with Kahan compensation, so a port gets the same last bit.
func Hypot(x, y float64) float64 { return hypot(x, y, 0, 2) }

// Hypot3 returns sqrt(x*x + y*y + z*z) the way JavaScript's Math.hypot computes it.
func Hypot3(x, y, z float64) float64 { return hypot(x, y, z, 3) }

func hypot(x, y, z float64, n int) float64 {
	vals := [3]float64{math.Abs(x), math.Abs(y), math.Abs(z)}
	m := 0.0
	nan := false
	for i := range n {
		v := vals[i]
		if math.IsInf(v, 0) {
			return math.Inf(1)
		}
		if math.IsNaN(v) {
			nan = true
		} else if v > m {
			m = v
		}
	}
	if nan {
		return math.NaN()
	}
	if m == 0 {
		return 0
	}
	sum, comp := 0.0, 0.0
	for i := range n {
		v := vals[i] / m
		summand := float64(v*v) - comp
		pre := sum + summand
		comp = (pre - sum) - summand
		sum = pre
	}
	return math.Sqrt(sum) * m
}

// Round rounds x half up, as JavaScript's Math.round does: Round(-2.5) is -2.
func Round(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	f := math.Floor(x)
	// Explicit conversion prevents arm64 FMA from fusing an inlined a*b argument with -f (e.g. Round(1.45*30) must be 44, not 43).
	if float64(x)-f >= 0.5 {
		return f + 1
	}
	return f
}

// Sign returns -1, 0 or 1 by the sign of x, as JavaScript's Math.sign does.
func Sign(x float64) float64 {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	}
	return x
}

// ToInt32 converts x to a 32-bit integer the way JavaScript's x | 0 does: truncated toward zero and wrapped modulo 2^32, with NaN and infinities giving 0.
func ToInt32(x float64) int32 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0
	}
	x = math.Trunc(x)
	if x >= math.MinInt32 && x <= math.MaxInt32 {
		return int32(x)
	}
	m := math.Mod(x, 4294967296)
	if m < 0 {
		m += 4294967296
	}
	return int32(uint32(m))
}

// ToUint32 converts x to an unsigned 32-bit integer the way JavaScript's x >>> 0 does.
func ToUint32(x float64) uint32 { return uint32(ToInt32(x)) }

// Easing curves: input 0..1, output 0..1 (back and elastic overshoot).
var Ease = struct {
	Linear, InQuad, OutQuad, InOutQuad, InCubic, OutCubic, InOutCubic, InQuart, OutQuart, InExpo, OutExpo, InSine, OutSine, InOutSine, InBack, OutBack, OutElastic, OutBounce func(t float64) float64
}{
	Linear:  func(t float64) float64 { return t },
	InQuad:  func(t float64) float64 { return t * t },
	OutQuad: func(t float64) float64 { return 1 - (1-t)*(1-t) },
	InOutQuad: func(t float64) float64 {
		if t < 0.5 {
			return 2 * t * t
		}
		return 1 - math.Pow(-2*t+2, 2)/2
	},
	InCubic:  func(t float64) float64 { return t * t * t },
	OutCubic: func(t float64) float64 { return 1 - math.Pow(1-t, 3) },
	InOutCubic: func(t float64) float64 {
		if t < 0.5 {
			return 4 * t * t * t
		}
		return 1 - math.Pow(-2*t+2, 3)/2
	},
	InQuart:  func(t float64) float64 { return t * t * t * t },
	OutQuart: func(t float64) float64 { return 1 - math.Pow(1-t, 4) },
	InExpo: func(t float64) float64 {
		if t <= 0 {
			return 0
		}
		return math.Pow(2, 10*t-10)
	},
	OutExpo: func(t float64) float64 {
		if t >= 1 {
			return 1
		}
		return 1 - math.Pow(2, -10*t)
	},
	InSine:    func(t float64) float64 { return 1 - math.Cos((t*math.Pi)/2) },
	OutSine:   func(t float64) float64 { return math.Sin((t * math.Pi) / 2) },
	InOutSine: func(t float64) float64 { return -(math.Cos(math.Pi*t) - 1) / 2 },
	InBack:    func(t float64) float64 { return InBack(t, 1.70158) },
	OutBack:   func(t float64) float64 { return OutBack(t, 1.70158) },
	OutElastic: func(t float64) float64 {
		if t <= 0 {
			return 0
		}
		if t >= 1 {
			return 1
		}
		return math.Pow(2, -10*t)*math.Sin((t*10-0.75)*((2*pi)/3)) + 1
	},
	OutBounce: func(t float64) float64 {
		const n, d = 7.5625, 2.75
		if t < 1/d {
			return n * t * t
		}
		if t < 2/d {
			t -= 1.5 / d
			return n*t*t + 0.75
		}
		if t < 2.5/d {
			t -= 2.25 / d
			return n*t*t + 0.9375
		}
		t -= 2.625 / d
		return n*t*t + 0.984375
	},
}

// InBack is the back-in easing with overshoot s (1.70158 in Ease.InBack).
func InBack(t, s float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	return (s+1)*t*t*t - s*t*t
}

// OutBack is the back-out easing with overshoot s (1.70158 in Ease.OutBack).
func OutBack(t, s float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	return 1 + (s+1)*math.Pow(t-1, 3) + s*math.Pow(t-1, 2)
}

// Hash returns a 32-bit integer hash of any number of integers; each is converted as JavaScript's n | 0 converts it.
func Hash(ns ...float64) uint32 {
	h := uint32(0x9e3779b9)
	for _, n := range ns {
		h = (h ^ uint32(ToInt32(n))) * 0x85ebca6b
		h ^= h >> 13
		h *= 0xc2b2ae35
		h ^= h >> 16
	}
	return h
}

// Rand returns a deterministic random number in [0, 1) from integers (seed, particle index, which value...).
func Rand(ns ...float64) float64 { return float64(Hash(ns...)) / 4294967296 }

// Rng is a seeded random generator (mulberry32), the same sequence as pixelfx's for the same seed.
type Rng struct {
	s uint32
}

// NewRng returns a generator seeded with seed, converted as JavaScript's seed >>> 0; a seed of 0 is read as 1.
func NewRng(seed float64) *Rng {
	r := &Rng{}
	r.Seed(seed)
	return r
}

// Seed restarts r from seed, as NewRng does.
func (r *Rng) Seed(seed float64) {
	r.s = ToUint32(seed)
	if r.s == 0 {
		r.s = 1
	}
}

// Next returns the next number in [0, 1).
func (r *Rng) Next() float64 {
	r.s += 0x6d2b79f5
	t := r.s
	t = (t ^ (t >> 15)) * (t | 1)
	t ^= t + (t^(t>>7))*(t|61)
	return float64(t^(t>>14)) / 4294967296
}

// Range returns a number in [a, b).
func (r *Rng) Range(a, b float64) float64 { return a + (b-a)*r.Next() }

// Int returns a whole number from a to b, both included.
func (r *Rng) Int(a, b float64) float64 { return math.Floor(r.Range(a, b+1)) }

// Chance reports true with probability p.
func (r *Rng) Chance(p float64) bool { return r.Next() < p }

// Sign returns -1 or 1 with even odds.
func (r *Rng) Sign() float64 {
	if r.Next() < 0.5 {
		return -1
	}
	return 1
}

// Gauss returns a roughly normal number, mean 0, standard deviation 1.
func (r *Rng) Gauss() float64 {
	return (r.Next() + r.Next() + r.Next() + r.Next() - 2) * 1.732
}

// Pick returns one of list, chosen by r.
func Pick[T any](r *Rng, list []T) T {
	return list[int(math.Floor(r.Next()*float64(len(list))))]
}

// RngOf returns a generator for one particle: RngOf(seed, i) gives the same numbers every frame.
func RngOf(ns ...float64) *Rng { return NewRng(float64(Hash(ns...))) }

// Noise returns smooth 1D value noise in [-1, 1].
func Noise(x, seed float64) float64 {
	i := math.Floor(x)
	f := x - i
	u := f * f * (3 - 2*f)
	return Lerp(Rand(i, seed), Rand(i+1, seed), u)*2 - 1
}

// Noise2 returns smooth 2D value noise in [-1, 1].
func Noise2(x, y, seed float64) float64 {
	ix, iy := math.Floor(x), math.Floor(y)
	fx, fy := x-ix, y-iy
	ux, uy := fx*fx*(3-2*fx), fy*fy*(3-2*fy)
	a, b, c, d := Rand(ix, iy, seed), Rand(ix+1, iy, seed), Rand(ix, iy+1, seed), Rand(ix+1, iy+1, seed)
	return Lerp(Lerp(a, b, ux), Lerp(c, d, ux), uy)*2 - 1
}

// Fbm returns fractal 2D noise in about [-1, 1], octaves layers of Noise2 (pixelfx's default is 3).
func Fbm(x, y, seed float64, octaves int) float64 {
	s, a, n := 0.0, 0.5, 0.0
	for o := range octaves {
		s += Noise2(x, y, seed+float64(o)*101) * a
		n += a
		x *= 2.03
		y *= 2.03
		a *= 0.5
	}
	return s / n
}

// TileNoise returns 2D value noise whose lattice repeats every px cells in x and py cells in y (0: no repeat). Scroll it by exactly py cells over a loop and the loop has no seam: TileNoise(x, y+phase*py, 0, py, seed).
func TileNoise(x, y, px, py, seed float64) float64 {
	ix, iy := math.Floor(x), math.Floor(y)
	fx, fy := x-ix, y-iy
	ux, uy := fx*fx*(3-2*fx), fy*fy*(3-2*fy)
	x0, x1 := ix, ix+1
	if px != 0 {
		x0, x1 = Wrap(ix, px), Wrap(ix+1, px)
	}
	y0, y1 := iy, iy+1
	if py != 0 {
		y0, y1 = Wrap(iy, py), Wrap(iy+1, py)
	}
	a, b, c, d := Rand(x0, y0, seed), Rand(x1, y0, seed), Rand(x0, y1, seed), Rand(x1, y1, seed)
	return Lerp(Lerp(a, b, ux), Lerp(c, d, ux), uy)*2 - 1
}

// TileFbm returns fractal TileNoise: every octave keeps the repeat, so scrolled fields still loop.
func TileFbm(x, y, px, py, seed float64, octaves int) float64 {
	s, a, n, k := 0.0, 0.5, 0.0, 1.0
	for o := range octaves {
		s += TileNoise(x*k, y*k, px*k, py*k, seed+float64(o)*101) * a
		n += a
		a *= 0.5
		k *= 2
	}
	return s / n
}

// LoopNoise returns noise that loops: the same value at phase 0 and phase 1, for seamless loops (pixelfx's default radius is 1).
func LoopNoise(phase, radius, seed float64) float64 {
	a := phase * Tau
	return Noise2(math.Cos(a)*radius+50, math.Sin(a)*radius+50, seed)
}

// pi is π as a float64 variable: expressions that combine it with other constants then round step by step as JavaScript's do, instead of being folded exactly at compile time.
var pi = math.Pi
