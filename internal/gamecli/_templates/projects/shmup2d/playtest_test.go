package main

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/playtest"
	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
)

func makeShooterPlaytestSim(seed uint64, src replay.ActionSource, metrics *playtest.Collector) (playtest.Sim, error) {
	data, err := loadData()
	if err != nil {
		return playtest.Sim{}, err
	}
	stageCopy := *data.stage
	stageCopy.Seed = seed
	w, err := newWorld(&stageCopy, data.patterns, data.atlas, data.explosion, viewport.Width, viewport.Height)
	if err != nil {
		return playtest.Sim{}, err
	}

	stats := &gamedebug.FrameStats{}
	started := false
	seenItems := make(map[*item]string)
	itemSeq := 0

	currentWave := func() (int, string) {
		if w.boss != nil {
			return len(w.stage.Waves) + 1, "boss"
		}
		active := 1
		for i := range w.waves {
			if w.waves[i].sent > 0 {
				active = i + 1
			}
		}
		return active, fmt.Sprintf("wave %d", active)
	}

	observe := func() any {
		waveIdx, _ := currentWave()
		rawBullets := w.enemyShots.Bullets()
		obsBullets := make([]playtest.ShooterBullet, 0, len(rawBullets))
		for _, b := range rawBullets {
			v := b.Velocity()
			obsBullets = append(obsBullets, playtest.ShooterBullet{
				X:      float64(b.X),
				Y:      float64(b.Y),
				VX:     float64(v.X),
				VY:     float64(v.Y),
				Radius: float64(b.Radius),
			})
		}

		obsEnemies := make([]playtest.ShooterTarget, 0, len(w.enemies)+1)
		for _, e := range w.enemies {
			if !e.dead && w.onScreen(e.x, e.y) {
				obsEnemies = append(obsEnemies, playtest.ShooterTarget{
					X:      float64(e.x),
					Y:      float64(e.y),
					Radius: float64(e.def.Radius),
				})
			}
		}
		if b := w.boss; b != nil && b.entered && !b.dead {
			obsEnemies = append(obsEnemies, playtest.ShooterTarget{
				X:      float64(b.x),
				Y:      float64(b.y),
				Radius: float64(b.def.Radius),
				IsBoss: true,
			})
		}

		obsItems := make([]playtest.ShooterPowerUp, 0, len(w.items))
		for _, it := range w.items {
			obsItems = append(obsItems, playtest.ShooterPowerUp{
				ID:   seenItems[it],
				Kind: it.kind,
				X:    float64(it.x),
				Y:    float64(it.y),
			})
		}

		return playtest.ShooterObservation{
			PlayfieldWidth:  float64(w.width),
			PlayfieldHeight: float64(w.height),
			ShipX:           float64(w.ship.x),
			ShipY:           float64(w.ship.y),
			ShipRadius:      float64(w.stage.Ship.Radius),
			ShipAlive:       w.ship.alive,
			Invulnerable:    w.ship.invulnerable > 0,
			Bombs:           w.ship.bombs,
			Wave:            waveIdx,
			Bullets:         obsBullets,
			Enemies:         obsEnemies,
			PowerUps:        obsItems,
		}
	}

	stepFn := func(dt time.Duration) {
		src.Step()
		if !started {
			started = true
			target := 1.0
			for i := range w.stage.Waves {
				playtest.RecordShooterWave(metrics, fmt.Sprintf("wave %d", i+1), &target)
			}
			playtest.RecordShooterWave(metrics, "boss", &target)
		}

		prevLives := w.ship.lives
		in := controls{
			MoveX: float32(src.Axis("moveX")),
			MoveY: float32(src.Axis("moveY")),
			Fire:  src.Pressed("fire"),
			Focus: src.Pressed("focus"),
			Bomb:  src.JustPressed("bomb"),
		}
		w.step(dt, in)

		// Deterministic simulated frame-time readout proportional to bullet count.
		bulletCount := len(w.enemyShots.Bullets()) + len(w.shipShots.Bullets())
		updateUs := time.Duration(2200+bulletCount*6) * time.Microsecond
		drawUs := time.Duration(1000+bulletCount*2) * time.Microsecond
		stats.Record(updateUs+drawUs, updateUs, drawUs)

		// Track newly spawned items.
		for _, it := range w.items {
			if _, ok := seenItems[it]; !ok {
				itemSeq++
				seenItems[it] = fmt.Sprintf("item_%d", itemSeq)
				playtest.RecordShooterPowerUp(metrics, false, float64(it.x), float64(it.y), it.kind)
			}
		}

		// Record power-up pickups reported by the world step.
		for _, ev := range w.events {
			if ev == eventPowerUp {
				playtest.RecordShooterPowerUp(metrics, true, float64(w.ship.x), float64(w.ship.y), "item")
			}
		}

		// Record ship hits (including losing the last life).
		if w.ship.lives < prevLives {
			_, waveLabel := currentWave()
			playtest.RecordShooterHit(metrics, waveLabel, float64(w.ship.x), float64(w.ship.y))
		}

		switch w.outcome {
		case cleared:
			metrics.Finish()
		case lost:
			metrics.Fail("out of lives")
		}
		_ = slices.Contains(w.events, eventShot)
	}

	return playtest.Sim{
		Step:       stepFn,
		Observe:    observe,
		FrameStats: stats,
	}, nil
}

// TestPlaytest runs playtest.yaml's shooter bot through Stage 1.
func TestPlaytest(t *testing.T) {
	playtest.RunTest(t, playtest.FileName, makeShooterPlaytestSim)
}

// TestBotReportCanonicalAndDeterministic checks that playtest.yaml is canonical and repeated runs on the same seeds write byte-identical reports.
func TestBotReportCanonicalAndDeterministic(t *testing.T) {
	raw, err := os.ReadFile(playtest.FileName)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := playtest.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := playtest.EncodeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, encoded) {
		t.Fatalf("playtest.yaml is not canonical;\n%s", encoded)
	}
	cfg.Runs.Count = 2
	r1, err := playtest.Run(cfg, makeShooterPlaytestSim, playtest.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := playtest.Run(cfg, makeShooterPlaytestSim, playtest.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	y1, err := playtest.EncodeReport(r1.Report)
	if err != nil {
		t.Fatal(err)
	}
	y2, err := playtest.EncodeReport(r2.Report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(y1, y2) {
		t.Fatalf("reports differ for identical seeds:\n--- 1 ---\n%s\n--- 2 ---\n%s", y1, y2)
	}
}
