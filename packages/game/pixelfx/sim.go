// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/core/sim.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See LICENSE and NOTICE.md.
//
// pixelfx adapts this file from Sprite Fusion's open-source pixel art VFX (https://www.spritefusion.com/pixel-art-vfx), MIT License, Copyright (c) 2026 Hugo Duprez.

package pixelfx

import "math"

// Simulated particles, debris and lights, for effects that want real physics: sparks that streak and bounce off the floor, shards that tumble and settle, vapour and smoke that swell and thin, lights that flare and die. Units are art pixels and seconds; they draw through a Painter, so effects stay in local space and mirror or rotate like any other.

// Gravity is the simulation's gravity, in px/s².
const Gravity = 950

// gravity is Gravity as a variable, so products with it round as pixelfx's do.
var gravity float64 = Gravity

var (
	cross = [4][2]float64{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	diag  = [4][2]float64{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	hot   = Ramp("#fff3a6", "#ffe26b", "#ff8a2a", "#f2561d")
)

// FireRamp runs from white-hot to smouldering, the ramp a Fire particle walks down.
var FireRamp = Ramp("#ffffff", "#fff3a6", "#ffe26b", "#ffc23d", "#ff8a2a", "#f2561d", "#c9281f", "#8a1f17", "#4a1712")

// SmokeRamp runs from fresh smoke to thin, the ramp a Smoke particle walks down.
var SmokeRamp = Ramp("#7b6f72", "#5d5660", "#433d48", "#2b2630", "#1a171e")

func rampAt(r []uint32, t float64) uint32 {
	return r[int(math.Min(float64(len(r)-1), math.Max(0, math.Floor(t*float64(len(r))))))]
}

// Kind is a simulated particle's kind.
type Kind int

// The particle kinds.
const (
	Spark Kind = iota
	Mote
	Streak
	Flake
	Vapour
	Dust
	StarKind
	Ember
	Smoke
	Fire
	Brand
	Grit
)

// Flag is a set of particle flags.
type Flag int

// The particle flags.
const (
	// Smokes makes a Fire puff leave smoke when it dies.
	Smokes Flag = 1
	// Big makes smoke bigger and slower.
	Big Flag = 2
	// Back draws the particle behind a character.
	Back Flag = 4
)

func soft(k Kind) bool { return k == Vapour || k == Dust || k == Smoke }

// Particle is one simulated particle.
type Particle struct {
	K            Kind
	X, Y, VX, VY float64
	Life, Max    float64
	Col          uint32
	Size, Seed   float64
	Delay        float64
	Flag         Flag
}

// SimOpts configure a Particles system: Rng is the generator it draws from (one seeded with Seed when nil), Ground the floor y under local x (sparks bounce on it; none when nil), Sparks the head-to-tail colors of a Spark (pixelfx's hot ramp when nil), Haze the color vapour glows with (none when 0), and OnLand what is called when a Brand hits the floor.
type SimOpts struct {
	Rng    *Rng
	Seed   float64
	Ground func(x float64) float64
	Sparks []uint32
	Haze   uint32
	OnLand func(x, y float64)
}

// Particles is a particle system: Spawn adds a particle, Update moves them all on, and Draw draws them.
type Particles struct {
	R      *Rng
	List   []Particle
	T      float64
	Ground func(x float64) float64
	Sparks []uint32
	Haze   uint32
	OnLand func(x, y float64)
}

func noGround(float64) float64 { return math.Inf(1) }

// NewParticles returns an empty particle system.
func NewParticles(o SimOpts) *Particles {
	ps := &Particles{R: o.Rng, Ground: o.Ground, Sparks: o.Sparks, Haze: o.Haze, OnLand: o.OnLand}
	if ps.R == nil {
		seed := o.Seed
		if seed == 0 {
			seed = 1
		}
		ps.R = NewRng(seed)
	}
	if ps.Ground == nil {
		ps.Ground = noGround
	}
	if ps.Sparks == nil {
		ps.Sparks = hot
	}
	return ps
}

// Spawn adds a particle of kind k at (x, y) moving (vx, vy) that lives life seconds, colored col (0 lets Fire and Smoke use their own ramps; pixelfx's default is White), size big (pixelfx's default is 1), appearing after delay seconds, with flag's bits.
func (ps *Particles) Spawn(k Kind, x, y, vx, vy, life float64, col uint32, size, delay float64, flag Flag) {
	ps.List = append(ps.List, Particle{K: k, X: x, Y: y, VX: vx, VY: vy, Life: life, Max: life, Col: col, Size: size, Seed: ps.R.Next() * 100, Delay: delay, Flag: flag})
}

// Count returns how many particles there are.
func (ps *Particles) Count() int { return len(ps.List) }

func (ps *Particles) remove(i int) { ps.List = append(ps.List[:i], ps.List[i+1:]...) }

// Update moves every particle on by dt seconds.
func (ps *Particles) Update(dt float64) {
	ps.T += dt
	R, t := ps.R, ps.T
	for i := len(ps.List) - 1; i >= 0; i-- {
		p := &ps.List[i]
		if p.Delay > 0 {
			p.Delay -= dt
			continue
		}
		p.Life -= dt
		if p.Life <= 0 {
			if p.K == Fire && p.Flag&Smokes != 0 {
				big := p.Flag&Big != 0
				var life, size float64
				var flag Flag
				if big {
					life = R.Range(1.3, 2.3)
				} else {
					life = R.Range(0.8, 1.4)
				}
				if big {
					size, flag = p.Size*0.85, Big
				} else {
					size = p.Size * 0.7
				}
				ps.Spawn(Smoke, p.X, p.Y, p.VX*0.6, p.VY*0.6-6, life, 0, size, 0, flag)
			}
			ps.remove(i)
			continue
		}
		switch p.K {
		case Fire:
			p.VY -= 95 * dt
			p.VX += math.Sin(t*17+p.Seed) * 30 * dt
			p.VX -= p.VX * 5 * dt
			p.VY -= p.VY * 3.2 * dt
		case Grit:
			p.VY += gravity * dt
			p.VX -= p.VX * 0.8 * dt
			g, ny := ps.Ground(p.X), p.Y+p.VY*dt
			p.X += p.VX * dt
			if p.VY > 0 && p.Y <= g && ny >= g {
				p.Y = g - 0.25
				if p.VY > 130 && R.Chance(0.4) {
					p.VY = -p.VY * 0.25
					p.VX = p.VX*0.5 + R.Range(-10, 10)
				} else {
					p.VX, p.VY = 0, 0
					p.Life = math.Min(p.Life, 0.5)
				}
			} else {
				p.Y = ny
			}
			continue
		case Brand:
			// a burning chunk: arcs, trails flame, and starts a little fire where it lands
			p.VY += gravity * 0.8 * dt
			p.VX -= p.VX * 0.9 * dt
			if R.Chance(0.9 * math.Min(1, dt*60)) {
				x, y, vx, vy, k := p.X, p.Y, p.VX, p.VY, p.Life/p.Max
				fvx := vx*0.1 + R.Range(-7, 7)
				fvy := vy*0.1 - 10
				life := R.Range(0.16, 0.32)
				size := R.Range(0.9, 1.7) * (0.5 + k*0.6)
				var flag Flag
				if R.Chance(0.4) {
					flag = Smokes
				}
				ps.Spawn(Fire, x, y, fvx, fvy, life, 0, size, 0, flag)
				p = &ps.List[i]
			}
			g, ny := ps.Ground(p.X), p.Y+p.VY*dt
			p.X += p.VX * dt
			if p.VY > 0 && p.Y <= g && ny >= g {
				x := p.X
				if ps.OnLand != nil {
					ps.OnLand(x, g)
				}
				ps.remove(i)
			} else {
				p.Y = ny
			}
			continue
		case Spark:
			p.VY += gravity * 0.45 * dt
			p.VX -= p.VX * 2.2 * dt
			p.VY -= p.VY * 2.2 * dt
			g, ny := ps.Ground(p.X), p.Y+p.VY*dt
			p.X += p.VX * dt
			if p.VY > 0 && p.Y <= g && ny >= g {
				p.VY = -p.VY * 0.45
				p.VX *= 0.7
				p.Y = g - 0.25
			} else {
				p.Y = ny
			}
			continue
		case Streak:
			p.VX += p.VX * 3 * dt
			p.VY += p.VY * 3 * dt
		case Flake:
			p.VX -= p.VX * 2.2 * dt
			p.VY -= p.VY*2.2*dt - 35*dt
			p.X += math.Sin(ps.T*2.5+p.Seed) * 7 * dt
		case Ember:
			p.VY += (-30 + math.Sin(t*3+p.Seed)*20) * dt
			p.VX += math.Cos(t*2.3+p.Seed) * 45 * dt
			p.VX -= p.VX * 1.6 * dt
			p.VY -= p.VY * 1.6 * dt
		case Smoke:
			rise := 16.0
			if p.Flag&Big != 0 {
				rise = 23
			}
			p.VY -= rise * dt
			p.VX += math.Sin(t*1.3+p.Seed) * 8 * dt
			p.VX -= p.VX * 1.6 * dt
			p.VY -= p.VY * 1.2 * dt
		case Mote:
			p.VX -= p.VX * 0.6 * dt
			p.VY -= p.VY * 0.6 * dt
		default:
			drag := 1.6
			if soft(p.K) {
				drag = 4
			}
			p.VX -= p.VX * drag * dt
			p.VY -= p.VY*drag*dt + 5*dt
		}
		p.X += p.VX * dt
		p.Y += p.VY * dt
	}
}

// Draw draws the particles: LayerBack the soft kinds (vapour, dust, smoke) and those flagged Back, LayerFront the rest, LayerBoth all of them.
func (ps *Particles) Draw(p *Painter, layer Layer) {
	for qi := range ps.List {
		q := &ps.List[qi]
		if q.Delay > 0 {
			continue
		}
		if layer != LayerBoth {
			l := LayerFront
			if soft(q.K) || q.Flag&Back != 0 {
				l = LayerBack
			}
			if l != layer {
				continue
			}
		}
		k, x, y := q.Life/q.Max, q.X, q.Y
		sp := Hypot(q.VX, q.VY)
		if sp == 0 {
			sp = 1
		}
		ux, uy := q.VX/sp, q.VY/sp
		switch q.K {
		case Spark:
			S := ps.Sparks
			head := S[2]
			if k > 0.6 {
				head = S[0]
			} else if k > 0.3 {
				head = S[1]
			}
			ln := math.Min(9, 1+sp*0.022)
			tail := S[len(S)-1]
			if k > 0.5 {
				tail = head
			}
			p.Line(x-ux*ln, y-uy*ln, x-ux*ln*0.4, y-uy*ln*0.4, tail, 1)
			p.Line(x-ux*ln*0.45, y-uy*ln*0.45, x, y, head, 1)
			p.Glow(x, y, 3, head, 0.45*k)
		case Mote:
			ln := math.Min(16, 0.5+sp*0.026) * q.Size
			a := 1.0
			if k < 0.35 {
				a = k / 0.35
			}
			p.Line(x-ux*ln, y-uy*ln, x-ux*ln*0.35, y-uy*ln*0.35, Scale(q.Col, 0.55), a)
			p.Line(x-ux*ln*0.4, y-uy*ln*0.4, x, y, q.Col, a)
			p.Glow(x, y, 2.5+q.Size, q.Col, 0.4*k)
		case Streak:
			L := ClampTo(sp/30, 1, 7)
			for d := L; d >= 0; d -= 0.5 {
				c, a := Scale(q.Col, 0.6), 1.2-d/L
				if d < L*0.5 {
					c, a = q.Col, 1
				}
				if d < 1 {
					c = White
				}
				p.Dot(x-ux*d, y-uy*d, c, a)
			}
			p.Glow(x, y, 4, q.Col, 0.4)
		case Flake:
			a := math.Min(1, k*3)
			p.Dot(x, y, q.Col, a)
			if q.Size > 1.3 {
				for _, d := range cross {
					p.Dot(x+d[0], y+d[1], Scale(q.Col, 0.8), a*0.8)
				}
			}
		case Vapour:
			r := q.Size * (0.6 + (1-k)*0.9)
			p.Disc(x, y, r, q.Col, math.Pow(k, 1.3)*0.45)
			if ps.Haze != 0 {
				p.Glow(x, y, r*1.8, ps.Haze, 0.16*k)
			}
		case Dust:
			p.Disc(x, y, q.Size*(0.6+(1-k)*0.9), q.Col, math.Pow(k, 1.3)*0.7)
		case Smoke:
			// three soft discs: a body, a denser core sitting low, a lit cap while it's young
			age, big := 1-k, q.Flag&Big != 0
			grow, alpha := 0.4, 0.58
			if big {
				grow, alpha = 0.5, 0.7
			}
			r := q.Size * (0.65 + math.Min(1, age*2.5)*0.45 + age*grow)
			a := math.Min(1, age*12) * math.Min(1, k*2.4) * alpha
			var c uint32
			if q.Col != 0 {
				c = Mix(q.Col, rampAt(SmokeRamp, age), age)
			} else {
				c = rampAt(SmokeRamp, age*1.05)
			}
			p.Disc(x, y, r, c, a*0.45)
			p.Disc(x+r*0.05, y+r*0.06, r*0.74, c, a*0.62)
			if age < 0.5 {
				p.Disc(x-r*0.22, y-r*0.3, r*0.5, Mix(c, White, 0.12), a*0.4*(1-age*2))
			}
		case Fire:
			// a ball of flame that cools down the ramp as it rises: a darker rim, a lit core, a tongue on top
			age := 1 - k
			heat := math.Min(0.999, math.Pow(age, 0.62)+math.Mod(q.Seed, 1)*0.1)
			F, n := FireRamp, len(FireRamp)
			idx := int(math.Min(float64(n-2), math.Floor(heat*float64(n-1))))
			r := q.Size * (0.55 + 0.45*math.Min(1, age*7)) * (1 - age*0.55)
			c := F[idx]
			if idx >= n-2 {
				p.Disc(x, y, r, c, math.Min(1, k*3)*0.85)
			} else {
				p.Circle(x, y, r, F[min(n-1, idx+2)], 1)
				p.Circle(x-r*0.16, y-r*0.22, r*0.78, c, 1)
				if r > 1.6 {
					p.Circle(x, y-r*0.9, r*0.42, c, 1)
				}
				if idx <= 3 && r > 1.8 {
					p.Circle(x-r*0.28, y-r*0.34, r*0.36, F[max(0, idx-1)], 1)
				}
				if idx < 6 {
					p.Glow(x, y, r*2.4+2, c, 0.18*k)
				}
			}
		case Brand:
			p.Circle(x, y, 1.6, hot[3], 1)
			c := hot[1]
			if math.Sin(ps.T*40+q.Seed) > 0 {
				c = hot[0]
			}
			p.Px(x, y, c)
			p.Px(x+1, y, hot[1])
			p.Glow(x, y, 7, hot[2], 0.45)
		case Grit:
			if k > 0.25 || k*4 > 0.5 {
				p.Px(x, y, q.Col)
				if q.Size >= 2 {
					p.Px(x+1, y, q.Col)
					p.Px(x, y+1, Scale(q.Col, 0.8))
					p.Px(x+1, y+1, Scale(q.Col, 0.7))
				}
			}
		case Ember:
			c := ps.Sparks[2]
			if k > 0.6 {
				c = ps.Sparks[0]
			} else if k > 0.3 {
				c = ps.Sparks[1]
			}
			if k < 0.25 && math.Sin(ps.T*40+q.Seed) < 0 {
				break
			}
			p.Px(x, y, c)
			if q.Size > 1.5 {
				p.Px(x, y+1, ps.Sparks[len(ps.Sparks)-1])
			}
			p.Glow(x, y, 3+q.Size, c, 0.35*k)
		default:
			// StarKind: a twinkling cross, arms set by size
			tw := 0.6 + 0.4*math.Sin(ps.T*(8+math.Mod(q.Seed, 5))+q.Seed)
			if q.Size < 1 {
				if tw*k > 0.25 {
					c := q.Col
					if tw > 0.8 {
						c = White
					}
					p.Px(x, y, c)
				}
				p.Glow(x, y, 3, q.Col, 0.4*k*tw)
				break
			}
			Sparkle(p, x, y, q.Size*tw*math.Min(1, k*3), q.Col, Scale(q.Col, 0.75), 0.5*k*tw)
		}
	}
}

// Blaze is a little fire burning where something landed.
type Blaze struct {
	X, Y, Life, Max, Acc, Power float64
}

// Blazes are little fires burning where something landed: Add feeds Fire puffs (and the odd ember) into a particle system, fading over its life; DrawBack adds a hot pixel and a glow at its foot.
type Blazes struct {
	PS   *Particles
	List []Blaze
}

// NewBlazes returns blazes feeding ps.
func NewBlazes(ps *Particles) *Blazes { return &Blazes{PS: ps} }

// Add starts a blaze at (x, y) burning life seconds at power.
func (bs *Blazes) Add(x, y, life, power float64) {
	bs.List = append(bs.List, Blaze{X: x, Y: y, Life: life, Max: life, Acc: bs.PS.R.Next(), Power: power})
}

// Count returns how many blazes are burning.
func (bs *Blazes) Count() int { return len(bs.List) }

// Update burns every blaze on by dt seconds.
func (bs *Blazes) Update(dt float64) {
	ps, R := bs.PS, bs.PS.R
	for i := len(bs.List) - 1; i >= 0; i-- {
		b := &bs.List[i]
		b.Life -= dt
		if b.Life <= 0 {
			bs.List = append(bs.List[:i], bs.List[i+1:]...)
			continue
		}
		k := b.Life / b.Max
		intensity := b.Power
		if k <= 0.7 {
			intensity = b.Power * (0.35 + (k/0.7)*0.65)
		}
		for b.Acc += dt * (6 + 16*intensity); b.Acc > 1; b.Acc-- {
			s := R.Range(1.2, 2.8) * intensity
			x := b.X + R.Range(-1.5, 1.5)
			y := b.Y - s*0.6
			vx := R.Range(-6, 6)
			vy := R.Range(-30, -12)
			life := R.Range(0.28, 0.6)
			flag := Back
			if R.Chance(0.3) {
				flag |= Smokes
			}
			ps.Spawn(Fire, x, y, vx, vy, life, 0, s, 0, flag)
			if R.Chance(0.06) {
				ps.Spawn(Ember, b.X, b.Y-1.5, R.Range(-15, 15), R.Range(-45, -15), R.Range(0.6, 1.4), hot[2], 1, 0, Back)
			}
		}
	}
}

// DrawBack draws each blaze's hot foot and its glow.
func (bs *Blazes) DrawBack(p *Painter) {
	for _, b := range bs.List {
		k := b.Life / b.Max
		c := hot[3]
		if k > 0.4 {
			c = hot[2]
			if math.Sin(b.Life*31+b.X) > 0 {
				c = hot[1]
			}
		}
		p.Px(b.X, b.Y, c)
		p.Glow(b.X, b.Y, 6+5*k*b.Power, hot[2], 0.3*k*b.Power)
	}
}

// BitKind is a kind of debris.
type BitKind int

// The debris kinds.
const (
	Shard BitKind = iota
	Chunk
	Drip
)

// defaultChunkRamp shades a Chunk without a ramp of its own.
var defaultChunkRamp = Ramp("#1c2030", "#3a4a66", "#6e86a8", "#a8c0dc", "#e8f2ff")

// Bit is one piece of debris. S is its size, A and VA its angle and spin, C its color, Ramp a chunk's shading (dark to bright), G its gravity scale, Drag, Bounce and Life (seconds). Pts, PVY, Dusted and Data are left to the effect, for its own bookkeeping.
type Bit struct {
	Kind            BitKind
	X, Y, VX, VY    float64
	A, VA, S        float64
	C               uint32
	Ramp            []uint32
	G, Drag, Bounce float64
	Life, Max       float64
	Rest            bool
	Pts             []float64
	PVY             float64
	Dusted          bool
	Data            any
}

// Bits is rigid debris that tumbles, bounces and settles.
type Bits struct {
	List   []Bit
	Ground func(x float64) float64
}

// NewBits returns no debris yet over ground (none when nil).
func NewBits(ground func(x float64) float64) *Bits {
	if ground == nil {
		ground = noGround
	}
	return &Bits{Ground: ground}
}

// Add adds a piece of debris. Fields left zero take pixelfx's defaults: S 1, C White, G 1 and Life 1 (Kind is Shard, and the rest start at 0).
func (bs *Bits) Add(b Bit) {
	if b.S == 0 {
		b.S = 1
	}
	if b.C == 0 {
		b.C = White
	}
	if b.G == 0 {
		b.G = 1
	}
	if b.Life == 0 {
		b.Life = 1
	}
	b.Max = b.Life
	bs.List = append(bs.List, b)
}

// Count returns how many pieces there are.
func (bs *Bits) Count() int { return len(bs.List) }

// Update moves every piece on by dt seconds.
func (bs *Bits) Update(dt float64) {
	for i := len(bs.List) - 1; i >= 0; i-- {
		b := &bs.List[i]
		b.Life -= dt
		if b.Life <= 0 {
			bs.List = append(bs.List[:i], bs.List[i+1:]...)
			continue
		}
		if b.Rest {
			continue
		}
		b.VY += gravity * b.G * dt
		b.VX -= b.VX * b.Drag * dt
		b.VY -= b.VY * b.Drag * dt
		b.X += b.VX * dt
		b.Y += b.VY * dt
		b.A += b.VA * dt
		floor, sp := bs.Ground(b.X), b.VY
		if b.G <= 0 || b.Y < floor || sp <= 0 {
			continue
		}
		b.Y = floor - 0.01
		switch {
		case b.Kind == Drip:
			b.Life = 0
		case b.Bounce > 0 && sp > 70:
			b.VY = -sp * b.Bounce
			b.VX *= 0.7
			b.VA *= 0.6
		default:
			b.Rest = true
			b.VX, b.VY = 0, 0
		}
	}
}

// Draw draws every piece.
func (bs *Bits) Draw(p *Painter) {
	for i := range bs.List {
		b := &bs.List[i]
		a, ca, sa := math.Min(1, b.Life/0.4), math.Cos(b.A), math.Sin(b.A)
		switch b.Kind {
		case Shard:
			for d := -b.S / 2; d <= b.S/2; d += 0.5 {
				c := b.C
				if d > b.S/2-0.6 {
					c = White
				}
				p.Dot(b.X+ca*d, b.Y+sa*d, c, a)
			}
		case Drip:
			p.Dot(b.X, b.Y, b.C, a)
			p.Dot(b.X, b.Y-1, b.C, a*0.5)
		default:
			// a shaded chunk: lit top-left, dark bottom-right, a dark rim
			R := b.Ramp
			if R == nil {
				R = defaultChunkRamp
			}
			n := len(R)
			x, y, r := Round(b.X), Round(b.Y), math.Ceil(b.S)+1
			for py := -r; py <= r; py++ {
				for px := -r; px <= r; px++ {
					u, v := (px+0.5)*ca+(py+0.5)*sa, -(px+0.5)*sa+(py+0.5)*ca
					d := math.Abs(u)/b.S + math.Abs(v)/(b.S*0.62)
					if d > 1.3 {
						continue
					}
					var c uint32
					switch {
					case d > 1:
						c = R[0]
					case v < 0 && u < 0:
						c = R[n-1]
					case v < 0:
						c = R[n-2]
					case u < 0:
						c = R[n-3]
					default:
						c = R[max(1, n-4)]
					}
					p.Dot(x+px, y+py, c, a)
				}
			}
		}
	}
}

// LightSpot is one light that flares and dies.
type LightSpot struct {
	X, Y, R   float64
	Color     uint32
	K         float64
	Life, Max float64
}

// Lights are lights that flare and die.
type Lights struct {
	List []LightSpot
}

// Add adds a light at (x, y) of radius r and color, strength k, lasting life seconds.
func (ls *Lights) Add(x, y, r float64, color uint32, k, life float64) {
	ls.List = append(ls.List, LightSpot{X: x, Y: y, R: r, Color: color, K: k, Life: life, Max: life})
}

// Update ages every light by dt seconds.
func (ls *Lights) Update(dt float64) {
	for i := len(ls.List) - 1; i >= 0; i-- {
		ls.List[i].Life -= dt
		if ls.List[i].Life <= 0 {
			ls.List = append(ls.List[:i], ls.List[i+1:]...)
		}
	}
}

// Draw draws every light.
func (ls *Lights) Draw(p *Painter) {
	for _, l := range ls.List {
		k := l.Life / l.Max
		p.Glow(l.X, l.Y, l.R*(0.55+0.45*k), l.Color, l.K*k*k)
	}
}

// Sparkle draws a star glint: arms of length arm in color c with tips in tip, a white heart, light of strength k (pixelfx's defaults are tip c and k 0.6).
func Sparkle(p *Painter, x, y, arm float64, c, tip uint32, k float64) {
	n := Round(arm)
	if n < 1 {
		return
	}
	X, Y := math.Floor(x), math.Floor(y)
	for d := 1.0; d <= n; d++ {
		cc := c
		if d > n*0.6 {
			cc = tip
		}
		for _, q := range cross {
			p.Px(X+q[0]*d, Y+q[1]*d, cc)
		}
	}
	if n >= 3 {
		for _, q := range diag {
			p.Px(X+q[0], Y+q[1], c)
		}
	}
	if n >= 6 {
		for _, q := range diag {
			p.Px(X+q[0]*2, Y+q[1]*2, tip)
		}
	}
	p.Px(X, Y, White)
	p.Glow(x, y, 4+n*2, c, k)
}

// Flare draws an 8-ray flare: long rays on the axes, shorter between, fading out along their length. ramp runs dark to bright.
func Flare(p *Painter, x, y, L, k float64, ramp []uint32) {
	if L < 1 {
		return
	}
	R, n := ramp, len(ramp)
	for i := range 8 {
		an := (float64(i) * math.Pi) / 4
		ln := L * 0.3
		if i%4 == 0 {
			ln = L
		} else if i%2 == 0 {
			ln = L * 0.6
		}
		ca, sa := math.Cos(an), math.Sin(an)
		for d := 0.0; d <= ln; d += 0.5 {
			u := d / ln
			c := R[n-3]
			if u < 0.35 {
				c = White
			} else if u < 0.7 {
				c = R[n-2]
			}
			a := k*1.5*(1-u) + 0.2
			if u < 0.6 {
				a = 1
			}
			p.Dot(x+ca*d, y+sa*d, c, a)
		}
	}
}

// Band draws a ring on a tilted plane (sy squashes it: 0.16 lies on the floor, 1 stands up), th thick inward, at alpha k, colored by depth through the band with ramp (dark to bright; white at the outer edge when hot). keep(angle) picks which part to draw, so the far half can go behind things and the near half in front: func(g) bool { return math.Sin(g) < 0 } is the far half; nil draws all of it.
func Band(p *Painter, cx, cy, r, sy, th, k float64, ramp []uint32, keep func(ang float64) bool, hot bool) {
	if r <= 0 || k <= 0 {
		return
	}
	R, n := ramp, len(ramp)
	m := int(math.Ceil(r * Tau * 1.2))
	for j := 0; j < m; j++ {
		ang := (float64(j) / float64(m)) * Tau
		ca, sa := math.Cos(ang), math.Sin(ang)
		if keep != nil && !keep(ang) {
			continue
		}
		for o := th; o >= 0; o -= 0.5 {
			u := o / th
			var c uint32
			switch {
			case u < 0.15:
				c = R[n-1]
				if hot {
					c = White
				}
			case u < 0.4:
				c = R[n-2]
			case u < 0.7:
				c = R[n-3]
			default:
				c = R[n-4]
			}
			a := 1.15 - u
			if u < 0.4 {
				a = 1.2
			}
			p.Dot(cx+ca*(r-o), cy+sa*(r-o)*sy, c, k*a)
		}
	}
}

// Crystal draws a faceted crystal spike standing on (x, base), h tall, w wide at its base, leaning lean px at the tip, shaded in ramp (dark to bright, 6+ steps): a lit ridge, a bright left face, a darker right face, a dark rim. a is its alpha, white flashes it white, and glint (0..1 up its height, or -1 for none) sweeps a highlight.
func Crystal(p *Painter, x, base, h, w, lean float64, ramp []uint32, a float64, white bool, glint float64) {
	if h < 1.5 {
		return
	}
	R, n := ramp, len(ramp)
	C := func(i int) uint32 { return R[max(0, n-1-i)] }
	top, ridge0 := base-h, x-w*0.14
	inside := func(px, py float64) bool {
		u := (base - (py + 0.5)) / h
		return u >= 0 && u <= 1 && math.Abs(px+0.5-(x+lean*u)) <= (w/2)*(1-u)+0.35
	}
	for py := math.Floor(top) - 1; py < math.Ceil(base); py++ {
		for px := math.Floor(x - w/2 - math.Abs(lean) - 2); px <= math.Ceil(x+w/2+math.Abs(lean)+2); px++ {
			if !inside(px, py) {
				if inside(px-1, py) || inside(px+1, py) || inside(px, py+1) || (inside(px, py-1) && py < base-1) {
					p.Dot(px, py, R[0], a)
				}
				continue
			}
			if white {
				p.Dot(px, py, White, a)
				continue
			}
			u := (base - (py + 0.5)) / h
			rx := ridge0 + (x+lean-ridge0)*u
			c := x + lean*u
			hw := (w/2)*(1-u) + 0.35
			fx := px + 0.5
			var col uint32
			switch {
			case u > 0.9:
				col = White
			case math.Abs(fx-rx) < 0.55:
				col = C(1)
			case fx < rx:
				switch {
				case fx-(c-hw) < 1:
					col = C(1)
				case (fx-(c-hw))/math.Max(1, rx-(c-hw)) > 0.55:
					col = C(2)
				default:
					col = C(3)
				}
			case c+hw-fx < 1.2:
				col = C(min(n-2, 6))
			case (fx-rx)/math.Max(1, c+hw-rx) > 0.5:
				col = C(5)
			default:
				col = C(4)
			}
			if glint >= 0 && fx <= rx+0.6 {
				g := math.Abs(u - glint)
				if g < 0.05 {
					col = White
				} else if g < 0.1 {
					col = C(1)
				}
			}
			p.Dot(px, py, col, a)
		}
	}
}
