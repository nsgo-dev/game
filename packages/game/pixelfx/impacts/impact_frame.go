// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/impacts/impact-frame.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.

package impacts

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// ImpactFrame is an anime impact: two frames of white with black burst lines rushing at the hit, two frames inverted, then the lines pull out past the edges and vanish. It is registered as "impacts/impact-frame".
var ImpactFrame = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Impact frame",
		Desc:     "An anime impact: two frames of white with black burst lines rushing at the hit, two frames inverted, then the lines pull out past the edges and vanish.",
		Tags:     []string{"impact", "hit", "anime", "manga", "flash", "speed-lines", "screen", "one-shot"},
		Size:     [2]int{impactW, impactH},
		Origin:   [2]int{impactOX, impactOY},
		Duration: 0.3,
		FPS:      30,
		BG:       "black",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#000000", "#ffffff")},
			{Name: "lines", Default: pixelfx.Num(64)},
			{Name: "seed", Default: pixelfx.Num(19)},
		},
	},
	New: newImpactFrame,
}

func init() { pixelfx.Register("impacts/impact-frame", ImpactFrame) }

// The frame's size and the origin in it: the hit is at the middle of the screen.
const (
	impactW, impactH   = 320, 180
	impactOX, impactOY = 160, 90
)

// impactFrameLen is one frame at 30 fps: the beats are whole frames, 2 white, 2 inverted, 4 pulling out.
var impactFrameLen = 1.0 / 30

// burstLine is a wedge aimed at the hit from past the edges: its angle, where it starts and its width.
type burstLine struct{ a, inner, w float64 }

// impactBurst returns n burst lines, each its own width and reach, in angle order.
func impactBurst(R *pixelfx.Rng, n int) []burstLine {
	out := make([]burstLine, n)
	for i := range out {
		thick := R.Chance(0.18)
		a := ((float64(i) + R.Range(0.1, 0.9)) / float64(n)) * pixelfx.Tau
		var l burstLine
		l.a = a
		if thick {
			l.inner = R.Range(30, 48)
			l.w = R.Range(5, 8)
		} else {
			l.inner = R.Range(18, 70)
			l.w = R.Range(1.2, 3.6)
		}
		out[i] = l
	}
	return out
}

// impactSpikes is the jagged burst at the middle: a spiky star whose points shift every frame.
func impactSpikes(a, ph float64) float64 {
	return 10 + 6*math.Abs(math.Sin(a*5+ph)) + 4*math.Abs(math.Sin(a*11+ph*2.7)) + 2*math.Sin(a*23+ph)
}

func newImpactFrame(o *pixelfx.Options) pixelfx.Instance {
	C, R := pixelfx.Fit(o.Palette(), 3), pixelfx.NewRng(o.Seed())
	INK, PAPER := C[0], C[2]
	W, H := impactW, impactH
	x0, y0 := -float64(impactOX), -float64(impactOY)
	n := max(int(o.Num("lines")), 0)
	sets := [2][]burstLine{impactBurst(R, n), impactBurst(R, n)}
	var jit [2][2]float64
	for i := range jit {
		jit[i][0] = pixelfx.Round(R.Range(-2, 2))
		jit[i][1] = pixelfx.Round(R.Range(-1, 1))
	}
	ph := [4]float64{R.Range(0, pixelfx.Tau), R.Range(0, pixelfx.Tau), R.Range(0, pixelfx.Tau), R.Range(0, pixelfx.Tau)}
	// every frame is fixed, so work each one out once: which pixels are ink (1) or paper (2), 0 for nothing
	var masks [8][]uint8
	for f := range 8 {
		m, set := make([]uint8, W*H), sets[f%2]
		var jx, jy float64
		if f < 4 {
			jx, jy = jit[f%2][0], jit[f%2][1]
		}
		reach, thin := 0.0, 1.0
		if f >= 4 {
			reach, thin = math.Pow(float64(f-3)/4.5, 1.5), 1-float64(f-3)/5.5
		}
		for j := range H {
			for i := range W {
				X, Y := x0+float64(i)+0.5-jx, y0+float64(j)+0.5-jy
				rho, ang := pixelfx.Hypot(X, Y*1.35), math.Atan2(Y*1.35, X)
				line := false
				if n > 0 {
					slot := int(math.Floor(math.Mod(math.Mod(ang, pixelfx.Tau)+pixelfx.Tau, pixelfx.Tau) / (pixelfx.Tau / float64(n))))
					for k := -1; k <= 1 && !line; k++ {
						l := set[((slot+k)%n+n)%n]
						d := math.Mod(math.Mod(ang-l.a, pixelfx.Tau)+pixelfx.Tau, pixelfx.Tau)
						if d > pi {
							d = pixelfx.Tau - d
						}
						inner := l.inner + reach*150
						hw := l.w * pixelfx.Clamp((rho-inner)/40) * thin
						line = rho > inner && d*rho < math.Max(0.5, hw)
					}
				}
				var v uint8
				if f < 4 {
					r2 := pixelfx.Hypot(X, Y*1.1)
					core := impactSpikes(math.Atan2(Y*1.1, X), ph[f])
					if f == 0 {
						core *= 0.85
					} else {
						core *= 1 + float64(f)*0.08
					}
					switch {
					case r2 < core && r2 < 4+float64(f):
						v = 2
					case r2 < core:
						v = 1
					case line:
						v = 1
					default:
						v = 2
					}
				} else if line {
					v = 2
				}
				m[j*W+i] = v
			}
		}
		masks[f] = m
	}
	t := 0.0
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) { t += dt },
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			f := int(math.Floor(t/impactFrameLen + 1e-6))
			if f >= 8 {
				return
			}
			// two frames of ink on paper, two inverted, then paper lines pulling out over the scene
			m, inv := masks[f], f == 2 || f == 3
			ink, paper := INK, PAPER
			if inv {
				ink, paper = PAPER, INK
			}
			if f >= 4 {
				paper = PAPER
			}
			p.Shade(x0, y0, x0+float64(W), y0+float64(H), func(x, y float64, _ *pixelfx.Out) uint32 {
				i, j := int(math.Floor(x-x0)), int(math.Floor(y-y0))
				if i < 0 || j < 0 || i >= W || j >= H {
					return 0
				}
				switch m[j*W+i] {
				case 1:
					return ink
				case 2:
					return paper
				}
				return 0
			})
		},
	}
}
