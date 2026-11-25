package scifi_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/packages/game/pixelfx/scifi"
)

var allEffects = []struct {
	name string
	fx   *pixelfx.Effect
}{
	{"emp-blast", scifi.EmpBlast},
	{"energy-core", scifi.EnergyCore},
	{"force-field", scifi.ForceField},
	{"glitch", scifi.Glitch},
	{"hologram", scifi.Hologram},
	{"jetpack", scifi.Jetpack},
	{"laser-grid", scifi.LaserGrid},
	{"scanner", scifi.Scanner},
	{"teleporter-pad", scifi.TeleporterPad},
	{"tractor-beam", scifi.TractorBeam},
	{"warp-in", scifi.WarpIn},
	{"warp-out", scifi.WarpOut},
}

func TestScifiMatchesPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckEffect(t, tc.fx, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestScifiIsDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckDeterministic(t, tc.fx)
		})
	}
}

func BenchmarkEmpBlast(b *testing.B)      { pixelfxtest.BenchmarkFrames(b, scifi.EmpBlast) }
func BenchmarkEnergyCore(b *testing.B)    { pixelfxtest.BenchmarkFrames(b, scifi.EnergyCore) }
func BenchmarkForceField(b *testing.B)    { pixelfxtest.BenchmarkFrames(b, scifi.ForceField) }
func BenchmarkGlitch(b *testing.B)        { pixelfxtest.BenchmarkFrames(b, scifi.Glitch) }
func BenchmarkHologram(b *testing.B)      { pixelfxtest.BenchmarkFrames(b, scifi.Hologram) }
func BenchmarkJetpack(b *testing.B)       { pixelfxtest.BenchmarkFrames(b, scifi.Jetpack) }
func BenchmarkLaserGrid(b *testing.B)     { pixelfxtest.BenchmarkFrames(b, scifi.LaserGrid) }
func BenchmarkScanner(b *testing.B)       { pixelfxtest.BenchmarkFrames(b, scifi.Scanner) }
func BenchmarkTeleporterPad(b *testing.B) { pixelfxtest.BenchmarkFrames(b, scifi.TeleporterPad) }
func BenchmarkTractorBeam(b *testing.B)   { pixelfxtest.BenchmarkFrames(b, scifi.TractorBeam) }
func BenchmarkWarpIn(b *testing.B)        { pixelfxtest.BenchmarkFrames(b, scifi.WarpIn) }
func BenchmarkWarpOut(b *testing.B)       { pixelfxtest.BenchmarkFrames(b, scifi.WarpOut) }
