// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/ice/freeze.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// The crack-and-burst recipe (cracks walking out from a point, a white frame, the ice flung apart as shards with ice dust and glints) is adapted from Sprite Fusion's open-source ice (https://www.spritefusion.com/pixel-art-vfx/ice), MIT License, Copyright (c) 2026 Hugo Duprez.

package ice

import (
	"math"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

var (
	freezeFear = pixelfx.Pose{}.With(pixelfx.Turn.Is(0.9), pixelfx.Lean.Is(-2), pixelfx.Crouch.Is(2), pixelfx.FX.Is(-8), pixelfx.FY.Is(-31), pixelfx.BX.Is(9), pixelfx.BY.Is(-31), pixelfx.FFX.Is(-3), pixelfx.FBX.Is(4))
	freezeFree = pixelfx.Pose{}.With(pixelfx.Turn.Is(1), pixelfx.Crouch.Is(5), pixelfx.FX.Is(-12), pixelfx.FY.Is(-18), pixelfx.BX.Is(12), pixelfx.BY.Is(-18), pixelfx.FFX.Is(-5), pixelfx.FBX.Is(6))
)

// Freeze races frost in and up the character, turning it to ice; a faceted block crystallizes round it pane by pane, glints, cracks and bursts, and the character thaws free. It is registered as "ice/freeze".
var Freeze = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Freeze",
		Desc:     "Frost races in and climbs the character, turning it to ice; a faceted block crystallizes round it pane by pane, glints, cracks and bursts, and the character thaws free.",
		Tags:     []string{"ice", "freeze", "frozen", "status", "crowd-control", "one-shot"},
		Size:     [2]int{176, 128},
		Origin:   [2]int{88, 108},
		Duration: freezeDuration,
		Loop:     false,
		FPS:      30,
		BG:       "ground",
		Actor:    2,
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette(icePalette...)},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	Act: &pixelfx.Act{
		OnBody: true,
		Poses: []pixelfx.Key{
			{T: -0.2, Pose: pixelfx.Ready},
			{T: 0.05, Pose: pixelfx.Ready},
			{T: 0.16, Pose: freezeFear, How: "snap"},
			{T: 0.3, Pose: freezeFear.With(pixelfx.Lean.Is(-1), pixelfx.FX.Is(-10), pixelfx.FY.Is(-34))},
			{T: 0.45, Pose: freezeFear.With(pixelfx.Lean.Is(-3), pixelfx.BX.Is(11), pixelfx.BY.Is(-34), pixelfx.FX.Is(-7), pixelfx.FY.Is(-29))},
			{T: 0.62, Pose: freezeFear},
			{T: 2.52, Pose: freezeFear},
			{T: 2.54, Pose: freezeFree, How: "snap"},
			{T: 2.9, Pose: freezeFree.With(pixelfx.Crouch.Is(4))},
			{T: 3.1, Pose: freezeFree.With(pixelfx.Crouch.Is(3), pixelfx.Lean.Is(1))},
			{T: 3.3, Pose: freezeFree.With(pixelfx.Crouch.Is(3), pixelfx.Lean.Is(-1))},
			{T: 4, Pose: pixelfx.Ready},
		},
		Hits: []pixelfx.Hit{
			{T: 0.62, Stop: 0.03, Shake: 0.08, Amp: pixelfx.Val(1)},
			{T: 2.52, Stop: 0.05, Shake: 0.2, Amp: pixelfx.Val(2)},
		},
	},
	New: newFreeze,
}

func init() { pixelfx.Register("ice/freeze", Freeze) }

var (
	freezeDuration = 4.0
	// frost lines race in along the floor to the feet
	freezeSnap = 0.16
	// and climb the character
	freezeClimb = [2]float64{0.14, 0.62}
	// the block crystallizes, pane by pane
	freezeSeal = 0.6
	// light slides across the faces while it holds
	freezeSweeps                                        = [2]float64{1.12, 1.82}
	freezeCrack, freezeCrack2, freezeFlash, freezeBurst = 2.2, 2.34, 2.46, 2.52
	freezeThaw                                          = 0.36
	// the floor frost goes
	freezeMelt  = [2]float64{2.95, 3.7}
	freezeReach = 38.0
	freezeCore  = [2]float64{0, -21}
)

// The block: a crown of three crystal points over straight sides, eleven corners round the character and eight inside, cut into thirteen panes.
var (
	freezeV = map[string][2]float64{
		"l0": {-15, 0}, "l1": {-17, -10}, "l2": {-17, -31}, "p1": {-12, -45}, "n1": {-6, -42}, "p2": {0, -52}, "n2": {6, -43}, "p3": {12, -47}, "r2": {17, -30}, "r1": {16, -9}, "r0": {14, 0},
		"i0": {-6, -36}, "i1": {6, -35}, "i2": {-9, -21}, "i3": {3, -25}, "i4": {9, -13}, "i5": {-5, -8}, "i6": {2, 0}, "i7": {0, -40},
	}
	// freezeVOrder is the order pixelfx lists the corners in.
	freezeVOrder = strings.Fields("l0 l1 l2 p1 n1 p2 n2 p3 r2 r1 r0 i0 i1 i2 i3 i4 i5 i6 i7")
	// freezeFacetDefs are the panes: their corners, how much light each catches (0 shadow .. 1 lit) and when it crystallizes after freezeSeal (from the floor up).
	freezeFacetDefs = []struct {
		k       string
		lit, at float64
	}{
		{"l0 l1 i5 i6", 0.62, 0}, {"i5 i4 i6", 0.42, 0.03}, {"i4 r2 r1 r0 i6", 0.2, 0.05}, {"l1 l2 i2 i5", 0.74, 0.08}, {"i2 i3 i4 i5", 0.5, 0.1},
		{"i1 r2 i4 i3", 0.28, 0.13}, {"l2 i0 i2", 0.7, 0.15}, {"i0 i1 i3 i2", 0.58, 0.18}, {"n1 i7 n2 i1 i0", 0.52, 0.2}, {"i1 n2 p3 r2", 0.4, 0.23},
		{"l2 p1 n1 i0", 0.88, 0.25}, {"p2 n2 i7", 0.66, 0.28}, {"n1 p2 i7", 1, 0.31},
	}
	freezeOutline                          = freezePts("l0 l1 l2 p1 n1 p2 n2 p3 r2 r1 r0")
	freezeX0, freezeX1, freezeY0, freezeY1 = -18, 18, -53, 0
	freezeBW                               = freezeX1 - freezeX0
)

func freezePts(names string) [][2]float64 {
	var out [][2]float64
	for _, v := range strings.Fields(names) {
		out = append(out, freezeV[v])
	}
	return out
}

// freezeFacet is a pane of the block and what is worked out about it.
type freezeFacet struct {
	pts       [][2]float64
	lit, at   float64
	c         polyCenter
	tint      uint32
	alpha     float64
	d, length float64
	g         float64
}

// freezeEdge is an edge of the block, once: its ends, the panes it borders, whether it's on the outline, how it faces the light, whether it lies on the floor and when it first shows.
type freezeEdge struct {
	a, b  [2]float64
	fs    []*freezeFacet
	outer bool
	lit   float64
	floor bool
	at    float64
}

// freezeFeather is a rime feather: a tick of frost standing out from the outline once the frost has passed.
type freezeFeather struct {
	x, y, dx, dy, L, at float64
}

// freezeFloorFrost draws frost on the floor: a crust on the floor line, rime ferns standing up from it, a cold bloom under the surface.
func freezeFloorFrost(p *pixelfx.Painter, C []uint32, t, out float64) {
	W := pixelfx.White
	for x := -freezeReach; x <= freezeReach; x++ {
		d := math.Abs(x+0.5) / freezeReach
		arrive := freezeSnap * (1 - d)
		age := t - arrive
		if age < 0 {
			continue
		}
		a, g, h, s := out*pixelfx.Clamp((1.12-d)/0.2), pixelfx.Smooth(0, 0.4, age), pixelfx.Rand(x, 31), pick(x < 0, -1.0, 1)
		if a <= 0 {
			continue
		}
		p.Dot(x, 0, pick(age < 0.05 || h < 0.3, W, C[6]), a)
		if h < 0.55 {
			L := pixelfx.Round(g * (1 + pixelfx.Rand(x, 7)*3.4) * (1 - d*0.55))
			for j := 1.0; j <= L; j++ {
				p.Dot(x, -j, pick(j == L, W, C[5]), a)
			}
			if L >= 2 {
				p.Dot(x+s, -L+1, C[6], a)
			}
			if L >= 4 {
				p.Dot(x-s, -L+2, C[5], a)
			}
		}
		D := g * (2 + pixelfx.Rand(x, 11)*4) * (1 - d*0.5)
		for j := 1.0; j <= D; j++ {
			var c uint32
			switch {
			case pixelfx.Rand(x*13+j, 5) < 0.07:
				c = W
			case j < 2:
				c = C[5]
			case j < 4:
				c = C[4]
			default:
				c = C[3]
			}
			p.Dot(x, j, c, a*(1.05-j/(D+1)))
		}
	}
}

func newFreeze(o *pixelfx.Options) pixelfx.Instance {
	C, R, A, seed := pixelfx.Fit(o.Palette(), 8), pixelfx.NewRng(o.Seed()), o.Actor, o.Seed()
	ps, bits, lights := newSim(R, C)
	Wh := pixelfx.White
	CORE, SEAL, CRACK, CRACK2, FLASH, BURST, THAW := freezeCore, freezeSeal, freezeCrack, freezeCrack2, freezeFlash, freezeBurst, freezeThaw
	X0, X1, Y0, Y1, BW := freezeX0, freezeX1, freezeY0, freezeY1, freezeBW
	pn := &panes{
		ground: floor0,
		onLand: func(q *pane, x, y, _ float64) {
			// a pane that lands breaks into splinters that skip along the floor, with a puff of ice dust and a glint
			for range 3 {
				bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: x + R.Range(-2, 2), Y: y - 1, VX: q.vx*0.6 + R.Range(-50, 50), VY: -R.Range(30, 100), G: 0.8, Drag: 0.6, Bounce: 0.35, Life: R.Range(0.4, 0.8), A: R.Range(0, tau), VA: R.Range(-18, 18), S: R.Range(1.2, 2.4), C: C[int(R.Int(5, 7))]})
			}
			ps.Spawn(pixelfx.Vapour, x, y-1, R.Range(-25, 25), -R.Range(2, 10), R.Range(0.4, 0.7), C[6], R.Range(1.4, 2.4), 0, 0)
			if R.Chance(0.4) {
				ps.Spawn(pixelfx.StarKind, x, y-2, 0, -6, R.Range(0.2, 0.4), Wh, R.Range(0.8, 1.6), 0, 0)
			}
		},
	}

	// ---- the character: each pixel's frozen color and when the frost reaches it. A randomized flood up from the feet, quicker round the outline, so the front creeps up the edges first in ragged fingers.
	W, H := A.W, A.H
	N := W * H
	on := func(x, y int) bool { return x >= 0 && y >= 0 && x < W && y < H && A.Mask.Data[y*W+x] > 0 }
	px := func(i int) uint32 {
		if i < len(A.Pixels) {
			return A.Pixels[i]
		}
		return 0
	}
	// dist and reach are 32-bit floats, as pixelfx keeps them in Float32Arrays.
	inf := float32(math.Inf(1))
	dist, ice, rim := make([]float32, N), make([]uint32, N), make([]bool, N)
	for i := range dist {
		dist[i] = inf
	}
	var open []int
	for y := range H {
		for x := range W {
			if !on(x, y) {
				continue
			}
			i := y*W + x
			rim[i] = !on(x-1, y) || !on(x+1, y) || !on(x, y-1)
			up := !on(x-1, y) || !on(x, y-1)
			k := iceIndex(px(i), C)
			c := C[k]
			if up {
				c = C[min(7, slices.Index(C, C[k])+1)]
			}
			ice[i] = pixelfx.Mix(c, C[6], 0.2)
			if y == H-1 || !on(x, y+1) {
				if y >= H-4 {
					dist[i] = float32(R.Range(0, 1.5))
					open = append(open, i)
				}
			}
		}
	}
	done := make([]bool, N)
	for len(open) > 0 {
		bi := 0
		for j := 1; j < len(open); j++ {
			if dist[open[j]] < dist[open[bi]] {
				bi = j
			}
		}
		i := open[bi]
		open[bi] = open[len(open)-1]
		open = open[:len(open)-1]
		if done[i] {
			continue
		}
		done[i] = true
		x, y := i%W, i/W
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, -1}, {0, 1}} {
			nx, ny := x+d[0], y+d[1]
			if !on(nx, ny) {
				continue
			}
			j := ny*W + nx
			w := pick(rim[j], 0.55, 1)*pick(d[1] < 0, 1, 1.3) + R.Range(0, 1.5)
			if float64(dist[i])+w < float64(dist[j]) {
				dist[j] = float32(float64(dist[i]) + w)
				open = append(open, j)
			}
		}
	}
	maxD := 0.0
	for i := range N {
		if dist[i] < inf {
			maxD = math.Max(maxD, float64(dist[i]))
		}
	}
	reach := make([]float32, N)
	var order []int
	for i := range N {
		if dist[i] < inf {
			reach[i] = float32(freezeClimb[0] + (freezeClimb[1]-freezeClimb[0])*math.Pow(float64(dist[i])/maxD, 0.85))
			order = append(order, i)
		}
	}
	slices.SortStableFunc(order, func(a, b int) int {
		switch d := reach[a] - reach[b]; {
		case d < 0:
			return -1
		case d > 0:
			return 1
		}
		return 0
	})
	// rime feathers: little ticks of frost standing out from the outline once the frost has passed
	var feathers []freezeFeather
	for _, i := range order {
		x, y := i%W, i/W
		fi := float64(i)
		if !rim[i] || pixelfx.Rand(fi, seed, 3) > 0.42 || y > H-3 {
			continue
		}
		var dx, dy float64
		switch {
		case !on(x-1, y):
			dx = -1
		case !on(x+1, y):
			dx = 1
		default:
			dx = pick(pixelfx.Rand(fi, 9) < 0.5, -1.0, 1)
		}
		switch {
		case !on(x, y-1):
			dy = -1
		default:
			dy = pick(pixelfx.Rand(fi, 4) < 0.5, -1.0, 0)
		}
		feathers = append(feathers, freezeFeather{x: A.X + float64(x) + dx, y: A.Y + float64(y) + dy, dx: dx, dy: dy, L: 1 + math.Floor(pixelfx.Rand(fi, 7)*2.6), at: float64(reach[i]) + 0.04})
	}

	// ---- the block: which pane every pixel belongs to, the panes' looks, the edges they share
	facets := make([]*freezeFacet, len(freezeFacetDefs))
	for i, f := range freezeFacetDefs {
		facets[i] = &freezeFacet{pts: freezePts(f.k), lit: f.lit, at: SEAL + f.at}
	}
	fid := make([]int8, BW*(Y1-Y0))
	for i := range fid {
		fid[i] = -1
	}
	inBlock := func(x, y float64) bool { return inPoly(freezeOutline, x, y) }
	for y := Y0; y < Y1; y++ {
		for x := X0; x < X1; x++ {
			fx, fy := float64(x), float64(y)
			if !inBlock(fx+0.5, fy+0.5) {
				continue
			}
			f := slices.IndexFunc(facets, func(F *freezeFacet) bool { return inPoly(F.pts, fx+0.5, fy+0.5) })
			if f < 0 {
				f = 0
				for k, F := range facets {
					ck, cb := centroid(F.pts), centroid(facets[f].pts)
					if pixelfx.Hypot(ck.x-fx, ck.y-fy) < pixelfx.Hypot(cb.x-fx, cb.y-fy) {
						f = k
					}
				}
			}
			fid[(y-Y0)*BW+(x-X0)] = int8(f)
		}
	}
	at := func(x, y int) int {
		if x < X0 || x >= X1 || y < Y0 || y >= Y1 {
			return -1
		}
		return int(fid[(y-Y0)*BW+(x-X0)])
	}
	for _, F := range facets {
		F.c = centroid(F.pts)
		F.tint = C[int(3+pixelfx.Round(F.lit*3))]
		F.alpha = 0.2 + 0.24*F.lit
		F.d = pixelfx.Clamp((F.c.x + 0.55*F.c.y + 36) / 46)
		for k, q := range F.pts {
			nq := F.pts[(k+1)%len(F.pts)]
			F.length += pixelfx.Hypot(nq[0]-q[0], nq[1]-q[1])
		}
	}
	// every edge once: its ends, the panes it borders, whether it's on the outline and how it faces the light
	var edges []*freezeEdge
	for _, F := range facets {
		for k, a := range F.pts {
			b := F.pts[(k+1)%len(F.pts)]
			i := slices.IndexFunc(edges, func(e *freezeEdge) bool { return (e.a == a && e.b == b) || (e.a == b && e.b == a) })
			var e *freezeEdge
			if i < 0 {
				e = &freezeEdge{a: a, b: b}
				edges = append(edges, e)
			} else {
				e = edges[i]
			}
			e.fs = append(e.fs, F)
		}
	}
	for _, e := range edges {
		x0, y0, x1, y1, c := e.a[0], e.a[1], e.b[0], e.b[1], e.fs[0].c
		nx, ny := y1-y0, x0-x1
		if nx*((x0+x1)/2-c.x)+ny*((y0+y1)/2-c.y) < 0 {
			nx, ny = -nx, -ny
		}
		e.outer = len(e.fs) == 1
		e.lit = (-nx*0.55 - ny*0.84) / pixelfx.Hypot(nx, ny)
		e.floor = y0 == 0 && y1 == 0
		e.at = math.Inf(1)
		for _, F := range e.fs {
			e.at = math.Min(e.at, F.at)
		}
	}
	verts := make([][2]float64, len(freezeVOrder))
	for i, v := range freezeVOrder {
		verts[i] = freezeV[v]
	}
	cracks := fractures(R, inBlock, CORE[0], CORE[1], 3, fractureOpts{len: 9, a0: pixelfx.Val(-2.2), spread: pixelfx.Val(3.6)})
	cracks2 := fractures(R, inBlock, CORE[0]+1, CORE[1]-2, 6, fractureOpts{len: 16, fork: pixelfx.Val(0.22)})

	t, formed, sealed, struck, burst, white := 0.0, 0, false, 0, false, false
	jolt := func() float64 {
		switch {
		case t >= CRACK && t < CRACK+0.06:
			return 1
		case t >= CRACK2 && t < FLASH:
			return pick(int(math.Floor(t*40))%2 != 0, 1.0, -1)
		}
		return 0
	}

	// the frozen character: each pixel turns as the frost reaches it, white at the front; after the burst the ice runs off it from the top down
	drawCharacter := func(p *pixelfx.Painter) {
		thaw := -1.0
		if t >= BURST {
			thaw = pixelfx.Ease.InQuad(pixelfx.Seg(t, BURST, BURST+THAW))*float64(H+2) - 2
		}
		for _, i := range order {
			age := t - float64(reach[i])
			if age < 0 {
				break
			}
			x, y := float64(i%W), float64(i/W)
			if y < thaw {
				continue
			}
			var c uint32
			switch {
			case y < thaw+1.2 && t >= BURST:
				c = C[7]
			case age < 0.016:
				c = Wh
			case age < 0.045:
				c = C[6]
			default:
				c = ice[i]
			}
			p.Px(A.X+x, A.Y+y, c)
			if age < 0.03 {
				p.Light(A.X+x, A.Y+y, C[6], 0.3)
			}
		}
		if t >= BURST {
			return
		}
		for _, f := range feathers {
			g := pixelfx.Clamp((t - f.at) / 0.08)
			if g <= 0 {
				continue
			}
			L := pixelfx.Round(f.L * g)
			for j := 0.0; j < L; j++ {
				p.Px(f.x+f.dx*j, f.y+f.dy*j, pick(j == L-1, Wh, C[6]))
			}
		}
	}

	// the block: panes fill in as they crystallize (edges traced in white, a white flash, then glassy), a light sweep that breaks at every facet, cracks, the white frame before it bursts
	drawBlock := func(p *pixelfx.Painter) {
		if t >= BURST || t < SEAL-0.08 {
			return
		}
		jx, wh := jolt(), t >= FLASH
		// light slides across the block: each facet flashes whole as the light passes it, the lit ones brightest
		for _, F := range facets {
			g := 0.0
			for _, s0 := range freezeSweeps {
				g = math.Max(g, math.Sin(pi*pixelfx.Clamp((t-s0-F.d*0.32)/0.13)))
			}
			F.g = g * (0.35 + 0.65*F.lit)
		}
		for y := Y0; y < Y1; y++ {
			for x := X0; x < X1; x++ {
				f := at(x, y)
				if f < 0 {
					continue
				}
				F := facets[f]
				age := t - F.at
				if age < 0 {
					continue
				}
				fx, fy := float64(x), float64(y)
				if wh {
					p.Px(fx+jx, fy, Wh)
					continue
				}
				if age < 0.03 {
					p.Blend(fx+jx, fy, C[7], 0.55)
					continue
				}
				fresh, g := 1-pixelfx.Clamp((age-0.03)/0.1), F.g
				p.Blend(fx+jx, fy, pick(fresh > 0.5 || g > 0.5, C[6], F.tint), F.alpha+0.25*fresh+0.15*g)
				if g > 0.05 {
					p.Light(fx+jx, fy, C[5], 0.42*g)
				}
			}
		}
		// edges: the outline lit where it faces up and left, cool on the shadow side with a deep rim outside it; inner edges pale; any pane that has just set rings white
		for _, e := range edges {
			if e.floor || t < e.at {
				continue
			}
			x0, y0, x1, y1 := e.a[0], e.a[1], e.b[0], e.b[1]
			fresh := slices.ContainsFunc(e.fs, func(F *freezeFacet) bool { return t-F.at < 0.1 || F.g > 0.6 })
			if e.outer && e.lit < 0 && !wh {
				p.Line(x0+jx+1, y0, x1+jx+1, y1, C[1], 1)
			}
			if e.outer {
				p.GlowLine(x0+jx, y0, x1+jx, y1, 5, C[4], 0.1)
			}
			var c uint32
			switch {
			case wh:
				c = C[6]
			case fresh:
				c = Wh
			case e.outer:
				c = pick(e.lit > 0.3, Wh, pick(e.lit > -0.2, C[6], C[4]))
			default:
				c = pick((y0+y1)/2 < -30, C[6], C[5])
			}
			p.Line(x0+jx, y0, x1+jx, y1, c, 1)
		}
		// a pane about to set: its edges traced in white from its first corner round, a bead of light at the head
		for _, F := range facets {
			age := t - F.at
			if age < -0.07 || age >= 0 {
				continue
			}
			left := ((age + 0.07) / 0.07) * F.length
			for k := 0; k < len(F.pts) && left > 0; k++ {
				x0, y0, x1, y1 := F.pts[k][0], F.pts[k][1], F.pts[(k+1)%len(F.pts)][0], F.pts[(k+1)%len(F.pts)][1]
				L := pixelfx.Hypot(x1-x0, y1-y0)
				v := math.Min(1, left/L)
				left -= L
				p.Line(x0+jx, y0, x0+jx+(x1-x0)*v, y0+(y1-y0)*v, Wh, 1)
				if v < 1 {
					p.Glow(x0+jx+(x1-x0)*v, y0+(y1-y0)*v, 6, C[6], 0.6)
				}
			}
		}
		if t >= CRACK && !wh {
			drawFractures(p, cracks, (t-CRACK)*150, C, crackOpts{ox: jx})
		}
		if t >= CRACK2 && !wh {
			drawFractures(p, cracks2, (t-CRACK2)*190, C, crackOpts{ox: jx})
		}
	}

	shard := func(lo, hi float64) uint32 { return C[int(R.Int(lo, hi))] }
	return &pixelfx.Funcs{
		UpdateFunc: func(dt float64) {
			t += dt
			// frost lines race in along the floor from both sides, vapour riding them
			if t < freezeSnap {
				for _, s := range [2]float64{-1, 1} {
					x := s * freezeReach * (1 - t/freezeSnap)
					if R.Chance(0.12) {
						ps.Spawn(pixelfx.Vapour, x, -R.Range(0, 2), -s*R.Range(40, 90), -R.Range(0, 5), R.Range(0.3, 0.5), C[6], R.Range(1.4, 2.4), 0, 0)
					}
					if R.Chance(0.45) {
						ps.Spawn(pixelfx.Streak, x+s*R.Range(0, 6), -R.Range(1, 4), -s*R.Range(50, 90), 0, R.Range(0.1, 0.16), pick(R.Chance(0.5), C[6], C[5]), 1, 0, 0)
					}
				}
			}
			// stars and motes kicked off the climbing front
			if t > freezeClimb[0] && t < freezeClimb[1]+0.03 {
				lo := 0
				for lo < len(order) && float64(reach[order[lo]]) < t-0.06 {
					lo++
				}
				hi := lo
				for hi < len(order) && float64(reach[order[hi]]) <= t {
					hi++
				}
				if hi > lo && R.Chance(0.45) {
					i := order[int(R.Int(float64(lo), float64(hi-1)))]
					ps.Spawn(pixelfx.StarKind, A.X+float64(i%W)+0.5, A.Y+float64(i/W), R.Range(-8, 8), -R.Range(0, 18), R.Range(0.2, 0.42), pick(R.Chance(0.5), C[6], Wh), R.Range(0.6, 1.9), 0, 0)
				}
				if hi > lo && R.Chance(0.12) {
					i := order[int(R.Int(float64(lo), float64(hi-1)))]
					ps.Spawn(pixelfx.Flake, A.X+float64(i%W), A.Y+float64(i/W), R.Range(-20, 20), -R.Range(10, 30), R.Range(0.5, 0.9), C[6], R.Range(0.5, 1.2), 0, 0)
				}
			}
			// each pane snaps into place with a chip of shards, a puff and a glint
			for formed < len(facets) && t >= facets[formed].at {
				F := facets[formed]
				formed++
				c := centroid(F.pts)
				for range 3 {
					a, sp := math.Atan2(c.y-CORE[1], c.x-CORE[0])+R.Range(-0.7, 0.7), R.Range(50, 120)
					bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: c.x, Y: c.y, VX: math.Cos(a) * sp, VY: math.Sin(a)*sp - 40, G: 0.8, Drag: 0.8, Bounce: 0.3, Life: R.Range(0.35, 0.7), A: R.Range(0, tau), VA: R.Range(-18, 18), S: R.Range(1, 2.2), C: shard(5, 7)})
				}
				if c.y > -14 {
					sx := c.x
					if sx == 0 {
						sx = 1
					}
					ps.Spawn(pixelfx.Vapour, c.x, -1, pixelfx.Sign(sx)*R.Range(10, 30), -R.Range(2, 8), R.Range(0.5, 0.8), C[6], R.Range(2.2, 3.4), 0, 0)
				}
				ps.Spawn(pixelfx.StarKind, c.x+R.Range(-2, 2), c.y+R.Range(-2, 2), 0, -4, R.Range(0.25, 0.45), Wh, R.Range(1.2, 2.2), 0, 0)
				lights.Add(c.x, c.y, 16, C[6], 0.4, 0.12)
			}
			if !sealed && formed == len(facets) {
				sealed = true
				lights.Add(0, -26, 70, C[5], 0.6, 0.4)
				for range 5 {
					ps.Spawn(pixelfx.StarKind, R.Range(-15, 15), -R.Range(6, 44), R.Range(-6, 6), -R.Range(0, 8), R.Range(0.3, 0.6), pick(R.Chance(0.5), Wh, C[6]), R.Range(1, 2.2), 0, 0)
				}
			}
			// while it holds: glints wink on the corners, cold vapour rolls off the foot, a little snow
			if sealed && t < CRACK2 {
				if R.Chance(0.035) {
					v := pixelfx.Pick(R, verts)
					ps.Spawn(pixelfx.StarKind, v[0]+R.Range(-1, 1), v[1]+R.Range(-1, 1), 0, -2, R.Range(0.3, 0.6), pick(R.Chance(0.6), C[6], Wh), R.Range(0.8, 2.2), 0, 0)
				}
				if R.Chance(0.025) {
					s := R.Sign()
					ps.Spawn(pixelfx.Vapour, s*R.Range(12, 18), -R.Range(1, 3), s*R.Range(8, 24), -R.Range(1, 5), R.Range(0.7, 1.1), C[6], R.Range(1.8, 3), 0, 0)
				}
				if R.Chance(0.012) {
					ps.Spawn(pixelfx.Flake, R.Range(-30, 30), -R.Range(56, 76), R.Range(-4, 4), R.Range(4, 10), R.Range(1.2, 1.8), pick(R.Chance(0.5), Wh, C[6]), R.Range(0.5, 1.4), 0, 0)
				}
			}
			// it cracks: a jolt, chips spall off, light leaks from the cracks
			if struck == 0 && t >= CRACK {
				struck = 1
				lights.Add(CORE[0], CORE[1], 26, Wh, 0.4, 0.1)
				for range 4 {
					bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: R.Range(-14, 14), Y: -R.Range(10, 40), VX: R.Range(-40, 40), VY: -R.Range(10, 60), G: 0.9, Drag: 0.6, Bounce: 0.3, Life: R.Range(0.5, 0.9), A: R.Range(0, tau), VA: R.Range(-20, 20), S: R.Range(1, 2), C: shard(5, 7)})
				}
			}
			if struck == 1 && t >= CRACK2 {
				struck = 2
				lights.Add(CORE[0], CORE[1], 40, C[6], 0.35, 0.16)
				for range 3 {
					ps.Spawn(pixelfx.StarKind, R.Range(-12, 12), -R.Range(8, 40), 0, -3, R.Range(0.15, 0.3), Wh, R.Range(1, 2), 0, 0)
				}
			}
			if !white && t >= FLASH {
				white = true
				lights.Add(CORE[0], CORE[1], 60, Wh, 0.9, 0.1)
			}
			// it bursts: the panes fly off tumbling, shards and ice dust, glints, a flurry, a flash of light
			if !burst && t >= BURST {
				burst = true
				lights.Add(CORE[0], CORE[1], 64, Wh, 0.85, 0.14)
				lights.Add(CORE[0], CORE[1]-4, 72, C[4], 0.55, 0.6)
				var chop func(pts [][2]float64, depth int) [][][2]float64
				chop = func(pts [][2]float64, depth int) [][][2]float64 {
					c := centroid(pts)
					if c.area < 20 || depth > 2 {
						return [][][2]float64{pts}
					}
					return splitAll(splitPoly(pts, c.x, c.y, R.Range(0, pi)), func(q [][2]float64) [][][2]float64 { return chop(q, depth+1) })
				}
				for _, F := range facets {
					for _, part := range chop(F.pts, 0) {
						m := centroid(part)
						dx, dy := m.x-CORE[0], m.y-CORE[1]
						d := pixelfx.Hypot(dx, dy)
						if d == 0 {
							d = 1
						}
						sp := R.Range(40, 85) * (1.2 - math.Min(0.5, m.area/120))
						pts := make([][2]float64, len(part))
						for i, q := range part {
							pts[i] = [2]float64{q[0] - m.x, q[1] - m.y}
						}
						np := pane{pts: pts, x: m.x, y: m.y}
						np.vx = (dx/d)*sp + R.Range(-15, 15)
						np.vy = (dy/d)*sp*0.6 - R.Range(90, 190)
						np.va = R.Range(-9, 9)
						np.f = R.Range(-0.8, 0.8)
						np.vf = R.Sign() * R.Range(5, 13)
						// bounce is pixelfx's default for a pane
						np.lit, np.g, np.drag, np.bounce, np.brittle = F.lit, 0.85, 0.7, 0.35, 1
						np.life = R.Range(1, 1.45)
						pn.add(np)
					}
				}
				for range 50 {
					x, y := 0, 0
					for k := 0; k < 12 && at(x, y) < 0; k++ {
						x = int(R.Int(float64(X0), float64(X1-1)))
						y = int(R.Int(float64(Y0), float64(Y1-1)))
					}
					fx, fy := float64(x), float64(y)
					dx, dy := fx-CORE[0], fy-CORE[1]
					d := pixelfx.Hypot(dx, dy)
					if d == 0 {
						d = 1
					}
					sp := R.Range(50, 130)
					bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: fx, Y: fy, VX: (dx / d) * sp, VY: (dy/d)*sp*0.7 - R.Range(60, 170), G: 0.85, Drag: 0.9, Bounce: 0.3, Life: R.Range(0.8, 1.4), A: R.Range(0, tau), VA: R.Range(-22, 22), S: R.Range(1.5, 3.2), C: shard(4, 7)})
				}
				for _, f := range feathers {
					if R.Chance(0.5) {
						bits.Add(pixelfx.Bit{Kind: pixelfx.Shard, X: f.x, Y: f.y, VX: f.dx * R.Range(20, 70), VY: -R.Range(20, 90), G: 0.8, Drag: 0.8, Bounce: 0.3, Life: R.Range(0.4, 0.8), A: R.Range(0, tau), VA: R.Range(-20, 20), S: R.Range(1, 1.8), C: Wh})
					}
				}
				for range 12 {
					s := R.Sign()
					ps.Spawn(pixelfx.Vapour, s*R.Range(2, 16), -R.Range(1, 6), s*R.Range(30, 90), -R.Range(4, 16), R.Range(0.6, 1.1), C[6], R.Range(2.2, 4), 0, 0)
				}
				for range 8 {
					ps.Spawn(pixelfx.Vapour, R.Range(-14, 14), -R.Range(8, 40), R.Range(-30, 30), -R.Range(10, 30), R.Range(0.6, 1), C[6], R.Range(3, 5), 0, 0)
				}
				for range 18 {
					a, sp := R.Range(-pi, 0), R.Range(40, 130)
					ps.Spawn(pixelfx.Flake, R.Range(-10, 10), CORE[1]+R.Range(-12, 12), math.Cos(a)*sp, math.Sin(a)*sp, R.Range(1, 1.5), pick(R.Chance(0.6), Wh, C[6]), R.Range(0.5, 1.6), 0, 0)
				}
				for range 7 {
					ps.Spawn(pixelfx.StarKind, R.Range(-26, 26), -R.Range(6, 52), R.Range(-12, 12), -R.Range(4, 16), R.Range(0.4, 0.8), pick(R.Chance(0.5), Wh, C[6]), R.Range(1, 2.4), 0, 0)
				}
			}
			// glints wink on the debris as it settles
			if burst && t < BURST+0.9 && R.Chance(0.07) {
				list := bits.List
				if len(list) == 0 {
					list = []pixelfx.Bit{{X: 0, Y: -10}}
				}
				b := pixelfx.Pick(R, list)
				ps.Spawn(pixelfx.StarKind, b.X, b.Y-1, 0, -3, R.Range(0.2, 0.4), pick(R.Chance(0.5), Wh, C[6]), R.Range(0.6, 1.6), 0, 0)
			}
			// the last of the ice runs off the character
			if burst && t < BURST+THAW && R.Chance(0.12) {
				y := -float64(H) + pixelfx.Ease.InQuad(pixelfx.Seg(t, BURST, BURST+THAW))*float64(H)
				bits.Add(pixelfx.Bit{Kind: pixelfx.Drip, X: R.Range(-9, 9), Y: y, VY: 10, G: 0.5, Life: 0.8, C: C[5]})
			}
			ps.Update(dt)
			bits.Update(dt)
			pn.update(dt)
			lights.Update(dt)
			endBy(t, freezeDuration, ps, bits, pn)
		},
		DrawBackFunc: func(p *pixelfx.Painter, _ float64) {
			if out := 1 - pixelfx.Smooth(freezeMelt[0], freezeMelt[1], t); out > 0 {
				freezeFloorFrost(p, C, t, out)
			}
			ps.Draw(p, pixelfx.LayerBack)
		},
		DrawFunc: func(p *pixelfx.Painter, _ float64) {
			drawCharacter(p)
			drawBlock(p)
			// the cold light: a pool under the ice, a glow in it that brightens as it cracks, the climbing front's light
			hold, crk := pixelfx.Smooth(freezeClimb[0], SEAL, t)*(1-pixelfx.Smooth(BURST, BURST+0.6, t)), pixelfx.Smooth(CRACK, FLASH, t)
			if hold > 0 {
				p.Glow(0, -2, 34, C[4], 0.22*hold*(0.85+0.15*math.Sin(t*3)))
				p.Glow(CORE[0], CORE[1]-4, 40, C[3], (0.22+0.1*crk)*hold)
			}
			if t > freezeClimb[0] && t < freezeClimb[1] {
				p.Glow(0, -38*pixelfx.Ease.OutQuad(pixelfx.Seg(t, freezeClimb[0], freezeClimb[1])), 18, C[5], 0.28)
			}
			// the seal: a star at the crown as the last pane sets
			if g := t - facets[len(facets)-1].at; g > 0 && g < 0.3 {
				p2 := freezeV["p2"]
				pixelfx.Sparkle(p, p2[0], p2[1], 8*pixelfx.Ease.OutBack(math.Min(1, g/0.06))*(1-g/0.3), C[6], C[4], 0.6)
			}
			if t >= FLASH && t < BURST+0.16 {
				a := pixelfx.Clamp((t - FLASH) / 0.22)
				pixelfx.Flare(p, CORE[0], CORE[1], 34*math.Pow(1-a, 1.4), 1-a, C)
			}
			pn.draw(p, C)
			bits.Draw(p)
			ps.Draw(p, pixelfx.LayerFront)
			lights.Draw(p)
		},
	}
}
