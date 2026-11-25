package arcane_test

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/arcane"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"arcane-beam", arcane.ArcaneBeam},
	{"arcane-missiles", arcane.ArcaneMissiles},
	{"arcane-orb", arcane.ArcaneOrb},
	{"blink-in", arcane.BlinkIn},
	{"blink-out", arcane.BlinkOut},
	{"magic-circle-cast", arcane.MagicCircleCast},
	{"mana-burst", arcane.ManaBurst},
	{"portal", arcane.Portal},
	{"rune-circle", arcane.RuneCircle},
	{"spell-charge", arcane.SpellCharge},
	{"star-fall", arcane.StarFall},
	{"time-stop", arcane.TimeStop},
}

func TestArcaneMatchesPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestArcaneIsDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

// Every effect pixelfx's arcane category has is registered under its own name, and has references.
func TestArcaneRegistersEveryEffect(t *testing.T) {
	var want []string
	for _, tc := range allEffects {
		want = append(want, "arcane/"+tc.name)
		if e, ok := pixelfx.Lookup("arcane/" + tc.name); !ok || e != tc.effect {
			t.Errorf("arcane/%s is not registered as %s", tc.name, tc.effect.Meta.Name)
		}
	}
	var got []string
	for _, id := range pixelfx.IDs() {
		if strings.HasPrefix(id, "arcane/") {
			got = append(got, id)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("registered %v, want %v", got, want)
	}
	entries, err := os.ReadDir(filepath.Join("testdata", "ref"))
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, de := range entries {
		if de.IsDir() {
			refs = append(refs, "arcane/"+de.Name())
		}
	}
	if !slices.Equal(refs, want) {
		t.Errorf("references for %v, want %v", refs, want)
	}
}

func TestPortalSurvivesEdgeOptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts pixelfx.Options
	}{
		{"width=0", pixelfx.Options{}.Set("width", pixelfx.Num(0))},
		{"height=0", pixelfx.Options{}.Set("height", pixelfx.Num(0))},
		{"width=NaN", pixelfx.Options{}.Set("width", pixelfx.Num(math.NaN()))},
		{"height=NaN", pixelfx.Options{}.Set("height", pixelfx.Num(math.NaN()))},
		{"width=-5", pixelfx.Options{}.Set("width", pixelfx.Num(-5))},
		{"height=-5", pixelfx.Options{}.Set("height", pixelfx.Num(-5))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if fr := pixelfx.Render(arcane.Portal, tc.opts, 0); len(fr.Pixels) == 0 {
				t.Fatalf("rendered no frames")
			}
		})
	}
}

func BenchmarkPortal(b *testing.B) { pixelfxtest.BenchmarkFrames(b, arcane.Portal) }

func BenchmarkMagicCircleCast(b *testing.B) { pixelfxtest.BenchmarkFrames(b, arcane.MagicCircleCast) }
