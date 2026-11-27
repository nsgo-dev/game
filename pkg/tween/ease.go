package tween

import "math"

// Ease maps normalized pass progress t in [0, 1] to an eased progress value, the same
// function type as EaseFunc. Named eases share the names of the interp values that
// resource curve keys use, so the same set is available from code and from clips.
type Ease = EaseFunc

const backS = 1.70158

func f32(v float64) float32 { return float32(v) }

// Named easing functions covering the sine, quad, cubic, quart, quint, expo, circ,
// back, elastic, bounce and spring families, each in the in, out, in-out and
// out-in variants. All of them are exact at the endpoints: f(0) == 0 and f(1) == 1.
var (
	SineIn    = makeEase(func(t float64) float64 { return 1 - math.Cos(t*math.Pi/2) })
	SineOut   = easeOutOf(SineIn)
	SineInOut = easeInOutOf(SineIn)
	SineOutIn = easeOutInOf(SineIn, SineOut)

	QuadIn    = makeEase(func(t float64) float64 { return t * t })
	QuadOut   = easeOutOf(QuadIn)
	QuadInOut = easeInOutOf(QuadIn)
	QuadOutIn = easeOutInOf(QuadIn, QuadOut)

	CubicIn    = makeEase(func(t float64) float64 { return t * t * t })
	CubicOut   = easeOutOf(CubicIn)
	CubicInOut = easeInOutOf(CubicIn)
	CubicOutIn = easeOutInOf(CubicIn, CubicOut)

	QuartIn    = makeEase(func(t float64) float64 { return t * t * t * t })
	QuartOut   = easeOutOf(QuartIn)
	QuartInOut = easeInOutOf(QuartIn)
	QuartOutIn = easeOutInOf(QuartIn, QuartOut)

	QuintIn    = makeEase(func(t float64) float64 { return t * t * t * t * t })
	QuintOut   = easeOutOf(QuintIn)
	QuintInOut = easeInOutOf(QuintIn)
	QuintOutIn = easeOutInOf(QuintIn, QuintOut)

	ExpoIn = makeEase(func(t float64) float64 {
		if t <= 0 {
			return 0
		}
		return math.Pow(2, 10*(t-1))
	})
	ExpoOut = makeEase(func(t float64) float64 {
		if t >= 1 {
			return 1
		}
		return 1 - math.Pow(2, -10*t)
	})
	ExpoInOut = easeInOutFromPair(ExpoIn, ExpoOut)
	ExpoOutIn = easeOutInOf(ExpoIn, ExpoOut)

	CircIn = makeEase(func(t float64) float64 {
		return 1 - math.Sqrt(math.Max(0, 1-t*t))
	})
	CircOut = makeEase(func(t float64) float64 {
		u := t - 1
		return math.Sqrt(math.Max(0, 1-u*u))
	})
	CircInOut = easeInOutFromPair(CircIn, CircOut)
	CircOutIn = easeOutInOf(CircIn, CircOut)

	BackIn = makeEase(func(t float64) float64 {
		return t * t * ((backS+1)*t - backS)
	})
	BackOut = makeEase(func(t float64) float64 {
		u := t - 1
		return 1 + u*u*((backS+1)*u+backS)
	})
	BackInOut = makeEase(func(t float64) float64 {
		const c2 = backS * 1.525
		if t < 0.5 {
			x := 2 * t
			return 0.5 * x * x * ((c2+1)*x - c2)
		}
		x := 2*t - 2
		return 0.5*x*x*((c2+1)*x+c2) + 1
	})
	BackOutIn = easeOutInOf(BackIn, BackOut)

	ElasticIn = makeEase(func(t float64) float64 {
		if t <= 0 || t >= 1 {
			return t
		}
		return -math.Pow(2, 10*(t-1)) * math.Sin((10*t-10.75)*elasticC4)
	})
	ElasticOut = makeEase(func(t float64) float64 {
		if t <= 0 || t >= 1 {
			return t
		}
		return math.Pow(2, -10*t)*math.Sin((10*t-0.75)*elasticC4) + 1
	})
	ElasticInOut = easeInOutFromPair(ElasticIn, ElasticOut)
	ElasticOutIn = easeOutInOf(ElasticIn, ElasticOut)

	BounceOut = makeEase(func(t float64) float64 {
		const n1 = 7.5625
		const d1 = 2.75
		switch {
		case t < 1/d1:
			return n1 * t * t
		case t < 2/d1:
			t -= 1.5 / d1
			return n1*t*t + 0.75
		case t < 2.5/d1:
			t -= 2.25 / d1
			return n1*t*t + 0.9375
		default:
			t -= 2.625 / d1
			return n1*t*t + 0.984375
		}
	})
	BounceIn = makeEase(func(t float64) float64 {
		return 1 - float64(BounceOut(f32(1-t)))
	})
	BounceInOut = easeInOutFromPair(BounceIn, BounceOut)
	BounceOutIn = easeOutInOf(BounceIn, BounceOut)

	// SpringOut overshoots with a damped oscillation that settles exactly on 1;
	// the other spring variants mirror and combine the same wave.
	SpringOut = makeEase(func(t float64) float64 {
		if t <= 0 {
			return 0
		}
		if t >= 1 {
			return 1
		}
		return 1 - (1-t)*math.Exp(-3*t)*math.Cos(6*math.Pi*t)
	})
	SpringIn    = easeOutOf(SpringOut)
	SpringInOut = easeInOutFromPair(SpringIn, SpringOut)
	SpringOutIn = easeOutInOf(SpringIn, SpringOut)
)

const elasticC4 = 2 * math.Pi / 3

// makeEase builds an Ease from a float64 easing formula, pinning the endpoints exactly.
func makeEase(in func(t float64) float64) Ease {
	return func(t float32) float32 {
		if t <= 0 || math.IsNaN(float64(t)) {
			return 0
		}
		if t >= 1 {
			return 1
		}
		return f32(in(float64(t)))
	}
}

// easeOutOf mirrors an in variant: out(t) = 1 - in(1-t).
func easeOutOf(in Ease) Ease {
	return func(t float32) float32 {
		if t <= 0 || math.IsNaN(float64(t)) {
			return 0
		}
		if t >= 1 {
			return 1
		}
		return 1 - in(1-t)
	}
}

// easeInOutOf combines an in variant's first half with its mirrored out second half.
func easeInOutOf(in Ease) Ease {
	return easeInOutFromPair(in, easeOutOf(in))
}

// easeInOutFromPair runs in on the first half scaled to [0, 0.5] and out on the
// second half scaled to [0.5, 1].
func easeInOutFromPair(in, out Ease) Ease {
	return func(t float32) float32 {
		if t <= 0 || math.IsNaN(float64(t)) {
			return 0
		}
		if t >= 1 {
			return 1
		}
		if t < 0.5 {
			return 0.5 * in(2*t)
		}
		return 0.5 + 0.5*out(2*t-1)
	}
}

// easeOutInOf runs out on the first half and in on the second half.
func easeOutInOf(in, out Ease) Ease {
	return func(t float32) float32 {
		if t <= 0 || math.IsNaN(float64(t)) {
			return 0
		}
		if t >= 1 {
			return 1
		}
		if t < 0.5 {
			return 0.5 * out(2*t)
		}
		return 0.5 + 0.5*in(2*t-1)
	}
}

// easeF64 adapts an Ease (nil uses linear) to the float64 evaluator Tween steps with.
func easeF64(e Ease) func(u float64) float64 {
	if e == nil {
		return func(u float64) float64 { return u }
	}
	return func(u float64) float64 {
		if u <= 0 {
			return 0
		}
		if u >= 1 {
			return 1
		}
		return float64(e(f32(u)))
	}
}
