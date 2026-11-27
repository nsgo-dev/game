// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/status/frozen.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package status

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/pixelfx"
)

const (
	frozenDuration = 2.0
	frozenShiver   = 1.0
)

var frozenStiff = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.9), pixelfx.Crouch.Is(1), pixelfx.FX.Is(-6), pixelfx.FY.Is(-15), pixelfx.BX.Is(7), pixelfx.BY.Is(-15), pixelfx.FFX.Is(-2), pixelfx.FBX.Is(3))

// Frozen is chilled to the bone: frost ferns creep over the body, icicles hang from chin and hands and drip, breath steams, snow settles, a shiver every second, frost spreads underfoot. It is registered as "status/frozen".
var Frozen = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Frozen",
		Desc:     "Chilled to the bone: frost ferns creep over the body, icicles hang from chin and hands and drip, breath steams, snow settles, a shiver every second, frost spreads underfoot.",
		Tags:     []string{"status", "ice", "freeze", "frozen", "cold", "chill", "debuff", "loop"},
		Size:     [2]int{72, 64},
		Origin:   [2]int{36, 54},
		Duration: frozenDuration,
		Loop:     true,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#151a3d", "#212f6b", "#2c58aa", "#3a8bd8", "#62c8f4", "#a6ecff", "#dcfbff", "#ffffff")},
			{Name: "seed", Default: pixelfx.Num(4)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: 0, Pose: frozenStiff.With(pixelfx.Crouch.Is(2))},
			{T: 0.22, Pose: frozenStiff},
			{T: 1, Pose: frozenStiff},
			{T: 1, Pose: frozenStiff.With(pixelfx.Crouch.Is(2)), How: "snap"},
			{T: 1.22, Pose: frozenStiff},
			{T: 2, Pose: frozenStiff.With(pixelfx.Crouch.Is(2))},
		},
	},
	ActorAlpha: func(float64, *pixelfx.Options) float64 { return 0 },
	New:        newFrozen,
}

func init() { pixelfx.Register("status/frozen", Frozen) }

func frozenSince(t float64) float64 { return pixelfx.Wrap(t, frozenShiver) }

func frozenShake(t float64) float64 {
	if frozenSince(t) < 0.22 {
		if int(math.Floor(t*50))%2 != 0 {
			return 1
		}
		return -1
	}
	return 0
}

// timSort reproduces V8's Array.prototype.sort (TimSort) comparison order, which frozen.js relies on when shuffling roots with .sort(() => R.next() - 0.5).
func timSort[T any](a []T, cmp func(x, y T) float64) {
	n := len(a)
	if n < 2 {
		return
	}
	minRun := computeMinRunLength(n)
	type run struct{ base, length int }
	var stack []run
	remaining := n
	low := 0
	for remaining > 0 {
		runLen := countAndMakeRun(a, low, low+remaining, cmp)
		if runLen < minRun {
			force := min(remaining, minRun)
			binaryInsertionSort(a, low, low+runLen, low+force, cmp)
			runLen = force
		}
		stack = append(stack, run{low, runLen})
		for len(stack) > 1 {
			m := len(stack) - 2
			if (m > 0 && stack[m-1].length <= stack[m].length+stack[m+1].length) ||
				(m > 1 && stack[m-2].length <= stack[m-1].length+stack[m].length) {
				if stack[m-1].length < stack[m+1].length {
					m--
				}
			} else if stack[m].length > stack[m+1].length {
				break
			}
			timMerge(a, stack[m].base, stack[m].length, stack[m+1].base, stack[m+1].length, cmp)
			stack[m].length += stack[m+1].length
			stack = append(stack[:m+1], stack[m+2:]...)
		}
		low += runLen
		remaining -= runLen
	}
	for len(stack) > 1 {
		m := len(stack) - 2
		if m > 0 && stack[m-1].length < stack[m+1].length {
			m--
		}
		timMerge(a, stack[m].base, stack[m].length, stack[m+1].base, stack[m+1].length, cmp)
		stack[m].length += stack[m+1].length
		stack = append(stack[:m+1], stack[m+2:]...)
	}
}

func computeMinRunLength(n int) int {
	r := 0
	for n >= 64 {
		r |= n & 1
		n >>= 1
	}
	return n + r
}

func countAndMakeRun[T any](a []T, low, high int, cmp func(x, y T) float64) int {
	runHigh := low + 1
	if runHigh == high {
		return 1
	}
	if cmp(a[runHigh], a[low]) < 0 {
		runHigh++
		for runHigh < high && cmp(a[runHigh], a[runHigh-1]) < 0 {
			runHigh++
		}
		for i, j := low, runHigh-1; i < j; i, j = i+1, j-1 {
			a[i], a[j] = a[j], a[i]
		}
	} else {
		runHigh++
		for runHigh < high && cmp(a[runHigh], a[runHigh-1]) >= 0 {
			runHigh++
		}
	}
	return runHigh - low
}

func binaryInsertionSort[T any](a []T, low, start, high int, cmp func(x, y T) float64) {
	if start == low {
		start++
	}
	for ; start < high; start++ {
		pivot := a[start]
		left, right := low, start
		for left < right {
			mid := left + ((right - left) >> 1)
			if cmp(pivot, a[mid]) < 0 {
				right = mid
			} else {
				left = mid + 1
			}
		}
		copy(a[left+1:start+1], a[left:start])
		a[left] = pivot
	}
}

func timMerge[T any](a []T, baseA, lenA, baseB, lenB int, cmp func(x, y T) float64) {
	tmp := make([]T, lenA)
	copy(tmp, a[baseA:baseA+lenA])
	ia, ib, out := 0, baseB, baseA
	endB := baseB + lenB
	for ia < lenA && ib < endB {
		if cmp(a[ib], tmp[ia]) < 0 {
			a[out] = a[ib]
			ib++
		} else {
			a[out] = tmp[ia]
			ia++
		}
		out++
	}
	copy(a[out:], tmp[ia:])
}

type frostFern struct {
	x, y int
	u    float64
	i    int
}

type frostIcicle struct {
	x, y       int
	len, phase float64
	period     float64
}

type frostSnow struct {
	x, y, d int
}

type frozenPlan struct {
	ferns   []frostFern
	icicles []frostIcicle
	snow    []frostSnow
	mouth   [2]float64
}

func frozenPlanOf(a *pixelfx.Actor, S *actorShape, E []eye, seed float64) frozenPlan {
	H, W := S.h, S.w
	R := pixelfx.NewRng(seed*11 + 1)
	var roots [][3]int
	for _, s := range S.side {
		if float64(s[1]) > float64(H)*0.2 && float64(s[1]) < float64(H)*0.85 {
			roots = append(roots, s)
		}
	}
	timSort(roots, func(_, _ [3]int) float64 { return R.Next() - 0.5 })
	if len(roots) > 6 {
		roots = roots[:6]
	}
	var ferns []frostFern
	for i, rt := range roots {
		x, y, dir := float64(rt[0]), float64(rt[1]), rt[2]
		ang := pi + 0.5
		if dir < 0 {
			ang = -0.5
		}
		ang += R.Range(-0.4, 0.4)
		for s := 0; s < 9; s++ {
			x += math.Cos(ang) * 1.1
			y += math.Sin(ang) * 1.1
			px, py := int(pixelfx.Round(x)), int(pixelfx.Round(y))
			if !S.on(px, py) {
				break
			}
			ferns = append(ferns, frostFern{px, py, float64(s) / 9, i})
			if s%2 == 1 {
				for _, side := range [2]float64{-1, 1} {
					ba := ang + side*1.05
					L := 1 + int(pixelfx.Round(R.Range(0, 2)*(1-float64(s)/9)))
					for k := 1; k <= L; k++ {
						bx := int(pixelfx.Round(x + math.Cos(ba)*float64(k)))
						by := int(pixelfx.Round(y + math.Sin(ba)*float64(k)))
						if S.on(bx, by) {
							ferns = append(ferns, frostFern{bx, by, (float64(s) + float64(k)*0.6) / 9, i})
						}
					}
				}
			}
			ang += R.Range(-0.25, 0.25)
		}
	}
	clear := func(x, y int) int {
		n := 0
		for n < 7 && y+1+n < H && !S.on(x, y+1+n) {
			n++
		}
		return n
	}
	var hangs [][3]int
	for _, pt := range S.body {
		x, y := pt[0], pt[1]
		if !S.on(x, y+1) && S.on(x, y-1) && float64(y) < float64(H)*0.72 && y > 4 {
			c := clear(x, y)
			if c >= 4 {
				hangs = append(hangs, [3]int{x, y, c})
			}
		}
	}
	best := func(f func(h [3]int) bool, key func(p, q [3]int) int) ([3]int, bool) {
		var list [][3]int
		for _, h := range hangs {
			if f(h) {
				list = append(list, h)
			}
		}
		if len(list) == 0 {
			return [3]int{}, false
		}
		slices.SortStableFunc(list, key)
		return list[0], true
	}
	ascX := func(p, q [3]int) int { return p[0] - q[0] }
	descX := func(p, q [3]int) int { return q[0] - p[0] }
	type cand struct {
		h  [3]int
		ok bool
	}
	c0, ok0 := best(func(h [3]int) bool { return float64(h[1]) < float64(H)*0.36 }, ascX)
	c1, ok1 := best(func(h [3]int) bool { return float64(h[1]) < float64(H)*0.36 }, descX)
	c2, ok2 := best(func(h [3]int) bool {
		return float64(h[1]) >= float64(H)*0.36 && float64(h[1]) < float64(H)*0.5 && float64(h[0]) < float64(W)/2
	}, descX)
	c3, ok3 := best(func(h [3]int) bool {
		return float64(h[1]) >= float64(H)*0.36 && float64(h[1]) < float64(H)*0.5 && float64(h[0]) >= float64(W)/2
	}, ascX)
	c4, ok4 := best(func(h [3]int) bool { return float64(h[1]) >= float64(H)*0.5 && float64(h[0]) < float64(W)/2 }, ascX)
	c5, ok5 := best(func(h [3]int) bool { return float64(h[1]) >= float64(H)*0.5 && float64(h[0]) >= float64(W)/2 }, descX)
	var icicles []frostIcicle
	idx := 0
	for _, c := range [6]cand{{c0, ok0}, {c1, ok1}, {c2, ok2}, {c3, ok3}, {c4, ok4}, {c5, ok5}} {
		if !c.ok {
			continue
		}
		per := 2.0
		if idx%2 != 0 {
			per = 1
		}
		ln := math.Min(float64(c.h[2]-1), R.Range(3, 5.5))
		ph := R.Next()
		icicles = append(icicles, frostIcicle{x: c.h[0], y: c.h[1] + 1, len: ln, phase: ph, period: per})
		idx++
	}
	var snow []frostSnow
	for _, pt := range S.up {
		if float64(pt[1]) < float64(H)*0.6 {
			d := 1
			if pixelfx.Rand(float64(pt[0]), seed, 3) < 0.45 {
				d = 2
			}
			snow = append(snow, frostSnow{pt[0], pt[1], d})
		}
	}
	mouth := [2]float64{float64(W) / 2, float64(H) * 0.38}
	if len(E) == 2 {
		mouth = [2]float64{float64(E[0].x1+E[1].x0+1) / 2, float64(max(E[0].y1, E[1].y1) + 2)}
	}
	return frozenPlan{ferns: ferns, icicles: icicles, snow: snow, mouth: mouth}
}

func frozenFrost(p *pixelfx.Painter, C []uint32, t, reach float64) {
	for x := -math.Ceil(reach) - 1; x <= reach+1; x++ {
		q := math.Abs(x+0.5) / reach
		h := pixelfx.Rand(x, 17)
		k := 1 - q*q
		if q > 1+h*0.15 {
			continue
		}
		tw := math.Sin((t/frozenDuration)*tau*2+pixelfx.Rand(x, 3)*tau) > 0.94
		col := C[6]
		if tw || h < 0.3 {
			col = C[7]
		} else if q > 0.85 {
			col = C[5]
		}
		p.Px(x, 0, col)
		if h < 0.3*k {
			p.Px(x, -1, C[6])
		}
		for j, j1 := 1.0, 1+pixelfx.Round(2*k); j <= j1; j++ {
			jc := C[4]
			if j == 1 {
				jc = C[5]
			}
			p.Dot(x, j, jc, (1.05-j/4)*0.85)
		}
	}
}

func newFrozen(o *pixelfx.Options) pixelfx.Instance {
	C := pixelfx.Fit(o.Palette(), 8)
	seed := o.Seed()
	a := o.Actor
	S := shapeOf(a)
	E := eyesOf(a, S)
	P := frozenPlanOf(a, S, E, seed)
	H := float64(S.h)
	return &pixelfx.Funcs{
		DrawBackFunc: func(p *pixelfx.Painter, t float64) {
			reach := 13 + 3.5*math.Sin((t/frozenDuration)*tau)
			p.Shade(-28, -3, 28, 6, func(x, y float64, out *pixelfx.Out) uint32 {
				d := pixelfx.Hypot(x/24, (y-0.5)/4)
				if d < 1 {
					out.L = C[4]
					out.K = 0.24 * (1 - d) * (1 - d)
				}
				return 0
			})
			frozenFrost(p, C, t, reach)
		},
		DrawFunc: func(p *pixelfx.Painter, t float64) {
			dx := frozenShake(t)
			ph := frozenSince(t)
			X := func(mx float64) float64 { return a.X + mx + dx }
			Y := func(my float64) float64 { return a.Y + my }
			for _, pt := range S.body {
				mx, my := pt[0], pt[1]
				c := colorAt(a, mx, my)
				l := pixelfx.Luma(c)
				ice := C[int(pixelfx.ClampTo(1+pixelfx.Round(l*5.6), 1, 7))]
				rim := !S.on(mx-1, my) || !S.on(mx, my-1) || !S.on(mx+1, my)
				var col uint32
				if rim && l < 0.3 {
					if float64(my) < H*0.5 || !S.on(mx-1, my) {
						col = C[5]
					} else {
						col = C[3]
					}
				} else {
					col = pixelfx.Mix(c, ice, 0.38)
				}
				p.Px(X(float64(mx)), Y(float64(my)), col)
			}
			grow := 0.2 + 0.65*pixelfx.Smooth(0.05, 0.95, ph)
			for _, f := range P.ferns {
				if f.u < grow {
					col := C[5]
					if f.u > grow-0.1 {
						col = pixelfx.White
					} else if pixelfx.Rand(float64(f.x), float64(f.y), 5) < 0.5 {
						col = C[6]
					}
					p.Px(X(float64(f.x)), Y(float64(f.y)), col)
				}
			}
			for _, sn := range P.snow {
				p.Px(X(float64(sn.x)), Y(float64(sn.y))-1, C[7])
				if sn.d > 1 {
					col := C[6]
					if pixelfx.Rand(float64(sn.x), float64(sn.y), 9) < 0.5 {
						col = C[7]
					}
					p.Px(X(float64(sn.x)), Y(float64(sn.y))-2, col)
				}
			}
			for _, ic := range P.icicles {
				per := ic.period
				u := pixelfx.Wrap(t/per+ic.phase, 1)
				L := ic.len * (0.55 + 0.45*pixelfx.Smooth(0, 0.7, u))
				x, y0 := X(float64(ic.x)), Y(float64(ic.y))
				for j := 0.0; j < L; j++ {
					v := j / L
					col := C[6]
					if v > 0.75 {
						col = pixelfx.White
					} else if v < 0.3 {
						col = C[7]
					}
					p.Px(x, y0+j, col)
					if v < 0.5 {
						p.Px(x+1, y0+j, C[4])
					}
				}
				if u > 0.7 && u < 0.86 {
					p.Px(x, y0+L, C[6])
				} else if u >= 0.86 {
					f := (u - 0.86) * per
					y := y0 + L + 0.5*760*f*f
					if y < -1 {
						p.Px(x, y, C[6])
						p.Px(x, y-1, C[5])
					}
				}
				p.Glow(x, y0+L*0.6, 4, C[5], 0.18)
			}
			mx0, my0 := P.mouth[0], P.mouth[1]
			for i := 0; i < 4; i++ {
				age := ph - 0.35 - float64(i)*0.05
				if age < 0 || age > 0.65 {
					continue
				}
				u := age / 0.65
				x := X(mx0) + 1 + travel(16, 0, 2.2, age) + float64(i)
				y := Y(my0) - travel(6, 3, 1, age) - float64(i)*0.5
				rad := 0.8 + 2.4*math.Sqrt(u)
				p.Disc(x, y, rad, C[7], 0.5*(1-u)*math.Min(1, u*8))
				p.Disc(x-rad*0.2, y-rad*0.3, rad*0.5, pixelfx.White, 0.4*(1-u))
			}
			pixelfx.Cycle(t, frozenDuration, 9, seed+2, func(r *pixelfx.Rng) float64 { return r.Range(1.2, 1.8) }, func(age, _ float64, r *pixelfx.Rng, _ int) {
				x := r.Range(-14, 14) + math.Sin(age*3+r.Next()*tau)*1.5
				y := a.Y - 16 + age*14
				if y > a.Y+2 {
					return
				}
				col := C[6]
				if r.Next() < 0.5 {
					col = pixelfx.White
				}
				p.Dot(x, y, col, math.Min(1, age*4))
			})
			if ph < 0.5 {
				for i := 0; i < 8; i++ {
					side := -1.0
					if i%2 != 0 {
						side = 1
					}
					vx := side * (20 + 30*pixelfx.Rand(float64(i), 4))
					vy := -(20 + 30*pixelfx.Rand(float64(i), 5))
					x := side*(6+6*pixelfx.Rand(float64(i), 6)) + vx*ph
					y := a.Y + 3*pixelfx.Rand(float64(i), 7) + vy*ph + 0.5*300*ph*ph
					col := pixelfx.White
					if i%3 != 0 {
						col = C[6]
					}
					p.Dot(x, y, col, 1.2-ph*2)
				}
			}
			if ph < 0.25 {
				pixelfx.Sparkle(p, X(pixelfx.Round(float64(S.w)*0.3)), Y(4), pixelfx.Round(3*(1-ph/0.25)), C[6], C[5], 0.4)
			}
			p.Glow(0, a.Y+H*0.45, 30, C[4], 0.2+0.12*math.Exp(-ph/0.2))
			p.Glow(0, a.Y+2, 16, C[5], 0.1)
		},
	}
}
