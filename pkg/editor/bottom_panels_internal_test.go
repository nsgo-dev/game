package editor

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/nsgo/core"
)

func TestAnimationDurationAndLaneLeftNaNInfGuards(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   float64
		want time.Duration
	}{
		{"nan", math.NaN(), 0},
		{"pos_inf", math.Inf(1), 0},
		{"neg_inf", math.Inf(-1), 0},
		{"negative", -2.5, 0},
		{"zero", 0, 0},
		{"normal", 1.5, 1500 * time.Millisecond},
	} {
		if got := durationOfSeconds(tc.in); got != tc.want {
			t.Errorf("durationOfSeconds(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := durationOfSeconds(1e20); got <= 0 {
		t.Fatalf("durationOfSeconds(1e20) overflowed to %v, want positive clamped duration", got)
	}

	ed := &Editor{
		clip: &resource.Animation{Length: 2},
	}
	for _, tc := range []struct {
		sec  float64
		want core.Length
	}{
		{math.NaN(), core.Pct(0)},
		{math.Inf(1), core.Pct(0)},
		{math.Inf(-1), core.Pct(0)},
		{-1, core.Pct(0)},
		{0, core.Pct(0)},
		{1, core.Pct(50)},
		{5, core.Pct(100)},
	} {
		if got := ed.laneLeft(tc.sec); got != tc.want {
			t.Fatalf("laneLeft(%v) = %+v, want %+v", tc.sec, got, tc.want)
		}
	}
	for _, badLen := range []float32{0, -1, float32(math.NaN()), float32(math.Inf(1))} {
		ed.clip.Length = badLen
		// clipLengthSeconds falls back to 1 when Length is non-positive or non-finite, so 0.5s maps to 50%.
		if got := ed.laneLeft(0.5); got != core.Pct(50) {
			t.Fatalf("laneLeft(0.5) with clip.Length=%v = %+v, want Pct(50)", badLen, got)
		}
	}
}
