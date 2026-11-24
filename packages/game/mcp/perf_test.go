package mcp_test

import (
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func ms(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func approxEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

// TestPerfReadoutMatchesDebugOverlay verifies that game_perf_readout returns
// values matching debug.Overlay's frame timing, draw statistics, GC/allocation
// counters, asset memory, and profiler scopes, including the rendered overlay HUD.
func TestPerfReadoutMatchesDebugOverlay(t *testing.T) {
	now := time.Unix(5000, 0)
	prof := debug.NewProfiler(60)
	prof.SetNow(func() time.Time { return now })

	gcMon := debug.NewGCMonitor(debug.FrameHistory)
	mem := debug.NewMemoryTracker()
	mem.SetTotalBudget(2 << 20)
	mem.SetBudget("level1", 1<<20)
	mem.Track("level1", "tiles.png", debug.AssetImage, 256<<10)
	mem.Track("level1", "sprites.atlas", debug.AssetAtlas, 128<<10)
	mem.Track("audio", "bgm.pcm", debug.AssetAudio, 512<<10)

	drawStats := debug.DrawStats{
		DrawCalls: 14,
		Sprites:   320,
		Batches:   4,
		Triangles: 640,
		Vertices:  1280,
	}

	overlay := debug.New(debug.Options{
		Visible:   true,
		Profiler:  prof,
		GC:        gcMon,
		Memory:    mem,
		DrawStats: func() debug.DrawStats { return drawStats },
	})

	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{
			Overlay: overlay,
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{
			Style:    core.Style{Width: core.Pt(400), Height: core.Pt(300)},
			Children: []core.Node{overlay.View(cx)},
		}
	})

	type frameInput struct {
		total, update, draw time.Duration
		phys, render, ai    time.Duration
		allocBytes          int
		triggerGC           bool
	}
	frames := []frameInput{
		{total: 10 * time.Millisecond, update: 3 * time.Millisecond, draw: 2 * time.Millisecond, phys: 2 * time.Millisecond, render: 2 * time.Millisecond, ai: 1 * time.Millisecond},
		{total: 15 * time.Millisecond, update: 4 * time.Millisecond, draw: 3 * time.Millisecond, phys: 3 * time.Millisecond, render: 3 * time.Millisecond, ai: 1 * time.Millisecond, allocBytes: 4096},
		{total: 20 * time.Millisecond, update: 5 * time.Millisecond, draw: 4 * time.Millisecond, phys: 4 * time.Millisecond, render: 4 * time.Millisecond, ai: 1 * time.Millisecond},
		{total: 25 * time.Millisecond, update: 6 * time.Millisecond, draw: 5 * time.Millisecond, phys: 5 * time.Millisecond, render: 4 * time.Millisecond, ai: 2 * time.Millisecond, triggerGC: true},
		{total: 30 * time.Millisecond, update: 7 * time.Millisecond, draw: 6 * time.Millisecond, phys: 6 * time.Millisecond, render: 5 * time.Millisecond, ai: 2 * time.Millisecond, allocBytes: 8192},
	}

	var escapeSink [][]byte
	app.Post(func() {
		for _, f := range frames {
			prof.BeginFrame()
			tPhys := prof.Begin("physics")
			now = now.Add(f.phys)
			prof.End(tPhys)

			tRender := prof.Begin("render")
			now = now.Add(f.render)
			prof.End(tRender)

			tAI := prof.Begin("ai")
			now = now.Add(f.ai)
			prof.End(tAI)
			prof.EndFrame()

			if f.allocBytes > 0 {
				buf := make([]byte, f.allocBytes)
				buf[0] = 1
				escapeSink = append(escapeSink, buf)
			}
			if f.triggerGC {
				runtime.GC()
			}
			overlay.RecordFrame(f.total, f.update, f.draw)
		}
	})
	app.Settle()
	runtime.KeepAlive(escapeSink)

	// Read default readout (without history or zones).
	res := callTool[mcp.PerfReadoutResult](t, app, cx, mcp.ToolPerfReadout, nil)

	stats := overlay.Stats()
	avg := stats.Average()
	latest := stats.Latest()

	if !res.Sources.Overlay || !res.Sources.Profiler || !res.Sources.GC || !res.Sources.Memory || !res.Sources.DrawStats || res.Sources.Render3D {
		t.Fatalf("sources = %+v, want overlay/profiler/gc/memory/draw_stats=true render3d=false", res.Sources)
	}
	if res.SampleCount != stats.Len() || res.SampleCount != 5 {
		t.Fatalf("sample_count = %d, want %d", res.SampleCount, stats.Len())
	}
	if res.FPS != stats.FPS() {
		t.Fatalf("fps = %v, want overlay %v", res.FPS, stats.FPS())
	}
	if res.FrameMs != ms(avg.Total) || res.UpdateMs != ms(avg.Update) || res.DrawMs != ms(avg.Draw) {
		t.Fatalf("avg = (%.4f, %.4f, %.4f), want (%.4f, %.4f, %.4f)",
			res.FrameMs, res.UpdateMs, res.DrawMs, ms(avg.Total), ms(avg.Update), ms(avg.Draw))
	}
	if res.LatestFrameMs != ms(latest.Total) || res.LatestUpdateMs != ms(latest.Update) || res.LatestDrawMs != ms(latest.Draw) {
		t.Fatalf("latest = (%.4f, %.4f, %.4f), want (%.4f, %.4f, %.4f)",
			res.LatestFrameMs, res.LatestUpdateMs, res.LatestDrawMs, ms(latest.Total), ms(latest.Update), ms(latest.Draw))
	}
	// Sorted totals: [10, 15, 20, 25, 30] ms -> min=10, p50=20, p95=30, p99=30, max=30.
	if res.MinFrameMs != 10 || res.MaxFrameMs != ms(stats.MaxTotal()) || res.P50FrameMs != 20 || res.P95FrameMs != 30 || res.P99FrameMs != 30 {
		t.Fatalf("percentiles = min:%v p50:%v p95:%v p99:%v max:%v, want 10/20/30/30/30",
			res.MinFrameMs, res.P50FrameMs, res.P95FrameMs, res.P99FrameMs, res.MaxFrameMs)
	}

	// Verify the rendered debug overlay HUD text matches the MCP readout verbatim.
	wantHUD := fmt.Sprintf("%.1f fps · %.2f ms (upd %.2f / drw %.2f)", res.FPS, res.FrameMs, res.UpdateMs, res.DrawMs)
	if gotHUD := app.Find("debug-frame-stats").Text; gotHUD != wantHUD {
		t.Fatalf("overlay HUD text = %q, MCP formatted = %q", gotHUD, wantHUD)
	}

	// Draw statistics match debug.Overlay.DrawStats().
	wantDraw := overlay.DrawStats()
	if res.DrawCalls != wantDraw.DrawCalls || res.Sprites != wantDraw.Sprites || res.Batches != wantDraw.Batches || res.Triangles != wantDraw.Triangles || res.Vertices != wantDraw.Vertices {
		t.Fatalf("draw stats = (%d,%d,%d,%d,%d), want %+v",
			res.DrawCalls, res.Sprites, res.Batches, res.Triangles, res.Vertices, wantDraw)
	}
	if !app.Exists("debug-draw-stats") {
		t.Fatal("expected debug-draw-stats section in visible overlay")
	}

	// GC & allocation readouts match debug.GCMonitor exactly.
	if res.AllocsPerFrame != gcMon.AllocsPerFrame() || res.AllocBytesPerFrame != gcMon.AllocBytesPerFrame() {
		t.Fatalf("allocs/frame = (%v, %v), want (%v, %v)",
			res.AllocsPerFrame, res.AllocBytesPerFrame, gcMon.AllocsPerFrame(), gcMon.AllocBytesPerFrame())
	}
	if res.AllocsPerFrame <= 0 || res.AllocBytesPerFrame <= 0 {
		t.Fatalf("expected positive allocations per frame after deliberate heap allocs, got (%v, %v)",
			res.AllocsPerFrame, res.AllocBytesPerFrame)
	}
	if res.LastFrameAllocs != gcMon.Latest().AllocsDelta || res.LastFrameAllocBytes != gcMon.Latest().AllocBytesDelta {
		t.Fatalf("last frame allocs = (%d, %d), want (%d, %d)",
			res.LastFrameAllocs, res.LastFrameAllocBytes, gcMon.Latest().AllocsDelta, gcMon.Latest().AllocBytesDelta)
	}
	if res.HeapBytes != gcMon.HeapObjectsBytes() || res.HeapBytes == 0 {
		t.Fatalf("heap_bytes = %d, want overlay %d (>0)", res.HeapBytes, gcMon.HeapObjectsBytes())
	}
	if res.GCCount != gcMon.TotalCycles() || res.GCCount == 0 {
		t.Fatalf("gc_count = %d, want overlay %d (>0)", res.GCCount, gcMon.TotalCycles())
	}
	if res.GCLastPauseMs != ms(gcMon.LastPause()) || res.GCMaxPauseMs != ms(gcMon.MaxPause()) || res.GCRecentHitches != gcMon.RecentHitches() || res.GCRecentHitches < 1 {
		t.Fatalf("gc pauses/hitches = (last=%v max=%v hitches=%d), want (%v, %v, %d)",
			res.GCLastPauseMs, res.GCMaxPauseMs, res.GCRecentHitches, ms(gcMon.LastPause()), ms(gcMon.MaxPause()), gcMon.RecentHitches())
	}

	// Asset memory matches debug.MemoryTracker exactly.
	if res.AssetMemory == nil || res.AssetMemory.TotalBytes != mem.TotalBytes() || res.AssetMemory.TotalBudget != mem.TotalBudget() || res.AssetMemory.OverBudget {
		t.Fatalf("asset_memory = %+v, want total=%d budget=%d", res.AssetMemory, mem.TotalBytes(), mem.TotalBudget())
	}
	if len(res.AssetMemory.Groups) != 2 || res.AssetMemory.Groups[0].Group != "audio" || res.AssetMemory.Groups[1].Group != "level1" {
		t.Fatalf("asset_memory.groups = %+v", res.AssetMemory.Groups)
	}
	if res.AssetMemory.Groups[1].ByKind["image"] != 256<<10 || res.AssetMemory.Groups[1].ByKind["atlas"] != 128<<10 {
		t.Fatalf("level1 by_kind = %+v", res.AssetMemory.Groups[1].ByKind)
	}

	// Default readout omits history and zones.
	if len(res.History) != 0 || len(res.Zones) != 0 {
		t.Fatalf("expected history and zones omitted by default, got len(history)=%d len(zones)=%d", len(res.History), len(res.Zones))
	}

	// Request with include_history + history_limit and include_zones + zones_limit.
	detailed := callTool[mcp.PerfReadoutResult](t, app, cx, mcp.ToolPerfReadout, map[string]any{
		"include_history": true,
		"history_limit":   3,
		"include_zones":   true,
		"zones_limit":     2,
	})
	if len(detailed.History) != 3 {
		t.Fatalf("len(history) = %d, want 3", len(detailed.History))
	}
	// Last 3 frames are frames[2], frames[3] (GC hitch), frames[4].
	if detailed.History[0].FrameMs != 20 || detailed.History[1].FrameMs != 25 || !detailed.History[1].GCHitch || detailed.History[2].FrameMs != 30 {
		t.Fatalf("history tail = %+v, want [20ms, 25ms+hitch, 30ms]", detailed.History)
	}
	if len(detailed.Zones) != 2 {
		t.Fatalf("len(zones) = %d, want 2", len(detailed.Zones))
	}
	wantTop := prof.TopScopes(2)
	for i, want := range wantTop {
		got := detailed.Zones[i]
		if got.Name != want.Name || got.LastMs != ms(want.Total) || got.AvgMs != ms(want.Avg) || got.MaxMs != ms(want.Max) || got.Calls != want.Calls {
			t.Fatalf("zone[%d] = %+v, want %+v", i, got, want)
		}
	}
}

func TestPerfZonesAndReset(t *testing.T) {
	now := time.Unix(6000, 0)
	prof := debug.NewProfiler(30)
	prof.SetNow(func() time.Time { return now })
	gcMon := debug.NewGCMonitor(16)
	overlay := debug.New(debug.Options{
		Visible:  true,
		Profiler: prof,
		GC:       gcMon,
	})

	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{Overlay: overlay}); err != nil {
			t.Fatal(err)
		}
		return core.View{
			Style:    core.Style{Width: core.Pt(400), Height: core.Pt(300)},
			Children: []core.Node{overlay.View(cx)},
		}
	})

	app.Post(func() {
		prof.StartRecording(10)
		for _, physMs := range []time.Duration{2 * time.Millisecond, 6 * time.Millisecond} {
			prof.BeginFrame()
			t1 := prof.Begin("physics")
			now = now.Add(physMs)
			prof.End(t1)
			t2 := prof.Begin("render")
			now = now.Add(3 * time.Millisecond)
			prof.End(t2)
			t3 := prof.Begin("audio")
			now = now.Add(1 * time.Millisecond)
			prof.End(t3)
			prof.EndFrame()
			overlay.RecordFrame(16*time.Millisecond, physMs, 3*time.Millisecond)
		}
		overlay.SetDrawStats(debug.DrawStats{DrawCalls: 9, Sprites: 50, Batches: 2})
	})
	app.Settle()

	// All zones.
	zonesAll := callTool[mcp.PerfZonesResult](t, app, cx, mcp.ToolPerfZones, nil)
	if !zonesAll.Recording || zonesAll.RecordedFrames != 2 || zonesAll.Total != 3 || len(zonesAll.Zones) != 3 {
		t.Fatalf("zonesAll = %+v, want recording=true recorded=2 total=3 len=3", zonesAll)
	}
	if zonesAll.Zones[0].Name != "physics" || !approxEqual(zonesAll.Zones[0].LastMs, 6, 1e-6) || !approxEqual(zonesAll.Zones[0].AvgMs, 4, 1e-6) || !approxEqual(zonesAll.Zones[0].MaxMs, 6, 1e-6) || zonesAll.Zones[0].Calls != 1 {
		t.Fatalf("zonesAll[0] = %+v, want physics last=6 avg=4 max=6 calls=1", zonesAll.Zones[0])
	}

	// Limited zones still reports Total == 3.
	zonesTop1 := callTool[mcp.PerfZonesResult](t, app, cx, mcp.ToolPerfZones, map[string]any{"limit": 1})
	if zonesTop1.Total != 3 || len(zonesTop1.Zones) != 1 || zonesTop1.Zones[0].Name != "physics" {
		t.Fatalf("zonesTop1 = %+v", zonesTop1)
	}

	// Reset clears overlay frame stats, pushed draw stats, profiler zones and GC history.
	resetRes := callTool[mcp.PerfResetResult](t, app, cx, mcp.ToolPerfReset, nil)
	if !resetRes.ResetFrameStats || !resetRes.ResetProfiler || !resetRes.ResetGC || !resetRes.ResetDrawStats {
		t.Fatalf("resetRes = %+v, want all true", resetRes)
	}

	after := callTool[mcp.PerfReadoutResult](t, app, cx, mcp.ToolPerfReadout, map[string]any{
		"include_history": true,
		"include_zones":   true,
	})
	if after.SampleCount != 0 || after.FPS != 0 || after.FrameMs != 0 || after.DrawCalls != 0 || after.AllocsPerFrame != 0 || after.GCRecentHitches != 0 {
		t.Fatalf("after reset readout = %+v, want zeroed metrics", after)
	}
	if after.Sources.DrawStats {
		t.Fatalf("expected sources.draw_stats=false after resetting pushed draw stats")
	}
	if gotHUD := app.Find("debug-frame-stats").Text; gotHUD != "0.0 fps · 0.00 ms" {
		t.Fatalf("overlay HUD after reset = %q, want %q", gotHUD, "0.0 fps · 0.00 ms")
	}

	// Zones after reset returns empty JSON array (not null).
	rawZones, err := callToolRaw(t, app, cx, mcp.ToolPerfZones, nil)
	if err != nil {
		t.Fatalf("game_perf_zones after reset failed: %v", err)
	}
	if !strings.Contains(string(rawZones), `"zones":[]`) {
		t.Fatalf("expected empty JSON array for zones after reset, got %s", rawZones)
	}
}

func TestPerfRender3DAndStandaloneProviders(t *testing.T) {
	r3d := render3d.RenderStats{
		TotalItems:       120,
		CulledItems:      35,
		Occluded:         15,
		VisibleItems:     70,
		OpaqueDraws:      12,
		TransparentDraws: 4,
		InstancedBatches: 6,
		TotalDrawCalls:   16,
		Triangles:        48_000,
		Vertices:         96_000,
		MaterialSwitches: 9,
		PipelineSwitches: 3,
		TextureBytes:     16 << 20,
		BufferBytes:      4 << 20,
	}

	t.Run("RenderStatsFuncOnly", func(t *testing.T) {
		var cx *core.Scope
		app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
			cx = scope
			if _, err := mcp.Attach(cx, mcp.Config{
				RenderStatsFunc: func() render3d.RenderStats { return r3d },
			}); err != nil {
				t.Fatal(err)
			}
			return core.View{}
		})

		res := callTool[mcp.PerfReadoutResult](t, app, cx, mcp.ToolPerfReadout, nil)
		if !res.Sources.Render3D || res.Sources.Overlay || res.Sources.DrawStats {
			t.Fatalf("sources = %+v, want only render3d=true", res.Sources)
		}
		// Top-level draw counts inherit from RenderStatsFunc when no 2D DrawStats is set.
		if res.DrawCalls != 16 || res.Batches != 6 || res.Triangles != 48_000 || res.Vertices != 96_000 {
			t.Fatalf("top-level draw counts = (%d,%d,%d,%d), want (16,6,48000,96000)",
				res.DrawCalls, res.Batches, res.Triangles, res.Vertices)
		}
		if res.Render3D == nil || res.Render3D.CulledItems != 35 || res.Render3D.Occluded != 15 || res.Render3D.MaterialSwitches != 9 || res.Render3D.TextureBytes != 16<<20 || res.Render3D.BufferBytes != 4<<20 {
			t.Fatalf("render3d = %+v, want exact match with %+v", res.Render3D, r3d)
		}
	})

	t.Run("DrawStatsFuncOverridesTopLevelAlongsideRender3D", func(t *testing.T) {
		var cx *core.Scope
		app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
			cx = scope
			if _, err := mcp.Attach(cx, mcp.Config{
				DrawStatsFunc: func() debug.DrawStats {
					return debug.DrawStats{DrawCalls: 5, Sprites: 80, Batches: 2, Triangles: 160, Vertices: 320}
				},
				RenderStatsFunc: func() render3d.RenderStats { return r3d },
			}); err != nil {
				t.Fatal(err)
			}
			return core.View{}
		})

		res := callTool[mcp.PerfReadoutResult](t, app, cx, mcp.ToolPerfReadout, nil)
		if !res.Sources.DrawStats || !res.Sources.Render3D {
			t.Fatalf("sources = %+v, want draw_stats and render3d", res.Sources)
		}
		if res.DrawCalls != 5 || res.Sprites != 80 || res.Batches != 2 || res.Triangles != 160 || res.Vertices != 320 {
			t.Fatalf("top-level draw stats = (%d,%d,%d,%d,%d), want DrawStatsFunc values",
				res.DrawCalls, res.Sprites, res.Batches, res.Triangles, res.Vertices)
		}
		if res.Render3D == nil || res.Render3D.TotalDrawCalls != 16 || res.Render3D.Triangles != 48_000 {
			t.Fatalf("render3d = %+v, want full 3D breakdown", res.Render3D)
		}
	})

	t.Run("StandaloneProfilerWithoutOverlay", func(t *testing.T) {
		now := time.Unix(7000, 0)
		prof := debug.NewProfiler(10)
		prof.SetNow(func() time.Time { return now })
		prof.BeginFrame()
		tok := prof.Begin("sim")
		now = now.Add(2500 * time.Microsecond)
		prof.End(tok)
		prof.EndFrame()

		var cx *core.Scope
		app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
			cx = scope
			if _, err := mcp.Attach(cx, mcp.Config{Profiler: prof}); err != nil {
				t.Fatal(err)
			}
			return core.View{}
		})

		res := callTool[mcp.PerfReadoutResult](t, app, cx, mcp.ToolPerfReadout, map[string]any{"include_zones": true})
		if !res.Sources.Profiler || res.Sources.Overlay {
			t.Fatalf("sources = %+v, want profiler=true overlay=false", res.Sources)
		}
		if len(res.Zones) != 1 || res.Zones[0].Name != "sim" || !approxEqual(res.Zones[0].LastMs, 2.5, 1e-6) {
			t.Fatalf("zones = %+v, want [sim 2.5ms]", res.Zones)
		}
	})
}

func TestPerfErrorsAndStrictArguments(t *testing.T) {
	t.Run("NoTelemetryConfigured", func(t *testing.T) {
		var cx *core.Scope
		app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
			cx = scope
			if _, err := mcp.Attach(cx, mcp.Config{}); err != nil {
				t.Fatal(err)
			}
			return core.View{}
		})

		if _, err := callToolRaw(t, app, cx, mcp.ToolPerfReadout, nil); err == nil || !strings.Contains(err.Error(), "no performance telemetry") {
			t.Fatalf("readout unconfigured = %v, want no performance telemetry error", err)
		}
		if _, err := callToolRaw(t, app, cx, mcp.ToolPerfZones, nil); err == nil || !strings.Contains(err.Error(), "no profiler configured") {
			t.Fatalf("zones unconfigured = %v, want no profiler configured error", err)
		}
		if _, err := callToolRaw(t, app, cx, mcp.ToolPerfReset, nil); err == nil || !strings.Contains(err.Error(), "no performance telemetry") {
			t.Fatalf("reset unconfigured = %v, want no performance telemetry error", err)
		}

		// Unknown fields rejected on every perf tool.
		for _, tool := range []string{mcp.ToolPerfReadout, mcp.ToolPerfZones, mcp.ToolPerfReset} {
			if _, err := callToolRaw(t, app, cx, tool, `{"bogus":1}`); err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("%s unknown field = %v, want unknown field error", tool, err)
			}
		}

		// Negative limits rejected.
		if _, err := callToolRaw(t, app, cx, mcp.ToolPerfReadout, map[string]any{"history_limit": -1}); err == nil || !strings.Contains(err.Error(), "history_limit") {
			t.Fatalf("negative history_limit = %v", err)
		}
		if _, err := callToolRaw(t, app, cx, mcp.ToolPerfReadout, map[string]any{"zones_limit": -1}); err == nil || !strings.Contains(err.Error(), "zones_limit") {
			t.Fatalf("negative zones_limit = %v", err)
		}
		if _, err := callToolRaw(t, app, cx, mcp.ToolPerfZones, map[string]any{"limit": -3}); err == nil || !strings.Contains(err.Error(), "limit") {
			t.Fatalf("negative zones limit = %v", err)
		}
	})

	t.Run("OverlayWithoutProfilerGraceful", func(t *testing.T) {
		overlay := debug.New(debug.Options{})
		overlay.RecordFrame(16*time.Millisecond, 3*time.Millisecond, 2*time.Millisecond)

		var cx *core.Scope
		app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
			cx = scope
			if _, err := mcp.Attach(cx, mcp.Config{Overlay: overlay}); err != nil {
				t.Fatal(err)
			}
			return core.View{}
		})

		raw, err := callToolRaw(t, app, cx, mcp.ToolPerfReadout, map[string]any{"include_zones": true, "include_history": true})
		if err != nil {
			t.Fatalf("readout with overlay-only failed: %v", err)
		}
		var res mcp.PerfReadoutResult
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatal(err)
		}
		if !res.Sources.Overlay || res.Sources.Profiler || res.SampleCount != 1 || len(res.History) != 1 || len(res.Zones) != 0 {
			t.Fatalf("overlay-only readout = %+v", res)
		}
		if _, err := callToolRaw(t, app, cx, mcp.ToolPerfZones, nil); err == nil || !strings.Contains(err.Error(), "no profiler configured") {
			t.Fatalf("zones without profiler = %v, want error", err)
		}
	})
}
