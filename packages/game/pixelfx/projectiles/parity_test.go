package projectiles_test

import (
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/internal/pixelfxtest"
	"github.com/nsgo-dev/game/packages/game/pixelfx/projectiles"
)

var allEffects = []struct {
	name   string
	effect *pixelfx.Effect
}{
	{"arrow", projectiles.Arrow},
	{"bullet-tracer", projectiles.BulletTracer},
	{"cannonball", projectiles.Cannonball},
	{"energy-disc", projectiles.EnergyDisc},
	{"flare", projectiles.Flare},
	{"homing-missile", projectiles.HomingMissile},
	{"laser-bolt", projectiles.LaserBolt},
	{"magic-missile", projectiles.MagicMissile},
	{"muzzle-pistol", projectiles.MuzzlePistol},
	{"muzzle-shotgun", projectiles.MuzzleShotgun},
	{"muzzle-smg", projectiles.MuzzleSMG},
	{"plasma-shot", projectiles.PlasmaShot},
	{"rocket", projectiles.Rocket},
}

func TestProjectilesMatchPixelfx(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckEffect(t, tc.effect, filepath.Join("testdata", "ref", tc.name))
		})
	}
}

func TestProjectilesAreDeterministic(t *testing.T) {
	for _, tc := range allEffects {
		t.Run(tc.name, func(t *testing.T) {
			pixelfxtest.CheckDeterministic(t, tc.effect)
		})
	}
}

func BenchmarkProjectiles(b *testing.B) {
	for _, tc := range allEffects {
		b.Run(tc.name, func(b *testing.B) {
			pixelfxtest.BenchmarkFrames(b, tc.effect)
		})
	}
}
