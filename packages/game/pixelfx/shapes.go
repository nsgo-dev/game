// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/core/shapes.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See LICENSE and NOTICE.md.

package pixelfx

import "math"

// Point lists for shapes effects keep reaching for: lightning paths, stars, polygons, trails.

// Bolt returns a jagged lightning path from (x0, y0) to (x1, y1) by midpoint displacement, depth times over. jag is how far a midpoint may move sideways, as a share of the segment's length (pixelfx's defaults are 0.22 and 5).
func Bolt(r *Rng, x0, y0, x1, y1, jag float64, depth int) [][2]float64 {
	pts := [][2]float64{{x0, y0}, {x1, y1}}
	for range depth {
		next := make([][2]float64, 0, len(pts)*2-1)
		next = append(next, pts[0])
		for i := 1; i < len(pts); i++ {
			ax, ay, bx, by := pts[i-1][0], pts[i-1][1], pts[i][0], pts[i][1]
			dx, dy := bx-ax, by-ay
			L := Hypot(dx, dy)
			off := (r.Next()*2 - 1) * jag * L
			l := L
			if l == 0 {
				l = 1
			}
			next = append(next, [2]float64{(ax+bx)/2 + (-dy/l)*off, (ay+by)/2 + (dx/l)*off}, pts[i])
		}
		pts = next
	}
	return pts
}

// ForkOpts are how Forks branches off a path: Count branches, each up to Len px long, with their own Jag and Spread (pixelfx's defaults are 3, 20, 0.3 and 0.9).
type ForkOpts struct {
	Count            int
	Len, Jag, Spread float64
}

// Forks returns branches off a bolt path, each a bolt of depth 3.
func Forks(r *Rng, path [][2]float64, o ForkOpts) [][][2]float64 {
	out := make([][][2]float64, 0, o.Count)
	for range o.Count {
		i := 1 + int(math.Floor(r.Next()*float64(len(path)-2)))
		x, y, px, py := path[i][0], path[i][1], path[i-1][0], path[i-1][1]
		side := 1.0
		if r.Next() < 0.5 {
			side = -1
		}
		a := math.Atan2(y-py, x-px) + side*o.Spread*(0.4+0.6*r.Next())
		l := o.Len * (0.4 + 0.6*r.Next())
		out = append(out, Bolt(r, x, y, x+math.Cos(a)*l, y+math.Sin(a)*l, o.Jag, 3))
	}
	return out
}

// Star returns the points of a star with n points between radii r1 (tips) and r0 (inner corners), turned by rot (pixelfx's defaults are 5 and -π/2).
func Star(cx, cy, r0, r1 float64, n int, rot float64) [][2]float64 {
	pts := make([][2]float64, 0, n*2)
	for i := range n * 2 {
		a := rot + (float64(i)*math.Pi)/float64(n)
		r := r1
		if i%2 != 0 {
			r = r0
		}
		pts = append(pts, [2]float64{cx + math.Cos(a)*r, cy + math.Sin(a)*r})
	}
	return pts
}

// Ngon returns the corners of a regular polygon with n sides, turned by rot.
func Ngon(cx, cy, r float64, n int, rot float64) [][2]float64 {
	pts := make([][2]float64, n)
	for i := range pts {
		pts[i] = [2]float64{cx + math.Cos(rot+(float64(i)*Tau)/float64(n))*r, cy + math.Sin(rot+(float64(i)*Tau)/float64(n))*r}
	}
	return pts
}

// Trail returns pos(t - i*dt) for i = 0..n-1, held at time 0: a moving thing's trail, newest first.
func Trail(pos func(t float64) [2]float64, t float64, n int, dt float64) [][2]float64 {
	out := make([][2]float64, n)
	for i := range out {
		out[i] = pos(math.Max(0, t-float64(i)*dt))
	}
	return out
}
