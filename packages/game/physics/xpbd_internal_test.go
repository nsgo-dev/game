package physics

import (
	"math"
	"testing"
)

func TestShapeMatchGainMatchesPow(t *testing.T) {
	stiffnesses := []float32{1e-6, 0.01, 0.1, 0.35, 0.4, 0.5, 0.9, 0.999, 1 - 1.0/(1<<24), 1}
	for _, s := range stiffnesses {
		stiff := float64(s)
		for iters := 0; iters <= 64; iters++ {
			got := shapeMatchGain(stiff, iters)
			want := 1 - math.Pow(1-stiff, 1/float64(max(1, iters)))
			if math.Abs(got-want) > 1e-14 {
				t.Fatalf("shapeMatchGain(%v, %d) = %v, want %v", stiff, iters, got, want)
			}
		}
	}
	if got := shapeMatchGain(math.NaN(), 6); got != 0 {
		t.Fatalf("shapeMatchGain(NaN, 6) = %v, want 0", got)
	}
}
