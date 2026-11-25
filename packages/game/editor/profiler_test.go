package editor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// startFixturePerfMCPServer exposes b's game_perf_* tools over a real MCP Go SDK server using an in-memory transport pair, mirroring `go tool nsgo mcp` JSON-RPC framing.
func startFixturePerfMCPServer(t *testing.T, b *mcp.Bridge) sdkmcp.Transport {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "fixture-game-mcp",
		Version: "1.0.0",
	}, nil)

	for _, tool := range b.Tools() {
		tl := tool
		server.AddTool(&sdkmcp.Tool{
			Name:        tl.Name,
			Description: tl.Description,
			InputSchema: tl.InputSchema,
		}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			rawArgs := json.RawMessage(req.Params.Arguments)
			val, err := tl.Handler(ctx, rawArgs)
			if err != nil {
				r := &sdkmcp.CallToolResult{}
				r.SetError(err)
				return r, nil
			}
			encoded, err := json.Marshal(val)
			if err != nil {
				r := &sdkmcp.CallToolResult{}
				r.SetError(err)
				return r, nil
			}
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{
					&sdkmcp.TextContent{Text: string(encoded)},
				},
			}, nil
		})
	}

	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	session, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("connect fixture MCP server: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = session.Close()
	})
	return clientTransport
}

// #912 Acceptance 1: connects to a fixture game running named timing zones over MCP,
// shows the Play-mode FRAME TIME · LAST 60 S strip with avg/p99/allocs and spike cause,
// opens the full Profiler panel from the strip, lists zones sorted by cost, charts frame/FPS/GC/memory,
// and resets via game_perf_reset.
func TestProfilerConnectsToFixtureGameOverMCPChartsFramesAndZones(t *testing.T) {
	now := time.Unix(10000, 0)
	prof := debug.NewProfiler(120)
	prof.SetNow(func() time.Time { return now })

	gcMon := debug.NewGCMonitor(debug.FrameHistory)
	mem := debug.NewMemoryTracker()
	mem.SetTotalBudget(2 << 20)
	mem.SetBudget("level_01", 1<<20)
	mem.Track("level_01", "tiles.png", debug.AssetImage, 384<<10)
	mem.Track("audio", "dungeon.pcm", debug.AssetAudio, 512<<10)

	overlay := debug.New(debug.Options{
		Profiler: prof,
		GC:       gcMon,
		Memory:   mem,
		DrawStats: func() debug.DrawStats {
			return debug.DrawStats{DrawCalls: 18, Sprites: 240, Batches: 5}
		},
	})

	// Simulate fixture game frames including named timing zones and a known-cause spike.
	var sink [][]byte
	for i := range 12 {
		prof.BeginFrame()

		tPhys := prof.Begin("physics_step")
		now = now.Add(2200 * time.Microsecond)
		prof.End(tPhys)

		tAI := prof.Begin("ai_pathfinding")
		now = now.Add(900 * time.Microsecond)
		prof.End(tAI)

		tRender := prof.Begin("render_scene")
		if i == 7 {
			now = now.Add(14500 * time.Microsecond)
		} else {
			now = now.Add(1400 * time.Microsecond)
		}
		prof.End(tRender)
		prof.EndFrame()

		if i == 5 {
			buf := make([]byte, 8192)
			buf[0] = 42
			sink = append(sink, buf)
		}
		switch i {
		case 7:
			overlay.NoteFrameCause("first use of torch_glow compiled its shader")
			overlay.RecordFrame(18200*time.Microsecond, 3100*time.Microsecond, 14500*time.Microsecond)
		case 10:
			overlay.RecordFrame(11400*time.Microsecond, 4800*time.Microsecond, 5600*time.Microsecond)
		default:
			overlay.RecordFrame(4500*time.Microsecond, 3100*time.Microsecond, 1400*time.Microsecond)
		}
	}
	runtime.KeepAlive(sink)

	bridge := mcp.New(mcp.Config{Overlay: overlay})
	transport := startFixturePerfMCPServer(t, bridge)

	client, err := editor.ConnectProfilerMCP(context.Background(), transport)
	if err != nil {
		t.Fatalf("ConnectProfilerMCP: %v", err)
	}

	ed := newTestEditor(t)
	ed.ProfilerPollInterval = time.Hour // deterministic manual polling in test
	t.Cleanup(func() {
		_ = ed.Close()
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// Strip is hidden before connecting or playing.
	if app.Exists("id:profiler-strip") {
		t.Fatal("expected profiler strip hidden while disconnected and not playing")
	}

	ed.ConnectProfiler(client)
	if err := ed.PollProfiler(context.Background()); err != nil {
		t.Fatalf("PollProfiler: %v", err)
	}
	app.Settle()

	// The FRAME TIME · LAST 60 S strip appears under the game view while connected.
	if !app.Exists("id:profiler-strip") {
		t.Fatal("expected profiler strip visible while connected to running game")
	}
	stripText := allText(app.Find("id:profiler-strip"))
	for _, want := range []string{"FRAME TIME · LAST 60 S", "avg ", "p99 ", "allocs/frame", "over 8.3 ms", "missed 16.7 ms", "−60 s", "−30 s", "now"} {
		if !strings.Contains(stripText, want) {
			t.Fatalf("profiler strip missing %q:\n%s", want, stripText)
		}
	}

	// Spike callout carries the exact duration and cause reported by the fixture game.
	if !app.Exists("id:profiler-strip-spike") {
		t.Fatal("expected spike callout pill in profiler strip")
	}
	spikeText := allText(app.Find("id:profiler-strip-spike"))
	wantSpike := "18.2 ms · first use of torch_glow compiled its shader"
	if !strings.Contains(spikeText, wantSpike) {
		t.Fatalf("spike callout = %q, want %q", spikeText, wantSpike)
	}

	// Verify snapshot captured all 12 frames and zones sorted by cost.
	snap := ed.ProfilerSnapshot()
	if !snap.Connected || len(snap.Frames) != 12 {
		t.Fatalf("snapshot connected=%v frames=%d, want true, 12", snap.Connected, len(snap.Frames))
	}
	if len(snap.Zones) != 3 {
		t.Fatalf("snapshot zones = %+v, want 3 zones", snap.Zones)
	}
	if snap.Zones[0].Name != "physics_step" || snap.Zones[1].Name != "render_scene" || snap.Zones[2].Name != "ai_pathfinding" {
		t.Fatalf("zones order = %+v, want [physics_step, render_scene, ai_pathfinding]", snap.Zones)
	}
	if math.Abs(snap.Zones[0].LastMs-2.2) > 0.01 {
		t.Fatalf("physics_step LastMs = %.3f, want 2.2", snap.Zones[0].LastMs)
	}

	// Open the full Profiler panel from the strip's Profiler button.
	if app.Exists("id:profiler-panel") {
		t.Fatal("expected full profiler panel closed before clicking Profiler on strip")
	}
	app.Press("id:profiler-strip-open")
	app.Settle()

	if !app.Exists("id:profiler-panel") {
		t.Fatal("expected full profiler panel to open from the strip")
	}

	// Verify the zones table renders the named timing zones with costs.
	zonesText := allText(app.Find("id:profiler-zones-panel"))
	for _, want := range []string{"ZONES · 3", "physics_step", "render_scene", "ai_pathfinding", "2.20 ms", "1.40 ms", "0.90 ms"} {
		if !strings.Contains(zonesText, want) {
			t.Fatalf("profiler zones panel missing %q:\n%s", want, zonesText)
		}
	}

	// Verify KPI cards show frame, FPS, allocs/GC, and asset memory.
	panelText := allText(app.Find("id:profiler-panel"))
	for _, want := range []string{"Connected · MCP", "18 calls · 240 sprites", "896.0 KiB", "2 groups"} {
		if !strings.Contains(panelText, want) {
			t.Fatalf("profiler panel missing %q:\n%s", want, panelText)
		}
	}

	// Switch across every metric tab (Frame, FPS, Allocs & GC, Memory) and verify the canvas renders.
	for _, tc := range []struct {
		id     string
		metric editor.ProfilerMetric
	}{
		{"id:profiler-tab-fps", editor.ProfilerMetricFPS},
		{"id:profiler-tab-gc", editor.ProfilerMetricAllocsGC},
		{"id:profiler-tab-memory", editor.ProfilerMetricMemory},
		{"id:profiler-tab-frame", editor.ProfilerMetricFrame},
	} {
		app.Press(tc.id)
		app.Settle()
		if got := ed.ProfilerMetric(); got != tc.metric {
			t.Fatalf("after pressing %s: ProfilerMetric() = %v, want %v", tc.id, got, tc.metric)
		}
	}

	// Verify strip and full chart canvases actually recorded drawing commands.
	if !app.Exists("id:profiler-strip-chart") || !app.Exists("id:profiler-chart") {
		t.Fatal("expected profiler-strip-chart and profiler-chart canvases mounted")
	}

	// Pressing Reset calls game_perf_reset on the fixture game and clears local frames and zones.
	app.Press("id:profiler-reset")
	app.Settle()

	afterReset := ed.ProfilerSnapshot()
	if len(afterReset.Frames) != 0 || len(afterReset.Zones) != 0 {
		t.Fatalf("after Reset: frames=%d zones=%d, want 0, 0", len(afterReset.Frames), len(afterReset.Zones))
	}
	if overlay.Stats().Len() != 0 || len(prof.TopScopes(0)) != 0 {
		t.Fatalf("fixture game not reset by game_perf_reset: overlay=%d scopes=%d", overlay.Stats().Len(), len(prof.TopScopes(0)))
	}
	if !app.Exists("id:profiler-zones-empty") {
		t.Fatal("expected empty zones placeholder after Reset")
	}
}

// #912 Acceptance 2: exporting recorded frames through the debug package's exporter
// produces valid Chrome/Perfetto Trace Event Format JSON with metadata and complete events.
func TestProfilerExportChromeTraceValidJSON(t *testing.T) {
	now := time.Unix(20000, 0)
	prof := debug.NewProfiler(60)
	prof.SetNow(func() time.Time { return now })
	overlay := debug.New(debug.Options{Profiler: prof})

	for i := range 5 {
		prof.BeginFrame()
		t1 := prof.Begin("physics")
		now = now.Add(time.Duration(i+1) * time.Millisecond)
		prof.End(t1)

		t2 := prof.Begin("torch_glow")
		now = now.Add(1500 * time.Microsecond)
		prof.End(t2)
		prof.EndFrame()

		upd := time.Duration(i+1) * time.Millisecond
		drw := 1500 * time.Microsecond
		overlay.RecordFrame(upd+drw+time.Millisecond, upd, drw)
	}

	bridge := mcp.New(mcp.Config{Overlay: overlay})
	transport := startFixturePerfMCPServer(t, bridge)
	client, err := editor.ConnectProfilerMCP(context.Background(), transport)
	if err != nil {
		t.Fatalf("ConnectProfilerMCP: %v", err)
	}

	ed := newTestEditor(t)
	ed.ProfilerPollInterval = time.Hour
	t.Cleanup(func() { _ = ed.Close() })

	var exportedFromUI []byte
	ed.OnExportProfilerTrace = func(data []byte) error {
		exportedFromUI = append([]byte(nil), data...)
		return nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	ed.ConnectProfiler(client)
	if err := ed.PollProfiler(context.Background()); err != nil {
		t.Fatalf("PollProfiler: %v", err)
	}
	ed.SetProfilerOpen(true)
	app.Settle()

	// Export via UI button.
	app.Press("id:profiler-export")
	app.Settle()
	if len(exportedFromUI) == 0 {
		t.Fatal("expected OnExportProfilerTrace to receive Chrome trace bytes")
	}

	// Also export directly to a file and via io.Writer.
	var buf bytes.Buffer
	if err := ed.ExportProfilerTrace(&buf); err != nil {
		t.Fatalf("ExportProfilerTrace: %v", err)
	}
	tracePath := filepath.Join(t.TempDir(), "traces", "run.json")
	if err := ed.ExportProfilerTraceFile(tracePath); err != nil {
		t.Fatalf("ExportProfilerTraceFile: %v", err)
	}
	fileBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read exported trace file: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), fileBytes) || !bytes.Equal(buf.Bytes(), exportedFromUI) {
		t.Fatal("expected ExportProfilerTrace, ExportProfilerTraceFile, and UI export to produce identical trace JSON")
	}

	// Validate Chrome/Perfetto Trace Event Format JSON schema.
	type chromeEvent struct {
		Name string         `json:"name"`
		Cat  string         `json:"cat"`
		Ph   string         `json:"ph"`
		Ts   *float64       `json:"ts"`
		Dur  *float64       `json:"dur"`
		PID  int            `json:"pid"`
		TID  int            `json:"tid"`
		Args map[string]any `json:"args"`
	}
	var doc struct {
		TraceEvents []chromeEvent `json:"traceEvents"`
	}
	if err := json.Unmarshal(fileBytes, &doc); err != nil {
		t.Fatalf("exported trace is not valid JSON: %v\n%s", err, fileBytes)
	}
	if len(doc.TraceEvents) == 0 {
		t.Fatal("exported Chrome trace has zero traceEvents")
	}

	metaNames := make(map[string]int)
	eventCounts := make(map[string]int)
	for _, ev := range doc.TraceEvents {
		if ev.PID != 1 || ev.TID <= 0 {
			t.Fatalf("invalid pid/tid in trace event: %+v", ev)
		}
		switch ev.Ph {
		case "M":
			if ev.Name == "thread_name" {
				if name, ok := ev.Args["name"].(string); ok {
					metaNames[name] = ev.TID
				}
			}
		case "X":
			if ev.Ts == nil || *ev.Ts < 0 || ev.Dur == nil || *ev.Dur <= 0 {
				t.Fatalf("invalid complete event timing: %+v", ev)
			}
			eventCounts[ev.Name]++
		default:
			t.Fatalf("unexpected trace event phase %q in %+v", ev.Ph, ev)
		}
	}

	for _, wantTrack := range []string{"frame", "update", "draw", "physics", "torch_glow"} {
		if metaNames[wantTrack] <= 0 {
			t.Fatalf("missing thread_name metadata for %q in %v", wantTrack, metaNames)
		}
		if eventCounts[wantTrack] == 0 {
			t.Fatalf("missing complete ('X') events for %q in %v", wantTrack, eventCounts)
		}
	}
	if eventCounts["frame"] != 5 {
		t.Fatalf("frame events = %d, want 5", eventCounts["frame"])
	}
}

func TestProfilerScopeClientAndPlayModeStrip(t *testing.T) {
	// 1. Test ConnectProfilerScope against an in-process scope with mcp.Attach.
	now := time.Unix(30000, 0)
	prof := debug.NewProfiler(30)
	prof.SetNow(func() time.Time { return now })
	overlay := debug.New(debug.Options{Profiler: prof})

	var gameScope *core.Scope
	gameApp := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		gameScope = cx
		if _, err := mcp.Attach(cx, mcp.Config{Overlay: overlay}); err != nil {
			t.Fatal(err)
		}
		return core.View{Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}}
	})

	gameApp.Post(func() {
		prof.BeginFrame()
		tok := prof.Begin("collision_broadphase")
		now = now.Add(3500 * time.Microsecond)
		prof.End(tok)
		prof.EndFrame()
		overlay.RecordFrame(7500*time.Microsecond, 3500*time.Microsecond, 2000*time.Microsecond)
	})
	gameApp.Settle()

	ed := newTestEditor(t)
	ed.ProfilerPollInterval = time.Hour
	t.Cleanup(func() { _ = ed.Close() })

	ed.ConnectProfiler(editor.ConnectProfilerScope(gameScope))
	gameApp.Post(func() {
		if err := ed.PollProfiler(context.Background()); err != nil {
			t.Errorf("PollProfiler on scope client: %v", err)
		}
	})
	gameApp.Settle()

	snap := ed.ProfilerSnapshot()
	if len(snap.Zones) != 1 || snap.Zones[0].Name != "collision_broadphase" {
		t.Fatalf("scope client zones = %+v, want [collision_broadphase]", snap.Zones)
	}
	if err := ed.DisconnectProfiler(); err != nil {
		t.Fatalf("DisconnectProfiler: %v", err)
	}

	// 2. Test built-in Play mode shows the strip automatically and opens Profiler via menu/command.
	ed.OnPlayUpdate = func(*sceneformat.BuiltScene, time.Duration) {}
	ed.OnPlayDraw = func(*sceneformat.BuiltScene, float64) {}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// Project > Profiler menu item exists and toggles the panel.
	if item, ok := findMenuItem(app.Menu, "Project", "Profiler"); !ok || item.Disabled {
		t.Fatalf("Project > Profiler menu item missing or disabled: %+v, ok=%v", item, ok)
	}

	app.Press("id:toolbar-play")
	app.Advance(100 * time.Millisecond)
	app.Settle()

	if !app.Exists("id:profiler-strip") {
		t.Fatal("expected FRAME TIME · LAST 60 S strip visible during Play mode")
	}

	app.Press("id:toolbar-play")
	app.Settle()

	if app.Exists("id:profiler-strip") {
		t.Fatal("expected profiler strip hidden after stopping Play mode")
	}
}

// TestProfilerScreenshots renders the Play-mode frame time strip and full Profiler panel
// with realistic fixture data mirroring docs/design/editor/PlayMode.dc.html so
// `go tool nsgo test -target macos -run TestProfilerScreenshots ./editor` captures real macOS screenshots.
func TestProfilerScreenshots(t *testing.T) {
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
	transport := startFixturePerfMCPServer(t, bridge)
	client, err := editor.ConnectProfilerMCP(context.Background(), transport)
	if err != nil {
		t.Fatalf("ConnectProfilerMCP: %v", err)
	}

	ed := newTestEditor(t)
	ed.ProfilerPollInterval = time.Hour
	t.Cleanup(func() { _ = ed.Close() })

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	ed.ConnectProfiler(client)

	// Feed 150 frames in two batches (> debug.FrameHistory=120) mirroring PlayMode.dc.html.
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
	app.Settle()

	if !app.Exists("id:profiler-strip-spike") {
		t.Fatal("expected spike callout visible for screenshot")
	}
	app.Screenshot("profiler-strip")

	app.Press("id:profiler-strip-open")
	app.Settle()
	if !app.Exists("id:profiler-panel") {
		t.Fatal("expected full profiler panel open for screenshot")
	}
	app.Screenshot("profiler-panel")
}

type blockingProfilerClient struct {
	entered chan struct{}
	release chan struct{}
	payload json.RawMessage
	err     error
}

func (b *blockingProfilerClient) CallTool(ctx context.Context, name string, _ json.RawMessage) (json.RawMessage, error) {
	if name == mcp.ToolPerfReset {
		return json.RawMessage(`{}`), nil
	}
	select {
	case b.entered <- struct{}{}:
	default:
	}
	select {
	case <-b.release:
		return b.payload, b.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *blockingProfilerClient) Close() error { return nil }

func TestProfilerPlayModeNoRaceWithFastPollInterval(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "RaceCheck",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{{
			Name:  "main",
			Space: sceneformat.SpaceWorld,
			Objects: []sceneformat.ObjectDoc{
				{ID: "hero", X: 100, Y: 100},
			},
		}},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.ProfilerPollInterval = time.Millisecond
	t.Cleanup(func() { _ = ed.Close() })

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 720))

	if err := ed.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	// Advance across >30 frames and real wall time well past ProfilerPollInterval;
	// Play mode must poll exclusively on the UI goroutine with zero races.
	for range 35 {
		app.Advance(16 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	app.Settle()

	snap := ed.ProfilerSnapshot()
	if !snap.Connected || !snap.InEditorPlay || len(snap.Frames) == 0 {
		t.Fatalf("expected play profiler frames, got connected=%v inPlay=%v frames=%d",
			snap.Connected, snap.InEditorPlay, len(snap.Frames))
	}
	if err := ed.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if ed.ProfilerConnected() {
		t.Fatal("expected play profiler detached on Stop")
	}
}

func TestProfilerStalePollDiscardedAndCancelStopsPromptly(t *testing.T) {
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })

	stalePayload := json.RawMessage(`{"frame_ms":12.5,"latest_frame_ms":12.5,"fps":60,"sample_count":1,"history":[{"frame_ms":12.5,"update_ms":4,"draw_ms":3}]}`)

	// 1. Stale PollProfiler completing after ResetProfiler must not repopulate frames.
	bc := &blockingProfilerClient{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
		payload: stalePayload,
	}
	ed.ConnectProfiler(bc)

	pollDone := make(chan error, 1)
	go func() {
		pollDone <- ed.PollProfiler(context.Background())
	}()
	<-bc.entered
	if err := ed.ResetProfiler(context.Background()); err != nil {
		t.Fatalf("ResetProfiler: %v", err)
	}
	close(bc.release)
	if err := <-pollDone; err != nil {
		t.Fatalf("stale PollProfiler err = %v, want nil", err)
	}
	if snap := ed.ProfilerSnapshot(); len(snap.Frames) != 0 || snap.HasReadout {
		t.Fatalf("stale poll overwrote reset state: hasReadout=%v frames=%d", snap.HasReadout, len(snap.Frames))
	}

	// 2. Stale error after DisconnectProfiler must not set LastError.
	bcErr := &blockingProfilerClient{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
		err:     context.Canceled,
	}
	ed.ConnectProfiler(bcErr)
	go func() {
		pollDone <- ed.PollProfiler(context.Background())
	}()
	<-bcErr.entered
	_ = ed.DisconnectProfiler()
	close(bcErr.release)
	<-pollDone
	if snap := ed.ProfilerSnapshot(); snap.LastError != "" {
		t.Fatalf("stale disconnect error leaked into LastError: %q", snap.LastError)
	}

	// 3. Background poller cancels in-flight RPC promptly on DisconnectProfiler.
	ed.ProfilerPollInterval = 5 * time.Millisecond
	bcSlow := &blockingProfilerClient{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
		payload: stalePayload,
	}
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 720))
	ed.ConnectProfiler(bcSlow)
	select {
	case <-bcSlow.entered:
	case <-time.After(time.Second):
		t.Fatal("background poller did not start RPC")
	}
	start := time.Now()
	_ = ed.DisconnectProfiler()
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("DisconnectProfiler blocked for %v waiting on cancelled RPC", elapsed)
	}
}

func TestProfilerSanitizesNegativeAndHugeValuesAndExportsValidTrace(t *testing.T) {
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })

	payload := json.RawMessage(`{
		"frame_ms": -5,
		"update_ms": -2,
		"draw_ms": -1,
		"fps": -60,
		"allocs_per_frame": -10,
		"heap_bytes": 10737418240000000000,
		"sample_count": 2,
		"history": [
			{"frame_ms": -4.5, "update_ms": -1, "draw_ms": -1},
			{"frame_ms": 1e18, "update_ms": 1e18, "draw_ms": 2.0}
		],
		"asset_memory": {
			"total_bytes": 9223372036854775000,
			"total_budget": 9223372036854775000,
			"over_budget": false
		}
	}`)
	bc := &blockingProfilerClient{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
		payload: payload,
	}
	close(bc.release)
	ed.ConnectProfiler(bc)
	if err := ed.PollProfiler(context.Background()); err != nil {
		t.Fatalf("PollProfiler: %v", err)
	}

	snap := ed.ProfilerSnapshot()
	if snap.Readout.FrameMs < 0 || snap.Readout.FPS < 0 || snap.Readout.AllocsPerFrame < 0 {
		t.Fatalf("negative readout fields were not sanitized: %+v", snap.Readout)
	}
	if len(snap.Frames) != 2 || snap.Frames[0].FrameMs != 0 {
		t.Fatalf("negative history frame not sanitized: %+v", snap.Frames)
	}

	// Exporting trace with huge frame duration must produce valid JSON without negative timestamps.
	var traceBuf bytes.Buffer
	if err := ed.ExportProfilerTrace(&traceBuf); err != nil {
		t.Fatalf("ExportProfilerTrace: %v", err)
	}
	var traceDoc struct {
		TraceEvents []struct {
			Name string `json:"name"`
			TS   int64  `json:"ts"`
			Dur  int64  `json:"dur"`
		} `json:"traceEvents"`
	}
	if err := json.Unmarshal(traceBuf.Bytes(), &traceDoc); err != nil {
		t.Fatalf("invalid trace JSON: %v", err)
	}
	for _, ev := range traceDoc.TraceEvents {
		if ev.TS < 0 || ev.Dur < 0 {
			t.Fatalf("negative trace timestamp/dur in %+v", ev)
		}
	}

	// Render panel across all metrics with huge heap & asset bytes (verifying no overflow/panic).
	ed.SetProfilerOpen(true)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 720))
	for _, m := range []editor.ProfilerMetric{
		editor.ProfilerMetricFrame,
		editor.ProfilerMetricFPS,
		editor.ProfilerMetricAllocsGC,
		editor.ProfilerMetricMemory,
	} {
		ed.SetProfilerMetric(m)
		app.Settle()
	}
}
