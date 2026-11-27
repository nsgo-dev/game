package melee_test

import (
	"hash/fnv"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/pkg/pixelfx/melee"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"claw-swipe", melee.ClawSwipe},
	{"dagger-flurry", melee.DaggerFlurry},
	{"double-slash", melee.DoubleSlash},
	{"hammer-smash", melee.HammerSmash},
	{"heavy-cleave", melee.HeavyCleave},
	{"scythe-reap", melee.ScytheReap},
	{"spear-sweep", melee.SpearSweep},
	{"spin-slash", melee.SpinSlash},
	{"sword-slash", melee.SwordSlash},
	{"thrust", melee.Thrust},
	{"uppercut", melee.Uppercut},
	{"whip-crack", melee.WhipCrack},
}

func TestMeleeMatchesPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestMeleeIsDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func TestMeleeRegistered(t *testing.T) {
	for _, tc := range allEffects {
		if e, ok := pixelfx.Lookup("melee/" + tc.name); !ok || e != tc.effect {
			t.Errorf("melee/%s is not registered as its effect", tc.name)
		}
	}
}

// TestMeleeOwnOptions renders every effect, sheet and act, with each of its own options moved off its default (numbers to larger, negative, zero and huge values, colors and ramps to others) and checks that each render finishes and is the same twice.
func TestMeleeOwnOptions(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, opt := range tc.effect.Meta.Options {
				for _, v := range offDefault(t, opt) {
					o := pixelfx.Options{}.Set(opt.Name, v)
					if a, b := renderHashes(tc.effect, o), renderHashes(tc.effect, o); !slices.Equal(a, b) {
						t.Errorf("%s=%s: two renders differ", opt.Name, v)
					}
				}
			}
		})
	}
}

// TestMeleeSizesAreClamped checks that the size options are kept between 1 and 416 px: a negative size renders as 1 (whip-crack's used to panic and sword-slash's to hang) and a huge one as 416.
func TestMeleeSizesAreClamped(t *testing.T) {
	for _, tc := range []struct {
		effect *pixelfx.Effect
		option string
	}{
		{melee.SwordSlash, "radius"},
		{melee.SpinSlash, "radius"},
		{melee.Thrust, "reach"},
		{melee.WhipCrack, "length"},
	} {
		t.Run(tc.effect.ID, func(t *testing.T) {
			t.Parallel()
			at := func(x float64) []uint64 {
				return renderHashes(tc.effect, pixelfx.Options{}.Set(tc.option, pixelfx.Num(x)))
			}
			if !slices.Equal(at(-10), at(1)) {
				t.Errorf("%s=-10 does not render as %s=1", tc.option, tc.option)
			}
			if !slices.Equal(at(math.NaN()), at(1)) {
				t.Errorf("%s=NaN does not render as %s=1", tc.option, tc.option)
			}
			if !slices.Equal(at(1e9), at(416)) {
				t.Errorf("%s=1e9 does not render as %s=416", tc.option, tc.option)
			}
		})
	}
}

// offDefault returns values for opt other than its default.
func offDefault(t *testing.T, opt pixelfx.Option) []pixelfx.Value {
	t.Helper()
	d := opt.Default
	switch d.Kind() {
	case pixelfx.KindNumber:
		x, err := strconv.ParseFloat(d.String(), 64)
		if err != nil {
			t.Fatalf("%s: default %s: %v", opt.Name, d, err)
		}
		return []pixelfx.Value{pixelfx.Num(x*1.5 + 1), pixelfx.Num(-10), pixelfx.Num(0), pixelfx.Num(1e9)}
	case pixelfx.KindColor:
		return []pixelfx.Value{pixelfx.Color("#ffffff"), pixelfx.Color("#000000")}
	case pixelfx.KindRamp:
		colors := strings.Split(d.String(), ",")
		slices.Reverse(colors)
		v, err := pixelfx.ParseValue(pixelfx.KindRamp, strings.Join(colors, ","))
		if err != nil {
			t.Fatalf("%s: reversed ramp: %v", opt.Name, err)
		}
		return []pixelfx.Value{v, pixelfx.Palette("#000000", "#ffffff")}
	}
	t.Fatalf("%s: no off-default values for kind %s", opt.Name, d.Kind())
	return nil
}

// renderHashes renders e's sheet with o, and its act when it has one, and returns each frame's hash.
func renderHashes(e *pixelfx.Effect, o pixelfx.Options) []uint64 {
	var out []uint64
	add := func(f *pixelfx.Frame) {
		h := fnv.New64a()
		h.Write(pixelfx.Composite(f, nil, nil, 1))
		out = append(out, h.Sum64())
	}
	pixelfx.EachFrame(e, o, 0, func(_ int, f *pixelfx.Frame) { add(f) })
	if e.Act != nil {
		pixelfx.EachActFrame(e, o, 0, func(_ int, _, _, _ float64, f *pixelfx.Frame) { add(f) })
	}
	return out
}

func BenchmarkSwordSlash(b *testing.B) { pixelfxtest.BenchmarkFrames(b, melee.SwordSlash) }

func BenchmarkSwordSlashActed(b *testing.B) { pixelfxtest.BenchmarkActFrames(b, melee.SwordSlash) }

func BenchmarkHammerSmash(b *testing.B) { pixelfxtest.BenchmarkFrames(b, melee.HammerSmash) }

func BenchmarkScytheReap(b *testing.B) { pixelfxtest.BenchmarkFrames(b, melee.ScytheReap) }
