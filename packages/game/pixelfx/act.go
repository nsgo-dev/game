// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/core/act.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See LICENSE and NOTICE.md.
//
// pixelfx's act.js adapts how Hollow Crown plays its skills (poses on 12 fps steps, hit-stops, the springy straw target), from https://github.com/rehan-remade/hollow-crown, MIT License, Copyright (c) 2026 rehan-remade.

package pixelfx

import (
	"encoding/json"
	"math"
	"slices"
)

// An act: an effect performed. The character winds up, lets go and recovers, a target takes the hit, and everything freezes for a moment on contact. Effects describe it as data (Effect.Act), so a game with its own character can read the same timing (ActTiming).

// Opt is a number that may be left unsaid, so it takes a default: the zero Opt is unsaid.
type Opt struct {
	V   float64
	Set bool
}

// Val returns the Opt that says x.
func Val(x float64) Opt { return Opt{V: x, Set: true} }

// Or returns o's number, or def when it is unsaid.
func (o Opt) Or(def float64) float64 {
	if o.Set {
		return o.V
	}
	return def
}

// Key is one key of an act's poses: at T seconds (effect time; negative is the wind-up before the effect starts) the character is in Pose, which How it arrives at: "" smoothly, "out" fast then settling, "in" slowly then fast, "line" evenly, "snap" holding the last pose and jumping on the key.
type Key struct {
	T    float64
	Pose Pose
	How  string
}

// Hit is a moment of contact: at T, freeze for Stop seconds, shake Shake seconds by Amp px (2 when unsaid), and unless NoTarget, hit the targets (only target At when it is said): Push sways it (1.2 when unsaid), Straw pieces of straw fly (8), Flash flashes it white that long (0.05), Y is where on it they fly from (-26), Skin leaves a look on it (a name in Skins, SkinNone to clear it, "" to keep it) and Shiver shivers it that long.
type Hit struct {
	T        float64
	Stop     float64
	Shake    float64
	Amp      Opt
	NoTarget bool
	At       Opt
	Push     Opt
	Straw    Opt
	Flash    Opt
	Y        Opt
	Skin     string
	Shiver   float64
}

// SkinNone, as a Hit's Skin, clears the target's skin.
const SkinNone = "none"

// DustKick is dust kicked up at T: N specks (6 when 0) at X along the floor, flying Dir (-1 left, 1 right, 0 either way at random), in color C (pixelfx's dust colors when 0).
type DustKick struct {
	T, X float64
	N    int
	Dir  float64
	C    uint32
}

// Act is how a character performs an effect: Poses over time, Hits, Dust, straw Targets at x along the floor (pixelfx's default is 66), OnBody when the effect paints the standing body so each body pixel must carry it into the pose, Follow how much the body's lean and crouch carry what the effect drew on it (1 when unsaid), and Ghost the color of its after-images ("" for the effect's own palette).
type Act struct {
	Poses   []Key
	Hits    []Hit
	Dust    []DustKick
	Targets []float64
	OnBody  bool
	Follow  Opt
	Ghost   string
}

// ---- poses over time

// LerpPose returns the pose u of the way from a to b: numbers move, everything else changes at the later key.
func LerpPose(a, b Pose, u float64) Pose {
	out, B := Stand.Merge(a), Stand.Merge(b)
	for k := range numPoseKeys {
		if !B.Has(k) || k.isSwitch() {
			continue
		}
		if out.Has(k) {
			out.v[k] = Lerp(out.v[k], B.v[k], u)
		} else {
			out = out.With(k.Is(B.v[k]))
		}
	}
	// what the later pose doesn't mention eases back to its default
	for _, k := range [4]PoseKey{HX, HY, Lean, Crouch} {
		if !B.Has(k) && out.Has(k) {
			out.v[k] = Lerp(out.v[k], 0, u)
		}
	}
	if !B.Has(WL) && out.Has(WL) {
		out.v[WL] = Lerp(out.v[WL], 1, u)
	}
	return out
}

func easeHow(how string, u float64) float64 {
	switch how {
	case "out":
		return 1 - (1-u)*(1-u)
	case "in":
		return u * u
	case "line":
		return u
	}
	return u * u * (3 - 2*u)
}

// PoseAt returns the pose at time t on a track of keys. Limbs move in whole 12 fps steps counted from each key, so they read as held drawings; the feet's place over the floor (X, Y) stays smooth.
func PoseAt(track []Key, t float64) Pose {
	if t <= track[0].T {
		return track[0].Pose
	}
	for i := 0; i < len(track)-1; i++ {
		t0, a := track[i].T, track[i].Pose
		t1, b, how := track[i+1].T, track[i+1].Pose, track[i+1].How
		if t >= t1 {
			continue
		}
		if how == "snap" {
			return a
		}
		stepped := t0 + math.Floor((t-t0)*12+1e-6)/12
		out := LerpPose(a, b, easeHow(how, (stepped-t0)/(t1-t0)))
		u := easeHow(how, (t-t0)/(t1-t0))
		return out.With(X.Is(Lerp(a.Or(X, 0), b.Or(X, 0), u)), Y.Is(Lerp(a.Or(Y, 0), b.Or(Y, 0), u)))
	}
	return track[len(track)-1].Pose
}

// isHeld reports whether the body is between keys that hardly differ, or past the last.
func isHeld(track []Key, t float64) bool {
	if t >= track[len(track)-1].T || t < track[0].T {
		return true
	}
	for i := 0; i < len(track)-1; i++ {
		if t >= track[i+1].T {
			continue
		}
		a, b := Stand.Merge(track[i].Pose), Stand.Merge(track[i+1].Pose)
		if track[i+1].T-track[i].T <= 0.4 {
			return false
		}
		for _, k := range [8]PoseKey{FX, FY, BX, BY, Crouch, Lean, X, Y} {
			if math.Abs(a.Or(k, 0)-b.Or(k, 0)) >= 1.5 {
				return false
			}
		}
		return true
	}
	return true
}

// ---- the target

const dummyW, dummyH = 38, 52

var (
	tp                                                = palette(map[rune]string{'k': "#0b0a1a", 'w': "#724c2e", 'W': "#a0703a", 'd': "#4e331f", 's': "#d2aa55", 'S': "#f0d98a", 'b': "#86643a", 'B': "#b38b57", 'L': "#c9a26c", 'r': "#9a2f3c", 'R': "#c4434f", 'h': "#ffffff"})
	tpInk, tpWood, tpWoodL, tpWoodD                   = tp['k'], tp['w'], tp['W'], tp['d']
	tpStraw, tpStrawL, tpBurlapD, tpBurlap, tpBurlapL = tp['s'], tp['S'], tp['b'], tp['B'], tp['L']
	tpRed, tpRedL, tpWhite                            = tp['r'], tp['R'], tp['h']
	dustCols                                          = Ramp("#efe7dc", "#c9bfb8", "#8f8693", "#5a5468")
	strawCols                                         = [5]uint32{tpStrawL, tpStraw, tpStraw, tpBurlapD, tpBurlapD}
	dummy                                             = buildDummy()
)

func buildDummy() []uint32 {
	D := make([]uint32, dummyW*dummyH)
	px := func(x, y int, c uint32) {
		if x >= 0 && y >= 0 && x < dummyW && y < dummyH {
			D[y*dummyW+x] = c
		}
	}
	R := func(x, y, w, h int, c uint32) {
		for j := 0; j < h; j++ {
			for i := 0; i < w; i++ {
				px(x+i, y+j, c)
			}
		}
	}
	// the post and the crossbar
	R(15, 38, 8, 14, tpInk)
	R(16, 34, 6, 18, tpWood)
	R(16, 34, 2, 18, tpWoodL)
	R(20, 34, 2, 18, tpWoodD)
	R(1, 18, 36, 1, tpInk)
	R(1, 23, 36, 1, tpInk)
	R(1, 19, 36, 4, tpWood)
	R(1, 19, 36, 1, tpWoodL)
	R(1, 22, 36, 1, tpWoodD)
	// straw bursting out of the ends of the arms
	for _, end := range [2][2]int{{0, -1}, {33, 1}} {
		bx, s := end[0], end[1]
		R(bx, 17, 5, 8, tpStraw)
		for _, d := range []struct {
			dx, dy int
			c      uint32
		}{{2, 16, tpStraw}, {3, 15, tpStrawL}, {1, 17, tpStrawL}, {2, 25, tpStraw}, {1, 26, tpStrawL}, {3, 25, tpBurlapD}, {2, 19, tpStrawL}, {3, 21, tpStrawL}, {1, 22, tpBurlapD}, {2, 23, tpStrawL}} {
			px(bx+d.dx, d.dy, d.c)
		}
		ex := bx + 5
		if s < 0 {
			ex = bx - 1
		}
		px(ex, 19, tpStraw)
		px(ex, 22, tpStrawL)
	}
	sack := func(x, y, w, h int) {
		R(x+1, y, w-2, 1, tpInk)
		R(x+1, y+h-1, w-2, 1, tpInk)
		R(x, y+1, 1, h-2, tpInk)
		R(x+w-1, y+1, 1, h-2, tpInk)
		R(x+1, y+1, w-2, h-2, tpBurlap)
		R(x+w-4, y+1, 3, h-2, tpBurlapD)
		R(x+1, y+h-3, w-2, 2, tpBurlapD)
		R(x+1, y+1, 1, h-4, tpBurlapL)
		R(x+1, y+1, w-6, 1, tpBurlapL)
		// the weave
		for j := y + 2; j < y+h-2; j += 3 {
			for i := x + 3 + j%2; i < x+w-4; i += 4 {
				px(i, j, tpBurlapD)
			}
		}
	}
	sack(9, 17, 20, 24)
	// the rings on its chest
	for y := 19; y < 36; y++ {
		for x := 11; x < 27; x++ {
			d := Hypot(float64(x)-18.5, float64(y)-27)
			if (d > 4 && d < 5.4) || d < 1.9 {
				c := tpRed
				if x+y < 45 {
					c = tpRedL
				}
				px(x, y, c)
			}
		}
	}
	// the rope belt, and straw coming out under it
	for x := 10; x < 28; x++ {
		if x&1 != 0 {
			px(x, 36, tpWoodL)
			px(x, 37, tpWoodD)
		} else {
			px(x, 36, tpStraw)
			px(x, 37, tpWood)
		}
	}
	for _, d := range []struct {
		x, y int
		c    uint32
	}{{11, 41, tpStraw}, {12, 42, tpStrawL}, {14, 41, tpStrawL}, {15, 42, tpStraw}, {23, 41, tpStraw}, {24, 42, tpStrawL}, {26, 41, tpStraw}, {13, 43, tpStraw}, {25, 43, tpStraw}} {
		px(d.x, d.y, d.c)
	}
	sack(10, 1, 18, 16)
	// X for eyes and a stitched mouth
	for _, ex := range [2]int{14, 23} {
		for d := -2; d <= 2; d++ {
			px(ex+d, 7+d, tpInk)
			px(ex-d, 7+d, tpInk)
		}
	}
	for x := 14; x < 24; x++ {
		if x&1 != 0 {
			px(x, 12, tpInk)
		} else {
			px(x, 12, tpBurlapD)
		}
	}
	for _, x := range [3]int{15, 18, 21} {
		px(x, 11, tpInk)
		px(x, 13, tpInk)
	}
	// the rope at its neck, and straw for hair
	for x := 12; x < 26; x++ {
		if x&1 != 0 {
			px(x, 16, tpWoodL)
			px(x, 17, tpWood)
		} else {
			px(x, 16, tpStraw)
			px(x, 17, tpWoodD)
		}
	}
	for _, d := range []struct {
		x, y int
		c    uint32
	}{{13, 0, tpStraw}, {15, 0, tpStrawL}, {18, 0, tpStraw}, {21, 0, tpStrawL}, {24, 0, tpStraw}, {9, 4, tpStraw}, {28, 6, tpStraw}, {8, 7, tpStrawL}, {29, 3, tpStrawL}} {
		px(d.x, d.y, d.c)
	}
	return D
}

// Skin is what a hit can leave on the target: a color, and how much of it.
type Skin struct {
	Color  uint32
	Amount float64
}

// Skins are the looks a hit can leave on the target, by name; "flash" is the white flash every hit gives.
var Skins = map[string]Skin{
	"flash":  {tpWhite, 1},
	"char":   {Hex("#1a0c0e"), 0.72},
	"ice":    {Hex("#a8e8ff"), 0.72},
	"shadow": {Hex("#3a1a70"), 0.5},
	"holy":   {Hex("#fff4c0"), 0.5},
	"red":    {Hex("#ff2040"), 0.45},
	"shock":  {Hex("#8fd6ff"), 0.6},
	"poison": {Hex("#58c43a"), 0.5},
}

type straw struct {
	x, y, vx, vy, age, life float64
	big                     bool
}

// Target is a straw target on a spring: it sways, flashes, takes a skin, shivers and sheds straw. X is the floor under it, in local space.
type Target struct {
	X                                   float64
	theta, omega, flashT, shiver, clock float64
	skin                                string
	r                                   *Rng
	straw                               []straw
}

// NewTarget returns a target standing at x, its straw seeded with seed.
func NewTarget(x, seed float64) *Target { return &Target{X: x, r: NewRng(seed)} }

// Hit hits the target as h says.
func (g *Target) Hit(h Hit) {
	g.omega += h.Push.Or(1.2)
	g.flashT = math.Max(g.flashT, h.Flash.Or(0.05))
	switch h.Skin {
	case "":
	case SkinNone:
		g.skin = ""
	default:
		g.skin = h.Skin
	}
	if h.Shiver != 0 {
		g.shiver = h.Shiver
	}
	R, y, n := g.r, h.Y.Or(-26), h.Straw.Or(8)
	for k := 0.0; k < n; k++ {
		back := R.Next() < 0.3
		s := straw{x: g.X + R.Range(-3, 3), y: y + R.Range(-3, 3)}
		if back {
			s.vx = R.Range(-60, -5)
		} else {
			s.vx = R.Range(10, 130)
		}
		s.vy = R.Range(-120, -5)
		s.life = R.Range(1.2, 2.2)
		s.big = R.Next() < 0.5
		g.straw = append(g.straw, s)
	}
}

// Step moves the target on by dt seconds.
func (g *Target) Step(dt float64) {
	g.clock += dt
	g.omega += (-60*g.theta - 3.4*g.omega) * dt
	g.theta = ClampTo(g.theta+g.omega*dt, -0.4, 0.42)
	if g.flashT > 0 {
		g.flashT -= dt
	}
	if g.shiver > 0 {
		g.shiver -= dt
	}
	for i := range g.straw {
		s := &g.straw[i]
		s.age += dt
		s.vy += 210 * dt
		s.x += s.vx * dt
		s.y += s.vy * dt
		if s.y >= 0 {
			s.y = 0
			s.vx *= 0.2
			s.vy = 0
		}
	}
	g.straw = slices.DeleteFunc(g.straw, func(s straw) bool { return !(s.age < s.life) })
}

// setPx writes c at (px, py) of a w x h color buffer when the point is a pixel inside it.
func setPx(C []uint32, W, H int, px, py float64, c uint32) {
	if px >= 0 && py >= 0 && px < float64(W) && py < float64(H) && px == math.Floor(px) && py == math.Floor(py) {
		C[int(py)*W+int(px)] = c
	}
}

// Draw draws the target into a w x h color buffer, its floor at (x, y), mirrored when flip.
func (g *Target) Draw(C []uint32, W, H int, x, y float64, flip bool) {
	tan := math.Tan(g.theta)
	sv := 0.0
	if g.shiver > 0 {
		sv = -1
		if ToInt32(math.Floor(g.clock*30))&1 != 0 {
			sv = 1
		}
	}
	name := g.skin
	if g.flashT > 0 {
		name = "flash"
	}
	sk, hasSkin := Skins[name]
	base := x + g.X
	if flip {
		base = x - g.X - 1
	}
	for r := 0; r < dummyH; r++ {
		o := Round(float64(dummyH-1-r)*tan*0.8) + sv
		for xx := 0; xx < dummyW; xx++ {
			c := dummy[r*dummyW+xx]
			if c == 0 {
				continue
			}
			px := base + (float64(xx) - 19 + o)
			if flip {
				px = base + (-(float64(xx) - 19 + o) - 1)
			}
			py := y - dummyH + float64(r)
			if hasSkin {
				c = Mix(c, sk.Color, sk.Amount)
			}
			setPx(C, W, H, px, py, c)
		}
	}
}

// DrawStraw draws the straw the target has shed.
func (g *Target) DrawStraw(C []uint32, W, H int, x, y float64, flip bool) {
	for _, s := range g.straw {
		c := strawCols[int(math.Min(4, math.Floor((s.age/s.life)*5)))]
		lx, ly := Round(s.x), Round(s.y)-1
		n := 1
		if s.big {
			n = 2
		}
		for i := range n {
			px := x + lx + float64(i)
			if flip {
				px = x + (-(lx + float64(i)) - 1)
			}
			setPx(C, W, H, px, y+ly, c)
		}
	}
}

// ---- the act itself

const ghostLife = 0.16

var (
	restMask, restPx = func() ([]uint8, []uint32) {
		m, px := make([]uint8, restW*restH), make([]uint32, restW*restH)
		for r, row := range Hero2 {
			c := -1
			for _, ch := range row {
				c++
				if ch != '.' {
					m[r*restW+c] = 1
					px[r*restW+c] = DummyPal[ch]
				}
			}
		}
		return m, px
	}()
	// every cell of the standing sprite, mapped to the nearest cell that's on the body
	nearest = func() []int16 {
		out := make([]int16, restW*restH)
		for r := 0; r < restH; r++ {
			for c := 0; c < restW; c++ {
				best, bd := -1, 1000000000
				for y := max(0, r-7); y < min(restH, r+8); y++ {
					for x := max(0, c-7); x < min(restW, c+8); x++ {
						d := (x-c)*(x-c) + (y-r)*(y-r)
						if restMask[y*restW+x] != 0 && d < bd {
							bd, best = d, y*restW+x
						}
					}
				}
				out[r*restW+c] = int16(best)
			}
		}
		return out
	}()
)

func lum(c uint32) float64 {
	return (0.3*float64(c&255) + 0.59*float64((c>>8)&255) + 0.11*float64((c>>16)&255)) / 255
}

// carry moves an effect's color over the standing body onto a pixel of the pose: its color, at this pixel's own light and shade.
func carry(v, rest, own uint32) uint32 {
	if lum(v) > 0.9 || v>>24 < 255 {
		return v
	}
	k := math.Max(0.45, math.Min(1.5, lum(own)/math.Max(0.08, lum(rest))))
	ch := func(n uint) uint32 { return uint32(ToInt32(math.Min(255, Round(float64((v>>n)&255)*k)))) }
	return 255<<24 | ch(16)<<16 | ch(8)<<8 | ch(0)
}

// ActSpan returns when an act's poses begin and end, in effect time; a loop ends when it's told to (to is +Inf).
func ActSpan(e *Effect) (from, to float64) {
	D := e.Meta.Duration
	first, last := 0.0, 0.0
	if e.Act != nil && len(e.Act.Poses) > 0 {
		first, last = e.Act.Poses[0].T, e.Act.Poses[len(e.Act.Poses)-1].T
	}
	from = math.Min(0, first)
	if e.Meta.Loop {
		return from, math.Inf(1)
	}
	return from, math.Max(D, last)
}

func (a *Act) targets() []float64 {
	if a == nil {
		return nil
	}
	return a.Targets
}

type speck struct {
	x, y, vx, vy, age, life float64
	big                     bool
	c                       uint32
}

type ghost struct {
	q   Pose
	age float64
}

// Performance is an effect performed: the character, the effect, the target and the freeze, drawn together. Its time runs from the first pose (before the effect's own t = 0) to the last; Update holds everything still during a freeze. Without an act, the effect just plays with the character standing by.
type Performance struct {
	// FX is the effect being performed.
	FX *Player
	// Targets are the straw targets.
	Targets []*Target
	// OnHit, when set, is called on every hit as it lands.
	OnHit func(h Hit)

	e                                  *Effect
	act                                *Act
	rig                                *Rig
	track                              []Key
	hits                               []Hit
	dust                               []DustKick
	r                                  *Rng
	specks                             []speck
	ghosts                             []ghost
	gc                                 uint32
	x, y                               float64
	flip                               bool
	from, to                           float64
	t, stop, shakeT, shake0, amp, gAcc float64
	stopped                            bool
	nh, nd, loopN                      int
	last                               Pose
	hasLast                            bool
	scratchA, scratchB                 *Frame
}

var standTrack = []Key{{T: 0, Pose: Stand}}

// Perform starts e performed with o (X and Y place the feet), its character drawn by rig (a new one when nil). Give it the Rig that ActBounds used to size the frame, as pixelfx shares one between them.
func Perform(e *Effect, o Options, rig *Rig) *Performance {
	if rig == nil {
		rig = NewRig()
	}
	a := e.Act
	if a == nil {
		a = &Act{}
	}
	seed := 1.0
	if v, ok := o.Values["seed"]; ok && v.kind == KindNumber {
		seed = v.num
	} else if d, ok := e.Meta.Default("seed"); ok && d.kind == KindNumber {
		seed = d.num
	}
	pf := &Performance{e: e, act: a, rig: rig, FX: NewPlayer(e, o), x: o.X, y: o.Y, flip: o.Flip, track: a.Poses}
	if len(pf.track) == 0 {
		pf.track = standTrack
	}
	pf.from, pf.to = ActSpan(e)
	for i, x := range a.targets() {
		pf.Targets = append(pf.Targets, NewTarget(x, seed+4+float64(i)))
	}
	pf.hits = slices.Clone(a.Hits)
	slices.SortStableFunc(pf.hits, func(p, q Hit) int {
		switch {
		case p.T < q.T:
			return -1
		case p.T > q.T:
			return 1
		}
		return 0
	})
	pf.dust = slices.Clone(a.Dust)
	pf.r = NewRng(seed + 9)
	// after-images take the effect's own color unless the act names one
	var ramp []uint32
	if v, ok := o.Values["palette"]; ok && v.kind == KindRamp {
		ramp = v.colors
	} else if d, ok := e.Meta.Default("palette"); ok && d.kind == KindRamp {
		ramp = d.colors
	}
	switch {
	case a.Ghost != "":
		pf.gc = Hex(a.Ghost)
	case len(ramp) > 0:
		pf.gc = ramp[int(math.Floor(float64(len(ramp))*0.7))]
	default:
		pf.gc = Hex("#8fb4ff")
	}
	pf.t = pf.from
	pf.poseNow()
	return pf
}

// T returns the performance's time: effect time, negative during the wind-up.
func (pf *Performance) T() float64 { return pf.t }

// Frozen reports whether a hit has everything frozen right now.
func (pf *Performance) Frozen() bool { return pf.stop > 0 }

// Pose returns the pose the character is in right now.
func (pf *Performance) Pose() Pose { return pf.last }

// Shake returns how far the whole picture is thrown right now, in px.
func (pf *Performance) Shake() (x, y float64) {
	if pf.shakeT <= 0 {
		return 0, 0
	}
	k, f := pf.amp*(pf.shakeT/pf.shake0), math.Floor(pf.shakeT*60)
	return Round(math.Sin(f*2.4) * k), Round(math.Cos(f*3.7) * k * 0.7)
}

func (pf *Performance) poseNow() Pose {
	m := &pf.e.Meta
	tt := pf.t
	if m.Loop && pf.t >= m.Duration {
		tt = math.Mod(pf.t, m.Duration)
	}
	q := PoseAt(pf.track, tt)
	// nothing is asked of him: he breathes
	if !m.Loop && isHeld(pf.track, tt) {
		up := float64(ToInt32(math.Floor(tt*3)) & 1)
		q = Stand.Merge(q).With(Crouch.Is(q.Or(Crouch, 0)+up), FY.Is(q.Or(FY, Stand.v[FY])+up), BY.Is(q.Or(BY, Stand.v[BY])+up))
	}
	pf.last, pf.hasLast = q, true
	return q
}

// Update moves the performance on by dt seconds.
func (pf *Performance) Update(dt float64) {
	if pf.shakeT > 0 {
		pf.shakeT -= dt
	}
	if pf.stop > 0 {
		pf.stop -= dt
		return
	}
	m := &pf.e.Meta
	D := m.Duration
	t0 := pf.t
	pf.t += dt
	t := pf.t
	if t >= 0 {
		if t0 < 0 {
			pf.FX.Update(t)
		} else {
			pf.FX.Update(dt)
		}
	}
	if m.Loop && t >= D {
		if n := int(math.Floor(t / D)); n != pf.loopN {
			pf.loopN, pf.nh, pf.nd = n, 0, 0
		}
	}
	tt := t
	if m.Loop && t >= D {
		tt = math.Mod(t, D)
	}
	for ; pf.nh < len(pf.hits) && pf.hits[pf.nh].T <= tt; pf.nh++ {
		h := pf.hits[pf.nh]
		if h.Stop != 0 {
			pf.stop = math.Max(pf.stop, h.Stop)
		}
		if h.Shake != 0 {
			pf.shakeT, pf.shake0 = h.Shake, h.Shake
			pf.amp = h.Amp.Or(2)
		}
		if !h.NoTarget {
			for i, g := range pf.Targets {
				if !h.At.Set || h.At.V == float64(i) {
					g.Hit(h)
				}
			}
		}
		if pf.OnHit != nil {
			pf.OnHit(h)
		}
	}
	R := pf.r
	for ; pf.nd < len(pf.dust) && pf.dust[pf.nd].T <= tt; pf.nd++ {
		d := pf.dust[pf.nd]
		n := d.N
		if n == 0 {
			n = 6
		}
		for range n {
			dir := d.Dir
			if dir == 0 {
				dir = 1
				if R.Next() < 0.5 {
					dir = -1
				}
			}
			s := speck{x: d.X + R.Range(-3, 3)}
			s.y = -1 - R.Range(0, 2)
			s.vx = dir * R.Range(22, 90)
			s.vy = R.Range(-34, -6)
			s.life = R.Range(0.16, 0.4)
			s.big = R.Next() < 0.45
			s.c = d.C
			pf.specks = append(pf.specks, s)
		}
	}
	for i := range pf.specks {
		k := &pf.specks[i]
		k.age += dt
		k.vx *= 1 - 5*dt
		k.vy += 110 * dt
		k.x += k.vx * dt
		k.y = math.Min(-1, k.y+k.vy*dt)
	}
	pf.specks = slices.DeleteFunc(pf.specks, func(k speck) bool { return k.age >= k.life })
	q := pf.poseNow()
	for _, g := range pf.Targets {
		g.Step(dt)
	}
	// after-images while a key asks for them
	pf.gAcc += dt
	if q.on(Ghost) && pf.gAcc >= 1.0/30 {
		pf.gAcc = 0
		pf.ghosts = append(pf.ghosts, ghost{q: q})
		if len(pf.ghosts) > 7 {
			pf.ghosts = pf.ghosts[1:]
		}
	}
	for i := range pf.ghosts {
		pf.ghosts[i].age += dt
	}
	for len(pf.ghosts) > 0 && pf.ghosts[0].age > ghostLife {
		pf.ghosts = pf.ghosts[1:]
	}
}

// Stop lets a loop end: it fades out and Done goes true.
func (pf *Performance) Stop() {
	pf.FX.Stop()
	pf.stopped = true
}

// Done reports whether the performance is over: a one-shot when its last pose is, a loop once stopped and faded.
func (pf *Performance) Done() bool {
	if pf.stopped {
		return pf.FX.Done()
	}
	return pf.t >= pf.to && pf.stop <= 0
}

// Draw paints the performance into frame: the effect, the character and the targets together. body false leaves the character out, targets false the targets.
func (pf *Performance) Draw(frame *Frame, body, targets bool) {
	W, H := frame.W, frame.H
	q := pf.last
	if !pf.hasLast {
		q = pf.poseNow()
	}
	active := pf.t >= 0
	flip := pf.flip
	FX, FY := pf.x-float64(frame.OX), pf.y-float64(frame.OY)
	var tgt []*Target
	if targets {
		tgt = pf.Targets
	}
	drawTargets := func() {
		for _, g := range tgt {
			g.Draw(frame.Color, W, H, FX, FY, flip)
		}
	}
	drawStraw := func() {
		for _, g := range tgt {
			g.DrawStraw(frame.Color, W, H, FX, FY, flip)
		}
	}
	drawDust := func() {
		for _, k := range pf.specks {
			c := k.c
			if c == 0 {
				c = dustCols[int(math.Min(3, math.Floor((k.age/k.life)*4)))]
			}
			lx, py := Round(k.x), FY+Round(k.y)
			n := 1
			if k.big {
				n = 2
			}
			for i := range n {
				px := FX + lx + float64(i)
				if flip {
					px = FX - lx - float64(i) - 1
				}
				setPx(frame.Color, W, H, px, py, c)
			}
		}
	}
	if !body {
		if active {
			pf.FX.Draw(frame, LayerBack)
		}
		drawTargets()
		if active {
			pf.FX.Draw(frame, LayerFront)
		}
		drawStraw()
		return
	}
	rx, ry := Round(q.Or(X, 0)), Round(q.Or(Y, 0))
	BX, BY := FX+rx, FY+ry
	if flip {
		BX = FX - rx
	}
	r := pf.rig.Posed(q)
	paint := func(i int, color uint32) {
		px, py := BX+float64(r.Xs[i]), BY+float64(r.Ys[i])
		if flip {
			px = BX - float64(r.Xs[i]) - 1
		}
		if px < 0 || py < 0 || px >= float64(W) || py >= float64(H) || px != math.Floor(px) || py != math.Floor(py) {
			return
		}
		j := int(py)*W + int(px)
		frame.Color[j] = color
		// he stays crisp in front of his own after-images
		if len(pf.ghosts) > 0 {
			frame.Light[j] = 0
		}
	}
	drawGhosts := func() {
		for _, g := range pf.ghosts {
			// after-images are light, not paint: they brighten what's behind them and leave nothing muddy
			gr := pf.rig.Posed(g.q)
			gx, gy := FX+Round(g.q.Or(X, 0)), FY+Round(g.q.Or(Y, 0))
			if flip {
				gx = FX - Round(g.q.Or(X, 0))
			}
			k := 0.5 * (1 - g.age/ghostLife)
			r0, g0, b0 := float64(pf.gc&255)*k, float64((pf.gc>>8)&255)*k, float64((pf.gc>>16)&255)*k
			for i := 0; i < gr.N; i++ {
				px, py := gx+float64(gr.Xs[i]), gy+float64(gr.Ys[i])
				if flip {
					px = gx - float64(gr.Xs[i]) - 1
				}
				if px < 0 || py < 0 || px >= float64(W) || py >= float64(H) || px != math.Floor(px) || py != math.Floor(py) {
					continue
				}
				j := int(py)*W + int(px)
				d := frame.Light[j]
				frame.Light[j] = 255<<24 | uint32(ToInt32(math.Min(255, float64((d>>16)&255)+b0)))<<16 | uint32(ToInt32(math.Min(255, float64((d>>8)&255)+g0)))<<8 | uint32(ToInt32(math.Min(255, float64(d&255)+r0)))
			}
		}
	}
	shadow := func() {
		sy, cx := FY, FX+rx
		if flip {
			cx = FX - rx
		}
		ffx, okf := q.Get(FFX)
		fbx, okb := q.Get(FBX)
		spread := math.NaN()
		if okf && okb {
			spread = math.Abs(ffx - fbx)
		}
		hw := float64(10 + ToInt32(spread)>>1 - min(8, ToInt32(math.Abs(ry))>>1))
		if sy < 0 || sy >= float64(H) || sy != math.Floor(sy) || cx != math.Floor(cx) {
			return
		}
		for dy := 0.0; dy < 2; dy++ {
			for x := -hw + dy*2; x <= hw-dy*2; x++ {
				px := cx + x
				if px < 0 || px >= float64(W) || sy+dy >= float64(H) {
					continue
				}
				i := int(sy+dy)*W + int(px)
				frame.Color[i] = frame.Color[i]&0xffffff | max(frame.Color[i]>>24, 110)<<24
			}
		}
	}
	if !pf.act.OnBody {
		// the effect doesn't care about the body: back, body, front
		if active {
			pf.FX.Draw(frame, LayerBack)
		}
		drawTargets()
		shadow()
		drawGhosts()
		for i := 0; i < r.N; i++ {
			paint(i, r.Color[i])
		}
		if active {
			pf.FX.Draw(frame, LayerFront)
		}
		drawStraw()
		drawDust()
		return
	}
	// the effect paints the standing body: draw it there, then carry each body pixel to where the pose put it
	if pf.scratchA == nil || pf.scratchA.W != frame.W || pf.scratchA.H != frame.H {
		pf.scratchA, pf.scratchB = NewFrame(frame.W, frame.H, frame.OX, frame.OY), NewFrame(frame.W, frame.H, frame.OX, frame.OY)
	}
	A, B := pf.scratchA.Clear(), pf.scratchB.Clear()
	if active {
		pf.FX.Draw(A, LayerBack)
	}
	copy(B.Color, A.Color)
	copy(B.Light, A.Light)
	m := &pf.e.Meta
	D := m.Duration
	var tt float64
	switch {
	case m.Loop && pf.t < 0:
		tt = 0
	case m.Loop:
		tt = math.Mod(pf.t, D)
	default:
		tt = ClampTo(pf.t, 0, D)
	}
	alpha := 1.0
	if active {
		alpha = pf.e.actorAlpha(tt, pf.FX.Options)
	}
	actor := pf.FX.Options.Actor
	if actor == nil {
		actor = StockActor(2)
	}
	DrawActor(B.Painter().Place(FX, FY, flip, 0), actor, alpha)
	if active {
		pf.FX.Draw(B, LayerFront)
	}
	follow := pf.act.Follow.Or(1)
	lean, cr := q.Or(Lean, 0)*follow, q.Or(Crouch, 0)*follow
	// what's up on the body goes with it; what's on the floor stays on the floor
	shiftOf := func(ly float64) (float64, float64) {
		k, f := ClampTo((-11-ly)/7, 0, 1), ClampTo((-1-ly)/6, 0, 1)
		return Round(lean*k + rx*f), Round(cr*k + ry*f)
	}
	rest := func(sx, sy int) bool {
		c := float64(sx) - FX + 12
		if flip {
			c = FX - float64(sx) - 1 + 12
		}
		r0 := float64(sy) - FY + 38
		return c >= 0 && r0 >= 0 && c < restW && r0 < restH && c == math.Floor(c) && r0 == math.Floor(r0) && restMask[int(r0)*restW+int(c)] == 1
	}
	C, Ls := frame.Color, frame.Light
	copyPx := func(light bool, sx, sy int, s uint32) {
		dx, dy := shiftOf(float64(sy) - FY)
		if flip {
			dx = -dx
		}
		px, py := float64(sx)+dx, float64(sy)+dy
		if px < 0 || py < 0 || px >= float64(W) || py >= float64(H) {
			return
		}
		i := int(py)*W + int(px)
		switch {
		case light:
			d := Ls[i]
			Ls[i] = 255<<24 | min(255, ((d>>16)&255)+((s>>16)&255))<<16 | min(255, ((d>>8)&255)+((s>>8)&255))<<8 | min(255, (d&255)+(s&255))
		case s>>24 == 255 || C[i] == 0:
			C[i] = s
		default:
			k, d := float64(s>>24)/255, C[i]
			ch := func(n uint) uint32 {
				return uint32(ToInt32(Round(float64((s>>n)&255)*k + float64((d>>n)&255)*(1-k))))
			}
			C[i] = max(s>>24, d>>24)<<24 | ch(16)<<16 | ch(8)<<8 | ch(0)
		}
	}
	drawTargets()
	shadow()
	for sy := 0; sy < H; sy++ {
		for sx := 0; sx < W; sx++ {
			if c := A.Color[sy*W+sx]; c != 0 {
				copyPx(false, sx, sy, c)
			}
		}
	}
	drawGhosts()
	restIndex := func(s int) int {
		c, r0 := s%restW, s/restW
		sx, sy := FX+float64(c)-12, FY+float64(r0)-38
		if flip {
			sx = FX - (float64(c) - 12) - 1
		}
		if sx >= 0 && sy >= 0 && sx < float64(W) && sy < float64(H) && sx == math.Floor(sx) && sy == math.Floor(sy) {
			return int(sy)*W + int(sx)
		}
		return -1
	}
	for i := 0; i < r.N; i++ {
		s := -1
		if r.Src[i] >= 0 {
			s = int(nearest[r.Src[i]])
		}
		if s < 0 {
			if alpha > 0.5 {
				paint(i, r.Color[i])
			}
			continue
		}
		v := restPx[s]
		if j := restIndex(s); j >= 0 {
			v = B.Color[j]
		}
		if v == 0 {
			continue
		}
		if v == restPx[s] {
			paint(i, r.Color[i])
		} else {
			paint(i, carry(v, restPx[s], r.Color[i]))
		}
	}
	for sy := 0; sy < H; sy++ {
		for sx := 0; sx < W; sx++ {
			j := sy*W + sx
			if B.Color[j] != 0 && B.Color[j] != A.Color[j] && !rest(sx, sy) {
				copyPx(false, sx, sy, B.Color[j])
			}
			if B.Light[j]&0xffffff != 0 {
				copyPx(true, sx, sy, B.Light[j])
			}
		}
	}
	drawStraw()
	drawDust()
}

// Timing is an act as plain data, for a game with its own character: how long the wind-up and recovery are, when to freeze and shake, what a target should do, and the poses themselves. Times are in the effect's own seconds: start the effect at 0.
type Timing struct {
	Windup   float64     `json:"windup"`
	Recovery float64     `json:"recovery"`
	OnBody   bool        `json:"onBody"`
	Targets  []float64   `json:"targets"`
	Hits     []TimedHit  `json:"hits"`
	Poses    []TimedPose `json:"poses"`
}

// TimedHit is one hit of a Timing: when, how long to freeze and shake and by how much, how hard it pushes the target, the skin it leaves and which target it hits (all when unsaid).
type TimedHit struct {
	T      float64 `json:"t"`
	Freeze float64 `json:"freeze"`
	Shake  float64 `json:"shake"`
	Amp    float64 `json:"amp"`
	Push   float64 `json:"push"`
	Skin   string  `json:"skin,omitempty"`
	At     *int    `json:"at,omitempty"`
}

// TimedPose is one key of a Timing: when, how the pose arrives, and the pose.
type TimedPose struct {
	T    float64 `json:"t"`
	How  string  `json:"how"`
	Pose Pose    `json:"pose"`
}

// ActTiming returns e's act as plain data, nil when it has none.
func ActTiming(e *Effect) *Timing {
	a := e.Act
	if a == nil {
		return nil
	}
	from, to := ActSpan(e)
	D := e.Meta.Duration
	out := &Timing{Windup: -from, OnBody: a.OnBody, Targets: slices.Clone(a.targets()), Hits: []TimedHit{}, Poses: []TimedPose{}}
	if out.Targets == nil {
		out.Targets = []float64{}
	}
	if !e.Meta.Loop {
		out.Recovery = math.Max(0, to-D)
	}
	for _, h := range a.Hits {
		th := TimedHit{T: h.T, Freeze: h.Stop, Shake: h.Shake, Amp: h.Amp.Or(0), Push: h.Push.Or(1.2), Skin: h.Skin}
		if h.NoTarget {
			th.Push = 0
		}
		if h.At.Set {
			at := int(h.At.V)
			th.At = &at
		}
		out.Hits = append(out.Hits, th)
	}
	for _, k := range a.Poses {
		how := k.How
		if how == "" {
			how = "smooth"
		}
		out.Poses = append(out.Poses, TimedPose{T: k.T, How: how, Pose: k.Pose})
	}
	return out
}

// MarshalJSON writes the pose as pixelfx writes one: an object of the keys it says, by their pixelfx names (switches as true or false), and weapon and offWeapon when it holds something.
func (p Pose) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	for k := range numPoseKeys {
		if !p.Has(k) {
			continue
		}
		if k.isSwitch() {
			m[k.String()] = p.v[k] != 0
		} else {
			m[k.String()] = p.v[k]
		}
	}
	if p.weapon != "" {
		m["weapon"] = p.weapon
	}
	if p.offWeapon != "" {
		m["offWeapon"] = p.offWeapon
	}
	return json.Marshal(m)
}

// Bounds is a box in local space: X0, Y0 its top-left and X1, Y1 its bottom-right.
type Bounds struct {
	X0, Y0, X1, Y1 float64
}

// ActBounds returns the box an act needs in local space, so nothing gets cut off, sampled over its poses with rig (a new one when nil); pass the same rig to Perform.
func ActBounds(e *Effect, rig *Rig) Bounds {
	if rig == nil {
		rig = NewRig()
	}
	w, h, ox, oy := float64(e.Meta.Size[0]), float64(e.Meta.Size[1]), float64(e.Meta.Origin[0]), float64(e.Meta.Origin[1])
	b := Bounds{-ox, -oy, w - ox, h - oy}
	see := func(p Pose) {
		r := rig.Posed(p)
		X, Y := Round(p.Or(X, 0)), Round(p.Or(Y, 0))
		b.X0 = math.Min(b.X0, float64(r.X0)+X-1)
		b.X1 = math.Max(b.X1, float64(r.X1)+X+1)
		b.Y0 = math.Min(b.Y0, float64(r.Y0)+Y-1)
		b.Y1 = math.Max(b.Y1, float64(r.Y1)+Y+1)
	}
	if e.Act != nil && len(e.Act.Poses) > 0 {
		k := e.Act.Poses
		for t := k[0].T; t <= k[len(k)-1].T; t += 1.0 / 12 {
			see(PoseAt(k, t))
		}
		for _, key := range k {
			see(key.Pose)
		}
	} else {
		see(Stand)
	}
	for _, tx := range e.Act.targets() {
		b.X0 = math.Min(b.X0, tx-30)
		b.X1 = math.Max(b.X1, tx+36)
		b.Y0 = math.Min(b.Y0, -56)
	}
	return b
}
