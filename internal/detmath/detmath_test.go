package detmath

import (
	"math"
	"testing"
)

func TestTrigAccuracy(t *testing.T) {
	for i := -1000; i <= 1000; i++ {
		x := float64(i) * 0.037
		gotS, gotC := Sincos(x)
		if s := Sin(x); math.Float64bits(s) != math.Float64bits(gotS) {
			t.Fatalf("Sin(%v) != Sincos(%v).sin: %v vs %v", x, x, s, gotS)
		}
		if c := Cos(x); math.Float64bits(c) != math.Float64bits(gotC) {
			t.Fatalf("Cos(%v) != Sincos(%v).cos: %v vs %v", x, x, c, gotC)
		}
		if diff := math.Abs(gotS - math.Sin(x)); diff > 1e-14 {
			t.Fatalf("Sin(%v) diff=%g", x, diff)
		}
		if diff := math.Abs(gotC - math.Cos(x)); diff > 1e-14 {
			t.Fatalf("Cos(%v) diff=%g", x, diff)
		}
		if math.Abs(gotC) > 0.1 {
			if diff := math.Abs(Tan(x) - math.Tan(x)); diff > 1e-13 {
				t.Fatalf("Tan(%v) diff=%g", x, diff)
			}
		}
	}

	// Large angle test
	for _, x := range []float64{1e5, -1e5, 1e7, -1e7} {
		if diff := math.Abs(Sin(x) - math.Sin(x)); diff > 1e-10 {
			t.Fatalf("Sin(%v) diff=%g", x, diff)
		}
		if diff := math.Abs(Cos(x) - math.Cos(x)); diff > 1e-10 {
			t.Fatalf("Cos(%v) diff=%g", x, diff)
		}
	}
	for _, x := range []float64{1e9, -1e9} {
		s, c := Sincos(x)
		if diff := math.Abs(float64(s*s) + float64(c*c) - 1); diff > 1e-14 {
			t.Fatalf("Sin^2+Cos^2(%v) diff=%g", x, diff)
		}
	}
}

func TestAtan2AcosHypotAccuracy(t *testing.T) {
	vals := []float64{-10, -2.5, -1, -0.7, -0.1, 0, 0.1, 0.7, 1, 2.5, 10}
	for _, y := range vals {
		for _, x := range vals {
			got := Atan2(y, x)
			want := math.Atan2(y, x)
			if diff := math.Abs(got - want); diff > 1e-14 {
				t.Fatalf("Atan2(%v, %v) = %v, want %v (diff=%g)", y, x, got, want, diff)
			}
			gotH := Hypot(y, x)
			wantH := math.Hypot(y, x)
			if diff := math.Abs(gotH - wantH); diff > 1e-14 {
				t.Fatalf("Hypot(%v, %v) = %v, want %v (diff=%g)", y, x, gotH, wantH, diff)
			}
		}
	}

	for i := -100; i <= 100; i++ {
		x := float64(i) / 100.0
		got := Acos(x)
		want := math.Acos(x)
		if diff := math.Abs(got - want); diff > 1e-14 {
			t.Fatalf("Acos(%v) = %v, want %v (diff=%g)", x, got, want, diff)
		}
	}

	if !math.IsNaN(Acos(1.1)) || !math.IsNaN(Acos(-1.1)) || !math.IsNaN(Sin(math.NaN())) || !math.IsNaN(Cos(math.Inf(1))) {
		t.Fatal("expected NaN for out-of-domain inputs")
	}
}

func TestHasherCanonicalFloats(t *testing.T) {
	posZero32 := float32(0)
	negZero32 := float32(math.Copysign(0, -1))
	if Float32Bits(posZero32) != Float32Bits(negZero32) {
		t.Fatal("-0.0 and +0.0 float32 should hash identically")
	}
	posZero64 := float64(0)
	negZero64 := math.Copysign(0, -1)
	if Float64Bits(posZero64) != Float64Bits(negZero64) {
		t.Fatal("-0.0 and +0.0 float64 should hash identically")
	}
}
