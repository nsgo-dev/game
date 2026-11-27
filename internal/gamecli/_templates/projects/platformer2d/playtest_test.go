package main

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/platformer"
	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
	"github.com/nsgo-dev/game/pkg/tooling/playtest"
)

func makePlatformerPlaytestSim(_ uint64, src replay.ActionSource, metrics *playtest.Collector) (playtest.Sim, error) {
	data := mustLoadData()
	viewport, _ := Project.Viewport()
	w, err := newWorld(data, viewport.Width, viewport.Height)
	if err != nil {
		return playtest.Sim{}, err
	}

	// Collect spike trigger boxes for ObservePlatformerGrid.
	var hazards []physics.Box
	for r, row := range w.level.rows() {
		for c := 0; c < len(row); c++ {
			if row[c] == glyphSpikes {
				hazards = append(hazards, spikeBox(w.tile, c, r))
			}
		}
	}
	exitX := (float32(w.goal[0]) + 0.5) * w.tile
	exitY := (float32(w.goal[1]) + 0.5) * w.tile
	stats := &gamedebug.FrameStats{}
	started := false

	observe := func() any {
		onWall := 0
		if w.body.OnLeftWall {
			onWall = -1
		} else if w.body.OnRightWall {
			onWall = 1
		}
		return playtest.ObservePlatformerGrid(
			w.grid,
			w.body.Bounds(),
			w.body.VX,
			w.body.VY,
			w.body.Grounded,
			onWall,
			exitX,
			exitY,
			hazards,
			w.tile*1.5,
		)
	}

	stepFn := func(dt time.Duration) {
		src.Step()
		if !started {
			started = true
			metrics.StartTimer("clear_time")
			metrics.SetCounter("sparks_total", float64(w.sparksTotal()))
		}
		prevLives := w.lives
		prevCollected := w.collected
		prevCenter := w.center()

		w.step(platformer.Input{
			Move:        float32(src.Axis(actionMove)),
			JumpPressed: src.JustPressed(actionJump),
			JumpHeld:    src.Pressed(actionJump),
		}, dt)

		// Deterministic simulated frame-time readout so repeated headless runs stay byte-identical.
		stats.Record(2400*time.Microsecond, 1800*time.Microsecond, 600*time.Microsecond)

		if w.collected > prevCollected {
			c := w.center()
			metrics.Count("sparks_taken", float64(w.collected-prevCollected))
			metrics.Event("pickup", float64(c.X), float64(c.Y), "spark")
		}
		if w.lives < prevLives {
			cause := "spikes"
			if float32(prevCenter.Y) > float32(w.rows-1)*w.tile {
				cause = "pit"
			}
			metrics.Event("death", float64(prevCenter.X), float64(prevCenter.Y), cause)
		}
		switch w.state {
		case won:
			metrics.StopTimer("clear_time")
			metrics.Finish()
		case lost:
			metrics.Fail("out of lives")
		}
	}

	return playtest.Sim{
		Step:       stepFn,
		Observe:    observe,
		FrameStats: stats,
	}, nil
}

// TestPlaytest runs playtest.yaml's platformer-runner bot through Lantern Hills.
func TestPlaytest(t *testing.T) {
	playtest.RunTest(t, playtest.FileName, makePlatformerPlaytestSim)
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
	cfg.Runs.Count = 4
	r1, err := playtest.Run(cfg, makePlatformerPlaytestSim, playtest.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := playtest.Run(cfg, makePlatformerPlaytestSim, playtest.RunOptions{})
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
