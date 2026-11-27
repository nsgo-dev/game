package math2d_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/nsgo/core"
)

const eps = float32(1e-5)

var (
	nan32    = float32(math.NaN())
	posInf32 = float32(math.Inf(1))
	negInf32 = float32(math.Inf(-1))
)

func TestVec2ConstructorsAndCoreRoundTrip(t *testing.T) {
	if got := math2d.V2(3, -4); got != (math2d.Vec2{X: 3, Y: -4}) {
		t.Fatalf("V2(3, -4) = %v", got)
	}
	if got := math2d.Zero(); got != (math2d.Vec2{X: 0, Y: 0}) {
		t.Fatalf("Zero() = %v", got)
	}
	if got := math2d.One(); got != (math2d.Vec2{X: 1, Y: 1}) {
		t.Fatalf("One() = %v", got)
	}
	if got := math2d.UnitX(); got != (math2d.Vec2{X: 1, Y: 0}) {
		t.Fatalf("UnitX() = %v", got)
	}
	if got := math2d.UnitY(); got != (math2d.Vec2{X: 0, Y: 1}) {
		t.Fatalf("UnitY() = %v", got)
	}

	angleCases := []struct {
		name string
		rad  float32
		want math2d.Vec2
	}{
		{"zero", 0, math2d.V2(1, 0)},
		{"pi/2", math.Pi / 2, math2d.V2(0, 1)},
		{"pi", math.Pi, math2d.V2(-1, 0)},
		{"-pi/2", -math.Pi / 2, math2d.V2(0, -1)},
		{"pi/4", math.Pi / 4, math2d.V2(float32(math.Sqrt2/2), float32(math.Sqrt2/2))},
	}
	for _, tc := range angleCases {
		t.Run("FromAngle/"+tc.name, func(t *testing.T) {
			got := math2d.FromAngle(tc.rad)
			if !got.ApproxEqual(tc.want, eps) {
				t.Fatalf("FromAngle(%v) = %v, want %v", tc.rad, got, tc.want)
			}
		})
	}

	roundTripVals := []math2d.Vec2{
		{0, 0},
		{12.5, -34.25},
		{math.MaxFloat32, -math.SmallestNonzeroFloat32},
		{1e-7, 1e7},
	}
	for _, v := range roundTripVals {
		p := v.Point()
		if got := math2d.FromPoint(p); got != v {
			t.Fatalf("Point round-trip(%v) = %v", v, got)
		}
		if p != (core.Point{X: core.Points(v.X), Y: core.Points(v.Y)}) {
			t.Fatalf("Point(%v) = %+v", v, p)
		}
		s := v.Size()
		if got := math2d.FromSize(s); got != v {
			t.Fatalf("Size round-trip(%v) = %v", v, got)
		}
		if s != (core.Size{Width: core.Points(v.X), Height: core.Points(v.Y)}) {
			t.Fatalf("Size(%v) = %+v", v, s)
		}
	}
}

func TestVec2Arithmetic(t *testing.T) {
	cases := []struct {
		name  string
		a, b  math2d.Vec2
		scale float32
		add   math2d.Vec2
		sub   math2d.Vec2
		scl   math2d.Vec2
		mul   math2d.Vec2
		div   math2d.Vec2
		neg   math2d.Vec2
		dot   float32
		cross float32
	}{
		{
			name:  "basic",
			a:     math2d.V2(6, 8),
			b:     math2d.V2(2, 4),
			scale: 0.5,
			add:   math2d.V2(8, 12),
			sub:   math2d.V2(4, 4),
			scl:   math2d.V2(3, 4),
			mul:   math2d.V2(12, 32),
			div:   math2d.V2(3, 2),
			neg:   math2d.V2(-6, -8),
			dot:   44,
			cross: 8,
		},
		{
			name:  "negative",
			a:     math2d.V2(-3, 5),
			b:     math2d.V2(3, -5),
			scale: -2,
			add:   math2d.V2(0, 0),
			sub:   math2d.V2(-6, 10),
			scl:   math2d.V2(6, -10),
			mul:   math2d.V2(-9, -25),
			div:   math2d.V2(-1, -1),
			neg:   math2d.V2(3, -5),
			dot:   -34,
			cross: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Add(tc.b); got != tc.add {
				t.Fatalf("Add = %v, want %v", got, tc.add)
			}
			if got := tc.a.Sub(tc.b); got != tc.sub {
				t.Fatalf("Sub = %v, want %v", got, tc.sub)
			}
			if got := tc.a.Scale(tc.scale); got != tc.scl {
				t.Fatalf("Scale = %v, want %v", got, tc.scl)
			}
			if got := tc.a.Mul(tc.b); got != tc.mul {
				t.Fatalf("Mul = %v, want %v", got, tc.mul)
			}
			if got := tc.a.Div(tc.b); got != tc.div {
				t.Fatalf("Div = %v, want %v", got, tc.div)
			}
			if got := tc.a.Neg(); got != tc.neg {
				t.Fatalf("Neg = %v, want %v", got, tc.neg)
			}
			if got := tc.a.Dot(tc.b); got != tc.dot {
				t.Fatalf("Dot = %v, want %v", got, tc.dot)
			}
			if got := tc.a.Cross(tc.b); got != tc.cross {
				t.Fatalf("Cross = %v, want %v", got, tc.cross)
			}
		})
	}
}

func TestVec2LengthDistanceNormalize(t *testing.T) {
	if got := math2d.V2(3, 4).Len(); got != 5 {
		t.Fatalf("Len(3,4) = %v, want 5", got)
	}
	if got := math2d.V2(3, 4).LenSq(); got != 25 {
		t.Fatalf("LenSq(3,4) = %v, want 25", got)
	}
	if got := math2d.V2(1, 2).Dist(math2d.V2(4, 6)); got != 5 {
		t.Fatalf("Dist = %v, want 5", got)
	}
	if got := math2d.V2(1, 2).DistSq(math2d.V2(4, 6)); got != 25 {
		t.Fatalf("DistSq = %v, want 25", got)
	}
	// Large components should not overflow float32 during Len.
	if got := math2d.V2(3e20, 4e20).Len(); math.Abs(float64(got-5e20))/5e20 > 1e-6 {
		t.Fatalf("Len(3e20, 4e20) = %v, want 5e20", got)
	}

	normCases := []struct {
		name string
		in   math2d.Vec2
		want math2d.Vec2
	}{
		{"3-4-5", math2d.V2(3, 4), math2d.V2(0.6, 0.8)},
		{"negative", math2d.V2(-10, 0), math2d.V2(-1, 0)},
		{"zero", math2d.Zero(), math2d.Zero()},
		{"tiny-subnormal", math2d.V2(1e-40, 0), math2d.V2(1, 0)},
		{"large", math2d.V2(3e30, 4e30), math2d.V2(0.6, 0.8)},
		{"nan-x", math2d.V2(nan32, 1), math2d.Zero()},
		{"nan-y", math2d.V2(1, nan32), math2d.Zero()},
		{"inf", math2d.V2(posInf32, 1), math2d.Zero()},
		{"neginf", math2d.V2(0, negInf32), math2d.Zero()},
	}
	for _, tc := range normCases {
		t.Run("Normalize/"+tc.name, func(t *testing.T) {
			got := tc.in.Normalize()
			if !got.ApproxEqual(tc.want, eps) {
				t.Fatalf("Normalize(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestVec2ClampLenLerpMoveTowards(t *testing.T) {
	clampCases := []struct {
		name   string
		v      math2d.Vec2
		maxLen float32
		want   math2d.Vec2
	}{
		{"under", math2d.V2(3, 4), 10, math2d.V2(3, 4)},
		{"equal", math2d.V2(3, 4), 5, math2d.V2(3, 4)},
		{"over", math2d.V2(6, 8), 5, math2d.V2(3, 4)},
		{"zero-vec", math2d.Zero(), 5, math2d.Zero()},
		{"zero-max", math2d.V2(3, 4), 0, math2d.Zero()},
		{"neg-max", math2d.V2(3, 4), -2, math2d.Zero()},
		{"nan-max", math2d.V2(3, 4), nan32, math2d.Zero()},
		{"nan-vec", math2d.V2(nan32, 4), 5, math2d.Zero()},
		{"inf-vec", math2d.V2(posInf32, 4), 5, math2d.Zero()},
	}
	for _, tc := range clampCases {
		t.Run("ClampLen/"+tc.name, func(t *testing.T) {
			got := tc.v.ClampLen(tc.maxLen)
			if !got.ApproxEqual(tc.want, eps) {
				t.Fatalf("ClampLen(%v, %v) = %v, want %v", tc.v, tc.maxLen, got, tc.want)
			}
		})
	}

	lerpCases := []struct {
		t    float32
		want math2d.Vec2
	}{
		{0, math2d.V2(10, 20)},
		{0.25, math2d.V2(12.5, 25)},
		{0.5, math2d.V2(15, 30)},
		{1, math2d.V2(20, 40)},
		{-0.5, math2d.V2(5, 10)},
		{2, math2d.V2(30, 60)},
	}
	for _, tc := range lerpCases {
		got := math2d.V2(10, 20).Lerp(math2d.V2(20, 40), tc.t)
		if !got.ApproxEqual(tc.want, eps) {
			t.Fatalf("Lerp(t=%v) = %v, want %v", tc.t, got, tc.want)
		}
	}

	moveCases := []struct {
		name     string
		from, to math2d.Vec2
		delta    float32
		want     math2d.Vec2
	}{
		{"partial", math2d.V2(0, 0), math2d.V2(6, 8), 5, math2d.V2(3, 4)},
		{"exact", math2d.V2(0, 0), math2d.V2(3, 4), 5, math2d.V2(3, 4)},
		{"overshoot", math2d.V2(0, 0), math2d.V2(3, 4), 20, math2d.V2(3, 4)},
		{"same", math2d.V2(5, 5), math2d.V2(5, 5), 1, math2d.V2(5, 5)},
		{"zero-delta", math2d.V2(1, 2), math2d.V2(10, 20), 0, math2d.V2(1, 2)},
		{"neg-delta", math2d.V2(1, 2), math2d.V2(10, 20), -5, math2d.V2(1, 2)},
		{"nan-delta", math2d.V2(1, 2), math2d.V2(10, 20), nan32, math2d.V2(1, 2)},
		{"nan-target", math2d.V2(1, 2), math2d.V2(nan32, 20), 5, math2d.V2(1, 2)},
	}
	for _, tc := range moveCases {
		t.Run("MoveTowards/"+tc.name, func(t *testing.T) {
			got := tc.from.MoveTowards(tc.to, tc.delta)
			if !got.ApproxEqual(tc.want, eps) {
				t.Fatalf("MoveTowards = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVec2AngleRotatePerpReflectProjectMinMaxClamp(t *testing.T) {
	if got := math2d.Zero().Angle(); got != 0 {
		t.Fatalf("Zero.Angle() = %v, want 0", got)
	}
	if got := math2d.UnitY().Angle(); math.Abs(float64(got-math.Pi/2)) > float64(eps) {
		t.Fatalf("UnitY.Angle() = %v, want pi/2", got)
	}
	v := math2d.V2(-3, 4)
	if got := math2d.FromAngle(v.Angle()); !got.ApproxEqual(v.Normalize(), eps) {
		t.Fatalf("FromAngle(v.Angle()) = %v, want %v", got, v.Normalize())
	}

	if got := math2d.V2(2, 3).Rotate(0); got != math2d.V2(2, 3) {
		t.Fatalf("Rotate(0) = %v, want (2, 3)", got)
	}
	if got := math2d.V2(1, 0).Rotate(math.Pi / 2); !got.ApproxEqual(math2d.V2(0, 1), eps) {
		t.Fatalf("Rotate(pi/2) = %v, want (0, 1)", got)
	}
	if got := math2d.V2(3, 4).Perp(); got != math2d.V2(-4, 3) {
		t.Fatalf("Perp(3, 4) = %v, want (-4, 3)", got)
	}
	if got := math2d.V2(3, 4).Dot(math2d.V2(3, 4).Perp()); got != 0 {
		t.Fatalf("v.Dot(v.Perp()) = %v, want 0", got)
	}

	reflectCases := []struct {
		name string
		v, n math2d.Vec2
		want math2d.Vec2
	}{
		{"horizontal-wall", math2d.V2(1, 1), math2d.V2(0, -1), math2d.V2(1, -1)},
		{"vertical-wall", math2d.V2(2, -3), math2d.V2(-1, 0), math2d.V2(-2, -3)},
		{"diagonal", math2d.V2(1, 0), math2d.V2(1, 1).Normalize(), math2d.V2(0, -1)},
		{"zero-normal", math2d.V2(2, 3), math2d.Zero(), math2d.V2(2, 3)},
	}
	for _, tc := range reflectCases {
		t.Run("Reflect/"+tc.name, func(t *testing.T) {
			got := tc.v.Reflect(tc.n)
			if !got.ApproxEqual(tc.want, eps) {
				t.Fatalf("Reflect = %v, want %v", got, tc.want)
			}
		})
	}

	projCases := []struct {
		name    string
		v, onto math2d.Vec2
		want    math2d.Vec2
	}{
		{"x-axis", math2d.V2(3, 4), math2d.V2(5, 0), math2d.V2(3, 0)},
		{"diagonal", math2d.V2(2, 0), math2d.V2(2, 2), math2d.V2(1, 1)},
		{"orthogonal", math2d.V2(0, 5), math2d.V2(3, 0), math2d.Zero()},
		{"zero-onto", math2d.V2(3, 4), math2d.Zero(), math2d.Zero()},
		{"nan-onto", math2d.V2(3, 4), math2d.V2(nan32, 1), math2d.Zero()},
		{"inf-onto", math2d.V2(3, 4), math2d.V2(posInf32, 1), math2d.Zero()},
		{"nan-v", math2d.V2(nan32, 4), math2d.V2(1, 0), math2d.Zero()},
	}
	for _, tc := range projCases {
		t.Run("Project/"+tc.name, func(t *testing.T) {
			got := tc.v.Project(tc.onto)
			if !got.ApproxEqual(tc.want, eps) {
				t.Fatalf("Project = %v, want %v", got, tc.want)
			}
		})
	}

	if got := math2d.V2(3, 10).Min(math2d.V2(7, 2)); got != math2d.V2(3, 2) {
		t.Fatalf("Min = %v", got)
	}
	if got := math2d.V2(3, 10).Max(math2d.V2(7, 2)); got != math2d.V2(7, 10) {
		t.Fatalf("Max = %v", got)
	}
	if got := math2d.V2(-5, 25).Clamp(math2d.V2(0, 0), math2d.V2(10, 20)); got != math2d.V2(0, 20) {
		t.Fatalf("Clamp = %v", got)
	}

	// ApproxEqual edge cases
	if !math2d.V2(1, 2).ApproxEqual(math2d.V2(1+1e-6, 2-1e-6), 1e-5) {
		t.Fatal("expected ApproxEqual true")
	}
	if math2d.V2(1, 2).ApproxEqual(math2d.V2(1.1, 2), 1e-5) {
		t.Fatal("expected ApproxEqual false")
	}
	if math2d.V2(1, 2).ApproxEqual(math2d.V2(1, 2), -1) {
		t.Fatal("negative eps must return false")
	}
	if math2d.V2(nan32, 0).ApproxEqual(math2d.V2(nan32, 0), 1) {
		t.Fatal("NaN must return false")
	}
	if math2d.V2(1, 2).ApproxEqual(math2d.V2(1, 2), nan32) {
		t.Fatal("NaN eps must return false")
	}
}

func TestRectConstructorsAccessorsAndCoreRoundTrip(t *testing.T) {
	r := math2d.Rect{X: 10, Y: 20, W: 30, H: 40}
	if r.Min() != math2d.V2(10, 20) {
		t.Fatalf("Min = %v", r.Min())
	}
	if r.Max() != math2d.V2(40, 60) {
		t.Fatalf("Max = %v", r.Max())
	}
	if r.Center() != math2d.V2(25, 40) {
		t.Fatalf("Center = %v", r.Center())
	}
	if r.Size() != math2d.V2(30, 40) {
		t.Fatalf("Size = %v", r.Size())
	}
	if got := math2d.FromCenter(math2d.V2(25, 40), math2d.V2(30, 40)); got != r {
		t.Fatalf("FromCenter = %+v, want %+v", got, r)
	}
	if got := math2d.FromMinMax(math2d.V2(10, 20), math2d.V2(40, 60)); got != r {
		t.Fatalf("FromMinMax = %+v, want %+v", got, r)
	}

	// Lossless round-trip with core.Bounds
	boundsVals := []math2d.Rect{
		{},
		{X: 10.25, Y: -20.5, W: 100.75, H: 200.125},
		{X: -1e6, Y: 1e-6, W: 0, H: -5},
	}
	for _, rc := range boundsVals {
		cb := rc.Bounds()
		if got := math2d.FromBounds(cb); got != rc {
			t.Fatalf("Bounds round-trip(%+v) = %+v", rc, got)
		}
		want := core.Bounds{
			Origin: core.Point{X: core.Points(rc.X), Y: core.Points(rc.Y)},
			Size:   core.Size{Width: core.Points(rc.W), Height: core.Points(rc.H)},
		}
		if cb != want {
			t.Fatalf("Bounds() = %+v, want %+v", cb, want)
		}
	}

	// Normalized with negative extents
	normCases := []struct {
		name string
		in   math2d.Rect
		want math2d.Rect
	}{
		{"positive", math2d.Rect{X: 1, Y: 2, W: 3, H: 4}, math2d.Rect{X: 1, Y: 2, W: 3, H: 4}},
		{"neg-w", math2d.Rect{X: 10, Y: 2, W: -4, H: 5}, math2d.Rect{X: 6, Y: 2, W: 4, H: 5}},
		{"neg-h", math2d.Rect{X: 1, Y: 20, W: 4, H: -6}, math2d.Rect{X: 1, Y: 14, W: 4, H: 6}},
		{"neg-both", math2d.Rect{X: 10, Y: 20, W: -4, H: -6}, math2d.Rect{X: 6, Y: 14, W: 4, H: 6}},
	}
	for _, tc := range normCases {
		t.Run("Normalized/"+tc.name, func(t *testing.T) {
			if got := tc.in.Normalized(); got != tc.want {
				t.Fatalf("Normalized(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestRectOperations(t *testing.T) {
	emptyCases := []struct {
		name  string
		r     math2d.Rect
		empty bool
	}{
		{"positive", math2d.Rect{X: 0, Y: 0, W: 1, H: 1}, false},
		{"zero", math2d.Rect{}, true},
		{"zero-w", math2d.Rect{X: 1, Y: 2, W: 0, H: 5}, true},
		{"zero-h", math2d.Rect{X: 1, Y: 2, W: 5, H: 0}, true},
		{"neg-w", math2d.Rect{X: 10, Y: 2, W: -5, H: 5}, true},
		{"neg-h", math2d.Rect{X: 1, Y: 20, W: 5, H: -5}, true},
		{"nan-x", math2d.Rect{X: nan32, Y: 0, W: 5, H: 5}, true},
		{"nan-y", math2d.Rect{X: 0, Y: nan32, W: 5, H: 5}, true},
		{"nan-w", math2d.Rect{X: 0, Y: 0, W: nan32, H: 5}, true},
		{"nan-h", math2d.Rect{X: 0, Y: 0, W: 5, H: nan32}, true},
	}
	for _, tc := range emptyCases {
		t.Run("Empty/"+tc.name, func(t *testing.T) {
			if got := tc.r.Empty(); got != tc.empty {
				t.Fatalf("Empty(%+v) = %v, want %v", tc.r, got, tc.empty)
			}
		})
	}

	base := math2d.Rect{X: 10, Y: 20, W: 30, H: 40} // [10,40) x [20,60)
	containsCases := []struct {
		name string
		r    math2d.Rect
		p    math2d.Vec2
		want bool
	}{
		{"inside", base, math2d.V2(25, 40), true},
		{"min-corner", base, math2d.V2(10, 20), true},
		{"max-x-edge", base, math2d.V2(40, 30), false},
		{"max-y-edge", base, math2d.V2(20, 60), false},
		{"outside", base, math2d.V2(9.99, 20), false},
		{"empty-rect", math2d.Rect{X: 10, Y: 20, W: 0, H: 40}, math2d.V2(10, 20), false},
		{"neg-rect", math2d.Rect{X: 40, Y: 60, W: -30, H: -40}, math2d.V2(25, 40), false},
		{"nan-point", base, math2d.V2(nan32, 30), false},
		{"nan-rect", math2d.Rect{X: nan32, Y: 20, W: 30, H: 40}, math2d.V2(25, 40), false},
	}
	for _, tc := range containsCases {
		t.Run("Contains/"+tc.name, func(t *testing.T) {
			got := tc.r.Contains(tc.p)
			if got != tc.want {
				t.Fatalf("Contains = %v, want %v", got, tc.want)
			}
			// Parity with NSGo core.Bounds.Contains on valid rects
			if !tc.r.Empty() && tc.p.X == tc.p.X && tc.p.Y == tc.p.Y {
				if coreGot := tc.r.Bounds().Contains(tc.p.Point()); coreGot != got {
					t.Fatalf("core.Bounds.Contains = %v, Rect.Contains = %v", coreGot, got)
				}
			}
		})
	}

	containsRectCases := []struct {
		name string
		a, b math2d.Rect
		want bool
	}{
		{"same", base, base, true},
		{"inside", base, math2d.Rect{X: 15, Y: 25, W: 10, H: 10}, true},
		{"partial", base, math2d.Rect{X: 35, Y: 25, W: 10, H: 10}, false},
		{"disjoint", base, math2d.Rect{X: 100, Y: 100, W: 5, H: 5}, false},
		{"empty-inner", base, math2d.Rect{X: 15, Y: 25, W: 0, H: 0}, false},
		{"empty-outer", math2d.Rect{}, base, false},
		{"neg-inner", base, math2d.Rect{X: 25, Y: 35, W: -5, H: -5}, false},
	}
	for _, tc := range containsRectCases {
		t.Run("ContainsRect/"+tc.name, func(t *testing.T) {
			if got := tc.a.ContainsRect(tc.b); got != tc.want {
				t.Fatalf("ContainsRect = %v, want %v", got, tc.want)
			}
		})
	}

	interCases := []struct {
		name     string
		a, b     math2d.Rect
		overlaps bool
		inter    math2d.Rect
	}{
		{
			name:     "partial",
			a:        math2d.Rect{X: 0, Y: 0, W: 10, H: 10},
			b:        math2d.Rect{X: 5, Y: 3, W: 10, H: 10},
			overlaps: true,
			inter:    math2d.Rect{X: 5, Y: 3, W: 5, H: 7},
		},
		{
			name:     "contained",
			a:        math2d.Rect{X: 0, Y: 0, W: 20, H: 20},
			b:        math2d.Rect{X: 4, Y: 6, W: 5, H: 7},
			overlaps: true,
			inter:    math2d.Rect{X: 4, Y: 6, W: 5, H: 7},
		},
		{
			name:     "edge-touching",
			a:        math2d.Rect{X: 0, Y: 0, W: 10, H: 10},
			b:        math2d.Rect{X: 10, Y: 0, W: 10, H: 10},
			overlaps: false,
		},
		{
			name:     "corner-touching",
			a:        math2d.Rect{X: 0, Y: 0, W: 10, H: 10},
			b:        math2d.Rect{X: 10, Y: 10, W: 10, H: 10},
			overlaps: false,
		},
		{
			name:     "disjoint",
			a:        math2d.Rect{X: 0, Y: 0, W: 5, H: 5},
			b:        math2d.Rect{X: 20, Y: 20, W: 5, H: 5},
			overlaps: false,
		},
		{
			name:     "empty",
			a:        math2d.Rect{X: 0, Y: 0, W: 10, H: 10},
			b:        math2d.Rect{X: 2, Y: 2, W: 0, H: 5},
			overlaps: false,
		},
		{
			name:     "negative-size",
			a:        math2d.Rect{X: 0, Y: 0, W: 10, H: 10},
			b:        math2d.Rect{X: 8, Y: 8, W: -5, H: -5},
			overlaps: false,
		},
		{
			name:     "nan",
			a:        math2d.Rect{X: 0, Y: 0, W: 10, H: 10},
			b:        math2d.Rect{X: nan32, Y: 0, W: 10, H: 10},
			overlaps: false,
		},
	}
	for _, tc := range interCases {
		t.Run("OverlapInter/"+tc.name, func(t *testing.T) {
			if got := tc.a.Overlaps(tc.b); got != tc.overlaps {
				t.Fatalf("Overlaps = %v, want %v", got, tc.overlaps)
			}
			got, ok := tc.a.Intersection(tc.b)
			if ok != tc.overlaps || got != tc.inter {
				t.Fatalf("Intersection = (%+v, %v), want (%+v, %v)", got, ok, tc.inter, tc.overlaps)
			}
		})
	}

	// Union
	u1 := math2d.Rect{X: 0, Y: 10, W: 5, H: 5}
	u2 := math2d.Rect{X: 10, Y: 0, W: 5, H: 20}
	if got := u1.Union(u2); got != (math2d.Rect{X: 0, Y: 0, W: 15, H: 20}) {
		t.Fatalf("Union = %+v", got)
	}
	if got := (math2d.Rect{}).Union(u1); got != u1 {
		t.Fatalf("Empty.Union(u1) = %+v, want %+v", got, u1)
	}
	if got := u1.Union(math2d.Rect{}); got != u1 {
		t.Fatalf("u1.Union(Empty) = %+v, want %+v", got, u1)
	}
	if got := (math2d.Rect{}).Union(math2d.Rect{W: -1, H: -1}); got != (math2d.Rect{}) {
		t.Fatalf("Empty.Union(Neg) = %+v, want zero", got)
	}

	// Expand & Translate
	if got := base.Expand(5); got != (math2d.Rect{X: 5, Y: 15, W: 40, H: 50}) {
		t.Fatalf("Expand(5) = %+v", got)
	}
	if got := base.Expand(-5); got != (math2d.Rect{X: 15, Y: 25, W: 20, H: 30}) {
		t.Fatalf("Expand(-5) = %+v", got)
	}
	if got := base.Expand(-20); !got.Empty() {
		t.Fatalf("Expand(-20) should be empty, got %+v", got)
	}
	if got := base.Translate(math2d.V2(-3, 7)); got != (math2d.Rect{X: 7, Y: 27, W: 30, H: 40}) {
		t.Fatalf("Translate = %+v", got)
	}

	// ClampPoint & ClosestPoint
	clampPtCases := []struct {
		name string
		r    math2d.Rect
		p    math2d.Vec2
		want math2d.Vec2
	}{
		{"inside", base, math2d.V2(20, 30), math2d.V2(20, 30)},
		{"left", base, math2d.V2(0, 30), math2d.V2(10, 30)},
		{"top-right", base, math2d.V2(100, 0), math2d.V2(40, 20)},
		{"bottom-left", base, math2d.V2(-5, 99), math2d.V2(10, 60)},
		{"negative-rect", math2d.Rect{X: 40, Y: 60, W: -30, H: -40}, math2d.V2(0, 100), math2d.V2(10, 60)},
	}
	for _, tc := range clampPtCases {
		t.Run("ClampClosest/"+tc.name, func(t *testing.T) {
			if got := tc.r.ClampPoint(tc.p); got != tc.want {
				t.Fatalf("ClampPoint = %v, want %v", got, tc.want)
			}
			if got := tc.r.ClosestPoint(tc.p); got != tc.want {
				t.Fatalf("ClosestPoint = %v, want %v", got, tc.want)
			}
		})
	}

	if !base.ApproxEqual(math2d.Rect{X: 10, Y: 20, W: 30 + 1e-6, H: 40}, eps) {
		t.Fatal("Rect.ApproxEqual should be true")
	}
	if base.ApproxEqual(math2d.Rect{X: nan32, Y: 20, W: 30, H: 40}, 1) {
		t.Fatal("Rect.ApproxEqual with NaN should be false")
	}
}

func TestCircleOperations(t *testing.T) {
	c := math2d.Circle{Center: math2d.V2(10, 20), Radius: 5}

	containsCases := []struct {
		name string
		c    math2d.Circle
		p    math2d.Vec2
		want bool
	}{
		{"center", c, math2d.V2(10, 20), true},
		{"interior", c, math2d.V2(13, 23), true},
		{"boundary", c, math2d.V2(13, 24), true},
		{"outside", c, math2d.V2(14, 24), false},
		{"zero-radius", math2d.Circle{Center: math2d.V2(0, 0), Radius: 0}, math2d.V2(0, 0), false},
		{"neg-radius", math2d.Circle{Center: math2d.V2(0, 0), Radius: -5}, math2d.V2(0, 0), false},
		{"nan-radius", math2d.Circle{Center: math2d.V2(0, 0), Radius: nan32}, math2d.V2(0, 0), false},
		{"nan-point", c, math2d.V2(nan32, 20), false},
	}
	for _, tc := range containsCases {
		t.Run("Contains/"+tc.name, func(t *testing.T) {
			if got := tc.c.Contains(tc.p); got != tc.want {
				t.Fatalf("Circle.Contains = %v, want %v", got, tc.want)
			}
		})
	}

	overlapCases := []struct {
		name string
		a, b math2d.Circle
		want bool
	}{
		{"overlap", c, math2d.Circle{Center: math2d.V2(16, 20), Radius: 3}, true},
		{"tangent", c, math2d.Circle{Center: math2d.V2(18, 20), Radius: 3}, true},
		{"disjoint", c, math2d.Circle{Center: math2d.V2(19, 20), Radius: 3}, false},
		{"zero-r", c, math2d.Circle{Center: math2d.V2(10, 20), Radius: 0}, false},
		{"neg-r", c, math2d.Circle{Center: math2d.V2(10, 20), Radius: -1}, false},
		{"nan", c, math2d.Circle{Center: math2d.V2(nan32, 20), Radius: 3}, false},
	}
	for _, tc := range overlapCases {
		t.Run("Overlaps/"+tc.name, func(t *testing.T) {
			if got := tc.a.Overlaps(tc.b); got != tc.want {
				t.Fatalf("Circle.Overlaps = %v, want %v", got, tc.want)
			}
		})
	}

	rect := math2d.Rect{X: 0, Y: 0, W: 10, H: 10}
	overlapRectCases := []struct {
		name string
		c    math2d.Circle
		r    math2d.Rect
		want bool
	}{
		{"center-inside", math2d.Circle{Center: math2d.V2(5, 5), Radius: 1}, rect, true},
		{"edge-overlap", math2d.Circle{Center: math2d.V2(12, 5), Radius: 3}, rect, true},
		{"edge-tangent", math2d.Circle{Center: math2d.V2(13, 5), Radius: 3}, rect, true},
		{"edge-disjoint", math2d.Circle{Center: math2d.V2(14, 5), Radius: 3}, rect, false},
		{"corner-overlap", math2d.Circle{Center: math2d.V2(12, 12), Radius: 3}, rect, true},
		// Bounding boxes overlap, but distance to corner (10,10) is sqrt(8)≈2.828 > 2.5
		{"corner-disjoint-aabb-overlap", math2d.Circle{Center: math2d.V2(12, 12), Radius: 2.5}, rect, false},
		{"empty-rect", math2d.Circle{Center: math2d.V2(5, 5), Radius: 10}, math2d.Rect{}, false},
		{"neg-rect", math2d.Circle{Center: math2d.V2(5, 5), Radius: 10}, math2d.Rect{X: 10, Y: 10, W: -10, H: -10}, false},
		{"zero-radius", math2d.Circle{Center: math2d.V2(5, 5), Radius: 0}, rect, false},
		{"nan-center", math2d.Circle{Center: math2d.V2(nan32, 5), Radius: 5}, rect, false},
	}
	for _, tc := range overlapRectCases {
		t.Run("OverlapsRect/"+tc.name, func(t *testing.T) {
			if got := tc.c.OverlapsRect(tc.r); got != tc.want {
				t.Fatalf("Circle.OverlapsRect = %v, want %v", got, tc.want)
			}
		})
	}

	if got := c.Bounds(); got != (math2d.Rect{X: 5, Y: 15, W: 10, H: 10}) {
		t.Fatalf("Circle.Bounds = %+v", got)
	}
	if got := (math2d.Circle{Center: math2d.V2(5, 5), Radius: -2}).Bounds(); got != (math2d.Rect{}) {
		t.Fatalf("negative radius Bounds = %+v, want zero", got)
	}
	if got := (math2d.Circle{Center: math2d.V2(nan32, 5), Radius: 2}).Bounds(); got != (math2d.Rect{}) {
		t.Fatalf("NaN center Bounds = %+v, want zero", got)
	}
	if got := c.Translate(math2d.V2(-4, 6)); got != (math2d.Circle{Center: math2d.V2(6, 26), Radius: 5}) {
		t.Fatalf("Circle.Translate = %+v", got)
	}
	if !c.ApproxEqual(math2d.Circle{Center: math2d.V2(10, 20), Radius: 5 + 1e-6}, eps) {
		t.Fatal("Circle.ApproxEqual should be true")
	}
	if c.ApproxEqual(math2d.Circle{Center: math2d.V2(10, 20), Radius: nan32}, 1) {
		t.Fatal("Circle.ApproxEqual with NaN should be false")
	}
}

func TestAffineTransformsAndCoreRoundTrip(t *testing.T) {
	id := math2d.Identity()
	p := math2d.V2(3, -4)
	if got := id.TransformPoint(p); got != p {
		t.Fatalf("Identity.TransformPoint = %v, want %v", got, p)
	}
	if got := id.TransformVector(p); got != p {
		t.Fatalf("Identity.TransformVector = %v, want %v", got, p)
	}

	tr := math2d.Translate(10, -20)
	if got := tr.TransformPoint(p); got != math2d.V2(13, -24) {
		t.Fatalf("Translate.TransformPoint = %v", got)
	}
	if got := tr.TransformVector(p); got != p {
		t.Fatalf("Translate.TransformVector = %v, want %v", got, p)
	}

	sc := math2d.Scale(2, -3)
	if got := sc.TransformPoint(p); got != math2d.V2(6, 12) {
		t.Fatalf("Scale.TransformPoint = %v", got)
	}

	rot90 := math2d.Rotate(math.Pi / 2)
	if got := rot90.TransformPoint(math2d.V2(1, 0)); !got.ApproxEqual(math2d.V2(0, 1), eps) {
		t.Fatalf("Rotate(pi/2)(1,0) = %v", got)
	}
	if got := rot90.TransformPoint(math2d.V2(0, 1)); !got.ApproxEqual(math2d.V2(-1, 0), eps) {
		t.Fatalf("Rotate(pi/2)(0,1) = %v", got)
	}
	if got := math2d.Rotate(0); got != id {
		t.Fatalf("Rotate(0) = %+v, want Identity", got)
	}

	// RotateAround keeps pivot fixed and rotates other points around it.
	ra := math2d.RotateAround(math.Pi/2, 10, 20)
	if got := ra.TransformPoint(math2d.V2(10, 20)); !got.ApproxEqual(math2d.V2(10, 20), eps) {
		t.Fatalf("RotateAround pivot moved to %v", got)
	}
	if got := ra.TransformPoint(math2d.V2(15, 20)); !got.ApproxEqual(math2d.V2(10, 25), eps) {
		t.Fatalf("RotateAround(15,20) = %v, want (10,25)", got)
	}
	if got := math2d.RotateAround(0, 10, 20); got != id {
		t.Fatalf("RotateAround(0) = %+v, want Identity", got)
	}

	// TRS equals Translate * Rotate * Scale
	pos := math2d.V2(12, -7)
	rot := float32(0.75)
	scl := math2d.V2(2.5, -1.5)
	trs := math2d.TRS(pos, rot, scl)
	composed := math2d.Translate(pos.X, pos.Y).Mul(math2d.Rotate(rot)).Mul(math2d.Scale(scl.X, scl.Y))
	if !trs.ApproxEqual(composed, eps) {
		t.Fatalf("TRS = %+v, composed = %+v", trs, composed)
	}
	// TRS with rot == 0
	trs0 := math2d.TRS(pos, 0, scl)
	composed0 := math2d.Translate(pos.X, pos.Y).Mul(math2d.Scale(scl.X, scl.Y))
	if trs0 != composed0 {
		t.Fatalf("TRS(rot=0) = %+v, composed = %+v", trs0, composed0)
	}

	// Multiplication order: m.Mul(o) applies o first, then m.
	m1 := math2d.Translate(10, 0).Mul(math2d.Scale(2, 2))
	if got := m1.TransformPoint(math2d.V2(1, 1)); got != math2d.V2(12, 2) {
		t.Fatalf("(T*S)(1,1) = %v, want (12, 2)", got)
	}
	m2 := math2d.Scale(2, 2).Mul(math2d.Translate(10, 0))
	if got := m2.TransformPoint(math2d.V2(1, 1)); got != math2d.V2(22, 2) {
		t.Fatalf("(S*T)(1,1) = %v, want (22, 2)", got)
	}

	// Lossless ToCore / FromCore round-trip with core.Matrix3D
	coreCases := []math2d.Affine{
		id,
		tr,
		sc,
		rot90,
		ra,
		trs,
		{A: 1.25, B: -0.5, C: 0.75, D: 2.5, Tx: -100.125, Ty: 42.625},
	}
	for _, a := range coreCases {
		cm := a.ToCore()
		got, ok := math2d.FromCore(cm)
		if !ok || got != a {
			t.Fatalf("core.Matrix3D round-trip(%+v) = (%+v, %v)", a, got, ok)
		}
		// Verify TransformPoint matches core.Matrix3D.Apply
		tp := a.TransformPoint(p)
		cx, cy := cm.Apply(float64(p.X), float64(p.Y))
		if !tp.ApproxEqual(math2d.V2(float32(cx), float32(cy)), 1e-4) {
			t.Fatalf("TransformPoint = %v, Matrix3D.Apply = (%v, %v)", tp, cx, cy)
		}
	}
	// Matrix product matches core.Matrix3D.Mul
	prod := trs.Mul(ra)
	coreProd, ok := math2d.FromCore(trs.ToCore().Mul(ra.ToCore()))
	if !ok || !prod.ApproxEqual(coreProd, 1e-4) {
		t.Fatalf("Mul = %+v, Matrix3D.Mul = (%+v, %v)", prod, coreProd, ok)
	}
	// Non-2D Matrix3D rejected by FromCore
	translateZ := core.Matrix3D{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		1, 2, 3, 1,
	}
	if got, ok := math2d.FromCore(translateZ); ok || got != (math2d.Affine{}) {
		t.Fatalf("FromCore(translateZ) = (%+v, %v), want false", got, ok)
	}
	perspective := core.Matrix3D{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, -0.002,
		0, 0, 0, 1,
	}
	if got, ok := math2d.FromCore(perspective); ok || got != (math2d.Affine{}) {
		t.Fatalf("FromCore(perspective) = (%+v, %v), want false", got, ok)
	}
}

func TestAffineInvert(t *testing.T) {
	invertible := []struct {
		name string
		m    math2d.Affine
	}{
		{"identity", math2d.Identity()},
		{"translate", math2d.Translate(15, -25)},
		{"scale", math2d.Scale(2, 4)},
		{"negative-scale", math2d.Scale(-1, 2)},
		{"rotate", math2d.Rotate(1.1)},
		{"trs", math2d.TRS(math2d.V2(30, -12), 0.7, math2d.V2(2, -3))},
		{"shear", math2d.Affine{A: 1, B: 0.5, C: 0.25, D: 1, Tx: 7, Ty: -9}},
		{"small-scale", math2d.Scale(1e-6, 1e-6)},
	}
	pt := math2d.V2(7.5, -3.25)
	for _, tc := range invertible {
		t.Run("Invertible/"+tc.name, func(t *testing.T) {
			inv, ok := tc.m.Invert()
			if !ok {
				t.Fatalf("expected %+v to be invertible", tc.m)
			}
			if prod := tc.m.Mul(inv); !prod.ApproxEqual(math2d.Identity(), 1e-3) {
				t.Fatalf("m * inv = %+v, want Identity", prod)
			}
			if prod := inv.Mul(tc.m); !prod.ApproxEqual(math2d.Identity(), 1e-3) {
				t.Fatalf("inv * m = %+v, want Identity", prod)
			}
			if back := inv.TransformPoint(tc.m.TransformPoint(pt)); !back.ApproxEqual(pt, 1e-3) {
				t.Fatalf("inv(m(pt)) = %v, want %v", back, pt)
			}
		})
	}

	singular := []struct {
		name string
		m    math2d.Affine
	}{
		{"zero", math2d.Affine{}},
		{"scale-x-zero", math2d.Scale(0, 1)},
		{"scale-y-zero", math2d.Scale(1, 0)},
		{"collinear", math2d.Affine{A: 1, B: 2, C: 2, D: 4, Tx: 5, Ty: 6}},
		{"nan-linear", math2d.Affine{A: nan32, D: 1}},
		{"nan-tx", math2d.Affine{A: 1, D: 1, Tx: nan32}},
		{"inf-linear", math2d.Affine{A: posInf32, D: 1}},
		{"inf-ty", math2d.Affine{A: 1, D: 1, Ty: negInf32}},
		{"overflow-inv", math2d.Scale(1e-40, 1e-40)},
	}
	for _, tc := range singular {
		t.Run("Singular/"+tc.name, func(t *testing.T) {
			got, ok := tc.m.Invert()
			if ok || got != (math2d.Affine{}) {
				t.Fatalf("Invert(%+v) = (%+v, %v), want (zero, false)", tc.m, got, ok)
			}
		})
	}
}

func TestAffineTransformRect(t *testing.T) {
	r := math2d.Rect{X: 0, Y: 0, W: 10, H: 20}
	cases := []struct {
		name string
		m    math2d.Affine
		r    math2d.Rect
		want math2d.Rect
	}{
		{"identity", math2d.Identity(), r, r},
		{"translate", math2d.Translate(5, -3), r, math2d.Rect{X: 5, Y: -3, W: 10, H: 20}},
		{"scale", math2d.Scale(2, 3), r, math2d.Rect{X: 0, Y: 0, W: 20, H: 60}},
		{"flip-x", math2d.Scale(-1, 1), r, math2d.Rect{X: -10, Y: 0, W: 10, H: 20}},
		{"rotate-90", math2d.Rotate(math.Pi / 2), r, math2d.Rect{X: -20, Y: 0, W: 20, H: 10}},
		{
			name: "rotate-45-square",
			m:    math2d.RotateAround(math.Pi/4, 5, 5),
			r:    math2d.Rect{X: 0, Y: 0, W: 10, H: 10},
			want: math2d.FromCenter(math2d.V2(5, 5), math2d.V2(float32(10*math.Sqrt2), float32(10*math.Sqrt2))),
		},
		{"empty-rect", math2d.Translate(5, 5), math2d.Rect{}, math2d.Rect{}},
		{"neg-rect", math2d.Identity(), math2d.Rect{X: 10, Y: 10, W: -5, H: -5}, math2d.Rect{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.m.TransformRect(tc.r)
			if !got.ApproxEqual(tc.want, 1e-4) {
				t.Fatalf("TransformRect = %+v, want %+v", got, tc.want)
			}
		})
	}
	// NaN transform produces an empty rect.
	if got := (math2d.Affine{A: nan32, D: 1}).TransformRect(r); !got.Empty() {
		t.Fatalf("NaN TransformRect should be empty, got %+v", got)
	}
}

func TestZeroAllocations(t *testing.T) {
	var (
		vSink math2d.Vec2
		rSink math2d.Rect
		cSink math2d.Circle
		aSink math2d.Affine
		bSink bool
	)
	allocs := testing.AllocsPerRun(1000, func() {
		v := math2d.V2(3, 4).Add(math2d.One()).Rotate(0.5).Normalize().ClampLen(2).Lerp(math2d.UnitX(), 0.25)
		v = math2d.FromPoint(v.Point())
		v = math2d.FromSize(v.Size())
		r := math2d.FromCenter(v, math2d.V2(10, 20)).Expand(2).Translate(math2d.UnitY())
		r = math2d.FromBounds(r.Bounds())
		inter, ok := r.Intersection(math2d.Rect{X: -5, Y: -5, W: 20, H: 20})
		c := math2d.Circle{Center: v, Radius: 5}.Translate(math2d.UnitX())
		b := c.OverlapsRect(r) && c.Contains(v) && ok
		m := math2d.TRS(v, 0.3, math2d.V2(2, 2)).Mul(math2d.RotateAround(0.2, 1, 2))
		inv, ok2 := m.Invert()
		m2, ok3 := math2d.FromCore(m.ToCore())
		r2 := m2.TransformRect(inter)
		vSink = inv.TransformPoint(v)
		rSink = r2
		cSink = c
		aSink = inv
		bSink = b && ok2 && ok3
	})
	_ = vSink
	_ = rSink
	_ = cSink
	_ = aSink
	_ = bSink
	if allocs != 0 {
		t.Fatalf("expected 0 allocations, got %v", allocs)
	}
}
