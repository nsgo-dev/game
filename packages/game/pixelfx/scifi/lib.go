// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/scifi/_lib.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx's particle motion follows src/core/sim.js, which is adapted from Sprite Fusion's open-source pixel art VFX (https://www.spritefusion.com/pixel-art-vfx), MIT License, Copyright (c) 2026 Hugo Duprez.

package scifi

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Shared by the scifi effects. ghosts: the toolkit's simulated particles for loops (sparks that fall and bounce, smoke that rises and thins) worked out from their birth each frame, so a loop is a function of t alone and has no seam. Also lights that flare in loops, and electric arcs stroked the way Hugo strokes his bolts.

var (
	pi      = math.Pi
	tau     = pixelfx.Tau
	gravity = float64(pixelfx.Gravity)
)

const ghostStep = 1.0 / 240

func isSoft(k pixelfx.Kind) bool {
	return k == pixelfx.Vapour || k == pixelfx.Dust || k == pixelfx.Smoke
}

// moveGhost runs one toolkit step for one particle, with its own age standing in for the system clock (so it repeats).
func moveGhost(q *pixelfx.Particle, hops *int, dt, age float64, ground func(float64) float64) {
	switch q.K {
	case pixelfx.Fire:
		q.VY -= 95 * dt
		q.VX += math.Sin(age*17+q.Seed) * 30 * dt
		q.VX -= q.VX * 5 * dt
		q.VY -= q.VY * 3.2 * dt
	case pixelfx.Grit:
		q.VY += gravity * dt
		q.VX -= q.VX * 0.8 * dt
		g, ny := ground(q.X), q.Y+q.VY*dt
		q.X += q.VX * dt
		if q.VY > 0 && q.Y <= g && ny >= g {
			q.Y = g - 0.25
			bounced := false
			if q.VY > 130 {
				h := float64(*hops)
				*hops++
				if pixelfx.Rand(q.Seed*1000, h) < 0.4 {
					q.VY = -q.VY * 0.25
					q.VX = q.VX * 0.5
					bounced = true
				}
			}
			if !bounced {
				q.VX, q.VY = 0, 0
				q.Life = math.Min(q.Life, 0.5)
			}
		} else {
			q.Y = ny
		}
		return
	case pixelfx.Spark:
		q.VY += gravity * 0.45 * dt
		q.VX -= q.VX * 2.2 * dt
		q.VY -= q.VY * 2.2 * dt
		g, ny := ground(q.X), q.Y+q.VY*dt
		q.X += q.VX * dt
		if q.VY > 0 && q.Y <= g && ny >= g {
			q.VY = -q.VY * 0.45
			q.VX *= 0.7
			q.Y = g - 0.25
		} else {
			q.Y = ny
		}
		return
	case pixelfx.Streak:
		q.VX += q.VX * 3 * dt
		q.VY += q.VY * 3 * dt
	case pixelfx.Flake:
		q.VX -= q.VX * 2.2 * dt
		q.VY -= q.VY*2.2*dt - 35*dt
		q.X += math.Sin(age*2.5+q.Seed) * 7 * dt
	case pixelfx.Ember:
		q.VY += (-30 + math.Sin(age*3+q.Seed)*20) * dt
		q.VX += math.Cos(age*2.3+q.Seed) * 45 * dt
		q.VX -= q.VX * 1.6 * dt
		q.VY -= q.VY * 1.6 * dt
	case pixelfx.Smoke:
		rise := 16.0
		if q.Flag&pixelfx.Big != 0 {
			rise = 23
		}
		q.VY -= rise * dt
		q.VX += math.Sin(age*1.3+q.Seed) * 8 * dt
		q.VX -= q.VX * 1.6 * dt
		q.VY -= q.VY * 1.2 * dt
	case pixelfx.Mote:
		q.VX -= q.VX * 0.6 * dt
		q.VY -= q.VY * 0.6 * dt
	default:
		drag := 1.6
		if isSoft(q.K) {
			drag = 4
		}
		q.VX -= q.VX * drag * dt
		q.VY -= q.VY*drag*dt + 5*dt
	}
	q.X += q.VX * dt
	q.Y += q.VY * dt
}

type ghostOpts struct {
	ground func(float64) float64
	sparks []uint32
	haze   uint32
	hasBox bool
	box    [4]float64
}

type ghostSpec struct {
	born         float64
	k            pixelfx.Kind
	x, y, vx, vy float64
	life         float64
	col          uint32
	size         float64
	flag         pixelfx.Flag
	seed         float64
}

// ghosts simulates loopable particles from their birth each frame so loops have no seam.
type ghosts struct {
	D      float64
	ground func(float64) float64
	hasBox bool
	box    [4]float64
	ps     *pixelfx.Particles
	specs  []ghostSpec
}

func noGround(float64) float64 { return math.Inf(1) }

func newGhosts(period float64, o ghostOpts) *ghosts {
	ground := o.ground
	if ground == nil {
		ground = noGround
	}
	return &ghosts{
		D:      period,
		ground: ground,
		hasBox: o.hasBox,
		box:    o.box,
		ps:     pixelfx.NewParticles(pixelfx.SimOpts{Ground: ground, Sparks: o.sparks, Haze: o.haze}),
	}
}

func (g *ghosts) add(born float64, k pixelfx.Kind, x, y, vx, vy, life float64, col uint32, size float64, flag pixelfx.Flag) {
	n, D := float64(len(g.specs)), g.D
	// a star's twinkle runs on the clock: pick its seed so the twinkle repeats with the period too
	seed := pixelfx.Rand(n, 41) * 100
	if k == pixelfx.StarKind {
		m := math.Ceil((8 * D) / tau)
		f := (tau*m)/D - 8
		if f < 5 {
			seed = f + 5*math.Floor(pixelfx.Rand(n, 43)*19)
		}
	}
	g.specs = append(g.specs, ghostSpec{
		born: pixelfx.Wrap(born, D),
		k:    k,
		x:    x,
		y:    y,
		vx:   vx,
		vy:   vy,
		life: life,
		col:  col,
		size: size,
		flag: flag,
		seed: seed,
	})
}

func (g *ghosts) state(s *ghostSpec, age float64) (pixelfx.Particle, bool) {
	q := pixelfx.Particle{
		K: s.k, X: s.x, Y: s.y, VX: s.vx, VY: s.vy,
		Life: s.life, Max: s.life, Col: s.col, Size: s.size, Seed: s.seed, Delay: 0, Flag: s.flag,
	}
	hops := 0
	for a := 0.0; a < age-1e-9; {
		dt := math.Min(ghostStep, age-a)
		moveGhost(&q, &hops, dt, a, g.ground)
		a += dt
		q.Life -= dt
		if q.Life <= 0 {
			// a fire puff with Smokes turns into smoke when it dies, as in the toolkit
			if q.K != pixelfx.Fire || q.Flag&pixelfx.Smokes == 0 {
				return pixelfx.Particle{}, false
			}
			big := q.Flag&pixelfx.Big != 0
			var L, size float64
			var flag pixelfx.Flag
			if big {
				L = 1.3 + pixelfx.Rand(s.seed*7, 1)
				size = q.Size * 0.85
				flag = pixelfx.Big
			} else {
				L = 0.8 + pixelfx.Rand(s.seed*7, 1)*0.6
				size = q.Size * 0.7
			}
			q.K = pixelfx.Smoke
			q.VX = q.VX * 0.6
			q.VY = q.VY*0.6 - 6
			q.Life = L
			q.Max = L
			q.Col = 0
			q.Size = size
			q.Flag = flag
		}
	}
	if g.hasBox {
		if q.X < g.box[0] || q.X > g.box[2] || q.Y < g.box[1] || q.Y > g.box[3] {
			return pixelfx.Particle{}, false
		}
	}
	return q, true
}

func (g *ghosts) at(t float64) *pixelfx.Particles {
	D := g.D
	list := g.ps.List[:0]
	for i := range g.specs {
		s := &g.specs[i]
		// its age now, and in each earlier period it may still be alive from (smoke from fire lives longer)
		span := s.life
		if s.k == pixelfx.Fire && s.flag&pixelfx.Smokes != 0 {
			span = s.life + 2.4
		}
		for age := pixelfx.Wrap(t-s.born, D); age < span; age += D {
			if q, ok := g.state(s, age); ok {
				list = append(list, q)
			}
		}
	}
	g.ps.List = list
	g.ps.T = t
	return g.ps
}

// flash draws a light that flares at t0 and dies over life, as the toolkit's Lights do, for loops: k falls with life².
func flash(p *pixelfx.Painter, t, t0, x, y, r float64, c uint32, k, life float64) {
	a := t - t0
	if a < 0 || a >= life {
		return
	}
	q := 1 - a/life
	p.Glow(x, y, r*(0.55+0.45*q), c, k*q*q)
}

// since returns the smallest positive age of an event at t0 in a loop of period D, seen at t.
func since(t, t0, D float64) float64 { return pixelfx.Wrap(t-t0, D) }

// strokeArc draws an electric arc along pts: a dark halo, a bright body, a white-hot core while it's fresh, light along it. C is [halo, body, glow]; k is 1 fresh falling to 0; w is weight.
func strokeArc(p *pixelfx.Painter, pts [][2]float64, C [3]uint32, k, w float64) {
	halo, body, glow := C[0], C[1], C[2]
	for pass := range 3 {
		for i := 1; i < len(pts); i++ {
			ax, ay := pts[i-1][0], pts[i-1][1]
			bx, by := pts[i][0], pts[i][1]
			L := math.Max(1, pixelfx.Hypot(bx-ax, by-ay))
			for d := 0.0; d <= L; d += 0.5 {
				x, y := ax+((bx-ax)*d)/L, ay+((by-ay)*d)/L
				switch pass {
				case 0:
					r := 1.1
					if k > 0.6 {
						r = 1.6
					}
					p.Circle(x, y, r*w, halo, 1)
				case 1:
					r := 0.6
					if k > 0.6 {
						r = 0.9
					}
					p.Circle(x, y, r*w, body, 1)
				default:
					if k > 0.3 {
						p.Px(x, y, pixelfx.White)
					}
				}
			}
			if pass == 0 && i%3 == 0 {
				p.Glow(ax, ay, 9*w, glow, 0.45*k)
			}
		}
	}
}

func zeroGround(float64) float64 { return 0 }
