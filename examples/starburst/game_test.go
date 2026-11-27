package main

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestProceduralAtlasZeroExternalAssets(t *testing.T) {
	atlas := NewProceduralAtlas()
	if atlas.Image.Pixels == nil {
		t.Fatal("expected procedural atlas to carry in-memory RGBA Pixels")
	}
	if atlas.Image.URI != "" {
		t.Fatalf("expected zero external URI, got %q", atlas.Image.URI)
	}
	required := []string{"ship", "drone", "swarm", "bullet", "orb", "spark", "ring"}
	for _, name := range required {
		f, ok := atlas.Frames[name]
		if !ok {
			t.Fatalf("atlas missing required frame %q", name)
		}
		if f.W <= 0 || f.H <= 0 {
			t.Fatalf("frame %q has invalid size %vx%v", name, f.W, f.H)
		}
		// Verify at least one visible pixel exists inside the frame bounds.
		visible := 0
		for y := int(f.Y); y < int(f.Y+f.H); y++ {
			for x := int(f.X); x < int(f.X+f.W); x++ {
				if atlas.Image.Pixels.RGBAAt(x, y).A > 0 {
					visible++
				}
			}
		}
		if visible == 0 {
			t.Fatalf("frame %q has no visible pixels in procedural RGBA atlas", name)
		}
	}
}

func TestDeterministicSeededGameplay(t *testing.T) {
	a1 := NewArena(4242)
	a2 := NewArena(4242)

	if a1.Wave != 1 || a1.ActiveEnemies() == 0 {
		t.Fatalf("expected initial wave 1 with spawned enemies, got wave=%d enemies=%d", a1.Wave, a1.ActiveEnemies())
	}

	var sawBullets, sawParticles, sawShake bool
	for step := range 180 {
		// Give a subtle deterministic weave so thruster particles and orb pickups trigger.
		if step < 40 {
			a1.Control.MoveX, a2.Control.MoveX = 0.6, 0.6
		} else if step < 80 {
			a1.Control.MoveX, a2.Control.MoveX = -0.6, -0.6
		} else {
			a1.Control.MoveX, a2.Control.MoveX = 0, 0
		}
		a1.Step(FixedDT)
		a2.Step(FixedDT)

		if a1.ActiveBullets() > 0 {
			sawBullets = true
		}
		if a1.ActiveParticles() > 0 {
			sawParticles = true
		}
		if off := a1.Camera.ShakeOffset(); off.X != 0 || off.Y != 0 {
			sawShake = true
		}
	}

	if !sawBullets {
		t.Fatal("expected player to fire plasma bullets during gameplay")
	}
	if !sawParticles {
		t.Fatal("expected particles.Emitter bursts/trails during combat")
	}
	if !sawShake {
		t.Fatal("expected camera shake when enemies explode")
	}
	if a1.Score <= 0 {
		t.Fatalf("expected enemies destroyed with score > 0, got score=%d", a1.Score)
	}

	// Same seed + identical steps must stay bit-for-bit identical.
	if a1.Score != a2.Score ||
		a1.Wave != a2.Wave ||
		a1.ActiveEnemies() != a2.ActiveEnemies() ||
		a1.ActiveBullets() != a2.ActiveBullets() ||
		a1.ActiveOrbs() != a2.ActiveOrbs() ||
		a1.ActiveParticles() != a2.ActiveParticles() ||
		math.Float32bits(a1.Player.X) != math.Float32bits(a2.Player.X) ||
		math.Float32bits(a1.Player.Y) != math.Float32bits(a2.Player.Y) {
		t.Fatalf("determinism mismatch between identical seeded runs:\na1: %+v\na2: %+v", a1.HUD(), a2.HUD())
	}

	// Trigger Nova with an enemy right next to the player and verify blast + shake + particle burst.
	a1.spawnEnemyAt(EnemySwarm, a1.Player.X+24, a1.Player.Y)
	scoreBeforeNova := a1.Score
	a1.TriggerNova()
	a1.Step(FixedDT)
	if a1.Score <= scoreBeforeNova {
		t.Fatalf("expected Nova shockwave to destroy nearby enemy and raise score: before=%d after=%d", scoreBeforeNova, a1.Score)
	}
	if off := a1.Camera.ShakeOffset(); off.X == 0 && off.Y == 0 {
		t.Fatal("expected Nova shockwave to trigger camera shake")
	}
	// Shake decays back to zero once duration elapses.
	for range 30 {
		a1.Step(FixedDT)
	}
	if a1.novaRingTimer != 0 {
		t.Fatalf("expected nova ring timer to finish, got %v", a1.novaRingTimer)
	}
}

func TestStressMode1000SpritesUnderLoop(t *testing.T) {
	var gc *GameController
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		gc, node = NewGameView(cx, 777)
		return node
	}, nsgotest.Size(960, 540))

	// Toggle 1000-sprite stress mode via the native HUD button.
	app.Press("btn-stress")
	app.Settle()

	if !gc.Arena.StressMode {
		t.Fatal("expected StressMode to be enabled after pressing btn-stress")
	}
	if got := gc.Arena.ActiveSprites(); got < 1000 {
		t.Fatalf("initial StressMode sprites = %d, want >= 1000", got)
	}

	// Snapshot positions of the first 20 enemies to confirm they actively move over 60 steps.
	type pos struct{ x, y float32 }
	before := make([]pos, 20)
	for i := range before {
		before[i] = pos{gc.Arena.enemies[i].X, gc.Arena.enemies[i].Y}
	}

	// Advance 60 fixed-step frames (1 second at 60 Hz).
	app.Advance(time.Second)

	if gc.Arena.Steps < 60 {
		t.Fatalf("expected >= 60 fixed steps in 1s, got %d", gc.Arena.Steps)
	}
	if got := gc.Arena.ActiveSprites(); got < 1000 {
		t.Fatalf("StressMode sprites after 60 steps = %d, want >= 1000", got)
	}

	moved := 0
	for i := range before {
		if i < len(gc.Arena.enemies) {
			if gc.Arena.enemies[i].X != before[i].x || gc.Arena.enemies[i].Y != before[i].y {
				moved++
			}
		}
	}
	if moved == 0 {
		t.Fatal("expected StressMode sprites to move across 60 steps")
	}

	// Verify the recorded Canvas ops actually drew >= 1000 sprites from our procedural atlas.
	ops := app.Find("arena-canvas").Ops
	ims := gametest.ImageDraws(ops)
	for _, im := range ims {
		if im.Image.Pixels == nil {
			t.Fatalf("image draw missing procedural atlas Pixels: %+v", im)
		}
	}
	if len(ims) < 1000 {
		t.Fatalf("drew %d sprites, want >= 1000", len(ims))
	}

	// Verify HUD reflects the active sprite count and Stress mode status.
	spritesText := app.Find("hud-sprites").Text
	if !strings.Contains(spritesText, "SPRITES ") {
		t.Fatalf("unexpected hud-sprites text: %q", spritesText)
	}
	if got := app.Find("hud-wave").Text; got != "STRESS" {
		t.Fatalf("hud-wave in stress mode = %q, want STRESS", got)
	}
}

func TestHUDControlsPauseRestartAndInput(t *testing.T) {
	var gc *GameController
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		gc, node = NewGameView(cx, 1337)
		return node
	}, nsgotest.Size(960, 540))

	if got := app.Find("hud-wave").Text; got != "WAVE 1" {
		t.Fatalf("initial hud-wave = %q, want WAVE 1", got)
	}
	if got := app.Find("hud-hp").Text; got != "HP 5/5" {
		t.Fatalf("initial hud-hp = %q, want HP 5/5", got)
	}

	// Keyboard movement ("d" moves ship toward +X).
	startX := gc.Arena.Player.X
	app.Key("d")
	app.Advance(200 * time.Millisecond)
	if gc.Arena.Player.X <= startX+5 {
		t.Fatalf("expected 'd' key to move player right: startX=%v now=%v", startX, gc.Arena.Player.X)
	}

	// Mouse hover aims toward the cursor in world space.
	app.HoverAt("arena-view", core.Point{X: 840, Y: 270})
	app.Advance(50 * time.Millisecond)
	if !gc.Arena.Control.HasWorldAim {
		t.Fatal("expected mouse hover over arena-view to enable world aim")
	}

	// Touch virtual pad region triggers move_left via input.Actions (mapped through Viewport.ViewToGame).
	// In a 960x540 window (2x scale of 480x270), touchMoveLeftRect (8..52, 184..226) maps to view (16..104, 368..452).
	xBeforeTouch := gc.Arena.Player.X
	finger := app.Touch("arena-view", core.Point{X: 60, Y: 410})
	app.Advance(250 * time.Millisecond)
	finger.Lift()
	if gc.Arena.Player.X >= xBeforeTouch {
		t.Fatalf("expected touch left region to move player left: before=%v after=%v", xBeforeTouch, gc.Arena.Player.X)
	}

	// Advance so combat scores points.
	app.Advance(1500 * time.Millisecond)
	if gc.Arena.Score <= 0 {
		t.Fatalf("expected score > 0 after combat, got %d", gc.Arena.Score)
	}

	// Pause via HUD button freezes step count and reveals the pause overlay.
	app.Press("btn-pause")
	app.Settle()
	if !gc.Arena.Paused {
		t.Fatal("expected Paused=true after pressing btn-pause")
	}
	if !app.Exists("overlay-status") {
		t.Fatal("expected overlay-status to appear while paused")
	}
	stepsPaused := gc.Arena.Steps
	app.Advance(500 * time.Millisecond)
	if gc.Arena.Steps != stepsPaused {
		t.Fatalf("steps advanced while paused: before=%d after=%d", stepsPaused, gc.Arena.Steps)
	}

	// Resume via the overlay button.
	app.Press("btn-overlay-action")
	app.Settle()
	if gc.Arena.Paused {
		t.Fatal("expected Paused=false after pressing btn-overlay-action")
	}
	if app.Exists("overlay-status") {
		t.Fatal("expected overlay-status to hide on resume")
	}
	app.Advance(200 * time.Millisecond)
	if gc.Arena.Steps <= stepsPaused {
		t.Fatalf("expected steps to resume advancing after unpause: paused=%d now=%d", stepsPaused, gc.Arena.Steps)
	}

	// Restart via HUD button resets wave, score, HP, and paused state cleanly.
	app.Press("btn-restart")
	app.Settle()
	if gc.Arena.Score != 0 || gc.Arena.Wave != 1 || gc.Arena.Player.HP != playerMaxHP || gc.Arena.Paused || gc.Arena.GameOver {
		t.Fatalf("unexpected state after restart: %+v", gc.Arena.HUD())
	}
	if got := app.Find("hud-score").Text; got != "SCORE 000000" {
		t.Fatalf("hud-score after restart = %q, want SCORE 000000", got)
	}
}

func TestSteadyStateStressStepZeroAllocs(t *testing.T) {
	arena := NewArena(9001)
	arena.SetStressMode(true)
	// Warm up emitters and pools past any first-use capacity growth.
	for range 30 {
		arena.Step(FixedDT)
	}
	if got := arena.ActiveSprites(); got < 1000 {
		t.Fatalf("StressMode sprites = %d, want >= 1000", got)
	}
	allocs := testing.AllocsPerRun(60, func() {
		arena.Step(FixedDT)
	})
	if allocs != 0 {
		t.Fatalf("steady-state 1000-sprite Step allocated %v times/step, want 0", allocs)
	}
}

func BenchmarkArena1000Sprites(b *testing.B) {
	arena := NewArena(2026)
	arena.SetStressMode(true)
	// Warm up pools and batch sorting buffer.
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 8192)}
	for range 20 {
		arena.Step(FixedDT)
		d.Ops = d.Ops[:0]
		arena.DrawWorld(&d)
	}
	if got := arena.ActiveSprites(); got < 1000 {
		b.Fatalf("StressMode sprites = %d, want >= 1000", got)
	}

	b.Run("StepOnly", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			arena.Step(FixedDT)
		}
		b.ReportMetric(float64(arena.ActiveSprites()), "sprites")
	})

	b.Run("StepAndBatchDraw", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			arena.Step(FixedDT)
			d.Ops = d.Ops[:0]
			arena.DrawWorld(&d)
		}
		b.ReportMetric(float64(arena.ActiveSprites()), "sprites")
	})
}
