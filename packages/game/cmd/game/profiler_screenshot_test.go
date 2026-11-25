package main

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestEditorProfilerScreenshots mounts the real NSGo Game Editor on macOS with fixture
// performance telemetry mirroring docs/design/editor/PlayMode.dc.html and captures
// screenshots of the Play-mode frame time strip and the full Profiler panel.
func TestEditorProfilerScreenshots(t *testing.T) {
	ed, err := newDemoEditor()
	if err != nil {
		t.Fatalf("newDemoEditor: %v", err)
	}
	ed.ProfilerPollInterval = time.Hour
	t.Cleanup(func() { _ = ed.Close() })

	now := time.Unix(40000, 0)
	prof := debug.NewProfiler(180)
	prof.SetNow(func() time.Time { return now })

	gcMon := debug.NewGCMonitor(debug.FrameHistory)
	mem := debug.NewMemoryTracker()
	mem.SetTotalBudget(2 << 20)
	mem.SetBudget("level_01", 1<<20)
	mem.Track("level_01", "dungeon_tiles.png", debug.AssetImage, 420<<10)
	mem.Track("level_01", "knight_atlas.png", debug.AssetAtlas, 210<<10)
	mem.Track("audio", "dungeon_bgm.pcm", debug.AssetAudio, 540<<10)

	overlay := debug.New(debug.Options{
		Profiler: prof,
		GC:       gcMon,
		Memory:   mem,
		DrawStats: func() debug.DrawStats {
			return debug.DrawStats{DrawCalls: 22, Sprites: 340, Batches: 6}
		},
	})

	bridge := mcp.New(mcp.Config{Overlay: overlay})
	ed.ConnectProfiler(editor.NewBridgeProfilerClient(bridge))

	seed := 7
	rnd := func() float64 {
		seed = (seed*9301 + 49297) % 233280
		return float64(seed) / 233280.0
	}
	recordBatch := func(start, end int) {
		for i := start; i < end; i++ {
			ms := 3.4 + 0.5*math.Sin(float64(i)*0.7) + 0.3*math.Sin(float64(i)*2.3+1) + rnd()*0.9
			switch i {
			case 72:
				ms = 18.2
				overlay.NoteFrameCause("first use of torch_glow compiled its shader")
			case 73:
				ms = 7.1
			case 118:
				ms = 11.4
			case 119:
				ms = 6.4
			}

			prof.BeginFrame()
			t1 := prof.Begin("render_sprites")
			now = now.Add(time.Duration(ms*0.42*1000) * time.Microsecond)
			prof.End(t1)
			t2 := prof.Begin("physics_world")
			now = now.Add(time.Duration(ms*0.28*1000) * time.Microsecond)
			prof.End(t2)
			t3 := prof.Begin("torch_glow")
			now = now.Add(time.Duration(ms*0.18*1000) * time.Microsecond)
			prof.End(t3)
			t4 := prof.Begin("ai_patrol")
			now = now.Add(time.Duration(ms*0.12*1000) * time.Microsecond)
			prof.End(t4)
			prof.EndFrame()

			total := time.Duration(math.Round(ms * float64(time.Millisecond)))
			upd := time.Duration(math.Round(ms * 0.45 * float64(time.Millisecond)))
			drw := time.Duration(math.Round(ms * 0.42 * float64(time.Millisecond)))
			overlay.RecordFrame(total, upd, drw)
		}
		if err := ed.PollProfiler(context.Background()); err != nil {
			t.Fatalf("PollProfiler: %v", err)
		}
	}

	recordBatch(0, 75)
	recordBatch(75, 150)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	if !app.Exists("id:profiler-strip-spike") {
		t.Fatal("expected spike callout visible in profiler strip")
	}
	app.Screenshot("profiler/strip")

	app.Press("id:profiler-strip-open")
	app.Settle()
	if !app.Exists("id:profiler-panel") {
		t.Fatal("expected full profiler panel open after pressing Profiler button")
	}
	app.Screenshot("profiler/panel")
}
