package navbake

import (
	"math"
	"math/big"
)

// maxCoord bounds the absolute value of every snapped coordinate, in grid steps, so that differences fit in 31 bits and the cross products of orient in 63 bits.
const maxCoord = 1 << 29

// ipt is a point snapped to the bake's grid, in grid steps.
type ipt struct {
	x, y int64
}

// less orders points by y, then x.
func (a ipt) less(b ipt) bool {
	if a.y != b.y {
		return a.y < b.y
	}
	return a.x < b.x
}

// orient returns twice the signed area of triangle (a, b, c): positive when a->b->c turns counter-clockwise, zero when the three are collinear. It is exact for coordinates within maxCoord.
func orient(a, b, c ipt) int64 {
	return (b.x-a.x)*(c.y-a.y) - (b.y-a.y)*(c.x-a.x)
}

// sign returns -1, 0 or 1 by the sign of v.
func sign(v int64) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// incircleErrBound is the relative error bound of the floating-point incircle determinant (Shewchuk's iccerrboundA, rounded up).
const incircleErrBound = 1.2e-15

// incircle reports whether d lies strictly inside the circle through the counter-clockwise triangle (a, b, c). It filters with float64 and falls back to exact big.Int arithmetic when the filter cannot decide.
func incircle(a, b, c, d ipt) bool {
	adx, ady := float64(a.x-d.x), float64(a.y-d.y)
	bdx, bdy := float64(b.x-d.x), float64(b.y-d.y)
	cdx, cdy := float64(c.x-d.x), float64(c.y-d.y)
	bdxcdy, cdxbdy := bdx*cdy, cdx*bdy
	cdxady, adxcdy := cdx*ady, adx*cdy
	adxbdy, bdxady := adx*bdy, bdx*ady
	alift := adx*adx + ady*ady
	blift := bdx*bdx + bdy*bdy
	clift := cdx*cdx + cdy*cdy
	det := alift*(bdxcdy-cdxbdy) + blift*(cdxady-adxcdy) + clift*(adxbdy-bdxady)
	permanent := (math.Abs(bdxcdy)+math.Abs(cdxbdy))*alift + (math.Abs(cdxady)+math.Abs(adxcdy))*blift + (math.Abs(adxbdy)+math.Abs(bdxady))*clift
	if bound := incircleErrBound * permanent; det > bound || -det > bound {
		return det > 0
	}
	return incircleExact(a, b, c, d) > 0
}

// incircleExact returns the sign of the incircle determinant of (a, b, c, d) computed exactly.
func incircleExact(a, b, c, d ipt) int {
	bi := func(v int64) *big.Int { return big.NewInt(v) }
	adx, ady := bi(a.x-d.x), bi(a.y-d.y)
	bdx, bdy := bi(b.x-d.x), bi(b.y-d.y)
	cdx, cdy := bi(c.x-d.x), bi(c.y-d.y)
	lift := func(x, y *big.Int) *big.Int {
		var l, t big.Int
		l.Mul(x, x)
		t.Mul(y, y)
		return l.Add(&l, &t)
	}
	cross := func(x1, y1, x2, y2 *big.Int) *big.Int {
		var l, r big.Int
		l.Mul(x1, y2)
		r.Mul(x2, y1)
		return l.Sub(&l, &r)
	}
	var det, t big.Int
	det.Mul(lift(adx, ady), cross(bdx, bdy, cdx, cdy))
	t.Mul(lift(bdx, bdy), cross(cdx, cdy, adx, ady))
	det.Add(&det, &t)
	t.Mul(lift(cdx, cdy), cross(adx, ady, bdx, bdy))
	det.Add(&det, &t)
	return det.Sign()
}

// strictlyBetween reports whether p, known to be collinear with segment (a, b), lies strictly between a and b.
func strictlyBetween(a, b, p ipt) bool {
	if a.x != b.x {
		return min(a.x, b.x) < p.x && p.x < max(a.x, b.x)
	}
	return min(a.y, b.y) < p.y && p.y < max(a.y, b.y)
}

// onSegment reports whether p lies strictly inside segment (a, b).
func onSegment(a, b, p ipt) bool {
	return orient(a, b, p) == 0 && strictlyBetween(a, b, p)
}

// crossPoint returns the grid point nearest to where segments (a, b) and (c, d) properly cross.
func crossPoint(a, b, c, d ipt) ipt {
	o1 := float64(orient(c, d, a))
	o2 := float64(orient(c, d, b))
	t := o1 / (o1 - o2)
	return ipt{
		x: int64(math.Round(float64(a.x) + t*float64(b.x-a.x))),
		y: int64(math.Round(float64(a.y) + t*float64(b.y-a.y))),
	}
}
